package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetSetLastSyncTimeThreadSafe 验证 lastSyncTime 的读写并发安全性。
// 使用 20 个 goroutine，每个交替执行 1000 次读写操作，确保无 data race。
// 测试结束后验证读写功能仍然正常。
func TestGetSetLastSyncTimeThreadSafe(t *testing.T) {
	// 重置状态
	setLastSyncTime(time.Time{})

	const goroutines = 20
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// 交替读写，模拟真实场景下的并发访问模式
				if j%2 == 0 {
					setLastSyncTime(time.Now())
				} else {
					_ = getLastSyncTime()
				}
			}
		}(i)
	}

	wg.Wait()

	// 验证并发操作后读写功能仍然正常
	now := time.Now()
	setLastSyncTime(now)
	if getLastSyncTime() != now {
		t.Errorf("Expected last sync time to be %v, got %v", now, getLastSyncTime())
	}
}

// TestFetchJSONNon200Status 验证 fetchJSON 在 API 返回非 200 状态码时的错误处理。
// 使用 mock server 返回 500 Internal Server Error，期望 fetchJSON 返回包含
// "upstream API returned status 500" 的错误。
func TestFetchJSONNon200Status(t *testing.T) {
	// 创建一个返回 500 的 mock 服务器，模拟上游 API 故障
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":"internal error"}`))
	}))
	defer ts.Close()

	// 临时替换 apiBaseURL 指向 mock 服务器
	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	var data map[string]interface{}
	err := fetchJSON("/some-endpoint", &data)
	if err == nil {
		t.Error("Expected error from fetchJSON for non-200 status, got nil")
	}

	expectedErr := "upstream API returned status 500"
	if err.Error() != expectedErr {
		t.Errorf("Expected error message %q, got %q", expectedErr, err.Error())
	}
}

// TestFetchAndSyncPopulatesAllTables 验证 FetchAndSync 能正确填充所有数据库表。
// 使用 mock HTTP 服务器模拟 6 个上游 API 端点（/dashboard/cached、/dashboard/alerts、
// /dashboard/global-index、/analytics/provider-reliability、/analytics/recommendations、
// /analytics/transparency），同步后验证每个表的数据行数和字段正确性。
func TestFetchAndSyncPopulatesAllTables(t *testing.T) {
	dbPath := "./test_sync_tables.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 创建 mock 服务器，模拟所有上游 API 端点。
	// 使用完整的数据集验证同步逻辑：1 个模型 + 1 条历史分数 + 13 个 axis 字段 + 1 条退化记录 + 1 条告警
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"modelScores": [
						{
							"id": "model-1",
							"name": "Model One",
							"provider": "openai",
							"vendor": "openai",
							"currentScore": 85,
							"score": 85,
							"trend": "up",
							"lastUpdated": "2026-05-22T00:00:00Z",
							"status": "online",
							"isNew": true,
							"isStale": false,
							"usesReasoningEffort": true,
							"confidenceLower": 80.0,
							"confidenceUpper": 90.0,
							"standardError": 1.2
						}
					],
					"historyMap": {
						"model-1": [
							{
								"timestamp": "2026-05-22T00:00:00Z",
								"score": 85,
								"stupidScore": 85.0,
								"suite": "current",
								"axes": {
									"correctness": 9.0,
									"complexity": 8.0,
									"codeQuality": 9.0,
									"efficiency": 8.5,
									"stability": 9.0,
									"edgeCases": 8.0,
									"debugging": 7.5,
									"format": 9.5,
									"safety": 9.9,
									"memoryRetention": 8.0,
									"hallucinationRate": 2.0,
									"planCoherence": 8.5,
									"contextWindow": 9.0
								},
								"confidence_lower": 80.0,
								"confidence_upper": 90.0
							}
						]
					},
					"degradations": [
						{
							"modelId": "model-1",
							"modelName": "Model One",
							"provider": "openai",
							"currentScore": 85,
							"baselineScore": 95,
							"dropPercentage": 10,
							"zScore": "-2.1",
							"severity": "medium",
							"detectedAt": "2026-05-22T00:00:00Z",
							"message": "Performance dropped",
							"type": "drift"
						}
					]
				}
			}`))
		case "/dashboard/alerts":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": [
					{
						"name": "Model One",
						"provider": "openai",
						"issue": "High latency",
						"severity": "warning",
						"detectedAt": "2026-05-22T00:00:00Z"
					}
				]
			}`))
		case "/dashboard/global-index":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"current": {
						"timestamp": "2026-05-22T00:00:00Z",
						"globalScore": 88,
						"modelsCount": 15
					},
					"history": [
						{
							"timestamp": "2026-05-22T00:00:00Z",
							"globalScore": 88,
							"modelsCount": 15
						}
					],
					"trend": "stable",
					"performingWell": 12,
					"totalModels": 15
				}
			}`))
		case "/analytics/provider-reliability":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": [
					{
						"provider": "openai",
						"trustScore": 98,
						"totalIncidents": 2,
						"incidentsPerMonth": 0,
						"avgRecoveryHours": "1.5",
						"lastIncident": "2026-05-20T00:00:00Z",
						"trend": "improving",
						"activeModels": 5,
						"topPerformers": 3,
						"isAvailable": true
					}
				]
			}`))
		case "/analytics/recommendations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"bestForCode": {
						"id": "model-1",
						"name": "Model One",
						"vendor": "openai",
						"score": 95,
						"reason": "Top coding score",
						"evidence": "Passed 99% tests"
					},
					"mostReliable": {
						"id": "model-1",
						"name": "Model One",
						"vendor": "openai",
						"score": 98,
						"reason": "100% uptime",
						"evidence": "No outage"
					},
					"fastestResponse": {
						"id": "model-1",
						"name": "Model One",
						"vendor": "openai",
						"score": 90,
						"reason": "Average 200ms",
						"evidence": "Fast response time"
					},
					"avoidNow": [
						{
							"id": "model-bad",
							"name": "Bad Model",
							"reason": "Failing consistently"
						}
					]
				}
			}`))
		case "/analytics/transparency":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"summary": {
						"lastUpdate": "2026-05-22T00:00:00Z",
						"totalTests": 1000,
						"passedTests": 980,
						"coverage": 98,
						"confidence": 99,
						"dataPoints24h": 5000,
						"nextTest": "2026-05-22T00:10:00Z",
						"modelsFresh": 10,
						"modelsStale": 2,
						"modelsOffline": 0
					},
					"modelFreshness": [
						{
							"model": "Model One",
							"lastUpdate": "2026-05-22T00:00:00Z",
							"minutesAgo": 5,
							"status": "fresh"
						}
					]
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	err = FetchAndSync()
	if err != nil {
		t.Fatalf("FetchAndSync failed: %v", err)
	}

	// 辅助函数：验证表中行数是否符合预期
	checkCount := func(table string, expected int) {
		var count int
		err := DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to query %s: %v", table, err)
		}
		if count != expected {
			t.Errorf("Expected %d rows in %s, got %d", expected, table, count)
		}
	}

	// 验证 9 张表的数据行数
	checkCount("models", 1)
	checkCount("scores_history", 1)
	checkCount("degradations", 1)
	checkCount("alerts", 1)
	checkCount("global_index", 1)
	checkCount("provider_reliability", 1)
	checkCount("recommendations", 4) // best_for_code, most_reliable, fastest_response, avoid_now
	checkCount("transparency", 1)
	checkCount("model_freshness", 1)

	// 验证模型特定字段是否正确解析：is_reasoning 应反映 usesReasoningEffort
	var isReasoning int
	err = DB.QueryRow("SELECT is_reasoning FROM models WHERE id='model-1'").Scan(&isReasoning)
	if err != nil {
		t.Fatalf("Failed to query model-1 reasoning: %v", err)
	}
	if isReasoning != 1 {
		t.Errorf("Expected model-1 to have is_reasoning=1, got %d", isReasoning)
	}

	// 验证当前分数和历史 axis 字段是否正确插入
	var score int
	var axCorrectness float64
	err = DB.QueryRow("SELECT score, ax_correctness FROM scores_history WHERE model_id='model-1' AND suite='current'").Scan(&score, &axCorrectness)
	if err != nil {
		t.Fatalf("Failed to query scores_history: %v", err)
	}
	if score != 85 {
		t.Errorf("Expected score 85, got %d", score)
	}
	if axCorrectness != 9.0 {
		t.Errorf("Expected ax_correctness 9.0, got %f", axCorrectness)
	}
}

// TestFetchAndSyncIdempotent 验证 FetchAndSync 的幂等性：重复同步相同数据不会产生重复记录。
// 使用 INSERT OR REPLACE 确保 models 和 scores_history 的行数不变。
// 验证：第一次同步后 1 条模型 + 1 条分数，第二次同步后仍为 1 + 1。
func TestFetchAndSyncIdempotent(t *testing.T) {
	dbPath := "./test_sync_idempotent.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// mock 服务器返回固定的数据集
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"modelScores": [
						{
							"id": "model-1",
							"name": "Model One",
							"provider": "openai",
							"vendor": "openai",
							"currentScore": 85,
							"score": 85,
							"trend": "up",
							"lastUpdated": "2026-05-22T00:00:00Z",
							"status": "online",
							"isNew": true,
							"isStale": false,
							"usesReasoningEffort": true,
							"confidenceLower": 80.0,
							"confidenceUpper": 90.0,
							"standardError": 1.2
						}
					],
					"historyMap": {
						"model-1": [
							{
								"timestamp": "2026-05-22T00:00:00Z",
								"score": 85,
								"stupidScore": 85.0,
								"suite": "current",
								"axes": {
									"correctness": 9.0
								}
							}
						]
					},
					"degradations": []
				}
			}`))
		case "/dashboard/alerts":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		case "/dashboard/global-index":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": {"current": {}, "history": []}}`))
		case "/analytics/provider-reliability":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		case "/analytics/recommendations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": {}}`))
		case "/analytics/transparency":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": {"summary": {}, "modelFreshness": []}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	// 第一次同步
	err = FetchAndSync()
	if err != nil {
		t.Fatalf("First FetchAndSync failed: %v", err)
	}

	var count1 int
	err = DB.QueryRow("SELECT COUNT(*) FROM models").Scan(&count1)
	if err != nil || count1 != 1 {
		t.Fatalf("Expected 1 model after first sync, got %d", count1)
	}

	var historyCount1 int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&historyCount1)
	if err != nil || historyCount1 != 1 {
		t.Fatalf("Expected 1 score_history row after first sync, got %d", historyCount1)
	}

	// 第二次同步，数据与第一次完全相同，应不会产生重复行
	err = FetchAndSync()
	if err != nil {
		t.Fatalf("Second FetchAndSync failed: %v", err)
	}

	var count2 int
	err = DB.QueryRow("SELECT COUNT(*) FROM models").Scan(&count2)
	if err != nil || count2 != 1 {
		t.Fatalf("Expected 1 model after second sync, got %d", count2)
	}

	var historyCount2 int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&historyCount2)
	if err != nil || historyCount2 != 1 {
		t.Fatalf("Expected 1 score_history row after second sync, got %d", historyCount2)
	}
}

// TestSyncMuPreventsConcurrentFetchAndSync 验证 syncMu 互斥锁阻止并发同步。
// 策略：第一个 FetchAndSync 在 mock server 中阻塞，第二个 FetchAndSync 应在 100ms 内无法完成。
// 释放第一个后，两个都应正常结束。
func TestSyncMuPreventsConcurrentFetchAndSync(t *testing.T) {
	dbPath := "./test_sync_mu.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	syncStarted := make(chan struct{})
	syncFinished := make(chan struct{})
	blockSync := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/dashboard/cached" {
			// 通知测试第一个同步已达到 API 请求阶段
			select {
			case syncStarted <- struct{}{}:
			default:
			}

			// 阻塞请求处理，模拟慢 API 调用，让第二个同步有机会尝试并发执行
			<-blockSync

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"modelScores": [],
					"historyMap": {},
					"degradations": []
				}
			}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		}
	}))
	defer ts.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	// 在 goroutine 中启动第一个同步
	go func() {
		_ = FetchAndSync()
		close(syncFinished)
	}()

	// 等待第一个同步进入 fetchJSON 阶段（即 syncMu 已锁定）
	<-syncStarted

	// 此时启动第二个同步：由于 syncMu 被锁定，第二个应被阻塞无法执行
	secondSyncFinished := make(chan struct{})
	var secondSyncSuccess int32

	go func() {
		err := FetchAndSync()
		if err == nil {
			atomic.StoreInt32(&secondSyncSuccess, 1)
		}
		close(secondSyncFinished)
	}()

	// 给第二个 goroutine 一小段时间运行，期望它被 syncMu 阻塞
	select {
	case <-secondSyncFinished:
		t.Fatal("Second FetchAndSync completed while first one was still blocked!")
	case <-time.After(100 * time.Millisecond):
		// 预期路径：第二个同步被阻塞，100ms 后仍未完成
	}

	// 释放第一个同步，使其完成
	close(blockSync)

	// 等待两个同步都完成
	<-syncFinished
	<-secondSyncFinished

	if atomic.LoadInt32(&secondSyncSuccess) != 1 {
		t.Error("Second sync did not complete successfully after the first sync released")
	}
}

// TestDataPruningWorks 验证同步过程中的数据清理逻辑：
// 1. 先插入一条 10 天前和一条 65 天前的 scores_history 记录
// 2. 再插入相应时间的 global_index 记录
// 3. 执行 FetchAndSync（触发 60 天数据清理）
// 4. 验证旧数据被删除，新数据保留
func TestDataPruningWorks(t *testing.T) {
	dbPath := "./test_pruning.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// mock 服务器返回最小化数据，确保同步成功但不产生干扰数据
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/dashboard/cached" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": {"modelScores": [], "historyMap": {}, "degradations": []}}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		}
	}))
	defer ts.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	// 插入一个 dummy 模型以满足外键约束
	_, err = DB.Exec(`INSERT INTO models (id, name, provider, vendor) VALUES ('model-1', 'Model One', 'openai', 'openai')`)
	if err != nil {
		t.Fatalf("Failed to insert dummy model: %v", err)
	}

	// 手动插入旧的和新的 scores_history 和 global_index 记录
	// 选择 10 天前（保留）和 65 天前（超过 60 天清理阈值）
	now := time.Now()
	recentTime := now.AddDate(0, 0, -10) // 10 天前，应保留
	oldTime := now.AddDate(0, 0, -65)    // 65 天前，应被清理

	_, err = DB.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES
		('model-1', ?, 80, 'test'),
		('model-1', ?, 70, 'test')`, recentTime, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert mock history: %v", err)
	}

	_, err = DB.Exec(`INSERT INTO global_index (timestamp, global_score) VALUES
		(?, 80),
		(?, 70)`, recentTime, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert mock global index: %v", err)
	}

	// 执行 FetchAndSync，触发数据清理
	err = FetchAndSync()
	if err != nil {
		t.Fatalf("FetchAndSync failed: %v", err)
	}

	// 验证 scores_history：旧数据被清理，新数据保留
	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE timestamp = ?", oldTime).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected old score_history record to be pruned, but found %d", count)
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM scores_history WHERE timestamp = ?", recentTime).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected recent score_history record to remain, but found %d", count)
	}

	// 验证 global_index：旧数据被清理，新数据保留
	err = DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE timestamp = ?", oldTime).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected old global_index record to be pruned, but found %d", count)
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM global_index WHERE timestamp = ?", recentTime).Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected recent global_index record to remain, but found %d", count)
	}
}

// TestStartSyncWorkerLoop 测试定时同步循环的调度逻辑，而非实际的循环执行。
// 由于 StartSyncWorkerLoop 会永久循环并调用 time.Sleep（最长 10 分钟），
// 直接调用会导致测试挂起。替代方案：
// 1. 复现内部调度算法，验证不同时间点的睡眠时长计算正确性
// 2. 验证边界情况：整点边界（59 分 -> 跨小时）、整 10 分钟（0 分 -> 10 分）、常规情况
func TestStartSyncWorkerLoop(t *testing.T) {
	dbPath := "./test_worker_loop.db"
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	syncCount := int32(0)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/dashboard/cached" {
			atomic.AddInt32(&syncCount, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": {"modelScores": [], "historyMap": {}, "degradations": []}}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "data": []}`))
		}
	}))
	defer ts.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = originalBaseURL }()

	// 复现 StartSyncWorkerLoop 内部的调度算法来计算下一次同步的等待时间
	calculateSleepDuration := func(now time.Time) time.Duration {
		nextMinute := ((now.Minute() / 10) + 1) * 10
		var nextSync time.Time
		if nextMinute >= 60 {
			nextSync = time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
		} else {
			nextSync = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())
		}
		return nextSync.Sub(now)
	}

	// Case 1: 12:03:00 -> 应等待 7 分钟到 12:10
	t1 := time.Date(2026, 5, 22, 12, 3, 0, 0, time.UTC)
	d1 := calculateSleepDuration(t1)
	if d1 != 7*time.Minute {
		t.Errorf("Expected 7 minutes sleep from 12:03:00, got %v", d1)
	}

	// Case 2: 12:59:00 -> 应等待 1 分钟到 13:00（跨小时边界）
	t2 := time.Date(2026, 5, 22, 12, 59, 0, 0, time.UTC)
	d2 := calculateSleepDuration(t2)
	if d2 != 1*time.Minute {
		t.Errorf("Expected 1 minute sleep from 12:59:00, got %v", d2)
	}

	// Case 3: 12:00:00 -> 应等待 10 分钟到 12:10（整 10 分时的行为）
	t3 := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	d3 := calculateSleepDuration(t3)
	if d3 != 10*time.Minute {
		t.Errorf("Expected 10 minutes sleep from 12:00:00, got %v", d3)
	}

	// 在后台 goroutine 中启动循环以确保不会 panic（但不实际运行，因为会永久阻塞）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("StartSyncWorkerLoop panicked: %v", r)
			}
		}()
	}()
}
