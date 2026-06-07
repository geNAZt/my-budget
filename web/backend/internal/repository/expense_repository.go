package repository

import (
	"database/sql"
	"encoding/json"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type ExpenseRepository struct {
	db *sql.DB
}

func NewExpenseRepository(db *sql.DB) *ExpenseRepository {
	return &ExpenseRepository{db: db}
}

func (r *ExpenseRepository) List(userID string) ([]domain.Expense, error) {
	query := `
		SELECT e.id, e.name, e.pool_id, e.created_at, v.id, v.amount, v.due_date, v.created_at
		FROM expenses e
		INNER JOIN expense_versions v ON e.id = v.expense_id
		WHERE e.user_id = ? AND e.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM expense_versions WHERE expense_id = e.id
		)
		ORDER BY v.due_date ASC, e.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load virtual account mappings for expenses
	vaMap := make(map[string][]string)
	vaQuery := `
		SELECT entity_id, virtual_account_id 
		FROM entity_virtual_accounts 
		WHERE entity_type = 'EXPENSE'`
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

	// Load all time slices for these versions
	sliceMap := make(map[string][]domain.TimeSlice)
	sliceRows, sliceErr := r.db.Query(`
		SELECT id, version_id, amount, interval_months, start_date, end_date, description
		FROM time_slices
		WHERE entity_type = 'EXPENSE'`)
	if sliceErr == nil {
		defer sliceRows.Close()
		for sliceRows.Next() {
			var s domain.TimeSlice
			var vID string
			var endDate sql.NullTime
			if err := sliceRows.Scan(&s.ID, &vID, &s.Value, &s.IntervalMonths, &s.StartDate, &endDate, &s.Description); err == nil {
				if endDate.Valid {
					s.EndDate = &endDate.Time
				}
				sliceMap[vID] = append(sliceMap[vID], s)
			}
		}
	}

	// Load all sub expenses for these versions
	subMap := make(map[string][]domain.SubExpense)
	subRows, subErr := r.db.Query(`
		SELECT id, expense_version_id, description, amount, metadata
		FROM sub_expenses`)
	if subErr == nil {
		defer subRows.Close()
		for subRows.Next() {
			var s domain.SubExpense
			var vID string
			var metadataJSON sql.NullString
			if err := subRows.Scan(&s.ID, &vID, &s.Description, &s.Amount, &metadataJSON); err == nil {
				if metadataJSON.Valid && metadataJSON.String != "" {
					_ = json.Unmarshal([]byte(metadataJSON.String), &s.Metadata)
				}
				if s.Metadata == nil {
					s.Metadata = make(map[string]string)
				}
				subMap[vID] = append(subMap[vID], s)
			}
		}
	}

	var expenses []domain.Expense
	for rows.Next() {
		var e domain.Expense
		var v domain.ExpenseVersion
		var poolID sql.NullString

		err := rows.Scan(&e.ID, &e.Name, &poolID, &e.CreatedAt, &v.ID, &v.Amount, &v.DueDate, &v.CreatedAt)
		if err != nil {
			return nil, err
		}

		if poolID.Valid {
			e.PoolID = &poolID.String
		}
		v.ExpenseID = e.ID
		v.Slices = sliceMap[v.ID]
		v.SubExpenses = subMap[v.ID]
		if v.SubExpenses == nil {
			v.SubExpenses = []domain.SubExpense{}
		}
		e.ActiveVersion = &v
		e.UserID = userID

		// Map the multi-assigned accounts
		if val, exists := vaMap[e.ID]; exists {
			e.AccountIDs = val
		} else {
			e.AccountIDs = []string{}
		}

		expenses = append(expenses, e)
	}

	return expenses, nil
}

func (r *ExpenseRepository) Save(userID string, expense *domain.Expense) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if expense.ID != "" {
		err = tx.QueryRow("SELECT 1 FROM expenses WHERE id = ? AND user_id = ?", expense.ID, userID).Scan(&exists)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
	}

	if !exists {
		if expense.ID == "" {
			expense.ID = uuid.New().String()
		}
		_, err = tx.Exec("INSERT INTO expenses (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", expense.ID, userID, expense.Name, expense.PoolID)
	} else {
		_, err = tx.Exec("UPDATE expenses SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", expense.Name, expense.PoolID, expense.ID, userID)
	}
	if err != nil {
		return err
	}

	// Save multiple virtual account linkages
	_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'EXPENSE'", expense.ID)
	if err != nil {
		return err
	}
	for _, vaID := range expense.AccountIDs {
		if vaID != "" {
			_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'EXPENSE', ?)", expense.ID, vaID)
			if err != nil {
				return err
			}
		}
	}

	v := expense.ActiveVersion
	v.ID = uuid.New().String()
	v.ExpenseID = expense.ID
	_, err = tx.Exec(`
		INSERT INTO expense_versions (id, expense_id, amount, due_date)
		VALUES (?, ?, ?, ?)`,
		v.ID, v.ExpenseID, v.Amount, v.DueDate)
	if err != nil {
		return err
	}

	// Save slices
	for _, s := range v.Slices {
		s.ID = uuid.New().String()
		_, err = tx.Exec(`
			INSERT INTO time_slices (id, version_id, entity_type, amount, interval_months, start_date, end_date, description)
			VALUES (?, ?, 'EXPENSE', ?, ?, ?, ?, ?)`,
			s.ID, v.ID, s.Value, s.IntervalMonths, s.StartDate, s.EndDate, s.Description)
		if err != nil {
			return err
		}
	}

	// Save sub expenses
	for i, s := range v.SubExpenses {
		if s.ID == "" {
			s.ID = uuid.New().String()
		}
		metadataBytes, _ := json.Marshal(s.Metadata)
		metadataStr := string(metadataBytes)
		_, err = tx.Exec(`
			INSERT INTO sub_expenses (id, expense_version_id, description, amount, metadata)
			VALUES (?, ?, ?, ?, ?)`,
			s.ID, v.ID, s.Description, s.Amount, metadataStr)
		if err != nil {
			return err
		}
		v.SubExpenses[i] = s
	}

	return tx.Commit()
}

func (r *ExpenseRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE expenses SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *ExpenseRepository) RevertLatest(userID string, expenseID string) error {
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM expenses WHERE id = ? AND user_id = ?", expenseID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		DELETE FROM expense_versions
		WHERE expense_id = ?
		AND created_at = (SELECT MAX(created_at) FROM expense_versions WHERE expense_id = ?)`,
		expenseID, expenseID)
	if err != nil {
		return err
	}

	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM expense_versions WHERE expense_id = ?", expenseID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE expenses SET is_deleted = TRUE WHERE id = ?", expenseID)
		return err
	}

	return nil
}
