package database

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MediaMetadata is one cached provider sub-resource for a media row (issue
// #68): videos, releases, translations, studios or season info. The
// (media_type, media_id, kind) tuple is unique, so the first request for an
// item stores the upstream payload and later requests serve it without a
// second provider call.
//
// payload is contract-shaped JSON, written by the handler that owns that kind,
// so reading a row back is a decode-and-emit rather than a re-mapping.
type MediaMetadata struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:media_metadata_scope_key,priority:1" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:media_metadata_scope_key,priority:2" json:"media_id"`
	Kind      string    `gorm:"type:text;not null;uniqueIndex:media_metadata_scope_key,priority:3" json:"kind"`
	Payload   JSONMap   `gorm:"type:jsonb;default:'{}'" json:"payload"`
	FetchedAt time.Time `json:"fetched_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (MediaMetadata) TableName() string { return "media_metadata" }

// GetMediaMetadata returns the stored payload for one (media_type, media_id,
// kind) tuple, or (nil, false, nil) when nothing is cached yet.
func GetMediaMetadata(db *gorm.DB, mediaType string, mediaID int64, kind string) (JSONMap, bool, error) {
	var row MediaMetadata
	err := db.Where("media_type = ? AND media_id = ? AND kind = ?", mediaType, mediaID, kind).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return row.Payload, true, nil
}

// PutMediaMetadata upserts a payload for one (media_type, media_id, kind)
// tuple. It is an upsert so a refresh overwrites the cached copy instead of
// tripping the unique index.
func PutMediaMetadata(db *gorm.DB, mediaType string, mediaID int64, kind string, payload JSONMap) error {
	if payload == nil {
		payload = JSONMap{}
	}
	now := time.Now().UTC()
	row := MediaMetadata{
		MediaType: mediaType,
		MediaID:   mediaID,
		Kind:      kind,
		Payload:   payload,
		FetchedAt: now,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "media_type"}, {Name: "media_id"}, {Name: "kind"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "fetched_at", "updated_at"}),
	}).Create(&row).Error
}
