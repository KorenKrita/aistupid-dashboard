package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Global variables for mock server control
var (
	integrationMockCachedResponse       string
	integrationMockAlertsResponse       string
	integrationMockGlobalIndexResponse  string
	integrationMockProviderRelResponse  string
	integrationMockRecommendationsResp  string
	integrationMockTransparencyResponse string
	integrationMockCachedStatus         int
)

func TestIntegrationFlow(t *testing.T) {
	// Initialize mock status
	integrationMockCachedStatus = http.StatusOK

	// Initialize mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			w.WriteHeader(integrationMockCachedStatus)
			if integrationMockCachedStatus == http.StatusOK {
				w.Write([]byte(integrationMockCachedResponse))
			} else {
				w.Write([]byte(`{"success":false,"error":"internal error"}`))
			}
		case "/dashboard/alerts":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(integrationMockAlertsResponse))
		case "/dashboard/global-index":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(integrationMockGlobalIndexResponse))
		case "/analytics/provider-reliability":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(integrationMockProviderRelResponse))
		case "/analytics/recommendations":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(integrationMockRecommendationsResp))
		case "/analytics/transparency":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(integrationMockTransparencyResponse))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Direct apiBaseURL to mock server
	oldBaseURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldBaseURL }()

	// Use temporary SQLite database for testing
	dbPath := "./test_integration_flow.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Prepare mock timestamps
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourAgoStr := oneHourAgo.Format(time.RFC3339)
	twoHoursAgoStr := now.Add(-2 * time.Hour).Format(time.RFC3339)

	// Set initial mock responses
	integrationMockCachedResponse = `{
		"success": true,
		"data": {
			"modelScores": [
				{
					"id": "model-integration-1",
					"name": "Model Integration One",
					"provider": "Provider A",
					"vendor": "Vendor X",
					"currentScore": 85,
					"score": 85,
					"trend": "stable",
					"lastUpdated": "` + nowStr + `",
					"status": "online",
					"isNew": true,
					"isStale": false,
					"usesReasoningEffort": true,
					"confidenceLower": 83.2,
					"confidenceUpper": 86.8,
					"standardError": 0.5
				}
			],
			"historyMap": {
				"model-integration-1": [
					{
						"timestamp": "` + oneHourAgoStr + `",
						"score": 84,
						"stupidScore": 84.0,
						"suite": "standard",
						"axes": {
							"correctness": 0.85,
							"complexity": 0.75,
							"codeQuality": 0.8,
							"efficiency": 0.9,
							"stability": 0.95,
							"edgeCases": 0.7,
							"debugging": 0.8,
							"format": 0.85,
							"safety": 0.95,
							"memoryRetention": 0.6,
							"hallucinationRate": 0.1,
							"planCoherence": 0.8,
							"contextWindow": 0.7
						},
						"confidence_lower": 82.0,
						"confidence_upper": 86.0
					}
				]
			},
			"degradations": [
				{
					"modelId": "model-integration-1",
					"modelName": "Model Integration One",
					"provider": "Provider A",
					"currentScore": 85,
					"baselineScore": 95,
					"dropPercentage": 10,
					"zScore": "2.1",
					"severity": "medium",
					"detectedAt": "` + twoHoursAgoStr + `",
					"message": "Performance drop standard",
					"type": "degradation_type"
				}
			]
		}
	}`

	integrationMockAlertsResponse = `{"success": true, "data": []}`
	integrationMockGlobalIndexResponse = `{"success": true, "data": {"current": {"timestamp": "` + nowStr + `", "globalScore": 80, "modelsCount": 1}, "history": [], "trend": "stable", "performingWell": 1, "totalModels": 1}}`
	integrationMockProviderRelResponse = `{"success": true, "data": []}`
	integrationMockRecommendationsResp = `{"success": true, "data": {"avoidNow": []}}`
	integrationMockTransparencyResponse = `{"success": true, "data": {"summary": {}, "modelFreshness": []}}`

	// -------------------------------------------------------------
	// Scenario 1: Full sync to query flow
	// -------------------------------------------------------------
	t.Run("Full sync to query flow", func(t *testing.T) {
		err := FetchAndSync()
		if err != nil {
			t.Fatalf("FetchAndSync failed: %v", err)
		}

		// Query handleModels
		reqModels := httptest.NewRequest("GET", "/api/models", nil)
		recModels := httptest.NewRecorder()
		handleModels(recModels, reqModels)

		if recModels.Code != http.StatusOK {
			t.Errorf("handleModels returned status %d", recModels.Code)
		}

		var models []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			IsReasoning bool   `json:"isReasoning"`
			IsNew       bool   `json:"isNew"`
		}
		if err := json.Unmarshal(recModels.Body.Bytes(), &models); err != nil {
			t.Fatalf("Failed to unmarshal models: %v", err)
		}
		if len(models) != 1 || models[0].ID != "model-integration-1" {
			t.Errorf("Unexpected models returned: %+v", models)
		}
		if !models[0].IsReasoning {
			t.Error("Expected IsReasoning to be true")
		}
		if !models[0].IsNew {
			t.Error("Expected IsNew to be true")
		}

		// Query handleScores (latest score)
		reqScores := httptest.NewRequest("GET", "/api/scores", nil)
		recScores := httptest.NewRecorder()
		handleScores(recScores, reqScores)

		if recScores.Code != http.StatusOK {
			t.Errorf("handleScores returned status %d", recScores.Code)
		}

		var latestScores []struct {
			ModelID   string `json:"modelId"`
			ModelName string `json:"modelName"`
			Score     int    `json:"score"`
			Timestamp string `json:"timestamp"`
			Axes      struct {
				Correctness *float64 `json:"correctness"`
			} `json:"axes"`
		}
		if err := json.Unmarshal(recScores.Body.Bytes(), &latestScores); err != nil {
			t.Fatalf("Failed to unmarshal latest scores: %v", err)
		}
		if len(latestScores) != 1 || latestScores[0].ModelID != "model-integration-1" {
			t.Errorf("Unexpected latest scores returned: %+v", latestScores)
		}
		if latestScores[0].Score != 85 {
			t.Errorf("Expected current score 85, got %d", latestScores[0].Score)
		}
		if latestScores[0].Axes.Correctness == nil || *latestScores[0].Axes.Correctness != 0.85 {
			t.Errorf("Expected axes.correctness to be 0.85, got %+v", latestScores[0].Axes.Correctness)
		}

		// Query handleScores (with period=24h)
		reqScoresHist := httptest.NewRequest("GET", "/api/scores?period=24h", nil)
		recScoresHist := httptest.NewRecorder()
		handleScores(recScoresHist, reqScoresHist)

		if recScoresHist.Code != http.StatusOK {
			t.Errorf("handleScores (24h) returned status %d", recScoresHist.Code)
		}

		var scoresHist []struct {
			ModelID   string `json:"modelId"`
			Score     int    `json:"score"`
			Suite     string `json:"suite"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(recScoresHist.Body.Bytes(), &scoresHist); err != nil {
			t.Fatalf("Failed to unmarshal scores history: %v", err)
		}
		// Expect 1 point: only non-current history (score 84, suite standard)
		// suite='current' records are filtered out by handleModelHistory
		if len(scoresHist) != 1 {
			t.Errorf("Expected 1 scores history point (current filtered out), got %d: %+v", len(scoresHist), scoresHist)
		}

		// Query handleDegradations
		reqDegradations := httptest.NewRequest("GET", "/api/degradations", nil)
		recDegradations := httptest.NewRecorder()
		handleDegradations(recDegradations, reqDegradations)

		if recDegradations.Code != http.StatusOK {
			t.Errorf("handleDegradations returned status %d", recDegradations.Code)
		}

		var degradations []struct {
			ModelID        string `json:"modelId"`
			CurrentScore   int    `json:"currentScore"`
			DropPercentage int    `json:"dropPercentage"`
			Severity       string `json:"severity"`
			DetectedAt     string `json:"detectedAt"`
		}
		if err := json.Unmarshal(recDegradations.Body.Bytes(), &degradations); err != nil {
			t.Fatalf("Failed to unmarshal degradations: %v", err)
		}
		if len(degradations) != 1 || degradations[0].ModelID != "model-integration-1" {
			t.Errorf("Unexpected degradations: %+v", degradations)
		}
	})

	// -------------------------------------------------------------
	// Scenario 2: Data consistency
	// -------------------------------------------------------------
	t.Run("Data consistency", func(t *testing.T) {
		// 1. Verify model ID relations match between scores_history and models table
		var count int
		err := DB.QueryRow(`
			SELECT COUNT(*)
			FROM scores_history h
			JOIN models m ON h.model_id = m.id
			WHERE m.id = 'model-integration-1'`).Scan(&count)
		if err != nil {
			t.Fatalf("Database query failed: %v", err)
		}
		// We expect 2 matching records in scores_history
		if count != 2 {
			t.Errorf("Expected 2 related scores_history entries, got %d", count)
		}

		// 2. Verify model ID relations match between degradations and models table
		err = DB.QueryRow(`
			SELECT COUNT(*)
			FROM degradations d
			JOIN models m ON d.model_id = m.id
			WHERE m.id = 'model-integration-1'`).Scan(&count)
		if err != nil {
			t.Fatalf("Database query failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 related degradation entry, got %d", count)
		}

		// 3. Verify timestamps format in response JSON (must be valid RFC3339)
		reqScores := httptest.NewRequest("GET", "/api/scores", nil)
		recScores := httptest.NewRecorder()
		handleScores(recScores, reqScores)
		var scores []map[string]interface{}
		_ = json.Unmarshal(recScores.Body.Bytes(), &scores)
		if len(scores) > 0 {
			tsStr, ok := scores[0]["timestamp"].(string)
			if !ok {
				t.Error("Scores response timestamp is not a string")
			}
			_, err = time.Parse(time.RFC3339, tsStr)
			if err != nil {
				t.Errorf("Invalid RFC3339 score timestamp %q: %v", tsStr, err)
			}
		}

		reqDegs := httptest.NewRequest("GET", "/api/degradations", nil)
		recDegs := httptest.NewRecorder()
		handleDegradations(recDegs, reqDegs)
		var degs []map[string]interface{}
		_ = json.Unmarshal(recDegs.Body.Bytes(), &degs)
		if len(degs) > 0 {
			daStr, ok := degs[0]["detectedAt"].(string)
			if !ok {
				t.Error("Degradations response detectedAt is not a string")
			}
			_, err = time.Parse(time.RFC3339, daStr)
			if err != nil {
				t.Errorf("Invalid RFC3339 degradation detectedAt %q: %v", daStr, err)
			}
		}
	})

	// -------------------------------------------------------------
	// Scenario 3: Update scenarios
	// -------------------------------------------------------------
	t.Run("Update scenarios", func(t *testing.T) {
		// 1. Sync twice with same data, verify no duplicates
		err := FetchAndSync()
		if err != nil {
			t.Fatalf("Second FetchAndSync failed: %v", err)
		}

		var modelCount, scoresCount, degsCount int
		DB.QueryRow("SELECT COUNT(*) FROM models").Scan(&modelCount)
		DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&scoresCount)
		DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degsCount)

		if modelCount != 1 {
			t.Errorf("Expected 1 model, got %d", modelCount)
		}
		if scoresCount != 2 {
			t.Errorf("Expected 2 scores (1 standard hist + 1 current), got %d", scoresCount)
		}
		if degsCount != 1 {
			t.Errorf("Expected 1 degradation, got %d", degsCount)
		}

		// 2. Verify INSERT OR IGNORE works correctly for scores_history
		// Manually insert same history point directly, verify no constraint failure
		_, err = DB.Exec(`INSERT OR IGNORE INTO scores_history (model_id, timestamp, score, suite)
			VALUES ('model-integration-1', ?, 84, 'standard')`, oneHourAgo)
		if err != nil {
			t.Errorf("Manually inserting duplicate history failed: %v", err)
		}

		// 3. Verify UPDATE logic for degradations: z_score/severity updates, detected_at stays same
		// Update mock response with updated degradation details
		integrationMockCachedResponse = `{
			"success": true,
			"data": {
				"modelScores": [
					{
						"id": "model-integration-1",
						"name": "Model Integration One",
						"provider": "Provider A",
						"vendor": "Vendor X",
						"currentScore": 75,
						"score": 75,
						"trend": "down",
						"lastUpdated": "` + nowStr + `",
						"status": "online",
						"isNew": true,
						"isStale": false,
						"usesReasoningEffort": true,
						"confidenceLower": 73.2,
						"confidenceUpper": 76.8,
						"standardError": 0.5
					}
				],
				"historyMap": {
					"model-integration-1": [
						{
							"timestamp": "` + oneHourAgoStr + `",
							"score": 84,
							"stupidScore": 84.0,
							"suite": "standard",
							"axes": {
								"correctness": 0.85
							}
						}
					]
				},
				"degradations": [
					{
						"modelId": "model-integration-1",
						"modelName": "Model Integration One",
						"provider": "Provider A",
						"currentScore": 75,
						"baselineScore": 95,
						"dropPercentage": 21,
						"zScore": "3.5",
						"severity": "high",
						"detectedAt": "` + nowStr + `",
						"message": "Performance drop standard",
						"type": "degradation_type"
					}
				]
			}
		}`

		err = FetchAndSync()
		if err != nil {
			t.Fatalf("Third FetchAndSync failed: %v", err)
		}

		var currentScore, dropPercentage int
		var zScore, severity, detectedAt string
		err = DB.QueryRow(`
			SELECT current_score, drop_percentage, z_score, severity, detected_at
			FROM degradations
			WHERE model_id = 'model-integration-1'`).Scan(&currentScore, &dropPercentage, &zScore, &severity, &detectedAt)
		if err != nil {
			t.Fatalf("Failed to query degradation: %v", err)
		}

		if currentScore != 75 {
			t.Errorf("Expected updated current score 75, got %d", currentScore)
		}
		if dropPercentage != 21 {
			t.Errorf("Expected updated drop percentage 21, got %d", dropPercentage)
		}
		if zScore != "3.5" {
			t.Errorf("Expected updated z-score 3.5, got %s", zScore)
		}
		if severity != "high" {
			t.Errorf("Expected updated severity 'high', got %s", severity)
		}

		// The detectedAt MUST still be twoHoursAgoStr (which we originally passed), NOT the new nowStr!
		parsedDetectedAt, _ := time.Parse(time.RFC3339, detectedAt)
		expectedDetectedAt, _ := time.Parse(time.RFC3339, twoHoursAgoStr)
		if !parsedDetectedAt.Equal(expectedDetectedAt) {
			t.Errorf("Expected detected_at to remain %s, but got %s", twoHoursAgoStr, detectedAt)
		}

		// 4. Verify removal of degradations no longer in API response
		integrationMockCachedResponse = `{
			"success": true,
			"data": {
				"modelScores": [],
				"historyMap": {},
				"degradations": []
			}
		}`
		err = FetchAndSync()
		if err != nil {
			t.Fatalf("Fourth FetchAndSync failed: %v", err)
		}

		DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degsCount)
		if degsCount != 0 {
			t.Errorf("Expected degradation to be deleted, but still got %d", degsCount)
		}
	})

	// -------------------------------------------------------------
	// Scenario 4: Cleanup scenarios
	// -------------------------------------------------------------
	t.Run("Cleanup scenarios", func(t *testing.T) {
		// Prepare old data (>60 days old)
		oldTime := time.Now().UTC().AddDate(0, 0, -65)

		// Create a model first (if not exists)
		_, err := DB.Exec(`INSERT OR IGNORE INTO models (id, name, provider, vendor) VALUES ('model-cleanup', 'Cleanup Model', 'P', 'V')`)
		if err != nil {
			t.Fatalf("Failed to insert cleanup model: %v", err)
		}

		// Insert history score and global index older than 60 days
		_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-cleanup', ?, 50, 'old')`, oldTime)
		if err != nil {
			t.Fatalf("Failed to insert old score history: %v", err)
		}

		_, err = DB.Exec(`INSERT INTO global_index (timestamp, global_score) VALUES (?, 40)`, oldTime)
		if err != nil {
			t.Fatalf("Failed to insert old global index: %v", err)
		}

		// Run sync (which triggers pruning)
		err = FetchAndSync()
		if err != nil {
			t.Fatalf("Pruning FetchAndSync failed: %v", err)
		}

		// Verify old records are pruned
		var count int
		DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE suite = 'old'").Scan(&count)
		if count != 0 {
			t.Errorf("Expected old score history to be pruned, got %d", count)
		}

		DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE global_score = 40").Scan(&count)
		if count != 0 {
			t.Errorf("Expected old global index to be pruned, got %d", count)
		}

		// Verify cascade delete
		// Let's insert a score and degradation for model-cleanup
		_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-cleanup', ?, 99, 'standard')`, now)
		if err != nil {
			t.Fatalf("Failed to insert cascade check score: %v", err)
		}

		_, err = DB.Exec(`INSERT INTO degradations (model_id, drop_percentage, severity, detected_at, type, message)
			VALUES ('model-cleanup', 15, 'high', ?, 'score', 'drop')`, now)
		if err != nil {
			t.Fatalf("Failed to insert cascade check degradation: %v", err)
		}

		// Verify they are in DB
		DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'model-cleanup'").Scan(&count)
		if count == 0 {
			t.Fatal("Cascade check score was not inserted")
		}
		DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'model-cleanup'").Scan(&count)
		if count == 0 {
			t.Fatal("Cascade check degradation was not inserted")
		}

		// Delete model-cleanup
		_, err = DB.Exec("DELETE FROM models WHERE id = 'model-cleanup'")
		if err != nil {
			t.Fatalf("Failed to delete model: %v", err)
		}

		// Verify scores and degradations deleted
		DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'model-cleanup'").Scan(&count)
		if count != 0 {
			t.Errorf("Cascade delete failed: scores_history still has %d records", count)
		}
		DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'model-cleanup'").Scan(&count)
		if count != 0 {
			t.Errorf("Cascade delete failed: degradations still has %d records", count)
		}
	})

	// -------------------------------------------------------------
	// Scenario 5: Error recovery (Rollback on error)
	// -------------------------------------------------------------
	t.Run("Error recovery", func(t *testing.T) {
		// Populate initial successful state
		integrationMockCachedResponse = `{
			"success": true,
			"data": {
				"modelScores": [
					{
						"id": "model-integration-rollback",
						"name": "Original Name",
						"provider": "Provider A",
						"vendor": "Vendor X",
						"currentScore": 80,
						"score": 80,
						"trend": "stable",
						"lastUpdated": "` + nowStr + `",
						"status": "online"
					}
				],
				"historyMap": {},
				"degradations": []
			}
		}`

		err := FetchAndSync()
		if err != nil {
			t.Fatalf("Initial rollback sync failed: %v", err)
		}

		// Make sure it exists in DB
		var name string
		err = DB.QueryRow("SELECT name FROM models WHERE id = 'model-integration-rollback'").Scan(&name)
		if err != nil || name != "Original Name" {
			t.Fatalf("Setup verification failed: %v", err)
		}

		// Modify mock response to have a new name, but configure mock server to return 500
		integrationMockCachedResponse = `{
			"success": true,
			"data": {
				"modelScores": [
					{
						"id": "model-integration-rollback",
						"name": "Modified Name",
						"provider": "Provider A",
						"vendor": "Vendor X",
						"currentScore": 80,
						"score": 80,
						"trend": "stable",
						"lastUpdated": "` + nowStr + `",
						"status": "online"
					}
				],
				"historyMap": {},
				"degradations": []
			}
		}`
		integrationMockCachedStatus = http.StatusInternalServerError

		// Call FetchAndSync, should fail
		err = FetchAndSync()
		if err == nil {
			t.Error("Expected FetchAndSync to fail due to 500 status, but got nil error")
		}

		// Verify that the database model name was NOT updated to "Modified Name" (rolled back)
		err = DB.QueryRow("SELECT name FROM models WHERE id = 'model-integration-rollback'").Scan(&name)
		if err != nil {
			t.Fatalf("Failed to query model: %v", err)
		}
		if name != "Original Name" {
			t.Errorf("Expected model name to remain 'Original Name', but got '%s' (changes were not rolled back)", name)
		}
	})
}
