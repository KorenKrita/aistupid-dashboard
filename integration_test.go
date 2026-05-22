package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// 全局变量，用于在测试间控制 mock HTTP 服务器的响应内容。
// 通过修改这些变量可以在不同测试场景中模拟不同的上游 API 行为。
var (
	integrationMockCachedResponse       string
	integrationMockAlertsResponse       string
	integrationMockGlobalIndexResponse  string
	integrationMockProviderRelResponse  string
	integrationMockRecommendationsResp  string
	integrationMockTransparencyResponse string
	integrationMockCachedStatus         int
)

// TestIntegrationFlow 端到端集成测试，覆盖完整的同步、查询、更新、清理和错误恢复流程。
// 使用 mock HTTP 服务器模拟上游 API 的所有端点，在一个测试函数中串联 5 个场景：
// 1. 完整同步 -> 查询流程
// 2. 数据一致性验证
// 3. 更新场景（幂等性、INSERT OR REPLACE、缺陷更新、缺陷删除）
// 4. 清理场景（旧数据清理、级联删除）
// 5. 错误恢复（同步失败时事务回滚）
func TestIntegrationFlow(t *testing.T) {
	integrationMockCachedStatus = http.StatusOK

	// 创建 mock HTTP 服务器，通过全局变量动态切换各端点的响应内容
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

	// 将 apiBaseURL 指向 mock 服务器
	oldBaseURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldBaseURL }()

	// 使用临时的 SQLite 数据库文件运行测试
	dbPath := "./test_integration_flow.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 准备 mock 时间戳，用于填充上游 API 响应中的各个时间字段
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourAgoStr := oneHourAgo.Format(time.RFC3339)
	twoHoursAgoStr := now.Add(-2 * time.Hour).Format(time.RFC3339)

	// 设置初始 mock 响应数据：包含 1 个模型、1 条历史分数（含完整 13 个 axis）、1 条退化记录
	// 模型配置为 isNew=true、usesReasoningEffort=true，用于验证布尔字段解析
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
	// 场景 1：完整同步 -> 查询流程
	// 同步数据后分别查询 /api/models、/api/scores、/api/degradations 端点，
	// 验证各端点返回的数据与上游数据一致。
	// -------------------------------------------------------------
	t.Run("Full sync to query flow", func(t *testing.T) {
		err := FetchAndSync()
		if err != nil {
			t.Fatalf("FetchAndSync failed: %v", err)
		}

		// 查询 handleModels，验证模型列表返回 1 条且布尔字段正确
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

		// 查询 handleScores（无 period 参数，默认返回最新分数），验证 score=85 且 axes.correctness=0.85
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

		// 查询 handleScores?period=24h，期望返回 2 条：历史（score=84, suite=standard）+ 当前（score=85, suite=current）
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
		// 期望 2 条：history（score=84, suite=standard）+ current（score=85, suite=current）
		if len(scoresHist) != 2 {
			t.Errorf("Expected 2 scores history points, got %d: %+v", len(scoresHist), scoresHist)
		}

		// 查询 handleDegradations，验证退化记录字段正确性
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
	// 场景 2：数据一致性验证
	// 验证外键关联完整性以及响应 JSON 中时间戳格式的合法性。
	// -------------------------------------------------------------
	t.Run("Data consistency", func(t *testing.T) {
		// 验证 scores_history 与 models 表的外键关联完整性，期望 2 条匹配记录
		var count int
		err := DB.QueryRow(`
			SELECT COUNT(*)
			FROM scores_history h
			JOIN models m ON h.model_id = m.id
			WHERE m.id = 'model-integration-1'`).Scan(&count)
		if err != nil {
			t.Fatalf("Database query failed: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 related scores_history entries, got %d", count)
		}

		// 验证 degradations 与 models 表的外键关联完整性，期望 1 条匹配记录
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

		// 验证 scores 响应中的 timestamp 字段符合 RFC3339 格式
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

		// 验证 degradations 响应中的 detectedAt 字段符合 RFC3339 格式
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
	// 场景 3：更新场景
	// 覆盖幂等性（重复同步不产生重复记录）、INSERT OR REPLACE 行为、
	// 缺陷记录更新（detectedAt 保持不变）、以及上游删除后同步清理。
	// -------------------------------------------------------------
	t.Run("Update scenarios", func(t *testing.T) {
		// 1. 重复同步相同数据，验证不产生重复记录（幂等性）
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

		// 2. 验证 INSERT OR IGNORE 不会因重复键报错
		_, err = DB.Exec(`INSERT OR IGNORE INTO scores_history (model_id, timestamp, score, suite)
			VALUES ('model-integration-1', ?, 84, 'standard')`, oneHourAgo)
		if err != nil {
			t.Errorf("Manually inserting duplicate history failed: %v", err)
		}

		// 3. 验证缺陷更新逻辑：更新 currentScore、dropPercentage、zScore、severity，
		//    detectedAt 应保留原始值（twoHoursAgoStr）而非更新为新值
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

		// detectedAt 应保留首次插入时的值 twoHoursAgoStr，而非被新值覆盖
		parsedDetectedAt, _ := time.Parse(time.RFC3339, detectedAt)
		expectedDetectedAt, _ := time.Parse(time.RFC3339, twoHoursAgoStr)
		if !parsedDetectedAt.Equal(expectedDetectedAt) {
			t.Errorf("Expected detected_at to remain %s, but got %s", twoHoursAgoStr, detectedAt)
		}

		// 4. 验证上游删除缺陷记录后，同步时本地应同步删除
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
	// 场景 4：清理场景
	// 验证超过 60 天的 scores_history 和 global_index 被自动清理，
	// 以及外键级联删除（删除 models 行后子表记录自动删除）。
	// -------------------------------------------------------------
	t.Run("Cleanup scenarios", func(t *testing.T) {
		oldTime := time.Now().UTC().AddDate(0, 0, -65)

		// 创建一个清理测试模型（INSERT OR IGNORE 避免重复创建）
		_, err := DB.Exec(`INSERT OR IGNORE INTO models (id, name, provider, vendor) VALUES ('model-cleanup', 'Cleanup Model', 'P', 'V')`)
		if err != nil {
			t.Fatalf("Failed to insert cleanup model: %v", err)
		}

		// 插入 65 天前的旧数据，期望在执行同步时被清理
		_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-cleanup', ?, 50, 'old')`, oldTime)
		if err != nil {
			t.Fatalf("Failed to insert old score history: %v", err)
		}

		_, err = DB.Exec(`INSERT INTO global_index (timestamp, global_score) VALUES (?, 40)`, oldTime)
		if err != nil {
			t.Fatalf("Failed to insert old global index: %v", err)
		}

		// 执行同步，触发 60 天数据清理
		err = FetchAndSync()
		if err != nil {
			t.Fatalf("Pruning FetchAndSync failed: %v", err)
		}

		// 验证旧 scores_history 被清理
		var count int
		DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE suite = 'old'").Scan(&count)
		if count != 0 {
			t.Errorf("Expected old score history to be pruned, got %d", count)
		}

		// 验证旧 global_index 被清理
		DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE global_score = 40").Scan(&count)
		if count != 0 {
			t.Errorf("Expected old global index to be pruned, got %d", count)
		}

		// 验证外键级联删除：为 model-cleanup 插入子表记录后删除父表，子表应自动清空
		_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-cleanup', ?, 99, 'standard')`, now)
		if err != nil {
			t.Fatalf("Failed to insert cascade check score: %v", err)
		}

		_, err = DB.Exec(`INSERT INTO degradations (model_id, drop_percentage, severity, detected_at, type, message)
			VALUES ('model-cleanup', 15, 'high', ?, 'score', 'drop')`, now)
		if err != nil {
			t.Fatalf("Failed to insert cascade check degradation: %v", err)
		}

		// 确认子记录已存在
		DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE model_id = 'model-cleanup'").Scan(&count)
		if count == 0 {
			t.Fatal("Cascade check score was not inserted")
		}
		DB.QueryRow("SELECT COUNT(*) FROM degradations WHERE model_id = 'model-cleanup'").Scan(&count)
		if count == 0 {
			t.Fatal("Cascade check degradation was not inserted")
		}

		// 删除父模型
		_, err = DB.Exec("DELETE FROM models WHERE id = 'model-cleanup'")
		if err != nil {
			t.Fatalf("Failed to delete model: %v", err)
		}

		// 验证子表记录被级联删除
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
	// 场景 5：错误恢复
	// 验证上游 API 返回 500 时，FetchAndSync 不修改数据库已有数据
	// （事务回滚保证数据一致性）。
	// -------------------------------------------------------------
	t.Run("Error recovery", func(t *testing.T) {
		// 先同步成功状态：插入一个模型
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

		// 确认模型已存在
		var name string
		err = DB.QueryRow("SELECT name FROM models WHERE id = 'model-integration-rollback'").Scan(&name)
		if err != nil || name != "Original Name" {
			t.Fatalf("Setup verification failed: %v", err)
		}

		// 修改 mock 响应中的数据（name 改为 "Modified Name"），同时让服务器返回 500
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

		// 执行同步，预期会失败（500 错误）
		err = FetchAndSync()
		if err == nil {
			t.Error("Expected FetchAndSync to fail due to 500 status, but got nil error")
		}

		// 验证数据库中的模型名仍为 "Original Name"，未被更新为 "Modified Name"
		// 这说明事务回滚生效，部分写入未被提交
		err = DB.QueryRow("SELECT name FROM models WHERE id = 'model-integration-rollback'").Scan(&name)
		if err != nil {
			t.Fatalf("Failed to query model: %v", err)
		}
		if name != "Original Name" {
			t.Errorf("Expected model name to remain 'Original Name', but got '%s' (changes were not rolled back)", name)
		}
	})
}
