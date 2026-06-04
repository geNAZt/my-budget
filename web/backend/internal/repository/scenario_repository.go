package repository

import (
	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/db"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type ScenarioRepository struct {
	db          *sql.DB
	incomeRepo  *IncomeRepository
	billRepo    *BillRepository
	expenseRepo *ExpenseRepository
}

func NewScenarioRepository(db *sql.DB, ir *IncomeRepository, br *BillRepository, er *ExpenseRepository) *ScenarioRepository {
	return &ScenarioRepository{
		db:          db,
		incomeRepo:  ir,
		billRepo:    br,
		expenseRepo: er,
	}
}

func (r *ScenarioRepository) List(userID string) ([]domain.Scenario, error) {
	rows, err := r.db.Query(`
		SELECT id, name, description, projection_months, is_active, created_at, remainder_order_json AS remainder_order_msgpack, simulations, sim_years, sim_percent, start_date, etf_params_json AS etf_params_msgpack, lookback_years, passive_income_percentage, mc_implementation, month_start_day
		FROM scenarios
		WHERE user_id = ? AND is_deleted = FALSE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scenarios []domain.Scenario
	for rows.Next() {
		var s domain.Scenario
		var remainderOrderMsgPack sql.NullString
		var etfParamsMsgPack sql.NullString
		var startDate sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.ProjectionMonths, &s.IsActive, &s.CreatedAt, &remainderOrderMsgPack, &s.Simulations, &s.SimYears, &s.SimPercent, &startDate, &etfParamsMsgPack, &s.LookbackYears, &s.PassiveIncomePercentage, &s.MonteCarloImplementation, &s.MonthStartDay)
		if err != nil {
			return nil, err
		}

		if remainderOrderMsgPack.Valid && remainderOrderMsgPack.String != "" {
			db.Unmarshal(remainderOrderMsgPack.String, &s.RemainderOrder)
		}
		if etfParamsMsgPack.Valid && etfParamsMsgPack.String != "" {
			db.Unmarshal(etfParamsMsgPack.String, &s.ETFParams)
		}
		if startDate.Valid {
			s.StartDate = &startDate.Time
		}
		s.UserID = userID
		scenarios = append(scenarios, s)
	}

	// Load all entities for these scenarios
	if len(scenarios) > 0 {
		scenarioByID := make(map[string]*domain.Scenario)
		for i := range scenarios {
			scenarioByID[scenarios[i].ID] = &scenarios[i]
		}

		rows, err := r.db.Query(`
			SELECT se.scenario_id, se.entity_id, se.entity_type, se.version_id
			FROM scenario_entities se
			JOIN scenarios s ON se.scenario_id = s.id
			WHERE s.user_id = ? AND s.is_deleted = FALSE`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var scenarioID string
			var e domain.ScenarioEntity
			var versionID sql.NullString
			if err := rows.Scan(&scenarioID, &e.EntityID, &e.EntityType, &versionID); err != nil {
				return nil, err
			}
			if versionID.Valid {
				e.VersionID = versionID.String
			}
			if s, ok := scenarioByID[scenarioID]; ok {
				s.Entities = append(s.Entities, e)
			}
		}
	}

	return scenarios, nil
}

func (r *ScenarioRepository) Save(userID string, s *domain.Scenario) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	remainderOrderStr, _ := db.Marshal(s.RemainderOrder)
	etfParamsStr, _ := db.Marshal(s.ETFParams)

	var exists bool
	if s.ID != "" {
		err = tx.QueryRow("SELECT 1 FROM scenarios WHERE id = ? AND user_id = ?", s.ID, userID).Scan(&exists)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
	}

	if !exists {
		if s.ID == "" {
			s.ID = uuid.New().String()
		}
		_, err = tx.Exec(`
			INSERT INTO scenarios (id, user_id, name, description, projection_months, is_active, remainder_order_json, simulations, sim_years, sim_percent, start_date, etf_params_json, lookback_years, passive_income_percentage, mc_implementation, month_start_day)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, userID, s.Name, s.Description, s.ProjectionMonths, s.IsActive, remainderOrderStr, s.Simulations, s.SimYears, s.SimPercent, s.StartDate, etfParamsStr, s.LookbackYears, s.PassiveIncomePercentage, s.MonteCarloImplementation, s.MonthStartDay)
	} else {
		_, err = tx.Exec(`
			UPDATE scenarios SET name = ?, description = ?, projection_months = ?, is_active = ?, remainder_order_json = ?, simulations = ?, sim_years = ?, sim_percent = ?, start_date = ?, etf_params_json = ?, lookback_years = ?, passive_income_percentage = ?, mc_implementation = ?, month_start_day = ?
			WHERE id = ? AND user_id = ?`,
			s.Name, s.Description, s.ProjectionMonths, s.IsActive, remainderOrderStr, s.Simulations, s.SimYears, s.SimPercent, s.StartDate, etfParamsStr, s.LookbackYears, s.PassiveIncomePercentage, s.MonteCarloImplementation, s.MonthStartDay, s.ID, userID)
	}
	if err != nil {
		return err
	}

	// Update entities (Sync)
	_, err = tx.Exec("DELETE FROM scenario_entities WHERE scenario_id = ?", s.ID)
	if err != nil {
		return err
	}

	for _, e := range s.Entities {
		_, err = tx.Exec(`
			INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id)
			VALUES (?, ?, ?, ?)`,
			s.ID, e.EntityID, e.EntityType, e.VersionID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ScenarioRepository) LinkEntityToScenarios(userID string, entityID string, entityType string, scenarioIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, sID := range scenarioIDs {
		// Verify ownership
		var exists bool
		err := r.db.QueryRow("SELECT 1 FROM scenarios WHERE id = ? AND user_id = ?", sID, userID).Scan(&exists)
		if err != nil || !exists {
			continue
		}

		_, err = tx.Exec(`
			INSERT INTO scenario_entities (scenario_id, entity_id, entity_type)
			VALUES (?, ?, ?)
			ON CONFLICT (scenario_id, entity_id) DO NOTHING`,
			sID, entityID, entityType)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ScenarioRepository) Fork(userID string, sourceID string, newName string) (string, error) {
	newID := uuid.New().String()

	tx, err := r.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Copy Scenario metadata
	_, err = tx.Exec(`
		INSERT INTO scenarios (id, user_id, name, description, projection_months, is_active, remainder_order_json, simulations, sim_years, sim_percent, start_date, etf_params_json, lookback_years, passive_income_percentage, mc_implementation, month_start_day)
		SELECT ?, user_id, ?, description, projection_months, FALSE, remainder_order_json, simulations, sim_years, sim_percent, start_date, etf_params_json, lookback_years, passive_income_percentage, mc_implementation, month_start_day
		FROM scenarios WHERE id = ? AND user_id = ?`,
		newID, newName, sourceID, userID)
	if err != nil {
		return "", err
	}

	// 2. Copy Entity Links
	_, err = tx.Exec(`
		INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id)
		SELECT ?, entity_id, entity_type, version_id
		FROM scenario_entities WHERE scenario_id = ?`,
		newID, sourceID)
	if err != nil {
		return "", err
	}

	return newID, tx.Commit()
}

func (r *ScenarioRepository) Archive(userID string, id string) error {
	_, err := r.db.Exec("UPDATE scenarios SET is_deleted = TRUE WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (r *ScenarioRepository) GetFull(userID string, id string) (*domain.Scenario, error) {
	s := &domain.Scenario{ID: id, UserID: userID}
	var remainderOrderMsgPack sql.NullString
	var etfParamsMsgPack sql.NullString
	var startDate sql.NullTime
	err := r.db.QueryRow(`
		SELECT name, description, projection_months, is_active, created_at, remainder_order_json AS remainder_order_msgpack, simulations, sim_years, sim_percent, start_date, etf_params_json AS etf_params_msgpack, lookback_years, passive_income_percentage, mc_implementation, month_start_day
		FROM scenarios WHERE id = ? AND user_id = ? AND is_deleted = FALSE`,
		id, userID).Scan(&s.Name, &s.Description, &s.ProjectionMonths, &s.IsActive, &s.CreatedAt, &remainderOrderMsgPack, &s.Simulations, &s.SimYears, &s.SimPercent, &startDate, &etfParamsMsgPack, &s.LookbackYears, &s.PassiveIncomePercentage, &s.MonteCarloImplementation, &s.MonthStartDay)
	if err != nil {
		return nil, err
	}
	if remainderOrderMsgPack.Valid && remainderOrderMsgPack.String != "" {
		db.Unmarshal(remainderOrderMsgPack.String, &s.RemainderOrder)
	}
	if etfParamsMsgPack.Valid && etfParamsMsgPack.String != "" {
		db.Unmarshal(etfParamsMsgPack.String, &s.ETFParams)
	}
	if startDate.Valid {
		s.StartDate = &startDate.Time
	}

	rows, err := r.db.Query(`
		SELECT entity_id, entity_type, version_id FROM scenario_entities WHERE scenario_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var e domain.ScenarioEntity
		var versionID sql.NullString
		err := rows.Scan(&e.EntityID, &e.EntityType, &versionID)
		if err != nil {
			return nil, err
		}
		if versionID.Valid {
			e.VersionID = versionID.String
		}
		s.Entities = append(s.Entities, e)
	}

	return s, nil
}
