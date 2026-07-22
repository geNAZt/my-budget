package domain

import "time"

type AssetType string

const (
	AssetTypeStatic AssetType = "STATIC"
	AssetTypeETF    AssetType = "ETF"
)

type Asset struct {
	ID              string
	UserID          string
	Name            string
	IsDeleted       bool
	PoolID          *string
	AccountIDs      []string
	CreatedAt       time.Time
	ActiveVersion   *AssetVersion
	LinkToScenarios []string
}

type PenaltyTriggerType string

const (
	PenaltyTriggerWithdrawal PenaltyTriggerType = "WITHDRAWAL"
	PenaltyTriggerInterest   PenaltyTriggerType = "INTEREST"
)

type AssetPenalty struct {
	Name        string             `json:"name"`
	TriggerType PenaltyTriggerType `json:"trigger_type"`
	Percentage  float64            `json:"percentage"`
}

type SubAsset struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	TargetValue         string     `json:"target_value"`
	AmountPerMonth      float64    `json:"amount_per_month"`
	IsRemainderConsumer bool       `json:"is_remainder_consumer"`
	RemainderStartDate  *time.Time `json:"remainder_start_date"`
	DumpingLoanID       *string    `json:"dumping_loan_id"`
	StartDate           time.Time  `json:"start_date"`
	EndDate             *time.Time `json:"end_date"`
	EarliestDumpDate    *time.Time `json:"earliest_dump_date"`
	ExpenseID           *string    `json:"expense_id"`
	RemainderPriority   int32      `json:"remainder_priority"`
}

type AssetTaxAllowance struct {
	ID        string     `json:"id"`
	Amount    float64    `json:"amount"`
	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
}

type AssetVersion struct {
	ID                 string
	AssetID            string
	Type               AssetType
	TargetValue        string
	DumpingLoanID      *string
	StopModificationID *string
	InterestRate       float64
	InterestInterval   string
	AmountPerMonth     float64
	RemainderStartDate *time.Time
	StartDate          time.Time
	EndDate            *time.Time
	ETFConfig          []ETFTracker
	Penalties          []AssetPenalty
	SubAssets          []SubAsset
	CreatedAt          time.Time
	UseForPassiveIncome bool
	TaxAllowance       float64
	TaxAllowanceStartDate *time.Time
	TaxAllowanceEndDate   *time.Time
	TaxAllowances      []AssetTaxAllowance
}

type HistoryStitchingSegment struct {
	Provider          string `json:"provider"`
	LookupTicker      string `json:"lookup_ticker"`
	ConversionTracker string `json:"conversion_tracker"`
}

type ETFTracker struct {
	Tracker           string                    `json:"tracker"`
	HistoricalTracker string                    `json:"historical_tracker"`
	ConversionTracker string                    `json:"conversion_tracker"`
	HistoryProvider   string                    `json:"history_provider"`
	Percentage        float64                   `json:"percentage"`
	TER               float64                   `json:"ter"`
	StitchingSegments []HistoryStitchingSegment `json:"stitching_segments"`
}
