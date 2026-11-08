package database

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

// MediaRef is the canonical identity of a media entity: which table its row
// lives in, and the row's id.
type MediaRef struct {
	MediaType string
	MediaID   int64
}

// IDRef is one item's external-id set as a client sent it. Fields may be zero:
// a TVDB-first client sends TVDB alone, a Trakt-shaped one sends TMDB.
type IDRef struct {
	TMDB int
	TVDB int
	IMDB string
}

// Empty reports whether the ref carries nothing to resolve on.
func (r IDRef) Empty() bool { return r.TMDB <= 0 && r.TVDB <= 0 && r.IMDB == "" }

// ResolveIDRefs resolves a batch of external-id sets to canonical media refs
// using a bounded number of queries, avoiding the per-item N+1 a loop of single
// resolutions would incur.
//
// It reads BOTH places an id can live: the movies/shows columns (tvdb_id,
// tmdb_id, imdb_id) and the external_ids lookup table. Reading the columns is
// what makes this work for the titles the TMDB backfill ingested — those rows
// carry their ids on the row and have NO external_ids entry, which is exactly
// the case a watchlist add used to fail on.
//
// Ids are tried in the order tvdb, tmdb, imdb: the TVDB id is haaku's primary
// identity, so when a client sends several and they disagree, the TVDB one
// decides. Unresolvable refs are simply absent from the returned map; callers
// treat an absent key as "not found".
func ResolveIDRefs(db *gorm.DB, refs []IDRef) (map[IDRef]MediaRef, error) {
	result := make(map[IDRef]MediaRef, len(refs))
	if db == nil || len(refs) == 0 {
		return result, nil
	}

	tmdbKeys, tvdbKeys, imdbKeys := collectIDKeys(refs)
	if len(tmdbKeys) == 0 && len(tvdbKeys) == 0 && len(imdbKeys) == 0 {
		return result, nil
	}

	index := newIDIndex(db)
	if err := index.load(tmdbKeys, tvdbKeys, imdbKeys); err != nil {
		return nil, err
	}

	for _, ref := range refs {
		if ref.Empty() {
			continue
		}
		if resolved, ok := index.lookup(ref); ok {
			result[ref] = resolved
		}
	}
	return result, nil
}

// LookupMediaByTVDBIDs returns the local media ids already held for a set of
// tvdb ids of ONE media type, keyed by tvdb id. It is the membership check a
// live provider search runs so a result can say "already in your library"
// without a query per hit.
func LookupMediaByTVDBIDs(db *gorm.DB, mediaType string, tvdbIDs []int) (map[int]int64, error) {
	found := make(map[int]int64, len(tvdbIDs))
	if db == nil || len(tvdbIDs) == 0 {
		return found, nil
	}

	ids := make([]int, 0, len(tvdbIDs))
	keys := make([]string, 0, len(tvdbIDs))
	for _, id := range tvdbIDs {
		if id > 0 {
			ids = append(ids, id)
			keys = append(keys, strconv.Itoa(id))
		}
	}
	if len(ids) == 0 {
		return found, nil
	}

	var table any
	switch mediaType {
	case "movie":
		table = &Movie{}
	case "show":
		table = &Show{}
	default:
		return found, nil
	}

	var rows []struct {
		ID     int64
		TvdbID int
	}
	if err := db.Model(table).Select("id", "tvdb_id").Where("tvdb_id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lookup %s by tvdb id: %w", mediaType, err)
	}
	for _, row := range rows {
		found[row.TvdbID] = row.ID
	}

	// Titles ingested before the tvdb_id column existed are only findable
	// through their external_ids row.
	var exts []ExternalID
	if err := db.Model(&ExternalID{}).
		Where("media_type = ? AND source = ? AND external_id IN ?", mediaType, "tvdb", keys).
		Find(&exts).Error; err != nil {
		return nil, fmt.Errorf("lookup %s external ids by tvdb id: %w", mediaType, err)
	}
	for _, ext := range exts {
		id, err := strconv.Atoi(ext.ExternalID)
		if err != nil {
			continue
		}
		if _, seen := found[id]; !seen {
			found[id] = ext.MediaID
		}
	}
	return found, nil
}

// collectIDKeys deduplicates the ids in a ref batch, per source, dropping the
// "unknown" values so no query asks for them.
func collectIDKeys(refs []IDRef) (tmdb, tvdb []int, imdb []string) {
	seenTMDB := map[int]bool{}
	seenTVDB := map[int]bool{}
	seenIMDB := map[string]bool{}
	for _, ref := range refs {
		if ref.TMDB > 0 && !seenTMDB[ref.TMDB] {
			seenTMDB[ref.TMDB] = true
			tmdb = append(tmdb, ref.TMDB)
		}
		if ref.TVDB > 0 && !seenTVDB[ref.TVDB] {
			seenTVDB[ref.TVDB] = true
			tvdb = append(tvdb, ref.TVDB)
		}
		if ref.IMDB != "" && !seenIMDB[ref.IMDB] {
			seenIMDB[ref.IMDB] = true
			imdb = append(imdb, ref.IMDB)
		}
	}
	return tmdb, tvdb, imdb
}

// idIndex maps each kind of external id to the media it names.
type idIndex struct {
	db   *gorm.DB
	tmdb map[int]MediaRef
	tvdb map[int]MediaRef
	imdb map[string]MediaRef
}

func newIDIndex(db *gorm.DB) *idIndex {
	return &idIndex{
		db:   db,
		tmdb: map[int]MediaRef{},
		tvdb: map[int]MediaRef{},
		imdb: map[string]MediaRef{},
	}
}

// load fills the index from the media columns and from external_ids: at most
// one query per media type, one for the lookup table, and one per type to
// verify, so a batch of N ids costs a bounded number of queries.
func (i *idIndex) load(tmdbKeys, tvdbKeys []int, imdbKeys []string) error {
	var movies []Movie
	if err := i.db.Model(&Movie{}).
		Select("id", "tmdb_id", "tvdb_id", "imdb_id").
		Where("tmdb_id IN ? OR tvdb_id IN ? OR imdb_id IN ?", tmdbKeys, tvdbKeys, imdbKeys).
		Find(&movies).Error; err != nil {
		return fmt.Errorf("resolve movies by id: %w", err)
	}
	for _, m := range movies {
		i.add(m.TmdbID, m.TvdbID, m.ImdbID, MediaRef{MediaType: "movie", MediaID: m.ID})
	}

	var shows []Show
	if err := i.db.Model(&Show{}).
		Select("id", "tmdb_id", "tvdb_id", "imdb_id").
		Where("tmdb_id IN ? OR tvdb_id IN ? OR imdb_id IN ?", tmdbKeys, tvdbKeys, imdbKeys).
		Find(&shows).Error; err != nil {
		return fmt.Errorf("resolve shows by id: %w", err)
	}
	for _, s := range shows {
		i.add(s.TmdbID, s.TvdbID, s.ImdbID, MediaRef{MediaType: "show", MediaID: s.ID})
	}

	allKeys := make([]string, 0, len(tmdbKeys)+len(tvdbKeys)+len(imdbKeys))
	for _, id := range tmdbKeys {
		allKeys = append(allKeys, strconv.Itoa(id))
	}
	for _, id := range tvdbKeys {
		allKeys = append(allKeys, strconv.Itoa(id))
	}
	allKeys = append(allKeys, imdbKeys...)

	var exts []ExternalID
	if err := i.db.Model(&ExternalID{}).
		Where("source IN ? AND external_id IN ?", []string{"tmdb", "tvdb", "imdb"}, allKeys).
		Find(&exts).Error; err != nil {
		return fmt.Errorf("resolve external ids: %w", err)
	}
	for _, e := range exts {
		media := MediaRef{MediaType: e.MediaType, MediaID: e.MediaID}
		switch e.Source {
		case "tvdb":
			if id, err := strconv.Atoi(e.ExternalID); err == nil {
				i.setTVDB(id, media)
			}
		case "tmdb":
			if id, err := strconv.Atoi(e.ExternalID); err == nil {
				i.setTMDB(id, media)
			}
		case "imdb":
			i.setIMDB(e.ExternalID, media)
		}
	}

	return i.verify()
}

// add records every id a media row carries.
func (i *idIndex) add(tmdbID, tvdbID int, imdbID string, media MediaRef) {
	if tmdbID > 0 {
		i.setTMDB(tmdbID, media)
	}
	if tvdbID > 0 {
		i.setTVDB(tvdbID, media)
	}
	if imdbID != "" {
		i.setIMDB(imdbID, media)
	}
}

// setTMDB/setTVDB/setIMDB record a mapping, keeping the FIRST writer: a
// duplicate id is either the same media reached twice (harmless) or dirty data,
// and the media-column pass runs before the external_ids pass, so the row's own
// id wins.
func (i *idIndex) setTMDB(id int, media MediaRef) {
	if _, exists := i.tmdb[id]; !exists {
		i.tmdb[id] = media
	}
}

func (i *idIndex) setTVDB(id int, media MediaRef) {
	if _, exists := i.tvdb[id]; !exists {
		i.tvdb[id] = media
	}
}

func (i *idIndex) setIMDB(id string, media MediaRef) {
	if _, exists := i.imdb[id]; !exists {
		i.imdb[id] = media
	}
}

// lookup resolves one ref, preferring the TVDB id, then TMDB, then IMDb.
func (i *idIndex) lookup(ref IDRef) (MediaRef, bool) {
	if ref.TVDB > 0 {
		if media, ok := i.tvdb[ref.TVDB]; ok {
			return media, true
		}
	}
	if ref.TMDB > 0 {
		if media, ok := i.tmdb[ref.TMDB]; ok {
			return media, true
		}
	}
	if ref.IMDB != "" {
		if media, ok := i.imdb[ref.IMDB]; ok {
			return media, true
		}
	}
	return MediaRef{}, false
}

// verify drops any mapping whose media row no longer exists. external_ids has
// no foreign key, so a deleted title can leave a lookup row pointing at
// nothing.
func (i *idIndex) verify() error {
	existing, err := i.existingMedia()
	if err != nil {
		return err
	}
	keep := func(media MediaRef) bool {
		return existing[media.MediaType][media.MediaID]
	}
	for key, media := range i.tmdb {
		if !keep(media) {
			delete(i.tmdb, key)
		}
	}
	for key, media := range i.tvdb {
		if !keep(media) {
			delete(i.tvdb, key)
		}
	}
	for key, media := range i.imdb {
		if !keep(media) {
			delete(i.imdb, key)
		}
	}
	return nil
}

// existingMedia collects the media ids that are still present, grouped by type.
func (i *idIndex) existingMedia() (map[string]map[int64]bool, error) {
	present := map[string]map[int64]bool{}
	check := func(table any, mediaType string) error {
		var ids []int64
		if err := i.db.Model(table).Distinct("id").Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("verify %s rows: %w", mediaType, err)
		}
		set := make(map[int64]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
		present[mediaType] = set
		return nil
	}
	if err := check(&Movie{}, "movie"); err != nil {
		return nil, err
	}
	if err := check(&Show{}, "show"); err != nil {
		return nil, err
	}
	// Seasons are addressed through the same lookup table (a season id can
	// resolve through a sync payload), so they are verified too.
	if err := check(&Season{}, "season"); err != nil {
		return nil, err
	}
	return present, nil
}
