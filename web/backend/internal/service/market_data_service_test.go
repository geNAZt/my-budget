package service

import (
	"math"
	"testing"
	"time"

	"github.com/wnjoon/go-yfinance/pkg/models"
)

func TestCalculateReturnsFromBars_GapInterpolation(t *testing.T) {
	// Create a mock provider with a gap
	// Week 1: 100
	// Week 3: 121 (Gap of 2 weeks. Total return 21%. Weekly should be 10%)
	date1, _ := time.Parse("2006-01-02", "2023-10-02") // Monday, Week 40
	date3, _ := time.Parse("2006-01-02", "2023-10-16") // Monday, Week 42

	bars := []models.Bar{
		{Date: date1, AdjClose: 100.0},
		{Date: date3, AdjClose: 121.0},
	}

	returns := calculateReturnsFromBars(bars, "MOCK")

	if len(returns) != 2 {
		t.Fatalf("Expected 2 returns, got %d", len(returns))
	}

	// 10% return (1.1^2 = 1.21)
	expectedReturn := 0.10
	tolerance := 0.000001

	if val, ok := returns["2023-W42"]; !ok || math.Abs(val-expectedReturn) > tolerance {
		t.Errorf("Expected 2023-W42 to be %f, got %f", expectedReturn, val)
	}
	if val, ok := returns["2023-W41"]; !ok || math.Abs(val-expectedReturn) > tolerance {
		t.Errorf("Expected 2023-W41 to be %f, got %f", expectedReturn, val)
	}
}