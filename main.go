package main

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	// Session token based cookie authentication
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

func SetupRoutes() {
	http.HandleFunc("/api/auth/setup", handleSetup)
	http.HandleFunc("/api/auth/login", handleLogin)
	http.HandleFunc("/api/auth/status", handleAuthStatus)
	http.HandleFunc("/api/admin/settings", handleAdminSettings)
	http.HandleFunc("/api/admin/sync-now", handleManualSync)
	http.HandleFunc("/api/dashboard/current", handleCurrent)
	http.HandleFunc("/api/dashboard/history", handleHistory)
}

func main() {
	_ = InitDB("./aistupid.db")
	StartSyncWorker()
	SetupRoutes()
	_ = http.ListenAndServe("127.0.0.1:3223", nil)
}
