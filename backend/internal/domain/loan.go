package domain

import "time"

type Loan struct {
	ID         string
	UserID     string
	Name       string
	IsDeleted  bool
	PoolID     *string
	AccountIDs []string
	CreatedAt  time.Time

	// The active/latest version
	ActiveVersion *LoanVersion

	// API Only: Scenarios to link during creation
	LinkToScenarios []string
}

type LoanVersion struct {
	ID                 string
	LoanID             string
	AmountLent         float64
	InterestRate       float64
	RuntimeMonths      int
	StartDate          time.Time
	RemainderStartDate *time.Time
	Priority           float64
	NextLoanID         *string
	BalloonLeftover    float64
	IsInterestOnly     bool
	EarlyPayoffPenalty float64
	CreatedAt          time.Time
}
