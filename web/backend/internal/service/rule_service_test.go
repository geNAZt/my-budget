package service

import (
	"testing"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func TestEvaluateRule(t *testing.T) {
	s := &RuleService{}

	// Helper to float pointer
	floatPtr := func(f float64) *float64 { return &f }

	// Test 1: Basic Leaf Rule Regex (Receiver)
	rule1 := domain.TransactionRule{
		Operator: "NONE",
		Field:    "RECEIVER",
		Regex:    "Amazon",
	}
	if !s.evaluateRule(rule1, "", "Amazon EU", "Order 123", "", "", 50.0) {
		t.Error("Expected rule1 to match Amazon EU")
	}
	if s.evaluateRule(rule1, "", "Starbucks", "Coffee", "", "", 10.0) {
		t.Error("Expected rule1 to not match Starbucks")
	}

	// Test 2: Amount comparisons
	ruleAmtLess := domain.TransactionRule{
		Operator:       "NONE",
		Field:          "AMOUNT",
		AmountOperator: "<",
		AmountValue:    floatPtr(50.0),
	}
	if !s.evaluateRule(ruleAmtLess, "", "Store", "Desc", "", "", 49.99) {
		t.Error("Expected 49.99 < 50.0 to match")
	}
	if s.evaluateRule(ruleAmtLess, "", "Store", "Desc", "", "", 50.0) {
		t.Error("Expected 50.0 < 50.0 to not match")
	}

	ruleAmtEqual := domain.TransactionRule{
		Operator:       "NONE",
		Field:          "AMOUNT",
		AmountOperator: "=",
		AmountValue:    floatPtr(-10.50),
	}
	if !s.evaluateRule(ruleAmtEqual, "", "Store", "Desc", "", "", -10.50) {
		t.Error("Expected -10.50 = -10.50 to match")
	}

	// Test 3: AND operator
	ruleAND := domain.TransactionRule{
		Operator: "AND",
		Children: []domain.TransactionRule{
			{
				Operator: "NONE",
				Field:    "RECEIVER",
				Regex:    "Starbucks",
			},
			{
				Operator:       "NONE",
				Field:          "AMOUNT",
				AmountOperator: ">",
				AmountValue:    floatPtr(10.0),
			},
		},
	}
	if !s.evaluateRule(ruleAND, "", "Starbucks Berlin", "Coffee", "", "", 12.50) {
		t.Error("Expected AND to match for Starbucks and amount > 10")
	}
	if s.evaluateRule(ruleAND, "", "Starbucks Berlin", "Coffee", "", "", 8.50) {
		t.Error("Expected AND to fail if amount <= 10")
	}
	if s.evaluateRule(ruleAND, "", "Dunkin Donuts", "Coffee", "", "", 15.00) {
		t.Error("Expected AND to fail if receiver is not Starbucks")
	}

	// Test 4: OR operator
	ruleOR := domain.TransactionRule{
		Operator: "OR",
		Children: []domain.TransactionRule{
			{
				Operator: "NONE",
				Field:    "DESCRIPTION",
				Regex:    "coffee",
			},
			{
				Operator: "NONE",
				Field:    "DESCRIPTION",
				Regex:    "cafe",
			},
		},
	}
	if !s.evaluateRule(ruleOR, "", "Store", "Morning coffee", "", "", 3.0) {
		t.Error("Expected OR to match 'coffee'")
	}
	if !s.evaluateRule(ruleOR, "", "Store", "Grande Cafe Latte", "", "", 4.0) {
		t.Error("Expected OR to match 'cafe'")
	}
	if s.evaluateRule(ruleOR, "", "Store", "Lunch sushi", "", "", 15.0) {
		t.Error("Expected OR to fail for 'sushi'")
	}

	// Test 5: Nested combination (AND containing OR and Leaf)
	// (RECEIVER Starbucks AND (DESCRIPTION coffee OR DESCRIPTION tea))
	ruleNested := domain.TransactionRule{
		Operator: "AND",
		Children: []domain.TransactionRule{
			{
				Operator: "NONE",
				Field:    "RECEIVER",
				Regex:    "Starbucks",
			},
			{
				Operator: "OR",
				Children: []domain.TransactionRule{
					{
						Operator: "NONE",
						Field:    "DESCRIPTION",
						Regex:    "coffee",
					},
					{
						Operator: "NONE",
						Field:    "DESCRIPTION",
						Regex:    "tea",
					},
				},
			},
		},
	}

	if !s.evaluateRule(ruleNested, "", "Starbucks Munich", "Green tea", "", "", 5.0) {
		t.Error("Expected nested to match Starbucks and green tea")
	}
	if s.evaluateRule(ruleNested, "", "Starbucks Munich", "Muffin", "", "", 3.5) {
		t.Error("Expected nested to fail for Starbucks and muffin")
	}

	// Test 6: Negate matches (REWE but not REWE Financial)
	ruleNegate := domain.TransactionRule{
		Operator: "AND",
		Children: []domain.TransactionRule{
			{
				Operator: "NONE",
				Field:    "RECEIVER",
				Regex:    "REWE",
			},
			{
				Operator: "NONE",
				Field:    "RECEIVER",
				Regex:    "REWE Financial",
				Negate:   true,
			},
		},
	}

	if !s.evaluateRule(ruleNegate, "", "REWE Supermarket", "Grocery", "", "", 25.0) {
		t.Error("Expected ruleNegate to match 'REWE Supermarket'")
	}
	if s.evaluateRule(ruleNegate, "", "REWE Financial Services", "Cashback", "", "", 10.0) {
		t.Error("Expected ruleNegate to reject 'REWE Financial Services' due to negation")
	}
}
