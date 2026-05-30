package repository

import (
	"database/sql"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

type TransactionRepository struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

func (r *TransactionRepository) SaveBulk(userID string, txs []domain.BankTransaction) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, _ := tx.Prepare(`
		INSERT INTO bank_transactions (id, user_id, integration_id, account_id, source_account_id, destination_account_id, pool_id, tags, external_id, encrypted_data, linked_transaction_id, is_link_confirmed, correlation_id, is_deleted, created_at, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, external_id) DO UPDATE SET
            integration_id = excluded.integration_id,
            account_id = excluded.account_id,
            encrypted_data = excluded.encrypted_data,
            correlation_id = excluded.correlation_id,
            created_at = excluded.created_at,
            synced_at = excluded.synced_at`)
	defer stmt.Close()

	for _, t := range txs {
		_, err = stmt.Exec(t.ID, userID, t.IntegrationID, t.AccountID, t.SourceAccountID, t.DestinationAccountID, t.PoolID, t.Tags, t.ExternalID, t.EncryptedData, t.LinkedTransactionID, t.IsLinkConfirmed, t.CorrelationID, t.IsDeleted, t.CreatedAt, t.SyncedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *TransactionRepository) List(userID string) ([]domain.BankTransaction, error) {
	return r.ListWithFilters(userID, "", "", "")
}

func (r *TransactionRepository) ListWithFilters(userID string, poolID string, startDate string, endDate string) ([]domain.BankTransaction, error) {
	query := `
		SELECT id, user_id, integration_id, account_id, source_account_id, destination_account_id, pool_id, tags, external_id, encrypted_data, linked_transaction_id, is_link_confirmed, COALESCE(correlation_id, ''), COALESCE(is_deleted, FALSE), COALESCE(denied_duplicate_ids, ''), created_at, synced_at
		FROM bank_transactions WHERE user_id = ? AND COALESCE(is_deleted, FALSE) = FALSE`
	args := []interface{}{userID}

	if poolID != "" {
		query += " AND pool_id = ?"
		args = append(args, poolID)
	}
	if startDate != "" {
		query += " AND created_at >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		query += " AND created_at <= ?"
		args = append(args, endDate)
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var pID sql.NullString
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, &pID, &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt)
		if err != nil {
			return nil, err
		}
		if pID.Valid {
			t.PoolID = &pID.String
		}
		if aID.Valid {
			t.AccountID = aID.String
		}
		if saID.Valid {
			t.SourceAccountID = saID.String
		}
		if daID.Valid {
			t.DestinationAccountID = daID.String
		}
		if tags.Valid {
			t.Tags = tags.String
		}
		if ltID.Valid {
			t.LinkedTransactionID = &ltID.String
		}
		txs = append(txs, t)
	}

	return txs, nil
}

func (r *TransactionRepository) ListByIntegration(userID string, integrationID string) ([]domain.BankTransaction, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, integration_id, account_id, source_account_id, destination_account_id, pool_id, tags, external_id, encrypted_data, linked_transaction_id, is_link_confirmed, COALESCE(correlation_id, ''), COALESCE(is_deleted, FALSE), COALESCE(denied_duplicate_ids, ''), created_at, synced_at
		FROM bank_transactions WHERE user_id = ? AND integration_id = ?`, userID, integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var pID sql.NullString
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, &pID, &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt)
		if err != nil {
			return nil, err
		}
		if pID.Valid {
			t.PoolID = &pID.String
		}
		if aID.Valid {
			t.AccountID = aID.String
		}
		if saID.Valid {
			t.SourceAccountID = saID.String
		}
		if daID.Valid {
			t.DestinationAccountID = daID.String
		}
		if tags.Valid {
			t.Tags = tags.String
		}
		if ltID.Valid {
			t.LinkedTransactionID = &ltID.String
		}
		txs = append(txs, t)
	}
	return txs, nil
}

func (r *TransactionRepository) LinkTransactions(userID string, txID1 string, txID2 string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE bank_transactions SET linked_transaction_id = ?, is_link_confirmed = TRUE WHERE id = ? AND user_id = ?", txID2, txID1, userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("UPDATE bank_transactions SET linked_transaction_id = ?, is_link_confirmed = TRUE WHERE id = ? AND user_id = ?", txID1, txID2, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *TransactionRepository) UnlinkTransaction(userID string, txID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var linkedID sql.NullString
	err = tx.QueryRow("SELECT linked_transaction_id FROM bank_transactions WHERE id = ? AND user_id = ?", txID, userID).Scan(&linkedID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	_, err = tx.Exec("UPDATE bank_transactions SET linked_transaction_id = NULL, is_link_confirmed = FALSE WHERE id = ? AND user_id = ?", txID, userID)
	if err != nil {
		return err
	}

	if linkedID.Valid {
		_, err = tx.Exec("UPDATE bank_transactions SET linked_transaction_id = NULL, is_link_confirmed = FALSE WHERE id = ? AND user_id = ?", linkedID.String, userID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *TransactionRepository) Update(userID string, txID string, accountID string, sourceAccountID string, destinationAccountID string, tags string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET account_id = ?, source_account_id = ?, destination_account_id = ?, tags = ? WHERE id = ? AND user_id = ?", accountID, sourceAccountID, destinationAccountID, tags, txID, userID)
	return err
}

func (r *TransactionRepository) UpdatePool(userID string, txID string, poolID *string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET pool_id = ? WHERE id = ? AND user_id = ?", poolID, txID, userID)
	return err
}

func (r *TransactionRepository) MigrateEmptyAccountIDs(integrationID string, accountID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET account_id = ? WHERE integration_id = ? AND (account_id IS NULL OR account_id = '')", accountID, integrationID)
	return err
}

func (r *TransactionRepository) Delete(userID string, id string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *TransactionRepository) DeleteByAccount(userID string, accountID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET is_deleted = TRUE WHERE user_id = ? AND (account_id = ? OR source_account_id = ? OR destination_account_id = ?)", userID, accountID, accountID, accountID)
	return err
}

func (r *TransactionRepository) UpdateIntegration(userID string, id string, integrationID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET integration_id = ? WHERE id = ? AND user_id = ?", integrationID, id, userID)
	return err
}

func (r *TransactionRepository) UpdateEncryptedData(userID string, id string, encryptedData string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET encrypted_data = ? WHERE id = ? AND user_id = ?", encryptedData, id, userID)
	return err
}

func (r *TransactionRepository) UpdateTimestampAndExternalID(userID string, id string, createdAt time.Time, externalID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET created_at = ?, external_id = ? WHERE id = ? AND user_id = ?", createdAt, externalID, id, userID)
	return err
}

func (r *TransactionRepository) UpdateIntegrationAndEncryptedData(userID string, id string, integrationID string, encryptedData string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET integration_id = ?, encrypted_data = ? WHERE id = ? AND user_id = ?", integrationID, encryptedData, id, userID)
	return err
}

func (r *TransactionRepository) GetByID(userID string, id string) (*domain.BankTransaction, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, integration_id, account_id, source_account_id, destination_account_id, pool_id, tags, external_id, encrypted_data, linked_transaction_id, is_link_confirmed, COALESCE(correlation_id, ''), COALESCE(is_deleted, FALSE), COALESCE(denied_duplicate_ids, ''), created_at, synced_at
		FROM bank_transactions WHERE user_id = ? AND id = ?`, userID, id)

	var t domain.BankTransaction
	var pID sql.NullString
	var aID, saID, daID sql.NullString
	var tags sql.NullString
	var ltID sql.NullString
	err := row.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, &pID, &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if pID.Valid {
		t.PoolID = &pID.String
	}
	if aID.Valid {
		t.AccountID = aID.String
	}
	if saID.Valid {
		t.SourceAccountID = saID.String
	}
	if daID.Valid {
		t.DestinationAccountID = daID.String
	}
	if tags.Valid {
		t.Tags = tags.String
	}
	if ltID.Valid {
		t.LinkedTransactionID = &ltID.String
	}
	return &t, nil
}

func (r *TransactionRepository) UpdateDeniedDuplicateIDs(userID string, id string, deniedIDs string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET denied_duplicate_ids = ? WHERE id = ? AND user_id = ?", deniedIDs, id, userID)
	return err
}

func (r *TransactionRepository) DeletePendingT212Transactions(userID string, integrationID string) error {
	_, err := r.db.Exec("DELETE FROM bank_transactions WHERE user_id = ? AND integration_id = ? AND external_id LIKE 'T212_PENDING_%'", userID, integrationID)
	return err
}
