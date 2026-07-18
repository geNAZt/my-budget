package domain

import "time"

type Income struct {
	ID         string
	UserID     string
	Name       string
	IsDeleted  bool
	PoolID     *string
	AccountIDs []string
	CreatedAt  time.Time

	// The active/latest version
	ActiveVersion *IncomeVersion

	// API Only: Scenarios to link during creation
	LinkToScenarios []string
}

type IncomeVersion struct {
	ID                         string
	IncomeID                   string
	Amount                     float64
	StopModificationID         *string
	StartDate                  time.Time
	EndDate                    *time.Time
	IntervalMonths             int
	CreatedAt                  time.Time
	Slices                     []TimeSlice
	IntervalIncreasePercentage float64
	IntervalIncreaseMonths     int
	IntervalIncreaseStartDate  *time.Time
}
