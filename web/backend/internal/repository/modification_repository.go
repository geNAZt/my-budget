package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type ModificationRepository struct {
	db *sql.DB
}

func NewModificationRepository(db *sql.DB) *ModificationRepository {
	return &ModificationRepository{db: db}
}

func (r *ModificationRepository) List(userID string) ([]domain.Modification, error) {
	query := `
		SELECT m.id, m.target_id, m.target_type, m.description, m.created_at, v.id, v.amount, v.withdrawal_percentage, v.start_date, v.end_date, v.interval_months
		FROM modifications m
		INNER JOIN modification_versions v ON m.id = v.modification_id
		WHERE m.user_id = ? AND m.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM modification_versions WHERE modification_id = m.id
		)
		ORDER BY m.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []domain.Modification
	for rows.Next() {
		var m domain.Modification
		var v domain.ModificationVersion
		var endDate sql.NullTime
		var targetID sql.NullString

		err := rows.Scan(&m.ID, &targetID, &m.TargetType, &m.Description, &m.CreatedAt, &v.ID, &v.Amount, &v.WithdrawalPercentage, &v.StartDate, &endDate, &v.IntervalMonths)
		if err != nil {
			return nil, err
		}

		if targetID.Valid {
			m.TargetID = targetID.String
		}
		if endDate.Valid {
			v.EndDate = &endDate.Time
		}
		v.ModificationID = m.ID
		m.ActiveVersion = &v
		m.UserID = userID

		// Fetch TargetIDs for ASSET type
		if m.TargetType == "ASSET" {
			targetRows, err := r.db.Query("SELECT asset_id FROM modification_assets WHERE modification_id = ?", m.ID)
			if err != nil {
				return nil, err
			}
			for targetRows.Next() {
				var assetID string
				if err := targetRows.Scan(&assetID); err != nil {
					targetRows.Close()
					return nil, err
				}
				m.TargetIDs = append(m.TargetIDs, assetID)
			}
			targetRows.Close()
		}

		mods = append(mods, m)
	}

	return mods, nil
}

func (r *ModificationRepository) Save(userID string, mod *domain.Modification) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if mod.ID == "" {
		mod.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", mod.ID, userID, mod.TargetID, mod.TargetType, mod.Description)
	} else {
		_, err = tx.Exec("UPDATE modifications SET description = ?, target_id = ?, target_type = ? WHERE id = ? AND user_id = ?", mod.Description, mod.TargetID, mod.TargetType, mod.ID, userID)
	}
	if err != nil {
		return err
	}

	// Update modification_assets for ASSET type
	if mod.TargetType == "ASSET" {
		_, err = tx.Exec("DELETE FROM modification_assets WHERE modification_id = ?", mod.ID)
		if err != nil {
			return err
		}
		for _, assetID := range mod.TargetIDs {
			_, err = tx.Exec("INSERT INTO modification_assets (modification_id, asset_id) VALUES (?, ?)", mod.ID, assetID)
			if err != nil {
				return err
			}
		}
	}

	v := mod.ActiveVersion
	v.ID = uuid.New().String()
	v.ModificationID = mod.ID

	_, err = tx.Exec(`
		INSERT INTO modification_versions (id, modification_id, amount, withdrawal_percentage, start_date, end_date, interval_months)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.ModificationID, v.Amount, v.WithdrawalPercentage, v.StartDate, v.EndDate, v.IntervalMonths)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *ModificationRepository) SaveBulk(userID string, mods []domain.Modification) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i := range mods {
		mod := &mods[i]
		if mod.ID == "" {
			mod.ID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", mod.ID, userID, mod.TargetID, mod.TargetType, mod.Description)
		} else {
			_, err = tx.Exec("UPDATE modifications SET description = ?, target_id = ?, target_type = ? WHERE id = ? AND user_id = ?", mod.Description, mod.TargetID, mod.TargetType, mod.ID, userID)
		}
		if err != nil {
			return err
		}

		// Update modification_assets for ASSET type
		if mod.TargetType == "ASSET" {
			_, err = tx.Exec("DELETE FROM modification_assets WHERE modification_id = ?", mod.ID)
			if err != nil {
				return err
			}
			for _, assetID := range mod.TargetIDs {
				_, err = tx.Exec("INSERT INTO modification_assets (modification_id, asset_id) VALUES (?, ?)", mod.ID, assetID)
				if err != nil {
					return err
				}
			}
		}

		v := mod.ActiveVersion
		v.ID = uuid.New().String()
		v.ModificationID = mod.ID
		_, err = tx.Exec(`
			INSERT INTO modification_versions (id, modification_id, amount, withdrawal_percentage, start_date, end_date, interval_months)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			v.ID, v.ModificationID, v.Amount, v.WithdrawalPercentage, v.StartDate, v.EndDate, v.IntervalMonths)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ModificationRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE modifications SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *ModificationRepository) RevertLatest(userID string, modID string) error {
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM modifications WHERE id = ? AND user_id = ?", modID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		DELETE FROM modification_versions
		WHERE modification_id = ?
		AND created_at = (SELECT MAX(created_at) FROM modification_versions WHERE modification_id = ?)`,
		modID, modID)
	if err != nil {
		return err
	}

	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM modification_versions WHERE modification_id = ?", modID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE modifications SET is_deleted = TRUE WHERE id = ?", modID)
		return err
	}

	return nil
}
