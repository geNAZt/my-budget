package service

import (
	"math"
	"os"
	"testing"
	"time"

	"database/sql"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	_ "modernc.org/sqlite"
)

func TestModificationMultiAssetMapping(t *testing.T) {
	// Setup a temporary SQLite DB file
	tmpFile := "test_mods.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(tmpFile)
	}()

	// Better: execute each line
	for _, stmt := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE incomes (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE income_versions (id TEXT PRIMARY KEY, income_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE assets (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE asset_versions (id TEXT PRIMARY KEY, asset_id TEXT, type TEXT, target_value TEXT, dumping_loan_id TEXT, stop_modification_id TEXT, interest_rate REAL, interest_interval TEXT, amount_per_month REAL, remainder_start_date DATETIME, start_date DATETIME, end_date DATETIME, withdrawal_penalty REAL DEFAULT 0, etf_config_json TEXT, penalties_json TEXT, sub_assets_json TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modifications (id TEXT PRIMARY KEY, user_id TEXT, target_id TEXT, target_type TEXT, description TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_versions (id TEXT PRIMARY KEY, modification_id TEXT, amount REAL, withdrawal_percentage REAL DEFAULT 0, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_assets (modification_id TEXT, asset_id TEXT, PRIMARY KEY(modification_id, asset_id))",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, description TEXT, projection_months INTEGER DEFAULT 12, is_active BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0, month_start_day INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, remainder_order_json TEXT, simulations INTEGER DEFAULT 50000, sim_years INTEGER DEFAULT 10, sim_percent REAL DEFAULT 50, start_date DATETIME, etf_params_json TEXT, lookback_years INTEGER DEFAULT 0, passive_income_percentage REAL DEFAULT 3.5, mc_implementation TEXT DEFAULT 'STANDARD')",
		"CREATE TABLE scenario_entities (scenario_id TEXT, entity_id TEXT, entity_type TEXT, version_id TEXT, PRIMARY KEY(scenario_id, entity_id))",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement %q: %v", stmt, err)
		}
	}

	userID := "user-1"
	db.Exec("INSERT INTO users (id, username) VALUES (?, ?)", userID, "testuser")

	// Create 2 assets
	asset1ID := "asset-1"
	asset2ID := "asset-2"
	db.Exec("INSERT INTO assets (id, user_id, name) VALUES (?, ?, ?)", asset1ID, userID, "Asset 1")
	db.Exec("INSERT INTO assets (id, user_id, name) VALUES (?, ?, ?)", asset2ID, userID, "Asset 2")

	v1ID := "v1"
	v2ID := "v2"
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	db.Exec("INSERT INTO asset_versions (id, asset_id, type, target_value, interest_rate, interest_interval, amount_per_month, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", v1ID, asset1ID, "STATIC", "100000", 0, "Monthly", 0, startDate)
	db.Exec("INSERT INTO asset_versions (id, asset_id, type, target_value, interest_rate, interest_interval, amount_per_month, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", v2ID, asset2ID, "STATIC", "100000", 0, "Monthly", 0, startDate)

	// Create 1 modification targeting BOTH assets
	modID := "mod-1"
	db.Exec("INSERT INTO modifications (id, user_id, target_type, description) VALUES (?, ?, ?, ?)", modID, userID, "ASSET", "Multi-Asset Mod")
	db.Exec("INSERT INTO modification_assets (modification_id, asset_id) VALUES (?, ?)", modID, asset1ID)
	db.Exec("INSERT INTO modification_assets (modification_id, asset_id) VALUES (?, ?)", modID, asset2ID)

	mvID := "mv1"
	modStartDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // Next month
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", mvID, modID, 500.0, modStartDate, 0)

	// Create Scenario
	scenarioID := "scen-1"
	db.Exec("INSERT INTO scenarios (id, user_id, name, description, projection_months, is_active, is_deleted, simulations) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", scenarioID, userID, "Test Scenario", "Desc", 12, 0, 0, 1)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, asset1ID, "ASSET", v1ID)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, asset2ID, "ASSET", v2ID)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, modID, "MODIFICATION", mvID)

	// Initialize repos
	assetRepo := repository.NewAssetRepository(db)
	modRepo := repository.NewModificationRepository(db)

	// Verify modRepo.List works as expected
	loadedMods, err := modRepo.List(userID)
	if err != nil {
		t.Fatalf("Failed to list mods: %v", err)
	}
	if len(loadedMods) == 0 {
		t.Fatalf("No modifications loaded from repo")
	}
	if len(loadedMods[0].TargetIDs) != 2 {
		t.Fatalf("Expected 2 target IDs in loaded modification, got %d", len(loadedMods[0].TargetIDs))
	}

	incomeRepo := repository.NewIncomeRepository(db)
	billRepo := repository.NewBillRepository(db)
	expenseRepo := repository.NewExpenseRepository(db)
	loanRepo := repository.NewLoanRepository(db)
	scenarioRepo := repository.NewScenarioRepository(db, incomeRepo, billRepo, expenseRepo)

	// Projection Service
	ps := NewProjectionService(scenarioRepo, incomeRepo, billRepo, expenseRepo, assetRepo, nil)
	ps.SetAdditionalRepos(loanRepo, modRepo)
	// Run projection
	result, err := ps.Run(userID, scenarioID, nil)
	if err != nil {
		t.Fatalf("Projection failed: %v", err)
	}

	// Verify results
	foundMod := false
	for _, m := range result.Months {
		// Compare month and year only
		if m.Date.Year() == modStartDate.Year() && m.Date.Month() == modStartDate.Month() {
			foundMod = true
			// Check breakdown for both assets having the mod
			modCount := 0
			for _, ae := range m.Breakdown.Assets {
				// The name in projection is Description + " (Mod)"
				if ae.Name == "Multi-Asset Mod (Mod)" {
					modCount++
					if ae.Amount != 500.0 {
						t.Errorf("Expected mod amount 500.0, got %f", ae.Amount)
					}
				}
			}
			// In our logic, a multi-target mod is applied to EACH target
			if modCount != 2 {
				// Log what we found to help debugging
				for _, ae := range m.Breakdown.Assets {
					t.Logf("Found asset breakdown: %s = %f", ae.Name, ae.Amount)
				}
				t.Errorf("Expected 2 assets to have the modification applied, got %d", modCount)
			}
		}
	}

	if !foundMod {
		t.Errorf("Modification month not found in projection")
	}
}

func TestModificationDeletion(t *testing.T) {
	// Setup a temporary SQLite DB file
	tmpFile := "test_del_mods.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(tmpFile)
	}()

	// Execute each line
	for _, stmt := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE incomes (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE income_versions (id TEXT PRIMARY KEY, income_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE assets (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE asset_versions (id TEXT PRIMARY KEY, asset_id TEXT, type TEXT, target_value TEXT, dumping_loan_id TEXT, stop_modification_id TEXT, interest_rate REAL, interest_interval TEXT, amount_per_month REAL, remainder_start_date DATETIME, start_date DATETIME, end_date DATETIME, withdrawal_penalty REAL DEFAULT 0, etf_config_json TEXT, penalties_json TEXT, sub_assets_json TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modifications (id TEXT PRIMARY KEY, user_id TEXT, target_id TEXT, target_type TEXT, description TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_versions (id TEXT PRIMARY KEY, modification_id TEXT, amount REAL, withdrawal_percentage REAL DEFAULT 0, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_assets (modification_id TEXT, asset_id TEXT, PRIMARY KEY(modification_id, asset_id))",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, description TEXT, projection_months INTEGER DEFAULT 12, is_active BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0, month_start_day INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, remainder_order_json TEXT, simulations INTEGER DEFAULT 50000, sim_years INTEGER DEFAULT 10, sim_percent REAL DEFAULT 50, start_date DATETIME, etf_params_json TEXT, lookback_years INTEGER DEFAULT 0, passive_income_percentage REAL DEFAULT 3.5, mc_implementation TEXT DEFAULT 'STANDARD')",
		"CREATE TABLE scenario_entities (scenario_id TEXT, entity_id TEXT, entity_type TEXT, version_id TEXT, PRIMARY KEY(scenario_id, entity_id))",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement %q: %v", stmt, err)
		}
	}

	userID := "user-1"
	assetID := "asset-1"
	db.Exec("INSERT INTO assets (id, user_id, name) VALUES (?, ?, ?)", assetID, userID, "Asset 1")
	startDate := time.Now().AddDate(0, -1, 0)
	db.Exec("INSERT INTO asset_versions (id, asset_id, type, target_value, interest_rate, interest_interval, amount_per_month, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", "v1", assetID, "STATIC", "1000", 0, "Monthly", 0, startDate)

	modID := "mod-1"
	db.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", modID, userID, assetID, "ASSET", "Delete Me")
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", "mv1", modID, 100.0, time.Now(), 0)

	modRepo := repository.NewModificationRepository(db)

	// 1. Verify it exists
	mods, _ := modRepo.List(userID)
	if len(mods) != 1 {
		t.Fatalf("Expected 1 mod, got %d", len(mods))
	}

	// 2. Delete it
	if err := modRepo.ArchiveFull(userID, modID); err != nil {
		t.Fatalf("ArchiveFull failed: %v", err)
	}

	// 3. Verify it's gone from List
	mods, _ = modRepo.List(userID)
	if len(mods) != 0 {
		t.Errorf("Expected 0 mods after deletion, got %d", len(mods))
	}
}

func TestPassiveIncomeMilestone(t *testing.T) {
	// Setup a temporary SQLite DB file
	tmpFile := "test_fi_mods.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(tmpFile)
	}()

	// Execute each line
	for _, stmt := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE incomes (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE income_versions (id TEXT PRIMARY KEY, income_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE assets (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE asset_versions (id TEXT PRIMARY KEY, asset_id TEXT, type TEXT, target_value TEXT, dumping_loan_id TEXT, stop_modification_id TEXT, interest_rate REAL, interest_interval TEXT, amount_per_month REAL, remainder_start_date DATETIME, start_date DATETIME, end_date DATETIME, withdrawal_penalty REAL DEFAULT 0, etf_config_json TEXT, penalties_json TEXT, sub_assets_json TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modifications (id TEXT PRIMARY KEY, user_id TEXT, target_id TEXT, target_type TEXT, description TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_versions (id TEXT PRIMARY KEY, modification_id TEXT, amount REAL, withdrawal_percentage REAL DEFAULT 0, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_assets (modification_id TEXT, asset_id TEXT, PRIMARY KEY(modification_id, asset_id))",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, description TEXT, projection_months INTEGER DEFAULT 12, is_active BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0, month_start_day INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, remainder_order_json TEXT, simulations INTEGER DEFAULT 50000, sim_years INTEGER DEFAULT 10, sim_percent REAL DEFAULT 50, start_date DATETIME, etf_params_json TEXT, lookback_years INTEGER DEFAULT 0, passive_income_percentage REAL DEFAULT 3.5, mc_implementation TEXT DEFAULT 'STANDARD')",
		"CREATE TABLE scenario_entities (scenario_id TEXT, entity_id TEXT, entity_type TEXT, version_id TEXT, PRIMARY KEY(scenario_id, entity_id))",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement %q: %v", stmt, err)
		}
	}

	userID := "user-1"
	assetID := "asset-1"
	db.Exec("INSERT INTO assets (id, user_id, name) VALUES (?, ?, ?)", assetID, userID, "ETF Asset")

	// Start 2 months ago
	startDate := time.Now().AddDate(0, -2, 0)
	db.Exec("INSERT INTO asset_versions (id, asset_id, type, target_value, interest_rate, interest_interval, amount_per_month, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", "v1", assetID, "ETF", "0", 0, "Monthly", 0, startDate)

	// Initial Balance Modification (1,000,000) 1 month ago
	initialModID := "mod-init"
	db.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", initialModID, userID, assetID, "ASSET", "Initial Balance")
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", "mv-init", initialModID, 1000000.0, startDate, 0)

	// Create Scenario
	scenarioID := "scen-1"
	db.Exec("INSERT INTO scenarios (id, user_id, name, description, projection_months, simulations, passive_income_percentage) VALUES (?, ?, ?, ?, ?, ?, ?)", scenarioID, userID, "FI Scenario", "Desc", 12, 1, 4.0)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, assetID, "ASSET", "v1")
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, initialModID, "MODIFICATION", "mv-init")

	// Dynamic Withdrawal Modification: 4% annually if >= 2000€
	modID := "mod-1"
	db.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", modID, userID, assetID, "ASSET", "FI Withdrawal")
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, withdrawal_percentage, start_date, interval_months) VALUES (?, ?, ?, ?, ?, ?)", "mv1", modID, 2000.0, 4.0, time.Now(), 1)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, modID, "MODIFICATION", "mv1")

	assetRepo := repository.NewAssetRepository(db)
	modRepo := repository.NewModificationRepository(db)
	scenarioRepo := repository.NewScenarioRepository(db, nil, nil, nil)

	ps := NewProjectionService(scenarioRepo, repository.NewIncomeRepository(db), repository.NewBillRepository(db), repository.NewExpenseRepository(db), assetRepo, nil)
	ps.SetAdditionalRepos(repository.NewLoanRepository(db), modRepo)

	result, err := ps.Run(userID, scenarioID, nil)
	if err != nil {
		t.Fatalf("Projection failed: %v", err)
	}

	// 1,000,000 * (4% / 12) = 3333.33
	// This is >= 2000, so it should be applied.
	foundFI := false
	for _, m := range result.Months {
		if m.PassiveIncome > 3000 {
			foundFI = true
		}
		// Check if modification applied
		for _, ae := range m.Breakdown.Assets {
			if ae.Name == "FI Withdrawal (4.00% Dynamic) (Mod)" {
				// Use a more relaxed threshold for precision
				if math.Abs(ae.Amount-(-3333.33)) > 50.0 {
					t.Errorf("Expected withdrawal around -3333.33, got %f", ae.Amount)
				}
			}
		}
	}

	if !foundFI {
		t.Errorf("Passive income not calculated correctly")
	}
}

func TestIncomeTerminationViaModification(t *testing.T) {
	// Setup a temporary SQLite DB file
	tmpFile := "test_income_term.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(tmpFile)
	}()

	// Execute each line
	for _, stmt := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE incomes (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE income_versions (id TEXT PRIMARY KEY, income_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE assets (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE asset_versions (id TEXT PRIMARY KEY, asset_id TEXT, type TEXT, target_value TEXT, dumping_loan_id TEXT, stop_modification_id TEXT, interest_rate REAL, interest_interval TEXT, amount_per_month REAL, remainder_start_date DATETIME, start_date DATETIME, end_date DATETIME, withdrawal_penalty REAL DEFAULT 0, etf_config_json TEXT, penalties_json TEXT, sub_assets_json TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modifications (id TEXT PRIMARY KEY, user_id TEXT, target_id TEXT, target_type TEXT, description TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_versions (id TEXT PRIMARY KEY, modification_id TEXT, amount REAL, withdrawal_percentage REAL DEFAULT 0, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_assets (modification_id TEXT, asset_id TEXT, PRIMARY KEY(modification_id, asset_id))",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, description TEXT, projection_months INTEGER DEFAULT 12, is_active BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0, month_start_day INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, remainder_order_json TEXT, simulations INTEGER DEFAULT 50000, sim_years INTEGER DEFAULT 10, sim_percent REAL DEFAULT 50, start_date DATETIME, etf_params_json TEXT, lookback_years INTEGER DEFAULT 0, passive_income_percentage REAL DEFAULT 3.5, mc_implementation TEXT DEFAULT 'STANDARD')",
		"CREATE TABLE scenario_entities (scenario_id TEXT, entity_id TEXT, entity_type TEXT, version_id TEXT, PRIMARY KEY(scenario_id, entity_id))",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement %q: %v", stmt, err)
		}
	}

	userID := "user-1"

	// 1. Create Asset with 1M balance (initial balance mod)
	assetID := "asset-1"
	db.Exec("INSERT INTO assets (id, user_id, name) VALUES (?, ?, ?)", assetID, userID, "ETF Asset")
	startDate := time.Now().AddDate(0, -2, 0)
	// Add stop_modification_id to asset version (index 5)
	// Use 5% interest rate to verify it still grows
	db.Exec("INSERT INTO asset_versions (id, asset_id, type, target_value, dumping_loan_id, stop_modification_id, interest_rate, interest_interval, amount_per_month, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", "v1", assetID, "STATIC", "0", nil, "mod-withdraw", 5.0, "Monthly", 500.0, startDate)

	initialModID := "mod-init"
	db.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", initialModID, userID, assetID, "ASSET", "Initial Balance")
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", "mv-init", initialModID, 1000000.0, startDate, 0)

	// 2. Dynamic Withdrawal Mod (4% annual = ~3.3k monthly)
	modID := "mod-withdraw"
	db.Exec("INSERT INTO modifications (id, user_id, target_id, target_type, description) VALUES (?, ?, ?, ?, ?)", modID, userID, assetID, "ASSET", "Passive Income")
	db.Exec("INSERT INTO modification_versions (id, modification_id, amount, withdrawal_percentage, start_date, interval_months) VALUES (?, ?, ?, ?, ?, ?)", "mv-withdraw", modID, 1000.0, 4.0, time.Now(), 1)

	// 3. Income linked to Mod
	incomeID := "income-1"
	db.Exec("INSERT INTO incomes (id, user_id, name) VALUES (?, ?, ?)", incomeID, userID, "Salary")
	db.Exec("INSERT INTO income_versions (id, income_id, amount, stop_modification_id, start_date, interval_months) VALUES (?, ?, ?, ?, ?, ?)", "iv1", incomeID, 2000.0, modID, startDate, 1)

	// Create Scenario
	scenarioID := "scen-1"
	db.Exec("INSERT INTO scenarios (id, user_id, name, description, projection_months, simulations) VALUES (?, ?, ?, ?, ?, ?)", scenarioID, userID, "Termination Scenario", "Desc", 12, 1)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, assetID, "ASSET", "v1")
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, initialModID, "MODIFICATION", "mv-init")
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, modID, "MODIFICATION", "mv-withdraw")
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, incomeID, "INCOME", "iv1")

	assetRepo := repository.NewAssetRepository(db)
	modRepo := repository.NewModificationRepository(db)
	incomeRepo := repository.NewIncomeRepository(db)
	scenarioRepo := repository.NewScenarioRepository(db, incomeRepo, nil, nil)

	ps := NewProjectionService(scenarioRepo, incomeRepo, repository.NewBillRepository(db), repository.NewExpenseRepository(db), assetRepo, nil)
	ps.SetAdditionalRepos(repository.NewLoanRepository(db), modRepo)

	result, err := ps.Run(userID, scenarioID, nil)
	if err != nil {
		t.Fatalf("Projection failed: %v", err)
	}

	// Since mod triggers "Now" (05/2026), the month 06/2026 should NOT have the income.
	salaryFoundInLastMonth := false
	lastMonth := result.Months[len(result.Months)-1]
	for _, ie := range lastMonth.Breakdown.Incomes {
		if ie.Name == "Salary" {
			salaryFoundInLastMonth = true
		}
	}

	if salaryFoundInLastMonth {
		t.Errorf("Income 'Salary' was found in the last projection month but should have been terminated by modification trigger")
	}

	// Verify Asset contributions stopped
	contributionFoundInLastMonth := false
	for _, ae := range lastMonth.Breakdown.Assets {
		if ae.Name == "ETF Asset" && ae.Amount == 500.0 {
			contributionFoundInLastMonth = true
		}
	}

	if contributionFoundInLastMonth {
		t.Errorf("Asset contribution was found in the last projection month but the asset should have stopped accepting money")
	}

	// Verify Interest was still applied
	interestFoundInLastMonth := false
	for _, ae := range lastMonth.Breakdown.Assets {
		if ae.Name == "ETF Asset" && ae.Interest > 0 {
			interestFoundInLastMonth = true
		}
	}

	if !interestFoundInLastMonth {
		t.Errorf("Asset interest was NOT found in the last projection month but it should still generate interest after contributions stop")
	}
}

func TestVirtualAccountsBalanceReset(t *testing.T) {
	// Setup a temporary SQLite DB file
	tmpFile := "test_va_reset.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer func() {
		db.Close()
		os.Remove(tmpFile)
	}()

	// Execute each line
	for _, stmt := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE incomes (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE income_versions (id TEXT PRIMARY KEY, income_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE bills (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE bill_versions (id TEXT PRIMARY KEY, bill_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE expenses (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE expense_versions (id TEXT PRIMARY KEY, expense_id TEXT, amount REAL, stop_modification_id TEXT, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE assets (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, pool_id TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE asset_versions (id TEXT PRIMARY KEY, asset_id TEXT, type TEXT, target_value TEXT, dumping_loan_id TEXT, stop_modification_id TEXT, interest_rate REAL, interest_interval TEXT, amount_per_month REAL, remainder_start_date DATETIME, start_date DATETIME, end_date DATETIME, withdrawal_penalty REAL DEFAULT 0, etf_config_json TEXT, penalties_json TEXT, sub_assets_json TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE loans (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, account_id TEXT DEFAULT '', is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE loan_versions (id TEXT PRIMARY KEY, loan_id TEXT, principal REAL, rate REAL, term_months INTEGER, balloon REAL, interest_only BOOLEAN, start_date DATETIME, end_date DATETIME, stop_modification_id TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modifications (id TEXT PRIMARY KEY, user_id TEXT, target_id TEXT, target_type TEXT, description TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_versions (id TEXT PRIMARY KEY, modification_id TEXT, amount REAL, withdrawal_percentage REAL DEFAULT 0, start_date DATETIME, end_date DATETIME, interval_months INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE modification_assets (modification_id TEXT, asset_id TEXT, PRIMARY KEY(modification_id, asset_id))",
		"CREATE TABLE scenarios (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, description TEXT, projection_months INTEGER DEFAULT 12, is_active BOOLEAN DEFAULT 0, is_deleted BOOLEAN DEFAULT 0, month_start_day INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, remainder_order_json TEXT, simulations INTEGER DEFAULT 50000, sim_years INTEGER DEFAULT 10, sim_percent REAL DEFAULT 50, start_date DATETIME, etf_params_json TEXT, lookback_years INTEGER DEFAULT 0, passive_income_percentage REAL DEFAULT 3.5, mc_implementation TEXT DEFAULT 'STANDARD')",
		"CREATE TABLE scenario_entities (scenario_id TEXT, entity_id TEXT, entity_type TEXT, version_id TEXT, PRIMARY KEY(scenario_id, entity_id))",
		"CREATE TABLE virtual_accounts (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, is_deleted BOOLEAN DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE virtual_account_versions (id TEXT PRIMARY KEY, virtual_account_id TEXT, color TEXT, starting_balance REAL, description TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE entity_virtual_accounts (entity_id TEXT, entity_type TEXT, virtual_account_id TEXT, PRIMARY KEY (entity_id, entity_type, virtual_account_id))",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("Failed to execute statement %q: %v", stmt, err)
		}
	}

	userID := "user-2"
	db.Exec("INSERT INTO users (id, username) VALUES (?, ?)", userID, "testuser2")

	// Create a virtual account
	vaID := "va-1"
	db.Exec("INSERT INTO virtual_accounts (id, user_id, name) VALUES (?, ?, ?)", vaID, userID, "Virtual Acc 1")
	db.Exec("INSERT INTO virtual_account_versions (id, virtual_account_id, color, starting_balance, description) VALUES (?, ?, ?, ?, ?)", "vav-1", vaID, "#ff0000", 100.0, "Test VA")

	// Create an income assigned to this virtual account
	incID := "inc-1"
	db.Exec("INSERT INTO incomes (id, user_id, name) VALUES (?, ?, ?)", incID, userID, "Income 1")
	incStartDate := time.Now().AddDate(0, -1, 0)
	db.Exec("INSERT INTO income_versions (id, income_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", "inv-1", incID, 1000.0, incStartDate, 1)
	db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, ?, ?)", incID, "INCOME", vaID)

	// Create a bill assigned to this virtual account
	billID := "bill-1"
	db.Exec("INSERT INTO bills (id, user_id, name) VALUES (?, ?, ?)", billID, userID, "Bill 1")
	billStartDate := time.Now().AddDate(0, -1, 0)
	db.Exec("INSERT INTO bill_versions (id, bill_id, amount, start_date, interval_months) VALUES (?, ?, ?, ?, ?)", "bv-1", billID, 200.0, billStartDate, 1)
	db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) VALUES (?, ?, ?)", billID, "BILL", vaID)

	// Create Scenario
	scenarioID := "scen-2"
	scenarioStartDate := time.Now()
	db.Exec("INSERT INTO scenarios (id, user_id, name, description, projection_months, is_active, is_deleted, month_start_day, start_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		scenarioID, userID, "Test Scenario 2", "Desc", 3, 0, 0, 1, scenarioStartDate)
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, incID, "INCOME", "inv-1")
	db.Exec("INSERT INTO scenario_entities (scenario_id, entity_id, entity_type, version_id) VALUES (?, ?, ?, ?)", scenarioID, billID, "BILL", "bv-1")

	// Initialize repositories
	incomeRepo := repository.NewIncomeRepository(db)
	billRepo := repository.NewBillRepository(db)
	expenseRepo := repository.NewExpenseRepository(db)
	assetRepo := repository.NewAssetRepository(db)
	loanRepo := repository.NewLoanRepository(db)
	modRepo := repository.NewModificationRepository(db)
	scenarioRepo := repository.NewScenarioRepository(db, incomeRepo, billRepo, expenseRepo)
	virtualAccountRepo := repository.NewVirtualAccountRepository(db)

	ps := NewProjectionService(scenarioRepo, incomeRepo, billRepo, expenseRepo, assetRepo, nil)
	ps.SetAdditionalRepos(loanRepo, modRepo)
	ps.SetVirtualAccountRepo(virtualAccountRepo)

	// Run projection
	result, err := ps.Run(userID, scenarioID, nil)
	if err != nil {
		t.Fatalf("Projection failed: %v", err)
	}

	// Verify that each of the 3 projection months has:
	// StartingBalance = 0.0, Inflow = 1000.0, Outflow = 200.0, Balance = 800.0
	if len(result.Months) != 3 {
		t.Fatalf("Expected 3 months in projection result, got %d", len(result.Months))
	}

	for monthIdx, month := range result.Months {
		if len(month.VirtualAccounts) == 0 {
			t.Fatalf("Expected virtual accounts in month %d, got none", monthIdx)
		}

		var testVA *domain.VirtualAccountMonthBalance
		var unassignedVA *domain.VirtualAccountMonthBalance
		for i := range month.VirtualAccounts {
			if month.VirtualAccounts[i].AccountID == vaID {
				testVA = &month.VirtualAccounts[i]
			} else if month.VirtualAccounts[i].AccountID == "unassigned" {
				unassignedVA = &month.VirtualAccounts[i]
			}
		}

		if testVA == nil {
			t.Fatalf("Expected virtual account %s in month %d, not found", vaID, monthIdx)
		}

		if testVA.StartingBalance != 0.0 {
			t.Errorf("Month %d: Expected virtual account StartingBalance to be 0.0, got %f", monthIdx, testVA.StartingBalance)
		}
		if testVA.Inflow != 1000.0 {
			t.Errorf("Month %d: Expected virtual account Inflow to be 1000.0, got %f", monthIdx, testVA.Inflow)
		}
		if testVA.Outflow != 200.0 {
			t.Errorf("Month %d: Expected virtual account Outflow to be 200.0, got %f", monthIdx, testVA.Outflow)
		}
		expectedBalance := 800.0
		if testVA.Balance != expectedBalance {
			t.Errorf("Month %d: Expected virtual account Balance to be %f, got %f", monthIdx, expectedBalance, testVA.Balance)
		}

		if unassignedVA != nil {
			if unassignedVA.StartingBalance != 0.0 {
				t.Errorf("Month %d: Expected unassigned virtual account StartingBalance to be 0.0, got %f", monthIdx, unassignedVA.StartingBalance)
			}
			if unassignedVA.Balance != 0.0 {
				t.Errorf("Month %d: Expected unassigned virtual account Balance to be 0.0, got %f", monthIdx, unassignedVA.Balance)
			}
		}
	}
}
