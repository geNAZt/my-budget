package domain

import "time"

type Bill struct {
	ID         string
	UserID     string
	Name       string
	IsDeleted  bool
	PoolID     *string
	AccountIDs []string
	CreatedAt  time.Time

	// The active/latest version
	ActiveVersion *BillVersion

	// API Only: Scenarios to link during creation
	LinkToScenarios []string
}

type BillVersion struct {
	ID             string
	BillID         string
	Amount         float64
	StartDate      time.Time
	EndDate        *time.Time
	IntervalMonths int
	CreatedAt      time.Time
}
