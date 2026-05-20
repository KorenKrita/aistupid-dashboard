# AIStupid Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a single-binary self-hosted dashboard that monitors AI performance drift by syncing from aistupidlevel.info API, persisting it to SQLite, and rendering a React UI with ECharts that supports custom theme switching (Light/Dark/System).

**Architecture:** A Go backend serves API endpoints for historical data and admin configuration. It uses a background ticker to pull API data and stores it in SQLite. A Vite + React + Tailwind + ECharts frontend is embedded directly into the Go binary using `go:embed` for zero-dependency single-file deployment.

**Tech Stack:** Go 1.21+, SQLite (mattn/go-sqlite3 or modernc.org/sqlite), React 18, TypeScript, Tailwind CSS, Vite, ECharts.

---

## File Structure Map
- `/main.go` - Entry point, routes, embeds dist
- `/db.go` - Database initialization and operations
- `/sync.go` - Upstream API client and background sync worker
- `/frontend/` - Vite React Frontend root
- `/frontend/src/main.tsx` - Frontend entry and theme bootstrap
- `/frontend/src/App.tsx` - Main Dashboard UI with chart and admin panel

---

## Task 1: Go Project & Database Setup

**Files:**
- Create: `go.mod`
- Create: `db.go`

- [ ] **Step 1: Initialize Go modules and install SQLite driver**

```bash
go mod init aistupid-dashboard
go get github.com/mattn/go-sqlite3
```

- [ ] **Step 2: Implement db.go with Schema initialization**

Write database initialization. Check if tables exist and create them if missing.

```go
package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite3", filepath)
	if err != nil {
		return err
	}

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			provider TEXT NOT NULL,
			vendor TEXT NOT NULL,
			is_reasoning INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS scores_history (
			model_id TEXT NOT NULL,
			score INTEGER NOT NULL,
			trend TEXT NOT NULL,
			confidence_lower REAL,
			confidence_upper REAL,
			timestamp DATETIME NOT NULL,
			PRIMARY KEY (model_id, timestamp),
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS degradations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			drop_percentage INTEGER NOT NULL,
			severity TEXT NOT NULL,
			detected_at DATETIME NOT NULL,
			message TEXT,
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,
	}

	for _, schema := range schemas {
		_, err = DB.Exec(schema)
		if err != nil {
			return err
		}
	}

	// Insert default settings
	defaults := map[string]string{
		"sync_interval_hours":    "4",
		"history_retention_days": "90",
		"tracked_models":         `["all"]`,
	}
	for k, v := range defaults {
		_, _ = DB.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)", k, v)
	}

	return nil
}
```

- [ ] **Step 3: Add database unit test in `db_test.go`**

```go
package main

import (
	"os"
	"testing"
)

func TestInitDB(t *testing.T) {
	dbPath := "./test_aistupid.db"
	defer os.Remove(dbPath)

	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	var name string
	err = DB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&name)
	if err != nil || name != "users" {
		t.Errorf("Expected 'users' table to exist, got %s (err: %v)", name, err)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -v ./...
```
Expected output: PASS

- [ ] **Step 5: Commit changes**

```bash
git add go.mod go.sum db.go db_test.go
git commit -m "chore: init database with schemas and setup tests"
```

---

## Task 2: API Integration & Data Sync Sync.go

**Files:**
- Create: `sync.go`

- [ ] **Step 1: Create sync client and worker**

Implement `FetchData` to query `https://aistupidlevel.info/dashboard/cached` and parse metrics. Update `models`, `scores_history`, and `degradations`.

```go
package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type UpstreamResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ModelScores []struct {
			ID                  string  `json:"id"`
			Name                string  `json:"name"`
			Provider            string  `json:"provider"`
			Vendor              string  `json:"vendor"`
			CurrentScore        int     `json:"currentScore"`
			Trend               string  `json:"trend"`
			ConfidenceLower     float64 `json:"confidenceLower"`
			ConfidenceUpper     float64 `json:"confidenceUpper"`
			UsesReasoningEffort bool    `json:"usesReasoningEffort"`
		} `json:"modelScores"`
		Degradations []struct {
			ModelID        interface{} `json:"modelId"` // can be int or string
			DropPercentage int         `json:"dropPercentage"`
			Severity       string      `json:"severity"`
			DetectedAt     string      `json:"detectedAt"`
			Message        string      `json:"message"`
		} `json:"degradations"`
	} `json:"data"`
}

func FetchAndSync() error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", "https://aistupidlevel.info/dashboard/cached", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aistupid-dashboard-selfhosted/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiRes UpstreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiRes); err != nil {
		return err
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()

	for _, m := range apiRes.Data.ModelScores {
		isReasoning := 0
		if m.UsesReasoningEffort {
			isReasoning = 1
		}
		_, err = tx.Exec(`INSERT INTO models (id, name, provider, vendor, is_reasoning) 
			VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET 
			name=excluded.name, provider=excluded.provider, vendor=excluded.vendor, is_reasoning=excluded.is_reasoning`,
			m.ID, m.Name, m.Provider, m.Vendor, isReasoning)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`INSERT OR IGNORE INTO scores_history 
			(model_id, score, trend, confidence_lower, confidence_upper, timestamp) 
			VALUES (?, ?, ?, ?, ?, ?)`,
			m.ID, m.CurrentScore, m.Trend, m.ConfidenceLower, m.ConfidenceUpper, now)
		if err != nil {
			return err
		}
	}

	// Clear active degradations and reload
	_, _ = tx.Exec("DELETE FROM degradations")
	for _, d := range apiRes.Data.Degradations {
		var modelIDStr string
		switch v := d.ModelID.(type) {
		case string:
			modelIDStr = v
		case float64:
			modelIDStr = string(int(v))
		}

		parsedTime, err := time.Parse(time.RFC3339, d.DetectedAt)
		if err != nil {
			parsedTime = now
		}

		_, err = tx.Exec(`INSERT INTO degradations (model_id, drop_percentage, severity, detected_at, message)
			VALUES (?, ?, ?, ?, ?)`, modelIDStr, d.DropPercentage, d.Severity, parsedTime, d.Message)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func StartSyncWorker() {
	go func() {
		for {
			var intervalHours int = 4
			_ = DB.QueryRow("SELECT value FROM settings WHERE key='sync_interval_hours'").Scan(&intervalHours)
			if intervalHours <= 0 {
				intervalHours = 4
			}

			_ = FetchAndSync()
			
			// Prune historical data according to settings
			var retentionDays int = 90
			_ = DB.QueryRow("SELECT value FROM settings WHERE key='history_retention_days'").Scan(&retentionDays)
			if retentionDays > 0 {
				cutoff := time.Now().AddDate(0, 0, -retentionDays)
				_, _ = DB.Exec("DELETE FROM scores_history WHERE timestamp < ?", cutoff)
			}

			time.Sleep(time.Duration(intervalHours) * time.Hour)
		}
	}()
}
```

- [ ] **Step 2: Commit sync file**

```bash
git add sync.go
git commit -m "feat: implement api fetching, SQLite persistence, and worker ticker"
```

---

## Task 3: Backend REST API endpoints

**Files:**
- Create: `main.go`

- [ ] **Step 1: Implement Server and HTTP routing**

Handle REST endpoints: setup register, login, config, dashboard statistics, history.

```go
package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var count int
	_ = DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count > 0 {
		http.Error(w, "First user already registered", http.StatusBadRequest)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		http.Error(w, "Invalid inputs", http.StatusBadRequest)
		return
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	_, err := DB.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", req.Username, string(hash))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"success":true}`))
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid inputs", http.StatusBadRequest)
		return
	}

	var hash string
	err := DB.QueryRow("SELECT password_hash FROM users WHERE username = ?", req.Username).Scan(&hash)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// In a simple setup, set a session cookie (in prod, use JWT/Secure Cookies)
	cookie := &http.Cookie{
		Name:     "session_token",
		Value:    req.Username,
		Expires:  time.Now().Add(24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
	w.Write([]byte(`{"success":true}`))
}

func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	var count int
	_ = DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"initialized":   count > 0,
		"authenticated": checkAuth(r),
	})
}

func checkAuth(r *http.Request) bool {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return false
	}
	return cookie.Value != ""
}

func handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == "GET" {
		rows, err := DB.Query("SELECT key, value FROM settings")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		res := make(map[string]string)
		for rows.Next() {
			var k, v string
			_ = rows.Scan(&k, &v)
			res[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
		return
	}

	if r.Method == "POST" {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid parameters", http.StatusBadRequest)
			return
		}
		for k, v := range payload {
			_, _ = DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", k, v)
		}
		w.Write([]byte(`{"success":true}`))
	}
}

func handleCurrent(w http.ResponseWriter, r *http.Request) {
	// Query current model scores and degradations
	// Get latest timestamp in history
	var latest string
	_ = DB.QueryRow("SELECT MAX(timestamp) FROM scores_history").Scan(&latest)

	rows, _ := DB.Query(`
		select m.id, m.name, m.provider, m.vendor, h.score, h.trend, h.confidence_lower, h.confidence_upper, m.is_reasoning 
		from models m 
		join scores_history h on m.id = h.model_id 
		where h.timestamp = ?`, latest)
	defer rows.Close()

	type ModelScoreRes struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Provider        string  `json:"provider"`
		Vendor          string  `json:"vendor"`
		Score           int     `json:"score"`
		Trend           string  `json:"trend"`
		ConfidenceLower float64 `json:"confidenceLower"`
		ConfidenceUpper float64 `json:"confidenceUpper"`
		IsReasoning     bool    `json:"isReasoning"`
	}

	var scores []ModelScoreRes
	for rows.Next() {
		var s ModelScoreRes
		var reasoning int
		_ = rows.Scan(&s.ID, &s.Name, &s.Provider, &s.Vendor, &s.Score, &s.Trend, &s.ConfidenceLower, &s.ConfidenceUpper, &reasoning)
		s.IsReasoning = reasoning == 1
		scores = append(scores, s)
	}

	degRows, _ := DB.Query(`
		select m.name, m.provider, d.drop_percentage, d.severity, d.detected_at, d.message 
		from degradations d 
		join models m on d.model_id = m.id`)
	defer degRows.Close()

	type DegRes struct {
		ModelName      string    `json:"modelName"`
		Provider       string    `json:"provider"`
		DropPercentage int       `json:"dropPercentage"`
		Severity       string    `json:"severity"`
		DetectedAt     time.Time `json:"detectedAt"`
		Message        string    `json:"message"`
	}
	var degs []DegRes
	for degRows.Next() {
		var d DegRes
		_ = degRows.Scan(&d.ModelName, &d.Provider, &d.DropPercentage, &d.Severity, &d.DetectedAt, &d.Message)
		degs = append(degs, d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scores":       scores,
		"degradations": degs,
	})
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		days = 30
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := DB.Query(`
		select model_id, score, timestamp 
		from scores_history 
		where timestamp >= ? 
		order by timestamp asc`, cutoff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type HistPoint struct {
		Score     int       `json:"score"`
		Timestamp time.Time `json:"timestamp"`
	}

	history := make(map[string][]HistPoint)
	for rows.Next() {
		var mid string
		var pt HistPoint
		_ = rows.Scan(&mid, &pt.Score, &pt.Timestamp)
		history[mid] = append(history[mid], pt)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func handleManualSync(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	err := FetchAndSync()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"success":true}`))
}

func main() {
	_ = InitDB("./aistupid.db")
	StartSyncWorker()

	http.HandleFunc("/api/auth/setup", handleSetup)
	http.HandleFunc("/api/auth/login", handleLogin)
	http.HandleFunc("/api/auth/status", handleAuthStatus)
	http.HandleFunc("/api/admin/settings", handleAdminSettings)
	http.HandleFunc("/api/admin/sync-now", handleManualSync)
	http.HandleFunc("/api/dashboard/current", handleCurrent)
	http.HandleFunc("/api/dashboard/history", handleHistory)

	// Local port listener
	_ = http.ListenAndServe("127.0.0.1:3223", nil)
}
```

- [ ] **Step 2: Commit REST API**

```bash
go get golang.org/x/crypto/bcrypt
git add main.go
git commit -m "feat: complete REST API endpoints for setup, login, current, history and manual sync"
```

---

## Task 4: Frontend Development (React + Tailwind + ECharts)

**Files:**
- Create: `frontend/` Structure
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/main.tsx`

- [ ] **Step 1: Scaffold React app with Vite**

```bash
npm create vite@latest frontend -- --template react-ts
cd frontend
npm install
npm install -D tailwindcss postcss autoprefixer
npx tailwindcss init -p
npm install lucide-react echarts echarts-for-react
```

- [ ] **Step 1b: Configure Vite Proxy in `vite.config.ts`**

Update configuration to proxy API requests to Go backend.

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:3223',
        changeOrigin: true,
      }
    }
  }
})
```

- [ ] **Step 2: Configure Tailwind `tailwind.config.js`**

Include dark mode class toggle configuration.

```javascript
/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        border: 'var(--border-color)',
        bgApp: 'var(--bg-app)',
        bgSurface: 'var(--bg-surface)',
        textMain: 'var(--text-main)',
        textMuted: 'var(--text-muted)',
        primary: 'var(--primary)',
        success: 'var(--success)',
        warning: 'var(--warning)',
        critical: 'var(--critical)',
      }
    },
  },
  plugins: [],
}
```

- [ ] **Step 3: Add CSS Custom variables in `frontend/src/index.css`**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@import url('https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500;600;700&family=Fira+Sans:wght@300;400;500;600;700&display=swap');

:root {
  --bg-app: #F8FAFC;
  --bg-surface: #FFFFFF;
  --text-main: #0F172A;
  --text-muted: #64748B;
  --primary: #0EA5E9;
  --success: #10B981;
  --warning: #F59E0B;
  --critical: #EF4444;
  --border-color: #E2E8F0;
  font-family: 'Fira Sans', sans-serif;
}

.dark {
  --bg-app: #020617;
  --bg-surface: #0B1329;
  --text-main: #F8FAFC;
  --text-muted: #94A3B8;
  --primary: #38BDF8;
  --success: #34D399;
  --warning: #FBBF24;
  --critical: #F87171;
  --border-color: #1E293B;
}

body {
  background-color: var(--bg-app);
  color: var(--text-main);
}
```

- [ ] **Step 4: Bootstrap Theme in `frontend/src/main.tsx`**

Inject the initial script to handle local theme preference.

```typescript
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import './index.css'

// Initial theme setup to avoid flashing
const saved = localStorage.getItem('theme');
const systemDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
if (saved === 'dark' || (!saved && systemDark)) {
  document.documentElement.classList.add('dark');
} else {
  document.documentElement.classList.add('light');
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
```

- [ ] **Step 5: Main Dashboard Component `frontend/src/App.tsx`**

Write the entire interface containing header, theme switch, score ranking, ECharts line chart, and settings panel.

```typescript
import { useEffect, useState, useRef } from 'react';
import ReactECharts from 'echarts-for-react';
import { Sun, Moon, Settings, RefreshCw, LogIn, Monitor } from 'lucide-react';

interface ModelScore {
  id: string;
  name: string;
  provider: string;
  vendor: string;
  score: number;
  trend: string;
  confidenceLower: number;
  confidenceUpper: number;
  isReasoning: boolean;
}

interface Degradation {
  modelName: string;
  provider: string;
  dropPercentage: number;
  severity: string;
  detectedAt: string;
  message: string;
}

export default function App() {
  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    return document.documentElement.classList.contains('dark') ? 'dark' : 'light';
  });
  const [scores, setScores] = useState<ModelScore[]>([]);
  const [degradations, setDegradations] = useState<Degradation[]>([]);
  const [history, setHistory] = useState<Record<string, { score: number, timestamp: string }[]>>({});
  
  // Settings & Login states
  const [isConfigOpen, setIsConfigOpen] = useState(false);
  const [isInitialized, setIsInitialized] = useState(true);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [isLoggedIn, setIsLoggedIn] = useState(false);

  useEffect(() => {
    fetchAuthStatus();
    fetchCurrent();
    fetchHistory();
  }, []);

  const fetchAuthStatus = async () => {
    try {
      const res = await fetch('/api/auth/status');
      const data = await res.json();
      setIsInitialized(data.initialized);
      setIsLoggedIn(data.authenticated);
    } catch (e) { console.error(e); }
  };

  const handleSetup = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      });
      if (res.ok) {
        setIsInitialized(true);
        setIsLoggedIn(true);
      }
    } catch (e) { alert('Setup failed'); }
  };

  const toggleTheme = () => {
    const nextTheme = theme === 'light' ? 'dark' : 'light';
    setTheme(nextTheme);
    if (nextTheme === 'dark') {
      document.documentElement.classList.add('dark');
      document.documentElement.classList.remove('light');
      localStorage.setItem('theme', 'dark');
    } else {
      document.documentElement.classList.add('light');
      document.documentElement.classList.remove('dark');
      localStorage.setItem('theme', 'light');
    }
  };

  const fetchCurrent = async () => {
    try {
      const res = await fetch('/api/dashboard/current');
      const data = await res.json();
      setScores(data.scores || []);
      setDegradations(data.degradations || []);
    } catch (e) { console.error(e); }
  };

  const fetchHistory = async () => {
    try {
      const res = await fetch('/api/dashboard/history?days=30');
      const data = await res.json();
      setHistory(data || {});
    } catch (e) { console.error(e); }
  };

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      });
      if (res.ok) {
        setIsLoggedIn(true);
      }
    } catch (e) { alert('Login failed'); }
  };

  const triggerSync = async () => {
    try {
      await fetch('/api/admin/sync-now', { method: 'POST' });
      fetchCurrent();
      fetchHistory();
    } catch (e) { alert('Sync failed'); }
  };

  // ECharts Configuration Options
  const getChartOptions = () => {
    const series = Object.keys(history).map(modelId => {
      const modelData = history[modelId] || [];
      const modelMeta = scores.find(s => s.id === modelId);
      return {
        name: modelMeta ? modelMeta.name : modelId,
        type: 'line',
        showSymbol: false,
        data: modelData.map(pt => [new Date(pt.timestamp), pt.score])
      };
    });

    const isDark = theme === 'dark';
    return {
      backgroundColor: 'transparent',
      textStyle: { color: isDark ? '#94A3B8' : '#64748B' },
      tooltip: { trigger: 'axis' },
      legend: {
        textStyle: { color: isDark ? '#F8FAFC' : '#0F172A' },
        pageTextStyle: { color: isDark ? '#94A3B8' : '#64748B' }
      },
      xAxis: {
        type: 'time',
        splitLine: { show: false },
        axisLabel: { color: isDark ? '#94A3B8' : '#64748B' }
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: isDark ? '#1E293B' : '#E2E8F0' } },
        axisLabel: { color: isDark ? '#94A3B8' : '#64748B' }
      },
      series
    };
  };

  return (
    <div className="min-h-screen bg-bgApp text-textMain px-4 py-8 md:px-8">
      {/* Top Navbar */}
      <nav className="flex justify-between items-center max-w-7xl mx-auto mb-8 border-b border-border pb-4">
        <h1 className="text-2xl font-bold flex items-center gap-2">
          <Monitor className="text-primary" /> AIStupid Dashboard
        </h1>
        <div className="flex gap-4">
          <button onClick={toggleTheme} className="p-2 border border-border rounded-lg bg-bgSurface">
            {theme === 'light' ? <Moon size={20} /> : <Sun size={20} />}
          </button>
          <button onClick={() => setIsConfigOpen(!isConfigOpen)} className="p-2 border border-border rounded-lg bg-bgSurface">
            <Settings size={20} />
          </button>
        </div>
      </nav>

      {/* Main Content */}
      <main className="max-w-7xl mx-auto grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Left 2 Cols: Charts and tables */}
        <div className="lg:col-span-2 space-y-8">
          {/* Chart Section */}
          <div className="p-6 rounded-xl border border-border bg-bgSurface">
            <h2 className="text-xl font-semibold mb-4">Performance History (30 Days)</h2>
            <div className="h-96">
              {Object.keys(history).length > 0 ? (
                <ReactECharts option={getChartOptions()} style={{ height: '100%', width: '100%' }} />
              ) : (
                <div className="flex h-full items-center justify-center text-textMuted">No data available</div>
              )}
            </div>
          </div>

          {/* Model Rankings */}
          <div className="p-6 rounded-xl border border-border bg-bgSurface overflow-hidden">
            <h2 className="text-xl font-semibold mb-4">Latest Leaderboard</h2>
            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b border-border text-textMuted">
                    <th className="py-2">Model</th>
                    <th className="py-2">Provider</th>
                    <th className="py-2">Score</th>
                    <th className="py-2">Trend</th>
                  </tr>
                </thead>
                <tbody>
                  {scores.map(s => (
                    <tr key={s.id} className="border-b border-border last:border-0 hover:bg-bgApp">
                      <td className="py-3 font-mono text-sm">{s.name}</td>
                      <td className="py-3 text-sm text-textMuted">{s.provider}</td>
                      <td className="py-3 font-semibold">{s.score}</td>
                      <td className="py-3 text-sm uppercase">{s.trend}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* Right Col: Degradations & Configuration */}
        <div className="space-y-8">
          {/* Degradations panel */}
          <div className="p-6 rounded-xl border border-border bg-bgSurface">
            <h2 className="text-xl font-semibold text-critical mb-4">Active Degradations</h2>
            {degradations.length > 0 ? (
              <div className="space-y-4">
                {degradations.map((d, i) => (
                  <div key={i} className="p-4 rounded-lg bg-bgApp border-l-4 border-critical">
                    <div className="font-semibold text-sm">{d.modelName}</div>
                    <div className="text-xs text-textMuted">{new Date(d.detectedAt).toLocaleDateString()}</div>
                    <div className="mt-2 text-sm">{d.message || `Dropped by ${d.dropPercentage}%`}</div>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-textMuted text-sm">No active degradations detected.</p>
            )}
          </div>

          {/* Settings panel */}
          {isConfigOpen && (
            <div className="p-6 rounded-xl border border-border bg-bgSurface">
              <h2 className="text-xl font-semibold mb-4">Administration</h2>
              {!isInitialized ? (
                <form onSubmit={handleSetup} className="space-y-4">
                  <p className="text-xs text-warning font-semibold">First-time Setup: Create Admin Account</p>
                  <div>
                    <label className="block text-xs uppercase text-textMuted mb-1">Username</label>
                    <input type="text" value={username} onChange={e => setUsername(e.target.value)} className="w-full p-2 border border-border bg-bgApp rounded-lg" required />
                  </div>
                  <div>
                    <label className="block text-xs uppercase text-textMuted mb-1">Password</label>
                    <input type="password" value={password} onChange={e => setPassword(e.target.value)} className="w-full p-2 border border-border bg-bgApp rounded-lg" required />
                  </div>
                  <button type="submit" className="w-full flex justify-center items-center gap-2 p-2 bg-primary text-white rounded-lg hover:opacity-90">
                    Create Admin
                  </button>
                </form>
              ) : !isLoggedIn ? (
                <form onSubmit={handleLogin} className="space-y-4">
                  <div>
                    <label className="block text-xs uppercase text-textMuted mb-1">Username</label>
                    <input type="text" value={username} onChange={e => setUsername(e.target.value)} className="w-full p-2 border border-border bg-bgApp rounded-lg" required />
                  </div>
                  <div>
                    <label className="block text-xs uppercase text-textMuted mb-1">Password</label>
                    <input type="password" value={password} onChange={e => setPassword(e.target.value)} className="w-full p-2 border border-border bg-bgApp rounded-lg" required />
                  </div>
                  <button type="submit" className="w-full flex justify-center items-center gap-2 p-2 bg-primary text-white rounded-lg hover:opacity-90">
                    <LogIn size={16} /> Sign In
                  </button>
                </form>
              ) : (
                <div className="space-y-4">
                  <p className="text-success text-sm font-semibold">Authorized Session Active</p>
                  <button onClick={triggerSync} className="w-full flex justify-center items-center gap-2 p-2 border border-border bg-bgApp rounded-lg hover:bg-bgSurface">
                    <RefreshCw size={16} /> Sync API Now
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
```

- [ ] **Step 6: Commit Frontend app**

```bash
git add frontend/
git commit -m "feat: scaffold react interface with dark mode and ECharts component"
```

---

## Task 5: Packaging & Embedded dist

**Files:**
- Create: `main.go` (modify)

- [ ] **Step 1: Build React Production dist**

```bash
cd frontend
npm run build
cd ..
```

- [ ] **Step 2: Add Embed static directive to Go router**

Import `embed` package and mount `frontend/dist` as an HTTP file system.

```go
// Add to imports in main.go
// import "embed"

//go:embed frontend/dist/*
var frontendFS embed.FS

// Modify main() in main.go:
//
// fs := http.FileServer(http.FS(frontendFS))
// http.Handle("/", http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//     // Serve static frontend files, fallback to index.html for SPA router support
//     if strings.HasPrefix(r.URL.Path, "/api") {
//         return
//     }
//     r.URL.Path = "frontend/dist" + r.URL.Path
//     fs.ServeHTTP(w, r)
// })))
```

Complete `main.go` code update:

```go
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
	"golang.org/x/crypto/bcrypt"
)

//go:embed frontend/dist
var frontendDist embed.FS

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var count int
	_ = DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count > 0 {
		http.Error(w, "First user already registered", http.StatusBadRequest)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		http.Error(w, "Invalid inputs", http.StatusBadRequest)
		return
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	_, err := DB.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", req.Username, string(hash))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"success":true}`))
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid inputs", http.StatusBadRequest)
		return
	}

	var hash string
	err := DB.QueryRow("SELECT password_hash FROM users WHERE username = ?", req.Username).Scan(&hash)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cookie := &http.Cookie{
		Name:     "session_token",
		Value:    req.Username,
		Expires:  time.Now().Add(24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
	w.Write([]byte(`{"success":true}`))
}

func checkAuth(r *http.Request) bool {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return false
	}
	return cookie.Value != ""
}

func handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == "GET" {
		rows, err := DB.Query("SELECT key, value FROM settings")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		res := make(map[string]string)
		for rows.Next() {
			var k, v string
			_ = rows.Scan(&k, &v)
			res[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
		return
	}

	if r.Method == "POST" {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid parameters", http.StatusBadRequest)
			return
		}
		for k, v := range payload {
			_, _ = DB.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", k, v)
		}
		w.Write([]byte(`{"success":true}`))
	}
}

func handleCurrent(w http.ResponseWriter, r *http.Request) {
	var latest string
	_ = DB.QueryRow("SELECT MAX(timestamp) FROM scores_history").Scan(&latest)

	rows, _ := DB.Query(`
		select m.id, m.name, m.provider, m.vendor, h.score, h.trend, h.confidence_lower, h.confidence_upper, m.is_reasoning 
		from models m 
		join scores_history h on m.id = h.model_id 
		where h.timestamp = ?`, latest)
	defer rows.Close()

	type ModelScoreRes struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Provider        string  `json:"provider"`
		Vendor          string  `json:"vendor"`
		Score           int     `json:"score"`
		Trend           string  `json:"trend"`
		ConfidenceLower float64 `json:"confidenceLower"`
		ConfidenceUpper float64 `json:"confidenceUpper"`
		IsReasoning     bool    `json:"isReasoning"`
	}

	scores := make([]ModelScoreRes, 0)
	for rows.Next() {
		var s ModelScoreRes
		var reasoning int
		_ = rows.Scan(&s.ID, &s.Name, &s.Provider, &s.Vendor, &s.Score, &s.Trend, &s.ConfidenceLower, &s.ConfidenceUpper, &reasoning)
		s.IsReasoning = reasoning == 1
		scores = append(scores, s)
	}

	degRows, _ := DB.Query(`
		select m.name, m.provider, d.drop_percentage, d.severity, d.detected_at, d.message 
		from degradations d 
		join models m on d.model_id = m.id`)
	defer degRows.Close()

	type DegRes struct {
		ModelName      string    `json:"modelName"`
		Provider       string    `json:"provider"`
		DropPercentage int       `json:"dropPercentage"`
		Severity       string    `json:"severity"`
		DetectedAt     time.Time `json:"detectedAt"`
		Message        string    `json:"message"`
	}
	degs := make([]DegRes, 0)
	for degRows.Next() {
		var d DegRes
		_ = degRows.Scan(&d.ModelName, &d.Provider, &d.DropPercentage, &d.Severity, &d.DetectedAt, &d.Message)
		degs = append(degs, d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scores":       scores,
		"degradations": degs,
	})
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		days = 30
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := DB.Query(`
		select model_id, score, timestamp 
		from scores_history 
		where timestamp >= ? 
		order by timestamp asc`, cutoff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type HistPoint struct {
		Score     int       `json:"score"`
		Timestamp time.Time `json:"timestamp"`
	}

	history := make(map[string][]HistPoint)
	for rows.Next() {
		var mid string
		var pt HistPoint
		_ = rows.Scan(&mid, &pt.Score, &pt.Timestamp)
		history[mid] = append(history[mid], pt)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func handleManualSync(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	err := FetchAndSync()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"success":true}`))
}

func main() {
	_ = InitDB("./aistupid.db")
	StartSyncWorker()

	http.HandleFunc("/api/auth/setup", handleSetup)
	http.HandleFunc("/api/auth/login", handleLogin)
	http.HandleFunc("/api/auth/status", handleAuthStatus)
	http.HandleFunc("/api/admin/settings", handleAdminSettings)
	http.HandleFunc("/api/admin/sync-now", handleManualSync)
	http.HandleFunc("/api/dashboard/current", handleCurrent)
	http.HandleFunc("/api/dashboard/history", handleHistory)

	// Embed SPA Fallback Handler
	subDist, _ := fs.Sub(frontendDist, "frontend/dist")
	fileServer := http.FileServer(http.FS(subDist))
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			return
		}
		// Serve index.html for unknown routes to allow React client router
		f, err := subDist.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	}))

	_ = http.ListenAndServe("127.0.0.1:3223", nil)
}
```

- [ ] **Step 3: Run final compilation**

```bash
go build -o aistupid-dashboard .
```
Expected output: Success with a runnable `./aistupid-dashboard` file.

- [ ] **Step 4: Commit release setup**

```bash
git add main.go
git commit -m "feat: embed production frontend static resources into single go binary"
```
