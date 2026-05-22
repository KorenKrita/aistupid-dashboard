// main.go — AI Stupid Dashboard 后端入口
// 提供模型性能监控仪表盘的 HTTP API 服务。
// 嵌入前端静态资源，周期性从上游同步评测数据，存入 SQLite。
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

// Config 存储运行时配置，当前仅包含被屏蔽的模型列表。
type Config struct {
	BlockedModels []string `json:"blocked_models"`
}

var (
	// config 保存当前生效的运行时配置，通过 configMu 保护并发安全。
	config   Config
	// configMu 读写锁，保护 config 的并发读写。读多写少场景使用 RWMutex 以减少竞争。
	configMu sync.RWMutex
)

// loadConfig 从磁盘读取 config.json，若文件不存在或解析失败则使用空配置兜底。
func loadConfig() {
	configMu.Lock()
	defer configMu.Unlock()

	data, err := os.ReadFile("config.json")
	if err != nil {
		// 配置文件不存在不影响启动，使用空配置继续运行。
		config = Config{BlockedModels: []string{}}
		return
	}
	if err := json.Unmarshal(data, &config); err != nil {
		// JSON 解析失败也使用空配置，避免因配置文件损坏导致服务不可用。
		config = Config{BlockedModels: []string{}}
	}
}

// saveConfigData 将模型列表原子地写入 config.json。
// 先写入临时文件再 rename，避免写入到一半崩溃导致配置文件损坏。
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

// saveConfig 是 saveConfigData 的线程安全封装，从 configMu 保护下复制数据后再写入。
func saveConfig() error {
	configMu.RLock()
	models := make([]string, len(config.BlockedModels))
	copy(models, config.BlockedModels)
	configMu.RUnlock()
	return saveConfigData(models)
}

// getBlockedModels 返回被屏蔽模型的副本，调用方无需关心数据竞争。
func getBlockedModels() []string {
	configMu.RLock()
	defer configMu.RUnlock()
	result := make([]string, len(config.BlockedModels))
	copy(result, config.BlockedModels)
	return result
}

// setBlockedModels 原子地设置被屏蔽模型列表：先写磁盘，失败则回滚内存状态。
func setBlockedModels(models []string) error {
	configMu.Lock()
	defer configMu.Unlock()
	old := config.BlockedModels
	config.BlockedModels = models
	if err := saveConfigData(models); err != nil {
		// 磁盘写入失败时回滚内存状态，避免内存与磁盘不一致。
		config.BlockedModels = old
		return err
	}
	return nil
}

// handleConfig 处理 /api/config 端点。
// GET 返回当前配置；POST 更新被屏蔽模型列表并返回新配置。
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
		// 请求中未提供 blocked_models 时使用空列表，避免 setBlockedModels 写入 nil。
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

// handleModels 返回所有模型的元数据列表，按名称排序。
// 包含供应商、是否支持推理、是否新模型、是否过期和标准误差等字段。
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
			// 单行扫描失败则跳过该行，不中断整个请求。
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

// handleScores 返回评分数据。
// 可选查询参数 period（24h/7d/14d/30d）控制返回时间范围。
// 不传 period 时返回每个模型的最新"current"评分（含 13 维轴数据）。
// 传 period 时返回指定天数内的历史评分序列。
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
		// period 为空或未识别：返回最新"current"评分快照。
		// 使用子查询取每个模型 suite='current' 的最新时间戳，保证只返回最新一条记录。
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

	// period 有值：返回指定天数内的所有评分记录（含所有 suite），按时间升序排列用于绘制折线图。
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
		// 将 13 维轴数据装入 map，前端可以按需读取。使用 *float64 以便处理 NULL 值。
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

// handleDegradations 返回所有性能退化记录，按降幅百分比降序排列。
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

// handleAlerts 返回所有告警记录，按检测时间降序排列（最新在前）。
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

// handleGlobalIndex 返回全局生态健康指数的时间序列，最多 100 条，按时间降序。
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

// handleProviderReliability 返回各供应商的可靠性指标，按信任分数降序排列。
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
		// SQLite 存储布尔值为 0/1 整数，需要转换。
		p.IsAvailable = isAvail == 1
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

// handleRecommendations 返回按类别划分的最佳模型推荐列表。
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

// handleTransparency 返回测试覆盖率和数据新鲜度信息。
// 包含摘要统计和每个模型的新鲜度明细两部分。
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
	// 查询透明度摘要（只取 id=1 的单行记录）。
	err := DB.QueryRow(`SELECT last_update, total_tests, passed_tests, coverage, confidence, data_points_24h, next_test, models_fresh, models_stale, models_offline FROM transparency WHERE id = 1`).
		Scan(&lastUpdate, &s.TotalTests, &s.PassedTests, &s.Coverage, &s.Confidence, &s.DataPoints24h, &nextTest, &s.ModelsFresh, &s.ModelsStale, &s.ModelsOffline)
	if err == nil {
		s.LastUpdate = lastUpdate.Format(time.RFC3339)
		s.NextTest = nextTest.Format(time.RFC3339)
	}

	// 查询各模型新鲜度明细，可独立于摘要行存在。
	rows, err := DB.Query(`SELECT model_name, last_update, minutes_ago, status FROM model_freshness`)
	if err != nil {
		// model_freshness 表可能尚未填充数据，返回空列表不报错。
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

// handleSyncStatus 返回上次同步和下次同步的时间戳。
// 使用 getLastSyncTime / getNextSyncTime 获取锁保护的时间值。
func handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	lastSync := getLastSyncTime()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"lastSync": lastSync.Format(time.RFC3339),
		"nextSync": getNextSyncTime().Format(time.RFC3339),
	})
}

// getNextSyncTime 计算下一次同步触发时间（基于当前时间）。
func getNextSyncTime() time.Time {
	return getNextSyncTimeAt(time.Now())
}

// getNextSyncTimeAt 根据指定时间计算下一个整十分钟边界。
// 同步间隔为 10 分钟，对齐到分钟的整十刻度（如 :00, :10, :20）。
func getNextSyncTimeAt(now time.Time) time.Time {
	nextMinute := ((now.Minute() / 10) + 1) * 10
	if nextMinute >= 60 {
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
	}
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())
}

// handleManualSync 处理手动触发同步的 POST 请求。
// 使用 TryFetchAndSync 尝试获取同步锁，若已有同步正在进行则返回 409 Conflict。
func handleManualSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	acquired, err := TryFetchAndSync()
	if !acquired {
		// syncMu 被占用，说明同步正在进行，返回 409 让调用方知晓。
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

// handleModelHistory 返回特定模型的历史评分时间序列。
// 参数：id（模型 ID，必填）、days（天数，可选，默认 30）。
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

// SetupRoutes 注册所有 API 路由到默认的 http.ServeMux。
// 数据查询路由和配置路由分别挂载。
func SetupRoutes() {
	// 配置和手动同步路由
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/sync-now", handleManualSync)

	// 数据查询路由
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

// main 是服务入口函数，依次完成：
// 1. 加载配置文件
// 2. 初始化 SQLite 数据库连接
// 3. 执行首次数据同步（含失败重试）
// 4. 启动后台周期性同步协程
// 5. 注册 HTTP 路由
// 6. 嵌入前端静态文件服务
// 7. 启动 HTTP 服务器并监听优雅关闭信号
func main() {
	// 加载磁盘上的屏蔽模型配置
	loadConfig()
	// 初始化 SQLite 数据库，单连接模式以避免并发写入冲突
	if err := InitDB("./aistupid.db"); err != nil {
		fmt.Println("InitDB error:", err)
		return
	}

	// 首次同步：若失败则以递增间隔重试 3 次（30s / 60s / 120s）。
	// 重试在后台协程进行，不阻塞服务启动。
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

	// 创建后台同步的上下文，通过 syncCancel 优雅停止。
	ctx, cancel := context.WithCancel(context.Background())
	syncCancel = cancel
	// 启动后台周期同步协程（每 10 分钟一次）
	go StartSyncWorkerLoop(ctx)
	SetupRoutes()

	// 从嵌入的 embed.FS 中提取前端打包文件，构建文件服务器。
	// 对于非 /api 开头的请求，优先尝试返回前端静态文件；文件不存在时回退到 index.html（SPA 支持）。
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

	// 配置 HTTP 服务器：仅监听本地回环地址，设置超时防止慢连接耗尽 goroutine。
	server := &http.Server{
		Addr:         "127.0.0.1:3223",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 优雅关闭：监听 SIGINT/SIGTERM，取消同步上下文后等待请求处理完毕再关闭。
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
