package main

import (
	"os"
	"testing"
)

func TestFetchAndSync(t *testing.T) {
	dbPath := "./test_sync.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	err = FetchAndSync()
	if err != nil {
		t.Fatalf("FetchAndSync failed: %v", err)
	}

	var modelCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM models").Scan(&modelCount)
	if err != nil {
		t.Fatalf("Failed to query models: %v", err)
	}

	if modelCount == 0 {
		t.Error("Expected models to be populated after sync, but got 0")
	}

	var scoreCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&scoreCount)
	if err != nil {
		t.Fatalf("Failed to query scores_history: %v", err)
	}

	if scoreCount == 0 {
		t.Error("Expected scores_history to be populated after sync, but got 0")
	}
}
