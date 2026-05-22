package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitDB(t *testing.T) {
	dbPath := "./test_aistupid.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	// 1. InitDB creates all required tables
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	expectedTables := []string{
		"models", "scores_history", "degradations", "alerts",
		"global_index", "provider_reliability", "recommendations",
		"transparency", "model_freshness",
	}

	for _, table := range expectedTables {
		var name string
		err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("Table %q not found: %v", table, err)
		}
	}

	// 2. InitDB creates indexes
	expectedIndexes := []string{
		"idx_scores_history_model",
		"idx_scores_history_timestamp",
	}

	for _, idx := range expectedIndexes {
		var name string
		err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err != nil {
			t.Errorf("Index %q not found: %v", idx, err)
		}
	}

	// 5. InitDB can be called twice (idempotent)
	err = InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed when called a second time: %v", err)
	}
}

func TestInitDB_InvalidPath(t *testing.T) {
	// 3. InitDB with invalid path returns error
	invalidPath := filepath.Join("/nonexistent-directory-12345", "test.db")
	err := InitDB(invalidPath)
	if err == nil {
		CloseDB()
		t.Error("Expected error when initializing DB at invalid path, but got nil")
	}
}

func TestCloseDB(t *testing.T) {
	dbPath := "./test_close.db"
	defer os.Remove(dbPath)

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// 4. CloseDB works correctly
	err = CloseDB()
	if err != nil {
		t.Fatalf("CloseDB failed: %v", err)
	}

	// Verify DB is closed: querying should return an error
	var val int
	err = DB.QueryRow("SELECT 1").Scan(&val)
	if err == nil {
		t.Error("Expected query to fail after CloseDB, but it succeeded")
	}
}

func TestForeignKeys(t *testing.T) {
	dbPath := "./test_fk.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Verify foreign keys are enabled: inserting a score referencing a non-existent model should fail
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score) VALUES ('nonexistent-model', '2026-05-22 00:00:00', 95)")
	if err == nil {
		t.Error("Expected foreign key violation error, but insert succeeded")
	}

	// Insert parent model
	_, err = DB.Exec("INSERT INTO models (id, name, provider, vendor) VALUES ('gpt-4o', 'GPT-4o', 'openai', 'openai')")
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Insert child score_history
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score) VALUES ('gpt-4o', '2026-05-22 00:00:00', 95)")
	if err != nil {
		t.Fatalf("Failed to insert scores_history: %v", err)
	}

	// Insert child degradation
	_, err = DB.Exec("INSERT INTO degradations (model_id, drop_percentage, severity, detected_at, type, message) VALUES ('gpt-4o', 10, 'high', '2026-05-22 00:00:00', 'score_drop', 'drop')")
	if err != nil {
		t.Fatalf("Failed to insert degradation: %v", err)
	}

	// Verify children exist
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Expected 1 score_history record, got %d (err: %v)", count, err)
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Expected 1 degradation record, got %d (err: %v)", count, err)
	}

	// Delete parent model
	_, err = DB.Exec("DELETE FROM models WHERE id = 'gpt-4o'")
	if err != nil {
		t.Fatalf("Failed to delete model: %v", err)
	}

	// Verify child score_history and degradation are deleted (ON DELETE CASCADE)
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query scores_history: %v", err)
	}
	if count != 0 {
		t.Error("Expected scores_history to be deleted due to cascade")
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'gpt-4o'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query degradations: %v", err)
	}
	if count != 0 {
		t.Error("Expected degradations to be deleted due to cascade")
	}
}
