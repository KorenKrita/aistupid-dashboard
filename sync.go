package main

import (
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
			LastUpdate     string `json:"lastUpdate"`
			TotalTests     int    `json:"totalTests"`
			PassedTests    int    `json:"passedTests"`
			Coverage       int    `json:"coverage"`
			Confidence     int    `json:"confidence"`
			DataPoints24h  int    `json:"dataPoints24h"`
			NextTest       string `json:"nextTest"`
			ModelsFresh    int    `json:"modelsFresh"`
			ModelsStale    int    `json:"modelsStale"`
			ModelsOffline  int    `json:"modelsOffline"`
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
	req, err := http.NewRequest("GET", "https://aistupidlevel.info"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aistupid-dashboard-selfhosted/2.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(target)
}

func FetchAndSync() error {
	syncMu.Lock()
	defer syncMu.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cached CachedResponse
	if err := fetchJSON("/dashboard/cached", &cached); err != nil {
		return fmt.Errorf("fetch cached: %w", err)
	}

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
		_, _ = tx.Exec(`INSERT INTO models (id, name, provider, vendor, is_reasoning, is_new, is_stale, status, standard_error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, provider=excluded.provider, vendor=excluded.vendor,
			is_reasoning=excluded.is_reasoning, is_new=excluded.is_new, is_stale=excluded.is_stale,
			status=excluded.status, standard_error=excluded.standard_error`,
			m.ID, m.Name, m.Provider, m.Vendor, isReasoning, isNew, isStale, m.Status, m.StandardError)
	}

	// Build trend/confidence map from modelScores
	modelTrendMap := make(map[string]string)
	modelConfMap := make(map[string][2]float64)
	for _, m := range cached.Data.ModelScores {
		modelTrendMap[m.ID] = m.Trend
		modelConfMap[m.ID] = [2]float64{m.ConfidenceLower, m.ConfidenceUpper}
	}

	// Insert history (only new records, local takes precedence)
	for modelID, points := range cached.Data.HistoryMap {
		trend := modelTrendMap[modelID]
		conf := modelConfMap[modelID]
		for _, pt := range points {
			ts, err := time.Parse(time.RFC3339, pt.Timestamp)
			if err != nil {
				continue
			}
			axes := pt.Axes
			_, _ = tx.Exec(`INSERT OR IGNORE INTO scores_history
				(model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite,
				ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
				ax_edge_cases, ax_debugging, ax_format, ax_safety,
				ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				modelID, ts, pt.Score, pt.StupidScore, trend, conf[0], conf[1], pt.Suite,
				axes["correctness"], axes["complexity"], axes["codeQuality"], axes["efficiency"], axes["stability"],
				axes["edgeCases"], axes["debugging"], axes["format"], axes["safety"],
				axes["memoryRetention"], axes["hallucinationRate"], axes["planCoherence"], axes["contextWindow"])
		}
	}

	// Insert current scores from modelScores with axes from latest historyMap entry
	for _, m := range cached.Data.ModelScores {
		ts, err := time.Parse(time.RFC3339, m.LastUpdated)
		if err != nil {
			continue
		}
		// Get axes from the latest historyMap entry for this model
		var axes map[string]float64
		if points, ok := cached.Data.HistoryMap[m.ID]; ok && len(points) > 0 {
			axes = points[len(points)-1].Axes
		}
		// Skip axes insertion if no history data available
		if axes == nil {
			_, _ = tx.Exec(`INSERT OR REPLACE INTO scores_history
				(model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				m.ID, ts, m.CurrentScore, float64(m.CurrentScore), m.Trend, m.ConfidenceLower, m.ConfidenceUpper, "current")
		} else {
			_, _ = tx.Exec(`INSERT OR REPLACE INTO scores_history
				(model_id, timestamp, score, stupid_score, trend, confidence_lower, confidence_upper, suite,
				ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
				ax_edge_cases, ax_debugging, ax_format, ax_safety,
				ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				m.ID, ts, m.CurrentScore, float64(m.CurrentScore), m.Trend, m.ConfidenceLower, m.ConfidenceUpper, "current",
				axes["correctness"], axes["complexity"], axes["codeQuality"], axes["efficiency"], axes["stability"],
				axes["edgeCases"], axes["debugging"], axes["format"], axes["safety"],
				axes["memoryRetention"], axes["hallucinationRate"], axes["planCoherence"], axes["contextWindow"])
		}
	}

	// Update degradations (keep first detected_at for same alerts)
	// Build set of current degradation keys
	currentKeys := make(map[string]bool)
	for _, d := range cached.Data.Degradations {
		var modelIDStr string
		switch v := d.ModelID.(type) {
		case string:
			modelIDStr = v
		case float64:
			modelIDStr = strconv.Itoa(int(v))
		default:
			continue
		}
		if modelIDStr == "" {
			continue
		}
		key := modelIDStr + "|" + d.Type + "|" + d.Message
		currentKeys[key] = true

		// Parse API-provided detection time, fallback to now if invalid
		detectedAt, err := time.Parse(time.RFC3339, d.DetectedAt)
		if err != nil {
			detectedAt = time.Now()
		}

		// INSERT OR IGNORE keeps the first detected_at
		_, _ = tx.Exec(`INSERT OR IGNORE INTO degradations
			(model_id, model_name, provider, current_score, baseline_score, drop_percentage, z_score, severity, detected_at, message, type)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			modelIDStr, d.ModelName, d.Provider, d.CurrentScore, d.BaselineScore, d.DropPercentage, d.ZScore, d.Severity, detectedAt, d.Message, d.Type)

		// Update other fields (but not detected_at)
		_, _ = tx.Exec(`UPDATE degradations SET current_score=?, baseline_score=?, drop_percentage=?, z_score=?, severity=?
			WHERE model_id=? AND type=? AND message=?`,
			d.CurrentScore, d.BaselineScore, d.DropPercentage, d.ZScore, d.Severity, modelIDStr, d.Type, d.Message)
	}

	// Remove degradations that are no longer in API response
	rows, err := tx.Query(`SELECT model_id, type, message FROM degradations`)
	var toDelete []struct{ modelID, typ, msg string }
	if err == nil {
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
	}
	for _, d := range toDelete {
		_, _ = tx.Exec(`DELETE FROM degradations WHERE model_id=? AND type=? AND message=?`, d.modelID, d.typ, d.msg)
	}

	// 2. Fetch alerts
	var alerts AlertsResponse
	if err := fetchJSON("/dashboard/alerts", &alerts); err == nil && alerts.Success {
		_, _ = tx.Exec("DELETE FROM alerts")
		for _, a := range alerts.Data {
			detectedAt, _ := time.Parse(time.RFC3339, a.DetectedAt)
			_, _ = tx.Exec(`INSERT INTO alerts (model_name, provider, issue, severity, detected_at)
				VALUES (?, ?, ?, ?, ?)`, a.Name, a.Provider, a.Issue, a.Severity, detectedAt)
		}
	}

	// 3. Fetch global index
	var globalIdx GlobalIndexResponse
	if err := fetchJSON("/dashboard/global-index", &globalIdx); err == nil && globalIdx.Success {
		for _, h := range globalIdx.Data.History {
			ts, _ := time.Parse(time.RFC3339, h.Timestamp)
			_, _ = tx.Exec(`INSERT OR IGNORE INTO global_index (timestamp, global_score, models_count, trend, performing_well, total_models)
				VALUES (?, ?, ?, ?, ?, ?)`,
				ts, h.GlobalScore, h.ModelsCount, globalIdx.Data.Trend, globalIdx.Data.PerformingWell, globalIdx.Data.TotalModels)
		}
	}

	// 4. Fetch provider reliability
	var provRel ProviderReliabilityResponse
	if err := fetchJSON("/analytics/provider-reliability", &provRel); err == nil && provRel.Success {
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

	// 5. Fetch recommendations
	var recs RecommendationsResponse
	if err := fetchJSON("/analytics/recommendations", &recs); err == nil && recs.Success {
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
		}
	}

	// 6. Fetch transparency
	var trans TransparencyResponse
	if err := fetchJSON("/analytics/transparency", &trans); err == nil && trans.Success {
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

	// 7. Prune old data (60 days)
	cutoff := time.Now().AddDate(0, 0, -60)
	_, _ = tx.Exec("DELETE FROM scores_history WHERE timestamp < ?", cutoff)
	_, _ = tx.Exec("DELETE FROM global_index WHERE timestamp < ?", cutoff)

	if err := tx.Commit(); err != nil {
		return err
	}
	setLastSyncTime(time.Now())
	return nil
}

func StartSyncWorkerLoop() {
	for {
		now := time.Now()
		nextMinute := ((now.Minute() / 10) + 1) * 10
		var nextSync time.Time
		if nextMinute >= 60 {
			nextSync = time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
		} else {
			nextSync = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())
		}

		sleepDuration := nextSync.Sub(now)
		if sleepDuration > 0 {
			time.Sleep(sleepDuration)
		}

		if err := FetchAndSync(); err != nil {
			fmt.Println("Scheduled sync error:", err)
		}
	}
}
