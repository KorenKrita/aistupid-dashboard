# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Go Backend
- Build backend binary: `go build -o aistupid-dashboard`
- Run local server: `./aistupid-dashboard` (Runs on `127.0.0.1:3223`)
- Run sync tests: `go test main.go db.go sync.go sync_test.go`
- Run main tests: `go test main.go db.go sync.go main_test.go`
- Run db tests: `go test main.go db.go sync.go db_test.go`
- Run concurrency tests: `go test main.go db.go sync.go main_test.go concurrency_test.go`
- Run time tests: `go test main.go db.go sync.go time_test.go`
- Run integration tests: `go test main.go db.go sync.go integration_test.go`

> **Note on Tests**: `go test ./...` will fail to compile because test files have cross-file dependencies (e.g., `concurrency_test.go` uses `setupTestData` from `main_test.go`). Run tests on specific file groupings as shown above.

### Frontend
- Install dependencies: `cd frontend && npm install`
- Start development server: `cd frontend && npm run dev`
- Lint code: `cd frontend && npm run lint`
- Build frontend: `cd frontend && npm run build`
- Run tests: `cd frontend && npm run test:run`
- Run tests in watch mode: `cd frontend && npm run test`

### Full Rebuild & Run
Since the Go backend embeds frontend assets at compile time using `//go:embed frontend/dist`, every frontend change requires a full rebuild cycle to be visible in the compiled Go server:
```bash
cd frontend && npm run build && cd .. && go build -o aistupid-dashboard && ./aistupid-dashboard
```

---

## High-Level Architecture & Constraints

### 1. Data Sync & Storage
- **Database**: SQLite (`aistupid.db`). Table schemas are managed dynamically in `db.go`.
- **Sync Routine**: At startup and scheduled every 10 minutes (via `StartSyncWorkerLoop` in `sync.go`), the server fetches performance metadata from `https://aistupidlevel.info/dashboard/cached` and other endpoints.
- **`scores_history` & Stale Scores**:
  - The upstream `historyMap` scores can be weeks stale.
  - To keep charts accurate, `FetchAndSync` always inserts `modelScores.currentScore` as a separate `scores_history` record with `suite='current'`.
  - Axes for this `suite='current'` record must be copied from the latest `historyMap` entry for that model.
  - Use `ON CONFLICT ... DO UPDATE` to upsert the record on re-sync. If the INSERT fails, the transaction rolls back to avoid data loss.
  - History API endpoints include all suites (including 'current') so charts always show the latest data point.
  - Raw and history data older than 60 days is automatically pruned on each sync.

### 2. Frontend & Charts
- **Stack**: React 19 + TypeScript + Vite + Tailwind CSS 4 + ECharts (`echarts-for-react`). Icons via `lucide-react`.
- **Deep Test Dimensions Filtering**:
  Radar charts and the model detail axis selector must filter out the following 4 deep test dimensions consistently:
  - `memoryRetention` (ax_memory_retention)
  - `hallucinationRate` (ax_hallucination_rate)
  - `planCoherence` (ax_plan_coherence)
  - `contextWindow` (ax_context_window)
  Only display the core 9 dimensions: `correctness`, `complexity`, `codeQuality`, `efficiency`, `stability`, `edgeCases`, `debugging`, `format`, `safety`.

### 3. API Endpoints
All endpoints are under `/api/` and return JSON. Key routes:
- `/api/models` — model list with metadata (provider, vendor, status, reasoning flag)
- `/api/scores` — latest scores per model with all 13 axis values
- `/api/model/history?id=ID&days=N` — historical score timeseries (includes current suite)
- `/api/degradations`, `/api/alerts` — incident tracking
- `/api/global-index` — aggregate ecosystem health score
- `/api/provider-reliability` — per-provider trust metrics
- `/api/recommendations` — best model picks by category
- `/api/transparency` — test coverage and freshness stats
- `/api/sync-status` — last/next sync timestamps
- `/api/sync-now` (POST) — trigger manual sync
- `/api/config` (GET/POST) — blocked models list

### 4. Concurrency
- `syncMu` (sync.Mutex) serializes sync operations — only one `FetchAndSync` runs at a time.
- `lastSyncMu` (sync.RWMutex) protects the last sync timestamp for concurrent reads.
- `configMu` (sync.RWMutex) protects the blocked models config.
- SQLite is opened with `SetMaxOpenConns(1)` and `busy_timeout(5000)` to avoid SQLITE_BUSY.

### 5. UI/Layout Constraints
- **CSS Grid Two-Column Height Matching**:
  - When rendering two columns, align heights by attaching a `ResizeObserver` to the left column wrapper, then set the right column's height dynamically via an inline style.
  - The scrollable card container in the column should use: `flex-1 flex flex-col min-h-0 overflow-hidden`.
  - Its scrollable inner area must use: `overflow-y-auto flex-1 min-h-0`.
- **Frontend is a single-file SPA**: All UI lives in `frontend/src/App.tsx` (~1400 lines). No component splitting — all views, charts, and state are co-located.
