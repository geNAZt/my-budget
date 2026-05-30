package service

import (
	"testing"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func TestProjectionMonthBoundaryDay(t *testing.T) {
	label := projectionMonthForDate(time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC), 26)
	if !label.Equal(time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May projection month to be labelled 2026-05-26, got %s", label.Format(time.DateOnly))
	}

	start, end := projectionPeriodBounds(label, 26)
	if !start.Equal(time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May period to start 2026-04-26, got %s", start.Format(time.DateOnly))
	}
	if !end.Equal(time.Date(2026, time.May, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected May period to end 2026-05-26, got %s", end.Format(time.DateOnly))
	}

	aprilPaycheck := projectionMonthForDate(time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC), 26)
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
