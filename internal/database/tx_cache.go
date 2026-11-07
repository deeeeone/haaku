package database

import (
	"github.com/haaku-tv/haaku/internal/cache"

	"gorm.io/gorm"
)

func init() {
	// Wire the cache package's WithCacheTransaction to our WithTransaction.
	// This breaks the circular dependency: database → cache → database.
	cache.RegisterDBTx(func(fn func(tx *gorm.DB) (*cache.CacheTransaction, error), cacheTx **cache.CacheTransaction) error {
		return WithTransaction(func(tx *gorm.DB) error {
			var err error
			*cacheTx, err = fn(tx)
			return err
		})
	})
}
