package domain

import "time"

type Modification struct {
	ID          string
	UserID      string
	TargetID    string // ID of Asset or Loan
	TargetIDs   []string
	TargetType  string // 'ASSET', 'LOAN'
	Description string
	IsDeleted   bool
	CreatedAt   time.Time

	// The active/latest version
	ActiveVersion *ModificationVersion
}

type ModificationVersion struct {
	ID                   string
	ModificationID       string
	Amount               float64 // Acts as threshold if WithdrawalPercentage > 0
	WithdrawalPercentage float64
	StartDate            time.Time
	EndDate              *time.Time
	IntervalMonths       int // 0 = one-time
	CreatedAt            time.Time
}
