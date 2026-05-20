# Specs: AIStupid Dashboard (Go + SQLite + React)

A single-binary self-hosted dashboard to monitor AI model performance drift and score history.

## System Architecture

```
[Browser Client]
       │
       ▼ (Port localhost:3223)
[Go Backend & Web Server] (Embedded React /dist)
       │
       ├─► [SQLite Database] (Data persistence)
       │
       └─► [External Target] (aistupidlevel.info API)
```

## Database Schema (SQLite)

### `users`
- `id` INTEGER PRIMARY KEY AUTOINCREMENT
- `username` TEXT UNIQUE NOT NULL
- `password_hash` TEXT NOT NULL
- `created_at` DATETIME DEFAULT CURRENT_TIMESTAMP

### `settings`
- `key` TEXT PRIMARY KEY
- `value` TEXT NOT NULL

*Default Keys:*
- `sync_interval_hours` (default: 4)
- `history_retention_days` (default: 90)
- `tracked_models` (default: `["all"]`, JSON array of model IDs)

### `models`
- `id` TEXT PRIMARY KEY
- `name` TEXT NOT NULL
- `provider` TEXT NOT NULL
- `vendor` TEXT NOT NULL
- `is_reasoning` INTEGER DEFAULT 0

### `scores_history`
- `model_id` TEXT NOT NULL
- `score` INTEGER NOT NULL
- `trend` TEXT NOT NULL
- `confidence_lower` REAL
- `confidence_upper` REAL
- `timestamp` DATETIME NOT NULL
- PRIMARY KEY (model_id, timestamp)
- FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE

### `degradations`
- `id` INTEGER PRIMARY KEY AUTOINCREMENT
- `model_id` TEXT NOT NULL
- `drop_percentage` INTEGER NOT NULL
- `severity` TEXT NOT NULL
- `detected_at` DATETIME NOT NULL
- `message` TEXT
- FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE

## API Specification

### Authentication
- `POST /api/auth/setup` -> Registers the first admin if `users` table is empty.
- `POST /api/auth/login` -> Authenticates and issues a session JWT/Cookie.
- `POST /api/auth/logout` -> Revokes active session.

### Dashboard Data
- `GET /api/dashboard/current` -> Returns latest models, scores, and degradations.
- `GET /api/dashboard/history` -> Returns score history for charting. Query parameters:
  - `days`: number of historical days.
  - `model_id`: specific model to filter.

### Admin Dashboard (Auth Required)
- `GET /api/admin/settings` -> Retrieves current configuration and setup completion state.
- `POST /api/admin/settings` -> Updates retention days, sync intervals, and tracked models.
- `POST /api/admin/sync-now` -> Triggers manual background fetching.

## UI/UX Flow
1. **Initial Wizard**: Detects if no admin account exists, redirecting users to `/setup` to create an administrator credential.
2. **Visitor Dashboard**: Displays cards for model rankings, interactive ECharts line graphs showing score trends, and list alerts for active model degradations.
3. **Admin Settings**: A secure pane to configure polling intervals, toggle models to display, select historical data pruning schedules, and manually pull upstream logs.
