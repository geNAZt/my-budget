package repository

import (
	"database/sql"
	"strings"

	"github.com/google/uuid"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

type RuleRepository struct {
	db *sql.DB
}

func NewRuleRepository(db *sql.DB) *RuleRepository {
	return &RuleRepository{db: db}
}

// Pools

func (r *RuleRepository) ListPools(userID string) ([]domain.TransactionPool, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, parent_id, name, color, is_hidden, created_at
		FROM transaction_pools WHERE user_id = ? ORDER BY name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pools := []domain.TransactionPool{}
	for rows.Next() {
		var p domain.TransactionPool
		var parentID sql.NullString
		err := rows.Scan(&p.ID, &p.UserID, &parentID, &p.Name, &p.Color, &p.IsHidden, &p.CreatedAt)
		if err != nil {
			return nil, err
		}
		if parentID.Valid && parentID.String != "" {
			p.ParentID = &parentID.String
		}
		pools = append(pools, p)
	}
	return pools, nil
}

func (r *RuleRepository) SavePool(userID string, p *domain.TransactionPool) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	_, err := r.db.Exec(`
		INSERT INTO transaction_pools (id, user_id, parent_id, name, color, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
            parent_id = excluded.parent_id,
            name = excluded.name,
            color = excluded.color,
            is_hidden = excluded.is_hidden`,
		p.ID, userID, p.ParentID, p.Name, p.Color, p.IsHidden)
	return err
}

func (r *RuleRepository) DeletePool(userID string, id string) error {
	// Should probably unassign transactions first or have FK RESTRICT
	_, err := r.db.Exec("DELETE FROM transaction_pools WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// Rules

func (r *RuleRepository) ListRules(userID string) ([]domain.TransactionRule, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate, created_at
		FROM transaction_rules WHERE user_id = ? ORDER BY priority DESC, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allRules []*domain.TransactionRule
	for rows.Next() {
		var rule domain.TransactionRule
		var parentID, integrationID, targetPoolID sql.NullString
		var operator, field, regex, amountOperator sql.NullString
		var amountValue sql.NullFloat64

		err := rows.Scan(
			&rule.ID,
			&rule.UserID,
			&parentID,
			&integrationID,
			&targetPoolID,
			&operator,
			&field,
			&regex,
			&amountOperator,
			&amountValue,
			&rule.Priority,
			&rule.Negate,
			&rule.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if parentID.Valid && parentID.String != "" {
			rule.ParentID = &parentID.String
		}
		if integrationID.Valid && integrationID.String != "" {
			rule.IntegrationID = &integrationID.String
		}
		if targetPoolID.Valid && targetPoolID.String != "" {
			rule.TargetPoolID = &targetPoolID.String
		}
		if amountValue.Valid {
			rule.AmountValue = &amountValue.Float64
		}
		rule.Operator = nullStringOr(operator, "NONE")
		rule.Field = nullStringOr(field, "NONE")
		rule.Regex = nullStringOr(regex, "")
		rule.AmountOperator = nullStringOr(amountOperator, "")

		rule.Children = []domain.TransactionRule{}
		allRules = append(allRules, &rule)
	}

	return buildTree(allRules, nil), nil
}

func buildTree(allRules []*domain.TransactionRule, parentID *string) []domain.TransactionRule {
	var result []domain.TransactionRule
	for _, rule := range allRules {
		match := false
		if parentID == nil {
			match = (rule.ParentID == nil || *rule.ParentID == "")
		} else {
			match = (rule.ParentID != nil && *rule.ParentID == *parentID)
		}

		if match {
			rule.Children = buildTree(allRules, &rule.ID)
			result = append(result, *rule)
		}
	}
	return result
}

func (r *RuleRepository) SaveRule(userID string, rule *domain.TransactionRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := r.saveRuleTx(tx, userID, rule); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *RuleRepository) saveRuleTx(tx *sql.Tx, userID string, rule *domain.TransactionRule) error {
	normalizeRule(rule)

	_, err := tx.Exec(`
		INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
            parent_id = excluded.parent_id,
            integration_id = excluded.integration_id,
            target_pool_id = excluded.target_pool_id,
            operator = excluded.operator,
            field = excluded.field,
            regex = excluded.regex,
            amount_operator = excluded.amount_operator,
            amount_value = excluded.amount_value,
            priority = excluded.priority,
            negate = excluded.negate`,
		rule.ID, userID, rule.ParentID, rule.IntegrationID, rule.TargetPoolID, rule.Operator, rule.Field, rule.Regex, rule.AmountOperator, rule.AmountValue, rule.Priority, rule.Negate)
	if err != nil {
		return err
	}

	// Fetch all existing child IDs in the database for this rule
	rows, err := tx.Query("SELECT id FROM transaction_rules WHERE parent_id = ?", rule.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	existingChildren := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			existingChildren[id] = true
		}
	}

	// Save all new children and track which ones we are keeping
	keptChildren := make(map[string]bool)
	for i := range rule.Children {
		child := &rule.Children[i]
		if child.ID == "" {
			child.ID = uuid.New().String()
		}
		child.UserID = userID
		child.ParentID = &rule.ID
		child.Priority = rule.Priority

		if err := r.saveRuleTx(tx, userID, child); err != nil {
			return err
		}
		keptChildren[child.ID] = true
	}

	// Delete any children that were in the database but are no longer in the list
	for id := range existingChildren {
		if !keptChildren[id] {
			_, err := tx.Exec("DELETE FROM transaction_rules WHERE id = ?", id)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func nullStringOr(value sql.NullString, fallback string) string {
	if !value.Valid {
		return fallback
	}
	return value.String
}

func optionalString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

func optionalFloat64(value *float64, enabled bool) *float64 {
	if !enabled {
		return nil
	}
	return value
}

func normalizeRule(rule *domain.TransactionRule) {
	rule.ParentID = optionalString(rule.ParentID)
	rule.IntegrationID = optionalString(rule.IntegrationID)
	rule.TargetPoolID = optionalString(rule.TargetPoolID)

	if rule.Operator == "" {
		rule.Operator = "NONE"
	}
	if rule.Field == "" {
		rule.Field = "NONE"
	}
	if rule.AmountOperator == "" && rule.Field == "AMOUNT" {
		rule.AmountOperator = ">"
	}
	rule.AmountValue = optionalFloat64(rule.AmountValue, rule.Field == "AMOUNT")

	for i := range rule.Children {
		normalizeRule(&rule.Children[i])
	}
}

func (r *RuleRepository) DeleteRule(userID string, id string) error {
	_, err := r.db.Exec("DELETE FROM transaction_rules WHERE id = ? AND user_id = ?", id, userID)
	return err
}
