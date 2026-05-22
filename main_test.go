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

// setupTestData 向测试数据库中插入一组完整的测试数据，涵盖所有表：
// models、scores_history、degradations、alerts、global_index、
// provider_reliability、recommendations、transparency、model_freshness。
// 时间点覆盖当前、12小时前、5天前、10天前、20天前，用于测试不同时间范围的时间序列查询。
func setupTestData(t *testing.T) {
	// 插入一个测试模型
	_, err := DB.Exec(`INSERT INTO models (id, name, provider, vendor, is_reasoning, is_new, is_stale, status, standard_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", "Test Model 1", "Test Provider 1", "Test Vendor 1", 1, 0, 0, "active", 0.05)
	if err != nil {
		t.Fatalf("setupTestData failed inserting model: %v", err)
	}

	now := time.Now().UTC()

	// 插入当前分数（suite='current'），用于测试 /api/scores 无 period 参数时返回最新分数
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite,
		ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
		ax_edge_cases, ax_debugging, ax_format, ax_safety)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now, 85, 85.0, "up", 80.0, 90.0, "current",
		0.8, 0.7, 0.9, 0.85, 0.9, 0.75, 0.8, 0.95, 0.9)
	if err != nil {
		t.Fatalf("setupTestData failed inserting current score: %v", err)
	}

	// 插入 12 小时前的历史分数（在 24h 查询范围内），用于测试 period=24h 时应返回 2 条记录
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-12*time.Hour), 83, 83.0, "stable", 79.0, 87.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// 插入 5 天前的历史分数（在 7d 查询范围内），用于测试 period=7d 时应返回 3 条记录
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-5*24*time.Hour), 80, 80.0, "stable", 75.0, 85.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// 插入 10 天前的历史分数（在 14d 查询范围内），用于测试 period=14d 时应返回 4 条记录
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-10*24*time.Hour), 78, 78.0, "stable", 73.0, 83.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// 插入 20 天前的历史分数（在 30d 查询范围内），用于测试 period=30d 时应返回 5 条记录
	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", now.Add(-20*24*time.Hour), 75, 75.0, "stable", 70.0, 80.0, "regular")
	if err != nil {
		t.Fatalf("setupTestData failed inserting history score: %v", err)
	}

	// 插入一条性能退化记录，用于测试 /api/degradations 端点
	_, err = DB.Exec(`INSERT INTO degradations (model_id, model_name, provider, current_score, baseline_score, drop_percentage, z_score, severity, detected_at, message, type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test-model-1", "Test Model 1", "Test Provider 1", 75, 85, 11, "2.1", "medium", now, "Performance drop", "score")
	if err != nil {
		t.Fatalf("setupTestData failed inserting degradation: %v", err)
	}

	// 插入一条告警记录，用于测试 /api/alerts 端点
	_, err = DB.Exec(`INSERT INTO alerts (model_name, provider, issue, severity, detected_at)
		VALUES (?, ?, ?, ?, ?)`,
		"Test Model 1", "Test Provider 1", "Latency spike", "high", now)
	if err != nil {
		t.Fatalf("setupTestData failed inserting alert: %v", err)
	}

	// 插入全局健康指数，用于测试 /api/global-index 端点
	_, err = DB.Exec(`INSERT INTO global_index (timestamp, global_score, models_count, trend, performing_well, total_models)
		VALUES (?, ?, ?, ?, ?, ?)`,
		now, 82, 15, "up", 12, 15)
	if err != nil {
		t.Fatalf("setupTestData failed inserting global index: %v", err)
	}

	// 插入提供商可靠性数据，用于测试 /api/provider-reliability 端点
	_, err = DB.Exec(`INSERT INTO provider_reliability (provider, trust_score, total_incidents, incidents_per_month, avg_recovery_hours, last_incident, trend, active_models, top_performers, is_available)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"Test Provider 1", 95, 2, 0, "1.5", now, "stable", 5, 2, 1)
	if err != nil {
		t.Fatalf("setupTestData failed inserting provider reliability: %v", err)
	}

	// 插入一条模型推荐，用于测试 /api/recommendations 端点
	_, err = DB.Exec(`INSERT INTO recommendations (type, model_id, model_name, vendor, score, reason, evidence, extra_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"best_for_code", "test-model-1", "Test Model 1", "Test Vendor 1", 85, "High code quality score", "Passes all syntax checks", "")
	if err != nil {
		t.Fatalf("setupTestData failed inserting recommendation: %v", err)
	}

	// 插入透明度/测试覆盖数据，用于测试 /api/transparency 端点
	_, err = DB.Exec(`INSERT INTO transparency (id, last_update, total_tests, passed_tests, coverage, confidence, data_points_24h, next_test, models_fresh, models_stale, models_offline)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now, 100, 95, 95, 9, 240, now.Add(10*time.Minute), 5, 1, 0)
	if err != nil {
		t.Fatalf("setupTestData failed inserting transparency: %v", err)
	}

	// 插入模型新鲜度数据，用于测试 model_freshness 表的关联数据
	_, err = DB.Exec(`INSERT INTO model_freshness (model_name, last_update, minutes_ago, status)
		VALUES (?, ?, ?, ?)`,
		"Test Model 1", now, 5, "fresh")
	if err != nil {
		t.Fatalf("setupTestData failed inserting model freshness: %v", err)
	}
}

// TestMainAPI 测试所有主要 API 端点的 HTTP 行为，包括：
// - 正常返回 200 状态码
// - Content-Type 为 application/json
// - 响应体可正确解析为预期的 JSON 结构
// - 不同 period 参数对 scores 查询结果数量的影响
func TestMainAPI(t *testing.T) {
	// 使用独立的测试数据库文件，测试结束后清理
	dbPath := "./test_main.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 准备完整的测试数据：一个模型 + 5 条分数 + 退化/告警/全局指数等关联数据
	setupTestData(t)
	SetupRoutes()

	// 测试 GET /api/config：验证返回 200 和 blocked_models 字段
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

	// 测试 GET /api/models：验证返回模型列表且字段正确
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

	// 测试 GET /api/scores（无 period 参数）：默认只返回 suite='current' 的最新分数，应返回 1 条
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

	// 测试 GET /api/scores?period=24h：24 小时内共 2 条（当前 + 12 小时前）
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
		// 期望 2 条：suite='current'（现在）+ suite='regular'（12 小时前）
		if len(scores) != 2 {
			t.Errorf("GET /api/scores?period=24h: expected 2 points, got %d", len(scores))
		}
	}

	// 测试 GET /api/scores?period=7d：7 天内共 3 条（当前 + 12h + 5d）
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
		// 期望 3 条：当前 + 12h + 5d
		if len(scores) != 3 {
			t.Errorf("GET /api/scores?period=7d: expected 3 points, got %d", len(scores))
		}
	}

	// 测试 GET /api/scores?period=14d：14 天内共 4 条（当前 + 12h + 5d + 10d）
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
		// 期望 4 条：当前 + 12h + 5d + 10d
		if len(scores) != 4 {
			t.Errorf("GET /api/scores?period=14d: expected 4 points, got %d", len(scores))
		}
	}

	// 测试 GET /api/scores?period=30d：30 天内共 5 条（当前 + 12h + 5d + 10d + 20d）
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
		// 期望 5 条：当前 + 12h + 5d + 10d + 20d
		if len(scores) != 5 {
			t.Errorf("GET /api/scores?period=30d: expected 5 points, got %d", len(scores))
		}
	}

	// 测试 GET /api/degradations：验证退化记录的正确性
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

	// 测试 GET /api/alerts：验证告警记录的字段正确性
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

	// 测试 handleManualSync 对非 POST 请求返回 405 Method Not Allowed
	{
		req := httptest.NewRequest("GET", "/api/sync-now", nil)
		w := httptest.NewRecorder()
		handleManualSync(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET /api/sync-now: expected 405 Method Not Allowed, got %d", w.Code)
		}
	}

	// 测试 handleManualSync POST 请求：成功时返回 200+success=true，失败时返回 500
	// 由于没有 mock 上游 API，此测试验证响应码在预期范围内即可
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

	// 测试 GET /api/model/history 缺失 id 参数时返回 400 Bad Request
	{
		req := httptest.NewRequest("GET", "/api/model/history", nil)
		w := httptest.NewRecorder()
		handleModelHistory(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("GET /api/model/history (missing id): expected 400 Bad Request, got %d", w.Code)
		}
	}

	// 测试 GET /api/model/history 有效 id：返回该模型的所有历史分数（共 5 条）
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
		// 期望 5 条：当前 + 12h + 5d + 10d + 20d
		if len(history) != 5 {
			t.Errorf("GET /api/model/history (valid id): expected 5 points, got %d", len(history))
		}
	}

	// 测试 GET /api/model/history 带 days=7 参数：只返回 7 天内的 3 条分数
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
		// days=7 只返回当前 + 12h + 5d
		if len(history) != 3 {
			t.Errorf("GET /api/model/history?days=7: expected 3 points, got %d", len(history))
		}
	}

	// 测试 GET /api/global-index：验证返回 200 和 JSON 响应头
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

	// 测试 GET /api/provider-reliability：验证返回 200 和 JSON 响应头
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

	// 测试 GET /api/recommendations：验证返回 200 和 JSON 响应头
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

	// 测试 GET /api/transparency：验证返回 200 和 JSON 响应头
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

	// 测试 GET /api/sync-status：验证返回 200 和 JSON 响应头
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

// TestConfigFunctions 测试配置文件的加载、保存和并发安全行为。
// 使用临时 config.json 文件，测试完成后恢复原始文件。
// 子测试覆盖 missing file、valid JSON、invalid JSON、save、copy safety、set 和 concurrent access。
func TestConfigFunctions(t *testing.T) {
	const tempConfigFilename = "config.json"

	// 备份现有的 config.json（如果存在），测试结束后恢复
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

	// 场景：配置文件不存在时，loadConfig 应清空 BlockedModels
	t.Run("loadConfig missing file", func(t *testing.T) {
		deleteConfigFile()

		// 先设置一个假值，验证 loadConfig 会清空它
		configMu.Lock()
		config.BlockedModels = []string{"dummy"}
		configMu.Unlock()

		loadConfig()

		blocked := getBlockedModels()
		if len(blocked) != 0 {
			t.Errorf("Expected empty BlockedModels, got %v", blocked)
		}
	})

	// 场景：配置文件包含合法 JSON 时，loadConfig 应正确解析
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

	// 场景：配置文件包含非法 JSON 时，loadConfig 应清空 BlockedModels 且不 panic
	t.Run("loadConfig invalid JSON", func(t *testing.T) {
		deleteConfigFile()

		invalidJSON := `{"blocked_models":`
		if err := os.WriteFile(tempConfigFilename, []byte(invalidJSON), 0644); err != nil {
			t.Fatalf("Failed to write mock config.json: %v", err)
		}

		// 先设置一个假值，验证 loadConfig 会清空它
		configMu.Lock()
		config.BlockedModels = []string{"dummy"}
		configMu.Unlock()

		loadConfig()

		blocked := getBlockedModels()
		if len(blocked) != 0 {
			t.Errorf("Expected empty BlockedModels, got %v", blocked)
		}
	})

	// 场景：saveConfig 应写入合法的 JSON 到文件
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

	// 场景：getBlockedModels 返回的是副本而非原始切片，外部修改不影响全局状态
	t.Run("getBlockedModels returns a copy", func(t *testing.T) {
		configMu.Lock()
		config.BlockedModels = []string{"model-x", "model-y"}
		configMu.Unlock()

		blocked := getBlockedModels()
		if len(blocked) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(blocked))
		}

		// 修改返回的切片
		blocked[0] = "mutated-model"

		// 重新获取，验证全局配置未被污染
		original := getBlockedModels()
		if original[0] != "model-x" {
			t.Errorf("Global config was mutated! Expected original[0] to be 'model-x', got %s", original[0])
		}
	})

	// 场景：setBlockedModels 应同步更新内存和磁盘上的配置
	t.Run("setBlockedModels updates config", func(t *testing.T) {
		deleteConfigFile()

		newModels := []string{"new-1", "new-2"}
		setBlockedModels(newModels)

		// 由于保存是异步 goroutine 执行的，需要轮询等待文件写入完成
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

	// 场景：并发读写 config 不应导致 data race 或 panic。
	// 使用 10 个 reader 和 10 个 writer goroutine，各执行 100 次读写操作。
	t.Run("Concurrent access safety", func(t *testing.T) {
		deleteConfigFile()

		var wg sync.WaitGroup
		numWorkers := 10
		iterations := 100

		wg.Add(numWorkers * 2)

		// 并发 reader：反复读取 BlockedModels
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = getBlockedModels()
				}
			}()
		}

		// 并发 writer：反复设置不同的 BlockedModels 值
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
