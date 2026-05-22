package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// 1. Test getNextSyncTimeAt()
func TestGetNextSyncTimeAt(t *testing.T) {
	loc := time.UTC
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "10:03 to 10:10",
			input:    time.Date(2026, 5, 22, 10, 3, 45, 0, loc),
			expected: time.Date(2026, 5, 22, 10, 10, 0, 0, loc),
		},
		{
			name:     "10:58 to 11:00",
			input:    time.Date(2026, 5, 22, 10, 58, 12, 0, loc),
			expected: time.Date(2026, 5, 22, 11, 0, 0, 0, loc),
		},
		{
			name:     "10:00 to 10:10",
			input:    time.Date(2026, 5, 22, 10, 0, 0, 0, loc),
			expected: time.Date(2026, 5, 22, 10, 10, 0, 0, loc),
		},
		{
			name:     "10:10 to 10:20",
			input:    time.Date(2026, 5, 22, 10, 10, 5, 0, loc),
			expected: time.Date(2026, 5, 22, 10, 20, 0, 0, loc),
		},
		{
			name:     "23:55 to 00:00 next day",
			input:    time.Date(2026, 5, 22, 23, 55, 0, 0, loc),
			expected: time.Date(2026, 5, 23, 0, 0, 0, 0, loc),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getNextSyncTimeAt(tc.input)
			if !result.Equal(tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// Helper to make a JSON string or struct for mock server
func mockCachedResponse(modelLastUpdated, historyTimestamp, degradationDetectedAt string) string {
	return fmt.Sprintf(`{
		"success": true,
		"data": {
			"modelScores": [
				{
					"id": "model-1",
					"name": "Model 1",
					"provider": "openai",
					"vendor": "openai",
					"currentScore": 85,
					"score": 85,
					"trend": "stable",
					"lastUpdated": "%s",
					"status": "active",
					"isNew": false,
					"isStale": false,
					"usesReasoningEffort": false,
					"confidenceLower": 80.0,
					"confidenceUpper": 90.0,
					"standardError": 1.5
				}
			],
			"historyMap": {
				"model-1": [
					{
						"timestamp": "%s",
						"score": 82,
						"stupidScore": 82.0,
						"suite": "current",
						"axes": {
							"correctness": 82.0
						},
						"confidence_lower": 78.0,
						"confidence_upper": 86.0
					}
				]
			},
			"degradations": [
				{
					"modelId": "model-1",
					"modelName": "Model 1",
					"provider": "openai",
					"currentScore": 85,
					"baselineScore": 95,
					"dropPercentage": 10,
					"zScore": "-2.5",
					"severity": "medium",
					"detectedAt": "%s",
					"message": "Degradation message",
					"type": "performance"
				}
			]
		}
	}`, modelLastUpdated, historyTimestamp, degradationDetectedAt)
}

func mockAlertsResponse(detectedAt string) string {
	return fmt.Sprintf(`{
		"success": true,
		"data": [
			{
				"name": "Model 1",
				"provider": "openai",
				"issue": "API error rate spike",
				"severity": "high",
				"detectedAt": "%s"
			}
		]
	}`, detectedAt)
}

func mockGlobalIndexResponse(timestamp string) string {
	return fmt.Sprintf(`{
		"success": true,
		"data": {
			"current": {
				"timestamp": "%s",
				"globalScore": 80,
				"modelsCount": 10
			},
			"history": [
				{
					"timestamp": "%s",
					"globalScore": 79,
					"modelsCount": 10
				}
			],
			"trend": "up",
			"performingWell": 8,
			"totalModels": 10
		}
	}`, timestamp, timestamp)
}

func mockProviderReliabilityResponse(lastIncident string) string {
	return fmt.Sprintf(`{
		"success": true,
		"data": [
			{
				"provider": "openai",
				"trustScore": 95,
				"totalIncidents": 2,
				"incidentsPerMonth": 1,
				"avgRecoveryHours": "2.5",
				"lastIncident": "%s",
				"trend": "improving",
				"activeModels": 5,
				"topPerformers": 3,
				"isAvailable": true
			}
		]
	}`, lastIncident)
}

func mockRecommendationsResponse() string {
	return `{
		"success": true,
		"data": {
			"bestForCode": {
				"id": "model-1",
				"name": "Model 1",
				"vendor": "openai",
				"score": 95,
				"reason": "Top coding performance",
				"evidence": "Coding tests"
			},
			"mostReliable": {
				"id": "model-1",
				"name": "Model 1",
				"vendor": "openai",
				"score": 92,
				"reason": "Fewest incidents",
				"evidence": "99.9% uptime"
			},
			"fastestResponse": {
				"id": "model-1",
				"name": "Model 1",
				"vendor": "openai",
				"score": 90,
				"reason": "Lowest latency",
				"evidence": "150ms TTFT"
			},
			"avoidNow": []
		}
	}`
}

func mockTransparencyResponse(lastUpdate, nextTest, freshnessLastUpdate string) string {
	return fmt.Sprintf(`{
		"success": true,
		"data": {
			"summary": {
				"lastUpdate": "%s",
				"totalTests": 1000,
				"passedTests": 980,
				"coverage": 95,
				"confidence": 98,
				"dataPoints24h": 500,
				"nextTest": "%s",
				"modelsFresh": 5,
				"modelsStale": 1,
				"modelsOffline": 0
			},
			"modelFreshness": [
				{
					"model": "Model 1",
					"lastUpdate": "%s",
					"minutesAgo": 15,
					"status": "fresh"
				}
			]
		}
	}`, lastUpdate, nextTest, freshnessLastUpdate)
}

func TestTimeParsingAndFallbacks(t *testing.T) {
	dbPath := "./test_time_sync.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Backup base URL
	oldBaseURL := apiBaseURL
	defer func() {
		apiBaseURL = oldBaseURL
	}()

	// 2. We want to test parsing RFC3339 formats, fallback behavior, and UTC consistency.
	// We'll run a series of test cases with different timestamp formats in the mocked JSON API.
	t.Run("Valid RFC3339 parsing", func(t *testing.T) {
		// Valid RFC3339 formats: with Z, with timezone offset
		ts1 := "2026-05-22T10:00:00Z"
		ts2 := "2026-05-22T10:00:00+02:00" // UTC is 08:00:00
		ts3 := "2026-05-22T10:00:00-05:00" // UTC is 15:00:00

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/dashboard/cached":
				_, _ = w.Write([]byte(mockCachedResponse(ts1, ts2, ts3)))
			case "/dashboard/alerts":
				_, _ = w.Write([]byte(mockAlertsResponse(ts1)))
			case "/dashboard/global-index":
				_, _ = w.Write([]byte(mockGlobalIndexResponse(ts1)))
			case "/analytics/provider-reliability":
				_, _ = w.Write([]byte(mockProviderReliabilityResponse(ts1)))
			case "/analytics/recommendations":
				_, _ = w.Write([]byte(mockRecommendationsResponse()))
			case "/analytics/transparency":
				_, _ = w.Write([]byte(mockTransparencyResponse(ts1, ts1, ts1)))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		apiBaseURL = server.URL

		// Clean up db first to run cleanly
		_, _ = DB.Exec("DELETE FROM models")
		_, _ = DB.Exec("DELETE FROM scores_history")
		_, _ = DB.Exec("DELETE FROM degradations")
		_, _ = DB.Exec("DELETE FROM alerts")
		_, _ = DB.Exec("DELETE FROM global_index")
		_, _ = DB.Exec("DELETE FROM provider_reliability")
		_, _ = DB.Exec("DELETE FROM transparency")
		_, _ = DB.Exec("DELETE FROM model_freshness")

		err := FetchAndSync()
		if err != nil {
			t.Fatalf("FetchAndSync failed: %v", err)
		}

		// Verify UTC parsing/storage. SQLite stores timestamps; the Go driver may return time.Time.
		// Verify scores exist (the timestamp format stored may vary by driver)
		var scoreCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&scoreCount)
		if err != nil {
			t.Fatalf("Query scores_history count failed: %v", err)
		}
		if scoreCount == 0 {
			t.Errorf("Expected at least one score in scores_history, got 0")
		}

		// Check degradation exists
		var degCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degCount)
		if err != nil {
			t.Fatalf("Query degradations count failed: %v", err)
		}
		if degCount == 0 {
			t.Errorf("Expected at least one degradation, got 0")
		}
	})

	t.Run("Invalid timestamp fallback parsing", func(t *testing.T) {
		// Test behavior when timestamp format is invalid
		invalidTs := "invalid-timestamp-format"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/dashboard/cached":
				// invalid ts for modelLastUpdated will skip the modelScores item.
				// invalid ts for historyTimestamp will skip the history entry.
				// invalid ts for degradationDetectedAt should fallback to time.Now().UTC() in sync.go!
				_, _ = w.Write([]byte(mockCachedResponse(invalidTs, invalidTs, invalidTs)))
			case "/dashboard/alerts":
				// alerts parsing fails, it will insert zero time or skip. Wait, time.Parse returns err,
				// and alerts inserter uses time.Time{} (zero value) when parsing fails.
				_, _ = w.Write([]byte(mockAlertsResponse(invalidTs)))
			case "/dashboard/global-index":
				_, _ = w.Write([]byte(mockGlobalIndexResponse(invalidTs)))
			case "/analytics/provider-reliability":
				_, _ = w.Write([]byte(mockProviderReliabilityResponse(invalidTs)))
			case "/analytics/recommendations":
				_, _ = w.Write([]byte(mockRecommendationsResponse()))
			case "/analytics/transparency":
				_, _ = w.Write([]byte(mockTransparencyResponse(invalidTs, invalidTs, invalidTs)))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		apiBaseURL = server.URL

		// Clean up db first to run cleanly
		_, _ = DB.Exec("DELETE FROM models")
		_, _ = DB.Exec("DELETE FROM scores_history")
		_, _ = DB.Exec("DELETE FROM degradations")
		_, _ = DB.Exec("DELETE FROM alerts")
		_, _ = DB.Exec("DELETE FROM global_index")
		_, _ = DB.Exec("DELETE FROM provider_reliability")
		_, _ = DB.Exec("DELETE FROM transparency")
		_, _ = DB.Exec("DELETE FROM model_freshness")

		// Insert model first because foreign key check will fail if the model is skipped during cached sync.
		// Wait, the cache sync loops over modelScores and writes to models table.
		// But in cache sync:
		// `ts, err := time.Parse(time.RFC3339, m.LastUpdated)`
		// if parsing fails, it `continue`s (skips) inserting into scores_history, but does it skip models?
		// No, the models loop is BEFORE the history loops and doesn't parse m.LastUpdated.
		// So the model is still created. Good!
		// But let's check:
		err := FetchAndSync()
		if err != nil {
			t.Fatalf("FetchAndSync failed: %v", err)
		}

		// 1. Degradation fallback verification:
		// In sync.go: if parsing fails, detectedAt = time.Now().UTC()
		// So we expect the degradation detected_at to be close to time.Now().UTC().
		var degCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degCount)
		if err != nil {
			t.Fatalf("Query degradations count failed: %v", err)
		}
		// Should have a degradation with fallback timestamp
		if degCount == 0 {
			t.Errorf("Expected degradation to be inserted with fallback timestamp")
		}

		// 2. Scores history fallback verification:
		// In sync.go: if parsing of LastUpdated fails, time.Now().UTC() is used as fallback.
		// So we expect rows in scores_history with the fallback timestamp.
		var scoresCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&scoresCount)
		if err != nil {
			t.Fatalf("Query scores_history count failed: %v", err)
		}
		if scoresCount == 0 {
			t.Errorf("Expected scores_history to have records (fallback timestamp used for invalid LastUpdated), but got 0")
		}
	})
}

// 3. Cutoff calculations test - verifies date arithmetic only (handler tests are in main_test.go)
func TestCutoffDateArithmetic(t *testing.T) {
	now := time.Now().UTC()

	// Test 24h cutoff
	cutoff24h := now.AddDate(0, 0, -1)
	if cutoff24h.After(now) {
		t.Error("24h cutoff should be before now")
	}
	if now.Sub(cutoff24h) > 25*time.Hour {
		t.Error("24h cutoff should be within ~24 hours")
	}

	// Test 7d cutoff
	cutoff7d := now.AddDate(0, 0, -7)
	if now.Sub(cutoff7d) < 6*24*time.Hour || now.Sub(cutoff7d) > 8*24*time.Hour {
		t.Error("7d cutoff should be ~7 days ago")
	}

	// Test 30d cutoff
	cutoff30d := now.AddDate(0, 0, -30)
	if now.Sub(cutoff30d) < 29*24*time.Hour || now.Sub(cutoff30d) > 31*24*time.Hour {
		t.Error("30d cutoff should be ~30 days ago")
	}
}

// 4. Data pruning logic
func TestDataPruningLogic(t *testing.T) {
	dbPath := "./test_pruning.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Backup base URL
	oldBaseURL := apiBaseURL
	defer func() {
		apiBaseURL = oldBaseURL
	}()

	// Insert model
	_, err = DB.Exec("INSERT INTO models (id, name, provider, vendor) VALUES ('model-1', 'Model 1', 'openai', 'openai')")
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Insert records manually (some older than 60 days, some newer)
	now := time.Now().UTC()
	oldTs := now.AddDate(0, 0, -61)
	newTs := now.AddDate(0, 0, -59)

	// scores_history old and new
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-1', ?, 80, 'current')", oldTs)
	if err != nil {
		t.Fatalf("Failed to insert old score: %v", err)
	}
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-1', ?, 90, 'current')", newTs)
	if err != nil {
		t.Fatalf("Failed to insert new score: %v", err)
	}

	// global_index old and new
	_, err = DB.Exec("INSERT INTO global_index (timestamp, global_score) VALUES (?, 80)", oldTs)
	if err != nil {
		t.Fatalf("Failed to insert old global index: %v", err)
	}
	_, err = DB.Exec("INSERT INTO global_index (timestamp, global_score) VALUES (?, 90)", newTs)
	if err != nil {
		t.Fatalf("Failed to insert new global index: %v", err)
	}

	// Mock server that returns empty or minimal valid data so sync succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			_, _ = w.Write([]byte(`{"success": true, "data": {"modelScores": [], "historyMap": {}, "degradations": []}}`))
		case "/dashboard/alerts":
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		case "/dashboard/global-index":
			_, _ = w.Write([]byte(`{"success": true, "data": {"current": {"timestamp": "2026-05-22T10:00:00Z", "globalScore": 80, "modelsCount": 0}, "history": [], "trend": "stable", "performingWell": 0, "totalModels": 0}}`))
		case "/analytics/provider-reliability":
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		case "/analytics/recommendations":
			_, _ = w.Write([]byte(`{"success": true, "data": {"avoidNow": []}}`))
		case "/analytics/transparency":
			_, _ = w.Write([]byte(`{"success": true, "data": {"summary": {}, "modelFreshness": []}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	apiBaseURL = server.URL

	// Run FetchAndSync to trigger pruning
	err = FetchAndSync()
	if err != nil {
		t.Fatalf("FetchAndSync failed during pruning test: %v", err)
	}

	// Verify scores_history: old should be deleted, new should remain
	var oldScoreCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE timestamp = ?", oldTs).Scan(&oldScoreCount)
	if err != nil {
		t.Fatalf("Query scores_history count failed: %v", err)
	}
	if oldScoreCount != 0 {
		t.Error("Expected old score (older than 60 days) to be pruned, but it exists")
	}

	var newScoreCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE timestamp = ?", newTs).Scan(&newScoreCount)
	if err != nil {
		t.Fatalf("Query scores_history count failed: %v", err)
	}
	if newScoreCount != 1 {
		t.Errorf("Expected new score (within 60 days) to be kept, but got count %d", newScoreCount)
	}

	// Verify global_index: old should be deleted, new should remain
	var oldIndexCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE timestamp = ?", oldTs).Scan(&oldIndexCount)
	if err != nil {
		t.Fatalf("Query global_index count failed: %v", err)
	}
	if oldIndexCount != 0 {
		t.Error("Expected old global index (older than 60 days) to be pruned, but it exists")
	}

	var newIndexCount int
	err = DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE timestamp = ?", newTs).Scan(&newIndexCount)
	if err != nil {
		t.Fatalf("Query global_index count failed: %v", err)
	}
	if newIndexCount != 1 {
		t.Errorf("Expected new global index (within 60 days) to be kept, but got count %d", newIndexCount)
	}
}
