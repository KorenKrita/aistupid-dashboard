package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

// migrateScoresHistory 迁移 scores_history 表的主键，将 suite 字段加入复合主键。
//
// 迁移背景：旧版 scores_history 表的主键为 (model_id, timestamp)，
// 但同一个模型在同一时间戳可能产生多组分数（例如历史数据和当前分数），
// 导致主键冲突。新版将 suite 字段纳入主键，形成 (model_id, timestamp, suite) 三元组主键，
// 使不同来源的分数可以共存。
//
// 迁移策略：该迁移是幂等的，仅在旧主键结构中不包含 suite 时执行。
// 采用"重命名旧表 -> 创建新表 -> 拷贝数据 -> 删除旧表"的四步策略，
// 全部在单个事务中完成，确保原子性——迁移中途失败时自动回滚，不会丢失数据。
func migrateScoresHistory() error {
	var count int
	// 检查 scores_history 表的 pragma_table_info 中 suite 字段是否已是主键的一部分 (pk > 0)
	err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('scores_history') WHERE name='suite' AND pk > 0`).Scan(&count)
	if err != nil {
		// pragma 查询出错（例如表不存在），说明不需要迁移
		return nil
	}
	if count > 0 {
		// suite 已经是主键的一部分，无需迁移
		return nil
	}

	// 确认 scores_history 表确实存在，防止误操作
	var tableExists int
	DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='scores_history'`).Scan(&tableExists)
	if tableExists == 0 {
		return nil
	}

	fmt.Println("Migrating scores_history: adding suite to PRIMARY KEY...")

	// 开启事务，保证迁移操作的原子性
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("migration begin tx: %w", err)
	}
	defer tx.Rollback()

	// 第一步：将旧表重命名为临时表
	_, err = tx.Exec(`ALTER TABLE scores_history RENAME TO scores_history_old`)
	if err != nil {
		return fmt.Errorf("migration rename: %w", err)
	}
	// 第二步：创建包含新主键结构的新表
	_, err = tx.Exec(`CREATE TABLE scores_history (
		model_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		suite TEXT NOT NULL DEFAULT '',
		score INTEGER NOT NULL,
		stupid_score REAL,
		trend TEXT,
		confidence_lower REAL,
		confidence_upper REAL,
		ax_correctness REAL,
		ax_complexity REAL,
		ax_code_quality REAL,
		ax_efficiency REAL,
		ax_stability REAL,
		ax_edge_cases REAL,
		ax_debugging REAL,
		ax_format REAL,
		ax_safety REAL,
		ax_memory_retention REAL,
		ax_hallucination_rate REAL,
		ax_plan_coherence REAL,
		ax_context_window REAL,
		PRIMARY KEY (model_id, timestamp, suite),
		FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
	)`)
	if err != nil {
		return fmt.Errorf("migration create: %w", err)
	}
	// 第三步：将旧表数据拷贝到新表，suite 字段用 COALESCE 填充空字符串作为默认值
	_, err = tx.Exec(`INSERT OR IGNORE INTO scores_history
		(model_id, timestamp, suite, score, stupid_score, trend, confidence_lower, confidence_upper,
		 ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
		 ax_edge_cases, ax_debugging, ax_format, ax_safety,
		 ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window)
		SELECT model_id, timestamp, COALESCE(suite, ''), score, stupid_score, trend, confidence_lower, confidence_upper,
		 ax_correctness, ax_complexity, ax_code_quality, ax_efficiency, ax_stability,
		 ax_edge_cases, ax_debugging, ax_format, ax_safety,
		 ax_memory_retention, ax_hallucination_rate, ax_plan_coherence, ax_context_window
		FROM scores_history_old`)
	if err != nil {
		return fmt.Errorf("migration copy: %w", err)
	}
	// 第四步：删除临时旧表
	_, err = tx.Exec(`DROP TABLE scores_history_old`)
	if err != nil {
		return fmt.Errorf("migration drop old: %w", err)
	}

	// 提交事务，使迁移生效
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration commit: %w", err)
	}
	fmt.Println("Migration complete.")
	return nil
}

// InitDB 初始化 SQLite 数据库连接并创建所有必要的表结构和索引。
//
// 参数 filepath 指定数据库文件的路径（例如 "aistupid.db"）。
// 该函数在应用启动时调用，执行以下操作：
//  1. 建立数据库连接并配置连接池
//  2. 执行数据库迁移（如果存在旧版本 schema）
//  3. 创建所有表（如不存在则跳过）
//  4. 创建查询性能所需的索引
func InitDB(filepath string) error {
	var err error
	// busy_timeout(5000): SQLite 在并发写入时返回 SQLITE_BUSY，该配置让 SQLite 等待最多 5000ms
	// 而非立即失败。由于本应用使用 SetMaxOpenConns(1) 限制了并发连接数，该配置作为额外安全网，
	// 防止极端情况下的写入冲突。
	// foreign_keys(1): 启用外键约束强制执行。SQLite 默认不强制外键（出于向后兼容），
	// 但本应用的 scores_history 和 degradations 表依赖外键级联删除来维护数据一致性。
	DB, err = sql.Open("sqlite", filepath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	// SetMaxOpenConns(1): SQLite 是嵌入式数据库，写入时锁定整个数据库文件。
	// 将最大打开连接数设为 1 避免 SQLITE_BUSY 错误，确保所有数据库操作串行执行。
	// 这是 SQLite 在并发场景下的推荐配置。
	DB.SetMaxOpenConns(1)

	// 执行数据表迁移，确保表结构与当前代码兼容
	if err := migrateScoresHistory(); err != nil {
		return fmt.Errorf("migration: %w", err)
	}

	// models: 模型元数据表，记录所有 AI 模型的基本信息和当前状态
	// schemas 中 CREATE TABLE IF NOT EXISTS 保证多次初始化不会覆盖已有数据
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS models (
			id TEXT PRIMARY KEY,                       -- 模型唯一标识符（如 "gpt-4", "claude-3"）
			name TEXT NOT NULL,                        -- 模型显示名称
			provider TEXT NOT NULL,                    -- 提供商名称（如 "openai", "anthropic"）
			vendor TEXT NOT NULL,                      -- 厂商名称（可能与 provider 不同，如 "Microsoft" 通过 Azure 提供）
			is_reasoning INTEGER DEFAULT 0,            -- 是否支持推理能力（0=否, 1=是）
			is_new INTEGER DEFAULT 0,                  -- 是否为新增模型（用于 UI 标记 "New" 标签）
			is_stale INTEGER DEFAULT 0,                -- 分数数据是否已过期（长时间未更新时置为 1）
			status TEXT DEFAULT 'unknown',              -- 模型状态（"active", "deprecated", "unknown" 等）
			standard_error REAL DEFAULT 0              -- 评估分数的标准误差，反映分数可信度
		);`,

		// scores_history: 模型分数历史记录表，按时间线存储每个模型的评估结果
		// 主键为 (model_id, timestamp, suite) 三元组，允许同一模型在同一时间点存在多组分数
		//（例如来自历史数据套件和当前数据套件）
		`CREATE TABLE IF NOT EXISTS scores_history (
			model_id TEXT NOT NULL,                    -- 关联模型 ID，外键指向 models(id)
			timestamp DATETIME NOT NULL,               -- 分数记录时间戳
			suite TEXT NOT NULL DEFAULT '',             -- 数据套件名称（如 "history", "current"），区分同一时间点的多组数据
			score INTEGER NOT NULL,                    -- 综合评分（整数，通常 0-100）
			stupid_score REAL,                         -- "愚蠢度"评分（越低越好，仅用于参考）
			trend TEXT,                                -- 趋势方向（"up", "down", "stable"）
			confidence_lower REAL,                     -- 置信区间下限
			confidence_upper REAL,                     -- 置信区间上限
			ax_correctness REAL,                       -- 维度：正确性（答案准确度）
			ax_complexity REAL,                        -- 维度：复杂度（处理复杂问题的能力）
			ax_code_quality REAL,                      -- 维度：代码质量（生成代码的可读性和维护性）
			ax_efficiency REAL,                        -- 维度：效率（资源利用和响应速度）
			ax_stability REAL,                         -- 维度：稳定性（输出一致性和可靠性）
			ax_edge_cases REAL,                        -- 维度：边界情况（处理异常输入的能力）
			ax_debugging REAL,                         -- 维度：调试能力（识别和修复错误的能力）
			ax_format REAL,                            -- 维度：格式合规性（输出格式的准确度）
			ax_safety REAL,                            -- 维度：安全性（避免有害输出的能力）
			ax_memory_retention REAL,                  -- 维度：记忆保持（长对话中的上下文记忆能力，深测维度）
			ax_hallucination_rate REAL,                -- 维度：幻觉率（生成虚构信息的频率，深测维度）
			ax_plan_coherence REAL,                    -- 维度：规划连贯性（长任务规划的合理性，深测维度）
			ax_context_window REAL,                    -- 维度：上下文窗口（支持的最大上下文长度，深测维度）
			PRIMARY KEY (model_id, timestamp, suite),  -- 复合主键：同一模型同一时刻可以有多个数据套件
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE  -- 模型删除时自动清理关联分数
		);`,

		// degradations: 模型性能退化记录表，自动检测并存储分数显著下降的事件
		// 每次同步时与基线分数比较，超过阈值则插入新的退化记录
		`CREATE TABLE IF NOT EXISTS degradations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,      -- 自增主键
			model_id TEXT NOT NULL,                    -- 关联模型 ID
			model_name TEXT,                           -- 模型名称（冗余字段，方便查询时减少 JOIN）
			provider TEXT,                             -- 提供商名称（冗余字段）
			current_score INTEGER,                     -- 当前分数
			baseline_score INTEGER,                    -- 基线分数（退化前的正常分数）
			drop_percentage INTEGER NOT NULL,          -- 下降百分比（正整数，如 15 表示下降 15%）
			z_score TEXT,                              -- Z 分数（以文本形式存储，衡量退化统计显著性）
			severity TEXT NOT NULL,                    -- 严重程度（"critical", "major", "minor"）
			detected_at DATETIME NOT NULL,             -- 退化检测时间
			message TEXT,                              -- 描述信息（如 "Score dropped from 85 to 72"）
			type TEXT,                                 -- 退化类型（如 "score_drop"）
			UNIQUE(model_id, type, message),           -- 防止重复记录同一退化事件
			FOREIGN KEY (model_id) REFERENCES models(id) ON DELETE CASCADE
		);`,

		// alerts: 告警记录表，存储系统运行时检测到的各种问题
		// 与 degradations 不同，alerts 更偏向运维层面的通知（如数据源不可用）
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,      -- 自增主键
			model_name TEXT NOT NULL,                  -- 关联模型名称
			provider TEXT,                             -- 提供商名称
			issue TEXT NOT NULL,                       -- 问题描述
			severity TEXT,                             -- 严重程度（"warning", "error", "critical"）
			detected_at DATETIME NOT NULL              -- 告警触发时间
		);`,

		// global_index: 生态健康全局指数表，每周期存储一次整体生态系统评分快照
		// 用于仪表盘的全局趋势图表展示
		`CREATE TABLE IF NOT EXISTS global_index (
			timestamp DATETIME PRIMARY KEY,            -- 指数计算时间（主键，每个时间点一条记录）
			global_score INTEGER NOT NULL,             -- 全局健康评分（0-100）
			models_count INTEGER,                      -- 参与评分的模型数量
			trend TEXT,                                -- 整体趋势方向
			performing_well INTEGER,                   -- 表现良好的模型数量
			total_models INTEGER                       -- 所有模型总数
		);`,

		// provider_reliability: 提供商可靠性评估表，记录各 AI 服务提供商的运行状况
		// 用于仪表盘的提供商信誉面板
		`CREATE TABLE IF NOT EXISTS provider_reliability (
			provider TEXT PRIMARY KEY,                 -- 提供商名称（主键）
			trust_score INTEGER,                       -- 信任分数（0-100）
			total_incidents INTEGER,                   -- 总事故数
			incidents_per_month INTEGER,               -- 月均事故数
			avg_recovery_hours TEXT,                   -- 平均恢复时间（如 "2.5 hours"）
			last_incident DATETIME,                    -- 最近一次事故时间
			trend TEXT,                                -- 可靠性趋势（"improving", "stable", "declining"）
			active_models INTEGER,                     -- 该提供商活跃模型数量
			top_performers INTEGER,                    -- 进入排行榜前列的模型数
			is_available INTEGER DEFAULT 1,            -- 服务是否可用（0=不可用, 1=可用）
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP  -- 本条记录的最后更新时间
		);`,

		// recommendations: 模型推荐表，按使用场景缓存最优模型推荐
		// 由同步逻辑自动计算，为前端提供即时的模型选型建议
		`CREATE TABLE IF NOT EXISTS recommendations (
			type TEXT PRIMARY KEY,                     -- 推荐类型/场景（如 "best_overall", "best_coding", "best_reasoning"）
			model_id TEXT,                             -- 推荐模型 ID
			model_name TEXT,                           -- 推荐模型名称
			vendor TEXT,                               -- 厂商名称
			score INTEGER,                             -- 推荐模型的评分
			reason TEXT,                               -- 推荐理由（人类可读的描述）
			evidence TEXT,                             -- 支持证据（如分数对比数据）
			extra_data TEXT,                           -- 额外的结构化数据（JSON 格式，用于扩展信息）
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP  -- 推荐的最后更新时间
		);`,

		// transparency: 透明度/测试覆盖概览表，记录测试套件的整体运行状况
		// 使用 CHECK (id = 1) 约束保证表中只有一条记录（单行配置表模式）
		`CREATE TABLE IF NOT EXISTS transparency (
			id INTEGER PRIMARY KEY CHECK (id = 1),    -- 固定 ID=1，保证单行记录
			last_update DATETIME,                      -- 最近一次测试更新时间
			total_tests INTEGER,                       -- 测试总数
			passed_tests INTEGER,                      -- 通过测试数
			coverage INTEGER,                          -- 测试覆盖率百分比
			confidence INTEGER,                        -- 置信度评分
			data_points_24h INTEGER,                   -- 过去 24 小时的数据点数
			next_test DATETIME,                        -- 下次计划测试时间
			models_fresh INTEGER,                      -- 数据新鲜的模型数
			models_stale INTEGER,                      -- 数据过期的模型数
			models_offline INTEGER,                    -- 离线模型的模型数
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		// model_freshness: 模型数据新鲜度表，记录每个模型最近一次评分更新的时间
		// 用于判断哪些模型的数据已过期，辅助决策是否需要重新同步
		`CREATE TABLE IF NOT EXISTS model_freshness (
			model_name TEXT PRIMARY KEY,               -- 模型名称（主键）
			last_update DATETIME,                      -- 最近一次数据更新时间
			minutes_ago INTEGER,                       -- 距现在多少分钟前更新（缓存值，减少实时计算）
			status TEXT                                -- 新鲜度状态（"fresh", "stale", "offline"）
		);`,

		// idx_scores_history_model: 按 model_id 查询 scores_history 的索引
		// 模型详情页需要加载单个模型的所有历史分数，无此索引将导致全表扫描
		`CREATE INDEX IF NOT EXISTS idx_scores_history_model ON scores_history(model_id);`,
		// idx_scores_history_timestamp: 按 timestamp 排序或过滤 scores_history 的索引
		// 趋势图表需要按时间排序展示数据，且同步时会按时间范围裁剪过期数据，此索引加速这些操作
		`CREATE INDEX IF NOT EXISTS idx_scores_history_timestamp ON scores_history(timestamp);`,
	}

	for _, schema := range schemas {
		_, err = DB.Exec(schema)
		if err != nil {
			return err
		}
	}

	return nil
}

// CloseDB 安全关闭数据库连接，释放资源。
// 在应用退出时调用，确保所有未完成的事务被清理。
func CloseDB() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
