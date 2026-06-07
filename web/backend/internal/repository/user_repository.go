package repository

import (
	"database/sql"
	"encoding/base64"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/db"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/go-webauthn/webauthn/webauthn"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetUser(username string) (*domain.User, error) {
	user := &domain.User{
		Username:               username,
		AuthenticatorNames:     make(map[string]string),
		AuthenticatorCreatedAt: make(map[string]time.Time),
	}
	var scenarioID sql.NullString
	var recoveryHash sql.NullString

	err := r.db.QueryRow("SELECT id, dashboard_scenario_id, dashboard_month_offset, recovery_hash FROM users WHERE username = ?", username).
		Scan(&user.ID, &scenarioID, &user.DashboardMonthOffset, &recoveryHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if scenarioID.Valid {
		user.DashboardScenarioID = scenarioID.String
	}
	user.RecoveryHash = recoveryHash.String

	// Fetch credentials
	rows, err := r.db.Query("SELECT id, credential_json AS credential_msgpack, name, created_at FROM authenticators WHERE user_id = ? ORDER BY id ASC", user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var credID []byte
		var msgpackStr sql.NullString
		var name sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&credID, &msgpackStr, &name, &createdAt); err != nil {
			return nil, err
		}

		if !msgpackStr.Valid {
			continue
		}

		var cred webauthn.Credential
		if err := db.Unmarshal(msgpackStr.String, &cred); err != nil {
			return nil, err
		}

		// Self-healing: if the stored data was JSON, migrate it to MsgPack
		if len(msgpackStr.String) > 0 && (msgpackStr.String[0] == '{' || msgpackStr.String[0] == '[') {
			r.UpdateCredential(user.ID, &cred)
		}

		cred.ID = credID
		user.Authenticators = append(user.Authenticators, cred)
		idB64 := base64.StdEncoding.EncodeToString(credID)
		user.AuthenticatorNames[idB64] = name.String
		user.AuthenticatorCreatedAt[idB64] = createdAt
	}

	return user, nil
}

func (r *UserRepository) GetUserByID(id string) (*domain.User, error) {
	user := domain.User{
		AuthenticatorNames:     make(map[string]string),
		AuthenticatorCreatedAt: make(map[string]time.Time),
	}
	var scenarioID sql.NullString
	var recoveryHash sql.NullString
	var timezone sql.NullString

	err := r.db.QueryRow("SELECT id, username, dashboard_scenario_id, dashboard_month_offset, recovery_hash, timezone FROM users WHERE id = ?", id).
		Scan(&user.ID, &user.Username, &scenarioID, &user.DashboardMonthOffset, &recoveryHash, &timezone)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if scenarioID.Valid {
		user.DashboardScenarioID = scenarioID.String
	}
	user.RecoveryHash = recoveryHash.String
	if timezone.Valid && timezone.String != "" {
		user.Timezone = timezone.String
	} else {
		user.Timezone = "UTC"
	}

	// Fetch credentials
	rows, err := r.db.Query("SELECT id, credential_json AS credential_msgpack, name, created_at FROM authenticators WHERE user_id = ? ORDER BY id ASC", user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var credID []byte
		var msgpackStr sql.NullString
		var name sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&credID, &msgpackStr, &name, &createdAt); err != nil {
			return nil, err
		}

		if !msgpackStr.Valid {
			continue
		}

		var cred webauthn.Credential
		if err := db.Unmarshal(msgpackStr.String, &cred); err != nil {
			return nil, err
		}

		// Self-healing: if the stored data was JSON, migrate it to MsgPack
		if len(msgpackStr.String) > 0 && (msgpackStr.String[0] == '{' || msgpackStr.String[0] == '[') {
			r.UpdateCredential(user.ID, &cred)
		}

		cred.ID = credID
		user.Authenticators = append(user.Authenticators, cred)
		idB64 := base64.StdEncoding.EncodeToString(credID)
		user.AuthenticatorNames[idB64] = name.String
		user.AuthenticatorCreatedAt[idB64] = createdAt
	}

	return &user, nil
}

func (r *UserRepository) CreateUser(user *domain.User) error {
	_, err := r.db.Exec("INSERT INTO users (id, username, recovery_hash) VALUES (?, ?, ?)", user.ID, user.Username, user.RecoveryHash)
	return err
}

func (r *UserRepository) UpdateRecoveryHash(userID string, hash string) error {
	_, err := r.db.Exec("UPDATE users SET recovery_hash = ? WHERE id = ?", hash, userID)
	return err
}

func (r *UserRepository) UpdateDashboardConfig(userID string, scenarioID string, monthOffset int) error {
	_, err := r.db.Exec("UPDATE users SET dashboard_scenario_id = ?, dashboard_month_offset = ? WHERE id = ?", scenarioID, monthOffset, userID)
	return err
}

func (r *UserRepository) ListAll() ([]domain.User, error) {
	rows, err := r.db.Query("SELECT id, username, recovery_hash, dashboard_scenario_id, dashboard_month_offset, timezone FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []domain.User{}
	for rows.Next() {
		var u domain.User
		var recoveryHash sql.NullString
		var scenarioID sql.NullString
		var timezone sql.NullString
		err := rows.Scan(&u.ID, &u.Username, &recoveryHash, &scenarioID, &u.DashboardMonthOffset, &timezone)
		if err != nil {
			return nil, err
		}
		u.RecoveryHash = recoveryHash.String
		if scenarioID.Valid {
			u.DashboardScenarioID = scenarioID.String
		}
		if timezone.Valid && timezone.String != "" {
			u.Timezone = timezone.String
		} else {
			u.Timezone = "UTC"
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *UserRepository) AddCredential(userID string, cred *webauthn.Credential, name string) error {
	msgpackBytes, err := db.Marshal(cred)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		INSERT INTO authenticators (id, user_id, credential_json, name)
		VALUES (?, ?, ?, ?)`,
		cred.ID, userID, msgpackBytes, name)
	return err
}

func (r *UserRepository) UpdateCredential(userID string, cred *webauthn.Credential) error {
	msgpackBytes, err := db.Marshal(cred)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
        UPDATE authenticators SET credential_json = ? WHERE id = ? AND user_id = ?`,
		msgpackBytes, cred.ID, userID)
	return err
}

func (r *UserRepository) DeleteAuthenticator(userID string, credID []byte) error {
	_, err := r.db.Exec("DELETE FROM authenticators WHERE id = ? AND user_id = ?", credID, userID)
	return err
}

func (r *UserRepository) UpdateTimezone(userID string, timezone string) error {
	_, err := r.db.Exec("UPDATE users SET timezone = ? WHERE id = ?", timezone, userID)
	return err
}

func (r *UserRepository) RenameAuthenticator(userID string, credID []byte, name string) error {
	_, err := r.db.Exec("UPDATE authenticators SET name = ? WHERE id = ? AND user_id = ?", name, credID, userID)
	return err
}
