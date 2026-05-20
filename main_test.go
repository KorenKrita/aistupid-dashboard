package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMainAPI(t *testing.T) {
	dbPath := "./test_main.db"
	defer os.Remove(dbPath)

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	SetupRoutes()

	// 1. Test Auth Status: should be uninitialized
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	handleAuthStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var statusRes map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &statusRes)
	if statusRes["initialized"] == true {
		t.Error("Expected system to be uninitialized initially")
	}

	// 2. Test Setup: register admin
	setupData := map[string]string{
		"username": "admin",
		"password": "securepassword",
	}
	body, _ := json.Marshal(setupData)
	req = httptest.NewRequest("POST", "/api/auth/setup", bytes.NewBuffer(body))
	w = httptest.NewRecorder()
	handleSetup(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected setup status 201 Created, got %d", w.Code)
	}

	// 3. Test Auth Status: should now be initialized but unauthenticated
	req = httptest.NewRequest("GET", "/api/auth/status", nil)
	w = httptest.NewRecorder()
	handleAuthStatus(w, req)

	_ = json.Unmarshal(w.Body.Bytes(), &statusRes)
	if statusRes["initialized"] == false {
		t.Error("Expected system to be initialized after setup")
	}
	if statusRes["authenticated"] == true {
		t.Error("Expected user to be unauthenticated")
	}
}
