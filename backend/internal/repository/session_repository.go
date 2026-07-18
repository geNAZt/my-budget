package repository

import (
	"database/sql"
	"log"

	"github.com/genazt/my-budget-script/backend/internal/db"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/go-webauthn/webauthn/webauthn"
)

type AuthSession struct {
	WebAuthnSession *webauthn.SessionData
	User            *domain.User
}

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) SaveSession(username string, session *AuthSession) error {
	data, err := db.Marshal(session)
	if err != nil {
		return err
	}

	// Use username as the primary ID for the session lookup during Finish
	_, err = r.db.Exec(`
		INSERT INTO webauthn_sessions (id, user_id, session_data)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET session_data = excluded.session_data, created_at = CURRENT_TIMESTAMP`,
		username, session.User.ID, data)
	if err != nil {
		log.Printf("[DB] Failed to save webauthn session: %v", err)
	}
	return err
}

func (r *SessionRepository) GetSession(username string) (*AuthSession, error) {
	var data string
	err := r.db.QueryRow("SELECT session_data FROM webauthn_sessions WHERE id = ?", username).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var session AuthSession
	if err := db.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (r *SessionRepository) DeleteSession(username string) error {
	_, err := r.db.Exec("DELETE FROM webauthn_sessions WHERE id = ?", username)
	return err
}
