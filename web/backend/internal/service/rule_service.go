package service

import (
	"regexp"
	"sort"
	"strings"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

type RuleService struct {
	repo *repository.RuleRepository
}

func NewRuleService(repo *repository.RuleRepository) *RuleService {

	return &RuleService{repo: repo}
}

// ProcessTransaction matches a single transaction against all applicable rules and returns the target pool IDs
func (s *RuleService) ProcessTransaction(userID string, integrationID string, receiver string, description string, tags string, accountTags string, accountName string, amount float64) ([]string, error) {

	rules, err := s.repo.ListRules(userID)
	if err != nil {
		return nil, err
	}

	// Sort rules solely by priority descending (all rules are global now!)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})

	var matchedPools []string
	seenPools := make(map[string]bool)

	for _, rule := range rules {
		if s.evaluateRule(rule, integrationID, receiver, description, tags, accountTags, accountName, amount) {
			if rule.TargetPoolID != nil && *rule.TargetPoolID != "" && !seenPools[*rule.TargetPoolID] {
				matchedPools = append(matchedPools, *rule.TargetPoolID)
				seenPools[*rule.TargetPoolID] = true
			}
		}
	}

	return matchedPools, nil
}

func (s *RuleService) evaluateRule(rule domain.TransactionRule, integrationID string, receiver string, description string, tags string, accountTags string, accountName string, amount float64) bool {
	matched := false

	// If it's a container rule (operator is AND or OR)
	if rule.Operator == "AND" || rule.Operator == "OR" {
		if len(rule.Children) > 0 {
			if rule.Operator == "AND" {
				matched = true
				for _, child := range rule.Children {
					if !s.evaluateRule(child, integrationID, receiver, description, tags, accountTags, accountName, amount) {
						matched = false
						break
					}
				}
			} else if rule.Operator == "OR" {
				for _, child := range rule.Children {
					if s.evaluateRule(child, integrationID, receiver, description, tags, accountTags, accountName, amount) {
						matched = true
						break
					}
				}
			}
		}
	} else {
		// Otherwise, it's a leaf rule, so we evaluate the rule's specific field condition
		var target string
		switch rule.Field {
		case "RECEIVER":
			target = strings.ToLower(receiver)
		case "DESCRIPTION":
			target = strings.ToLower(description)
		case "TAGS":
			target = strings.ToLower(tags)
		case "ACCOUNT_TAGS":
			target = strings.ToLower(accountTags)
		case "ACCOUNT_NAME":
			target = strings.ToLower(accountName)
		case "DATA_CHAIN":
			target = strings.ToLower(integrationID)
		case "AMOUNT":
			if rule.AmountValue != nil {
				val := *rule.AmountValue
				switch rule.AmountOperator {
				case ">":
					matched = amount > val
				case "<":
					matched = amount < val
				case "=":
					matched = amount == val
				case ">=":
					matched = amount >= val
				case "<=":
					matched = amount <= val
				}
			}
		}

		if rule.Field != "AMOUNT" && rule.Field != "NONE" {
			// Regex matching for string fields
			m, _ := regexp.MatchString("(?i)"+rule.Regex, target)
			matched = m
		}
	}

	if rule.Negate {
		return !matched
	}
	return matched
}
