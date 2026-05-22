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

// TestArea 1: Config mutex concurrency tests
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

	// Run concurrent read and write operations
	for i := 0; i < numWorkers; i++ {
		wg.Add(2)
		// Reader goroutine
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = getBlockedModels()
			}
		}()
		// Writer goroutine
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				setBlockedModels([]string{fmt.Sprintf("model-%d-%d", workerID, j)})
			}
		}(i)
	}
	wg.Wait()
	// Wait a moment for any remaining async saveConfig goroutines to finish
	time.Sleep(100 * time.Millisecond)
}

// TestArea 2: Sync mutex concurrency tests
func TestConcurrencySyncMutex(t *testing.T) {
	dbPath := "./test_sync_concurrency.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Backup original apiBaseURL and restore it afterwards
	origURL := apiBaseURL
	defer func() { apiBaseURL = origURL }()

	var activeRequests int32
	var maxActiveRequests int32

	// Setup mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentActive := atomic.AddInt32(&activeRequests, 1)
		defer atomic.AddInt32(&activeRequests, -1)

		// Update max observed concurrent requests
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

		// Insert a delay to allow overlaps if mutex locking is not working
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
	if maxConns > 1 {
		t.Errorf("Expected max 1 active HTTP request at a time, but got %d (indicating concurrent FetchAndSync executions)", maxConns)
	}
}

// TestArea 3: lastSyncTime mutex concurrency tests
func TestConcurrencyLastSyncTimeMutex(t *testing.T) {
	var wg sync.WaitGroup
	numWorkers := 20
	iterations := 100

	wg.Add(numWorkers * 2)

	// Concurrent Readers
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = getLastSyncTime()
			}
		}()
	}

	// Concurrent Writers
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

// TestArea 4: Database access concurrency tests (busy_timeout verification)
func TestConcurrencyDatabaseAccess(t *testing.T) {
	dbPath := "./test_db_concurrency.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Insert a base model so foreign key constraints are satisfied
	_, err = DB.Exec(`INSERT INTO models (id, name, provider, vendor) VALUES ('m1', 'Model 1', 'p1', 'v1')`)
	if err != nil {
		t.Fatalf("Failed to insert initial model: %v", err)
	}

	// Open a second connection with busy_timeout(5000)
	db2, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("Failed to open second db connection: %v", err)
	}
	defer db2.Close()

	// Open a third connection with busy_timeout(0) to verify lock failure
	db3, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(0)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("Failed to open third db connection: %v", err)
	}
	defer db3.Close()

	// Start a transaction on DB (busy_timeout(5000) but has max open connections = 1)
	tx, err := DB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Lock the database by inserting into scores_history
	_, err = tx.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 80, 'test')`, time.Now())
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	// Attempt write on db3 (busy_timeout(0)) while transaction is active -> should fail immediately
	_, err = db3.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 85, 'test')`, time.Now().Add(time.Second))
	if err == nil {
		t.Error("Expected write on db3 (busy_timeout=0) to fail due to active transaction lock, but it succeeded")
	} else if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
		t.Errorf("Expected database locked/busy error on db3, got: %v", err)
	}

	// Commit transaction after 100ms in a goroutine
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = tx.Commit()
	}()

	// Attempt write on db2 (busy_timeout(5000)) -> should wait and then succeed after commit
	startTime := time.Now()
	_, err = db2.Exec(`INSERT INTO scores_history (model_id, timestamp, score, suite) VALUES ('m1', ?, 90, 'test')`, time.Now().Add(2*time.Second))
	if err != nil {
		t.Errorf("Expected write on db2 to succeed after waiting, but failed: %v", err)
	}
	duration := time.Since(startTime)
	if duration < 100*time.Millisecond {
		t.Errorf("Expected db2 query to block for at least 100ms, but it took %v", duration)
	}
}

// TestArea 4 (continued): Multiple handlers querying DB while sync runs concurrently
func TestConcurrencyDBHandlersAndSync(t *testing.T) {
	dbPath := "./test_db_handlers_sync.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)
	defer CloseDB()

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Insert base data
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

	// Concurrent DB readers (handlers)
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

	// Concurrent DB writers (sync)
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

// TestArea 5: HTTP handlers concurrency tests
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

	// Test concurrent requests to the SAME handler
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

	// Test concurrent requests to DIFFERENT handlers
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
