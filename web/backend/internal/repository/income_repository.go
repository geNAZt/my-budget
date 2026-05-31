package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type IncomeRepository struct {
	db *sql.DB
}

func NewIncomeRepository(db *sql.DB) *IncomeRepository {
	return &IncomeRepository{db: db}
}

func (r *IncomeRepository) List(userID string) ([]domain.Income, error) {
	query := `
		SELECT i.id, i.name, i.pool_id, i.created_at, v.id, v.amount, v.stop_modification_id, v.start_date, v.end_date, v.interval_months, v.created_at
		FROM incomes i
		INNER JOIN income_versions v ON i.id = v.income_id
		WHERE i.user_id = ? AND i.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM income_versions WHERE income_id = i.id
		)
		ORDER BY i.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load virtual account mappings for incomes
	vaMap := make(map[string][]string)
	vaQuery := `
		SELECT entity_id, virtual_account_id 
		FROM entity_virtual_accounts 
		WHERE entity_type = 'INCOME'`
	vaRows, vaErr := r.db.Query(vaQuery)
	if vaErr == nil {
		defer vaRows.Close()
		for vaRows.Next() {
			var entID, vaID string
			if scanErr := vaRows.Scan(&entID, &vaID); scanErr == nil {
				vaMap[entID] = append(vaMap[entID], vaID)
			}
		}
	}

	var incomes []domain.Income
	for rows.Next() {
		var i domain.Income
		var v domain.IncomeVersion
		var endDate sql.NullTime
		var stopModID sql.NullString
		var poolID sql.NullString

		err := rows.Scan(&i.ID, &i.Name, &poolID, &i.CreatedAt, &v.ID, &v.Amount, &stopModID, &v.StartDate, &endDate, &v.IntervalMonths, &v.CreatedAt)
		if err != nil {
			return nil, err
		}

		if poolID.Valid {
			i.PoolID = &poolID.String
		}
		if stopModID.Valid {
			v.StopModificationID = &stopModID.String
		}
		if endDate.Valid {
			v.EndDate = &endDate.Time
		}
		v.IncomeID = i.ID
		i.ActiveVersion = &v
		i.UserID = userID

		// Map the multi-assigned accounts
		if val, exists := vaMap[i.ID]; exists {
			i.AccountIDs = val
		} else {
			i.AccountIDs = []string{}
		}

		incomes = append(incomes, i)
	}

	return incomes, nil
}

func (r *IncomeRepository) Save(userID string, income *domain.Income) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if income.ID == "" {
		income.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO incomes (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", income.ID, userID, income.Name, income.PoolID)
	} else {
		_, err = tx.Exec("UPDATE incomes SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", income.Name, income.PoolID, income.ID, userID)
	}
	if err != nil {
		return err
	}

	// Save multiple virtual account linkages
	_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'INCOME'", income.ID)
	if err != nil {
		return err
	}
	for _, vaID := range income.AccountIDs {
		if vaID != "" {
			_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'INCOME', ?)", income.ID, vaID)
			if err != nil {
				return err
			}
		}
	}

	// Create new immutable version
	v := income.ActiveVersion
	v.ID = uuid.New().String()
	v.IncomeID = income.ID
	_, err = tx.Exec(`
		INSERT INTO income_versions (id, income_id, amount, stop_modification_id, start_date, end_date, interval_months)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.IncomeID, v.Amount, v.StopModificationID, v.StartDate, v.EndDate, v.IntervalMonths)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *IncomeRepository) SaveBulk(userID string, incomes []domain.Income) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, income := range incomes {
		if income.ID == "" {
			income.ID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO incomes (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", income.ID, userID, income.Name, income.PoolID)
		} else {
			_, err = tx.Exec("UPDATE incomes SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", income.Name, income.PoolID, income.ID, userID)
		}
		if err != nil {
			return err
		}

		// Save multiple virtual account linkages
		_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'INCOME'", income.ID)
		if err != nil {
			return err
		}
		for _, vaID := range income.AccountIDs {
			if vaID != "" {
				_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'INCOME', ?)", income.ID, vaID)
				if err != nil {
					return err
				}
			}
		}

		v := income.ActiveVersion
		v.ID = uuid.New().String()
		v.IncomeID = income.ID
		_, err = tx.Exec(`
			INSERT INTO income_versions (id, income_id, amount, stop_modification_id, start_date, end_date, interval_months)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			v.ID, v.IncomeID, v.Amount, v.StopModificationID, v.StartDate, v.EndDate, v.IntervalMonths)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ArchiveFull soft-deletes the entire income entity
func (r *IncomeRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE incomes SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// RevertLatest deletes only the most recent version of an income
func (r *IncomeRepository) RevertLatest(userID string, incomeID string) error {
	// 1. Verify ownership
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM incomes WHERE id = ? AND user_id = ?", incomeID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	// 2. Delete the latest version
	_, err = r.db.Exec(`
		DELETE FROM income_versions
		WHERE income_id = ?
		AND created_at = (SELECT MAX(created_at) FROM income_versions WHERE income_id = ?)`,
		incomeID, incomeID)
	if err != nil {
		return err
	}

	// 3. If no versions left, mark income as deleted
	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM income_versions WHERE income_id = ?", incomeID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE incomes SET is_deleted = TRUE WHERE id = ?", incomeID)
		return err
	}

	return nil
}
