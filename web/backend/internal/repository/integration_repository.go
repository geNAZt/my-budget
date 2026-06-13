package repository

import (
	"database/sql"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

type IntegrationRepository struct {
	db *sql.DB
}

func NewIntegrationRepository(db *sql.DB) *IntegrationRepository {
	return &IntegrationRepository{db: db}
}

func (r *IntegrationRepository) Save(userID string, i *domain.Integration) error {
	i.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`
		INSERT INTO integrations (id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, updated_at, backoff_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            encrypted_config = excluded.encrypted_config,
            status = excluded.status,
            sync_interval_seconds = excluded.sync_interval_seconds,
            last_sync_at = excluded.last_sync_at,
            last_error = excluded.last_error,
            cached_balance = excluded.cached_balance,
            updated_at = excluded.updated_at,
            backoff_until = excluded.backoff_until`,
		i.ID, userID, i.ServiceType, i.Name, i.EncryptedConfig, i.Status, i.SyncIntervalSeconds, i.LastSyncAt, i.LastError, i.CachedBalance, i.UpdatedAt, i.BackoffUntil)
	return err
}

func (r *IntegrationRepository) List(userID string) ([]domain.Integration, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []domain.Integration{}
	for rows.Next() {
		var i domain.Integration
		var lastSyncAt, backoffUntil sql.NullTime
		var name, status, lastError sql.NullString
		err := rows.Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)
		if err != nil {
			return nil, err
		}
		if lastSyncAt.Valid {
			i.LastSyncAt = &lastSyncAt.Time
		}
		if backoffUntil.Valid {
			i.BackoffUntil = &backoffUntil.Time
		}
		i.Name = name.String
		i.Status = status.String
		i.LastError = lastError.String
		result = append(result, i)
	}
	return result, nil
}

func (r *IntegrationRepository) ListAll() ([]domain.Integration, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []domain.Integration{}
	for rows.Next() {
		var i domain.Integration
		var lastSyncAt, backoffUntil sql.NullTime
		var name, status, lastError sql.NullString
		err := rows.Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)
		if err != nil {
			return nil, err
		}
		if lastSyncAt.Valid {
			i.LastSyncAt = &lastSyncAt.Time
		}
		if backoffUntil.Valid {
			i.BackoffUntil = &backoffUntil.Time
		}
		i.Name = name.String
		i.Status = status.String
		i.LastError = lastError.String
		result = append(result, i)
	}
	return result, nil
}

func (r *IntegrationRepository) ResetAllErrors() error {
	_, err := r.db.Exec(`UPDATE integrations SET status = 'ACTIVE', last_error = '' WHERE status = 'ERROR'`)
	return err
}

func (r *IntegrationRepository) GetByIDGlobal(id string) (*domain.Integration, error) {
	var i domain.Integration
	var lastSyncAt, backoffUntil sql.NullTime
	var name, status, lastError sql.NullString
	err := r.db.QueryRow(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations WHERE id = ?`,
		id).Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastSyncAt.Valid {
		i.LastSyncAt = &lastSyncAt.Time
	}
	if backoffUntil.Valid {
		i.BackoffUntil = &backoffUntil.Time
	}
	i.Name = name.String
	i.Status = status.String
	i.LastError = lastError.String
	return &i, nil
}

func (r *IntegrationRepository) GetByID(userID string, id string) (*domain.Integration, error) {
	var i domain.Integration
	var lastSyncAt, backoffUntil sql.NullTime
	var name, status, lastError sql.NullString
	err := r.db.QueryRow(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations WHERE user_id = ? AND id = ?`,
		userID, id).Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastSyncAt.Valid {
		i.LastSyncAt = &lastSyncAt.Time
	}
	if backoffUntil.Valid {
		i.BackoffUntil = &backoffUntil.Time
	}
	i.Name = name.String
	i.Status = status.String
	i.LastError = lastError.String
	return &i, nil
}

func (r *IntegrationRepository) Get(userID string, serviceType string) (*domain.Integration, error) {
	var i domain.Integration
	var lastSyncAt, backoffUntil sql.NullTime
	var name, status, lastError sql.NullString
	err := r.db.QueryRow(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations WHERE user_id = ? AND service_type = ? LIMIT 1`,
		userID, serviceType).Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastSyncAt.Valid {
		i.LastSyncAt = &lastSyncAt.Time
	}
	if backoffUntil.Valid {
		i.BackoffUntil = &backoffUntil.Time
	}
	i.Name = name.String
	i.Status = status.String
	i.LastError = lastError.String
	return &i, nil
}

func (r *IntegrationRepository) ListAllActive() ([]domain.Integration, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, service_type, name, encrypted_config, status, sync_interval_seconds, last_sync_at, last_error, cached_balance, created_at, updated_at, backoff_until
		FROM integrations WHERE status != 'ERROR'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []domain.Integration{}
	for rows.Next() {
		var i domain.Integration
		var lastSyncAt, backoffUntil sql.NullTime
		var name, status, lastError sql.NullString
		err := rows.Scan(&i.ID, &i.UserID, &i.ServiceType, &name, &i.EncryptedConfig, &status, &i.SyncIntervalSeconds, &lastSyncAt, &lastError, &i.CachedBalance, &i.CreatedAt, &i.UpdatedAt, &backoffUntil)
		if err != nil {
			return nil, err
		}
		if lastSyncAt.Valid {
			i.LastSyncAt = &lastSyncAt.Time
		}
		if backoffUntil.Valid {
			i.BackoffUntil = &backoffUntil.Time
		}
		i.Name = name.String
		i.Status = status.String
		i.LastError = lastError.String
		result = append(result, i)
	}
	return result, nil
}


func (r *IntegrationRepository) SaveKeySlot(integrationID string, authID []byte, encryptedKey string) error {
	_, err := r.db.Exec(`
		INSERT INTO integration_key_slots (integration_id, authenticator_id, encrypted_key)
		VALUES (?, ?, ?)
		ON CONFLICT(integration_id, authenticator_id) DO UPDATE SET encrypted_key = excluded.encrypted_key`,
		integrationID, authID, encryptedKey)
	return err
}

func (r *IntegrationRepository) GetKeySlot(integrationID string, authID []byte) (string, error) {
	var encryptedKey string
	err := r.db.QueryRow("SELECT encrypted_key FROM integration_key_slots WHERE integration_id = ? AND authenticator_id = ?",
		integrationID, authID).Scan(&encryptedKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return encryptedKey, err
}

func (r *IntegrationRepository) ListAllSlots(integrationID string) (map[string]string, error) {
	rows, err := r.db.Query("SELECT authenticator_id, encrypted_key FROM integration_key_slots WHERE integration_id = ?", integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	slots := make(map[string]string)
	for rows.Next() {
		var authID []byte
		var key string
		if err := rows.Scan(&authID, &key); err != nil {
			return nil, err
		}
		slots[string(authID)] = key
	}
	return slots, nil
}

func (r *IntegrationRepository) UpdateEncryptedConfig(userID string, id string, encryptedConfig string) error {
	_, err := r.db.Exec("UPDATE integrations SET encrypted_config = ? WHERE id = ? AND user_id = ?", encryptedConfig, id, userID)
	return err
}

func (r *IntegrationRepository) Delete(userID string, id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM integration_key_slots WHERE integration_id = ?", id)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM bank_transactions WHERE integration_id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}

	// Clean up references to this integration inside transaction rules to avoid orphaned IDs
	_, err = tx.Exec("UPDATE transaction_rules SET integration_id = NULL WHERE integration_id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM integrations WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
