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

	var name string
	err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&name)
	if err != nil || name != "users" {
		t.Errorf("Expected 'users' table to exist, got %s (err: %v)", name, err)
	}
}
