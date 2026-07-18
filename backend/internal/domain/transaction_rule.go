package domain

import "time"

type TransactionPool struct {
	ID        string
	UserID    string
	ParentID  *string
	Name      string
	Color     string
	IsHidden  bool
	CreatedAt time.Time
}

type TransactionRule struct {
	ID             string
	UserID         string
	ParentID       *string
	IntegrationID  *string
	TargetPoolID   *string
	Operator       string // 'AND', 'OR', 'NONE'
	Field          string // 'RECEIVER', 'DESCRIPTION', 'TAGS', 'ACCOUNT_TAGS', 'AMOUNT', 'DATA_CHAIN'
	Regex          string
	AmountOperator string // '>', '<', '=', '>=', '<='
	AmountValue    *float64
	Priority       int
	Negate         bool
	CreatedAt      time.Time

	// Virtual field for API responses
	Children []TransactionRule
}
