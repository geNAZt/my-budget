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

	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("backup-%s.sql", timestamp))

	cmd := exec.Command("pg_dump", "-d", dsn, "-f", backupFile)

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

	_, err = db.Exec(Schema)
	if err != nil {
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		return nil, err
	}

	return db, nil
}
