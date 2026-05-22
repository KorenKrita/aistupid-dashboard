package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var (
	lastSyncTime time.Time
	lastSyncMu   sync.RWMutex
	syncMu       sync.Mutex
	syncCancel   context.CancelFunc
	apiBaseURL   = "https://aistupidlevel.info"
)

func getLastSyncTime() time.Time {
	lastSyncMu.RLock()
	defer lastSyncMu.RUnlock()
	return lastSyncTime
}

func setLastSyncTime(t time.Time) {
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	lastSyncTime = t
}

type CachedResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ModelScores []struct {
			ID                  string  `json:"id"`
			Name                string  `json:"name"`
			Provider            string  `json:"provider"`
			Vendor              string  `json:"vendor"`
			CurrentScore        int     `json:"currentScore"`
			Score               int     `json:"score"`
			Trend               string  `json:"trend"`
			LastUpdated         string  `json:"lastUpdated"`
			Status              string  `json:"status"`
			IsNew               bool    `json:"isNew"`
			IsStale             bool    `json:"isStale"`
			UsesReasoningEffort bool    `json:"usesReasoningEffort"`
			ConfidenceLower     float64 `json:"confidenceLower"`
			ConfidenceUpper     float64 `json:"confidenceUpper"`
			StandardError       float64 `json:"standardError"`
		} `json:"modelScores"`
		HistoryMap map[string][]struct {
			Timestamp       string             `json:"timestamp"`
			Score           int                `json:"score"`
			StupidScore     float64            `json:"stupidScore"`
			Suite           string             `json:"suite"`
			Axes            map[string]float64 `json:"axes"`
			ConfidenceLower float64            `json:"confidence_lower"`
			ConfidenceUpper float64            `json:"confidence_upper"`
		} `json:"historyMap"`
		Degradations []struct {
			ModelID        interface{} `json:"modelId"`
			ModelName      string      `json:"modelName"`
			Provider       string      `json:"provider"`
			CurrentScore   int         `json:"currentScore"`
			BaselineScore  int         `json:"baselineScore"`
			DropPercentage int         `json:"dropPercentage"`
			ZScore         string      `json:"zScore"`
			Severity       string      `json:"severity"`
			DetectedAt     string      `json:"detectedAt"`
			Message        string      `json:"message"`
			Type           string      `json:"type"`
		} `json:"degradations"`
	} `json:"data"`
}

type AlertsResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		Name       string `json:"name"`
		Provider   string `json:"provider"`
		Issue      string `json:"issue"`
		Severity   string `json:"severity"`
		DetectedAt string `json:"detectedAt"`
	} `json:"data"`
}

type GlobalIndexResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Current struct {
			Timestamp   string `json:"timestamp"`
			GlobalScore int    `json:"globalScore"`
			ModelsCount int    `json:"modelsCount"`
		} `json:"current"`
		History []struct {
			Timestamp   string `json:"timestamp"`
			GlobalScore int    `json:"globalScore"`
			ModelsCount int    `json:"modelsCount"`
		} `json:"history"`
		Trend          string `json:"trend"`
		PerformingWell int    `json:"performingWell"`
		TotalModels    int    `json:"totalModels"`
	} `json:"data"`
}

type ProviderReliabilityResponse struct {
	Success bool `json:"success"`
	Data    []struct {
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
	} `json:"data"`
}

type RecommendationsResponse struct {
	Success bool `json:"success"`
	Data    struct {
		BestForCode struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Vendor   string `json:"vendor"`
			Score    int    `json:"score"`
			Reason   string `json:"reason"`
			Evidence string `json:"evidence"`
		} `json:"bestForCode"`
		MostReliable struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Vendor   string `json:"vendor"`
			Score    int    `json:"score"`
			Reason   string `json:"reason"`
			Evidence string `json:"evidence"`
		} `json:"mostReliable"`
		FastestResponse struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Vendor   string `json:"vendor"`
			Score    int    `json:"score"`
			Reason   string `json:"reason"`
			Evidence string `json:"evidence"`
		} `json:"fastestResponse"`
		AvoidNow []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"avoidNow"`
	} `json:"data"`
}

type TransparencyResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Summary struct {
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
		} `json:"summary"`
		ModelFreshness []struct {
			Model      string `json:"model"`
			LastUpdate string `json:"lastUpdate"`
			MinutesAgo int    `json:"minutesAgo"`
			Status     string `json:"status"`
		} `json:"modelFreshness"`
	} `json:"data"`
}

func fetchJSON(path string, target interface{}) error {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aistupid-dashboard-selfhosted/2.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream API returned status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func FetchAndSync() error {
	syncMu.Lock()
	defer syncMu.Unlock()
	return fetchAndSyncLocked()
}

func TryFetchAndSync() (bool, error) {
	if !syncMu.TryLock() {
		return false, nil
	}
	defer syncMu.Unlock()
	return true, fetchAndSyncLocked()
}

func fetchAndSyncLocked() error {

	var cached CachedResponse
	if err := fetchJSON("/dashboard/cached", &cached); err != nil {
		return fmt.Errorf("fetch cached: %w", err)
	}

	var alertsResp AlertsResponse
	if err := fetchJSON("/dashboard/alerts", &alertsResp); err != nil {
		fmt.Printf("Warning: fetch alerts failed: %v\n", err)
	}

	var globalIdx GlobalIndexResponse
	if err := fetchJSON("/dashboard/global-index", &globalIdx); err != nil {
		fmt.Printf("Warning: fetch global-index failed: %v\n", err)
	}

	var provRel ProviderReliabilityResponse
	if err := fetchJSON("/analytics/provider-reliability", &provRel); err != nil {
		fmt.Printf("Warning: fetch provider-reliability failed: %v\n", err)
	}

	var recs RecommendationsResponse
	if err := fetchJSON("/analytics/recommendations", &recs); err != nil {
		fmt.Printf("Warning: fetch recommendations failed: %v\n", err)
	}

	var trans TransparencyResponse
	if err := fetchJSON("/analytics/transparency", &trans); err != nil {
		fmt.Printf("Warning: fetch transparency failed: %v\n", err)
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, m := range cached.Data.ModelScores {
		isReasoning, isNew, isStale := 0, 0, 0
		if m.UsesReasoningEffort {
			isReasoning = 1
		}
		if m.IsNew {
			isNew = 1
		}
		if m.IsStale {
			isStale = 1
		}
		if _, err := tx.Exec(`INSERT INTO models (id, name, provider, vendor, is_reasoning, is_new, is_stale, status, standard_error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, provider=excluded.provider, vendor=excluded.vendor,
			is_reasoning=excluded.is_reasoning, is_new=excluded.is_new, is_stale=excluded.is_stale,
			status=excluded.status, standard_error=excluded.standard_error`,
			m.ID, m.Name, m.Provider, m.Vendor, isReasoning, isNew, isStale, m.Status, m.StandardError); err != nil {
			return fmt.Errorf("insert model %s: %w", m.ID, err)
		}
	}

	for modelID, points := range cached.Data.HistoryMap {
		for _, pt := range points {
			ts, err := time.Parse(time.RFC3339, pt.Timestamp)
			if err != nil {
				continue
			}
			suite := pt.Suite
			if suite == "" {
				suite = "test"
			}
			axes := pt.Axes
			if axes == nil {
				_, err = tx.Exec(`INSERT INTO scores_history
					(model_id, timestamp, suite, score, stupid_score, confidence_lower, confidence_upper)
					VALUES (?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(model_id, timestamp, suite) DO UPDATE SET
					score=excluded.score, stupid_score=excluded.stupid_score,
					confidence_lower=excluded.confidence_lower, confidence_upper=excluded.confidence_upper`,
					modelID, ts, suite, pt.Score, pt.StupidScore, pt.ConfidenceLower, pt.ConfidenceUpper)
			} else {
				_, err = tx.Exec(`INSERT INTO scores_history
					(model_id, timestamp, suite, score, stupid_score, confidence_lower, confidence_upper,
					ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
					ax_edge_cases, ax_debugging, ax_format, ax_safety,
					ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(model_id, timestamp, suite) DO UPDATE SET
					score=excluded.score, stupid_score=excluded.stupid_score,
					confidence_lower=excluded.confidence_lower, confidence_upper=excluded.confidence_upper,
					ax_correctness=excluded.ax_correctness, ax_complexity=excluded.ax_complexity,
					ax_code_quality=excluded.ax_code_quality, ax_efficiency=excluded.ax_efficiency,
					ax_stability=excluded.ax_stability, ax_edge_cases=excluded.ax_edge_cases,
					ax_debugging=excluded.ax_debugging, ax_format=excluded.ax_format, ax_safety=excluded.ax_safety,
					ax_memory_retention=excluded.ax_memory_retention, ax_hallucination_rate=excluded.ax_hallucination_rate,
					ax_plan_coherence=excluded.ax_plan_coherence, ax_context_window=excluded.ax_context_window`,
					modelID, ts, suite, pt.Score, pt.StupidScore, pt.ConfidenceLower, pt.ConfidenceUpper,
					axes["correctness"], axes["complexity"], axes["codeQuality"], axes["efficiency"], axes["stability"],
					axes["edgeCases"], axes["debugging"], axes["format"], axes["safety"],
					axes["memoryRetention"], axes["hallucinationRate"], axes["planCoherence"], axes["contextWindow"])
			}
			if err != nil {
				fmt.Printf("Warning: insert history %s@%s: %v\n", modelID, pt.Timestamp, err)
				continue
			}
		}
	}

	for _, m := range cached.Data.ModelScores {
		ts, err := time.Parse(time.RFC3339, m.LastUpdated)
		if err != nil {
			ts = time.Now().UTC()
		}
		var axes map[string]float64
		if points, ok := cached.Data.HistoryMap[m.ID]; ok && len(points) > 0 {
			latest := points[0]
			for _, pt := range points[1:] {
				ptTime, _ := time.Parse(time.RFC3339, pt.Timestamp)
				latestTime, _ := time.Parse(time.RFC3339, latest.Timestamp)
				if ptTime.After(latestTime) {
					latest = pt
				}
			}
			axes = latest.Axes
		}

		_, _ = tx.Exec(`DELETE FROM scores_history WHERE model_id = ? AND suite = 'current' AND timestamp != ?`, m.ID, ts)

		if axes == nil || len(axes) == 0 {
			_, err = tx.Exec(`INSERT INTO scores_history
				(model_id, timestamp, suite, score, stupid_score, trend, confidence_lower, confidence_upper)
				VALUES (?, ?, 'current', ?, ?, ?, ?, ?)
				ON CONFLICT(model_id, timestamp, suite) DO UPDATE SET
				score=excluded.score, stupid_score=excluded.stupid_score, trend=excluded.trend,
				confidence_lower=excluded.confidence_lower, confidence_upper=excluded.confidence_upper`,
				m.ID, ts, m.CurrentScore, float64(m.Score), m.Trend, m.ConfidenceLower, m.ConfidenceUpper)
		} else {
			_, err = tx.Exec(`INSERT INTO scores_history
				(model_id, timestamp, suite, score, stupid_score, trend, confidence_lower, confidence_upper,
				ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
				ax_edge_cases, ax_debugging, ax_format, ax_safety,
				ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window)
				VALUES (?, ?, 'current', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(model_id, timestamp, suite) DO UPDATE SET
				score=excluded.score, stupid_score=excluded.stupid_score, trend=excluded.trend,
				confidence_lower=excluded.confidence_lower, confidence_upper=excluded.confidence_upper,
				ax_correctness=excluded.ax_correctness, ax_complexity=excluded.ax_complexity,
				ax_code_quality=excluded.ax_code_quality, ax_efficiency=excluded.ax_efficiency,
				ax_stability=excluded.ax_stability, ax_edge_cases=excluded.ax_edge_cases,
				ax_debugging=excluded.ax_debugging, ax_format=excluded.ax_format, ax_safety=excluded.ax_safety,
				ax_memory_retention=excluded.ax_memory_retention, ax_hallucination_rate=excluded.ax_hallucination_rate,
				ax_plan_coherence=excluded.ax_plan_coherence, ax_context_window=excluded.ax_context_window`,
				m.ID, ts, m.CurrentScore, float64(m.Score), m.Trend, m.ConfidenceLower, m.ConfidenceUpper,
				axes["correctness"], axes["complexity"], axes["codeQuality"], axes["efficiency"], axes["stability"],
				axes["edgeCases"], axes["debugging"], axes["format"], axes["safety"],
				axes["memoryRetention"], axes["hallucinationRate"], axes["planCoherence"], axes["contextWindow"])
		}
		if err != nil {
			fmt.Printf("Warning: insert current score %s: %v\n", m.ID, err)
		}
	}

	// Degradations: upsert current, remove stale
	currentKeys := make(map[string]bool)
	for _, d := range cached.Data.Degradations {
		var modelIDStr string
		switch v := d.ModelID.(type) {
		case string:
			modelIDStr = v
		case float64:
			modelIDStr = strconv.FormatInt(int64(v), 10)
		default:
			continue
		}
		if modelIDStr == "" {
			continue
		}
		key := modelIDStr + "|" + d.Type + "|" + d.Message
		currentKeys[key] = true

		detectedAt, err := time.Parse(time.RFC3339, d.DetectedAt)
		if err != nil {
			detectedAt = time.Now().UTC()
		}

		_, _ = tx.Exec(`INSERT INTO degradations
			(model_id, model_name, provider, current_score, baseline_score, drop_percentage, z_score, severity, detected_at, message, type)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(model_id, type, message) DO UPDATE SET
			current_score=excluded.current_score, baseline_score=excluded.baseline_score,
			drop_percentage=excluded.drop_percentage, z_score=excluded.z_score, severity=excluded.severity`,
			modelIDStr, d.ModelName, d.Provider, d.CurrentScore, d.BaselineScore, d.DropPercentage, d.ZScore, d.Severity, detectedAt, d.Message, d.Type)
	}

	rows, err := tx.Query(`SELECT model_id, type, message FROM degradations`)
	if err == nil {
		var toDelete []struct{ modelID, typ, msg string }
		for rows.Next() {
			var modelID, typ, msg string
			if err := rows.Scan(&modelID, &typ, &msg); err == nil {
				key := modelID + "|" + typ + "|" + msg
				if !currentKeys[key] {
					toDelete = append(toDelete, struct{ modelID, typ, msg string }{modelID, typ, msg})
				}
			}
		}
		rows.Close()
		for _, d := range toDelete {
			_, _ = tx.Exec(`DELETE FROM degradations WHERE model_id=? AND type=? AND message=?`, d.modelID, d.typ, d.msg)
		}
	}

	// Alerts: full replacement
	if alertsResp.Success {
		_, _ = tx.Exec("DELETE FROM alerts")
		for _, a := range alertsResp.Data {
			detectedAt, _ := time.Parse(time.RFC3339, a.DetectedAt)
			_, _ = tx.Exec(`INSERT INTO alerts (model_name, provider, issue, severity, detected_at)
				VALUES (?, ?, ?, ?, ?)`, a.Name, a.Provider, a.Issue, a.Severity, detectedAt)
		}
	}

	// Global index
	if globalIdx.Success {
		for _, h := range globalIdx.Data.History {
			ts, err := time.Parse(time.RFC3339, h.Timestamp)
			if err != nil || ts.IsZero() {
				continue
			}
			_, _ = tx.Exec(`INSERT OR IGNORE INTO global_index (timestamp, global_score, models_count, trend, performing_well, total_models)
				VALUES (?, ?, ?, ?, ?, ?)`,
				ts, h.GlobalScore, h.ModelsCount, globalIdx.Data.Trend, globalIdx.Data.PerformingWell, globalIdx.Data.TotalModels)
		}
	}

	// Provider reliability
	if provRel.Success {
		for _, p := range provRel.Data {
			lastIncident, _ := time.Parse(time.RFC3339, p.LastIncident)
			isAvail := 0
			if p.IsAvailable {
				isAvail = 1
			}
			_, _ = tx.Exec(`INSERT INTO provider_reliability
				(provider, trust_score, total_incidents, incidents_per_month, avg_recovery_hours, last_incident, trend, active_models, top_performers, is_available, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(provider) DO UPDATE SET
				trust_score=excluded.trust_score, total_incidents=excluded.total_incidents,
				incidents_per_month=excluded.incidents_per_month, avg_recovery_hours=excluded.avg_recovery_hours,
				last_incident=excluded.last_incident, trend=excluded.trend,
				active_models=excluded.active_models, top_performers=excluded.top_performers,
				is_available=excluded.is_available, updated_at=CURRENT_TIMESTAMP`,
				p.Provider, p.TrustScore, p.TotalIncidents, p.IncidentsPerMonth, p.AvgRecoveryHours, lastIncident, p.Trend, p.ActiveModels, p.TopPerformers, isAvail)
		}
	}

	// Recommendations
	if recs.Success {
		saveRec := func(recType, id, name, vendor string, score int, reason, evidence, extra string) {
			if id == "" && extra == "" {
				_, _ = tx.Exec(`DELETE FROM recommendations WHERE type = ?`, recType)
				return
			}
			_, _ = tx.Exec(`INSERT INTO recommendations (type, model_id, model_name, vendor, score, reason, evidence, extra_data, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(type) DO UPDATE SET
				model_id=excluded.model_id, model_name=excluded.model_name, vendor=excluded.vendor,
				score=excluded.score, reason=excluded.reason, evidence=excluded.evidence,
				extra_data=excluded.extra_data, updated_at=CURRENT_TIMESTAMP`,
				recType, id, name, vendor, score, reason, evidence, extra)
		}
		b := recs.Data.BestForCode
		saveRec("best_for_code", b.ID, b.Name, b.Vendor, b.Score, b.Reason, b.Evidence, "")
		r := recs.Data.MostReliable
		saveRec("most_reliable", r.ID, r.Name, r.Vendor, r.Score, r.Reason, r.Evidence, "")
		f := recs.Data.FastestResponse
		saveRec("fastest_response", f.ID, f.Name, f.Vendor, f.Score, f.Reason, f.Evidence, "")
		if len(recs.Data.AvoidNow) > 0 {
			avoidJSON, _ := json.Marshal(recs.Data.AvoidNow)
			saveRec("avoid_now", "", "", "", 0, "", "", string(avoidJSON))
		} else {
			_, _ = tx.Exec(`DELETE FROM recommendations WHERE type = 'avoid_now'`)
		}
	}

	// Transparency
	if trans.Success {
		s := trans.Data.Summary
		lastUpdate, _ := time.Parse(time.RFC3339, s.LastUpdate)
		nextTest, _ := time.Parse(time.RFC3339, s.NextTest)
		_, _ = tx.Exec(`INSERT INTO transparency (id, last_update, total_tests, passed_tests, coverage, confidence, data_points_24h, next_test, models_fresh, models_stale, models_offline, updated_at)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(id) DO UPDATE SET
			last_update=excluded.last_update, total_tests=excluded.total_tests, passed_tests=excluded.passed_tests,
			coverage=excluded.coverage, confidence=excluded.confidence, data_points_24h=excluded.data_points_24h,
			next_test=excluded.next_test, models_fresh=excluded.models_fresh, models_stale=excluded.models_stale,
			models_offline=excluded.models_offline, updated_at=CURRENT_TIMESTAMP`,
			lastUpdate, s.TotalTests, s.PassedTests, s.Coverage, s.Confidence, s.DataPoints24h, nextTest, s.ModelsFresh, s.ModelsStale, s.ModelsOffline)

		_, _ = tx.Exec("DELETE FROM model_freshness")
		for _, mf := range trans.Data.ModelFreshness {
			lastUp, _ := time.Parse(time.RFC3339, mf.LastUpdate)
			_, _ = tx.Exec(`INSERT INTO model_freshness (model_name, last_update, minutes_ago, status) VALUES (?, ?, ?, ?)`,
				mf.Model, lastUp, mf.MinutesAgo, mf.Status)
		}
	}

	// Prune old data (60 days)
	cutoff := time.Now().UTC().AddDate(0, 0, -60)
	_, _ = tx.Exec("DELETE FROM scores_history WHERE timestamp < ?", cutoff)
	_, _ = tx.Exec("DELETE FROM global_index WHERE timestamp < ?", cutoff)

	if err := tx.Commit(); err != nil {
		return err
	}
	setLastSyncTime(time.Now())
	return nil
}

func StartSyncWorkerLoop(ctx context.Context) {
	for {
		now := time.Now()
		nextSync := getNextSyncTimeAt(now)

		sleepDuration := nextSync.Sub(now)
		if sleepDuration > 0 {
			timer := time.NewTimer(sleepDuration)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := FetchAndSync(); err != nil {
			fmt.Println("Scheduled sync error:", err)
		}
	}
}
