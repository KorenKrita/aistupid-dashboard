package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func setupTestData(t *testing.T) {
	// Insert a test model
	_, err := DB.Exec(`INSERT INTO models (id, name, provider, vendor, is_reasoning, is_new, is_stale, status, standard_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", "Test Model 1", "Test Provider 1", "Test Vendor 1", 1, 0, 0, "active", 0.05)
	if err != nil {
		t.Fatalf("setupTestData failed inserting model: %v", err)
	}

	// Insert scores history
	now := time.Now().UTC()
	// Current score (suite = 'current')
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite,
		ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
		ax_edge_cases, ax_debugging, ax_format, ax_safety)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now, 85, 85.0, "up", 80.0, 90.0, "current",
		0.8, 0.7, 0.9, 0.85, 0.9, 0.75, 0.8, 0.95, 0.9)
	if err != nil {
		t.Fatalf("setupTestData failed inserting current score: %v", err)
	}

	// Score from 12 hours ago (within 24h)
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-12*time.Hour), 83, 83.0, "stable", 79.0, 87.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// Score from 5 days ago (within 7d)
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-5*24*time.Hour), 80, 80.0, "stable", 75.0, 85.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// Score from 10 days ago (within 14d)
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-10*24*time.Hour), 78, 78.0, "stable", 73.0, 83.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// Score from 20 days ago (within 30d)
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-20*24*time.Hour), 75, 75.0, "stable", 70.0, 80.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// Insert degradation
	_, err = DB.Exec(`INSERT INTO degradations (model_id, model_name, provider, current_score, baseline_score, drop_percentage, z_score, severity, detected_at, message, type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", "Test Model 1", "Test Provider 1", 75, 85, 11, "2.1", "medium", now, "Performance drop", "score")
	if err != nil {
		t.Fatalf("setupTestData failed inserting degradation: %v", err)
	}

	// Insert alert
	_, err = DB.Exec(`INSERT INTO alerts (model_name, provider, issue, severity, detected_at)
		VALUES (?, ?, ?, ?, ?)`,
		"Test Model 1", "Test Provider 1", "Latency spike", "high", now)
	if err != nil {
		t.Fatalf("setupTestData failed inserting alert: %v", err)
	}

	// Insert global index
	_, err = DB.Exec(`INSERT INTO global_index (timestamp, global_score, models_count, trend, performing_well, total_models)
		VALUES (?, ?, ?, ?, ?, ?)`,
		now, 82, 15, "up", 12, 15)
	if err != nil {
		t.Fatalf("setupTestData failed inserting global index: %v", err)
	}

	// Insert provider reliability
	_, err = DB.Exec(`INSERT INTO provider_reliability (provider, trust_score, total_incidents, incidents_per_month, avg_recovery_hours, last_incident, trend, active_models, top_performers, is_available)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Test Provider 1", 95, 2, 0, "1.5", now, "stable", 5, 2, 1)
	if err != nil {
		t.Fatalf("setupTestData failed inserting provider reliability: %v", err)
	}

	// Insert recommendation
	_, err = DB.Exec(`INSERT INTO recommendations (type, model_id, model_name, vendor, score, reason, evidence, extra_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"best_for_code", "test-model-1", "Test Model 1", "Test Vendor 1", 85, "High code quality score", "Passes all syntax checks", "")
	if err != nil {
		t.Fatalf("setupTestData failed inserting recommendation: %v", err)
	}

	// Insert transparency
	_, err = DB.Exec(`INSERT INTO transparency (id, last_update, total_tests, passed_tests, coverage, confidence, data_points_24h, next_test, models_fresh, models_stale, models_offline)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now, 100, 95, 95, 9, 240, now.Add(10*time.Minute), 5, 1, 0)
	if err != nil {
		t.Fatalf("setupTestData failed inserting transparency: %v", err)
	}

	// Insert model freshness
	_, err = DB.Exec(`INSERT INTO model_freshness (model_name, last_update, minutes_ago, status)
		VALUES (?, ?, ?, ?)`,
		"Test Model 1", now, 5, "fresh")
	if err != nil {
		t.Fatalf("setupTestData failed inserting model freshness: %v", err)
	}
}

func TestMainAPI(t *testing.T) {
	dbPath := "./test_main.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	setupTestData(t)
	SetupRoutes()

	// 1. Test GET /api/config
	{
		req := httptest.NewRequest("GET", "/api/config", nil)
		w := httptest.NewRecorder()
		handleConfig(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/config: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/config: expected Content-Type application/json, got %q", ct)
		}
		var configRes map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &configRes); err != nil {
			t.Errorf("GET /api/config: failed to unmarshal JSON: %v", err)
		}
		if _, ok := configRes["blocked_models"]; !ok {
			t.Error("GET /api/config: response missing 'blocked_models'")
		}
	}

	// 2. Test GET /api/models
	{
		req := httptest.NewRequest("GET", "/api/models", nil)
		w := httptest.NewRecorder()
		handleModels(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/models: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/models: expected Content-Type application/json, got %q", ct)
		}
		var models []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil {
			t.Errorf("GET /api/models: failed to unmarshal JSON: %v", err)
		}
		if len(models) == 0 {
			t.Error("GET /api/models: expected at least one model, got 0")
		} else {
			m := models[0]
			if m["id"] != "test-model-1" || m["name"] != "Test Model 1" || m["provider"] != "Test Provider 1" {
				t.Errorf("GET /api/models: unexpected model data: %+v", m)
			}
		}
	}

	// 3. Test GET /api/scores (none / empty)
	{
		req := httptest.NewRequest("GET", "/api/scores", nil)
		w := httptest.NewRecorder()
		handleScores(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/scores (no period): expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/scores (no period): expected Content-Type application/json, got %q", ct)
		}
		var scores []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &scores); err != nil {
			t.Errorf("GET /api/scores (no period): failed to unmarshal JSON: %v", err)
		}
		if len(scores) == 0 {
			t.Error("GET /api/scores (no period): expected at least one score, got 0")
		} else {
			s := scores[0]
			if s["modelId"] != "test-model-1" || s["score"] != float64(85) {
				t.Errorf("GET /api/scores (no period): unexpected score data: %+v", s)
			}
		}
	}

	// 3. Test GET /api/scores (24h)
	{
		req := httptest.NewRequest("GET", "/api/scores?period=24h", nil)
		w := httptest.NewRecorder()
		handleScores(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/scores?period=24h: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/scores?period=24h: expected Content-Type application/json, got %q", ct)
		}
		var scores []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &scores); err != nil {
			t.Errorf("GET /api/scores?period=24h: failed to unmarshal JSON: %v", err)
		}
		if len(scores) != 2 {
			t.Errorf("GET /api/scores?period=24h: expected 2 points, got %d", len(scores))
		}
	}

	// 3. Test GET /api/scores (7d)
	{
		req := httptest.NewRequest("GET", "/api/scores?period=7d", nil)
		w := httptest.NewRecorder()
		handleScores(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/scores?period=7d: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/scores?period=7d: expected Content-Type application/json, got %q", ct)
		}
		var scores []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &scores); err != nil {
			t.Errorf("GET /api/scores?period=7d: failed to unmarshal JSON: %v", err)
		}
		if len(scores) != 3 {
			t.Errorf("GET /api/scores?period=7d: expected 3 points, got %d", len(scores))
		}
	}

	// 3. Test GET /api/scores (14d)
	{
		req := httptest.NewRequest("GET", "/api/scores?period=14d", nil)
		w := httptest.NewRecorder()
		handleScores(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/scores?period=14d: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/scores?period=14d: expected Content-Type application/json, got %q", ct)
		}
		var scores []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &scores); err != nil {
			t.Errorf("GET /api/scores?period=14d: failed to unmarshal JSON: %v", err)
		}
		if len(scores) != 4 {
			t.Errorf("GET /api/scores?period=14d: expected 4 points, got %d", len(scores))
		}
	}

	// 3. Test GET /api/scores (30d)
	{
		req := httptest.NewRequest("GET", "/api/scores?period=30d", nil)
		w := httptest.NewRecorder()
		handleScores(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/scores?period=30d: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/scores?period=30d: expected Content-Type application/json, got %q", ct)
		}
		var scores []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &scores); err != nil {
			t.Errorf("GET /api/scores?period=30d: failed to unmarshal JSON: %v", err)
		}
		if len(scores) != 5 {
			t.Errorf("GET /api/scores?period=30d: expected 5 points, got %d", len(scores))
		}
	}

	// 4. Test GET /api/degradations
	{
		req := httptest.NewRequest("GET", "/api/degradations", nil)
		w := httptest.NewRecorder()
		handleDegradations(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/degradations: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/degradations: expected Content-Type application/json, got %q", ct)
		}
		var degradations []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &degradations); err != nil {
			t.Errorf("GET /api/degradations: failed to unmarshal JSON: %v", err)
		}
		if len(degradations) == 0 {
			t.Error("GET /api/degradations: expected at least one degradation, got 0")
		} else {
			d := degradations[0]
			if d["modelId"] != "test-model-1" || d["severity"] != "medium" || d["dropPercentage"] != float64(11) {
				t.Errorf("GET /api/degradations: unexpected degradation data: %+v", d)
			}
		}
	}

	// 5. Test GET /api/alerts
	{
		req := httptest.NewRequest("GET", "/api/alerts", nil)
		w := httptest.NewRecorder()
		handleAlerts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/alerts: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/alerts: expected Content-Type application/json, got %q", ct)
		}
		var alerts []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &alerts); err != nil {
			t.Errorf("GET /api/alerts: failed to unmarshal JSON: %v", err)
		}
		if len(alerts) == 0 {
			t.Error("GET /api/alerts: expected at least one alert, got 0")
		} else {
			a := alerts[0]
			if a["modelName"] != "Test Model 1" || a["issue"] != "Latency spike" || a["severity"] != "high" {
				t.Errorf("GET /api/alerts: unexpected alert data: %+v", a)
			}
		}
	}

	// 6. Test handleManualSync Method Not Allowed
	{
		req := httptest.NewRequest("GET", "/api/sync-now", nil)
		w := httptest.NewRecorder()
		handleManualSync(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET /api/sync-now: expected 405 Method Not Allowed, got %d", w.Code)
		}
	}

	// 6. Test handleManualSync POST Success/Failure
	{
		req := httptest.NewRequest("POST", "/api/sync-now", nil)
		w := httptest.NewRecorder()
		handleManualSync(w, req)

		if w.Code == http.StatusOK {
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("POST /api/sync-now: expected Content-Type application/json, got %q", ct)
			}
			var syncRes map[string]bool
			if err := json.Unmarshal(w.Body.Bytes(), &syncRes); err != nil {
				t.Errorf("POST /api/sync-now: failed to unmarshal JSON: %v", err)
			}
			if !syncRes["success"] {
				t.Error("POST /api/sync-now: expected success: true")
			}
		} else if w.Code != http.StatusInternalServerError {
			t.Errorf("POST /api/sync-now: expected 200 or 500, got %d", w.Code)
		}
	}

	// 7. Test GET /api/model/history missing id
	{
		req := httptest.NewRequest("GET", "/api/model/history", nil)
		w := httptest.NewRecorder()
		handleModelHistory(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("GET /api/model/history (missing id): expected 400 Bad Request, got %d", w.Code)
		}
	}

	// 7. Test GET /api/model/history valid id
	{
		req := httptest.NewRequest("GET", "/api/model/history?id=test-model-1", nil)
		w := httptest.NewRecorder()
		handleModelHistory(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/model/history (valid id): expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/model/history: expected Content-Type application/json, got %q", ct)
		}
		var history []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
			t.Errorf("GET /api/model/history: failed to unmarshal JSON: %v", err)
		}
		if len(history) != 5 {
			t.Errorf("GET /api/model/history (valid id): expected 5 points, got %d", len(history))
		}
	}

	// 7. Test GET /api/model/history with days limit
	{
		req := httptest.NewRequest("GET", "/api/model/history?id=test-model-1&days=7", nil)
		w := httptest.NewRecorder()
		handleModelHistory(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/model/history?days=7: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/model/history?days=7: expected Content-Type application/json, got %q", ct)
		}
		var history []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
			t.Errorf("GET /api/model/history?days=7: failed to unmarshal JSON: %v", err)
		}
		if len(history) != 3 {
			t.Errorf("GET /api/model/history?days=7: expected 3 points, got %d", len(history))
		}
	}

	// Test GET /api/global-index
	{
		req := httptest.NewRequest("GET", "/api/global-index", nil)
		w := httptest.NewRecorder()
		handleGlobalIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/global-index: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/global-index: expected Content-Type application/json, got %q", ct)
		}
	}

	// Test GET /api/provider-reliability
	{
		req := httptest.NewRequest("GET", "/api/provider-reliability", nil)
		w := httptest.NewRecorder()
		handleProviderReliability(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/provider-reliability: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/provider-reliability: expected Content-Type application/json, got %q", ct)
		}
	}

	// Test GET /api/recommendations
	{
		req := httptest.NewRequest("GET", "/api/recommendations", nil)
		w := httptest.NewRecorder()
		handleRecommendations(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/recommendations: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/recommendations: expected Content-Type application/json, got %q", ct)
		}
	}

	// Test GET /api/transparency
	{
		req := httptest.NewRequest("GET", "/api/transparency", nil)
		w := httptest.NewRecorder()
		handleTransparency(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/transparency: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/transparency: expected Content-Type application/json, got %q", ct)
		}
	}

	// Test GET /api/sync-status
	{
		req := httptest.NewRequest("GET", "/api/sync-status", nil)
		w := httptest.NewRecorder()
		handleSyncStatus(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET /api/sync-status: expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET /api/sync-status: expected Content-Type application/json, got %q", ct)
		}
	}
}

func TestConfigFunctions(t *testing.T) {
	const tempConfigFilename = "config.json"

	var backupData []byte
	backupExists := false
	if _, err := os.Stat(tempConfigFilename); err == nil {
		backupData, err = os.ReadFile(tempConfigFilename)
		if err != nil {
			t.Fatalf("Failed to backup config.json: %v", err)
		}
		backupExists = true
		if err := os.Remove(tempConfigFilename); err != nil {
			t.Fatalf("Failed to remove config.json: %v", err)
		}
	}

	defer func() {
		_ = os.Remove(tempConfigFilename)
		if backupExists {
			_ = os.WriteFile(tempConfigFilename, backupData, 0644)
		}
	}()

	deleteConfigFile := func() {
		_ = os.Remove(tempConfigFilename)
	}

	t.Run("loadConfig missing file", func(t *testing.T) {
		deleteConfigFile()

		configMu.Lock()
		config.BlockedModels = []string{"dummy"}
		configMu.Unlock()

		loadConfig()

		blocked := getBlockedModels()
		if len(blocked) != 0 {
			t.Errorf("Expected empty BlockedModels, got %v", blocked)
		}
	})

	t.Run("loadConfig valid JSON", func(t *testing.T) {
		deleteConfigFile()

		validJSON := `{"blocked_models": ["model-a", "model-b"]}`
		if err := os.WriteFile(tempConfigFilename, []byte(validJSON), 0644); err != nil {
			t.Fatalf("Failed to write mock config.json: %v", err)
		}

		loadConfig()

		blocked := getBlockedModels()
		if len(blocked) != 2 || blocked[0] != "model-a" || blocked[1] != "model-b" {
			t.Errorf("Expected [model-a model-b], got %v", blocked)
		}
	})

	t.Run("loadConfig invalid JSON", func(t *testing.T) {
		deleteConfigFile()

		invalidJSON := `{"blocked_models":`
		if err := os.WriteFile(tempConfigFilename, []byte(invalidJSON), 0644); err != nil {
			t.Fatalf("Failed to write mock config.json: %v", err)
		}

		configMu.Lock()
		config.BlockedModels = []string{"dummy"}
		configMu.Unlock()

		loadConfig()

		blocked := getBlockedModels()
		if len(blocked) != 0 {
			t.Errorf("Expected empty BlockedModels, got %v", blocked)
		}
	})

	t.Run("saveConfig writes valid JSON", func(t *testing.T) {
		deleteConfigFile()

		configMu.Lock()
		config.BlockedModels = []string{"saved-model-1", "saved-model-2"}
		configMu.Unlock()

		err := saveConfig()
		if err != nil {
			t.Fatalf("saveConfig failed: %v", err)
		}

		data, err := os.ReadFile(tempConfigFilename)
		if err != nil {
			t.Fatalf("Failed to read saved config.json: %v", err)
		}

		var parsed Config
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to parse saved config: %v", err)
		}

		if len(parsed.BlockedModels) != 2 || parsed.BlockedModels[0] != "saved-model-1" || parsed.BlockedModels[1] != "saved-model-2" {
			t.Errorf("Expected [saved-model-1 saved-model-2] in saved config, got %v", parsed.BlockedModels)
		}
	})

	t.Run("getBlockedModels returns a copy", func(t *testing.T) {
		configMu.Lock()
		config.BlockedModels = []string{"model-x", "model-y"}
		configMu.Unlock()

		blocked := getBlockedModels()
		if len(blocked) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(blocked))
		}

		blocked[0] = "mutated-model"

		original := getBlockedModels()
		if original[0] != "model-x" {
			t.Errorf("Global config was mutated! Expected original[0] to be 'model-x', got %s", original[0])
		}
	})

	t.Run("setBlockedModels updates config", func(t *testing.T) {
		deleteConfigFile()

		newModels := []string{"new-1", "new-2"}
		setBlockedModels(newModels)

		var data []byte
		var err error
		for i := 0; i < 50; i++ {
			data, err = os.ReadFile(tempConfigFilename)
			if err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("Failed to read saved config.json after async save: %v", err)
		}

		var parsed Config
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to parse config: %v", err)
		}

		if len(parsed.BlockedModels) != 2 || parsed.BlockedModels[0] != "new-1" || parsed.BlockedModels[1] != "new-2" {
			t.Errorf("Expected config file to have [new-1 new-2], got %v", parsed.BlockedModels)
		}

		blocked := getBlockedModels()
		if len(blocked) != 2 || blocked[0] != "new-1" || blocked[1] != "new-2" {
			t.Errorf("Expected in-memory config to have [new-1 new-2], got %v", blocked)
		}
	})

	t.Run("Concurrent access safety", func(t *testing.T) {
		deleteConfigFile()

		var wg sync.WaitGroup
		numWorkers := 10
		iterations := 100

		wg.Add(numWorkers * 2)

		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = getBlockedModels()
				}
			}()
		}

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					models := []string{
						fmt.Sprintf("model-%d-%d", workerID, j),
					}
					setBlockedModels(models)
				}
			}(i)
		}

		wg.Wait()
	})
}
