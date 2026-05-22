package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed frontend/dist
var frontendDist embed.FS

type Config struct {
	BlockedModels []string `json:"blocked_models"`
}

var (
	config   Config
	configMu sync.RWMutex
)

func loadConfig() {
	configMu.Lock()
	defer configMu.Unlock()

	data, err := os.ReadFile("config.json")
	if err != nil {
		config = Config{BlockedModels: []string{}}
		return
	}
	if err := json.Unmarshal(data, &config); err != nil {
		config = Config{BlockedModels: []string{}}
	}
}

func saveConfigData(models []string) error {
	data, err := json.MarshalIndent(Config{BlockedModels: models}, "", "  ")
	if err != nil {
		return err
	}
	tmp := "config.json.tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, "config.json")
}

func saveConfig() error {
	configMu.RLock()
	models := make([]string, len(config.BlockedModels))
	copy(models, config.BlockedModels)
	configMu.RUnlock()
	return saveConfigData(models)
}

func getBlockedModels() []string {
	configMu.RLock()
	defer configMu.RUnlock()
	result := make([]string, len(config.BlockedModels))
	copy(result, config.BlockedModels)
	return result
}

func setBlockedModels(models []string) error {
	configMu.Lock()
	defer configMu.Unlock()
	old := config.BlockedModels
	config.BlockedModels = models
	if err := saveConfigData(models); err != nil {
		config.BlockedModels = old
		return err
	}
	return nil
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"blocked_models": getBlockedModels(),
		})
	case http.MethodPost:
		var body struct {
			BlockedModels []string `json:"blocked_models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if body.BlockedModels == nil {
			body.BlockedModels = []string{}
		}
		if err := setBlockedModels(body.BlockedModels); err != nil {
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"blocked_models": getBlockedModels(),
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT id, name, provider, vendor, is_reasoning, is_new, is_stale, status, standard_error FROM models ORDER BY name`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Model struct {
		ID            string  `json:"id"`
		Name          string  `json:"name"`
		Provider      string  `json:"provider"`
		Vendor        string  `json:"vendor"`
		IsReasoning   bool    `json:"isReasoning"`
		IsNew         bool    `json:"isNew"`
		IsStale       bool    `json:"isStale"`
		Status        string  `json:"status"`
		StandardError float64 `json:"standardError"`
	}

	models := []Model{}
	for rows.Next() {
		var m Model
		var isReasoning, isNew, isStale int
		if err := rows.Scan(&m.ID, &m.Name, &m.Provider, &m.Vendor, &isReasoning, &isNew, &isStale, &m.Status, &m.StandardError); err != nil {
			continue
		}
		m.IsReasoning = isReasoning == 1
		m.IsNew = isNew == 1
		m.IsStale = isStale == 1
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(models)
}

func handleScores(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	period := r.URL.Query().Get("period")
	days := 1
	switch period {
	case "7d":
		days = 7
	case "14d":
		days = 14
	case "30d":
		days = 30
	case "24h":
		days = 1
	default:
		days = 0
	}

	if days == 0 {
		type LatestScore struct {
			ModelID         string   `json:"modelId"`
			ModelName       string   `json:"modelName"`
			Provider        string   `json:"provider"`
			Score           int      `json:"score"`
			Trend           string   `json:"trend"`
			ConfidenceLower float64  `json:"confidenceLower"`
			ConfidenceUpper float64  `json:"confidenceUpper"`
			StandardError   float64  `json:"standardError"`
			Timestamp       string   `json:"timestamp"`
			Axes            struct {
				Correctness       *float64 `json:"correctness"`
				Complexity        *float64 `json:"complexity"`
				CodeQuality       *float64 `json:"codeQuality"`
				Efficiency        *float64 `json:"efficiency"`
				Stability         *float64 `json:"stability"`
				EdgeCases         *float64 `json:"edgeCases"`
				Debugging         *float64 `json:"debugging"`
				Format            *float64 `json:"format"`
				Safety            *float64 `json:"safety"`
				MemoryRetention   *float64 `json:"memoryRetention"`
				HallucinationRate *float64 `json:"hallucinationRate"`
				PlanCoherence     *float64 `json:"planCoherence"`
				ContextWindow     *float64 `json:"contextWindow"`
			} `json:"axes"`
		}

		sqlRows, qErr := DB.Query(`
			SELECT h.model_id, m.name, m.provider, h.score, h.trend, h.confidence_lower, h.confidence_upper, m.standard_error, h.timestamp,
				h.ax_correctness, h.ax_complexity, h.ax_code_quality, h.ax_efficiency, h.ax_stability,
				h.ax_edge_cases, h.ax_debugging, h.ax_format, h.ax_safety,
				h.ax_memory_retention, h.ax_hallucination_rate, h.ax_plan_coherence, h.ax_context_window
			FROM scores_history h
			JOIN models m ON h.model_id = m.id
			WHERE h.suite = 'current'
			AND h.timestamp = (SELECT MAX(timestamp) FROM scores_history WHERE model_id = h.model_id AND suite = 'current')
			ORDER BY h.score DESC`)
		if qErr != nil {
			http.Error(w, qErr.Error(), http.StatusInternalServerError)
			return
		}
		defer sqlRows.Close()

		results := []LatestScore{}
		for sqlRows.Next() {
			var s LatestScore
			var ts time.Time
			if err := sqlRows.Scan(&s.ModelID, &s.ModelName, &s.Provider, &s.Score, &s.Trend, &s.ConfidenceLower, &s.ConfidenceUpper, &s.StandardError, &ts,
				&s.Axes.Correctness, &s.Axes.Complexity, &s.Axes.CodeQuality, &s.Axes.Efficiency, &s.Axes.Stability,
				&s.Axes.EdgeCases, &s.Axes.Debugging, &s.Axes.Format, &s.Axes.Safety,
				&s.Axes.MemoryRetention, &s.Axes.HallucinationRate, &s.Axes.PlanCoherence, &s.Axes.ContextWindow); err != nil {
				continue
			}
			s.Timestamp = ts.Format(time.RFC3339)
			results = append(results, s)
		}
		if err := sqlRows.Err(); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(results)
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	sqlRows, err := DB.Query(`
		SELECT h.model_id, m.name, h.score, h.timestamp, h.suite,
			h.ax_correctness, h.ax_complexity, h.ax_code_quality, h.ax_efficiency, h.ax_stability,
			h.ax_edge_cases, h.ax_debugging, h.ax_format, h.ax_safety,
			h.ax_memory_retention, h.ax_hallucination_rate, h.ax_plan_coherence, h.ax_context_window
		FROM scores_history h
		JOIN models m ON h.model_id = m.id
		WHERE h.timestamp >= ?
		ORDER BY h.timestamp ASC`, cutoff)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer sqlRows.Close()

	type HistoryPoint struct {
		ModelID   string              `json:"modelId"`
		ModelName string              `json:"modelName"`
		Score     int                 `json:"score"`
		Timestamp string              `json:"timestamp"`
		Suite     string              `json:"suite"`
		Axes      map[string]*float64 `json:"axes"`
	}

	results := []HistoryPoint{}
	for sqlRows.Next() {
		var h HistoryPoint
		var ts time.Time
		var axCorr, axComp, axQual, axEff, axStab, axEdge, axDebug, axFmt, axSafe, axMem, axHall, axPlan, axCtx *float64
		if err := sqlRows.Scan(&h.ModelID, &h.ModelName, &h.Score, &ts, &h.Suite,
			&axCorr, &axComp, &axQual, &axEff, &axStab, &axEdge, &axDebug, &axFmt, &axSafe, &axMem, &axHall, &axPlan, &axCtx); err != nil {
			continue
		}
		h.Timestamp = ts.Format(time.RFC3339)
		h.Axes = map[string]*float64{
			"correctness": axCorr, "complexity": axComp, "codeQuality": axQual,
			"efficiency": axEff, "stability": axStab, "edgeCases": axEdge,
			"debugging": axDebug, "format": axFmt, "safety": axSafe,
			"memoryRetention": axMem, "hallucinationRate": axHall, "planCoherence": axPlan, "contextWindow": axCtx,
		}
		results = append(results, h)
	}
	if err := sqlRows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleDegradations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT model_id, model_name, provider, current_score, baseline_score, drop_percentage, z_score, severity, detected_at, message, type FROM degradations ORDER BY drop_percentage DESC`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Degradation struct {
		ModelID        string `json:"modelId"`
		ModelName      string `json:"modelName"`
		Provider       string `json:"provider"`
		CurrentScore   int    `json:"currentScore"`
		BaselineScore  int    `json:"baselineScore"`
		DropPercentage int    `json:"dropPercentage"`
		ZScore         string `json:"zScore"`
		Severity       string `json:"severity"`
		DetectedAt     string `json:"detectedAt"`
		Message        string `json:"message"`
		Type           string `json:"type"`
	}

	results := []Degradation{}
	for rows.Next() {
		var d Degradation
		var ts time.Time
		if err := rows.Scan(&d.ModelID, &d.ModelName, &d.Provider, &d.CurrentScore, &d.BaselineScore, &d.DropPercentage, &d.ZScore, &d.Severity, &ts, &d.Message, &d.Type); err != nil {
			continue
		}
		d.DetectedAt = ts.Format(time.RFC3339)
		results = append(results, d)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleAlerts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT model_name, provider, issue, severity, detected_at FROM alerts ORDER BY detected_at DESC`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Alert struct {
		ModelName  string `json:"modelName"`
		Provider   string `json:"provider"`
		Issue      string `json:"issue"`
		Severity   string `json:"severity"`
		DetectedAt string `json:"detectedAt"`
	}

	results := []Alert{}
	for rows.Next() {
		var a Alert
		var ts time.Time
		if err := rows.Scan(&a.ModelName, &a.Provider, &a.Issue, &a.Severity, &ts); err != nil {
			continue
		}
		a.DetectedAt = ts.Format(time.RFC3339)
		results = append(results, a)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleGlobalIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT timestamp, global_score, models_count, trend, performing_well, total_models FROM global_index ORDER BY timestamp DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type GlobalIndex struct {
		Timestamp      string `json:"timestamp"`
		GlobalScore    int    `json:"globalScore"`
		ModelsCount    int    `json:"modelsCount"`
		Trend          string `json:"trend"`
		PerformingWell int    `json:"performingWell"`
		TotalModels    int    `json:"totalModels"`
	}

	results := []GlobalIndex{}
	for rows.Next() {
		var g GlobalIndex
		var ts time.Time
		if err := rows.Scan(&ts, &g.GlobalScore, &g.ModelsCount, &g.Trend, &g.PerformingWell, &g.TotalModels); err != nil {
			continue
		}
		g.Timestamp = ts.Format(time.RFC3339)
		results = append(results, g)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleProviderReliability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT provider, trust_score, total_incidents, incidents_per_month, avg_recovery_hours, last_incident, trend, active_models, top_performers, is_available FROM provider_reliability ORDER BY trust_score DESC`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type ProviderReliability struct {
		Provider          string `json:"provider"`
		TrustScore        int    `json:"trustScore"`
		TotalIncidents    int    `json:"totalIncidents"`
		IncidentsPerMonth int    `json:"incidentsPerMonth"`
		AvgRecoveryHours  string `json:"avgRecoveryHours"`
		LastIncident      string `json:"lastIncident"`
		Trend             string `json:"trend"`
		ActiveModels      int    `json:"activeModels"`
		TopPerformers     int    `json:"topPerformers"`
		IsAvailable       bool   `json:"isAvailable"`
	}

	results := []ProviderReliability{}
	for rows.Next() {
		var p ProviderReliability
		var ts time.Time
		var isAvail int
		if err := rows.Scan(&p.Provider, &p.TrustScore, &p.TotalIncidents, &p.IncidentsPerMonth, &p.AvgRecoveryHours, &ts, &p.Trend, &p.ActiveModels, &p.TopPerformers, &isAvail); err != nil {
			continue
		}
		p.LastIncident = ts.Format(time.RFC3339)
		p.IsAvailable = isAvail == 1
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleRecommendations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := DB.Query(`SELECT type, model_id, model_name, vendor, score, reason, evidence, extra_data FROM recommendations`)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Recommendation struct {
		Type      string `json:"type"`
		ModelID   string `json:"modelId"`
		ModelName string `json:"modelName"`
		Vendor    string `json:"vendor"`
		Score     int    `json:"score"`
		Reason    string `json:"reason"`
		Evidence  string `json:"evidence"`
		ExtraData string `json:"extraData,omitempty"`
	}

	results := []Recommendation{}
	for rows.Next() {
		var rec Recommendation
		if err := rows.Scan(&rec.Type, &rec.ModelID, &rec.ModelName, &rec.Vendor, &rec.Score, &rec.Reason, &rec.Evidence, &rec.ExtraData); err != nil {
			continue
		}
		results = append(results, rec)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func handleTransparency(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type Summary struct {
		LastUpdate    string `json:"lastUpdate"`
		TotalTests    int    `json:"totalTests"`
		PassedTests   int    `json:"passedTests"`
		Coverage      int    `json:"coverage"`
		Confidence    int    `json:"confidence"`
		DataPoints24h int    `json:"dataPoints24h"`
		NextTest      string `json:"nextTest"`
		ModelsFresh   int    `json:"modelsFresh"`
		ModelsStale   int    `json:"modelsStale"`
		ModelsOffline int    `json:"modelsOffline"`
	}

	type ModelFreshness struct {
		Model      string `json:"model"`
		LastUpdate string `json:"lastUpdate"`
		MinutesAgo int    `json:"minutesAgo"`
		Status     string `json:"status"`
	}

	var s Summary
	var lastUpdate, nextTest time.Time
	err := DB.QueryRow(`SELECT last_update, total_tests, passed_tests, coverage, confidence, data_points_24h, next_test, models_fresh, models_stale, models_offline FROM transparency WHERE id = 1`).
		Scan(&lastUpdate, &s.TotalTests, &s.PassedTests, &s.Coverage, &s.Confidence, &s.DataPoints24h, &nextTest, &s.ModelsFresh, &s.ModelsStale, &s.ModelsOffline)
	if err == nil {
		s.LastUpdate = lastUpdate.Format(time.RFC3339)
		s.NextTest = nextTest.Format(time.RFC3339)
	}

	rows, err := DB.Query(`SELECT model_name, last_update, minutes_ago, status FROM model_freshness`)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"summary":        s,
			"modelFreshness": []ModelFreshness{},
		})
		return
	}
	defer rows.Close()

	freshness := []ModelFreshness{}
	for rows.Next() {
		var mf ModelFreshness
		var ts time.Time
		if err := rows.Scan(&mf.Model, &ts, &mf.MinutesAgo, &mf.Status); err != nil {
			continue
		}
		mf.LastUpdate = ts.Format(time.RFC3339)
		freshness = append(freshness, mf)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"summary":        s,
		"modelFreshness": freshness,
	})
}

func handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	lastSync := getLastSyncTime()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"lastSync": lastSync.Format(time.RFC3339),
		"nextSync": getNextSyncTime().Format(time.RFC3339),
	})
}

func getNextSyncTime() time.Time {
	return getNextSyncTimeAt(time.Now())
}

func getNextSyncTimeAt(now time.Time) time.Time {
	nextMinute := ((now.Minute() / 10) + 1) * 10
	if nextMinute >= 60 {
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	}
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())
}

func handleManualSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	acquired, err := TryFetchAndSync()
	if !acquired {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "sync already in progress"})
		return
	}
	if err != nil {
		http.Error(w, "Sync failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func handleModelHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	modelID := r.URL.Query().Get("id")
	if modelID == "" {
		http.Error(w, "Missing model id", http.StatusBadRequest)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		days = 30
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := DB.Query(`
		SELECT timestamp, score, stupid_score, suite, confidence_lower, confidence_upper,
			ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
			ax_edge_cases, ax_debugging, ax_format, ax_safety,
			ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window
		FROM scores_history
		WHERE model_id = ? AND timestamp >= ?
		ORDER BY timestamp ASC`, modelID, cutoff)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type HistoryPoint struct {
		Timestamp       string              `json:"timestamp"`
		Score           int                 `json:"score"`
		StupidScore     float64             `json:"stupidScore"`
		Suite           string              `json:"suite"`
		ConfidenceLower float64             `json:"confidenceLower"`
		ConfidenceUpper float64             `json:"confidenceUpper"`
		Axes            map[string]*float64 `json:"axes"`
	}

	results := []HistoryPoint{}
	for rows.Next() {
		var h HistoryPoint
		var ts time.Time
		var axCorr, axComp, axQual, axEff, axStab, axEdge, axDebug, axFmt, axSafe, axMem, axHall, axPlan, axCtx *float64
		if err := rows.Scan(&ts, &h.Score, &h.StupidScore, &h.Suite, &h.ConfidenceLower, &h.ConfidenceUpper,
			&axCorr, &axComp, &axQual, &axEff, &axStab, &axEdge, &axDebug, &axFmt, &axSafe, &axMem, &axHall, &axPlan, &axCtx); err != nil {
			continue
		}
		h.Timestamp = ts.Format(time.RFC3339)
		h.Axes = map[string]*float64{
			"correctness": axCorr, "complexity": axComp, "codeQuality": axQual,
			"efficiency": axEff, "stability": axStab, "edgeCases": axEdge,
			"debugging": axDebug, "format": axFmt, "safety": axSafe,
			"memoryRetention": axMem, "hallucinationRate": axHall, "planCoherence": axPlan, "contextWindow": axCtx,
		}
		results = append(results, h)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func SetupRoutes() {
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/sync-now", handleManualSync)

	http.HandleFunc("/api/models", handleModels)
	http.HandleFunc("/api/scores", handleScores)
	http.HandleFunc("/api/model/history", handleModelHistory)
	http.HandleFunc("/api/degradations", handleDegradations)
	http.HandleFunc("/api/alerts", handleAlerts)
	http.HandleFunc("/api/global-index", handleGlobalIndex)
	http.HandleFunc("/api/provider-reliability", handleProviderReliability)
	http.HandleFunc("/api/recommendations", handleRecommendations)
	http.HandleFunc("/api/transparency", handleTransparency)
	http.HandleFunc("/api/sync-status", handleSyncStatus)
}

func main() {
	loadConfig()
	if err := InitDB("./aistupid.db"); err != nil {
		fmt.Println("InitDB error:", err)
		return
	}

	if err := FetchAndSync(); err != nil {
		fmt.Println("Initial sync error:", err)
		go func() {
			for _, delay := range []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second} {
				time.Sleep(delay)
				if err := FetchAndSync(); err != nil {
					fmt.Printf("Retry sync error (after %v): %v\n", delay, err)
					continue
				}
				fmt.Println("Retry sync succeeded")
				return
			}
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	syncCancel = cancel
	go StartSyncWorkerLoop(ctx)
	SetupRoutes()

	subDist, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		fmt.Println("Frontend embed error:", err)
		return
	}
	fileServer := http.FileServer(http.FS(subDist))
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		f, err := subDist.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	}))

	server := &http.Server{
		Addr:         "127.0.0.1:3223",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("Shutting down...")
		syncCancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Println("Server error:", err)
	}
}
