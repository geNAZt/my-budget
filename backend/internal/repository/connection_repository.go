package repository

import (
	"database/sql"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/google/uuid"
)

type ConnectionRepository struct {
	db *sql.DB
}

func NewConnectionRepository(db *sql.DB) *ConnectionRepository {
	return &ConnectionRepository{db: db}
}

func (r *ConnectionRepository) List(userID string) ([]domain.Connection, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, name, created_at, updated_at
		FROM connections
		WHERE user_id = ?
		ORDER BY name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []domain.Connection
	for rows.Next() {
		var c domain.Connection
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	if connections == nil {
		connections = []domain.Connection{}
	}
	return connections, nil
}

func (r *ConnectionRepository) GetByID(userID string, id string) (*domain.Connection, error) {
	var c domain.Connection
	err := r.db.QueryRow(`
		SELECT id, user_id, name, value, created_at, updated_at
		FROM connections
		WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&c.ID, &c.UserID, &c.Name, &c.Value, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConnectionRepository) Save(userID string, conn *domain.Connection) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	conn.UpdatedAt = time.Now()

	var exists bool
	if conn.ID != "" {
		err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM connections WHERE user_id = ? AND id = ?)", userID, conn.ID).Scan(&exists)
		if err != nil {
			return err
		}
	}

	if !exists {
		if conn.ID == "" {
			conn.ID = uuid.New().String()
		}
		conn.CreatedAt = time.Now()
		_, err = tx.Exec(`
			INSERT INTO connections (id, user_id, name, value, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			conn.ID, userID, conn.Name, conn.Value, conn.CreatedAt, conn.UpdatedAt)
	} else {
		_, err = tx.Exec(`
			UPDATE connections
			SET name = ?, value = ?, updated_at = ?
			WHERE user_id = ? AND id = ?`,
			conn.Name, conn.Value, conn.UpdatedAt, userID, conn.ID)
	}
	if err != nil {
		return err
	}

	// Save Key Slots (if provided during creation)
	if len(conn.KeySlots) > 0 {
		for authIDStr, encKey := range conn.KeySlots {
			_, err = tx.Exec(`
				INSERT INTO connection_key_slots (connection_id, authenticator_id, encrypted_key)
				VALUES (?, ?, ?)
				ON CONFLICT(connection_id, authenticator_id) DO UPDATE SET encrypted_key = excluded.encrypted_key`,
				conn.ID, []byte(authIDStr), encKey)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (r *ConnectionRepository) Delete(userID string, id string) error {
	_, err := r.db.Exec("DELETE FROM connections WHERE user_id = ? AND id = ?", userID, id)
	return err
}

func (r *ConnectionRepository) GetKeySlot(connectionID string, authID []byte) (string, error) {
	var encryptedKey string
	err := r.db.QueryRow(`
		SELECT encrypted_key
		FROM connection_key_slots
		WHERE connection_id = ? AND authenticator_id = ?`,
		connectionID, authID).Scan(&encryptedKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return encryptedKey, err
}

func (r *ConnectionRepository) UpdateValue(userID string, id string, value string) error {
	_, err := r.db.Exec("UPDATE connections SET value = ? WHERE id = ? AND user_id = ?", value, id, userID)
	return err
}
