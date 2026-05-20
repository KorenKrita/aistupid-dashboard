package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		return err
	}

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			provider TEXT NOT NULL,
			vendor TEXT NOT NULL,
			is_reasoning INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS scores_history (
			model_id TEXT NOT NULL,
			score INTEGER NOT NULL,
			trend TEXT NOT NULL,
			confidence_lower REAL,
			confidence_upper REAL,
			timestamp DATETIME NOT NULL,
			PRIMARY KEY (model_id, timestamp),
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS degradations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			drop_percentage INTEGER NOT NULL,
			severity TEXT NOT NULL,
			detected_at DATETIME NOT NULL,
			message TEXT,
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,
	}

	for _, schema := range schemas {
		_, err = DB.Exec(schema)
		if err != nil {
			return err
		}
	}

	// Insert default settings
	defaults := map[string]string{
		"sync_interval_hours":    "4",
		"history_retention_days": "90",
		"tracked_models":         `["all"]`,
	}
	for k, v := range defaults {
		_, _ = DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)", k, v)
	}

	return nil
}
