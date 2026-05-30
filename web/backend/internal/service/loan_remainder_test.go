package service

import (
	"testing"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func TestLoanRemainderStartDate(t *testing.T) {
	// Setup service with mock repos (not strictly needed for pure logic test if we call internal methods)
	s := &ProjectionService{}

	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	remainderStartDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	loan := domain.Loan{
		ID:   "loan1",
		Name: "Test Loan",
		ActiveVersion: &domain.LoanVersion{
			StartDate:          startDate,
			RemainderStartDate: &remainderStartDate,
			AmountLent:         1000,
			InterestRate:       0,
			RuntimeMonths:      10,
		},
	}

	// Test Month 1 (Jan 2026): Should NOT consume remainder
	currentDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(loan.ActiveVersion.StartDate, nil, currentDate, 1) {
		// This confirms it IS active as a loan
		if loan.ActiveVersion.RemainderStartDate != nil && !s.isActiveAt(*loan.ActiveVersion.RemainderStartDate, nil, currentDate, 1) {
			// This is the logic we added to the loop
			// It should correctly identify that it should NOT consume remainders yet
		} else {
			t.Errorf("Expected remainder to NOT be active in Jan 2026")
		}
	} else {
		t.Errorf("Expected loan to be active in Jan 2026")
	}

	// Test Month 3 (Mar 2026): SHOULD consume remainder
	currentDate = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if s.isActiveAt(loan.ActiveVersion.StartDate, nil, currentDate, 1) {
		if loan.ActiveVersion.RemainderStartDate != nil && s.isActiveAt(*loan.ActiveVersion.RemainderStartDate, nil, currentDate, 1) {
			// Correct
		} else {
			t.Errorf("Expected remainder to BE active in Mar 2026")
		}
	} else {
		t.Errorf("Expected loan to be active in Mar 2026")
	}
}
