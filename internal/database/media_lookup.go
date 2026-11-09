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

	// MediaType is the type the caller expects this id to name ("movie",
	// "show", "season"), or "" when the caller does not know. It matters
	// because the SAME numeric id is routinely reused across tables: TVDB's
	// 1396 is both a movie and a show, so a bare-int index answered a show
	// lookup with the movie row and the request was reported as not found.
	// With MediaType set, an id that only exists as another type is a miss,
	// which is the truthful answer.
	MediaType string
}

// WithType returns a copy of the ref carrying the media type the caller
// expects. Resolution uses it to disambiguate an id that names more than one
// row; the returned map is keyed by the ref, so the caller must look the
// result up with the same typed ref it built.
func (r IDRef) WithType(mediaType string) IDRef {
	r.MediaType = mediaType
	return r
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
// Ids are tried in the order tmdb, tvdb, imdb: the TMDB id is haaku's primary
// identity, so when a client sends several and they disagree, the TMDB one
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

// LookupMediaByTMDBIDs returns the local media ids already held for a set of
// tmdb ids of ONE media type, keyed by tmdb id. It is the membership check a
// live provider search runs so a result can say "already in your library"
// without a query per hit.
func LookupMediaByTMDBIDs(db *gorm.DB, mediaType string, tmdbIDs []int) (map[int]int64, error) {
	return lookupMediaByColumn(db, mediaType, true, tmdbIDs)
}

// LookupMediaByTVDBIDs returns the local media ids already held for a set of
// tvdb ids of ONE media type, keyed by tvdb id.
func LookupMediaByTVDBIDs(db *gorm.DB, mediaType string, tvdbIDs []int) (map[int]int64, error) {
	return lookupMediaByColumn(db, mediaType, false, tvdbIDs)
}

// lookupMediaByColumn is the shared body of the two membership checks: the ids
// on the media rows are the fast path, and the external_ids table is the
// fallback for rows ingested before that column existed.
func lookupMediaByColumn(db *gorm.DB, mediaType string, byTmdb bool, externalIDs []int) (map[int]int64, error) {
	found := make(map[int]int64, len(externalIDs))
	if db == nil || len(externalIDs) == 0 {
		return found, nil
	}

	column, source := "tvdb_id", "tvdb"
	if byTmdb {
		column, source = "tmdb_id", "tmdb"
	}

	ids := make([]int, 0, len(externalIDs))
	keys := make([]string, 0, len(externalIDs))
	for _, id := range externalIDs {
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
		TmdbID int
		TvdbID int
	}
	if err := db.Model(table).Select("id", "tmdb_id", "tvdb_id").
		Where(column+" IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lookup %s by %s: %w", mediaType, column, err)
	}
	for _, row := range rows {
		key := row.TvdbID
		if byTmdb {
			key = row.TmdbID
		}
		found[key] = row.ID
	}

	// Titles ingested before the id column existed are only findable through
	// their external_ids row.
	var exts []ExternalID
	if err := db.Model(&ExternalID{}).
		Where("media_type = ? AND source = ? AND external_id IN ?", mediaType, source, keys).
		Find(&exts).Error; err != nil {
		return nil, fmt.Errorf("lookup %s external ids by %s: %w", mediaType, source, err)
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
	tmdb map[int][]MediaRef
	tvdb map[int][]MediaRef
	imdb map[string][]MediaRef
}

func newIDIndex(db *gorm.DB) *idIndex {
	return &idIndex{
		db:   db,
		tmdb: map[int][]MediaRef{},
		tvdb: map[int][]MediaRef{},
		imdb: map[string][]MediaRef{},
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

// setTMDB/setTVDB/setIMDB record a mapping. An id can name more than one row
// (a movie and a show sharing a TMDB id), so every distinct row is kept and the
// type-aware lookup below picks the one the caller asked for. Recording only
// the first writer is what made a show request resolve to a movie row.
func (i *idIndex) setTMDB(id int, media MediaRef) {
	i.tmdb[id] = appendRef(i.tmdb[id], media)
}

func (i *idIndex) setTVDB(id int, media MediaRef) {
	i.tvdb[id] = appendRef(i.tvdb[id], media)
}

func (i *idIndex) setIMDB(id string, media MediaRef) {
	i.imdb[id] = appendRef(i.imdb[id], media)
}

// appendRef adds media to refs unless the same row is already recorded.
func appendRef(refs []MediaRef, media MediaRef) []MediaRef {
	for _, existing := range refs {
		if existing == media {
			return refs
		}
	}
	return append(refs, media)
}

// lookup resolves one ref, preferring the TMDB id, then TVDB, then IMDb.
func (i *idIndex) lookup(ref IDRef) (MediaRef, bool) {
	if ref.TMDB > 0 {
		if media, ok := pickRef(i.tmdb[ref.TMDB], ref.MediaType); ok {
			return media, true
		}
	}
	if ref.TVDB > 0 {
		if media, ok := pickRef(i.tvdb[ref.TVDB], ref.MediaType); ok {
			return media, true
		}
	}
	if ref.IMDB != "" {
		if media, ok := pickRef(i.imdb[ref.IMDB], ref.MediaType); ok {
			return media, true
		}
	}
	return MediaRef{}, false
}

// pickRef chooses the candidate the caller asked for. When the ref names a
// type and no candidate has it, the id belongs to some other kind of media and
// is reported as a miss rather than silently resolving to the wrong table.
func pickRef(candidates []MediaRef, wantType string) (MediaRef, bool) {
	if len(candidates) == 0 {
		return MediaRef{}, false
	}
	if wantType == "" {
		return candidates[0], true
	}
	for _, candidate := range candidates {
		if candidate.MediaType == wantType {
			return candidate, true
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
	i.tmdb = pruneRefs(i.tmdb, keep)
	i.tvdb = pruneRefs(i.tvdb, keep)
	i.imdb = pruneRefs(i.imdb, keep)
	return nil
}

// pruneRefs drops candidates whose row is gone and forgets ids with none left.
func pruneRefs[K comparable](m map[K][]MediaRef, keep func(MediaRef) bool) map[K][]MediaRef {
	for key, refs := range m {
		kept := make([]MediaRef, 0, len(refs))
		for _, ref := range refs {
			if keep(ref) {
				kept = append(kept, ref)
			}
		}
		if len(kept) == 0 {
			delete(m, key)
			continue
		}
		m[key] = kept
	}
	return m
}

// existingMedia reports which candidate rows are still present. It looks up
// only the ids this batch resolved, not every row in the table: the earlier
// version Plucked every id from movies, shows and seasons on every resolve
// call, so the cost of adding one title grew with the size of the catalogue.
func (i *idIndex) existingMedia() (map[string]map[int64]bool, error) {
	present := map[string]map[int64]bool{}
	candidates := map[string]map[int64]bool{}
	collect := func(refs []MediaRef) {
		for _, ref := range refs {
			if candidates[ref.MediaType] == nil {
				candidates[ref.MediaType] = map[int64]bool{}
			}
			candidates[ref.MediaType][ref.MediaID] = true
		}
	}
	for _, refs := range i.tmdb {
		collect(refs)
	}
	for _, refs := range i.tvdb {
		collect(refs)
	}
	for _, refs := range i.imdb {
		collect(refs)
	}

	for mediaType, ids := range candidates {
		table := tableForMediaType(mediaType)
		if table == nil {
			// An unknown type has no table to check, so nothing is dropped on
			// its account; the resolver simply cannot return it.
			continue
		}
		list := make([]int64, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		var found []int64
		if err := i.db.Model(table).Where("id IN ?", list).Pluck("id", &found).Error; err != nil {
			return nil, fmt.Errorf("verify %s rows: %w", mediaType, err)
		}
		set := make(map[int64]bool, len(found))
		for _, id := range found {
			set[id] = true
		}
		present[mediaType] = set
	}
	return present, nil
}

// tableForMediaType maps a media type to the model whose rows it identifies.
func tableForMediaType(mediaType string) any {
	switch mediaType {
	case "movie":
		return &Movie{}
	case "show":
		return &Show{}
	case "season":
		return &Season{}
	default:
		return nil
	}
}
