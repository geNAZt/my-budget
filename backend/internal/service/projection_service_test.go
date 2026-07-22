package service

import (
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/db"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
)

func TestProjectionMonthBoundaryDay(t *testing.T) {
	label := projectionMonthForDate(time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC), 26, nil)
	if !label.Equal(time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May projection month to be labelled 2026-05-26, got %s", label.Format(time.DateOnly))
	}

	start, end := projectionPeriodBounds(label, 26, nil)
	if !start.Equal(time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May period to start 2026-04-26, got %s", start.Format(time.DateOnly))
	}
	if !end.Equal(time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May period to end 2026-05-26, got %s", end.Format(time.DateOnly))
	}

	aprilPaycheck := projectionMonthForDate(time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC), 26, nil)
	if !aprilPaycheck.Equal(label) {
		t.Fatalf("expected transaction on 2026-04-26 to bucket into May, got %s", aprilPaycheck.Format(time.DateOnly))
	}
}

func TestGetWithdrawalPenaltyRate(t *testing.T) {
	v := &domain.AssetVersion{
		Penalties: []domain.AssetPenalty{
			{Name: "P1", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 5.0},
			{Name: "P2", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 10.0},
			{Name: "P3", TriggerType: domain.PenaltyTriggerInterest, Percentage: 2.0},
		},
	}
	rate := getWithdrawalPenaltyRate(v)
	if rate != 0.15 {
		t.Errorf("Expected 0.15, got %f", rate)
	}
}

func TestGetInterestPenaltyRate(t *testing.T) {
	v := &domain.AssetVersion{
		Penalties: []domain.AssetPenalty{
			{Name: "P1", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 5.0},
			{Name: "P3", TriggerType: domain.PenaltyTriggerInterest, Percentage: 2.0},
		},
	}
	rate := getInterestPenaltyRate(v)
	if rate != 0.02 {
		t.Errorf("Expected 0.02, got %f", rate)
	}
}

func TestSubAssetsHelpers(t *testing.T) {
	// Setup sub-assets
	sa1 := &subAssetState{id: "sa1", name: "Target A", currentBalance: 100.0, targetValue: "200.0", amountPerMonth: 50.0, isRemainderConsumer: false}
	sa2 := &subAssetState{id: "sa2", name: "Target B", currentBalance: 50.0, targetValue: "300.0", amountPerMonth: 100.0, isRemainderConsumer: false}
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: &domain.AssetVersion{
				Type: "STATIC",
			},
		},
		currentBalance: 150.0,
		subAssets:      []*subAssetState{sa1, sa2},
	}

	// 1. Proportional deposit (e.g. interest)
	// Balances: sa1=100 (2/3), sa2=50 (1/3)
	// Deposit: 30.0 -> sa1 should get 20.0, sa2 should get 10.0
	depositAssetProportionally(as, 30.0, nil)
	if sa1.currentBalance != 120.0 {
		t.Errorf("Expected sa1 balance 120.0, got %f", sa1.currentBalance)
	}
	if sa2.currentBalance != 60.0 {
		t.Errorf("Expected sa2 balance 60.0, got %f", sa2.currentBalance)
	}
	if as.currentBalance != 180.0 {
		t.Errorf("Expected total balance 180.0, got %f", as.currentBalance)
	}

	// 2. Sequential deposit (e.g. remainder waterfall or positive modifications)
	// sa1 target is 200. sa1 current is 120. room is 80.
	// sa2 target is 300. sa2 current is 60. room is 240.
	// Deposit: 100.0 -> sa1 should get 80.0 (reached target 200), sa2 should get 20.0 (new balance 80.0)
	depositAsset(as, 100.0, nil)
	if sa1.currentBalance != 200.0 {
		t.Errorf("Expected sa1 balance 200.0, got %f", sa1.currentBalance)
	}
	if sa2.currentBalance != 80.0 {
		t.Errorf("Expected sa2 balance 80.0, got %f", sa2.currentBalance)
	}
	if as.currentBalance != 280.0 {
		t.Errorf("Expected total balance 280.0, got %f", as.currentBalance)
	}

	// 3. Proportional withdrawal
	// Balances: sa1=200 (5/7), sa2=80 (2/7)
	// Withdraw net: 70.0 (no penalty)
	gross, net := withdrawAsset(as, 70.0)
	if gross != 70.0 || net != 70.0 {
		t.Errorf("Expected 70.0/70.0, got %f/%f", gross, net)
	}
	// sa1 should lose 50.0 (200 - 50 = 150)
	// sa2 should lose 20.0 (80 - 20 = 60)
	if sa1.currentBalance != 150.0 {
		t.Errorf("Expected sa1 150.0, got %f", sa1.currentBalance)
	}
	if sa2.currentBalance != 60.0 {
		t.Errorf("Expected sa2 60.0, got %f", sa2.currentBalance)
	}
}

func TestWithdrawFromSubAsset(t *testing.T) {
	parentVersion := &domain.AssetVersion{
		Type: "STATIC",
		Penalties: []domain.AssetPenalty{
			{Name: "W1", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 10.0},
		},
	}

	sa1 := &subAssetState{
		id:                  "sa1",
		name:                "Target A",
		currentBalance:      500.0,
		isRemainderConsumer: false,
	}

	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 500.0,
		subAssets:      []*subAssetState{sa1},
	}

	// Test partial withdrawal of 180 net.
	// penalty = 10%. grossSold = 180 / 0.9 = 200.
	gross, net := withdrawFromSubAsset(as, "sa1", 180.0)
	if gross != 200.0 || net != 180.0 {
		t.Errorf("Expected gross 200.0 and net 180.0, got gross %f, net %f", gross, net)
	}
	if sa1.currentBalance != 300.0 {
		t.Errorf("Expected sub-asset remaining balance 300.0, got %f", sa1.currentBalance)
	}
	if as.currentBalance != 300.0 {
		t.Errorf("Expected parent asset remaining balance 300.0, got %f", as.currentBalance)
	}

	// Test withdrawal larger than maximum net possible (maximum net = 300 * 0.9 = 270)
	// requesting 300 net should empty it out and return net 270.
	gross, net = withdrawFromSubAsset(as, "sa1", 300.0)
	if gross != 300.0 || net != 270.0 {
		t.Errorf("Expected gross 300.0 and net 270.0, got gross %f, net %f", gross, net)
	}
	if sa1.currentBalance != 0.0 {
		t.Errorf("Expected sub-asset remaining balance 0.0, got %f", sa1.currentBalance)
	}
	if as.currentBalance != 0.0 {
		t.Errorf("Expected parent asset remaining balance 0.0, got %f", as.currentBalance)
	}
}

func TestETFSubAssetFIFOAndGrowth(t *testing.T) {
	// 1. Test Deposit to ETF sub-assets appends parent lots
	parentVersion := &domain.AssetVersion{
		Type: "ETF",
		Penalties: []domain.AssetPenalty{
			{Name: "Tax", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 25.0},
		},
	}
	sa1 := &subAssetState{
		id:                  "sa1",
		name:                "Target A",
		currentBalance:      0.0,
		isRemainderConsumer: false,
	}
	sa2 := &subAssetState{
		id:                  "sa2",
		name:                "Target B",
		currentBalance:      0.0,
		isRemainderConsumer: false,
	}
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 0.0,
		subAssets:      []*subAssetState{sa1, sa2},
		lots:           []etfLot{},
	}

	depositToSubAsset(as, "sa1", 100.0)
	depositToSubAsset(as, "sa2", 300.0)

	if len(as.lots) != 2 {
		t.Fatalf("Expected 2 ETF lots, got %d", len(as.lots))
	}
	if as.lots[0].principal != 100.0 || as.lots[1].principal != 300.0 {
		t.Errorf("Expected lot principals 100.0 and 300.0, got %f and %f", as.lots[0].principal, as.lots[1].principal)
	}
	if sa1.currentBalance != 100.0 || sa2.currentBalance != 300.0 {
		t.Errorf("Expected sub-asset balances 100.0 and 300.0, got %f and %f", sa1.currentBalance, sa2.currentBalance)
	}
	if as.currentBalance != 400.0 {
		t.Errorf("Expected parent balance 400.0, got %f", as.currentBalance)
	}

	// 2. Test ETF sub-asset FIFO withdrawal capped at sub-asset balance
	// Let's pretend the first lot doubled in value (100 -> 200).
	// First lot: principal = 100, currentValue = 200. Profit margin = (200 - 100) / 200 = 50%.
	// Second lot: principal = 300, currentValue = 300. Profit margin = 0%.
	as.lots[0].currentValue = 200.0
	as.currentBalance = 500.0

	// Target A (sa1) currentBalance = 100.
	// Let's withdraw 100 net from sa1.
	// Available from lot 0: currentValue = 200, but capped at sa1.currentBalance = 100.
	// Max gross from lot 0 is min(200, 100) = 100.
	// Profit margin of lot 0 is 50%. Penalty rate is 25%.
	// Net output from lot 0 if we sell the max gross of 100 is:
	// 100 * (1.0 - 0.50 * 0.25) = 100 * (1.0 - 0.125) = 87.5 net.
	// Since 87.5 net < requested 100 net, we will withdraw the full 100 gross.
	// This will exhaust sa1's balance (currentBalance becomes 0).
	gross, net := withdrawFromSubAsset(as, "sa1", 100.0)
	if gross != 100.0 {
		t.Errorf("Expected gross sold 100.0, got %f", gross)
	}
	if net != 87.5 {
		t.Errorf("Expected net fulfilled 87.5, got %f", net)
	}
	if sa1.currentBalance != 0.0 {
		t.Errorf("Expected Target A remaining balance to be 0, got %f", sa1.currentBalance)
	}
	// The first lot should be reduced by 100 gross (currentValue becomes 100, principal becomes 50).
	if as.lots[0].currentValue != 100.0 || as.lots[0].principal != 50.0 {
		t.Errorf("Expected remaining lot 0 currentValue 100.0 and principal 50.0, got currentValue %f, principal %f", as.lots[0].currentValue, as.lots[0].principal)
	}
	if as.currentBalance != 400.0 {
		t.Errorf("Expected parent remaining balance 400.0, got %f", as.currentBalance)
	}
}

func TestETFSubAssetPayout(t *testing.T) {
	// 1. Create an ETF asset version with a 25.0% early withdrawal penalty
	parentVersion := &domain.AssetVersion{
		Type: "ETF",
		Penalties: []domain.AssetPenalty{
			{Name: "Tax", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 25.0},
		},
	}
	sa1 := &subAssetState{
		id:                  "sa1",
		name:                "Umzug München",
		currentBalance:      0.0,
		isRemainderConsumer: false,
	}
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 0.0,
		subAssets:      []*subAssetState{sa1},
		lots:           []etfLot{},
	}

	// 2. Perform contribution (deposit)
	depositToSubAsset(as, "sa1", 1000.0)

	// Verify lot was created with principal 1000 and value 1000
	if len(as.lots) != 1 {
		t.Fatalf("Expected 1 lot, got %d", len(as.lots))
	}
	if as.lots[0].principal != 1000.0 || as.lots[0].currentValue != 1000.0 {
		t.Errorf("Expected lot principal and currentValue to be 1000.0, got principal %f, value %f", as.lots[0].principal, as.lots[0].currentValue)
	}

	// 3. Simulate growth (e.g. lot doubles in value)
	as.lots[0].currentValue = 2000.0
	as.currentBalance = 2000.0
	sa1.currentBalance = 2000.0

	// 4. Calculate Max Net for the sub-asset.
	// Since profit is 1000, tax/penalty paid is 250 (25% of 1000).
	// Max Net obtainable = 2000 - 250 = 1750.
	maxNet := calculateMaxNetForSubAsset(as, sa1)
	if maxNet != 1750.0 {
		t.Errorf("Expected maxNet to be 1750.0, got %f", maxNet)
	}

	// 5. Withdraw the max net from the sub-asset
	grossSold, netFulfilled := withdrawFromSubAsset(as, "sa1", maxNet)
	if grossSold != 2000.0 {
		t.Errorf("Expected gross sold to be 2000.0, got %f", grossSold)
	}
	if netFulfilled != 1750.0 {
		t.Errorf("Expected net fulfilled 1750.0, got %f", netFulfilled)
	}
	if sa1.currentBalance != 0.0 {
		t.Errorf("Expected sub-asset balance to be completely emptied (0.0), got %f", sa1.currentBalance)
	}
	if as.currentBalance != 0.0 {
		t.Errorf("Expected parent asset balance to be reduced to 0.0, got %f", as.currentBalance)
	}
	if len(as.lots) != 0 {
		t.Errorf("Expected all lots to be consumed, got %d remaining lots", len(as.lots))
	}

	// 6. Test with multiple lots inside the sub-asset
	sa2 := &subAssetState{
		id:                  "sa2",
		name:                "Kaution München",
		currentBalance:      0.0,
		isRemainderConsumer: false,
	}
	as2 := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 0.0,
		subAssets:      []*subAssetState{sa2},
		lots:           []etfLot{},
	}

	// Deposit twice to create two lots
	depositToSubAsset(as2, "sa2", 400.0)
	depositToSubAsset(as2, "sa2", 600.0)

	// Lot 1: principal = 400, currentValue = 400.
	// Lot 2: principal = 600, currentValue = 600.
	// Now let's double Lot 1 currentValue to 800 (profit = 400, profit margin = 50%).
	// Keep Lot 2 currentValue at 600 (profit = 0, profit margin = 0%).
	as2.lots[0].currentValue = 800.0
	as2.currentBalance = 1400.0
	sa2.currentBalance = 1400.0

	// Net from Lot 1 (800 gross): 800 * (1 - 0.5 * 0.25) = 800 * 0.875 = 700 net.
	// Net from Lot 2 (600 gross): 600 * (1 - 0) = 600 net.
	// Total max net: 700 + 600 = 1300 net.
	maxNet2 := calculateMaxNetForSubAsset(as2, sa2)
	if maxNet2 != 1300.0 {
		t.Errorf("Expected maxNet2 to be 1300.0, got %f", maxNet2)
	}

	// Withdraw the max net
	grossSold2, netFulfilled2 := withdrawFromSubAsset(as2, "sa2", maxNet2)
	if grossSold2 != 1400.0 {
		t.Errorf("Expected gross sold 1400.0, got %f", grossSold2)
	}
	if netFulfilled2 != 1300.0 {
		t.Errorf("Expected net fulfilled 1300.0, got %f", netFulfilled2)
	}
	if sa2.currentBalance != 0.0 {
		t.Errorf("Expected sub-asset balance sa2 to be completely emptied (0.0), got %f", sa2.currentBalance)
	}
	if as2.currentBalance != 0.0 {
		t.Errorf("Expected parent asset balance to be 0.0, got %f", as2.currentBalance)
	}
}

func TestETFInterestAccumulationAndStartingBalance(t *testing.T) {
	// Verify that if we have a simulated yield of 10% (0.10)
	// and an initial balance of 1000.0,
	// the monthly rate is computed correctly and applied to the starting lot.
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: &domain.AssetVersion{
				Type: "ETF",
			},
		},
		currentBalance: 1000.0,
		simulatedYield: 0.10,
		lots: []etfLot{
			{principal: 1000.0, currentValue: 1000.0},
		},
	}

	monthlyRate := math.Pow(1.0+as.simulatedYield, 1.0/12.0) - 1.0
	if monthlyRate <= 0 {
		t.Fatalf("Expected monthlyRate to be positive, got %f", monthlyRate)
	}

	// Simulate 1 month of compound growth
	var newBal float64
	for i := range as.lots {
		grossGrowth := as.lots[i].currentValue * monthlyRate
		netGrowth := grossGrowth * 1.0 // no penalties
		as.lots[i].currentValue += netGrowth
		newBal += as.lots[i].currentValue
	}
	as.currentBalance = newBal

	expectedBalance := 1000.0 * math.Pow(1.10, 1.0/12.0)
	if math.Abs(as.currentBalance-expectedBalance) > 0.00001 {
		t.Errorf("Expected currentBalance to be %f after 1 month, got %f", expectedBalance, as.currentBalance)
	}
}

func TestDistributingETF(t *testing.T) {
	// Setup asset state for a distributing ETF
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: &domain.AssetVersion{
				Type:             "ETF",
				InterestInterval: "Monthly", // Monthly = Distributing
			},
			AccountIDs: []string{"va-1"},
		},
		currentBalance: 1000.0,
		simulatedYield: 0.12, // 12% yearly = 1% monthly (approx)
		lots: []etfLot{
			{principal: 1000.0, currentValue: 1000.0},
		},
		currentMonth: time.Now(),
	}

	month := &domain.ProjectionMonth{
		Breakdown: domain.MonthBreakdown{
			Incomes: []domain.EntryBreakdown{},
			Assets:  []domain.EntryBreakdown{},
		},
	}
	availableFunds := 0.0

	v := as.asset.ActiveVersion
	interestPenaltyRate := 0.0
	monthlyRate := math.Pow(1.0+as.simulatedYield, 1.0/12.0) - 1.0
	var newBal float64
	var totalGrossGrowth float64
	var interestEarned float64
	var interestPenaltyPaid float64

	isDistributing := v.InterestInterval == "Monthly"

	if monthlyRate > 0 {
		for i := range as.lots {
			grossGrowth := as.lots[i].currentValue * monthlyRate
			totalGrossGrowth += grossGrowth

			if !isDistributing {
				netGrowth := grossGrowth * (1.0 - interestPenaltyRate)
				as.lots[i].currentValue += netGrowth
			}
			newBal += as.lots[i].currentValue
		}
		interestEarned = totalGrossGrowth
		interestPenaltyPaid = totalGrossGrowth * interestPenaltyRate
	}

	as.currentBalance = newBal

	if isDistributing && interestEarned > 0 {
		payout := interestEarned - interestPenaltyPaid
		month.Income += payout
		availableFunds += payout
		month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
			Name:       as.asset.Name + " (Dividend)",
			Amount:     payout,
			AccountIDs: as.asset.AccountIDs,
		})

		// Reset interestEarned/PenaltyPaid for final breakdown check in the loop
		interestEarned = 0
		interestPenaltyPaid = 0
	}

	// Verify that asset balance did NOT grow
	if as.currentBalance != 1000.0 {
		t.Errorf("Expected asset balance to remain 1000.0 for distributing ETF, got %f", as.currentBalance)
	}

	// Verify that income was generated
	expectedPayout := 1000.0 * (math.Pow(1.12, 1.0/12.0) - 1.0)
	if math.Abs(month.Income-expectedPayout) > 0.00001 {
		t.Errorf("Expected income payout of %f, got %f", expectedPayout, month.Income)
	}

	if len(month.Breakdown.Incomes) != 1 {
		t.Errorf("Expected 1 income entry in breakdown, got %d", len(month.Breakdown.Incomes))
	} else if month.Breakdown.Incomes[0].AccountIDs[0] != "va-1" {
		t.Errorf("Expected income entry to be linked to va-1, got %v", month.Breakdown.Incomes[0].AccountIDs)
	}
}

func TestSubAssetPayoutVirtualAccountAttribution(t *testing.T) {
	// Setup asset state with a sub-asset that has reached its end date
	as := &assetState{
		asset: domain.Asset{
			Name:       "Test Asset",
			AccountIDs: []string{"va-1"},
			ActiveVersion: &domain.AssetVersion{
				Type: "ETF",
			},
		},
		currentBalance: 1000.0,
		subAssets: []*subAssetState{
			{
				id:             "sa-1",
				name:           "Test SubAsset",
				currentBalance: 1000.0,
			},
		},
		lots: []etfLot{
			{principal: 1000.0, currentValue: 1000.0},
		},
		currentMonth: time.Now(),
	}

	month := &domain.ProjectionMonth{
		Breakdown: domain.MonthBreakdown{
			Incomes: []domain.EntryBreakdown{},
			Assets:  []domain.EntryBreakdown{},
		},
	}

	sa := as.subAssets[0]
	// Simulate the payout logic from lines ~1950
	maxNetLeftover := calculateMaxNetForSubAsset(as, sa)
	grossPayout, netPayout := withdrawFromSubAsset(as, sa.id, maxNetLeftover)
	penaltyPaid := grossPayout - netPayout

	month.Income += netPayout
	month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
		Name:       as.asset.Name + " (" + sa.name + " Payout)",
		Amount:     netPayout,
		Penalty:    penaltyPaid,
		AccountIDs: as.asset.AccountIDs,
	})

	// Verify that income was generated with correct account IDs
	if len(month.Breakdown.Incomes) != 1 {
		t.Fatalf("Expected 1 income entry, got %d", len(month.Breakdown.Incomes))
	}

	entry := month.Breakdown.Incomes[0]
	if entry.Amount != 1000.0 {
		t.Errorf("Expected payout amount 1000.0, got %f", entry.Amount)
	}

	if len(entry.AccountIDs) != 1 || entry.AccountIDs[0] != "va-1" {
		t.Errorf("Expected AccountIDs [va-1], got %v", entry.AccountIDs)
	}

	// Verify that NO asset breakdown entry was created (to avoid double counting)
	if len(month.Breakdown.Assets) != 0 {
		t.Errorf("Expected 0 asset entries in breakdown for payout, got %d", len(month.Breakdown.Assets))
	}
}

func TestSWRModificationTrigger(t *testing.T) {
	// We want to test:
	// 1. Triggering SWR modification when totalBalance >= threshold (Amount)
	// 2. Ensuring the trigger persists in subsequent months (even if balance decreases below threshold)
	// 3. Verifying that the income linked via StopModificationID is stopped.

	mID := "mod-1"
	m := domain.Modification{
		ID:         mID,
		TargetType: "ASSET",
		TargetID:   "asset-1",
		ActiveVersion: &domain.ModificationVersion{
			StartDate:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			IntervalMonths:       1,
			WithdrawalPercentage: 4.0,     // 4% SWR
			Amount:               12000.0, // threshold of 12,000 annual SWR withdrawal (4% SWR of 300,000 portfolio balance)
		},
	}

	as := &assetState{
		asset: domain.Asset{
			ID:   "asset-1",
			Name: "ETF Asset",
			ActiveVersion: &domain.AssetVersion{
				Type: "ETF",
			},
		},
		currentBalance: 300000.0, // Exactly hits threshold!
		lots: []etfLot{
			{principal: 300000.0, currentValue: 300000.0},
		},
	}

	triggeredMods := make(map[string]bool)

	// Month 1: Start of Month Pre-evaluation
	totalBalance := as.currentBalance
	annualWithdrawal := totalBalance * (m.ActiveVersion.WithdrawalPercentage / 100.0)
	if annualWithdrawal >= m.ActiveVersion.Amount {
		triggeredMods[m.ID] = true
	}

	if !triggeredMods[mID] {
		t.Fatalf("Expected SWR modification to trigger with balance 300,000 (annual SWR withdrawal = 12,000)")
	}

	inc := domain.Income{
		Name: "Job Income",
		ActiveVersion: &domain.IncomeVersion{
			Amount:             2000.0,
			StopModificationID: &mID,
		},
	}

	if inc.ActiveVersion.StopModificationID != nil && triggeredMods[*inc.ActiveVersion.StopModificationID] {
		// correctly skipped
	} else {
		t.Errorf("Expected income to be skipped because the stop modification was triggered")
	}

	swrAmt := totalBalance * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)
	if triggeredMods[m.ID] || totalBalance >= m.ActiveVersion.Amount {
		toWithdrawTotal := swrAmt
		if math.Abs(toWithdrawTotal-1000.0) > 0.001 {
			t.Errorf("Expected to withdraw 1000.0, got %f", toWithdrawTotal)
		}
	} else {
		t.Errorf("Expected withdrawal block to execute")
	}

	// Month 2: Balance falls to 250,000.
	as.currentBalance = 250000.0
	totalBalance2 := as.currentBalance
	swrAmt2 := totalBalance2 * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)

	if !triggeredMods[mID] {
		t.Fatalf("Expected SWR modification trigger to persist in Month 2")
	}

	annualWithdrawal2 := totalBalance2 * (m.ActiveVersion.WithdrawalPercentage / 100.0)
	if triggeredMods[m.ID] || annualWithdrawal2 >= m.ActiveVersion.Amount {
		toWithdrawTotal := swrAmt2
		expectedWithdrawal := 250000.0 * 0.04 / 12.0 // 833.3333
		if math.Abs(toWithdrawTotal-expectedWithdrawal) > 0.001 {
			t.Errorf("Expected to withdraw %f in Month 2, got %f", expectedWithdrawal, toWithdrawTotal)
		}
	} else {
		t.Errorf("Expected withdrawal block to execute in Month 2 even with lower balance")
	}
}

func TestSWRModificationIntervals(t *testing.T) {
	s := &ProjectionService{}
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// SWR uses interval = 1 under the hood now. Let's make sure s.isActiveAt with interval = 1 returns true for subsequent months.
	if !s.isActiveAt(startDate, nil, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 1) {
		t.Errorf("Expected isActiveAt to be true in month 2 for interval 1")
	}

	// For a one-time interval (0), isActiveAt should be false in subsequent months (which is why SWR needs to override it to 1)
	if s.isActiveAt(startDate, nil, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 0) {
		t.Errorf("Expected isActiveAt to be false in month 2 for interval 0")
	}

	// For an annual interval (12), isActiveAt should be false in month 2
	if s.isActiveAt(startDate, nil, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 12) {
		t.Errorf("Expected isActiveAt to be false in month 2 for interval 12")
	}
}

func TestResolveModifications(t *testing.T) {
	dbURL := "postgres://budget:budgetpass@localhost:5432/budget?sslmode=disable"
	database, err := db.InitDB(dbURL)
	if err != nil {
		t.Skip("Skipping resolve modifications test because database is not available:", err)
	}
	defer database.Close()

	userID := "test-user-resolve-mods"
	mr := repository.NewModificationRepository(database)

	// Ensure test user exists to satisfy foreign keys
	_, err = database.Exec("INSERT INTO users (id, username) VALUES (?, ?) ON CONFLICT (id) DO NOTHING", userID, userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	defer func() {
		_, _ = database.Exec("DELETE FROM modification_assets WHERE modification_id IN (SELECT id FROM modifications WHERE user_id = ?)", userID)
		_, _ = database.Exec("DELETE FROM modification_versions WHERE modification_id IN (SELECT id FROM modifications WHERE user_id = ?)", userID)
		_, _ = database.Exec("DELETE FROM modifications WHERE user_id = ?", userID)
		_, _ = database.Exec("DELETE FROM assets WHERE user_id = ?", userID)
		_, _ = database.Exec("DELETE FROM loans WHERE user_id = ?", userID)
		_, _ = database.Exec("DELETE FROM users WHERE id = ?", userID)
	}()

	// Insert test assets and loans to satisfy foreign keys
	_, _ = database.Exec("INSERT INTO assets (id, user_id, name) VALUES ('asset-1', ?, 'Asset 1') ON CONFLICT (id) DO NOTHING", userID)
	_, _ = database.Exec("INSERT INTO assets (id, user_id, name) VALUES ('asset-2', ?, 'Asset 2') ON CONFLICT (id) DO NOTHING", userID)
	_, _ = database.Exec("INSERT INTO assets (id, user_id, name) VALUES ('asset-3', ?, 'Asset 3') ON CONFLICT (id) DO NOTHING", userID)
	_, _ = database.Exec("INSERT INTO loans (id, user_id, name) VALUES ('loan-1', ?, 'Loan 1') ON CONFLICT (id) DO NOTHING", userID)

	// Clean up any left-overs from previous runs
	_, _ = database.Exec("DELETE FROM modification_assets WHERE modification_id IN (SELECT id FROM modifications WHERE user_id = ?)", userID)
	_, _ = database.Exec("DELETE FROM modification_versions WHERE modification_id IN (SELECT id FROM modifications WHERE user_id = ?)", userID)
	_, _ = database.Exec("DELETE FROM modifications WHERE user_id = ?", userID)

	// Create test modifications
	m1 := &domain.Modification{
		TargetType:  "ASSET",
		TargetID:    "asset-1",
		Description: "Single Asset Mod",
		ActiveVersion: &domain.ModificationVersion{
			Amount:    100.0,
			StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	err = mr.Save(userID, m1)
	if err != nil {
		t.Fatalf("Failed to save m1: %v", err)
	}
	defer func() {
		_, _ = database.Exec("DELETE FROM modification_versions WHERE modification_id = ?", m1.ID)
		_, _ = database.Exec("DELETE FROM modifications WHERE id = ?", m1.ID)
	}()

	m2 := &domain.Modification{
		TargetType:  "ASSET",
		Description: "Multi Asset Mod",
		TargetIDs:   []string{"asset-2", "asset-3"},
		ActiveVersion: &domain.ModificationVersion{
			Amount:    200.0,
			StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	err = mr.Save(userID, m2)
	if err != nil {
		t.Fatalf("Failed to save m2: %v", err)
	}
	defer func() {
		_, _ = database.Exec("DELETE FROM modification_assets WHERE modification_id = ?", m2.ID)
		_, _ = database.Exec("DELETE FROM modification_versions WHERE modification_id = ?", m2.ID)
		_, _ = database.Exec("DELETE FROM modifications WHERE id = ?", m2.ID)
	}()

	m3 := &domain.Modification{
		TargetType:  "LOAN",
		TargetID:    "loan-1",
		Description: "Loan Mod",
		ActiveVersion: &domain.ModificationVersion{
			Amount:    50.0,
			StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	err = mr.Save(userID, m3)
	if err != nil {
		t.Fatalf("Failed to save m3: %v", err)
	}
	defer func() {
		_, _ = database.Exec("DELETE FROM modification_versions WHERE modification_id = ?", m3.ID)
		_, _ = database.Exec("DELETE FROM modifications WHERE id = ?", m3.ID)
	}()

	ps := &ProjectionService{}
	ps.SetAdditionalRepos(nil, mr)

	// Case 1: Unscoped scenario (len(Entities) == 0) -> should return all modifications
	sc1 := &domain.Scenario{}
	resolved, err := ps.resolveModifications(userID, sc1)
	if err != nil {
		t.Fatalf("resolveModifications failed: %v", err)
	}
	if len(resolved) < 3 {
		t.Errorf("Expected at least 3 resolved modifications, got %d", len(resolved))
	}

	// Case 2: Scoped scenario with asset-1 -> should include m1, exclude m2 and m3
	sc2 := &domain.Scenario{
		Entities: []domain.ScenarioEntity{
			{EntityType: "ASSET", EntityID: "asset-1"},
		},
	}
	resolved2, err := ps.resolveModifications(userID, sc2)
	if err != nil {
		t.Fatalf("resolveModifications failed: %v", err)
	}
	hasM1, hasM2, hasM3 := false, false, false
	for _, m := range resolved2 {
		if m.ID == m1.ID {
			hasM1 = true
		}
		if m.ID == m2.ID {
			hasM2 = true
		}
		if m.ID == m3.ID {
			hasM3 = true
		}
	}
	if !hasM1 || hasM2 || hasM3 {
		t.Errorf("Case 2 expected only m1. Got hasM1=%t, hasM2=%t, hasM3=%t", hasM1, hasM2, hasM3)
	}

	// Case 3: Scoped scenario with asset-2 -> should include m2 (via TargetIDs), exclude m1 and m3
	sc3 := &domain.Scenario{
		Entities: []domain.ScenarioEntity{
			{EntityType: "ASSET", EntityID: "asset-2"},
		},
	}
	resolved3, err := ps.resolveModifications(userID, sc3)
	if err != nil {
		t.Fatalf("resolveModifications failed: %v", err)
	}
	hasM1, hasM2, hasM3 = false, false, false
	for _, m := range resolved3 {
		if m.ID == m1.ID {
			hasM1 = true
		}
		if m.ID == m2.ID {
			hasM2 = true
		}
		if m.ID == m3.ID {
			hasM3 = true
		}
	}
	if hasM1 || !hasM2 || hasM3 {
		t.Errorf("Case 3 expected only m2. Got hasM1=%t, hasM2=%t, hasM3=%t", hasM1, hasM2, hasM3)
	}

	// Case 4: Scoped scenario with loan-1 -> should include m3, exclude m1 and m2
	sc4 := &domain.Scenario{
		Entities: []domain.ScenarioEntity{
			{EntityType: "LOAN", EntityID: "loan-1"},
		},
	}
	resolved4, err := ps.resolveModifications(userID, sc4)
	if err != nil {
		t.Fatalf("resolveModifications failed: %v", err)
	}
	hasM1, hasM2, hasM3 = false, false, false
	for _, m := range resolved4 {
		if m.ID == m1.ID {
			hasM1 = true
		}
		if m.ID == m2.ID {
			hasM2 = true
		}
		if m.ID == m3.ID {
			hasM3 = true
		}
	}
	if hasM1 || hasM2 || !hasM3 {
		t.Errorf("Case 4 expected only m3. Got hasM1=%t, hasM2=%t, hasM3=%t", hasM1, hasM2, hasM3)
	}
}

func TestSWRModificationMonthlyTrigger(t *testing.T) {
	// 1. Scenario: Monthly interval (IntervalMonths = 1), SWR = 3.5%, threshold Amount = 10,000 (meaning 10k/month).
	// Under 300,000 balance, monthly SWR withdrawal is 300,000 * 0.035 / 12 = 875. This is less than 10,000. It should NOT trigger.
	m := domain.Modification{
		ID:         "mod-monthly-swr",
		TargetType: "ASSET",
		TargetID:   "asset-1",
		ActiveVersion: &domain.ModificationVersion{
			StartDate:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			IntervalMonths:       1,
			WithdrawalPercentage: 3.5,
			Amount:               10000.0, // 10,000€/month threshold
		},
	}

	// Case A: Balance is 300,000.
	balanceA := 300000.0
	targetWithdrawalA := balanceA * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)
	if targetWithdrawalA >= m.ActiveVersion.Amount {
		t.Errorf("Expected monthly SWR not to trigger at 300k balance (withdrawal = %f, threshold = 10,000)", targetWithdrawalA)
	}

	// Case B: Balance is 3,500,000.
	// Monthly SWR withdrawal is 3,500,000 * 0.035 / 12 = 10,208.33. This is >= 10,000. It SHOULD trigger.
	balanceB := 3500000.0
	targetWithdrawalB := balanceB * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)
	if targetWithdrawalB < m.ActiveVersion.Amount {
		t.Errorf("Expected monthly SWR to trigger at 3.5M balance (withdrawal = %f, threshold = 10,000)", targetWithdrawalB)
	}
}

func TestFlexibleRemainderFundedExpense(t *testing.T) {
	// Setup a flexible expense
	expID := "exp-vacation"
	expense := domain.Expense{
		ID:        expID,
		Name:      "Vacation (Flexible)",
		IsDeleted: false,
		ActiveVersion: &domain.ExpenseVersion{
			ID:        "exp-ver-vacation",
			ExpenseID: expID,
			Amount:     500.0,
			DueDate:   time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	// Setup linked remainder consumer sub-asset
	sa1 := &subAssetState{
		id:                  "sa-vacation",
		name:                "Vacation Fund",
		currentBalance:      300.0,
		targetValue:         "500",
		amountPerMonth:      0,
		isRemainderConsumer: true,
		expenseID:           &expID,
		isClosed:            false,
	}

	as := &assetState{
		asset: domain.Asset{
			ID: "asset-savings",
			ActiveVersion: &domain.AssetVersion{
				ID:   "asset-ver-savings",
				Type: domain.AssetTypeStatic,
			},
		},
		currentBalance: 300.0,
		subAssets:      []*subAssetState{sa1},
	}

	// Test Case 1: Sub-asset balance (300) < expense amount (500).
	// Expense should NOT trigger.
	{
		month := &domain.ProjectionMonth{
			Breakdown: domain.MonthBreakdown{
				Expenses: []domain.EntryBreakdown{},
				Incomes:  []domain.EntryBreakdown{},
			},
		}

		expensesList := []domain.Expense{expense}
		assetStatesList := []*assetState{as}

		for _, e := range expensesList {
			v := e.ActiveVersion
			isFlexibleRemainderConsumer := false
			var matchingSubAsset *subAssetState

			if strings.HasSuffix(e.Name, " (Flexible)") || strings.Contains(e.Name, "[Flex]") {
				for _, asState := range assetStatesList {
					for _, sa := range asState.subAssets {
						if sa.expenseID != nil && *sa.expenseID == e.ID && sa.isRemainderConsumer {
							isFlexibleRemainderConsumer = true
							matchingSubAsset = sa
							break
						}
					}
					if isFlexibleRemainderConsumer {
						break
					}
				}
			}

			if isFlexibleRemainderConsumer {
				if matchingSubAsset != nil && !matchingSubAsset.isClosed && matchingSubAsset.currentBalance >= v.Amount {
					month.Expenses += v.Amount
					matchingSubAsset.isClosed = true
				}
			}
		}

		if month.Expenses > 0 {
			t.Errorf("Expected expense not to trigger when sub-asset balance (300) is less than expense amount (500)")
		}
		if sa1.isClosed {
			t.Errorf("Expected sub-asset not to close when expense is not triggered")
		}
	}

	// Test Case 2: Sub-asset balance (550) >= expense amount (500).
	// Expense SHOULD trigger, sub-asset should close.
	{
		sa1.currentBalance = 550.0
		as.currentBalance = 550.0
		sa1.isClosed = false

		month := &domain.ProjectionMonth{
			Breakdown: domain.MonthBreakdown{
				Expenses: []domain.EntryBreakdown{},
				Incomes:  []domain.EntryBreakdown{},
			},
		}

		expensesList := []domain.Expense{expense}
		assetStatesList := []*assetState{as}

		for _, e := range expensesList {
			v := e.ActiveVersion
			isFlexibleRemainderConsumer := false
			var matchingSubAsset *subAssetState

			if strings.HasSuffix(e.Name, " (Flexible)") || strings.Contains(e.Name, "[Flex]") {
				for _, asState := range assetStatesList {
					for _, sa := range asState.subAssets {
						if sa.expenseID != nil && *sa.expenseID == e.ID && sa.isRemainderConsumer {
							isFlexibleRemainderConsumer = true
							matchingSubAsset = sa
							break
						}
					}
					if isFlexibleRemainderConsumer {
						break
					}
				}
			}

			if isFlexibleRemainderConsumer {
				if matchingSubAsset != nil && !matchingSubAsset.isClosed && matchingSubAsset.currentBalance >= v.Amount {
					month.Expenses += v.Amount
					matchingSubAsset.isClosed = true
				}
			}
		}

		if month.Expenses != 500.0 {
			t.Errorf("Expected expense of 500.0 to trigger, got %f", month.Expenses)
		}
		if !sa1.isClosed {
			t.Errorf("Expected sub-asset to close when expense triggers")
		}
	}
}

func TestSubAssetRemainderPriorities(t *testing.T) {
	// Setup sub-assets with different priorities
	saA := &subAssetState{
		id:                  "sa-high-prio",
		name:                "High Priority Fund",
		currentBalance:      0.0,
		targetValue:         "100",
		amountPerMonth:      0,
		isRemainderConsumer: true,
		remainderPriority:   1, // Higher priority (lower number)
	}

	saB := &subAssetState{
		id:                  "sa-low-prio",
		name:                "Low Priority Fund",
		currentBalance:      0.0,
		targetValue:         "200",
		amountPerMonth:      0,
		isRemainderConsumer: true,
		remainderPriority:   2, // Lower priority
	}

	as := &assetState{
		asset: domain.Asset{
			ID: "asset-savings",
			ActiveVersion: &domain.AssetVersion{
				ID:          "asset-ver-savings",
				Type:        domain.AssetTypeStatic,
				TargetValue: "1000",
			},
		},
		currentBalance: 0.0,
		subAssets:      []*subAssetState{saA, saB},
	}

	// We simulate the remainder waterfall logic for this asset with leftover = 150
	leftover := 150.0

	// Mock environment
	loanByID := make(map[string]*loanState)

	// Replicate waterfall logic
	activeRemainderConsumers := []*subAssetState{}
	for _, sa := range as.subAssets {
		if !sa.isClosed && sa.isRemainderConsumer && sa.amountPerMonth == 0 {
			target := getSubAssetTarget(sa, loanByID)
			if target < 0 || sa.currentBalance < (target-0.01) {
				activeRemainderConsumers = append(activeRemainderConsumers, sa)
			}
		}
	}

	if len(activeRemainderConsumers) > 0 {
		priorityMap := make(map[int32][]*subAssetState)
		for _, sa := range activeRemainderConsumers {
			priorityMap[sa.remainderPriority] = append(priorityMap[sa.remainderPriority], sa)
		}
		var sortedPriorities []int32
		for prio := range priorityMap {
			sortedPriorities = append(sortedPriorities, prio)
		}
		sort.Slice(sortedPriorities, func(i, j int) bool {
			return sortedPriorities[i] < sortedPriorities[j]
		})

		remainingInAssetLoop := leftover

		for _, prio := range sortedPriorities {
			if remainingInAssetLoop <= 0.01 {
				break
			}

			remainderConsumers := priorityMap[prio]

			for remainingInAssetLoop > 0.01 && len(remainderConsumers) > 0 {
				evenShare := remainingInAssetLoop / float64(len(remainderConsumers))
				newRemainderConsumers := []*subAssetState{}
				thisRoundConsumed := 0.0

				for _, sa := range remainderConsumers {
					target := getSubAssetTarget(sa, loanByID)
					toDep := evenShare
					if target >= 0 {
						room := math.Max(0, target-sa.currentBalance)
						toDep = math.Min(evenShare, room)
					}

					if toDep > 0 {
						sa.currentBalance += toDep // depositToSubAsset mock
						thisRoundConsumed += toDep
						remainingInAssetLoop -= toDep

						if target < 0 || sa.currentBalance < (target-0.01) {
							newRemainderConsumers = append(newRemainderConsumers, sa)
						}
					}
				}

				remainderConsumers = newRemainderConsumers
				if thisRoundConsumed <= 0.0001 {
					break
				}
			}
		}
	}

	// Assertions
	if saA.currentBalance != 100.0 {
		t.Errorf("Expected saA (prio 1) to be fully funded with 100.0, got %f", saA.currentBalance)
	}
	if saB.currentBalance != 50.0 {
		t.Errorf("Expected saB (prio 2) to receive remaining 50.0, got %f", saB.currentBalance)
	}
}

func TestConfigurablePassiveIncome(t *testing.T) {
	// Setup two ETF assets: one with UseForPassiveIncome=true, one with UseForPassiveIncome=false
	etf1 := &assetState{
		asset: domain.Asset{
			ID: "etf-active",
			ActiveVersion: &domain.AssetVersion{
				ID:                  "active-version-etf-active",
				Type:                domain.AssetTypeETF,
				UseForPassiveIncome: true,
			},
		},
		currentBalance: 100000.0,
	}

	etf2 := &assetState{
		asset: domain.Asset{
			ID: "etf-inactive",
			ActiveVersion: &domain.AssetVersion{
				ID:                  "active-version-etf-inactive",
				Type:                domain.AssetTypeETF,
				UseForPassiveIncome: false,
			},
		},
		currentBalance: 500000.0,
	}

	assetStates := []*assetState{etf1, etf2}

	// Calculate etfWorth replicating the production code
	etfWorth := 0.0
	for _, as := range assetStates {
		if !as.isClosed {
			if as.asset.ActiveVersion.Type == domain.AssetTypeETF && as.asset.ActiveVersion.UseForPassiveIncome {
				etfWorth += as.currentBalance
			}
		}
	}

	// Assertions
	if etfWorth != 100000.0 {
		t.Errorf("Expected etfWorth to only include the active ETF asset (100000.0), but got %f", etfWorth)
	}
}

func TestTargetExpenseFunding(t *testing.T) {
	// Setup a non-flexible expense
	expID := "exp-car"
	expense := domain.Expense{
		ID:        expID,
		Name:      "Kia EV5",
		IsDeleted: false,
		ActiveVersion: &domain.ExpenseVersion{
			ID:        "exp-ver-car",
			ExpenseID: expID,
			Amount:     31384.90,
			DueDate:   time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	// Setup linked non-remainder consumer sub-asset
	endDate := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	sa1 := &subAssetState{
		id:                  "sa-car",
		name:                "Kia EV5 Savings",
		currentBalance:      34811.03,
		targetValue:         "31385",
		amountPerMonth:      653.85,
		isRemainderConsumer: false,
		expenseID:           &expID,
		endDate:             &endDate,
		isClosed:            false,
	}

	as := &assetState{
		asset: domain.Asset{
			ID: "asset-savings",
			ActiveVersion: &domain.AssetVersion{
				ID:   "asset-ver-savings",
				Type: domain.AssetTypeStatic,
			},
		},
		currentBalance: 34811.03,
		subAssets:      []*subAssetState{sa1},
	}

	// Simulation loop snippet representation for Jan 2031
	currentDate := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	loc := time.UTC
	scenarioMonthStartDay := 1

	month := &domain.ProjectionMonth{
		Date: currentDate,
		Breakdown: domain.MonthBreakdown{
			Expenses: []domain.EntryBreakdown{},
		},
	}

	// Replicate step 2 logic for this test case
	expensesList := []domain.Expense{expense}
	assetStatesList := []*assetState{as}

	for _, e := range expensesList {
		v := e.ActiveVersion
		isFlexibleRemainderConsumer := false
		if strings.HasSuffix(e.Name, " (Flexible)") || strings.Contains(e.Name, "[Flex]") {
			// Flexible path skipped
		}

		if isFlexibleRemainderConsumer {
			// Skip
		} else {
			linkedSubAssetEndReached := false
			for _, asState := range assetStatesList {
				for _, sa := range asState.subAssets {
					if sa.expenseID != nil && *sa.expenseID == e.ID && !sa.isRemainderConsumer && !sa.isClosed {
						if sa.endDate != nil {
							saEnd := sa.endDate.UTC()
							if currentDate.Year() == saEnd.Year() && currentDate.Month() == saEnd.Month() {
								linkedSubAssetEndReached = true
								break
							}
						}
					}
				}
				if linkedSubAssetEndReached {
					break
				}
			}

			uDue := v.DueDate.UTC()
			start, end := projectionPeriodBounds(currentDate, scenarioMonthStartDay, loc)
			isDue := (uDue.Equal(start) || uDue.After(start)) && uDue.Before(end)

			if isDue || linkedSubAssetEndReached {
				month.Expenses += v.Amount
				month.Breakdown.Expenses = append(month.Breakdown.Expenses, domain.EntryBreakdown{
					Name:       e.Name,
					EntityName: e.Name,
					Amount:     v.Amount,
					AccountIDs: e.AccountIDs,
					PoolID:     e.PoolID,
				})
			}
		}
	}

	// Assertions
	if month.Expenses != 31384.90 {
		t.Errorf("Expected month.Expenses to be 31384.90, got %f", month.Expenses)
	}
	if len(month.Breakdown.Expenses) != 1 {
		t.Errorf("Expected 1 expense breakdown entry, got %d", len(month.Breakdown.Expenses))
	} else if month.Breakdown.Expenses[0].Name != "Kia EV5" {
		t.Errorf("Expected breakdown expense name to be 'Kia EV5', got '%s'", month.Breakdown.Expenses[0].Name)
	}
}

func TestPayoutVirtualAccountAttribution(t *testing.T) {
	// Scenario 1: Linked Payout
	expID := "exp-linked"
	expense := domain.Expense{
		ID:         expID,
		Name:       "Car Purchase",
		IsDeleted:  false,
		AccountIDs: []string{"account-car-fund"},
		ActiveVersion: &domain.ExpenseVersion{
			ID:        "exp-ver-linked",
			ExpenseID: expID,
			Amount:    10000,
		},
	}
	endDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	saLinked := &subAssetState{
		id:                  "sa-linked",
		name:                "Car Fund Sub-Asset",
		currentBalance:      10000,
		targetValue:         "10000",
		amountPerMonth:      100,
		isRemainderConsumer: false,
		expenseID:           &expID,
		endDate:             &endDate,
		isClosed:            false,
	}
	asLinked := &assetState{
		asset: domain.Asset{
			ID:         "asset-parent-linked",
			AccountIDs: []string{"account-parent-asset-savings"},
			ActiveVersion: &domain.AssetVersion{
				ID:   "asset-ver-parent-linked",
				Type: domain.AssetTypeStatic,
			},
		},
		currentBalance: 10000,
		subAssets:      []*subAssetState{saLinked},
	}

	// Scenario 2: Non-Linked Payout
	saNonLinked := &subAssetState{
		id:                  "sa-nonlinked",
		name:                "General Fund Sub-Asset",
		currentBalance:      5000,
		targetValue:         "5000",
		amountPerMonth:      50,
		isRemainderConsumer: false,
		expenseID:           nil, // Non-linked
		endDate:             &endDate,
		isClosed:            false,
	}
	asNonLinked := &assetState{
		asset: domain.Asset{
			ID:         "asset-parent-nonlinked",
			AccountIDs: []string{"account-parent-asset-savings"},
			ActiveVersion: &domain.AssetVersion{
				ID:   "asset-ver-parent-nonlinked",
				Type: domain.AssetTypeStatic,
			},
		},
		currentBalance: 5000,
		subAssets:      []*subAssetState{saNonLinked},
	}

	// Simulation inputs
	expensesList := []domain.Expense{expense}
	_ = asLinked
	_ = asNonLinked

	// Verify Step 5 logic for linked payout
	var linkedPayoutAccountIDs []string
	if saLinked.expenseID != nil && *saLinked.expenseID != "" {
		for _, e := range expensesList {
			if e.ID == *saLinked.expenseID {
				linkedPayoutAccountIDs = e.AccountIDs
				break
			}
		}
	}
	if len(linkedPayoutAccountIDs) != 1 || linkedPayoutAccountIDs[0] != "account-car-fund" {
		t.Errorf("Expected linked payout account IDs to be ['account-car-fund'], got %v", linkedPayoutAccountIDs)
	}

	// Verify Step 5 logic for non-linked payout
	var nonLinkedPayoutAccountIDs []string
	if saNonLinked.expenseID != nil && *saNonLinked.expenseID != "" {
		for _, e := range expensesList {
			if e.ID == *saNonLinked.expenseID {
				nonLinkedPayoutAccountIDs = e.AccountIDs
				break
			}
		}
	}
	if len(nonLinkedPayoutAccountIDs) != 0 {
		t.Errorf("Expected non-linked payout account IDs to be empty, got %v", nonLinkedPayoutAccountIDs)
	}
}

func TestETFPartialSellTaxGuardrail(t *testing.T) {
	// Setup asset with early withdrawal penalty (25%)
	parentVersion := &domain.AssetVersion{
		Type: "ETF",
		Penalties: []domain.AssetPenalty{
			{Name: "Tax", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 25.0},
		},
	}
	sa1 := &subAssetState{
		id:                  "sa1",
		name:                "Target SA",
		currentBalance:      1000.0, // Capped sub-asset balance for partial sell
		isRemainderConsumer: false,
	}
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 5079.16,
		subAssets:      []*subAssetState{sa1},
		lots: []etfLot{
			{
				id:           "lot-1",
				createdAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				principal:    5079.16,
				currentValue: 5079.16,
			},
		},
		penaltyAnalysis: &[]domain.PenaltyEvent{},
		currentMonth:    time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	// 1. Partial sell where there is no gain/loss (cost basis == current value)
	// Selling 957.55 net. Sub-asset has 1000.0.
	// Since cost basis == current value, profitMargin = 0.
	// So grossNeeded = 957.55.
	gross, net := withdrawFromSubAsset(as, "sa1", 957.55)
	if gross != 957.55 || net != 957.55 {
		t.Errorf("Expected gross/net to be 957.55, got gross=%f, net=%f", gross, net)
	}

	events := *as.penaltyAnalysis
	if len(events) != 1 {
		t.Fatalf("Expected 1 penalty event, got %d", len(events))
	}

	event := events[0]
	expectedPrincipalSold := 5079.16 * (957.55 / 5079.16) // proportional principal
	if math.Abs(event.PrincipalSold-expectedPrincipalSold) > 0.01 {
		t.Errorf("Expected PrincipalSold to be proportional (~%f), got %f", expectedPrincipalSold, event.PrincipalSold)
	}
	if event.PenaltyPaid != 0.0 {
		t.Errorf("Expected PenaltyPaid to be 0.00 under zero gain, got %f", event.PenaltyPaid)
	}
	if event.InterestGenerated > 0.01 || event.InterestGenerated < -0.01 {
		t.Errorf("Expected InterestGenerated to be ~0, got %f", event.InterestGenerated)
	}

	// 2. Partial sell with negative gain/loss (value fell: currentValue = 3000, principal = 4000)
	as.lots = []etfLot{
		{
			id:           "lot-2",
			createdAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			principal:    4000.0,
			currentValue: 3000.0,
		},
	}
	as.currentBalance = 3000.0
	sa1.currentBalance = 500.0 // deplete this sub-asset using lot-2
	as.penaltyAnalysis = &[]domain.PenaltyEvent{}

	// Withdraw 200.0 net from sub-asset (depleting it partially using lot-2)
	// Since currentValue (3000) < principal (4000), profitMargin = 0.
	// So grossNeeded = 200.0.
	gross, net = withdrawFromSubAsset(as, "sa1", 200.0)
	if gross != 200.0 || net != 200.0 {
		t.Errorf("Expected gross/net to be 200.0, got gross=%f, net=%f", gross, net)
	}

	events = *as.penaltyAnalysis
	if len(events) != 1 {
		t.Fatalf("Expected 1 penalty event, got %d", len(events))
	}
	event = events[0]
	// Fraction sold: 200 / 3000
	expectedPrincipalSold = 4000.0 * (200.0 / 3000.0)
	if math.Abs(event.PrincipalSold-expectedPrincipalSold) > 0.01 {
		t.Errorf("Expected PrincipalSold to be proportional (~%f), got %f", expectedPrincipalSold, event.PrincipalSold)
	}
	if event.InterestGenerated >= 0 {
		t.Errorf("Expected negative InterestGenerated, got %f", event.InterestGenerated)
	}
	if event.PenaltyPaid != 0.0 {
		t.Errorf("Expected PenaltyPaid to be exactly 0.00 for negative gain, got %f", event.PenaltyPaid)
	}
}

func TestGenerateScenarioPDF(t *testing.T) {
	months := []domain.ProjectionMonth{
		{
			Date:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Income: 5000.0,
			Bills:  1200.0,
			Breakdown: domain.MonthBreakdown{
				Incomes: []domain.EntryBreakdown{
					{Name: "Job", Amount: 5000.0},
				},
				Bills: []domain.EntryBreakdown{
					{Name: "Rent", Amount: 1200.0},
				},
			},
		},
	}

	pdfBytes, err := GenerateScenarioPDF("Test Scenario", months)
	if err != nil {
		t.Fatalf("Expected no error generating PDF, got %v", err)
	}

	if len(pdfBytes) == 0 {
		t.Error("Expected generated PDF bytes to be non-empty")
	}
}

func TestETFTaxAllowanceAndTaxHarvesting(t *testing.T) {
	allowanceStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	allowanceEnd := time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC)

	parentVersion := &domain.AssetVersion{
		Type:                  "ETF",
		TaxAllowance:          1000.0,
		TaxAllowanceStartDate: &allowanceStart,
		TaxAllowanceEndDate:   &allowanceEnd,
		Penalties: []domain.AssetPenalty{
			{Name: "Tax", TriggerType: domain.PenaltyTriggerWithdrawal, Percentage: 25.0},
		},
	}
	sa1 := &subAssetState{
		id:                  "sa1",
		name:                "Target SA",
		currentBalance:      5000.0,
		isRemainderConsumer: false,
	}
	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentBalance: 5000.0,
		subAssets:      []*subAssetState{sa1},
		lots: []etfLot{
			{
				id:           "lot-1",
				createdAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				principal:    2000.0, // Significant gain: value = 5000, principal = 2000
				currentValue: 5000.0,
			},
		},
		penaltyAnalysis: &[]domain.PenaltyEvent{},
		currentMonth:    time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	// 1. Withdraw 800 net from sub-asset.
	// Profit margin is (5000-2000)/5000 = 60%.
	// With 800 net and candidate gain = 800 * 60% = 480.
	// Since 480 <= 1000 (TaxAllowance), the entire gain is covered by allowance, so gross = 800.
	gross, net := withdrawFromSubAsset(as, "sa1", 800.0)
	if math.Abs(gross-800.0) > 0.01 || math.Abs(net-800.0) > 0.01 {
		t.Errorf("Expected gross/net to be 800.0, got gross=%f, net=%f", gross, net)
	}

	events := *as.penaltyAnalysis
	if len(events) != 1 {
		t.Fatalf("Expected 1 penalty event, got %d", len(events))
	}
	event := events[0]
	if event.PenaltyPaid != 0.0 {
		t.Errorf("Expected PenaltyPaid to be 0.00 since it is within TaxAllowance, got %f", event.PenaltyPaid)
	}
	if math.Abs(as.getRemainingTaxAllowance()-520.0) > 0.01 {
		t.Errorf("Expected remaining TaxAllowance to be 520.0, got %f", as.getRemainingTaxAllowance())
	}

	// 2. Withdraw another 2000. Remaining TaxAllowance is 520.
	gross, net = withdrawFromSubAsset(as, "sa1", 2000.0)
	if math.Abs(gross-2200.0) > 0.01 || math.Abs(net-2000.0) > 0.01 {
		t.Errorf("Expected gross=2200, net=2000, got gross=%f, net=%f", gross, net)
	}
	if math.Abs(as.getRemainingTaxAllowance()) > 0.01 {
		t.Errorf("Expected remaining TaxAllowance to be 0, got %f", as.getRemainingTaxAllowance())
	}

	// 3. Reset TaxAllowance by moving to next year (2027) within active range
	as.currentMonth = time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC) // December 2027
	prevEventsCount := len(*as.penaltyAnalysis)
	as.PerformDecemberStepUp()

	if math.Abs(as.getRemainingTaxAllowance()) > 0.01 {
		t.Errorf("Expected remaining TaxAllowance to be 0 after December step-up, got %f", as.getRemainingTaxAllowance())
	}

	if len(as.lots) != 2 {
		t.Fatalf("Expected 2 lots after step-up, got %d", len(as.lots))
	}

	stepUpEvents := (*as.penaltyAnalysis)[prevEventsCount:]
	if len(stepUpEvents) != 2 {
		t.Fatalf("Expected 2 step-up penalty events (SELL and BUY), got %d", len(stepUpEvents))
	}
	if stepUpEvents[0].Type != "SELL" || stepUpEvents[0].Reason != "STEP UP" {
		t.Errorf("Expected first step-up event to be SELL with Reason STEP UP, got %s / %s", stepUpEvents[0].Type, stepUpEvents[0].Reason)
	}
	if stepUpEvents[1].Type != "BUY" || stepUpEvents[1].Reason != "STEP UP" {
		t.Errorf("Expected second step-up event to be BUY with Reason STEP UP, got %s / %s", stepUpEvents[1].Type, stepUpEvents[1].Reason)
	}

	// 4. Move to 2028 (outside allowance date range)
	as.currentMonth = time.Date(2028, 5, 1, 0, 0, 0, 0, time.UTC)
	if as.getRemainingTaxAllowance() != 0.0 {
		t.Errorf("Expected TaxAllowance to be 0 outside active date range, got %f", as.getRemainingTaxAllowance())
	}
}

func TestMultipleETFTaxAllowances(t *testing.T) {
	d1Start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d1End := time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)

	d2Start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d2End := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	parentVersion := &domain.AssetVersion{
		Type: "ETF",
		TaxAllowances: []domain.AssetTaxAllowance{
			{ID: "ta-h1", Amount: 500.0, StartDate: &d1Start, EndDate: &d1End},
			{ID: "ta-h2", Amount: 1000.0, StartDate: &d2Start, EndDate: &d2End},
		},
	}

	as := &assetState{
		asset: domain.Asset{
			ActiveVersion: parentVersion,
		},
		currentMonth: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}

	// In March 2026, only ta-h1 (500€) should be active
	rem := as.getRemainingTaxAllowance()
	if math.Abs(rem-500.0) > 0.01 {
		t.Errorf("Expected 500.0 active tax allowance in March, got %f", rem)
	}

	// In September 2026, only ta-h2 (1000€) should be active
	as.currentMonth = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rem = as.getRemainingTaxAllowance()
	if math.Abs(rem-1000.0) > 0.01 {
		t.Errorf("Expected 1000.0 active tax allowance in September, got %f", rem)
	}
}






