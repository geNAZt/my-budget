package domain

import "time"

// BaseAccount handles core balance and transaction history.
type BaseAccount struct {
	ID          string
	Name        string
	StartDate   time.Time
	CurrentDate time.Time
	Balance     float64
	History     []History
	IsClosed    bool
}

// History records the state of an account at a specific point in time.
type History struct {
	Key             int
	Date            time.Time
	Balance         float64
	Contribution    float64
	Principal       float64
	AppliedInterest float64
}

// AccountActions defines the interface for account behavior.
type AccountActions interface {
	AdvanceTo(targetDate time.Time, rateOverride *float64) History
	Step(rate *float64, key int)
	ApplyExtraContribution(amount float64)
	GetState() History
}

// NewBaseAccount creates a new BaseAccount instance.
func NewBaseAccount(id, name string, startDate time.Time) *BaseAccount {
	// Normalize to the first of the month
	normalizedStart := time.Date(startDate.Year(), startDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	return &BaseAccount{
		ID:          id,
		Name:        name,
		StartDate:   normalizedStart,
		CurrentDate: normalizedStart,
		Balance:     0,
		History:     []History{},
		IsClosed:    false,
	}
}

// GetKey calculates the unique month key for a given date.
func GetKey(t time.Time) int {
	return t.Year()*12 + int(t.Month())
}
