package domain

import "time"

type Connection struct {
	ID        string
	UserID    string
	Name      string
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
	KeySlots  map[string]string
}
