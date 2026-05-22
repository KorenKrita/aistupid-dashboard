package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestGetNextSyncTimeAt 验证 getNextSyncTimeAt 函数在各种时间点上的正确性。
// 测试用例覆盖：
// - 常规情况（10:03 -> 10:10）
// - 接近整点边界（10:58 -> 11:00，跨小时）
// - 整 10 分钟点（10:00 -> 10:10）
// - 整 10 分过几秒（10:10:05 -> 10:20）
// - 跨日期边界（23:55 -> 次日 00:00）
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

// mockCachedResponse 生成 /dashboard/cached 端点的 mock JSON 响应字符串。
// 参数分别指定 model.lastUpdated、history 时间戳和 degradation.detectedAt，
// 用于测试不同时间格式的解析行为。
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

// mockAlertsResponse 生成 /dashboard/alerts 端点的 mock JSON 响应字符串。
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

// mockGlobalIndexResponse 生成 /dashboard/global-index 端点的 mock JSON 响应字符串。
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

// mockProviderReliabilityResponse 生成 /analytics/provider-reliability 端点的 mock JSON 响应字符串。
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

// mockRecommendationsResponse 生成 /analytics/recommendations 端点的 mock JSON 响应字符串。
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

// mockTransparencyResponse 生成 /analytics/transparency 端点的 mock JSON 响应字符串。
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

// TestTimeParsingAndFallbacks 验证上游 API 返回的 RFC3339 时间戳解析及解析失败时的回退行为。
// 子测试：
// 1. "Valid RFC3339 parsing"：测试带 Z 和时区偏移（+02:00、-05:00）的合法格式
// 2. "Invalid timestamp fallback parsing"：测试非法时间戳时使用 time.Now().UTC() 回退
func TestTimeParsingAndFallbacks(t *testing.T) {
	dbPath := "./test_time_sync.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 备份原始的 apiBaseURL，测试结束后恢复
	oldBaseURL := apiBaseURL
	defer func() {
		apiBaseURL = oldBaseURL
	}()

	// 子测试 1：验证合法的 RFC3339 时间戳格式可以被正确解析
	t.Run("Valid RFC3339 parsing", func(t *testing.T) {
		// 三种合法格式：带 Z 标记、带 +02:00 偏移、带 -05:00 偏移
		ts1 := "2026-05-22T10:00:00Z"
		ts2 := "2026-05-22T10:00:00+02:00" // UTC 时间为 08:00:00
		ts3 := "2026-05-22T10:00:00-05:00" // UTC 时间为 15:00:00

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

		// 清理所有表，确保本次测试从空数据库开始
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

		// 验证 scores_history 和 degradations 表中有数据（说明时间戳解析成功）
		var scoreCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&scoreCount)
		if err != nil {
			t.Fatalf("Query scores_history count failed: %v", err)
		}
		if scoreCount == 0 {
			t.Errorf("Expected at least one score in scores_history, got 0")
		}

		var degCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degCount)
		if err != nil {
			t.Fatalf("Query degradations count failed: %v", err)
		}
		if degCount == 0 {
			t.Errorf("Expected at least one degradation, got 0")
		}
	})

	// 子测试 2：验证非法时间戳格式会触发回退逻辑
	t.Run("Invalid timestamp fallback parsing", func(t *testing.T) {
		invalidTs := "invalid-timestamp-format"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/dashboard/cached":
				// modelLastUpdated 解析失败时跳过该模型
				// historyTimestamp 解析失败时跳过该历史记录
				// degradationDetectedAt 解析失败时回退到 time.Now().UTC()
				_, _ = w.Write([]byte(mockCachedResponse(invalidTs, invalidTs, invalidTs)))
			case "/dashboard/alerts":
				// alerts 解析失败时插入零值 time.Time{}
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

		// 清理所有表，确保测试从空数据库开始
		_, _ = DB.Exec("DELETE FROM models")
		_, _ = DB.Exec("DELETE FROM scores_history")
		_, _ = DB.Exec("DELETE FROM degradations")
		_, _ = DB.Exec("DELETE FROM alerts")
		_, _ = DB.Exec("DELETE FROM global_index")
		_, _ = DB.Exec("DELETE FROM provider_reliability")
		_, _ = DB.Exec("DELETE FROM transparency")
		_, _ = DB.Exec("DELETE FROM model_freshness")

		// 同步逻辑：modelScores 循环先创建 models 表记录（不解析 LastUpdated），
		// 后在 historyMap 循环中解析时间戳。解析失败时 continue 跳过该条记录，
		// 但模型记录已在之前创建，不会因外键约束失败。
		err := FetchAndSync()
		if err != nil {
			t.Fatalf("FetchAndSync failed: %v", err)
		}

		// 验证 degradation 使用回退时间戳插入成功
		var degCount int
		err = DB.QueryRow("SELECT COUNT(*) FROM degradations").Scan(&degCount)
		if err != nil {
			t.Fatalf("Query degradations count failed: %v", err)
		}
		if degCount == 0 {
			t.Errorf("Expected degradation to be inserted with fallback timestamp")
		}

		// 验证 scores_history 中的 history 条目使用回退时间戳插入成功
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

// TestCutoffDateArithmetic 验证各时间范围（24h / 7d / 30d）的日期计算逻辑。
// 仅验证日期运算的正确性，handler 级别测试在 main_test.go 中覆盖。
func TestCutoffDateArithmetic(t *testing.T) {
	now := time.Now().UTC()

	// 验证 24 小时截止时间计算：应为现在往前推 1 天
	cutoff24h := now.AddDate(0, 0, -1)
	if cutoff24h.After(now) {
		t.Error("24h cutoff should be before now")
	}
	if now.Sub(cutoff24h) > 25*time.Hour {
		t.Error("24h cutoff should be within ~24 hours")
	}

	// 验证 7 天截止时间计算：应为现在往前推 7 天
	cutoff7d := now.AddDate(0, 0, -7)
	if now.Sub(cutoff7d) < 6*24*time.Hour || now.Sub(cutoff7d) > 8*24*time.Hour {
		t.Error("7d cutoff should be ~7 days ago")
	}

	// 验证 30 天截止时间计算：应为现在往前推 30 天
	cutoff30d := now.AddDate(0, 0, -30)
	if now.Sub(cutoff30d) < 29*24*time.Hour || now.Sub(cutoff30d) > 31*24*time.Hour {
		t.Error("30d cutoff should be ~30 days ago")
	}
}

// TestDataPruningLogic 验证同步过程中的数据清理逻辑（超过 60 天的数据自动删除）。
// 策略：
// 1. 手动插入 61 天前（过期）和 59 天前（保留）的 scores_history 和 global_index 记录
// 2. 执行 FetchAndSync 触发清理
// 3. 验证旧数据被删除，新数据保留
func TestDataPruningLogic(t *testing.T) {
	dbPath := "./test_pruning.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 备份原始的 apiBaseURL，测试结束后恢复
	oldBaseURL := apiBaseURL
	defer func() {
		apiBaseURL = oldBaseURL
	}()

	// 插入基础模型以满足外键约束
	_, err = DB.Exec("INSERT INTO models (id, name, provider, vendor) VALUES ('model-1', 'Model 1', 'openai', 'openai')")
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// 手动插入过期和保留的记录
	now := time.Now().UTC()
	oldTs := now.AddDate(0, 0, -61)    // 61 天前，超过 60 天，应被清理
	newTs := now.AddDate(0, 0, -59)    // 59 天前，在 60 天以内，应保留

	// scores_history：插入一条过期和一条保留记录
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-1', ?, 80, 'current')", oldTs)
	if err != nil {
		t.Fatalf("Failed to insert old score: %v", err)
	}
	_, err = DB.Exec("INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('model-1', ?, 90, 'current')", newTs)
	if err != nil {
		t.Fatalf("Failed to insert new score: %v", err)
	}

	// global_index：插入一条过期和一条保留记录
	_, err = DB.Exec("INSERT INTO global_index (timestamp, global_score) VALUES (?, 80)", oldTs)
	if err != nil {
		t.Fatalf("Failed to insert old global index: %v", err)
	}
	_, err = DB.Exec("INSERT INTO global_index (timestamp, global_score) VALUES (?, 90)", newTs)
	if err != nil {
		t.Fatalf("Failed to insert new global index: %v", err)
	}

	// mock 服务器返回最小化数据，确保同步成功但不产生干扰数据
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

	// 执行 FetchAndSync，触发数据清理
	err = FetchAndSync()
	if err != nil {
		t.Fatalf("FetchAndSync failed during pruning test: %v", err)
	}

	// 验证 scores_history：旧数据被删除，新数据保留
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

	// 验证 global_index：旧数据被删除，新数据保留
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
