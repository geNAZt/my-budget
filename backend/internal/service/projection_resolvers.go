package service

import (
	"github.com/genazt/my-budget-script/backend/internal/domain"
)

func (s *ProjectionService) resolveIncomes(userID string, scenario *domain.Scenario) ([]domain.Income, error) {
	all, err := s.incomeRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Income
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "INCOME" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveBills(userID string, scenario *domain.Scenario) ([]domain.Bill, error) {
	all, err := s.billRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Bill
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "BILL" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveExpenses(userID string, scenario *domain.Scenario) ([]domain.Expense, error) {
	all, err := s.expenseRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Expense
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "EXPENSE" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveAssets(userID string, scenario *domain.Scenario) ([]domain.Asset, error) {
	all, err := s.assetRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Asset
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "ASSET" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveLoans(userID string, scenario *domain.Scenario) ([]domain.Loan, error) {
	all, err := s.loanRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Loan
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "LOAN" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveModifications(userID string, scenario *domain.Scenario) ([]domain.Modification, error) {
	all, err := s.modRepo.List(userID)
	if err != nil {
		return nil, err
	}

	if len(scenario.Entities) == 0 {
		return all, nil
	}

	// Create a fast lookup map of active entity IDs by type
	activeEntities := make(map[string]map[string]bool)
	for _, e := range scenario.Entities {
		if _, ok := activeEntities[e.EntityType]; !ok {
			activeEntities[e.EntityType] = make(map[string]bool)
		}
		activeEntities[e.EntityType][e.EntityID] = true
	}

	var filtered []domain.Modification
	for _, item := range all {
		// If explicitly linked to the scenario
		if activeEntities["MODIFICATION"] != nil && activeEntities["MODIFICATION"][item.ID] {
			filtered = append(filtered, item)
			continue
		}

		// Or if the target is active in the scenario
		if item.TargetType == "ASSET" {
			targetActive := false
			if item.TargetID != "" && activeEntities["ASSET"] != nil && activeEntities["ASSET"][item.TargetID] {
				targetActive = true
			}
			if !targetActive && activeEntities["ASSET"] != nil {
				for _, tid := range item.TargetIDs {
					if activeEntities["ASSET"][tid] {
						targetActive = true
						break
					}
				}
			}
			if targetActive {
				filtered = append(filtered, item)
			}
		} else if item.TargetType == "LOAN" {
			if item.TargetID != "" && activeEntities["LOAN"] != nil && activeEntities["LOAN"][item.TargetID] {
				filtered = append(filtered, item)
			}
		}
	}
	return filtered, nil
}
