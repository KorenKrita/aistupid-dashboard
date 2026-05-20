package main

import (
	"encoding/json"
	"net/http"
	"strconv"
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
			modelIDStr = strconv.Itoa(int(v))
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
