package main

import (
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

	// Test GET /api/models (should return empty list on fresh DB)
	req := httptest.NewRequest("GET", "/api/models", nil)
	w := httptest.NewRecorder()
	handleModels(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var models []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to unmarshal models response: %v", err)
	}

	// Test GET /api/scores
	req = httptest.NewRequest("GET", "/api/scores", nil)
	w = httptest.NewRecorder()
	handleScores(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/degradations
	req = httptest.NewRequest("GET", "/api/degradations", nil)
	w = httptest.NewRecorder()
	handleDegradations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/alerts
	req = httptest.NewRequest("GET", "/api/alerts", nil)
	w = httptest.NewRecorder()
	handleAlerts(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/global-index
	req = httptest.NewRequest("GET", "/api/global-index", nil)
	w = httptest.NewRecorder()
	handleGlobalIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/provider-reliability
	req = httptest.NewRequest("GET", "/api/provider-reliability", nil)
	w = httptest.NewRecorder()
	handleProviderReliability(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/recommendations
	req = httptest.NewRequest("GET", "/api/recommendations", nil)
	w = httptest.NewRecorder()
	handleRecommendations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/sync-status
	req = httptest.NewRequest("GET", "/api/sync-status", nil)
	w = httptest.NewRecorder()
	handleSyncStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test GET /api/config
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	handleConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var configRes map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &configRes); err != nil {
		t.Fatalf("Failed to unmarshal config response: %v", err)
	}
	if _, ok := configRes["blocked_models"]; !ok {
		t.Error("Expected config to contain blocked_models")
	}
}
