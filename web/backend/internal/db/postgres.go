package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
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
        etf_config_json TEXT,
        penalties_json TEXT,
        sub_assets_json TEXT,
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

    CREATE TABLE IF NOT EXISTS scenarios (
        id TEXT PRIMARY KEY,
        user_id TEXT,
        name TEXT,
        description TEXT,
        projection_months INTEGER DEFAULT 360,
        remainder_order_json TEXT,
        is_active BOOLEAN DEFAULT FALSE,
        month_start_day INTEGER DEFAULT 1,
        start_date TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        is_deleted BOOLEAN DEFAULT FALSE,
        simulations INTEGER DEFAULT 50000,
        sim_years INTEGER DEFAULT 10,
        sim_percent DOUBLE PRECISION DEFAULT 50,
        etf_params_json TEXT,
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
	`

	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	migrate(db)

	return db, nil
}

func hasColumn(db *sql.DB, table, column string) bool {
	var name string
	query := "SELECT column_name FROM information_schema.columns WHERE table_name = $1 AND column_name = $2"
	err := db.QueryRow(query, table, column).Scan(&name)
	if err != nil {
		return false
	}
	return name == column
}

func columnDataType(db *sql.DB, table, column string) string {
	var dataType string
	query := "SELECT data_type FROM information_schema.columns WHERE table_name = $1 AND column_name = $2"
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

func migrate(db *sql.DB) {
	ensurePostgresTypes(db)

	// Users table enhancements
	if !hasColumn(db, "users", "dashboard_scenario_id") {
		db.Exec("ALTER TABLE users ADD COLUMN dashboard_scenario_id TEXT")
	}
	if !hasColumn(db, "users", "dashboard_month_offset") {
		db.Exec("ALTER TABLE users ADD COLUMN dashboard_month_offset INTEGER DEFAULT 0")
	}
	if !hasColumn(db, "users", "recovery_hash") {
		db.Exec("ALTER TABLE users ADD COLUMN recovery_hash TEXT")
	}

	// Income versions
	if !hasColumn(db, "income_versions", "stop_modification_id") {
		db.Exec("ALTER TABLE income_versions ADD COLUMN stop_modification_id TEXT")
	}

	// Loan versions
	if !hasColumn(db, "loan_versions", "early_payoff_penalty") {
		db.Exec("ALTER TABLE loan_versions ADD COLUMN early_payoff_penalty DOUBLE PRECISION DEFAULT 1")
	}
	if !hasColumn(db, "loan_versions", "next_loan_id") {
		db.Exec("ALTER TABLE loan_versions ADD COLUMN next_loan_id TEXT")
	}
	if !hasColumn(db, "loan_versions", "remainder_start_date") {
		db.Exec("ALTER TABLE loan_versions ADD COLUMN remainder_start_date TIMESTAMP")
	}

	// Asset versions
	if !hasColumn(db, "asset_versions", "dumping_loan_id") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN dumping_loan_id TEXT")
	}
	if !hasColumn(db, "asset_versions", "stop_modification_id") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN stop_modification_id TEXT")
	}
	if !hasColumn(db, "asset_versions", "withdrawal_penalty") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN withdrawal_penalty DOUBLE PRECISION DEFAULT 0")
	}
	if !hasColumn(db, "asset_versions", "penalties_json") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN penalties_json TEXT")
	}
	if !hasColumn(db, "asset_versions", "remainder_start_date") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN remainder_start_date TIMESTAMP")
	}
	if !hasColumn(db, "asset_versions", "sub_assets_json") {
		db.Exec("ALTER TABLE asset_versions ADD COLUMN sub_assets_json TEXT")
	}

	// Authenticators
	if !hasColumn(db, "authenticators", "credential_json") {
		db.Exec("ALTER TABLE authenticators ADD COLUMN credential_json TEXT")
	}
	if !hasColumn(db, "authenticators", "name") {
		db.Exec("ALTER TABLE authenticators ADD COLUMN name TEXT DEFAULT ''")
	}
	if !hasColumn(db, "authenticators", "created_at") {
		db.Exec("ALTER TABLE authenticators ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
	}

	// Scenarios
	if !hasColumn(db, "scenarios", "month_start_day") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN month_start_day INTEGER DEFAULT 1")
	}
	if !hasColumn(db, "scenarios", "simulations") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN simulations INTEGER DEFAULT 50000")
		db.Exec("ALTER TABLE scenarios ADD COLUMN sim_years INTEGER DEFAULT 10")
		db.Exec("ALTER TABLE scenarios ADD COLUMN sim_percent DOUBLE PRECISION DEFAULT 50")
	}
	if !hasColumn(db, "scenarios", "etf_params_json") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN etf_params_json TEXT")
	}
	if !hasColumn(db, "scenarios", "lookback_years") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN lookback_years INTEGER DEFAULT 0")
	}
	if !hasColumn(db, "scenarios", "passive_income_percentage") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN passive_income_percentage DOUBLE PRECISION DEFAULT 3.5")
	}
	if !hasColumn(db, "scenarios", "mc_implementation") {
		db.Exec("ALTER TABLE scenarios ADD COLUMN mc_implementation TEXT DEFAULT 'STANDARD'")
	}

	// Integrations
	if !hasColumn(db, "integrations", "name") {
		db.Exec("ALTER TABLE integrations ADD COLUMN name TEXT")
		db.Exec("ALTER TABLE integrations ADD COLUMN status TEXT DEFAULT 'AWAITING_AUTH'")
		db.Exec("ALTER TABLE integrations ADD COLUMN last_sync_at TIMESTAMP")
	}
	if !hasColumn(db, "integrations", "sync_interval_seconds") {
		db.Exec("ALTER TABLE integrations ADD COLUMN sync_interval_seconds INTEGER DEFAULT 21600")
	}
	if !hasColumn(db, "integrations", "last_error") {
		db.Exec("ALTER TABLE integrations ADD COLUMN last_error TEXT")
	}
	if !hasColumn(db, "integrations", "cached_balance") {
		db.Exec("ALTER TABLE integrations ADD COLUMN cached_balance DOUBLE PRECISION DEFAULT 0")
	}

	// Transaction Pools
	if !hasColumn(db, "transaction_pools", "parent_id") {
		db.Exec("ALTER TABLE transaction_pools ADD COLUMN parent_id TEXT")
	}

	// Transaction Rules
	if !hasColumn(db, "transaction_rules", "parent_id") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN parent_id TEXT")
	}
	if !hasColumn(db, "transaction_rules", "operator") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN operator TEXT DEFAULT 'NONE'")
	}
	if !hasColumn(db, "transaction_rules", "field") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN field TEXT DEFAULT 'NONE'")
	}
	if !hasColumn(db, "transaction_rules", "regex") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN regex TEXT DEFAULT ''")
	}
	if !hasColumn(db, "transaction_rules", "amount_operator") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN amount_operator TEXT DEFAULT ''")
	}
	if !hasColumn(db, "transaction_rules", "amount_value") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN amount_value DOUBLE PRECISION DEFAULT 0")
	}
	if !hasColumn(db, "transaction_rules", "priority") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN priority INTEGER DEFAULT 0")
	}
	if !hasColumn(db, "transaction_rules", "negate") {
		db.Exec("ALTER TABLE transaction_rules ADD COLUMN negate BOOLEAN DEFAULT FALSE")
	}
	db.Exec("UPDATE transaction_rules SET operator = 'NONE' WHERE operator IS NULL OR operator = ''")
	db.Exec("UPDATE transaction_rules SET field = 'NONE' WHERE field IS NULL OR field = ''")
	db.Exec("UPDATE transaction_rules SET regex = '' WHERE regex IS NULL")
	db.Exec("UPDATE transaction_rules SET amount_operator = '' WHERE amount_operator IS NULL")
	db.Exec("UPDATE transaction_rules SET parent_id = NULL WHERE parent_id = ''")
	db.Exec("UPDATE transaction_rules SET integration_id = NULL WHERE integration_id = ''")
	db.Exec("UPDATE transaction_rules SET target_pool_id = NULL WHERE target_pool_id = ''")

	// Bank Transactions
	if !hasColumn(db, "bank_transactions", "pool_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN pool_id TEXT")
	}
	if !hasColumn(db, "bank_transactions", "account_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "bank_transactions", "source_account_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN source_account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "bank_transactions", "destination_account_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN destination_account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "bank_transactions", "synced_at") {
		_, err := db.Exec("ALTER TABLE bank_transactions ADD COLUMN synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
		if err != nil {
			log.Printf("[DB] Failed to add synced_at: %v", err)
		}
	}
	if !hasColumn(db, "bank_transactions", "tags") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN tags TEXT DEFAULT ''")
	}
	if !hasColumn(db, "bank_transactions", "linked_transaction_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN linked_transaction_id TEXT")
	}
	if !hasColumn(db, "bank_transactions", "is_link_confirmed") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN is_link_confirmed BOOLEAN DEFAULT FALSE")
	}
	if !hasColumn(db, "bank_transactions", "correlation_id") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN correlation_id TEXT")
	}
	if !hasColumn(db, "bank_transactions", "is_deleted") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN is_deleted BOOLEAN DEFAULT FALSE")
	}
	if !hasColumn(db, "bank_transactions", "denied_duplicate_ids") {
		db.Exec("ALTER TABLE bank_transactions ADD COLUMN denied_duplicate_ids TEXT DEFAULT ''")
	}

	// Modifications
	if !hasColumn(db, "modification_versions", "withdrawal_percentage") {
		db.Exec("ALTER TABLE modification_versions ADD COLUMN withdrawal_percentage DOUBLE PRECISION DEFAULT 0")
	}

	// Join tables
	db.Exec(`CREATE TABLE IF NOT EXISTS modification_assets (
		modification_id TEXT,
		asset_id TEXT,
		PRIMARY KEY (modification_id, asset_id),
		FOREIGN KEY(modification_id) REFERENCES modifications(id),
		FOREIGN KEY(asset_id) REFERENCES assets(id)
	)`)

	db.Exec(`INSERT INTO modification_assets (modification_id, asset_id)
		SELECT id, target_id FROM modifications
		WHERE target_type = 'ASSET' AND target_id IS NOT NULL AND target_id != ''
		ON CONFLICT DO NOTHING`)

	// Planning Entity Realtime Pool Links
	if !hasColumn(db, "incomes", "pool_id") {
		db.Exec("ALTER TABLE incomes ADD COLUMN pool_id TEXT")
	}
	if !hasColumn(db, "bills", "pool_id") {
		db.Exec("ALTER TABLE bills ADD COLUMN pool_id TEXT")
	}
	if !hasColumn(db, "expenses", "pool_id") {
		db.Exec("ALTER TABLE expenses ADD COLUMN pool_id TEXT")
	}
	if !hasColumn(db, "assets", "pool_id") {
		db.Exec("ALTER TABLE assets ADD COLUMN pool_id TEXT")
	}
	if !hasColumn(db, "loans", "pool_id") {
		db.Exec("ALTER TABLE loans ADD COLUMN pool_id TEXT")
	}

	// Planning Entity Virtual Account Links
	if !hasColumn(db, "incomes", "account_id") {
		db.Exec("ALTER TABLE incomes ADD COLUMN account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "bills", "account_id") {
		db.Exec("ALTER TABLE bills ADD COLUMN account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "expenses", "account_id") {
		db.Exec("ALTER TABLE expenses ADD COLUMN account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "assets", "account_id") {
		db.Exec("ALTER TABLE assets ADD COLUMN account_id TEXT DEFAULT ''")
	}
	if !hasColumn(db, "loans", "account_id") {
		db.Exec("ALTER TABLE loans ADD COLUMN account_id TEXT DEFAULT ''")
	}

	if !hasColumn(db, "users", "timezone") {
		db.Exec("ALTER TABLE users ADD COLUMN timezone TEXT DEFAULT 'UTC'")
	}

	MigrateRulesToGlobal(db)
	MigrateVirtualAccountsToMulti(db)
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
