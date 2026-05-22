package main

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite", filepath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	DB.SetMaxOpenConns(1)

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			provider TEXT NOT NULL,
			vendor TEXT NOT NULL,
			is_reasoning INTEGER DEFAULT 0,
			is_new INTEGER DEFAULT 0,
			is_stale INTEGER DEFAULT 0,
			status TEXT DEFAULT 'unknown',
			standard_error REAL DEFAULT 0
		);`,

		`CREATE TABLE IF NOT EXISTS scores_history (
			model_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			score INTEGER NOT NULL,
			stupid_score REAL,
			trend TEXT,
			confidence_lower REAL,
			confidence_upper REAL,
			suite TEXT,
			ax_correctness REAL,
			ax_complexity REAL,
			ax_code_quality REAL,
			ax_efficiency REAL,
			ax_stability REAL,
			ax_edge_cases REAL,
			ax_debugging REAL,
			ax_format REAL,
			ax_safety REAL,
			ax_memory_retention REAL,
			ax_hallucination_rate REAL,
			ax_plan_coherence REAL,
			ax_context_window REAL,
			PRIMARY KEY (model_id, timestamp),
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS degradations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			model_name TEXT,
			provider TEXT,
			current_score INTEGER,
			baseline_score INTEGER,
			drop_percentage INTEGER NOT NULL,
			z_score TEXT,
			severity TEXT NOT NULL,
			detected_at DATETIME NOT NULL,
			message TEXT,
			type TEXT,
			UNIQUE(model_id, type, message),
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_name TEXT NOT NULL,
			provider TEXT,
			issue TEXT NOT NULL,
			severity TEXT,
			detected_at DATETIME NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS global_index (
			timestamp DATETIME PRIMARY KEY,
			global_score INTEGER NOT NULL,
			models_count INTEGER,
			trend TEXT,
			performing_well INTEGER,
			total_models INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS provider_reliability (
			provider TEXT PRIMARY KEY,
			trust_score INTEGER,
			total_incidents INTEGER,
			incidents_per_month INTEGER,
			avg_recovery_hours TEXT,
			last_incident DATETIME,
			trend TEXT,
			active_models INTEGER,
			top_performers INTEGER,
			is_available INTEGER DEFAULT 1,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS recommendations (
			type TEXT PRIMARY KEY,
			model_id TEXT,
			model_name TEXT,
			vendor TEXT,
			score INTEGER,
			reason TEXT,
			evidence TEXT,
			extra_data TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS transparency (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			last_update DATETIME,
			total_tests INTEGER,
			passed_tests INTEGER,
			coverage INTEGER,
			confidence INTEGER,
			data_points_24h INTEGER,
			next_test DATETIME,
			models_fresh INTEGER,
			models_stale INTEGER,
			models_offline INTEGER,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS model_freshness (
			model_name TEXT PRIMARY KEY,
			last_update DATETIME,
			minutes_ago INTEGER,
			status TEXT
		);`,

		`CREATE INDEX IF NOT EXISTS idx_scores_history_model ON scores_history(model_id);`,
		`CREATE INDEX IF NOT EXISTS idx_scores_history_timestamp ON scores_history(timestamp);`,
	}

	for _, schema := range schemas {
		_, err = DB.Exec(schema)
		if err != nil {
			return err
		}
	}

	return nil
}
