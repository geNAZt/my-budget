package repository

import (
	"database/sql"
	"time"
)

type CacheRepository struct {
	db *sql.DB
}

func NewCacheRepository(db *sql.DB) *CacheRepository {
	return &CacheRepository{db: db}
}

func (r *CacheRepository) Get(key string) (string, bool, error) {
	var data string
	var updatedAt time.Time
	err := r.db.QueryRow("SELECT data, updated_at FROM external_cache WHERE key = ?", key).Scan(&data, &updatedAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	// 12-hour TTL for the cache
	if time.Since(updatedAt) > 12*time.Hour {
		return "", false, nil
	}

	return data, true, nil
}

func (r *CacheRepository) Set(key string, data string) error {
	_, err := r.db.Exec(`
		INSERT INTO external_cache (key, data, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET data = excluded.data, updated_at = CURRENT_TIMESTAMP`,
		key, data)
	return err
}
