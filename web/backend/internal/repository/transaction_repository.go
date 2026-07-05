package repository

import (
	"database/sql"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
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
		INSERT INTO bank_transactions (id, user_id, integration_id, account_id, source_account_id, destination_account_id, tags, external_id, encrypted_data, linked_transaction_id, is_link_confirmed, correlation_id, is_deleted, created_at, synced_at, internal_status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, external_id) DO UPDATE SET
            integration_id = excluded.integration_id,
            account_id = excluded.account_id,
            encrypted_data = excluded.encrypted_data,
            correlation_id = excluded.correlation_id,
            created_at = excluded.created_at,
            synced_at = excluded.synced_at,
            internal_status = excluded.internal_status
		RETURNING id`)
	defer stmt.Close()

	poolStmt, _ := tx.Prepare(`INSERT INTO bank_transaction_pools (transaction_id, pool_id) VALUES (?, ?) ON CONFLICT DO NOTHING`)
	defer poolStmt.Close()

	for _, t := range txs {
		var actualID string
		err = stmt.QueryRow(t.ID, userID, t.IntegrationID, t.AccountID, t.SourceAccountID, t.DestinationAccountID, t.Tags, t.ExternalID, t.EncryptedData, t.LinkedTransactionID, t.IsLinkConfirmed, t.CorrelationID, t.IsDeleted, t.CreatedAt, t.SyncedAt, t.InternalStatus).Scan(&actualID)
		if err != nil {
			return err
		}

		if len(t.PoolIDs) > 0 {
			for _, pID := range t.PoolIDs {
				if pID != "" {
					_, err = poolStmt.Exec(actualID, pID)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return tx.Commit()
}

func (r *TransactionRepository) List(userID string) ([]domain.BankTransaction, error) {
	return r.ListWithFilters(userID, "", "", "")
}

func (r *TransactionRepository) ListWithFilters(userID string, poolID string, startDate string, endDate string) ([]domain.BankTransaction, error) {
	query := `
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND COALESCE(t.is_deleted, FALSE) = FALSE`
	args := []interface{}{userID}

	if poolID != "" {
		query += " AND t.id IN (SELECT transaction_id FROM bank_transaction_pools WHERE pool_id = ?)"
		args = append(args, poolID)
	}
	if startDate != "" {
		query += " AND t.created_at >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		query += " AND t.created_at <= ?"
		args = append(args, endDate)
	}

	query += " GROUP BY t.id ORDER BY t.created_at DESC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) ListByCorrelationID(userID string, correlationID string) ([]domain.BankTransaction, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id,
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed,
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''),
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND t.correlation_id = ?
        GROUP BY t.id`, userID, correlationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) GetCorrelationIDMapping(userID string) (map[string]struct{ IntegrationID, AccountID string }, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT correlation_id, integration_id, account_id 
		FROM bank_transactions 
		WHERE user_id = ? AND correlation_id != ''`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mapping := make(map[string]struct{ IntegrationID, AccountID string })
	for rows.Next() {
		var cid, iid, aid string
		if err := rows.Scan(&cid, &iid, &aid); err == nil {
			mapping[cid] = struct{ IntegrationID, AccountID string }{iid, aid}
		}
	}
	return mapping, nil
}

func (r *TransactionRepository) WipeTransactionsByServiceType(userID string, serviceTypes []string) error {
	_, err := r.db.Exec(`
		DELETE FROM bank_transactions 
		WHERE user_id = ? AND integration_id IN (
			SELECT id FROM integrations WHERE service_type = ANY(?)
		)`, userID, pq.Array(serviceTypes))
	return err
}

func (r *TransactionRepository) ListByIntegration(userID string, integrationID string) ([]domain.BankTransaction, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND t.integration_id = ?
        GROUP BY t.id`, userID, integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) LinkInternalTransfer(userID string, txID1 string, txID2 string, sourceAcc string, destAcc string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE bank_transactions 
		SET linked_transaction_id = ?, is_link_confirmed = TRUE, source_account_id = ?, destination_account_id = ? 
		WHERE id = ? AND user_id = ?`, txID2, sourceAcc, destAcc, txID1, userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE bank_transactions 
		SET linked_transaction_id = ?, is_link_confirmed = TRUE, source_account_id = ?, destination_account_id = ? 
		WHERE id = ? AND user_id = ?`, txID1, sourceAcc, destAcc, txID2, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *TransactionRepository) Update(userID string, txID string, accountID string, sourceAccountID string, destinationAccountID string, tags string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET account_id = ?, source_account_id = ?, destination_account_id = ?, tags = ? WHERE id = ? AND user_id = ?", accountID, sourceAccountID, destinationAccountID, tags, txID, userID)
	return err
}

func (r *TransactionRepository) UpdatePools(userID string, txID string, poolIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete existing associations
	_, err = tx.Exec("DELETE FROM bank_transaction_pools WHERE transaction_id = ?", txID)
	if err != nil {
		return err
	}

	// 2. Insert new associations
	if len(poolIDs) > 0 {
		stmt, err := tx.Prepare("INSERT INTO bank_transaction_pools (transaction_id, pool_id) VALUES (?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, pID := range poolIDs {
			if pID != "" {
				_, err = stmt.Exec(txID, pID)
				if err != nil {
					return err
				}
			}
		}
	}

	return tx.Commit()
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
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND t.id = ?
        GROUP BY t.id`, userID, id)

	var t domain.BankTransaction
	var aID, saID, daID sql.NullString
	var tags sql.NullString
	var ltID sql.NullString
	err := row.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
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

func (r *TransactionRepository) ListAll(userID string) ([]domain.BankTransaction, error) {
	query := `
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ?
        GROUP BY t.id 
        ORDER BY t.created_at DESC`
	
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) HardDelete(userID string, id string) error {
	_, err := r.db.Exec("DELETE FROM bank_transactions WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *TransactionRepository) UpdateExternalID(userID string, id string, externalID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET external_id = ? WHERE id = ? AND user_id = ?", externalID, id, userID)
	return err
}

func (r *TransactionRepository) UpdateSourceAccountID(userID string, id string, sourceAccountID string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET source_account_id = ? WHERE id = ? AND user_id = ?", sourceAccountID, id, userID)
	return err
}

func (r *TransactionRepository) UpdateInternalStatus(userID string, id string, status string) error {
	_, err := r.db.Exec("UPDATE bank_transactions SET internal_status = ? WHERE id = ? AND user_id = ?", status, id, userID)
	return err
}

func (r *TransactionRepository) ListByInternalStatus(userID string, status string) ([]domain.BankTransaction, error) {
	query := `
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND t.internal_status = ?
        GROUP BY t.id 
        ORDER BY t.created_at DESC`
	
	rows, err := r.db.Query(query, userID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) ListByAccount(userID string, accountID string) ([]domain.BankTransaction, error) {
	query := `
		SELECT t.id, t.user_id, t.integration_id, t.account_id, t.source_account_id, t.destination_account_id, 
               array_remove(array_agg(tp.pool_id), NULL) as pool_ids,
               t.tags, t.external_id, t.encrypted_data, t.linked_transaction_id, t.is_link_confirmed, 
               COALESCE(t.correlation_id, ''), COALESCE(t.is_deleted, FALSE), COALESCE(t.denied_duplicate_ids, ''), 
               t.created_at, t.synced_at, COALESCE(t.internal_status, '')
		FROM bank_transactions t
        LEFT JOIN bank_transaction_pools tp ON t.id = tp.transaction_id
        WHERE t.user_id = ? AND (t.account_id = ? OR t.source_account_id = ? OR t.destination_account_id = ?) AND COALESCE(t.is_deleted, FALSE) = FALSE
        GROUP BY t.id 
        ORDER BY t.created_at DESC`
	
	rows, err := r.db.Query(query, userID, accountID, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txs := []domain.BankTransaction{}
	for rows.Next() {
		var t domain.BankTransaction
		var aID, saID, daID sql.NullString
		var tags sql.NullString
		var ltID sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.IntegrationID, &aID, &saID, &daID, pq.Array(&t.PoolIDs), &tags, &t.ExternalID, &t.EncryptedData, &ltID, &t.IsLinkConfirmed, &t.CorrelationID, &t.IsDeleted, &t.DeniedDuplicateIDs, &t.CreatedAt, &t.SyncedAt, &t.InternalStatus)
		if err != nil {
			return nil, err
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

func (r *TransactionRepository) SaveAccountBalanceHistory(userID string, integrationID string, accountID string, balance float64, recordedAt time.Time) error {
	id := uuid.New().String()
	_, err := r.db.Exec(`
		INSERT INTO account_balance_history (id, user_id, integration_id, account_id, balance, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, integrationID, accountID, balance, recordedAt)
	return err
}

type AccountBalanceHistory struct {
	ID            string
	UserID        string
	IntegrationID string
	AccountID     string
	Balance       float64
	RecordedAt    time.Time
}

func (r *TransactionRepository) ListAccountBalanceHistory(userID string, accountID string) ([]AccountBalanceHistory, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, integration_id, account_id, balance, recorded_at
		FROM account_balance_history
		WHERE user_id = ? AND account_id = ?
		ORDER BY recorded_at ASC`, userID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []AccountBalanceHistory
	for rows.Next() {
		var h AccountBalanceHistory
		err := rows.Scan(&h.ID, &h.UserID, &h.IntegrationID, &h.AccountID, &h.Balance, &h.RecordedAt)
		if err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, nil
}


