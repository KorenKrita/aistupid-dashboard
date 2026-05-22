package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ------------------------------
// 并发测试组：覆盖整个应用的并发安全边界
// ------------------------------

// TestConcurrencyConfigMutex 测试 configMu 在并发读写下的安全性。
// 使用 20 个 worker（每个包含 1 reader + 1 writer goroutine），各执行 50 次操作。
// 验证逻辑：reader 反复调用 getBlockedModels，writer 反复调用 setBlockedModels，
// 确保在 configMu 保护下不会出现 data race。
func TestConcurrencyConfigMutex(t *testing.T) {
	const configPath = "config.json"
	var backup []byte
	backupExists := false
	if _, err := os.Stat(configPath); err == nil {
		var readErr error
		backup, readErr = os.ReadFile(configPath)
		if readErr == nil {
			backupExists = true
			_ = os.Remove(configPath)
		}
	}
	defer func() {
		_ = os.Remove(configPath)
		if backupExists {
			_ = os.WriteFile(configPath, backup, 0644)
		}
	}()

	var wg sync.WaitGroup
	numWorkers := 20
	iterations := 50

	// 并发读写操作：每个 worker 配对 1 reader + 1 writer goroutine
	for i := 0; i < numWorkers; i++ {
		wg.Add(2)
		// Reader goroutine：反复读取 BlockedModels 配置
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = getBlockedModels()
			}
		}()
		// Writer goroutine：反复写入不同的 BlockedModels 值
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				setBlockedModels([]string{fmt.Sprintf("model-%d-%d", workerID, j)})
			}
		}(i)
	}
	wg.Wait()
	// 等待异步 saveConfig goroutine 完成
	time.Sleep(100 * time.Millisecond)
}

// TestConcurrencySyncMutex 测试 syncMu 阻止并发 FetchAndSync 执行。
// 策略：启动 5 个 goroutine 同时调用 FetchAndSync，通过 mock server 统计
// 最大并发 HTTP 请求数，验证 syncMu 确保同一时间最多只有 1 个活跃请求。
func TestConcurrencySyncMutex(t *testing.T) {
	dbPath := "./test_sync_concurrency.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	origURL := apiBaseURL
	defer func() { apiBaseURL = origURL }()

	var activeRequests int32
	var maxActiveRequests int32

	// mock 服务器统计并发请求数，每个请求处理时插入 15ms 延迟以暴露竞态
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentActive := atomic.AddInt32(&activeRequests, 1)
		defer atomic.AddInt32(&activeRequests, -1)

		// 使用 CAS 无锁更新最大并发数
		for {
			maxVal := atomic.LoadInt32(&maxActiveRequests)
			if currentActive > maxVal {
				if atomic.CompareAndSwapInt32(&maxActiveRequests, maxVal, currentActive) {
					break
				}
			} else {
				break
			}
		}

		// 插入延迟，让并发请求有机会重叠（如果互斥锁不起作用的话）
		time.Sleep(15 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			w.Write([]byte(`{
				"success": true,
				"data": {
					"modelScores": [
						{"id": "m-sync-1", "name": "Model Sync 1", "provider": "p1", "vendor": "v1", "currentScore": 90, "score": 90, "trend": "stable", "lastUpdated": "2026-05-22T00:00:00Z", "status": "active", "isNew": false, "isStale": false, "usesReasoningEffort": false}
					],
					"historyMap": {
						"m-sync-1": [{"timestamp": "2026-05-22T00:00:00Z", "score": 90, "stupidScore": 90.0, "suite": "regular"}]
					},
					"degradations": []
				}
			}`))
		case "/dashboard/alerts":
			w.Write([]byte(`{"success": true, "data": []}`))
		case "/dashboard/global-index":
			w.Write([]byte(`{"success": true, "data": {"current": {"timestamp": "2026-05-22T00:00:00Z", "globalScore": 90, "modelsCount": 1}, "history": []}}`))
		case "/analytics/provider-reliability":
			w.Write([]byte(`{"success": true, "data": []}`))
		case "/analytics/recommendations":
			w.Write([]byte(`{"success": true, "data": {}}`))
		case "/analytics/transparency":
			w.Write([]byte(`{"success": true, "data": {"summary": {}, "modelFreshness": []}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	apiBaseURL = ts.URL

	var wg sync.WaitGroup
	numGoroutines := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := FetchAndSync()
			if err != nil {
				t.Errorf("FetchAndSync failed: %v", err)
			}
		}()
	}

	wg.Wait()

	maxConns := atomic.LoadInt32(&maxActiveRequests)
	// 验证：syncMu 应确保同一时间最多 1 个 HTTP 请求（即并发 FetchAndSync 被序列化）
	if maxConns > 1 {
		t.Errorf("Expected max 1 active HTTP request at a time, but got %d (indicating concurrent FetchAndSync executions)", maxConns)
	}
}

// TestConcurrencyLastSyncTimeMutex 测试 lastSyncTime 在 RWMutex 保护下的并发读写安全。
// 使用 20 个 reader + 20 个 writer goroutine，各执行 100 次操作。
// reader 调用 getLastSyncTime，writer 调用 setLastSyncTime，验证无 data race。
func TestConcurrencyLastSyncTimeMutex(t *testing.T) {
	var wg sync.WaitGroup
	numWorkers := 20
	iterations := 100

	wg.Add(numWorkers * 2)

	// 并发 reader：反复读取最后同步时间
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = getLastSyncTime()
			}
		}()
	}

	// 并发 writer：反复更新最后同步时间
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				setLastSyncTime(time.Now())
			}
		}()
	}

	wg.Wait()
}

// TestConcurrencyDatabaseAccess 验证 SQLite busy_timeout 机制在并发数据库访问时的行为。
// 场景：
// 1. 启动事务锁定数据库（通过主连接 DB，SetMaxOpenConns=1）
// 2. 使用 busy_timeout(0) 的连接尝试写入 -> 应立即失败（database is locked）
// 3. 使用 busy_timeout(5000) 的连接尝试写入 -> 应等待后成功（等待 100ms 后事务提交）
// 该测试验证即使 SQLite 被锁，适当的 busy_timeout 配置能让请求等待而非立即失败。
func TestConcurrencyDatabaseAccess(t *testing.T) {
	dbPath := "./test_db_concurrency.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 插入基础模型以满足外键约束
	_, err = DB.Exec(`INSERT INTO models (id, name, provider, vendor) VALUES ('m1', 'Model 1', 'p1', 'v1')`)
	if err != nil {
		t.Fatalf("Failed to insert initial model: %v", err)
	}

	// 打开第二个连接（busy_timeout=5000，会等待锁释放）
	db2, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("Failed to open second db connection: %v", err)
	}
	defer db2.Close()

	// 打开第三个连接（busy_timeout=0，遇到锁立即失败）
	db3, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(0)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("Failed to open third db connection: %v", err)
	}
	defer db3.Close()

	// 在主连接上启动事务（SetMaxOpenConns=1 但事务持有写锁）
	tx, err := DB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// 通过事务写入，锁定数据库
	_, err = tx.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 80, 'test')`, time.Now())
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	// db3（busy_timeout=0）尝试写入，预期立即失败
	_, err = db3.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 85, 'test')`, time.Now().Add(time.Second))
	if err == nil {
		t.Error("Expected write on db3 (busy_timeout=0) to fail due to active transaction lock, but it succeeded")
	} else if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
		t.Errorf("Expected database locked/busy error on db3, got: %v", err)
	}

	// 100ms 后在 goroutine 中提交事务，模拟慢事务
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = tx.Commit()
	}()

	// db2（busy_timeout=5000）尝试写入，应等待锁释放后成功
	startTime := time.Now()
	_, err = db2.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 90, 'test')`, time.Now().Add(2*time.Second))
	if err != nil {
		t.Errorf("Expected write on db2 to succeed after waiting, but failed: %v", err)
	}
	duration := time.Since(startTime)
	// 验证 db2 确实等待了（至少 100ms 直到事务提交）
	if duration < 100*time.Millisecond {
		t.Errorf("Expected db2 query to block for at least 100ms, but it took %v", duration)
	}
}

// TestConcurrencyDBHandlersAndSync 测试 HTTP handler 读 DB 与 FetchAndSync 写 DB 并发执行的稳定性。
// 场景：
// - 15 个 goroutine 模拟 HTTP handler 反复查询 models 和 scores_history
// - 3 个 goroutine 模拟同步器反复调用 FetchAndSync
// - 验证在并发读写下不会 panic 或死锁
func TestConcurrencyDBHandlersAndSync(t *testing.T) {
	dbPath := "./test_db_handlers_sync.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 插入基础数据
	_, err = DB.Exec(`INSERT INTO models (id, name, provider, vendor) VALUES ('m1', 'Model 1', 'p1', 'v1')`)
	if err != nil {
		t.Fatalf("Failed to insert initial model: %v", err)
	}

	origURL := apiBaseURL
	defer func() { apiBaseURL = origURL }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/cached":
			w.Write([]byte(`{
				"success": true,
				"data": {
					"modelScores": [
						{"id": "m1", "name": "Model 1", "provider": "p1", "vendor": "v1", "currentScore": 85, "score": 85, "trend": "stable", "lastUpdated": "2026-05-22T00:00:00Z", "status": "active", "isNew": false, "isStale": false, "usesReasoningEffort": false}
					],
					"historyMap": {
						"m1": [{"timestamp": "2026-05-22T00:00:00Z", "score": 85, "stupidScore": 85.0, "suite": "regular"}]
					},
					"degradations": []
				}
			}`))
		case "/dashboard/alerts":
			w.Write([]byte(`{"success": true, "data": []}`))
		case "/dashboard/global-index":
			w.Write([]byte(`{"success": true, "data": {"current": {"timestamp": "2026-05-22T00:00:00Z", "globalScore": 85, "modelsCount": 1}, "history": []}}`))
		case "/analytics/provider-reliability":
			w.Write([]byte(`{"success": true, "data": []}`))
		case "/analytics/recommendations":
			w.Write([]byte(`{"success": true, "data": {}}`))
		case "/analytics/transparency":
			w.Write([]byte(`{"success": true, "data": {"summary": {}, "modelFreshness": []}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	apiBaseURL = ts.URL

	var wg sync.WaitGroup
	numWorkers := 15
	iterations := 20

	// 并发 DB reader：模拟 HTTP handler 在同步同时查询数据库
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				var count int
				_ = DB.QueryRow("SELECT COUNT(*) FROM models").Scan(&count)
				_ = DB.QueryRow("SELECT COUNT(*) FROM scores_history").Scan(&count)
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// 并发 DB writer：模拟同步器在查询同时写入数据库
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = FetchAndSync()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrencyHTTPHandlers 测试 HTTP handler 在并发请求下的稳定性。
// 子测试：
// 1. "Same Handler Concurrently"：50 个并发请求访问同一个 /api/models handler
// 2. "Different Handlers Concurrently"：30 个并发请求轮询访问 10 个不同的 handler
// 验证所有请求都能返回 200 且无 panic。
func TestConcurrencyHTTPHandlers(t *testing.T) {
	dbPath := "./test_http_concurrency.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	setupTestData(t)

	// 子测试：50 个并发请求打到同一个 handler（handleModels）
	t.Run("Same Handler Concurrently", func(t *testing.T) {
		var wg sync.WaitGroup
		numRequests := 50
		wg.Add(numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/api/models", nil)
				w := httptest.NewRecorder()
				handleModels(w, req)
				if w.Code != http.StatusOK {
					t.Errorf("Expected 200, got %d", w.Code)
				}
			}()
		}
		wg.Wait()
	})

	// 子测试：30 个并发请求轮询访问 10 个不同的 handler
	t.Run("Different Handlers Concurrently", func(t *testing.T) {
		handlers := []struct {
			path string
			fn   http.HandlerFunc
		}{
			{"/api/config", handleConfig},
			{"/api/models", handleModels},
			{"/api/scores", handleScores},
			{"/api/degradations", handleDegradations},
			{"/api/alerts", handleAlerts},
			{"/api/global-index", handleGlobalIndex},
			{"/api/provider-reliability", handleProviderReliability},
			{"/api/recommendations", handleRecommendations},
			{"/api/transparency", handleTransparency},
			{"/api/sync-status", handleSyncStatus},
		}

		var wg sync.WaitGroup
		numWorkers := 30
		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer wg.Done()
				h := handlers[workerID%len(handlers)]
				req := httptest.NewRequest("GET", h.path, nil)
				w := httptest.NewRecorder()
				h.fn(w, req)
				if w.Code != http.StatusOK {
					t.Errorf("Handler %s failed: expected 200, got %d", h.path, w.Code)
				}
			}(i)
		}
		wg.Wait()
	})
}
