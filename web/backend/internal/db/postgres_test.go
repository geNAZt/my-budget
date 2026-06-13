package db

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
)

func TestMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://budget:budgetpass@localhost:5432/budget?sslmode=disable"
	}

	// 1. Connect to the database
	db, err := sql.Open("postgres-compat", dsn)
	if err != nil {
		t.Fatalf("failed to open database connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("Skipping integration migration test (Postgres not accessible: %v)", err)
	}

	// 2. Create a temporary schema for isolation
	schemaName := "test_migrations_temp"
	_, err = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
	if err != nil {
		t.Fatalf("failed to drop old temp schema: %v", err)
	}
	_, err = db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create temp schema: %v", err)
	}
	defer func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
	}()

	// 3. Set search path to the temp schema so migrations run there
	_, err = db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}

	// 4. Create base tables
	// Normally InitDB runs Exec(schema). We need to run it so table structures are initialized.
	// We can use a test schema or just run the base schema.
	// Let's get the base schema from postgres.go.
	// Since schema string isn't exported, we can just run a minimal set of base tables or run InitDB's logic.
	// Let's write a small helper or just export/recreate the table creation.
	// Actually, wait, does postgres.go's InitDB run Exec(schema)? Yes!
	// But InitDB also sets max connections and pings, and then runs migrations.
	// We want to test the entire migration sequence on a fresh DB using our migrations slice.
	// Let's define the base tables that migrations expect. Since migrations ALTER these tables, we must create them.
	// Let's execute the exact same table creations as postgres.go's InitDB.
	// Instead of copy-pasting the schema string, can we just call InitDB with a schema override, or can we copy the schema string?
	// Let's copy the base schema string from postgres.go to ensure the test starts with the exact initial schema.
	baseSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        dashboard_scenario_id TEXT,
        dashboard_month_offset INTEGER DEFAULT 0,
        recovery_hash TEXT,
        timezone TEXT DEFAULT 'UTC'
	);

	CREATE TABLE IF NOT EXISTS authenticators (
		id BYTEA PRIMARY KEY,
		user_id TEXT,
		credential_json TEXT,
		name TEXT DEFAULT '',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS webauthn_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		session_data TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

    CREATE TABLE IF NOT EXISTS incomes (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        pool_id TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS income_versions (
        id TEXT PRIMARY KEY,
        income_id TEXT,
        amount DOUBLE PRECISION,
        start_date TIMESTAMP,
        end_date TIMESTAMP,
        interval_months INTEGER,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        stop_modification_id TEXT,
        interval_increase_percentage DOUBLE PRECISION DEFAULT 0,
        interval_increase_months INTEGER DEFAULT 0,
        interval_increase_start_date TIMESTAMP,
        FOREIGN KEY(income_id) REFERENCES incomes(id)
    );

    CREATE TABLE IF NOT EXISTS bills (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        pool_id TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS bill_versions (
        id TEXT PRIMARY KEY,
        bill_id TEXT,
        amount DOUBLE PRECISION,
        start_date TIMESTAMP,
        end_date TIMESTAMP,
        interval_months INTEGER,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(bill_id) REFERENCES bills(id)
    );

    CREATE TABLE IF NOT EXISTS expenses (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        pool_id TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS expense_versions (
        id TEXT PRIMARY KEY,
        expense_id TEXT,
        amount DOUBLE PRECISION,
        due_date TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(expense_id) REFERENCES expenses(id)
    );

    CREATE TABLE IF NOT EXISTS sub_expenses (
        id TEXT PRIMARY KEY,
        expense_version_id TEXT,
        description TEXT,
        amount DOUBLE PRECISION,
        metadata TEXT,
        FOREIGN KEY(expense_version_id) REFERENCES expense_versions(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS loans (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        pool_id TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS loan_versions (
        id TEXT PRIMARY KEY,
        loan_id TEXT,
        amount_lent DOUBLE PRECISION,
        interest_rate DOUBLE PRECISION,
        runtime_months INTEGER,
        start_date TIMESTAMP,
        remainder_start_date TIMESTAMP,
        priority DOUBLE PRECISION DEFAULT 0,
        next_loan_id TEXT,
        balloon_leftover DOUBLE PRECISION DEFAULT 0,
        is_interest_only BOOLEAN DEFAULT FALSE,
        early_payoff_penalty DOUBLE PRECISION DEFAULT 1,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(loan_id) REFERENCES loans(id),
        FOREIGN KEY(next_loan_id) REFERENCES loans(id)
    );

    CREATE TABLE IF NOT EXISTS assets (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        pool_id TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS asset_versions (
        id TEXT PRIMARY KEY,
        asset_id TEXT,
        type TEXT,
        target_value TEXT,
        dumping_loan_id TEXT,
        stop_modification_id TEXT,
        interest_rate DOUBLE PRECISION,
        interest_interval TEXT,
        amount_per_month DOUBLE PRECISION,
        remainder_start_date TIMESTAMP,
        start_date TIMESTAMP,
        end_date TIMESTAMP,
        withdrawal_penalty DOUBLE PRECISION DEFAULT 0,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(asset_id) REFERENCES assets(id),
        FOREIGN KEY(dumping_loan_id) REFERENCES loans(id)
    );

    CREATE TABLE IF NOT EXISTS modifications (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        target_id TEXT,
        target_type TEXT,
        description TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS modification_versions (
        id TEXT PRIMARY KEY,
        modification_id TEXT,
        amount DOUBLE PRECISION,
        withdrawal_percentage DOUBLE PRECISION DEFAULT 0,
        start_date TIMESTAMP,
        end_date TIMESTAMP,
        interval_months INTEGER,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(modification_id) REFERENCES modifications(id)
    );

    CREATE TABLE IF NOT EXISTS external_cache (
        key TEXT PRIMARY KEY,
        data TEXT,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    CREATE TABLE IF NOT EXISTS integrations (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        service_type TEXT,
        name TEXT,
        encrypted_config TEXT,
        status TEXT DEFAULT 'AWAITING_AUTH',
        last_sync_at TIMESTAMP,
        sync_interval_seconds INTEGER DEFAULT 21600,
        last_error TEXT,
        cached_balance DOUBLE PRECISION DEFAULT 0,
        backoff_until TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS transaction_pools (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        parent_id TEXT,
        name TEXT,
        color TEXT,
        is_hidden BOOLEAN DEFAULT FALSE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id),
        FOREIGN KEY(parent_id) REFERENCES transaction_pools(id) ON DELETE SET NULL
    );

    CREATE TABLE IF NOT EXISTS transaction_rules (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        parent_id TEXT,
        integration_id TEXT,
        target_pool_id TEXT,
        operator TEXT DEFAULT 'NONE',
        field TEXT DEFAULT 'NONE',
        regex TEXT DEFAULT '',
        amount_operator TEXT DEFAULT '',
        amount_value DOUBLE PRECISION DEFAULT 0,
        priority INTEGER DEFAULT 0,
        negate BOOLEAN DEFAULT FALSE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id),
        FOREIGN KEY(parent_id) REFERENCES transaction_rules(id) ON DELETE CASCADE,
        FOREIGN KEY(target_pool_id) REFERENCES transaction_pools(id)
    );

	CREATE TABLE IF NOT EXISTS bank_transactions (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		integration_id TEXT,
		account_id TEXT DEFAULT '',
		source_account_id TEXT DEFAULT '',
		destination_account_id TEXT DEFAULT '',
		pool_id TEXT,
		tags TEXT DEFAULT '',
		external_id TEXT,
		encrypted_data TEXT,
		linked_transaction_id TEXT,
		is_link_confirmed BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP,
		synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		correlation_id TEXT,
		is_deleted BOOLEAN DEFAULT FALSE,
		denied_duplicate_ids TEXT DEFAULT '',
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(integration_id) REFERENCES integrations(id),
		FOREIGN KEY(pool_id) REFERENCES transaction_pools(id),
		UNIQUE(user_id, external_id)
	);

	CREATE TABLE IF NOT EXISTS bank_transaction_pools (
		transaction_id TEXT NOT NULL,
		pool_id TEXT NOT NULL,
		PRIMARY KEY(transaction_id, pool_id),
		FOREIGN KEY(transaction_id) REFERENCES bank_transactions(id) ON DELETE CASCADE,
		FOREIGN KEY(pool_id) REFERENCES transaction_pools(id) ON DELETE CASCADE
	);

    CREATE TABLE IF NOT EXISTS scenarios (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        description TEXT,
        projection_months INTEGER DEFAULT 360,
        is_active BOOLEAN DEFAULT FALSE,
        month_start_day INTEGER DEFAULT 1,
        start_date TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        simulations INTEGER DEFAULT 50000,
        sim_years INTEGER DEFAULT 10,
        sim_percent DOUBLE PRECISION DEFAULT 50,
        lookback_years INTEGER DEFAULT 0,
        passive_income_percentage DOUBLE PRECISION DEFAULT 3.5,
        mc_implementation TEXT DEFAULT 'STANDARD',
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS integration_key_slots (
        integration_id TEXT,
        authenticator_id BYTEA,
        encrypted_key TEXT,
        PRIMARY KEY(integration_id, authenticator_id),
        FOREIGN KEY(integration_id) REFERENCES integrations(id),
        FOREIGN KEY(authenticator_id) REFERENCES authenticators(id)
    );

    CREATE TABLE IF NOT EXISTS scenario_entities (
        scenario_id TEXT,
        entity_id TEXT,
        entity_type TEXT,
        version_id TEXT,
        PRIMARY KEY(scenario_id, entity_id),
        FOREIGN KEY(scenario_id) REFERENCES scenarios(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS execution_plans (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        code TEXT,
        trigger_type TEXT,
        trigger_value TEXT,
        is_enabled BOOLEAN DEFAULT TRUE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS connections (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        value TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS connection_key_slots (
        connection_id TEXT,
        authenticator_id BYTEA,
        encrypted_key TEXT,
        PRIMARY KEY(connection_id, authenticator_id),
        FOREIGN KEY(connection_id) REFERENCES connections(id) ON DELETE CASCADE,
        FOREIGN KEY(authenticator_id) REFERENCES authenticators(id)
    );

    CREATE TABLE IF NOT EXISTS execution_logs (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        plan_id TEXT,
        status TEXT,
        stdout TEXT,
        stderr TEXT,
        exit_code INTEGER,
        started_at TIMESTAMP,
        finished_at TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id),
        FOREIGN KEY(plan_id) REFERENCES execution_plans(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS virtual_accounts (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        is_deleted BOOLEAN DEFAULT FALSE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(user_id) REFERENCES users(id)
    );

    CREATE TABLE IF NOT EXISTS virtual_account_versions (
        id TEXT PRIMARY KEY,
        virtual_account_id TEXT,
        color TEXT DEFAULT '#6366f1',
        starting_balance DOUBLE PRECISION DEFAULT 0,
        description TEXT DEFAULT '',
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(virtual_account_id) REFERENCES virtual_accounts(id)
    );

    CREATE TABLE IF NOT EXISTS time_slices (
        id TEXT PRIMARY KEY,
        version_id TEXT,
        entity_type TEXT, -- 'BILL', 'EXPENSE', 'INCOME'
        amount DOUBLE PRECISION,
        interval_months INTEGER,
        start_date TIMESTAMP,
        end_date TIMESTAMP,
        description TEXT DEFAULT '',
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    CREATE TABLE IF NOT EXISTS asset_version_etf_configs (
        id TEXT PRIMARY KEY,
        asset_version_id TEXT NOT NULL,
        tracker TEXT DEFAULT '',
        historical_tracker TEXT DEFAULT '',
        conversion_tracker TEXT DEFAULT '',
        history_provider TEXT DEFAULT '',
        percentage DOUBLE PRECISION DEFAULT 0.0,
        ter DOUBLE PRECISION DEFAULT 0.0,
        FOREIGN KEY(asset_version_id) REFERENCES asset_versions(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS asset_version_etf_stitching_segments (
        id TEXT PRIMARY KEY,
        etf_config_id TEXT NOT NULL,
        provider TEXT DEFAULT '',
        lookup_ticker TEXT DEFAULT '',
        conversion_tracker TEXT DEFAULT '',
        sort_order INTEGER DEFAULT 0,
        FOREIGN KEY(etf_config_id) REFERENCES asset_version_etf_configs(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS asset_version_penalties (
        id TEXT PRIMARY KEY,
        asset_version_id TEXT NOT NULL,
        name TEXT DEFAULT '',
        trigger_type TEXT DEFAULT '',
        percentage DOUBLE PRECISION DEFAULT 0.0,
        FOREIGN KEY(asset_version_id) REFERENCES asset_versions(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS asset_version_sub_assets (
        id TEXT PRIMARY KEY,
        asset_version_id TEXT NOT NULL,
        sub_asset_id TEXT NOT NULL,
        name TEXT DEFAULT '',
        target_value TEXT DEFAULT '',
        amount_per_month DOUBLE PRECISION DEFAULT 0.0,
        is_remainder_consumer BOOLEAN DEFAULT FALSE,
        remainder_start_date TIMESTAMP,
        dumping_loan_id TEXT,
        start_date TIMESTAMP NOT NULL,
        end_date TIMESTAMP,
        earliest_dump_date TIMESTAMP,
        expense_id TEXT,
        FOREIGN KEY(asset_version_id) REFERENCES asset_versions(id) ON DELETE CASCADE,
        FOREIGN KEY(dumping_loan_id) REFERENCES loans(id) ON DELETE SET NULL,
        FOREIGN KEY(expense_id) REFERENCES expenses(id) ON DELETE SET NULL
    );

    CREATE TABLE IF NOT EXISTS scenario_remainder_orders (
        scenario_id TEXT NOT NULL,
        entity_id TEXT NOT NULL,
        position INTEGER NOT NULL,
        PRIMARY KEY(scenario_id, position),
        FOREIGN KEY(scenario_id) REFERENCES scenarios(id) ON DELETE CASCADE
    );

    CREATE TABLE IF NOT EXISTS scenario_etf_params (
        scenario_id TEXT NOT NULL,
        ticker TEXT NOT NULL DEFAULT '',
        simulations INTEGER DEFAULT 0,
        sim_years INTEGER DEFAULT 0,
        sim_percent DOUBLE PRECISION DEFAULT 0.0,
        lookback_years INTEGER DEFAULT 0,
        PRIMARY KEY(scenario_id, ticker),
        FOREIGN KEY(scenario_id) REFERENCES scenarios(id) ON DELETE CASCADE
    );
	`
	_, err = db.Exec(baseSchema)
	if err != nil {
		t.Fatalf("failed to run base schema: %v", err)
	}

	// 5. Run migrations for the first time
	err = runMigrations(db)
	if err != nil {
		t.Fatalf("runMigrations failed on fresh schema: %v", err)
	}

	// 6. Verify migrations table exists and lists all migrations
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	defer rows.Close()

	var recorded []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("failed to scan version: %v", err)
		}
		recorded = append(recorded, v)
	}

	expectedCount := len(migrations)
	if len(recorded) != expectedCount {
		t.Errorf("expected %d migrations recorded, got %d: %v", expectedCount, len(recorded), recorded)
	}

	// 7. Run migrations a second time (should do nothing and skip all)
	err = runMigrations(db)
	if err != nil {
		t.Fatalf("runMigrations failed on second run: %v", err)
	}
}
