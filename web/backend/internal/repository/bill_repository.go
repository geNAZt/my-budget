package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type BillRepository struct {
	db *sql.DB
}

func NewBillRepository(db *sql.DB) *BillRepository {
	return &BillRepository{db: db}
}

func (r *BillRepository) List(userID string) ([]domain.Bill, error) {
	query := `
		SELECT b.id, b.name, b.pool_id, b.created_at, v.id, v.amount, v.start_date, v.end_date, v.interval_months, v.created_at
		FROM bills b
		INNER JOIN bill_versions v ON b.id = v.bill_id
		WHERE b.user_id = ? AND b.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM bill_versions WHERE bill_id = b.id
		)
		ORDER BY b.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load virtual account mappings for bills
	vaMap := make(map[string][]string)
	vaQuery := `
		SELECT entity_id, virtual_account_id 
		FROM entity_virtual_accounts 
		WHERE entity_type = 'BILL'`
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

	var bills []domain.Bill
	for rows.Next() {
		var b domain.Bill
		var v domain.BillVersion
		var endDate sql.NullTime
		var poolID sql.NullString

		err := rows.Scan(&b.ID, &b.Name, &poolID, &b.CreatedAt, &v.ID, &v.Amount, &v.StartDate, &endDate, &v.IntervalMonths, &v.CreatedAt)
		if err != nil {
			return nil, err
		}

		if poolID.Valid {
			b.PoolID = &poolID.String
		}
		if endDate.Valid {
			v.EndDate = &endDate.Time
		}
		v.BillID = b.ID
		b.ActiveVersion = &v
		b.UserID = userID

		// Map the multi-assigned accounts
		if val, exists := vaMap[b.ID]; exists {
			b.AccountIDs = val
		} else {
			b.AccountIDs = []string{}
		}

		bills = append(bills, b)
	}

	return bills, nil
}

func (r *BillRepository) Save(userID string, bill *domain.Bill) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if bill.ID == "" {
		bill.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO bills (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", bill.ID, userID, bill.Name, bill.PoolID)
	} else {
		_, err = tx.Exec("UPDATE bills SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", bill.Name, bill.PoolID, bill.ID, userID)
	}
	if err != nil {
		return err
	}

	// Save multiple virtual account linkages
	_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'BILL'", bill.ID)
	if err != nil {
		return err
	}
	for _, vaID := range bill.AccountIDs {
		if vaID != "" {
			_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'BILL', ?)", bill.ID, vaID)
			if err != nil {
				return err
			}
		}
	}

	// Create new immutable version
	v := bill.ActiveVersion
	v.ID = uuid.New().String()
	v.BillID = bill.ID
	_, err = tx.Exec(`
		INSERT INTO bill_versions (id, bill_id, amount, start_date, end_date, interval_months)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.ID, v.BillID, v.Amount, v.StartDate, v.EndDate, v.IntervalMonths)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *BillRepository) SaveBulk(userID string, bills []domain.Bill) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, bill := range bills {
		if bill.ID == "" {
			bill.ID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO bills (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", bill.ID, userID, bill.Name, bill.PoolID)
		} else {
			_, err = tx.Exec("UPDATE bills SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", bill.Name, bill.PoolID, bill.ID, userID)
		}
		if err != nil {
			return err
		}

		// Save multiple virtual account linkages
		_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'BILL'", bill.ID)
		if err != nil {
			return err
		}
		for _, vaID := range bill.AccountIDs {
			if vaID != "" {
				_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'BILL', ?)", bill.ID, vaID)
				if err != nil {
					return err
				}
			}
		}

		v := bill.ActiveVersion
		v.ID = uuid.New().String()
		v.BillID = bill.ID
		_, err = tx.Exec(`
			INSERT INTO bill_versions (id, bill_id, amount, start_date, end_date, interval_months)
			VALUES (?, ?, ?, ?, ?, ?)`,
			v.ID, v.BillID, v.Amount, v.StartDate, v.EndDate, v.IntervalMonths)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *BillRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE bills SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *BillRepository) RevertLatest(userID string, billID string) error {
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM bills WHERE id = ? AND user_id = ?", billID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		DELETE FROM bill_versions
		WHERE bill_id = ?
		AND created_at = (SELECT MAX(created_at) FROM bill_versions WHERE bill_id = ?)`,
		billID, billID)
	if err != nil {
		return err
	}

	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM bill_versions WHERE bill_id = ?", billID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE bills SET is_deleted = TRUE WHERE id = ?", billID)
		return err
	}

	return nil
}
