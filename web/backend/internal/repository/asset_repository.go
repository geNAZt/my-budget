package repository

import (
	"database/sql"
	"log"

	"github.com/genazt/my-budget-script/web/backend/internal/db"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type AssetRepository struct {
	db *sql.DB
}

func NewAssetRepository(db *sql.DB) *AssetRepository {
	return &AssetRepository{db: db}
}

func (r *AssetRepository) List(userID string) ([]domain.Asset, error) {
	query := `
		SELECT a.id, a.name, a.pool_id, a.created_at, v.id, v.type, v.target_value, v.dumping_loan_id, v.stop_modification_id, v.interest_rate, v.interest_interval, v.amount_per_month, v.remainder_start_date, v.start_date, v.end_date, v.withdrawal_penalty, v.etf_config_json AS etf_config_msgpack, v.penalties_json AS penalties_msgpack, v.sub_assets_json AS sub_assets_msgpack, v.created_at
		FROM assets a
		INNER JOIN asset_versions v ON a.id = v.asset_id
		WHERE a.user_id = ? AND a.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM asset_versions WHERE asset_id = a.id
		)
		ORDER BY a.created_at DESC`

	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load virtual account mappings for assets
	vaMap := make(map[string][]string)
	vaQuery := `
		SELECT entity_id, virtual_account_id
		FROM entity_virtual_accounts
		WHERE entity_type = 'ASSET'`
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

	assets := []domain.Asset{}
	for rows.Next() {
		var a domain.Asset
		var v domain.AssetVersion
		var endDate sql.NullTime
		var remainderStartDate sql.NullTime
		var etfConfigMsgPack sql.NullString
		var penaltiesMsgPack sql.NullString
		var subAssetsMsgPack sql.NullString
		var dumpingLoanID sql.NullString
		var stopModID sql.NullString
		var withdrawalPenaltyDummy float64
		var poolID sql.NullString

		err := rows.Scan(&a.ID, &a.Name, &poolID, &a.CreatedAt, &v.ID, &v.Type, &v.TargetValue, &dumpingLoanID, &stopModID, &v.InterestRate, &v.InterestInterval, &v.AmountPerMonth, &remainderStartDate, &v.StartDate, &endDate, &withdrawalPenaltyDummy, &etfConfigMsgPack, &penaltiesMsgPack, &subAssetsMsgPack, &v.CreatedAt)
		if err != nil {
			return nil, err
		}

		log.Printf("[ASSET_REPO] Scanned asset %s version %s. CreatedAt: %s, EndDate: %s, ETFConfig: %s, Penalties: %s, SubAssets: %s", a.ID, v.ID, v.CreatedAt, v.EndDate, etfConfigMsgPack.String, penaltiesMsgPack.String, subAssetsMsgPack.String)

		if poolID.Valid {
			a.PoolID = &poolID.String
		}
		if endDate.Valid {
			v.EndDate = &endDate.Time
		}
		if remainderStartDate.Valid {
			v.RemainderStartDate = &remainderStartDate.Time
		}
		if dumpingLoanID.Valid {
			v.DumpingLoanID = &dumpingLoanID.String
		}
		if stopModID.Valid {
			v.StopModificationID = &stopModID.String
		}
		if etfConfigMsgPack.Valid && etfConfigMsgPack.String != "" {
			db.Unmarshal(etfConfigMsgPack.String, &v.ETFConfig)
		}
		if penaltiesMsgPack.Valid && penaltiesMsgPack.String != "" {
			db.Unmarshal(penaltiesMsgPack.String, &v.Penalties)
		}
		if v.Penalties == nil {
			v.Penalties = []domain.AssetPenalty{}
		}
		if subAssetsMsgPack.Valid && subAssetsMsgPack.String != "" {
			db.Unmarshal(subAssetsMsgPack.String, &v.SubAssets)
		}
		if v.SubAssets == nil {
			v.SubAssets = []domain.SubAsset{}
		}
		v.AssetID = a.ID
		a.ActiveVersion = &v
		a.UserID = userID

		// Map the multi-assigned accounts
		if val, exists := vaMap[a.ID]; exists {
			a.AccountIDs = val
		} else {
			a.AccountIDs = []string{}
		}

		assets = append(assets, a)
	}

	return assets, nil
}

func (r *AssetRepository) GetByID(userID string, id string) (*domain.Asset, error) {
	query := `
		SELECT a.id, a.name, a.pool_id, a.created_at, v.id, v.type, v.target_value, v.dumping_loan_id, v.stop_modification_id, v.interest_rate, v.interest_interval, v.amount_per_month, v.remainder_start_date, v.start_date, v.end_date, v.withdrawal_penalty, v.etf_config_json AS etf_config_msgpack, v.penalties_json AS penalties_msgpack, v.sub_assets_json AS sub_assets_msgpack, v.created_at
		FROM assets a
		INNER JOIN asset_versions v ON a.id = v.asset_id
		WHERE a.user_id = ? AND a.id = ? AND a.is_deleted = FALSE
		AND v.created_at = (
			SELECT MAX(created_at) FROM asset_versions WHERE asset_id = a.id
		) LIMIT 1`

	var a domain.Asset
	var v domain.AssetVersion
	var endDate sql.NullTime
	var remainderStartDate sql.NullTime
	var etfConfigMsgPack sql.NullString
	var penaltiesMsgPack sql.NullString
	var subAssetsMsgPack sql.NullString
	var dumpingLoanID sql.NullString
	var stopModID sql.NullString
	var withdrawalPenaltyDummy float64
	var poolID sql.NullString

	err := r.db.QueryRow(query, userID, id).Scan(&a.ID, &a.Name, &poolID, &a.CreatedAt, &v.ID, &v.Type, &v.TargetValue, &dumpingLoanID, &stopModID, &v.InterestRate, &v.InterestInterval, &v.AmountPerMonth, &remainderStartDate, &v.StartDate, &endDate, &withdrawalPenaltyDummy, &etfConfigMsgPack, &penaltiesMsgPack, &subAssetsMsgPack, &v.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if poolID.Valid {
		a.PoolID = &poolID.String
	}
	if endDate.Valid {
		v.EndDate = &endDate.Time
	}
	if remainderStartDate.Valid {
		v.RemainderStartDate = &remainderStartDate.Time
	}
	if dumpingLoanID.Valid {
		v.DumpingLoanID = &dumpingLoanID.String
	}
	if stopModID.Valid {
		v.StopModificationID = &stopModID.String
	}
	if etfConfigMsgPack.Valid && etfConfigMsgPack.String != "" {
		db.Unmarshal(etfConfigMsgPack.String, &v.ETFConfig)
	}
	if penaltiesMsgPack.Valid && penaltiesMsgPack.String != "" {
		db.Unmarshal(penaltiesMsgPack.String, &v.Penalties)
	}
	if subAssetsMsgPack.Valid && subAssetsMsgPack.String != "" {
		db.Unmarshal(subAssetsMsgPack.String, &v.SubAssets)
	}
	v.AssetID = a.ID
	a.ActiveVersion = &v
	a.UserID = userID

	// Load virtual account mappings for this single asset
	a.AccountIDs = []string{}
	vaRows, vaErr := r.db.Query("SELECT virtual_account_id FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'ASSET'", a.ID)
	if vaErr == nil {
		defer vaRows.Close()
		for vaRows.Next() {
			var vaID string
			if scanErr := vaRows.Scan(&vaID); scanErr == nil {
				a.AccountIDs = append(a.AccountIDs, vaID)
			}
		}
	}

	return &a, nil
}

func (r *AssetRepository) Save(userID string, asset *domain.Asset) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if asset.ID == "" {
		asset.ID = uuid.New().String()
		_, err = tx.Exec("INSERT INTO assets (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", asset.ID, userID, asset.Name, asset.PoolID)
	} else {
		_, err = tx.Exec("UPDATE assets SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", asset.Name, asset.PoolID, asset.ID, userID)
	}
	if err != nil {
		return err
	}

	// Save multiple virtual account linkages
	_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'ASSET'", asset.ID)
	if err != nil {
		return err
	}
	for _, vaID := range asset.AccountIDs {
		if vaID != "" {
			_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'ASSET', ?)", asset.ID, vaID)
			if err != nil {
				return err
			}
		}
	}

	v := asset.ActiveVersion
	v.ID = uuid.New().String()
	v.AssetID = asset.ID

	etfBytes, _ := db.Marshal(v.ETFConfig)
	penaltiesBytes, _ := db.Marshal(v.Penalties)
	subAssetsBytes, _ := db.Marshal(v.SubAssets)

	_, err = tx.Exec(`
		INSERT INTO asset_versions (id, asset_id, type, target_value, dumping_loan_id, stop_modification_id, interest_rate, interest_interval, amount_per_month, remainder_start_date, start_date, end_date, withdrawal_penalty, etf_config_json, penalties_json, sub_assets_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0.0, ?, ?, ?)`,
		v.ID, v.AssetID, v.Type, v.TargetValue, v.DumpingLoanID, v.StopModificationID, v.InterestRate, v.InterestInterval, v.AmountPerMonth, v.RemainderStartDate, v.StartDate, v.EndDate, etfBytes, penaltiesBytes, subAssetsBytes)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *AssetRepository) SaveBulk(userID string, assets []domain.Asset) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, asset := range assets {
		if asset.ID == "" {
			asset.ID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO assets (id, user_id, name, pool_id) VALUES (?, ?, ?, ?)", asset.ID, userID, asset.Name, asset.PoolID)
		} else {
			_, err = tx.Exec("UPDATE assets SET name = ?, pool_id = ? WHERE id = ? AND user_id = ?", asset.Name, asset.PoolID, asset.ID, userID)
		}
		if err != nil {
			return err
		}

		// Save multiple virtual account linkages
		_, err = tx.Exec("DELETE FROM entity_virtual_accounts WHERE entity_id = ? AND entity_type = 'ASSET'", asset.ID)
		if err != nil {
			return err
		}
		for _, vaID := range asset.AccountIDs {
			if vaID != "" {
				_, err = tx.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, 'ASSET', ?)", asset.ID, vaID)
				if err != nil {
					return err
				}
			}
		}

		v := asset.ActiveVersion
		v.ID = uuid.New().String()
		v.AssetID = asset.ID
		etfBytes, _ := db.Marshal(v.ETFConfig)
		penaltiesBytes, _ := db.Marshal(v.Penalties)
		subAssetsBytes, _ := db.Marshal(v.SubAssets)

		_, err = tx.Exec(`
			INSERT INTO asset_versions (id, asset_id, type, target_value, dumping_loan_id, stop_modification_id, interest_rate, interest_interval, amount_per_month, remainder_start_date, start_date, end_date, withdrawal_penalty, etf_config_json, penalties_json, sub_assets_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0.0, ?, ?, ?)`,
			v.ID, v.AssetID, v.Type, v.TargetValue, v.DumpingLoanID, v.StopModificationID, v.InterestRate, v.InterestInterval, v.AmountPerMonth, v.RemainderStartDate, v.StartDate, v.EndDate, etfBytes, penaltiesBytes, subAssetsBytes)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *AssetRepository) ArchiveFull(userID string, id string) error {
	_, err := r.db.Exec("UPDATE assets SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *AssetRepository) RevertLatest(userID string, assetID string) error {
	var exists bool
	err := r.db.QueryRow("SELECT 1 FROM assets WHERE id = ? AND user_id = ?", assetID, userID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = r.db.Exec(`
		DELETE FROM asset_versions
		WHERE asset_id = ?
		AND created_at = (SELECT MAX(created_at) FROM asset_versions WHERE asset_id = ?)`,
		assetID, assetID)
	if err != nil {
		return err
	}

	var count int
	err = r.db.QueryRow("SELECT COUNT(*) FROM asset_versions WHERE asset_id = ?", assetID).Scan(&count)
	if count == 0 {
		_, err = r.db.Exec("UPDATE assets SET is_deleted = TRUE WHERE id = ?", assetID)
		return err
	}

	return nil
}
