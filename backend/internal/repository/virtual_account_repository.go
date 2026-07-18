package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/google/uuid"
)

type VirtualAccountRepository struct {
	db *sql.DB
}

func NewVirtualAccountRepository(db *sql.DB) *VirtualAccountRepository {
	return &VirtualAccountRepository{db: db}
}

func (r *VirtualAccountRepository) List(userID string) ([]domain.VirtualAccount, error) {
	// Subquery to find the latest created_at for each virtual_account_id
	query := `
		SELECT va.id, va.name, va.created_at, vav.id, vav.color, vav.starting_balance, vav.description, vav.realtime_account_id, vav.created_at
		FROM virtual_accounts va
		INNER JOIN virtual_account_versions vav ON va.id = vav.virtual_account_id
		WHERE va.user_id = ? AND va.is_deleted = FALSE
		AND vav.created_at = (
			SELECT MAX(created_at) FROM virtual_account_versions WHERE virtual_account_id = va.id
		)
		ORDER BY va.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.VirtualAccount
	for rows.Next() {
		var va domain.VirtualAccount
		var v domain.VirtualAccountVersion

		err := rows.Scan(&va.ID, &va.Name, &va.CreatedAt, &v.ID, &v.Color, &v.StartingBalance, &v.Description, &v.RealtimeAccountID, &v.CreatedAt)
		if err != nil {
			return nil, err
		}

		v.VirtualAccountID = va.ID
		va.ActiveVersion = &v
		va.UserID = userID
		accounts = append(accounts, va)
	}

	return accounts, nil
}

func (r *VirtualAccountRepository) Save(userID string, va *domain.VirtualAccount) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if va.ID == "" {
		va.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO virtual_accounts (id, user_id, name) VALUES (?, ?, ?)", va.ID, userID, va.Name)
	} else {
		_, err = tx.Exec("UPDATE virtual_accounts SET name = ? WHERE id = ? AND user_id = ?", va.Name, va.ID, userID)
	}
	if err != nil {
		return err
	}

	// Create new immutable version
	v := va.ActiveVersion
	v.ID = uuid.New().String()
	v.VirtualAccountID = va.ID
	_, err = tx.Exec(`
		INSERT INTO virtual_account_versions (id, virtual_account_id, color, starting_balance, description, realtime_account_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.ID, v.VirtualAccountID, v.Color, v.StartingBalance, v.Description, v.RealtimeAccountID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ArchiveFull soft-deletes the entire virtual account entity
func (r *VirtualAccountRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE virtual_accounts SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// RevertLatest deletes only the most recent version of a virtual account
func (r *VirtualAccountRepository) RevertLatest(userID string, vaID string) error {
	// 1. Verify ownership
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM virtual_accounts WHERE id = ? AND user_id = ?", vaID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	// 2. Delete the latest version
	_, err = r.db.Exec(`
		DELETE FROM virtual_account_versions
		WHERE virtual_account_id = ?
		AND created_at = (SELECT MAX(created_at) FROM virtual_account_versions WHERE virtual_account_id = ?)`,
		vaID, vaID)
	if err != nil {
		return err
	}

	// 3. If no versions left, mark virtual account as deleted
	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM virtual_account_versions WHERE virtual_account_id = ?", vaID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE virtual_accounts SET is_deleted = TRUE WHERE id = ?", vaID)
		return err
	}

	return nil
}
