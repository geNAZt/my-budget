package domain

import "time"

type Expense struct {
	ID         string
	UserID     string
	Name       string
	IsDeleted  bool
	PoolID     *string
	AccountIDs []string
	CreatedAt  time.Time

	// The active/latest version
	ActiveVersion *ExpenseVersion

	// API Only: Scenarios to link during creation
	LinkToScenarios []string
}

type ExpenseVersion struct {
	ID        string
	ExpenseID string
	Amount    float64
	DueDate   time.Time
	CreatedAt time.Time
}
