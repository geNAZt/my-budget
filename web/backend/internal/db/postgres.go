package db

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/hkdf"
)

func init() {
	sql.Register("postgres-compat", &compatDriver{parent: &pq.Driver{}})
}

type compatDriver struct {
	parent driver.Driver
}

func (d *compatDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.parent.Open(name)
	if err != nil {
		return nil, err
	}
	return &compatConn{conn}, nil
}

type compatConn struct {
	driver.Conn
}

func (c *compatConn) Prepare(query string) (driver.Stmt, error) {
	q := rebind(query)
	stmt, err := c.Conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	return &compatStmt{stmt, q}, nil
}

func (c *compatConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	q := rebind(query)
	if prepareCtx, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err := prepareCtx.PrepareContext(ctx, q)
		if err != nil {
			return nil, err
		}
		return &compatStmt{stmt, q}, nil
	}
	return c.Prepare(q)
}

func (c *compatConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q := rebind(query)
	if queryerCtx, ok := c.Conn.(driver.QueryerContext); ok {
		return queryerCtx.QueryContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

func (c *compatConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	q := rebind(query)
	if execerCtx, ok := c.Conn.(driver.ExecerContext); ok {
		return execerCtx.ExecContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}

type compatStmt struct {
	driver.Stmt
	query string
}

func (s *compatStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if stmtQueryCtx, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return stmtQueryCtx.QueryContext(ctx, args)
	}
	vals := make([]driver.Value, len(args))
	for i, arg := range args {
		vals[i] = arg.Value
	}
	return s.Query(vals)
}

func (s *compatStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if stmtExecCtx, ok := s.Stmt.(driver.StmtExecContext); ok {
		return stmtExecCtx.ExecContext(ctx, args)
	}
	vals := make([]driver.Value, len(args))
	for i, arg := range args {
		vals[i] = arg.Value
	}
	return s.Exec(vals)
}

func rebind(query string) string {
	var sb strings.Builder
	paramIdx := 1
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false

	for i := 0; i < len(query); i++ {
		char := query[i]
		switch char {
		case '\'':
			if !inDoubleQuote && !inBacktick {
				inSingleQuote = !inSingleQuote
			}
			sb.WriteByte(char)
		case '"':
			if !inSingleQuote && !inBacktick {
				inDoubleQuote = !inDoubleQuote
			}
			sb.WriteByte(char)
		case '`':
			if !inSingleQuote && !inDoubleQuote {
				inBacktick = !inBacktick
			}
			sb.WriteByte(char)
		case '?':
			if !inSingleQuote && !inDoubleQuote && !inBacktick {
				sb.WriteString(fmt.Sprintf("$%d", paramIdx))
				paramIdx++
			} else {
				sb.WriteByte(char)
			}
		default:
			sb.WriteByte(char)
		}
	}
	return sb.String()
}

func BackupDB(dsn string, dataDir string) {
	log.Printf("[DB] Starting database backup...")

	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		log.Printf("[DB] Failed to create backup directory: %v", err)
		return
	}

	// pg_dump -d <dsn> -f <file>
	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("backup-%s.sql", timestamp))

	cmd := exec.Command("pg_dump", "-d", dsn, "-f", backupFile)
	// pg_dump might need PGPASSWORD if it's not in the DSN,
	// but DATABASE_URL usually contains it.

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[DB] Backup failed: %v\nOutput: %s", err, string(output))
		return
	}

	log.Printf("[DB] Backup successful: %s", backupFile)

	// Clean up old backups (keep last 30)
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	type fileInfo struct {
		name string
		time time.Time
	}
	var backups []fileInfo
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "backup-") && strings.HasSuffix(f.Name(), ".sql") {
			info, err := f.Info()
			if err == nil {
				backups = append(backups, fileInfo{f.Name(), info.ModTime()})
			}
		}
	}

	if len(backups) > 30 {
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].time.After(backups[j].time)
		})

		for i := 30; i < len(backups); i++ {
			os.Remove(filepath.Join(backupDir, backups[i].name))
			log.Printf("[DB] Removed old backup: %s", backups[i].name)
		}
	}
}

func InitDB(dsn string) (*sql.DB, error) {
	log.Printf("[DB] Opening database...")
	db, err := sql.Open("postgres-compat", dsn)
	if err != nil {
		return nil, err
	}

	// Set connection pool limits
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Printf("[DB] Ping failed: %v", err)
		return nil, err
	}

	// Base tables in PostgreSQL-native format and topological order.
	schema := `
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

	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		return nil, err
	}

	return db, nil
}

func hasColumn(db *sql.DB, table, column string) bool {
	var name string
	query := "SELECT column_name FROM information_schema.columns WHERE table_name = $1 AND column_name = $2 AND table_schema = current_schema()"
	err := db.QueryRow(query, table, column).Scan(&name)
	if err != nil {
		return false
	}
	return name == column
}

func columnDataType(db *sql.DB, table, column string) string {
	var dataType string
	query := "SELECT data_type FROM information_schema.columns WHERE table_name = $1 AND column_name = $2 AND table_schema = current_schema()"
	if err := db.QueryRow(query, table, column).Scan(&dataType); err != nil {
		return ""
	}
	return dataType
}

func ensureBooleanColumn(db *sql.DB, table, column string, defaultValue bool) {
	if !hasColumn(db, table, column) {
		return
	}
	if columnDataType(db, table, column) == "boolean" {
		return
	}

	defaultSQL := "FALSE"
	if defaultValue {
		defaultSQL = "TRUE"
	}
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", table, column))
	if err != nil {
		log.Printf("[DB] Failed to drop default for %s.%s before BOOLEAN conversion: %v", table, column, err)
		return
	}
	_, err = db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ALTER COLUMN %s TYPE BOOLEAN USING CASE WHEN %s IS NULL THEN NULL WHEN %s::text IN ('1', 'true', 't', 'yes') THEN TRUE ELSE FALSE END",
		table, column, column, column,
	))
	if err != nil {
		log.Printf("[DB] Failed to convert %s.%s to BOOLEAN: %v", table, column, err)
		return
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", table, column, defaultSQL))
	if err != nil {
		log.Printf("[DB] Failed to set default for %s.%s: %v", table, column, err)
	}
}

func ensurePostgresTypes(db *sql.DB) {
	booleanColumns := map[string][]string{
		"incomes":           {"is_deleted"},
		"bills":             {"is_deleted"},
		"expenses":          {"is_deleted"},
		"loans":             {"is_deleted"},
		"loan_versions":     {"is_interest_only"},
		"assets":            {"is_deleted"},
		"modifications":     {"is_deleted"},
		"transaction_pools": {"is_hidden"},
		"transaction_rules": {"negate"},
		"scenarios":         {"is_active", "is_deleted"},
	}

	for table, columns := range booleanColumns {
		for _, column := range columns {
			ensureBooleanColumn(db, table, column, false)
		}
	}
}

type Migration struct {
	ID  string
	Run func(*sql.DB) error
}

var migrations = []Migration{
	{
		ID: "001_ensure_postgres_types",
		Run: func(db *sql.DB) error {
			ensurePostgresTypes(db)
			return nil
		},
	},
	{
		ID: "002_users_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "users", "dashboard_scenario_id") {
				if _, err := db.Exec("ALTER TABLE users ADD COLUMN dashboard_scenario_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "users", "dashboard_month_offset") {
				if _, err := db.Exec("ALTER TABLE users ADD COLUMN dashboard_month_offset INTEGER DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "users", "recovery_hash") {
				if _, err := db.Exec("ALTER TABLE users ADD COLUMN recovery_hash TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "003_income_versions_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "income_versions", "stop_modification_id") {
				if _, err := db.Exec("ALTER TABLE income_versions ADD COLUMN stop_modification_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "income_versions", "interval_increase_percentage") {
				if _, err := db.Exec("ALTER TABLE income_versions ADD COLUMN interval_increase_percentage DOUBLE PRECISION DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "income_versions", "interval_increase_months") {
				if _, err := db.Exec("ALTER TABLE income_versions ADD COLUMN interval_increase_months INTEGER DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "income_versions", "interval_increase_start_date") {
				if _, err := db.Exec("ALTER TABLE income_versions ADD COLUMN interval_increase_start_date TIMESTAMP"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "004_loan_versions_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "loan_versions", "early_payoff_penalty") {
				if _, err := db.Exec("ALTER TABLE loan_versions ADD COLUMN early_payoff_penalty DOUBLE PRECISION DEFAULT 1"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "loan_versions", "next_loan_id") {
				if _, err := db.Exec("ALTER TABLE loan_versions ADD COLUMN next_loan_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "loan_versions", "remainder_start_date") {
				if _, err := db.Exec("ALTER TABLE loan_versions ADD COLUMN remainder_start_date TIMESTAMP"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "005_asset_versions_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "asset_versions", "dumping_loan_id") {
				if _, err := db.Exec("ALTER TABLE asset_versions ADD COLUMN dumping_loan_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "asset_versions", "stop_modification_id") {
				if _, err := db.Exec("ALTER TABLE asset_versions ADD COLUMN stop_modification_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "asset_versions", "withdrawal_penalty") {
				if _, err := db.Exec("ALTER TABLE asset_versions ADD COLUMN withdrawal_penalty DOUBLE PRECISION DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "asset_versions", "remainder_start_date") {
				if _, err := db.Exec("ALTER TABLE asset_versions ADD COLUMN remainder_start_date TIMESTAMP"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "006_asset_version_sub_assets_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "asset_version_sub_assets", "expense_id") {
				if _, err := db.Exec("ALTER TABLE asset_version_sub_assets ADD COLUMN expense_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "007_etf_stitching_segments",
		Run: func(db *sql.DB) error {
			_, err := db.Exec(`
				CREATE TABLE IF NOT EXISTS asset_version_etf_stitching_segments (
					id TEXT PRIMARY KEY,
					etf_config_id TEXT NOT NULL,
					provider TEXT DEFAULT '',
					lookup_ticker TEXT DEFAULT '',
					conversion_tracker TEXT DEFAULT '',
					sort_order INTEGER DEFAULT 0,
					FOREIGN KEY(etf_config_id) REFERENCES asset_version_etf_configs(id) ON DELETE CASCADE
				)
			`)
			if err != nil {
				return err
			}

			if !hasColumn(db, "asset_version_etf_stitching_segments", "conversion_tracker") {
				if _, err = db.Exec("ALTER TABLE asset_version_etf_stitching_segments ADD COLUMN conversion_tracker TEXT DEFAULT ''"); err != nil {
					return err
				}
			}

			// Migrate data from stitching_segments_json if it exists
			if hasColumn(db, "asset_version_etf_configs", "stitching_segments_json") {
				log.Printf("[DB] Found old stitching_segments_json column. Migrating stitching segments...")
				type dbSegment struct {
					Provider     string `json:"provider"`
					LookupTicker string `json:"lookup_ticker"`
				}
				rows, err := db.Query("SELECT id, stitching_segments_json FROM asset_version_etf_configs WHERE stitching_segments_json IS NOT NULL AND stitching_segments_json != ''")
				if err != nil {
					return err
				}
				defer rows.Close()

				var updates []struct {
					ConfigID string
					Segments []dbSegment
				}
				for rows.Next() {
					var configID string
					var jsonStr string
					if err := rows.Scan(&configID, &jsonStr); err == nil {
						var segs []dbSegment
						if err := json.Unmarshal([]byte(jsonStr), &segs); err == nil && len(segs) > 0 {
							updates = append(updates, struct {
								ConfigID string
								Segments []dbSegment
							}{configID, segs})
						}
					}
				}

				for _, update := range updates {
					// Avoid duplicate migration
					var count int
					_ = db.QueryRow("SELECT COUNT(*) FROM asset_version_etf_stitching_segments WHERE etf_config_id = $1", update.ConfigID).Scan(&count)
					if count == 0 {
						for i, seg := range update.Segments {
							_, err = db.Exec(`
								INSERT INTO asset_version_etf_stitching_segments (id, etf_config_id, provider, lookup_ticker, sort_order)
								VALUES ($1, $2, $3, $4, $5)`,
								uuid.New().String(), update.ConfigID, seg.Provider, seg.LookupTicker, i)
							if err != nil {
								return err
							}
						}
					}
				}
				log.Printf("[DB] Migrated stitching segments for %d ETF configurations.", len(updates))

				// Now drop the column so it's clean
				_, err = db.Exec("ALTER TABLE asset_version_etf_configs DROP COLUMN stitching_segments_json")
				if err != nil {
					log.Printf("[DB Warning] Failed to drop stitching_segments_json column: %v", err)
				} else {
					log.Printf("[DB] Dropped stitching_segments_json column.")
				}
			}
			return nil
		},
	},
	{
		ID: "008_authenticators_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "authenticators", "credential_json") {
				if _, err := db.Exec("ALTER TABLE authenticators ADD COLUMN credential_json TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "authenticators", "name") {
				if _, err := db.Exec("ALTER TABLE authenticators ADD COLUMN name TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "authenticators", "created_at") {
				if _, err := db.Exec("ALTER TABLE authenticators ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "009_scenarios_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "scenarios", "month_start_day") {
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN month_start_day INTEGER DEFAULT 1"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "scenarios", "simulations") {
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN simulations INTEGER DEFAULT 50000"); err != nil {
					return err
				}
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN sim_years INTEGER DEFAULT 10"); err != nil {
					return err
				}
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN sim_percent DOUBLE PRECISION DEFAULT 50"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "scenarios", "lookback_years") {
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN lookback_years INTEGER DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "scenarios", "passive_income_percentage") {
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN passive_income_percentage DOUBLE PRECISION DEFAULT 3.5"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "scenarios", "mc_implementation") {
				if _, err := db.Exec("ALTER TABLE scenarios ADD COLUMN mc_implementation TEXT DEFAULT 'STANDARD'"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "010_integrations_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "integrations", "name") {
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN name TEXT DEFAULT ''"); err != nil {
					return err
				}
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN status TEXT DEFAULT 'AWAITING_AUTH'"); err != nil {
					return err
				}
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN last_sync_at TIMESTAMP"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "integrations", "sync_interval_seconds") {
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN sync_interval_seconds INTEGER DEFAULT 21600"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "integrations", "last_error") {
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN last_error TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "integrations", "cached_balance") {
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN cached_balance DOUBLE PRECISION DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "integrations", "backoff_until") {
				if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN backoff_until TIMESTAMP"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "011_transaction_pools_parent_id",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "transaction_pools", "parent_id") {
				if _, err := db.Exec("ALTER TABLE transaction_pools ADD COLUMN parent_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "012_transaction_rules_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "transaction_rules", "parent_id") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN parent_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "operator") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN operator TEXT DEFAULT 'NONE'"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "field") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN field TEXT DEFAULT 'NONE'"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "regex") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN regex TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "amount_operator") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN amount_operator TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "amount_value") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN amount_value DOUBLE PRECISION DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "priority") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN priority INTEGER DEFAULT 0"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "transaction_rules", "negate") {
				if _, err := db.Exec("ALTER TABLE transaction_rules ADD COLUMN negate BOOLEAN DEFAULT FALSE"); err != nil {
					return err
				}
			}

			_, _ = db.Exec("UPDATE transaction_rules SET operator = 'NONE' WHERE operator IS NULL OR operator = ''")
			_, _ = db.Exec("UPDATE transaction_rules SET field = 'NONE' WHERE field IS NULL OR field = ''")
			_, _ = db.Exec("UPDATE transaction_rules SET regex = '' WHERE regex IS NULL")
			_, _ = db.Exec("UPDATE transaction_rules SET amount_operator = '' WHERE amount_operator IS NULL")
			_, _ = db.Exec("UPDATE transaction_rules SET parent_id = NULL WHERE parent_id = ''")
			_, _ = db.Exec("UPDATE transaction_rules SET integration_id = NULL WHERE integration_id = ''")
			_, _ = db.Exec("UPDATE transaction_rules SET target_pool_id = NULL WHERE target_pool_id = ''")
			return nil
		},
	},
	{
		ID: "013_bank_transactions_enhancements",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "bank_transactions", "pool_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN pool_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "account_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN account_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "source_account_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN source_account_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "destination_account_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN destination_account_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "synced_at") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "tags") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN tags TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "linked_transaction_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN linked_transaction_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "is_link_confirmed") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN is_link_confirmed BOOLEAN DEFAULT FALSE"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "correlation_id") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN correlation_id TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "is_deleted") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN is_deleted BOOLEAN DEFAULT FALSE"); err != nil {
					return err
				}
			}
			if !hasColumn(db, "bank_transactions", "denied_duplicate_ids") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN denied_duplicate_ids TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "014_bank_transactions_unique_idx",
		Run: func(db *sql.DB) error {
			_, err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_bank_transactions_user_external ON bank_transactions(user_id, external_id) WHERE external_id != ''")
			return err
		},
	},
	{
		ID: "015_bank_transactions_internal_status",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "bank_transactions", "internal_status") {
				if _, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN internal_status TEXT DEFAULT ''"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "016_modification_assets",
		Run: func(db *sql.DB) error {
			if !hasColumn(db, "modification_versions", "withdrawal_percentage") {
				if _, err := db.Exec("ALTER TABLE modification_versions ADD COLUMN withdrawal_percentage DOUBLE PRECISION DEFAULT 0"); err != nil {
					return err
				}
			}

			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS modification_assets (
				modification_id TEXT,
				asset_id TEXT,
				PRIMARY KEY (modification_id, asset_id),
				FOREIGN KEY(modification_id) REFERENCES modifications(id),
				FOREIGN KEY(asset_id) REFERENCES assets(id)
			)`)
			if err != nil {
				return err
			}

			_, err = db.Exec(`INSERT INTO modification_assets (modification_id, asset_id)
				SELECT id, target_id FROM modifications
				WHERE target_type = 'ASSET' AND target_id IS NOT NULL AND target_id != ''
				ON CONFLICT DO NOTHING`)
			return err
		},
	},
	{
		ID: "017_entity_pool_account_links_and_timezone",
		Run: func(db *sql.DB) error {
			entities := []string{"incomes", "bills", "expenses", "assets", "loans"}
			for _, ent := range entities {
				if !hasColumn(db, ent, "pool_id") {
					if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN pool_id TEXT DEFAULT ''", ent)); err != nil {
						return err
					}
				}
				if !hasColumn(db, ent, "account_id") {
					if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN account_id TEXT DEFAULT ''", ent)); err != nil {
						return err
					}
				}
			}
			if !hasColumn(db, "users", "timezone") {
				if _, err := db.Exec("ALTER TABLE users ADD COLUMN timezone TEXT DEFAULT 'UTC'"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		ID: "018_bank_transaction_pools_migration",
		Run: func(db *sql.DB) error {
			_, err := db.Exec(`INSERT INTO bank_transaction_pools (transaction_id, pool_id)
				SELECT id, pool_id FROM bank_transactions
				WHERE pool_id IS NOT NULL AND pool_id != ''
				ON CONFLICT DO NOTHING`)
			return err
		},
	},
	{
		ID: "019_migrate_rules_to_global",
		Run: func(db *sql.DB) error {
			MigrateRulesToGlobal(db)
			return nil
		},
	},
	{
		ID: "020_migrate_virtual_accounts_to_multi",
		Run: func(db *sql.DB) error {
			MigrateVirtualAccountsToMulti(db)
			return nil
		},
	},
	{
		ID: "021_normalize_json_columns",
		Run: func(db *sql.DB) error {
			NormalizeJSONColumns(db)
			return nil
		},
	},
	{
		ID: "022_restore_expired_bank_transactions_upct_bug",
		Run: func(db *sql.DB) error {
			if _, err := db.Exec("UPDATE bank_transactions SET is_deleted = FALSE, internal_status = '' WHERE is_deleted = TRUE AND internal_status = 'EXPIRED_REJECTION'"); err != nil {
				return err
			}
			return nil
		},
	},
	{
		ID: "023_fix_upct_posd_transitions_from_logs_v2",
		Run: func(db *sql.DB) error {
			return FixUPCTTransitions(db)
		},
	},
	{
		ID: "024_fix_upct_posd_transitions_from_all_logs",
		Run: func(db *sql.DB) error {
			return FixUPCTTransitionsV3(db)
		},
	},
	{
		ID: "025_create_account_balance_history",
		Run: func(db *sql.DB) error {
			var exists bool
			err := db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'account_balance_history' AND table_schema = current_schema())").Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				_, err = db.Exec(`
					CREATE TABLE account_balance_history (
						id TEXT PRIMARY KEY,
						user_id TEXT NOT NULL DEFAULT '',
						integration_id TEXT NOT NULL DEFAULT '',
						account_id TEXT NOT NULL DEFAULT '',
						balance DOUBLE PRECISION NOT NULL DEFAULT 0,
						recorded_at TIMESTAMP NOT NULL,
						FOREIGN KEY(user_id) REFERENCES users(id),
						FOREIGN KEY(integration_id) REFERENCES integrations(id)
					)
				`)
				if err != nil {
					return err
				}
				_, err = db.Exec("CREATE INDEX idx_account_balance_history_acc_date ON account_balance_history (account_id, recorded_at)")
				return err
			}
			return nil
		},
	},
}

func runMigrations(db *sql.DB) error {
	log.Printf("[DB] Running migrations...")
	// 1. Create schema_migrations table if not exists
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// 2. Fetch already run migrations
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("failed to query schema_migrations: %w", err)
	}
	defer rows.Close()

	runVersions := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
		runVersions[v] = true
	}

	// 3. Run migrations sequentially
	for _, migration := range migrations {
		if runVersions[migration.ID] {
			continue
		}

		log.Printf("[DB] Running migration %s...", migration.ID)
		if err := migration.Run(db); err != nil {
			return fmt.Errorf("migration %s failed: %w", migration.ID, err)
		}

		// Record migration as run
		_, err = db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", migration.ID)
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", migration.ID, err)
		}
		log.Printf("[DB] Migration %s completed successfully.", migration.ID)
	}

	return nil
}

func MigrateRulesToGlobal(db *sql.DB) {
	log.Printf("[DB] Running rule migration to global rules with DATA_CHAIN parameter...")

	type ruleRow struct {
		id             string
		userID         string
		parentID       sql.NullString
		integrationID  sql.NullString
		targetPoolID   sql.NullString
		operator       string
		field          string
		regex          string
		amountOperator string
		amountValue    float64
		priority       int
		negate         bool
	}

	rows, err := db.Query(`
		SELECT id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate
		FROM transaction_rules
		WHERE integration_id IS NOT NULL AND integration_id != ''`)
	if err != nil {
		log.Printf("[DB] Rule migration query failed: %v", err)
		return
	}
	defer rows.Close()

	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		err := rows.Scan(
			&r.id, &r.userID, &r.parentID, &r.integrationID, &r.targetPoolID,
			&r.operator, &r.field, &r.regex, &r.amountOperator, &r.amountValue,
			&r.priority, &r.negate,
		)
		if err == nil {
			rules = append(rules, r)
		}
	}

	for _, r := range rules {
		log.Printf("[DB] Migrating rule %s (tied to integration %s)", r.id, r.integrationID.String)

		// Let's run inside a transaction
		tx, err := db.Begin()
		if err != nil {
			log.Printf("[DB] Migration transaction failed: %v", err)
			continue
		}

		if r.operator == "AND" {
			// Simply insert the DATA_CHAIN child
			childID := uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
				VALUES (?, ?, ?, NULL, NULL, 'NONE', 'DATA_CHAIN', ?, '', 0, ?, FALSE)`,
				childID, r.userID, r.id, r.integrationID.String, r.priority)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to insert DATA_CHAIN child: %v", err)
				continue
			}

			// Clear integration_id on parent
			_, err = tx.Exec("UPDATE transaction_rules SET integration_id = NULL WHERE id = ?", r.id)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to clear integration_id: %v", err)
				continue
			}
		} else if r.operator == "OR" {
			// Wrap OR rule:
			// 1. Insert C_or rule which represents the original OR rule
			cOrID := uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
				VALUES (?, ?, ?, NULL, NULL, 'OR', 'NONE', '', '', 0, ?, ?)`,
				cOrID, r.userID, r.id, r.priority, r.negate)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to insert C_or child: %v", err)
				continue
			}

			// 2. Update existing children of r.id to point to cOrID
			_, err = tx.Exec("UPDATE transaction_rules SET parent_id = ? WHERE parent_id = ?", cOrID, r.id)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to update parent_id of children: %v", err)
				continue
			}

			// 3. Insert DATA_CHAIN child
			childID := uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
				VALUES (?, ?, ?, NULL, NULL, 'NONE', 'DATA_CHAIN', ?, '', 0, ?, FALSE)`,
				childID, r.userID, r.id, r.integrationID.String, r.priority)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to insert DATA_CHAIN child for OR: %v", err)
				continue
			}

			// 4. Update r to be an AND compound rule
			_, err = tx.Exec(`
				UPDATE transaction_rules
				SET operator = 'AND', field = 'NONE', regex = '', amount_operator = '', amount_value = 0, negate = FALSE, integration_id = NULL
				WHERE id = ?`, r.id)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to convert parent to AND: %v", err)
				continue
			}
		} else {
			// Leaf rule (operator = NONE/empty)
			// Convert R to AND rule and move original leaf condition to a new child, plus insert DATA_CHAIN child.

			// 1. Move original leaf condition to a new child
			origChildID := uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
				VALUES (?, ?, ?, NULL, NULL, 'NONE', ?, ?, ?, ?, ?, ?)`,
				origChildID, r.userID, r.id, r.field, r.regex, r.amountOperator, r.amountValue, r.priority, r.negate)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to insert original leaf child: %v", err)
				continue
			}

			// 2. Insert DATA_CHAIN child
			childID := uuid.New().String()
			_, err = tx.Exec(`
				INSERT INTO transaction_rules (id, user_id, parent_id, integration_id, target_pool_id, operator, field, regex, amount_operator, amount_value, priority, negate)
				VALUES (?, ?, ?, NULL, NULL, 'NONE', 'DATA_CHAIN', ?, '', 0, ?, FALSE)`,
				childID, r.userID, r.id, r.integrationID.String, r.priority)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to insert DATA_CHAIN child: %v", err)
				continue
			}

			// 3. Update R to be an AND compound rule
			_, err = tx.Exec(`
				UPDATE transaction_rules
				SET operator = 'AND', field = 'NONE', regex = '', amount_operator = '', amount_value = 0, negate = FALSE, integration_id = NULL
				WHERE id = ?`, r.id)
			if err != nil {
				tx.Rollback()
				log.Printf("[DB] Failed to convert leaf parent to AND: %v", err)
				continue
			}
		}

		tx.Commit()
		log.Printf("[DB] Successfully migrated rule %s", r.id)
	}
}

func MigrateVirtualAccountsToMulti(db *sql.DB) {
	log.Printf("[DB] Running virtual accounts 1:N multi-assignment migration...")

	// 1. Create the join table entity_virtual_accounts
	joinTableSchema := `
	CREATE TABLE IF NOT EXISTS entity_virtual_accounts (
		entity_id TEXT NOT NULL,
		entity_type TEXT NOT NULL, -- 'INCOME', 'BILL', 'EXPENSE', 'ASSET', 'LOAN'
		virtual_account_id TEXT NOT NULL,
		PRIMARY KEY (entity_id, entity_type, virtual_account_id),
		FOREIGN KEY (virtual_account_id) REFERENCES virtual_accounts(id) ON DELETE CASCADE
	);`
	_, err := db.Exec(joinTableSchema)
	if err != nil {
		log.Printf("[DB Warning] Failed to create entity_virtual_accounts join table: %v", err)
		return
	}

	// 2. Check if we need to migrate any existing data from standard columns
	// If the join table is empty, migrate existing single-column linkages.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM entity_virtual_accounts").Scan(&count)
	if err == nil && count == 0 {
		log.Printf("[DB] Join table entity_virtual_accounts is empty. Migrating existing single-column account_id linkages...")

		// Migrate Incomes
		_, _ = db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) SELECT id, 'INCOME', account_id FROM incomes WHERE account_id IS NOT NULL AND account_id != ''")

		// Migrate Bills
		_, _ = db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) SELECT id, 'BILL', account_id FROM bills WHERE account_id IS NOT NULL AND account_id != ''")

		// Migrate Expenses
		_, _ = db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) SELECT id, 'EXPENSE', account_id FROM expenses WHERE account_id IS NOT NULL AND account_id != ''")

		// Migrate Assets
		_, _ = db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) SELECT id, 'ASSET', account_id FROM assets WHERE account_id IS NOT NULL AND account_id != ''")

		// Migrate Loans
		_, _ = db.Exec("INSERT INTO entity_virtual_accounts (entity_id, entity_type, virtual_account_id) SELECT id, 'LOAN', account_id FROM loans WHERE account_id IS NOT NULL AND account_id != ''")

		log.Printf("[DB] Single-column virtual account migrations complete.")
	}
}

type migETFTracker struct {
	Tracker           string  `json:"tracker"`
	HistoricalTracker string  `json:"historical_tracker"`
	ConversionTracker string  `json:"conversion_tracker"`
	HistoryProvider   string  `json:"history_provider"`
	Percentage        float64 `json:"percentage"`
	TER               float64 `json:"ter"`
}

type migAssetPenalty struct {
	Name        string  `json:"name"`
	TriggerType string  `json:"trigger_type"`
	Percentage  float64 `json:"percentage"`
}

type migSubAsset struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	TargetValue         string     `json:"target_value"`
	AmountPerMonth      float64    `json:"amount_per_month"`
	IsRemainderConsumer bool       `json:"is_remainder_consumer"`
	RemainderStartDate  *time.Time `json:"remainder_start_date"`
	DumpingLoanID       *string    `json:"dumping_loan_id"`
	StartDate           time.Time  `json:"start_date"`
	EndDate             *time.Time `json:"end_date"`
	EarliestDumpDate    *time.Time `json:"earliest_dump_date"`
}

type migETFScenarioParams struct {
	Simulations   int     `json:"simulations"`
	SimYears      int     `json:"sim_years"`
	SimPercent    float64 `json:"sim_percent"`
	LookbackYears int     `json:"lookback_years"`
}

func NormalizeJSONColumns(db *sql.DB) {
	log.Printf("[DB] Running JSON columns normalization migration...")

	// Drop old table schema if it's using the old format without sub_asset_id column
	if !hasColumn(db, "asset_version_sub_assets", "sub_asset_id") {
		db.Exec("DROP TABLE IF EXISTS asset_version_sub_assets CASCADE")
	}

	// 1. Create the new relational tables if they don't exist yet (this ensures they exist during migration)
	newTablesSchema := `
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
        FOREIGN KEY(asset_version_id) REFERENCES asset_versions(id) ON DELETE CASCADE,
        FOREIGN KEY(dumping_loan_id) REFERENCES loans(id) ON DELETE SET NULL
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
    );`

	if _, err := db.Exec(newTablesSchema); err != nil {
		log.Printf("[DB Warning] Failed to create new tables: %v", err)
		return
	}

	// Helper to unmarshal base64 JSON string (mimics db.Unmarshal)
	unmarshalHelper := func(data string, v interface{}) error {
		if data == "" || data == "null" || data == "NULL" {
			return nil
		}
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			b = []byte(data)
		}
		return json.Unmarshal(b, v)
	}

	// 2. Migrate asset_versions JSON columns
	if hasColumn(db, "asset_versions", "etf_config_json") || hasColumn(db, "asset_versions", "penalties_json") || hasColumn(db, "asset_versions", "sub_assets_json") {
		log.Printf("[DB] Migrating asset_versions JSON columns...")
		rows, err := db.Query("SELECT id, etf_config_json, penalties_json, sub_assets_json FROM asset_versions")
		if err != nil {
			log.Printf("[DB Warning] Failed to query asset_versions: %v", err)
		} else {
			defer rows.Close()
			tx, err := db.Begin()
			if err != nil {
				log.Printf("[DB Warning] Failed to begin transaction: %v", err)
				return
			}
			defer tx.Rollback()

			for rows.Next() {
				var id string
				var etfJSON, penaltiesJSON, subAssetsJSON sql.NullString
				if err := rows.Scan(&id, &etfJSON, &penaltiesJSON, &subAssetsJSON); err != nil {
					log.Printf("[DB Warning] Scan failed: %v", err)
					continue
				}

				if etfJSON.Valid && etfJSON.String != "" {
					var etfs []migETFTracker
					if err := unmarshalHelper(etfJSON.String, &etfs); err == nil {
						for _, etf := range etfs {
							_, err = tx.Exec(`
								INSERT INTO asset_version_etf_configs (id, asset_version_id, tracker, historical_tracker, conversion_tracker, history_provider, percentage, ter)
								VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
								uuid.New().String(), id, etf.Tracker, etf.HistoricalTracker, etf.ConversionTracker, etf.HistoryProvider, etf.Percentage, etf.TER)
							if err != nil {
								log.Printf("[DB Warning] Failed to insert etf config: %v", err)
							}
						}
					} else {
						log.Printf("[DB Warning] Failed to unmarshal etf config for %s: %v", id, err)
					}
				}

				if penaltiesJSON.Valid && penaltiesJSON.String != "" {
					var penalties []migAssetPenalty
					if err := unmarshalHelper(penaltiesJSON.String, &penalties); err == nil {
						for _, penalty := range penalties {
							_, err = tx.Exec(`
								INSERT INTO asset_version_penalties (id, asset_version_id, name, trigger_type, percentage)
								VALUES (?, ?, ?, ?, ?)`,
								uuid.New().String(), id, penalty.Name, penalty.TriggerType, penalty.Percentage)
							if err != nil {
								log.Printf("[DB Warning] Failed to insert penalty: %v", err)
							}
						}
					} else {
						log.Printf("[DB Warning] Failed to unmarshal penalties for %s: %v", id, err)
					}
				}

				if subAssetsJSON.Valid && subAssetsJSON.String != "" {
					var subAssets []migSubAsset
					if err := unmarshalHelper(subAssetsJSON.String, &subAssets); err == nil {
						for _, sa := range subAssets {
							saID := sa.ID
							if saID == "" {
								saID = uuid.New().String()
							}
							var dLoanID *string = sa.DumpingLoanID
							if dLoanID != nil && *dLoanID != "" {
								// Check if loan exists
								var exists bool
								err := tx.QueryRow("SELECT 1 FROM loans WHERE id = ?", *dLoanID).Scan(&exists)
								if err != nil || !exists {
									dLoanID = nil
								}
							} else {
								dLoanID = nil
							}
							_, err = tx.Exec(`
								INSERT INTO asset_version_sub_assets (id, asset_version_id, sub_asset_id, name, target_value, amount_per_month, is_remainder_consumer, remainder_start_date, dumping_loan_id, start_date, end_date, earliest_dump_date)
								VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
								uuid.New().String(), id, saID, sa.Name, sa.TargetValue, sa.AmountPerMonth, sa.IsRemainderConsumer, sa.RemainderStartDate, dLoanID, sa.StartDate, sa.EndDate, sa.EarliestDumpDate)
							if err != nil {
								log.Printf("[DB Warning] Failed to insert sub asset: %v", err)
							}
						}
					} else {
						log.Printf("[DB Warning] Failed to unmarshal sub assets for %s: %v", id, err)
					}
				}
			}

			if err := tx.Commit(); err != nil {
				log.Printf("[DB Warning] Commit failed: %v", err)
				return
			}

			// Drop columns
			if hasColumn(db, "asset_versions", "etf_config_json") {
				if _, err := db.Exec("ALTER TABLE asset_versions DROP COLUMN etf_config_json"); err != nil {
					log.Printf("[DB Warning] Failed to drop etf_config_json: %v", err)
				}
			}
			if hasColumn(db, "asset_versions", "penalties_json") {
				if _, err := db.Exec("ALTER TABLE asset_versions DROP COLUMN penalties_json"); err != nil {
					log.Printf("[DB Warning] Failed to drop penalties_json: %v", err)
				}
			}
			if hasColumn(db, "asset_versions", "sub_assets_json") {
				if _, err := db.Exec("ALTER TABLE asset_versions DROP COLUMN sub_assets_json"); err != nil {
					log.Printf("[DB Warning] Failed to drop sub_assets_json: %v", err)
				}
			}
			log.Printf("[DB] asset_versions JSON columns migration complete.")
		}
	}

	// 3. Migrate scenarios JSON columns
	if hasColumn(db, "scenarios", "remainder_order_json") || hasColumn(db, "scenarios", "etf_params_json") {
		log.Printf("[DB] Migrating scenarios JSON columns...")
		rows, err := db.Query("SELECT id, remainder_order_json, etf_params_json FROM scenarios")
		if err != nil {
			log.Printf("[DB Warning] Failed to query scenarios: %v", err)
		} else {
			defer rows.Close()
			tx, err := db.Begin()
			if err != nil {
				log.Printf("[DB Warning] Failed to begin transaction: %v", err)
				return
			}
			defer tx.Rollback()

			for rows.Next() {
				var id string
				var remainderJSON, etfParamsJSON sql.NullString
				if err := rows.Scan(&id, &remainderJSON, &etfParamsJSON); err != nil {
					log.Printf("[DB Warning] Scan failed: %v", err)
					continue
				}

				if remainderJSON.Valid && remainderJSON.String != "" {
					var remainderOrder []string
					if err := unmarshalHelper(remainderJSON.String, &remainderOrder); err == nil {
						for pos, entityID := range remainderOrder {
							_, err = tx.Exec(`
								INSERT INTO scenario_remainder_orders (scenario_id, entity_id, position)
								VALUES (?, ?, ?)`,
								id, entityID, pos)
							if err != nil {
								log.Printf("[DB Warning] Failed to insert scenario remainder order: %v", err)
							}
						}
					} else {
						log.Printf("[DB Warning] Failed to unmarshal remainder order for %s: %v", id, err)
					}
				}

				if etfParamsJSON.Valid && etfParamsJSON.String != "" {
					var etfParams map[string]migETFScenarioParams
					if err := unmarshalHelper(etfParamsJSON.String, &etfParams); err == nil {
						for ticker, params := range etfParams {
							_, err = tx.Exec(`
								INSERT INTO scenario_etf_params (scenario_id, ticker, simulations, sim_years, sim_percent, lookback_years)
								VALUES (?, ?, ?, ?, ?, ?)`,
								id, ticker, params.Simulations, params.SimYears, params.SimPercent, params.LookbackYears)
							if err != nil {
								log.Printf("[DB Warning] Failed to insert scenario etf params: %v", err)
							}
						}
					} else {
						log.Printf("[DB Warning] Failed to unmarshal etf params for %s: %v", id, err)
					}
				}
			}

			if err := tx.Commit(); err != nil {
				log.Printf("[DB Warning] Commit failed: %v", err)
				return
			}

			// Drop columns
			if hasColumn(db, "scenarios", "remainder_order_json") {
				if _, err := db.Exec("ALTER TABLE scenarios DROP COLUMN remainder_order_json"); err != nil {
					log.Printf("[DB Warning] Failed to drop remainder_order_json: %v", err)
				}
			}
			if hasColumn(db, "scenarios", "etf_params_json") {
				if _, err := db.Exec("ALTER TABLE scenarios DROP COLUMN etf_params_json"); err != nil {
					log.Printf("[DB Warning] Failed to drop etf_params_json: %v", err)
				}
			}
			log.Printf("[DB] scenarios JSON columns migration complete.")
		}
	}
}

type logTx struct {
	Ref        string
	Amount     float64
	Receiver   string
	Date       time.Time
	SubCode    string
	IsPending  bool
	StatusCode string
}

type enableBankingTx struct {
	EntryReference     string `json:"entry_reference"`
	TransactionID      string `json:"transaction_id"`
	BookingDate        string `json:"booking_date"`
	ValueDate          string `json:"value_date"`
	CreditDebit        string `json:"credit_debit_indicator"`
	Status             string `json:"status"`
	TransactionAmount  *struct {
		Amount string `json:"amount"`
	} `json:"transaction_amount"`
	Creditor *struct {
		Name string `json:"name"`
	} `json:"creditor"`
	Debtor *struct {
		Name string `json:"name"`
	} `json:"debtor"`
	BankTransactionCode *struct {
		SubCode string `json:"sub_code"`
	} `json:"bank_transaction_code"`
}

type goCardlessTx struct {
	TransactionID   string `json:"transactionId"`
	EntryReference  string `json:"entryReference"`
	BookingDate     string `json:"bookingDate"`
	ValueDate       string `json:"valueDate"`
	CreditorName    string `json:"creditorName"`
	DebtorName      string `json:"debtorName"`
	TransactionAmount *struct {
		Amount string `json:"amount"`
	} `json:"transactionAmount"`
}

type syncRespBody struct {
	Body struct {
		Transactions interface{} `json:"transactions"`
	} `json:"body"`
}

func parseLogFile(filePath string) ([]logTx, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var resp syncRespBody
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	var txs []logTx

	// Check if Transactions is a slice (EnableBanking)
	txsSliceJSON, err := json.Marshal(resp.Body.Transactions)
	if err != nil {
		return nil, err
	}

	var ebTxs []enableBankingTx
	if err := json.Unmarshal(txsSliceJSON, &ebTxs); err == nil && len(ebTxs) > 0 {
		for _, tx := range ebTxs {
			ref := tx.EntryReference
			if ref == "" {
				ref = tx.TransactionID
			}
			if ref == "" {
				continue
			}

			dateStr := tx.BookingDate
			if dateStr == "" {
				dateStr = tx.ValueDate
			}
			if dateStr == "" {
				continue
			}
			tDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}

			var amount float64
			if tx.TransactionAmount != nil {
				fmt.Sscanf(tx.TransactionAmount.Amount, "%f", &amount)
			}
			if tx.CreditDebit == "DBIT" {
				amount = -amount
			}

			receiver := ""
			if tx.Creditor != nil {
				receiver = tx.Creditor.Name
			} else if tx.Debtor != nil {
				receiver = tx.Debtor.Name
			}

			subcode := ""
			if tx.BankTransactionCode != nil {
				subcode = tx.BankTransactionCode.SubCode
			}

			isPending := (subcode == "UPCT" || (tx.Status != "" && tx.Status != "BOOK" && tx.Status != "BOOKED" && tx.Status != "booked"))

			txs = append(txs, logTx{
				Ref:        ref,
				Amount:     amount,
				Receiver:   receiver,
				Date:       tDate,
				SubCode:    subcode,
				IsPending:  isPending,
				StatusCode: tx.Status,
			})
		}
		return txs, nil
	}

	// Try GoCardless format
	var gcTxs map[string][]goCardlessTx
	if err := json.Unmarshal(txsSliceJSON, &gcTxs); err == nil {
		for section, list := range gcTxs {
			for _, tx := range list {
				ref := tx.TransactionID
				if ref == "" {
					ref = tx.EntryReference
				}
				if ref == "" {
					continue
				}

				dateStr := tx.BookingDate
				if dateStr == "" {
					dateStr = tx.ValueDate
				}
				if dateStr == "" {
					continue
				}
				tDate, err := time.Parse("2006-01-02", dateStr)
				if err != nil {
					continue
				}

				var amount float64
				if tx.TransactionAmount != nil {
					fmt.Sscanf(tx.TransactionAmount.Amount, "%f", &amount)
				}

				receiver := tx.CreditorName
				if receiver == "" {
					receiver = tx.DebtorName
				}

				isPending := (section == "pending")

				txs = append(txs, logTx{
					Ref:        ref,
					Amount:     amount,
					Receiver:   receiver,
					Date:       tDate,
					SubCode:    "",
					IsPending:  isPending,
					StatusCode: section,
				})
			}
		}
		return txs, nil
	}

	return nil, nil
}

func findServerID() (string, error) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir != "" {
		data, err := os.ReadFile(filepath.Join(dataDir, "server.id"))
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}

	dir, err := os.Getwd()
	if err == nil {
		for {
			paths := []string{
				filepath.Join(dir, "web/data/server.id"),
				filepath.Join(dir, "data/server.id"),
				filepath.Join(dir, "server.id"),
				filepath.Join(dir, "app/data/server.id"),
			}
			for _, p := range paths {
				data, err := os.ReadFile(p)
				if err == nil {
					return strings.TrimSpace(string(data)), nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	paths := []string{
		"/app/data/server.id",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}
	return "", fmt.Errorf("server.id not found")
}

type dbAuthenticator struct {
	ID             []byte
	CredentialJSON string
}

type dbKeySlot struct {
	AuthenticatorID []byte
	EncryptedKey    string
}

func deriveIdentityKey(serverID string, pubKey []byte) ([]byte, error) {
	hash := sha256.New
	masterSecret := make([]byte, 0, len(serverID)+len(pubKey))
	masterSecret = append(masterSecret, serverID...)
	masterSecret = append(masterSecret, pubKey...)

	hkdfReader := hkdf.New(hash, masterSecret, []byte("IDENTITY_V1"), nil)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func decryptAESGCM(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func getIntegrationMasterKey(db *sql.DB, serverID string, userID string, integrationID string) ([]byte, error) {
	rows, err := db.Query("SELECT authenticator_id, encrypted_key FROM integration_key_slots WHERE integration_id = $1", integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []dbKeySlot
	for rows.Next() {
		var slot dbKeySlot
		if err := rows.Scan(&slot.AuthenticatorID, &slot.EncryptedKey); err == nil {
			slots = append(slots, slot)
		}
	}

	if len(slots) == 0 {
		return nil, fmt.Errorf("no key slots found")
	}

	rows2, err := db.Query("SELECT id, credential_json FROM authenticators WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	var auths []dbAuthenticator
	for rows2.Next() {
		var auth dbAuthenticator
		if err := rows2.Scan(&auth.ID, &auth.CredentialJSON); err == nil {
			auths = append(auths, auth)
		}
	}

	for _, auth := range auths {
		var matchingSlot *dbKeySlot
		for _, slot := range slots {
			if bytes.Equal(slot.AuthenticatorID, auth.ID) {
				matchingSlot = &slot
				break
			}
		}
		if matchingSlot == nil {
			continue
		}

		var cred struct {
			PublicKey []byte `json:"PublicKey"`
		}
		credData, err := base64.StdEncoding.DecodeString(auth.CredentialJSON)
		if err != nil {
			credData = []byte(auth.CredentialJSON)
		}
		if err := json.Unmarshal(credData, &cred); err != nil {
			continue
		}

		ik, err := deriveIdentityKey(serverID, cred.PublicKey)
		if err != nil {
			continue
		}

		wrapped, err := base64.StdEncoding.DecodeString(matchingSlot.EncryptedKey)
		if err != nil {
			wrapped = []byte(matchingSlot.EncryptedKey)
		}

		mik, err := decryptAESGCM(ik, wrapped)
		if err == nil {
			return mik, nil
		}
	}

	return nil, fmt.Errorf("failed to unwrap master key")
}

func getLatestCorrelationID(db *sql.DB, integrationID string) (string, error) {
	var cid string
	err := db.QueryRow("SELECT correlation_id FROM bank_transactions WHERE integration_id = $1 ORDER BY synced_at DESC LIMIT 1", integrationID).Scan(&cid)
	return cid, err
}

func decryptTransactionData(serverID string, encryptedData string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return nil, err
	}
	return decryptAESGCM(key, ciphertext)
}

func FixUPCTTransitions(db *sql.DB) error {
	log.Printf("[DB] Running UPCT -> POSD transition migration...")

	serverID, err := findServerID()
	if err != nil {
		log.Printf("[DB Warning] Skipping UPCT migration: %v", err)
		return nil
	}

	logsDir := os.Getenv("LOGS_DIR")
	if logsDir == "" {
		logsDir = "/app/logs"
	}
	logsDir = filepath.Join(logsDir, "sync_runs")

	type dbIntegration struct {
		ID     string
		UserID string
	}
	rows, err := db.Query("SELECT id, user_id FROM integrations")
	if err != nil {
		return err
	}
	defer rows.Close()

	var integrations []dbIntegration
	for rows.Next() {
		var i dbIntegration
		if err := rows.Scan(&i.ID, &i.UserID); err == nil {
			integrations = append(integrations, i)
		}
	}

	for _, integration := range integrations {
		mik, err := getIntegrationMasterKey(db, serverID, integration.UserID, integration.ID)
		if err != nil {
			log.Printf("[DB Warning] Failed to get master key for integration %s: %v", integration.ID, err)
			continue
		}

		latestCID, err := getLatestCorrelationID(db, integration.ID)
		var latestRefs = make(map[string]bool)
		if err == nil && latestCID != "" {
			dirPath := filepath.Join(logsDir, latestCID)
			entries, err := os.ReadDir(dirPath)
			if err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_resp.json") {
						txs, err := parseLogFile(filepath.Join(dirPath, entry.Name()))
						if err == nil {
							for _, tx := range txs {
								latestRefs[tx.Ref] = true
							}
						}
					}
				}
			}
		}

		type dbTx struct {
			ID            string
			ExternalID    string
			EncryptedData string
		}
		rowsTx, err := db.Query("SELECT id, external_id, encrypted_data FROM bank_transactions WHERE integration_id = $1 AND is_deleted = FALSE", integration.ID)
		if err != nil {
			continue
		}

		var txList []dbTx
		for rowsTx.Next() {
			var t dbTx
			if err := rowsTx.Scan(&t.ID, &t.ExternalID, &t.EncryptedData); err == nil {
				txList = append(txList, t)
			}
		}
		rowsTx.Close()

		type parsedTx struct {
			dbID       string
			externalID string
			amount     float64
			receiver   string
			date       time.Time
			isPending  bool
		}

		var decryptedTxs []parsedTx
		for _, t := range txList {
			decrypted, err := decryptTransactionData(serverID, t.EncryptedData, mik)
			if err != nil {
				continue
			}

			var rawMap map[string]interface{}
			if err := json.Unmarshal(decrypted, &rawMap); err != nil {
				continue
			}

			var amount float64
			var txDate time.Time
			var receiver string
			var isPending bool

			// 1. Try GenericTransaction format
			if _, ok := rawMap["Amount"]; ok || rawMap["Peer"] != nil {
				if amtVal, ok := rawMap["Amount"].(float64); ok {
					amount = amtVal
				}
				if peerVal, ok := rawMap["Peer"].(string); ok {
					receiver = peerVal
				}
				if createdVal, ok := rawMap["CreatedAt"].(string); ok {
					txDate, _ = time.Parse(time.RFC3339, createdVal)
				}
				if statusVal, ok := rawMap["InternalStatus"].(string); ok {
					if statusVal == "PENDING_REJECTION" {
						isPending = true
					}
				}
			} else {
				// 2. Fallback to raw format
				if amtVal, ok := rawMap["amount"].(float64); ok {
					amount = amtVal
				} else if amtObj, ok := rawMap["transaction_amount"].(map[string]interface{}); ok {
					if amtStr, ok := amtObj["amount"].(string); ok {
						fmt.Sscanf(amtStr, "%f", &amount)
					}
				}
				
				if creditDebit, ok := rawMap["credit_debit_indicator"].(string); ok && creditDebit == "DBIT" {
					amount = -amount
				}

				if dateStr, ok := rawMap["date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				} else if dateStr, ok := rawMap["booking_date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				} else if dateStr, ok := rawMap["value_date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				}

				if peerVal, ok := rawMap["receiver"].(string); ok {
					receiver = peerVal
				} else if creditor, ok := rawMap["creditor"].(map[string]interface{}); ok {
					if name, ok := creditor["name"].(string); ok {
						receiver = name
					}
				} else if debtor, ok := rawMap["debtor"].(map[string]interface{}); ok {
					if name, ok := debtor["name"].(string); ok {
						receiver = name
					}
				}

				subcode := ""
				if codeObj, ok := rawMap["bank_transaction_code"].(map[string]interface{}); ok {
					if sc, ok := codeObj["sub_code"].(string); ok {
						subcode = sc
					}
				}

				if subcode == "UPCT" {
					isPending = true
				}
				if status, ok := rawMap["status"].(string); ok {
					if status == "PENDING_REJECTION" || (status != "BOOK" && status != "BOOKED" && status != "booked" && status != "") {
						isPending = true
					}
				}
			}

			decryptedTxs = append(decryptedTxs, parsedTx{
				dbID:       t.ID,
				externalID: t.ExternalID,
				amount:     amount,
				receiver:   receiver,
				date:       txDate,
				isPending:  isPending,
			})
		}

		normalizeName := func(name string) string {
			name = strings.ToLower(name)
			var sb strings.Builder
			for _, r := range name {
				if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
					sb.WriteRune(r)
				}
			}
			return sb.String()
		}

		for _, pTx := range decryptedTxs {
			if !pTx.isPending {
				continue
			}

			if len(latestRefs) > 0 && latestRefs[pTx.externalID] {
				continue
			}

			for _, other := range decryptedTxs {
				if other.dbID == pTx.dbID || other.isPending {
					continue
				}

				diff := other.date.Sub(pTx.date)
				if diff < 0 {
					diff = -diff
				}
				if diff.Hours() > 72.0 {
					continue
				}

				diffAmt := other.amount - pTx.amount
				if diffAmt < 0 {
					diffAmt = -diffAmt
				}
				if diffAmt >= 0.01 {
					continue
				}

				n1 := normalizeName(pTx.receiver)
				n2 := normalizeName(other.receiver)
				isSimilar := false
				if n1 == n2 || (n1 != "" && n2 != "" && (strings.Contains(n1, n2) || strings.Contains(n2, n1))) {
					isSimilar = true
				} else if strings.Contains(n1, "paypal") && strings.Contains(n2, "paypal") {
					isSimilar = true
				} else if strings.Contains(n1, "amzn") && strings.Contains(n2, "amzn") {
					isSimilar = true
				} else if strings.Contains(n1, "amazon") && strings.Contains(n2, "amazon") {
					isSimilar = true
				} else if strings.Contains(n1, "lidl") && strings.Contains(n2, "lidl") {
					isSimilar = true
				}

				if isSimilar {
					log.Printf("[DB MIGRATION] Soft-deleting transitioned pending duplicate transaction %s (ExternalID: %s, Amount: %.2f, Receiver: %s) matching finalized %s.",
						pTx.dbID, pTx.externalID, pTx.amount, pTx.receiver, other.dbID)
					_, err = db.Exec("UPDATE bank_transactions SET is_deleted = TRUE, internal_status = 'EXPIRED_REJECTION' WHERE id = $1", pTx.dbID)
					if err != nil {
						log.Printf("[DB MIGRATION Warning] Failed to delete transaction: %v", err)
					}
					break
				}
			}
		}
	}

	return nil
}

func FixUPCTTransitionsV3(db *sql.DB) error {
	log.Printf("[DB] Running UPCT -> POSD transition migration V3...")

	serverID, err := findServerID()
	if err != nil {
		log.Printf("[DB Warning] Skipping UPCT migration V3: %v", err)
		return nil
	}

	logsDir := os.Getenv("LOGS_DIR")
	if logsDir == "" {
		logsDir = "/app/logs"
		if _, err := os.Stat(logsDir); os.IsNotExist(err) {
			if _, err := os.Stat("web/logs"); err == nil {
				logsDir = "web/logs"
			} else if _, err := os.Stat("logs"); err == nil {
				logsDir = "logs"
			}
		}
	}
	logsDir = filepath.Join(logsDir, "sync_runs")

	// 1. Scan all folders under logs/sync_runs to build a map of UPCT external IDs
	upctRefs := make(map[string]bool)
	syncRunDirs, err := os.ReadDir(logsDir)
	if err == nil {
		for _, d := range syncRunDirs {
			if d.IsDir() {
				dirPath := filepath.Join(logsDir, d.Name())
				entries, err := os.ReadDir(dirPath)
				if err == nil {
					for _, entry := range entries {
						if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_resp.json") {
							txs, err := parseLogFile(filepath.Join(dirPath, entry.Name()))
							if err == nil {
								for _, tx := range txs {
									if tx.IsPending || tx.SubCode == "UPCT" {
										upctRefs[tx.Ref] = true
									}
								}
							}
						}
					}
				}
			}
		}
	} else {
		log.Printf("[DB Warning] Could not read logsDir %s: %v", logsDir, err)
	}

	type dbIntegration struct {
		ID     string
		UserID string
	}
	rows, err := db.Query("SELECT id, user_id FROM integrations")
	if err != nil {
		return err
	}
	defer rows.Close()

	var integrations []dbIntegration
	for rows.Next() {
		var i dbIntegration
		if err := rows.Scan(&i.ID, &i.UserID); err == nil {
			integrations = append(integrations, i)
		}
	}

	for _, integration := range integrations {
		mik, err := getIntegrationMasterKey(db, serverID, integration.UserID, integration.ID)
		if err != nil {
			log.Printf("[DB Warning] Failed to get master key for integration %s: %v", integration.ID, err)
			continue
		}

		type dbTx struct {
			ID            string
			ExternalID    string
			EncryptedData string
		}
		rowsTx, err := db.Query("SELECT id, external_id, encrypted_data FROM bank_transactions WHERE integration_id = $1 AND is_deleted = FALSE", integration.ID)
		if err != nil {
			continue
		}

		var txList []dbTx
		for rowsTx.Next() {
			var t dbTx
			if err := rowsTx.Scan(&t.ID, &t.ExternalID, &t.EncryptedData); err == nil {
				txList = append(txList, t)
			}
		}
		rowsTx.Close()

		type parsedTx struct {
			dbID       string
			externalID string
			amount     float64
			receiver   string
			date       time.Time
			isPending  bool
		}

		var decryptedTxs []parsedTx
		for _, t := range txList {
			decrypted, err := decryptTransactionData(serverID, t.EncryptedData, mik)
			if err != nil {
				continue
			}

			var rawMap map[string]interface{}
			if err := json.Unmarshal(decrypted, &rawMap); err != nil {
				continue
			}

			var amount float64
			var txDate time.Time
			var receiver string
			var isPending bool

			// 1. Try GenericTransaction format
			if _, ok := rawMap["Amount"]; ok || rawMap["Peer"] != nil {
				if amtVal, ok := rawMap["Amount"].(float64); ok {
					amount = amtVal
				}
				if peerVal, ok := rawMap["Peer"].(string); ok {
					receiver = peerVal
				}
				if createdVal, ok := rawMap["CreatedAt"].(string); ok {
					txDate, _ = time.Parse(time.RFC3339, createdVal)
				}
				if statusVal, ok := rawMap["InternalStatus"].(string); ok {
					if statusVal == "PENDING_REJECTION" {
						isPending = true
					}
				}
			} else {
				// 2. Fallback to raw format
				if amtVal, ok := rawMap["amount"].(float64); ok {
					amount = amtVal
				} else if amtObj, ok := rawMap["transaction_amount"].(map[string]interface{}); ok {
					if amtStr, ok := amtObj["amount"].(string); ok {
						fmt.Sscanf(amtStr, "%f", &amount)
					}
				}
				
				if creditDebit, ok := rawMap["credit_debit_indicator"].(string); ok && creditDebit == "DBIT" {
					amount = -amount
				}

				if dateStr, ok := rawMap["date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				} else if dateStr, ok := rawMap["booking_date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				} else if dateStr, ok := rawMap["value_date"].(string); ok {
					txDate, _ = time.Parse("2006-01-02", dateStr)
				}

				if peerVal, ok := rawMap["receiver"].(string); ok {
					receiver = peerVal
				} else if creditor, ok := rawMap["creditor"].(map[string]interface{}); ok {
					if name, ok := creditor["name"].(string); ok {
						receiver = name
					}
				} else if debtor, ok := rawMap["debtor"].(map[string]interface{}); ok {
					if name, ok := debtor["name"].(string); ok {
						receiver = name
					}
				}

				subcode := ""
				if codeObj, ok := rawMap["bank_transaction_code"].(map[string]interface{}); ok {
					if sc, ok := codeObj["sub_code"].(string); ok {
						subcode = sc
					}
				}

				if subcode == "UPCT" {
					isPending = true
				}
				if status, ok := rawMap["status"].(string); ok {
					if status == "PENDING_REJECTION" || (status != "BOOK" && status != "BOOKED" && status != "booked" && status != "") {
						isPending = true
					}
				}
			}

			// If the external ID matches any parsed UPCT/pending transaction from any historical log, set it as pending!
			rawExtID := t.ExternalID
			if idx := strings.Index(rawExtID, "_"); idx != -1 {
				rawExtID = rawExtID[idx+1:]
			}
			if upctRefs[rawExtID] || upctRefs[t.ExternalID] {
				isPending = true
			}

			decryptedTxs = append(decryptedTxs, parsedTx{
				dbID:       t.ID,
				externalID: t.ExternalID,
				amount:     amount,
				receiver:   receiver,
				date:       txDate,
				isPending:  isPending,
			})
		}

		normalizeName := func(name string) string {
			name = strings.ToLower(name)
			var sb strings.Builder
			for _, r := range name {
				if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
					sb.WriteRune(r)
				}
			}
			return sb.String()
		}

		for _, pTx := range decryptedTxs {
			if !pTx.isPending {
				continue
			}

			for _, other := range decryptedTxs {
				if other.dbID == pTx.dbID || other.isPending {
					continue
				}

				diff := other.date.Sub(pTx.date)
				if diff < 0 {
					diff = -diff
				}
				if diff.Hours() > 72.0 {
					continue
				}

				diffAmt := other.amount - pTx.amount
				if diffAmt < 0 {
					diffAmt = -diffAmt
				}
				if diffAmt >= 0.01 {
					continue
				}

				n1 := normalizeName(pTx.receiver)
				n2 := normalizeName(other.receiver)
				isSimilar := false
				if n1 == n2 || (n1 != "" && n2 != "" && (strings.Contains(n1, n2) || strings.Contains(n2, n1))) {
					isSimilar = true
				} else if strings.Contains(n1, "paypal") && strings.Contains(n2, "paypal") {
					isSimilar = true
				} else if strings.Contains(n1, "amzn") && strings.Contains(n2, "amzn") {
					isSimilar = true
				} else if strings.Contains(n1, "amazon") && strings.Contains(n2, "amazon") {
					isSimilar = true
				} else if strings.Contains(n1, "lidl") && strings.Contains(n2, "lidl") {
					isSimilar = true
				}

				if isSimilar {
					log.Printf("[DB MIGRATION V3] Soft-deleting transitioned pending duplicate transaction %s (ExternalID: %s, Amount: %.2f, Receiver: %s) matching finalized %s.",
						pTx.dbID, pTx.externalID, pTx.amount, pTx.receiver, other.dbID)
					_, err = db.Exec("UPDATE bank_transactions SET is_deleted = TRUE, internal_status = 'EXPIRED_REJECTION' WHERE id = $1", pTx.dbID)
					if err != nil {
						log.Printf("[DB MIGRATION V3 Warning] Failed to delete transaction: %v", err)
					}
					break
				}
			}
		}
	}

	return nil
}

