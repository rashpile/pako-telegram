// Package audit provides command execution logging to SQLite.
package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Entry represents a single audit log record.
type Entry struct {
	Timestamp  time.Time
	ChatID     int64
	Username   string
	Command    string
	Args       string
	ExitCode   int
	DurationMs int64
}

// Logger persists command execution records.
type Logger interface {
	Log(ctx context.Context, entry Entry) error
	Close() error
}

// SQLiteLogger implements Logger using SQLite.
type SQLiteLogger struct {
	db *sql.DB
}

// NewSQLiteLogger creates a logger backed by SQLite.
func NewSQLiteLogger(dbPath string) (*SQLiteLogger, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}

	// Create schema
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteLogger{db: db}, nil
}

// createSchema creates the audit_log table if it doesn't exist.
func createSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			chat_id INTEGER NOT NULL,
			username TEXT,
			command TEXT NOT NULL,
			args TEXT,
			exit_code INTEGER,
			duration_ms INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_chat_id ON audit_log(chat_id);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	return nil
}

// Log records a command execution.
func (l *SQLiteLogger) Log(ctx context.Context, entry Entry) error {
	query := `
		INSERT INTO audit_log (timestamp, chat_id, username, command, args, exit_code, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := l.db.ExecContext(ctx, query,
		entry.Timestamp,
		entry.ChatID,
		entry.Username,
		entry.Command,
		entry.Args,
		entry.ExitCode,
		entry.DurationMs,
	)

	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}

	return nil
}

// Close releases database resources.
func (l *SQLiteLogger) Close() error {
	return l.db.Close()
}

// NopLogger is a no-op logger for testing or when audit is disabled.
type NopLogger struct{}

// Log does nothing.
func (NopLogger) Log(ctx context.Context, entry Entry) error {
	return nil
}

// Close does nothing.
func (NopLogger) Close() error {
	return nil
}
