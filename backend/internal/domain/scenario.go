package domain

import "time"

type Scenario struct {
	ID               string
	UserID           string
	Name             string
	Description      string
	ProjectionMonths int
	RemainderOrder   []string // Array of EntityIDs
	IsActive         bool
	MonthStartDay    int
	StartDate        *time.Time
	CreatedAt        time.Time

	// Monte Carlo Configuration
	Simulations              int
	SimYears                 int
	SimPercent               float64
	LookbackYears            int
	MonteCarloImplementation string // 'STANDARD', 'PARALLEL', 'SIMD'

	// Passive Income
	PassiveIncomePercentage float64

	Entities []ScenarioEntity

	// Per ETF Asset Monte Carlo overrides
	ETFParams map[string]ETFScenarioParams
}

type ETFScenarioParams struct {
	Simulations   int
	SimYears      int
	SimPercent    float64
	LookbackYears int
}

type ScenarioEntity struct {
	EntityID   string
	EntityType string // 'INCOME', 'BILL', 'EXPENSE', 'ASSET', 'LOAN'
	VersionID  string // NULL means latest
}

type ProjectionResult struct {
	Months          []ProjectionMonth
	TotalRemainder  float64
	SimulatedYields map[string]float64 // Map of AssetID -> Simulated Annual Yield %
	PenaltyAnalysis []PenaltyEvent
	Metrics         *PerformanceMetrics
}

type PenaltyEvent struct {
	Type              string    `json:"type"` // 'BUY' or 'SELL'
	Reason            string    `json:"reason"`
	Date              time.Time `json:"date"`
	AssetName         string    `json:"asset_name"`
	LotID             string    `json:"lot_id"`
	LotCreatedAt      time.Time `json:"lot_created_at"`
	Amount            float64   `json:"amount"`
	PrincipalSold     float64   `json:"principal_sold"`     // Only for SELL
	PenaltyPaid       float64   `json:"penalty_paid"`       // Only for SELL
	MonthsHeld        int       `json:"months_held"`        // Only for SELL
	InterestGenerated float64   `json:"interest_generated"` // Only for SELL
}

type PerformanceMetrics struct {
	TotalDurationMS      int64
	ResolutionDurationMS int64
	MonteCarloDurationMS int64
	CatchupDurationMS    int64
	ProjectionDurationMS int64
	PerAssetMCMS         map[string]int64
}

type VirtualAccountMonthBalance struct {
	AccountID         string
	Name              string
	Color             string
	StartingBalance   float64
	Inflow            float64
	Outflow           float64
	Balance           float64
	AssetWorth        float64
	LoanDebt          float64
	RealtimeAccountID string
}

type ProjectionMonth struct {
	Date            time.Time
	PeriodStart     time.Time
	PeriodEnd       time.Time
	Income          float64
	PassiveIncome   float64
	Bills           float64
	Expenses        float64
	Assets          float64
	Loans           float64 // Added field
	Remainder       float64
	Balance         float64 // Cumulative remainder
	AssetWorth      float64
	LoanDebt        float64
	VirtualAccounts []VirtualAccountMonthBalance

	// Node Breakdown for Budget Sheet
	Breakdown MonthBreakdown
}

type MonthBreakdown struct {
	Incomes  []EntryBreakdown
	Bills    []EntryBreakdown
	Expenses []EntryBreakdown
	Assets   []EntryBreakdown
	Loans    []EntryBreakdown
}

type EntryBreakdown struct {
	Name            string
	EntityName      string // For grouping in CSV export
	Amount          float64
	RealtimeBalance *float64
	PoolID          *string
	AccountIDs      []string
	Interest        float64 // Added for CSV export
	Penalty         float64 // Applied withdrawal penalty
	Balance         float64 // For Assets/Loans
	RealSplit       map[string]float64
	TrackerFlows    map[string]float64
	SubAssetFlows   map[string]float64
	PreviousBookingDate string
	BookingDate         string
}
