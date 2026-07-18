package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type TagRepository struct {
	db *sql.DB
}

func NewTagRepository(db *sql.DB) *TagRepository {
	return &TagRepository{db: db}
}

func (r *TagRepository) List(userID string) ([]string, error) {
	rows, err := r.db.Query("SELECT name FROM available_tags WHERE user_id = ? ORDER BY name ASC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tags = append(tags, name)
	}
	return tags, nil
}

func (r *TagRepository) Create(userID string, name string) error {
	id := uuid.New().String()
	_, err := r.db.Exec("INSERT INTO available_tags (id, user_id, name, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (user_id, name) DO NOTHING", id, userID, name, time.Now())
	return err
}
