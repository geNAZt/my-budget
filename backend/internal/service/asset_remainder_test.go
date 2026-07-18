package service

import (
	"testing"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/domain"
)

func TestAssetRemainderStartDate(t *testing.T) {
	s := &ProjectionService{}

	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	remainderStartDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	asset := domain.Asset{
		ID:   "asset1",
		Name: "Test Asset",
		ActiveVersion: &domain.AssetVersion{
			StartDate:          startDate,
			RemainderStartDate: &remainderStartDate,
			TargetValue:        "10000",
		},
	}

	// Test Month 1 (Jan 2026): Should NOT consume remainder
	currentDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(asset.ActiveVersion.StartDate, asset.ActiveVersion.EndDate, currentDate, 1) {
		if asset.ActiveVersion.RemainderStartDate != nil && !s.isActiveAt(*asset.ActiveVersion.RemainderStartDate, nil, currentDate, 1) {
			// Correct: Should NOT consume
		} else {
			t.Errorf("Expected asset remainder to NOT be active in Jan 2026")
		}
	}

	// Test Month 3 (Mar 2026): SHOULD consume remainder
	currentDate = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(asset.ActiveVersion.StartDate, asset.ActiveVersion.EndDate, currentDate, 1) {
		if asset.ActiveVersion.RemainderStartDate != nil && s.isActiveAt(*asset.ActiveVersion.RemainderStartDate, nil, currentDate, 1) {
			// Correct: SHOULD consume
		} else {
			t.Errorf("Expected asset remainder to BE active in Mar 2026")
		}
	}
}

func TestSubAssetRemainderStartDate(t *testing.T) {
	s := &ProjectionService{}

	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	remainderStartDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	sa := subAssetState{
		id:                  "sa1",
		name:                "Sub Asset",
		startDate:           startDate,
		remainderStartDate:  &remainderStartDate,
		isRemainderConsumer: true,
		amountPerMonth:      0,
	}

	// Test Month 1 (Jan 2026): Should NOT consume remainder
	currentDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
		if sa.remainderStartDate != nil && !s.isActiveAt(*sa.remainderStartDate, nil, currentDate, 1) {
			// Correct
		} else {
			t.Errorf("Expected sub-asset remainder to NOT be active in Jan 2026")
		}
	}

	// Test Month 3 (Mar 2026): SHOULD consume remainder
	currentDate = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
		if sa.remainderStartDate != nil && s.isActiveAt(*sa.remainderStartDate, nil, currentDate, 1) {
			// Correct
		} else {
			t.Errorf("Expected sub-asset remainder to BE active in Mar 2026")
		}
	}
}
