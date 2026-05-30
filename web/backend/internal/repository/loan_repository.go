package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type LoanRepository struct {
	db *sql.DB
}

func NewLoanRepository(db *sql.DB) *LoanRepository {
	return &LoanRepository{db: db}
}

func (r *LoanRepository) List(userID string) ([]domain.Loan, error) {
	query := `
		SELECT l.id, l.name, l.pool_id, l.created_at, v.id, v.amount_lent, v.interest_rate, v.runtime_months, v.start_date, v.remainder_start_date, v.priority, v.next_loan_id, v.balloon_leftover, v.is_interest_only, v.early_payoff_penalty
		FROM loans l
		INNER JOIN loan_versions v ON l.id = v.loan_id
		WHERE l.user_id = ? AND l.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM loan_versions WHERE loan_id = l.id
		)
		ORDER BY v.priority ASC, l.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load virtual account mappings for loans
	vaMap := make(map[string][]string)
	vaQuery := `
		SELECT entity_id, virtual_account_id 
		FROM entity_virtual_accounts 
		WHERE entity_type = 'LOAN'`
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

	var loans []domain.Loan
	for rows.Next() {
		var l domain.Loan
		var v domain.LoanVersion
		var nextLoanID sql.NullString
		var poolID sql.NullString
		var remainderStartDate sql.NullTime

		err := rows.Scan(&l.ID, &l.Name, &poolID, &l.CreatedAt, &v.ID, &v.AmountLent, &v.InterestRate, &v.RuntimeMonths, &v.StartDate, &remainderStartDate, &v.Priority, &nextLoanID, &v.BalloonLeftover, &v.IsInterestOnly, &v.EarlyPayoffPenalty)
		if err != nil {
			return nil, err
		}

		if poolID.Valid {
			l.PoolID = &poolID.String
		}
		if nextLoanID.Valid {
			v.NextLoanID = &nextLoanID.String
		}
		if remainderStartDate.Valid {
			v.RemainderStartDate = &remainderStartDate.Time
		}

		v.LoanID = l.ID
		l.ActiveVersion = &v
		l.UserID = userID

		// Map the multi-assigned accounts
		if val, exists := vaMap[l.ID]; exists {
			l.AccountIDs = val
		} else {
			l.AccountIDs = []string{}
		}

		loans = append(loans, l)
	}

	return loans, nil
}

func (r *LoanRepository) Save(userID string, loan *domain.Loan) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if loan.ID == "" {
		loan.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO loans (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", loan.ID, userID, loan.Name, loan.PoolID)
	} else {
		_, err = tx.Exec("UPDATE loans SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", loan.Name, loan.PoolID, loan.ID, userID)
	}
	if err != nil {
		return err
	}

	// Save multiple virtual account linkages
	_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'LOAN'", loan.ID)
	if err != nil {
		return err
	}
	for _, vaID := range loan.AccountIDs {
		if vaID != "" {
			_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'LOAN', ?)", loan.ID, vaID)
			if err != nil {
				return err
			}
		}
	}

	v := loan.ActiveVersion
	v.ID = uuid.New().String()
	v.LoanID = loan.ID

	_, err = tx.Exec(`
		INSERT INTO loan_versions (id, loan_id, amount_lent, interest_rate, runtime_months, start_date, remainder_start_date, priority, next_loan_id, balloon_leftover, is_interest_only, early_payoff_penalty)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.LoanID, v.AmountLent, v.InterestRate, v.RuntimeMonths, v.StartDate, v.RemainderStartDate, v.Priority, v.NextLoanID, v.BalloonLeftover, v.IsInterestOnly, v.EarlyPayoffPenalty)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *LoanRepository) SaveBulk(userID string, loans []domain.Loan) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, loan := range loans {
		if loan.ID == "" {
			loan.ID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO loans (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", loan.ID, userID, loan.Name, loan.PoolID)
		} else {
			_, err = tx.Exec("UPDATE loans SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", loan.Name, loan.PoolID, loan.ID, userID)
		}
		if err != nil {
			return err
		}

		// Save multiple virtual account linkages
		_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'LOAN'", loan.ID)
		if err != nil {
			return err
		}
		for _, vaID := range loan.AccountIDs {
			if vaID != "" {
				_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'LOAN', ?)", loan.ID, vaID)
				if err != nil {
					return err
				}
			}
		}

		v := loan.ActiveVersion
		v.ID = uuid.New().String()
		v.LoanID = loan.ID

		_, err = tx.Exec(`
			INSERT INTO loan_versions (id, loan_id, amount_lent, interest_rate, runtime_months, start_date, remainder_start_date, priority, next_loan_id, balloon_leftover, is_interest_only, early_payoff_penalty)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.ID, v.LoanID, v.AmountLent, v.InterestRate, v.RuntimeMonths, v.StartDate, v.RemainderStartDate, v.Priority, v.NextLoanID, v.BalloonLeftover, v.IsInterestOnly, v.EarlyPayoffPenalty)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *LoanRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE loans SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *LoanRepository) RevertLatest(userID string, loanID string) error {
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM loans WHERE id = ? AND user_id = ?", loanID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		DELETE FROM loan_versions
		WHERE loan_id = ?
		AND created_at = (SELECT MAX(created_at) FROM loan_versions WHERE loan_id = ?)`,
		loanID, loanID)
	if err != nil {
		return err
	}

	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM loan_versions WHERE loan_id = ?", loanID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE loans SET is_deleted = TRUE WHERE id = ?", loanID)
		return err
	}

	return nil
}
