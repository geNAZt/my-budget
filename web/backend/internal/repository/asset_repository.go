package repository

import (
	"database/sql"
	"fmt"
	"strings"

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
		SELECT a.id, a.name, a.pool_id, a.created_at, v.id, v.type, v.target_value, v.dumping_loan_id, v.stop_modification_id, v.interest_rate, v.interest_interval, v.amount_per_month, v.remainder_start_date, v.start_date, v.end_date, v.withdrawal_penalty, v.created_at
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
		var dumpingLoanID sql.NullString
		var stopModID sql.NullString
		var withdrawalPenaltyDummy float64
		var poolID sql.NullString

		err := rows.Scan(&a.ID, &a.Name, &poolID, &a.CreatedAt, &v.ID, &v.Type, &v.TargetValue, &dumpingLoanID, &stopModID, &v.InterestRate, &v.InterestInterval, &v.AmountPerMonth, &remainderStartDate, &v.StartDate, &endDate, &withdrawalPenaltyDummy, &v.CreatedAt)
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

		v.ETFConfig = []domain.ETFTracker{}
		v.Penalties = []domain.AssetPenalty{}
		v.SubAssets = []domain.SubAsset{}

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

	// Batch load sub-elements (etf configs, penalties, sub-assets) for active versions
	if len(assets) > 0 {
		versionIDs := make([]string, 0, len(assets))
		versionIndexMap := make(map[string]*domain.AssetVersion)
		for i := range assets {
			if assets[i].ActiveVersion != nil {
				versionIDs = append(versionIDs, assets[i].ActiveVersion.ID)
				versionIndexMap[assets[i].ActiveVersion.ID] = assets[i].ActiveVersion
			}
		}

		if len(versionIDs) > 0 {
			placeholders := make([]string, len(versionIDs))
			args := make([]interface{}, len(versionIDs))
			for i, id := range versionIDs {
				placeholders[i] = "?"
				args[i] = id
			}

			// 1. Load Stitching Segments for the configs
			configSegments := make(map[string][]domain.HistoryStitchingSegment)
			segmentsQuery := fmt.Sprintf(`
				SELECT s.etf_config_id, s.provider, s.lookup_ticker, s.conversion_tracker
				FROM asset_version_etf_stitching_segments s
				JOIN asset_version_etf_configs c ON s.etf_config_id = c.id
				WHERE c.asset_version_id IN (%s)
				ORDER BY s.sort_order`, strings.Join(placeholders, ","))
			segRows, err := r.db.Query(segmentsQuery, args...)
			if err == nil {
				defer segRows.Close()
				for segRows.Next() {
					var configID string
					var seg domain.HistoryStitchingSegment
					if err := segRows.Scan(&configID, &seg.Provider, &seg.LookupTicker, &seg.ConversionTracker); err == nil {
						configSegments[configID] = append(configSegments[configID], seg)
					}
				}
			}

			// 2. Load ETF configs
			etfQuery := fmt.Sprintf(`
				SELECT id, asset_version_id, tracker, historical_tracker, conversion_tracker, history_provider, percentage, ter
				FROM asset_version_etf_configs
				WHERE asset_version_id IN (%s)`, strings.Join(placeholders, ","))
			etfRows, err := r.db.Query(etfQuery, args...)
			if err == nil {
				defer etfRows.Close()
				for etfRows.Next() {
					var cID, vID string
					var etf domain.ETFTracker
					if err := etfRows.Scan(&cID, &vID, &etf.Tracker, &etf.HistoricalTracker, &etf.ConversionTracker, &etf.HistoryProvider, &etf.Percentage, &etf.TER); err == nil {
						if segs, ok := configSegments[cID]; ok {
							etf.StitchingSegments = segs
						} else {
							etf.StitchingSegments = []domain.HistoryStitchingSegment{}
						}
						if ver, ok := versionIndexMap[vID]; ok {
							ver.ETFConfig = append(ver.ETFConfig, etf)
						}
					}
				}
			}

			// 2. Load Penalties
			penaltiesQuery := fmt.Sprintf(`
				SELECT asset_version_id, name, trigger_type, percentage
				FROM asset_version_penalties
				WHERE asset_version_id IN (%s)`, strings.Join(placeholders, ","))
			penRows, err := r.db.Query(penaltiesQuery, args...)
			if err == nil {
				defer penRows.Close()
				for penRows.Next() {
					var vID string
					var penalty domain.AssetPenalty
					if err := penRows.Scan(&vID, &penalty.Name, (*string)(&penalty.TriggerType), &penalty.Percentage); err == nil {
						if ver, ok := versionIndexMap[vID]; ok {
							ver.Penalties = append(ver.Penalties, penalty)
						}
					}
				}
			}

			// 3. Load Sub-assets
			subQuery := fmt.Sprintf(`
				SELECT asset_version_id, sub_asset_id, name, target_value, amount_per_month, is_remainder_consumer, remainder_start_date, dumping_loan_id, start_date, end_date, earliest_dump_date, expense_id
				FROM asset_version_sub_assets
				WHERE asset_version_id IN (%s)`, strings.Join(placeholders, ","))
			saRows, err := r.db.Query(subQuery, args...)
			if err == nil {
				defer saRows.Close()
				for saRows.Next() {
					var vID string
					var sa domain.SubAsset
					var remainderStartDate sql.NullTime
					var dumpingLoanID sql.NullString
					var endDate sql.NullTime
					var earliestDumpDate sql.NullTime
					var expenseID sql.NullString
					err := saRows.Scan(&vID, &sa.ID, &sa.Name, &sa.TargetValue, &sa.AmountPerMonth, &sa.IsRemainderConsumer, &remainderStartDate, &dumpingLoanID, &sa.StartDate, &endDate, &earliestDumpDate, &expenseID)
					if err == nil {
						if remainderStartDate.Valid {
							sa.RemainderStartDate = &remainderStartDate.Time
						}
						if dumpingLoanID.Valid {
							sa.DumpingLoanID = &dumpingLoanID.String
						}
						if endDate.Valid {
							sa.EndDate = &endDate.Time
						}
						if earliestDumpDate.Valid {
							sa.EarliestDumpDate = &earliestDumpDate.Time
						}
						if expenseID.Valid {
							sa.ExpenseID = &expenseID.String
						}
						if ver, ok := versionIndexMap[vID]; ok {
							ver.SubAssets = append(ver.SubAssets, sa)
						}
					}
				}
			}
		}
	}

	return assets, nil
}

func (r *AssetRepository) GetByID(userID string, id string) (*domain.Asset, error) {
	query := `
		SELECT a.id, a.name, a.pool_id, a.created_at, v.id, v.type, v.target_value, v.dumping_loan_id, v.stop_modification_id, v.interest_rate, v.interest_interval, v.amount_per_month, v.remainder_start_date, v.start_date, v.end_date, v.withdrawal_penalty, v.created_at
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
	var dumpingLoanID sql.NullString
	var stopModID sql.NullString
	var withdrawalPenaltyDummy float64
	var poolID sql.NullString

	err := r.db.QueryRow(query, userID, id).Scan(&a.ID, &a.Name, &poolID, &a.CreatedAt, &v.ID, &v.Type, &v.TargetValue, &dumpingLoanID, &stopModID, &v.InterestRate, &v.InterestInterval, &v.AmountPerMonth, &remainderStartDate, &v.StartDate, &endDate, &withdrawalPenaltyDummy, &v.CreatedAt)

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

	v.ETFConfig = []domain.ETFTracker{}
	v.Penalties = []domain.AssetPenalty{}
	v.SubAssets = []domain.SubAsset{}

	v.AssetID = a.ID
	a.ActiveVersion = &v
	a.UserID = userID

	// Load Stitching Segments for the configs
	configSegments := make(map[string][]domain.HistoryStitchingSegment)
	segRows, err := r.db.Query(`
		SELECT s.etf_config_id, s.provider, s.lookup_ticker, s.conversion_tracker
		FROM asset_version_etf_stitching_segments s
		JOIN asset_version_etf_configs c ON s.etf_config_id = c.id
		WHERE c.asset_version_id = ?
		ORDER BY s.sort_order`, v.ID)
	if err == nil {
		defer segRows.Close()
		for segRows.Next() {
			var configID string
			var seg domain.HistoryStitchingSegment
			if err := segRows.Scan(&configID, &seg.Provider, &seg.LookupTicker, &seg.ConversionTracker); err == nil {
				configSegments[configID] = append(configSegments[configID], seg)
			}
		}
	}

	// Load ETFConfig
	etfRows, err := r.db.Query(`
		SELECT id, tracker, historical_tracker, conversion_tracker, history_provider, percentage, ter
		FROM asset_version_etf_configs
		WHERE asset_version_id = ?`, v.ID)
	if err == nil {
		defer etfRows.Close()
		for etfRows.Next() {
			var cID string
			var etf domain.ETFTracker
			if err := etfRows.Scan(&cID, &etf.Tracker, &etf.HistoricalTracker, &etf.ConversionTracker, &etf.HistoryProvider, &etf.Percentage, &etf.TER); err == nil {
				if segs, ok := configSegments[cID]; ok {
					etf.StitchingSegments = segs
				} else {
					etf.StitchingSegments = []domain.HistoryStitchingSegment{}
				}
				v.ETFConfig = append(v.ETFConfig, etf)
			}
		}
	}

	// Load Penalties
	penRows, err := r.db.Query(`
		SELECT name, trigger_type, percentage
		FROM asset_version_penalties
		WHERE asset_version_id = ?`, v.ID)
	if err == nil {
		defer penRows.Close()
		for penRows.Next() {
			var penalty domain.AssetPenalty
			if err := penRows.Scan(&penalty.Name, (*string)(&penalty.TriggerType), &penalty.Percentage); err == nil {
				v.Penalties = append(v.Penalties, penalty)
			}
		}
	}

	// Load SubAssets
	saRows, err := r.db.Query(`
		SELECT sub_asset_id, name, target_value, amount_per_month, is_remainder_consumer, remainder_start_date, dumping_loan_id, start_date, end_date, earliest_dump_date, expense_id
		FROM asset_version_sub_assets
		WHERE asset_version_id = ?`, v.ID)
	if err == nil {
		defer saRows.Close()
		for saRows.Next() {
			var sa domain.SubAsset
			var remainderStartDate sql.NullTime
			var dumpingLoanID sql.NullString
			var endDate sql.NullTime
			var earliestDumpDate sql.NullTime
			var expenseID sql.NullString
			err := saRows.Scan(&sa.ID, &sa.Name, &sa.TargetValue, &sa.AmountPerMonth, &sa.IsRemainderConsumer, &remainderStartDate, &dumpingLoanID, &sa.StartDate, &endDate, &earliestDumpDate, &expenseID)
			if err == nil {
				if remainderStartDate.Valid {
					sa.RemainderStartDate = &remainderStartDate.Time
				}
				if dumpingLoanID.Valid {
					sa.DumpingLoanID = &dumpingLoanID.String
				}
				if endDate.Valid {
					sa.EndDate = &endDate.Time
				}
				if earliestDumpDate.Valid {
					sa.EarliestDumpDate = &earliestDumpDate.Time
				}
				if expenseID.Valid {
					sa.ExpenseID = &expenseID.String
				}
				v.SubAssets = append(v.SubAssets, sa)
			}
		}
	}

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

	var exists bool
	if asset.ID != "" {
		err = tx.QueryRow("SELECT 1 FROM assets WHERE id = ? AND user_id = ?", asset.ID, userID).Scan(&exists)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
	}

	if !exists {
		if asset.ID == "" {
			asset.ID = uuid.New().String()
		}
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

	_, err = tx.Exec(`
		INSERT INTO asset_versions (id, asset_id, type, target_value, dumping_loan_id, stop_modification_id, interest_rate, interest_interval, amount_per_month, remainder_start_date, start_date, end_date, withdrawal_penalty)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0.0)`,
		v.ID, v.AssetID, v.Type, v.TargetValue, v.DumpingLoanID, v.StopModificationID, v.InterestRate, v.InterestInterval, v.AmountPerMonth, v.RemainderStartDate, v.StartDate, v.EndDate)
	if err != nil {
		return err
	}

	// Save ETFConfig
	for _, etf := range v.ETFConfig {
		etfConfigID := uuid.New().String()
		_, err = tx.Exec(`
			INSERT INTO asset_version_etf_configs (id, asset_version_id, tracker, historical_tracker, conversion_tracker, history_provider, percentage, ter)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			etfConfigID, v.ID, etf.Tracker, etf.HistoricalTracker, etf.ConversionTracker, etf.HistoryProvider, etf.Percentage, etf.TER)
		if err != nil {
			return err
		}

		// Save Stitching Segments
		for i, seg := range etf.StitchingSegments {
			_, err = tx.Exec(`
				INSERT INTO asset_version_etf_stitching_segments (id, etf_config_id, provider, lookup_ticker, conversion_tracker, sort_order)
				VALUES (?, ?, ?, ?, ?, ?)`,
				uuid.New().String(), etfConfigID, seg.Provider, seg.LookupTicker, seg.ConversionTracker, i)
			if err != nil {
				return err
			}
		}
	}

	// Save Penalties
	for _, penalty := range v.Penalties {
		_, err = tx.Exec(`
			INSERT INTO asset_version_penalties (id, asset_version_id, name, trigger_type, percentage)
			VALUES (?, ?, ?, ?, ?)`,
			uuid.New().String(), v.ID, penalty.Name, penalty.TriggerType, penalty.Percentage)
		if err != nil {
			return err
		}
	}

	// Save SubAssets
	for _, sa := range v.SubAssets {
		saID := sa.ID
		if saID == "" {
			saID = uuid.New().String()
		}
		var dLoanID *string = sa.DumpingLoanID
		if dLoanID != nil && *dLoanID != "" {
			var exists bool
			err := tx.QueryRow("SELECT 1 FROM loans WHERE id = ?", *dLoanID).Scan(&exists)
			if err != nil || !exists {
				dLoanID = nil
			}
		} else {
			dLoanID = nil
		}
		var expenseID *string = sa.ExpenseID
		if expenseID != nil && *expenseID != "" {
			var exists bool
			err := tx.QueryRow("SELECT 1 FROM expenses WHERE id = ?", *expenseID).Scan(&exists)
			if err != nil || !exists {
				expenseID = nil
			}
		} else {
			expenseID = nil
		}
		_, err = tx.Exec(`
			INSERT INTO asset_version_sub_assets (id, asset_version_id, sub_asset_id, name, target_value, amount_per_month, is_remainder_consumer, remainder_start_date, dumping_loan_id, start_date, end_date, earliest_dump_date, expense_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.New().String(), v.ID, saID, sa.Name, sa.TargetValue, sa.AmountPerMonth, sa.IsRemainderConsumer, sa.RemainderStartDate, dLoanID, sa.StartDate, sa.EndDate, sa.EarliestDumpDate, expenseID)
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

func (r *AssetRepository) ClearHistoryCache() (int64, error) {
	res, err := r.db.Exec("DELETE FROM external_cache WHERE key LIKE '%_history%'")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
