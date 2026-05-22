package main

import (
	"os"
	"testing"
)

func TestInitDB(t *testing.T) {
	dbPath := "./test_aistupid.db"
	defer os.Remove(dbPath)

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
}
