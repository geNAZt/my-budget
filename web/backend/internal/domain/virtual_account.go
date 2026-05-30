package domain

import "time"

type VirtualAccount struct {
	ID            string
	UserID        string
	Name          string
	IsDeleted     bool
	CreatedAt     time.Time
	ActiveVersion *VirtualAccountVersion
}

type VirtualAccountVersion struct {
	ID               string
	VirtualAccountID string
	Color            string
	StartingBalance  float64
	Description      string
	CreatedAt        time.Time
}
