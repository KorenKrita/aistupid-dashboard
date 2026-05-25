import { useEffect, useState, useMemo, useRef, useLayoutEffect, useCallback } from 'react';
import ReactECharts from 'echarts-for-react';
import {
  Sun, Moon, RefreshCw, Activity, TrendingUp,
  AlertTriangle, Shield, CheckCircle2, Zap, Clock, EyeOff, BarChart3,
  Gauge, Users, Server, FileWarning, Award, Target, Sparkles, XCircle
} from 'lucide-react';

// ============================================================
// 类型定义 - 描述后端 API 返回的数据结构
// ============================================================

/** 模型元数据：名称、厂商、推理类型、状态等基本信息 */
interface Model {
  id: string;            // 模型唯一标识（如 claude-opus-4-6）
  name: string;          // 模型展示名称（如 Claude Opus 4.6）
  provider: string;      // 提供商 key（如 anthropic、openai）
  vendor: string;        // 供应商全称（如 Anthropic）
  isReasoning: boolean;  // 是否为推理模型（支持思考过程）
  isNew: boolean;        // 是否为新上线模型
  isStale: boolean;      // 数据是否已过期
  status: string;        // 当前状态（active / degraded / down）
  standardError: number; // 评分标准误差
}

/** 模型最新评分：综合得分、置信区间、各维度轴评分 */
interface Score {
  modelId: string;                        // 对应 Model.id
  modelName: string;                      // 模型展示名称
  provider: string;                       // 提供商 key
  score: number;                          // 综合得分（0-100）
  trend: string;                          // 趋势方向：up / down / stable
  confidenceLower: number;                // 95% 置信区间下限
  confidenceUpper: number;                // 95% 置信区间上限
  standardError: number;                  // 标准误差
  timestamp: string;                      // 评分时间戳
  axes: Record<string, number | null>;    // 各维度评分（13个轴，值为 0-1 或 null）
}

/** 历史评分数据点：用于趋势折线图 */
interface HistoryPoint {
  modelId: string;                        // 模型 ID
  modelName: string;                      // 模型名称
  score: number;                          // 该时间点的综合得分
  timestamp: string;                      // 数据采集时间
  suite: string;                          // 数据来源：'current' / 'historyMap'
  axes: Record<string, number | null>;    // 各维度评分快照
}

/** 性能退化记录：当前评分相比基线显著下降 */
interface Degradation {
  modelId: string;        // 模型 ID
  modelName: string;      // 模型名称
  provider: string;       // 提供商 key
  currentScore: number;   // 当前评分
  baselineScore: number;  // 基线评分（退化前正常值）
  dropPercentage: number; // 下降百分比
  zScore: string;         // Z 分数（统计显著性）
  severity: string;       // 严重程度：critical / major / warning / minor
  detectedAt: string;     // 检测时间
  message: string;        // 退化描述
  type: string;           // 退化类型
}

/** 系统警报：服务异常、超时、可用性问题 */
interface Alert {
  modelName: string;   // 模型名称
  provider: string;    // 提供商 key
  issue: string;       // 问题描述
  severity: string;    // 严重程度：critical / warning / minor
  detectedAt: string;  // 检测时间
}

/** 全局智能指数：整体生态健康度评分时间序列 */
interface GlobalIndex {
  timestamp: string;    // 数据时间点
  globalScore: number;  // 全局综合指数（0-100）
  modelsCount: number;  // 参与计算的模型数
  trend: string;        // 趋势方向
  performingWell: number; // 表现良好的模型数
  totalModels: number;  // 总模型数
}

/** 厂商可靠性数据：信任评分、事故统计、可用性 */
interface ProviderReliability {
  provider: string;             // 提供商 key
  trustScore: number;           // 信任评分（0-100）
  totalIncidents: number;       // 总事故次数
  incidentsPerMonth: number;    // 月均事故数
  avgRecoveryHours: string;     // 平均恢复时间（小时）
  lastIncident: string;         // 最近事故时间
  trend: string;                // 趋势：improving / moderate / unreliable
  activeModels: number;         // 活跃模型数
  topPerformers: number;        // 顶级表现者数
  isAvailable: boolean;         // 当前是否在线可用
}

/** 智能推荐条目：最佳编程 / 最可靠 / 最快响应 / 应避免 */
interface Recommendation {
  type: string;         // 推荐类型：best_for_code / most_reliable / fastest_response / avoid_now
  modelId: string;      // 模型 ID
  modelName: string;    // 模型名称
  vendor: string;       // 供应商
  score: number;        // 对应评分
  reason: string;       // 推荐理由
  evidence: string;     // 数据依据
  extraData?: string;   // 额外数据（如 avoid_list 的 JSON 序列化）
}

/** 同步状态：数据上次同步和下次同步时间 */
interface SyncStatus {
  lastSync: string;  // 上次同步时间
  nextSync: string;  // 下次同步时间
}

/** 13 个测试维度的中英文映射表，用于图表轴标签和详情面板 */
const AXES_ZH: Record<string, string> = {
  correctness: '正确性',
  complexity: '复杂度',
  codeQuality: '代码质量',
  efficiency: '效率',
  stability: '稳定性',
  edgeCases: '边界情况',
  debugging: '调试能力',
  format: '格式规范',
  safety: '安全性',
  memoryRetention: '记忆保持',
  hallucinationRate: '幻觉率',
  planCoherence: '规划连贯',
  contextWindow: '上下文窗口'
};

/** 厂商名称中英文映射 */
const PROVIDER_ZH: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  xai: 'xAI',
  kimi: 'Kimi',
  glm: 'GLM',
  deepseek: 'DeepSeek'
};

/** 警报严重程度中英文映射 */
const SEVERITY_ZH: Record<string, string> = {
  critical: '严重',
  major: '重要',
  warning: '警告',
  minor: '轻微'
};

/** 时间范围选择器选项：value 为 API 参数，label 为展示文字 */
const PERIOD_OPTIONS = [
  { value: 'latest', label: '最新' },
  { value: '24h', label: '24小时' },
  { value: '7d', label: '7天' },
  { value: '14d', label: '14天' },
  { value: '30d', label: '30天' }
];

/** 模型颜色调色板（20 色循环使用）用于柱状图、雷达图、标签区分 */
const MODEL_COLORS = [
  '#2563eb', '#dc2626', '#16a34a', '#ca8a04', '#9333ea',
  '#0891b2', '#e11d48', '#65a30d', '#ea580c', '#4f46e5',
  '#0d9488', '#be185d', '#a16207', '#0284c7', '#7c3aed',
  '#059669', '#b91c1c', '#c026d3', '#475569', '#15803d'
];

/**
 * AIStupid 监控中心 - 主应用组件
 *
 * 功能概述：
 * - 展示 AI 模型的性能评分、历史趋势、退化警报和厂商可靠性
 * - 支持雷达图对比、历史趋势查看、模型筛选和排序
 * - 自动定时同步数据，支持手动触发同步
 *
 * 数据流：
 * 1. 组件挂载时通过 fetchAll 加载所有初始数据
 * 2. 每 5 秒轮询 /api/config 获取阻塞列表
 * 3. 每 45 秒轮询 /api/sync-status；lastSync 变化时自动 refreshAfterBackendSync
 * 4. 标签页重新可见时立即检查 sync-status；nextSync 后 60 秒兜底检查一次
 * 5. period 变化时通过 fetchHistory 加载历史评分
 * 6. 点击模型通过 fetchModelHistory 加载单个模型近30天历史
 * 7. useMemo 对原始数据做过滤、排序和图表配置派生
 */

/** 轮询 sync-status 的间隔（毫秒） */
const SYNC_POLL_INTERVAL_MS = 45_000;
/** nextSync 之后的兜底检查延迟（毫秒） */
const SYNC_FALLBACK_DELAY_MS = 60_000;
export default function App() {
  // 主题切换状态：从 localStorage 恢复，或跟随系统偏好
  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    const saved = localStorage.getItem('theme');
    if (saved) return saved as 'light' | 'dark';
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  });

  // ---------- 远程数据状态 ----------
  // 模型元数据列表（id、名称、供应商、推理类型等）
  const [models, setModels] = useState<Model[]>([]);
  // 各模型最新评分及轴数据（用于排行榜和雷达图）
  const [scores, setScores] = useState<Score[]>([]);
  // 历史评分时间序列（用于趋势折线图）
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  // 性能退化记录（当前 vs 基线）
  const [degradations, setDegradations] = useState<Degradation[]>([]);
  // 系统警报列表
  const [alerts, setAlerts] = useState<Alert[]>([]);
  // 全局智能指数时间序列
  const [globalIndex, setGlobalIndex] = useState<GlobalIndex[]>([]);
  // 各厂商可靠性数据
  const [providerReliability, setProviderReliability] = useState<ProviderReliability[]>([]);
  // 智能推荐列表（最佳编程、最可靠、最快响应、应避免）
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  // 同步状态（上次/下次同步时间）
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);
  // 前端数据最近一次刷新时间（用于 header 展示）
  const [dataUpdatedAt, setDataUpdatedAt] = useState<Date | null>(null);

  // ---------- UI 交互状态 ----------
  // 时间范围选择：latest / 24h / 7d / 14d / 30d
  const [period, setPeriod] = useState('latest');
  // 概览页右侧选中的模型 ID（展开雷达图详情）
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  // 历史趋势图中可见的模型 ID 列表
  const [visibleModels, setVisibleModels] = useState<string[]>([]);
  // 被阻塞（隐藏）的模型 ID 列表
  const [blockedModels, setBlockedModels] = useState<string[]>([]);
  // 搜索关键字（过滤模型列表）
  const [searchQuery, setSearchQuery] = useState('');
  // 当前激活的标签页：概览 / 模型详情 / 警报 / 厂商
  const [activeTab, setActiveTab] = useState<'overview' | 'models' | 'alerts' | 'providers'>('overview');
  // 手动同步进行中标志（控制按钮旋转动画）
  const [isSyncing, setIsSyncing] = useState(false);
  // 历史趋势数据加载中标志
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  // 模型对比列表中选中的模型 ID 列表（最多3个）
  const [compareModels, setCompareModels] = useState<string[]>([]);
  // 模型列表页排序字段：score 或轴名称
  const [sortBy, setSortBy] = useState<string>('score');
  // 模型详情弹窗中的模型 ID
  const [detailModel, setDetailModel] = useState<string | null>(null);
  // 模型详情弹窗中的图表维度：score 或具体轴名称
  const [detailAxis, setDetailAxis] = useState<string>('score');
  // 模型详情弹窗中的历史数据
  const [modelHistory, setModelHistory] = useState<HistoryPoint[]>([]);
  // 模型详情历史数据加载中标志
  const [isLoadingModelHistory, setIsLoadingModelHistory] = useState(false);
  // 左侧列高度（用于两列等高对齐）
  const [leftColHeight, setLeftColHeight] = useState<number | null>(null);
  // 左侧列 DOM 引用（用于 ResizeObserver 测量高度）
  const leftColRef = useRef<HTMLDivElement>(null);
  // 已知的后端 lastSync，用于检测定时同步是否完成
  const knownLastSyncRef = useRef<string | null>(null);
  // 详情弹窗中的模型 ID（供轮询回调读取最新值）
  const detailModelRef = useRef<string | null>(null);
  // 模型详情历史请求的 AbortController 引用
  const modelHistoryAbortRef = useRef<AbortController | null>(null);

  // 使用 ResizeObserver 监听左侧列高度变化，同步设置右侧列高度以实现两列等高布局
  // 依赖：activeTab === 'overview' 时才生效，scores/period 变化时重新测量
  useLayoutEffect(() => {
    if (leftColRef.current && activeTab === 'overview') {
      const updateHeight = () => {
        if (leftColRef.current) {
          setLeftColHeight(leftColRef.current.offsetHeight);
        }
      };
      updateHeight();
      const observer = new ResizeObserver(updateHeight);
      observer.observe(leftColRef.current);
      // 清理：组件卸载或依赖变化时断开 observer
      return () => observer.disconnect();
    }
  }, [activeTab, scores.length, period]);

  // 按名称 + 版本号对模型排序：先按前缀字母序，再按版本号降序
  // 例如：kimi-k2.5 排在 kimi-k2.4 之前，gpt-4 排在 gpt-3.5 之前
  const sortModelName = (a: string, b: string): number => {
    const extractParts = (name: string) => {
      // Handle patterns like: kimi-k2.5, gpt-5.4, claude-opus-4-6
      // Extract all numbers from the name for version comparison
      const numbers = name.match(/[\d.]+/g);
      const lastNumber = numbers && numbers.length > 0 ? parseFloat(numbers[numbers.length - 1]) : 0;
      // Get prefix by removing the last number pattern
      const prefix = name.replace(/[\d.]+$/, '').replace(/-$/, '').toLowerCase();
      return { prefix, version: lastNumber };
    };
    const pa = extractParts(a);
    const pb = extractParts(b);
    if (pa.prefix !== pb.prefix) return pa.prefix.localeCompare(pb.prefix);
    return pb.version - pa.version;
  };

  const toggleTheme = () => {
    // 亮/暗主题切换：更新 state、切换 html 根元素 class、持久化到 localStorage
    const next = theme === 'light' ? 'dark' : 'light';
    setTheme(next);
    document.documentElement.classList.toggle('dark', next === 'dark');
    localStorage.setItem('theme', next);
  };

  // 初始化：根据当前 theme 值设置 html 根元素的 dark class
  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
  }, []);

  // 首次加载所有数据：并行请求 9 个 API 端点，逐个安全解析 JSON 并更新状态
  // 通过 AbortSignal 支持取消请求（组件卸载时自动中止）
  const fetchAll = useCallback(async (signal?: AbortSignal) => {
    try {
      const responses = await Promise.all([
        fetch('/api/models', { signal }),
        fetch('/api/scores?period=latest', { signal }),
        fetch('/api/degradations', { signal }),
        fetch('/api/alerts', { signal }),
        fetch('/api/global-index', { signal }),
        fetch('/api/provider-reliability', { signal }),
        fetch('/api/recommendations', { signal }),
        fetch('/api/sync-status', { signal }),
        fetch('/api/config', { signal })
      ]);
      const [modelsRes, latestScoresRes, degradRes, alertsRes, globalRes, provRes, recRes, syncRes, configRes] = responses;

      const safeJson = async (res: Response) => res.ok ? res.json() : null;

      const [modelsData, scoresData, degradData, alertsData, globalData, provData, recData, syncData, configData] = await Promise.all([
        safeJson(modelsRes), safeJson(latestScoresRes), safeJson(degradRes),
        safeJson(alertsRes), safeJson(globalRes), safeJson(provRes),
        safeJson(recRes), safeJson(syncRes), safeJson(configRes)
      ]);

      if (modelsData) setModels(modelsData);
      if (scoresData) setScores(scoresData);
      if (degradData) setDegradations(degradData);
      if (alertsData) setAlerts(alertsData);
      if (globalData) setGlobalIndex(globalData);
      if (provData) setProviderReliability(provData);
      if (recData) setRecommendations(recData);
      if (syncData) {
        setSyncStatus(syncData);
        knownLastSyncRef.current = syncData.lastSync;
      }
      if (configData) setBlockedModels(configData.blocked_models || []);
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return;
      console.error('Fetch error:', e);
    }
  }, []);

  // 根据 period 参数加载历史评分数据
  // period === 'latest' 时不加载，直接清空历史状态
  const fetchHistory = useCallback(async (signal?: AbortSignal) => {
    if (period === 'latest') {
      setHistory([]);
      return;
    }
    setIsLoadingHistory(true);
    try {
      const res = await fetch(`/api/scores?period=${period}`, { signal });
      setHistory(await res.json());
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return;
      console.error('Fetch history error:', e);
    } finally {
      setIsLoadingHistory(false);
    }
  }, [period]);

  /**
   * 获取指定模型近 30 天历史评分数据
   * 使用 AbortController 实现：新请求发起时自动中止上一次未完成的请求
   */
  const fetchModelHistory = useCallback(async (modelId: string) => {
    if (modelHistoryAbortRef.current) {
      modelHistoryAbortRef.current.abort();
    }
    const controller = new AbortController();
    modelHistoryAbortRef.current = controller;
    setIsLoadingModelHistory(true);
    try {
      const res = await fetch(`/api/model/history?id=${modelId}&days=30`, { signal: controller.signal });
      const data = await res.json();
      if (!controller.signal.aborted) {
        setModelHistory(data);
      }
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return;
      console.error('Fetch model history error:', e);
      setModelHistory([]);
    } finally {
      if (!controller.signal.aborted) {
        setIsLoadingModelHistory(false);
      }
    }
  }, []);

  // 后端同步完成后刷新仪表盘数据（含详情弹窗历史）
  const refreshAfterBackendSync = useCallback(async (signal?: AbortSignal) => {
    await fetchAll(signal);
    await fetchHistory(signal);
    if (detailModelRef.current) {
      await fetchModelHistory(detailModelRef.current);
    }
    setDataUpdatedAt(new Date());
  }, [fetchAll, fetchHistory, fetchModelHistory]);

  useEffect(() => {
    detailModelRef.current = detailModel;
  }, [detailModel]);

  // 组件挂载时触发首次数据加载，并启动 /api/config 轮询（每 5 秒）
  // 使用 AbortController 管理生命周期：组件卸载时中止所有未完成的请求
  useEffect(() => {
    const controller = new AbortController();
    void fetchAll(controller.signal).then(() => setDataUpdatedAt(new Date()));
    const configInterval = setInterval(async () => {
      try {
        const res = await fetch('/api/config', { signal: controller.signal });
        const data = await res.json();
        setBlockedModels(data.blocked_models || []);
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return;
        console.error('Config poll error:', e);
      }
    }, 5000);
    return () => {
      // 清理：中止请求 + 清除定时器
      controller.abort();
      clearInterval(configInterval);
    };
  }, [fetchAll]);

  // 轮询 sync-status：lastSync 变化时自动刷新；标签页重新可见时立即检查
  useEffect(() => {
    const controller = new AbortController();

    const pollSyncStatus = async () => {
      try {
        const res = await fetch('/api/sync-status', { signal: controller.signal });
        if (!res.ok) return;
        const data: SyncStatus = await res.json();
        setSyncStatus(data);
        const prev = knownLastSyncRef.current;
        if (prev !== null && data.lastSync !== prev) {
          await refreshAfterBackendSync(controller.signal);
        } else {
          knownLastSyncRef.current = data.lastSync;
        }
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return;
        console.error('Sync status poll error:', e);
      }
    };

    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        void pollSyncStatus();
      }
    };

    const interval = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void pollSyncStatus();
      }
    }, SYNC_POLL_INTERVAL_MS);

    document.addEventListener('visibilitychange', onVisibilityChange);

    return () => {
      controller.abort();
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [refreshAfterBackendSync]);

  // nextSync 后兜底检查一次（防止轮询与后台同步时间错位）
  useEffect(() => {
    if (!syncStatus?.nextSync) return;
    const due = new Date(syncStatus.nextSync).getTime() + SYNC_FALLBACK_DELAY_MS - Date.now();
    if (due <= 0 || due > 20 * 60 * 1000) return;

    const controller = new AbortController();
    const timer = setTimeout(() => {
      if (document.visibilityState !== 'visible') return;
      void (async () => {
        try {
          const res = await fetch('/api/sync-status', { signal: controller.signal });
          if (!res.ok) return;
          const data: SyncStatus = await res.json();
          setSyncStatus(data);
          const prev = knownLastSyncRef.current;
          if (prev !== null && data.lastSync !== prev) {
            await refreshAfterBackendSync(controller.signal);
          } else {
            knownLastSyncRef.current = data.lastSync;
          }
        } catch (e) {
          if (e instanceof DOMException && e.name === 'AbortError') return;
          console.error('Sync fallback check error:', e);
        }
      })();
    }, due);

    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [syncStatus?.nextSync, refreshAfterBackendSync]);

  // period 变化时重新加载历史数据，并使用 AbortController 取消上一次未完成的请求
  useEffect(() => {
    const controller = new AbortController();
    fetchHistory(controller.signal);
    return () => controller.abort();
  }, [fetchHistory]);

  // 手动触发数据同步：POST /api/sync-now，同步完成后刷新所有数据
  const triggerSync = async () => {
    setIsSyncing(true);
    try {
      await fetch('/api/sync-now', { method: 'POST' });
      await refreshAfterBackendSync();
    } catch { alert('同步失败'); }
    setIsSyncing(false);
  };

  // ---------- 派生数据：过滤 & 排序 ----------
  // 过滤阻塞的模型 + 按搜索关键字过滤 + 按名称+版本排序
  // 依赖：models / blockedModels / searchQuery 任一变化时重新计算
  const filteredModels = useMemo(() => {
    return models
      .filter(m => !blockedModels.includes(m.id))
      .filter(m => m.name.toLowerCase().includes(searchQuery.toLowerCase()))
      .sort((a, b) => sortModelName(a.name, b.name));
  }, [models, blockedModels, searchQuery]);

  // 过滤阻塞模型 + 搜索关键字 + 按 sortBy 排序
  // sortBy === 'score' 时按综合得分降序，否则按对应轴值降序
  const filteredScores = useMemo(() => {
    const filtered = scores
      .filter(s => !blockedModels.includes(s.modelId))
      .filter(s => s.modelName.toLowerCase().includes(searchQuery.toLowerCase()));
    if (sortBy === 'score') {
      return filtered.sort((a, b) => b.score - a.score);
    }
    return filtered.sort((a, b) => {
      const va = a.axes[sortBy] ?? 0;
      const vb = b.axes[sortBy] ?? 0;
      return (vb as number) - (va as number);
    });
  }, [scores, blockedModels, searchQuery, sortBy]);

  // 历史数据过滤：只保留可见模型 + 未被阻塞的模型
  // 依赖：history / blockedModels / visibleModels 任一变化时重新计算
  const filteredHistory = useMemo(() => {
    return history.filter(h =>
      !blockedModels.includes(h.modelId) &&
      visibleModels.includes(h.modelId)
    );
  }, [history, blockedModels, visibleModels]);

  // 性能退化警报过滤：只保留当前可见且未被阻塞的模型
  // 依赖：degradations / blockedModels / filteredScores（派生）任一变化时重新计算
  const filteredDegradations = useMemo(() => {
    // 从已过滤评分列表中提取可见模型 ID 集合，作为退化列表的白名单
    const visibleModelIds = new Set(filteredScores.map(s => s.modelId));
    return degradations.filter(d => !blockedModels.includes(d.modelId) && visibleModelIds.has(d.modelId));
  }, [degradations, blockedModels, filteredScores]);

  // 系统警报过滤：只保留当前可见且未被阻塞的模型
  // 依赖：alerts / models / blockedModels / filteredScores 任一变化时重新计算
  const filteredAlerts = useMemo(() => {
    const visibleNames = new Set(filteredScores.map(s => s.modelName));
    const blockedNames = models.filter(m => blockedModels.includes(m.id)).map(m => m.name);
    return alerts.filter(a => !blockedNames.includes(a.modelName) && visibleNames.has(a.modelName));
  }, [alerts, models, blockedModels, filteredScores]);

  // 如果当前详情模型被过滤掉（阻塞或搜索筛除），自动关闭详情弹窗并清空历史数据
  useEffect(() => {
    if (detailModel && !filteredScores.find(s => s.modelId === detailModel)) {
      setDetailModel(null);
      setModelHistory([]);
    }
  }, [detailModel, filteredScores]);

  /** 根据模型 ID 获取固定颜色（按 models 数组中索引模 20 取色） */
  const getModelColor = (modelId: string) => {
    const idx = models.findIndex(m => m.id === modelId);
    if (idx < 0) return MODEL_COLORS[0];
    return MODEL_COLORS[idx % MODEL_COLORS.length];
  };

  /**
   * 历史趋势折线图 ECharts 配置
   * 将 filteredHistory 按 modelId 分组，每个模型生成一条折线 series
   * x 轴为时间，y 轴为综合得分（0-100），支持 Tooltip 显示多模型对比
   * 依赖：filteredHistory / models / theme 任一变化时重新生成配置对象
   */
  const historyChartOptions = useMemo(() => {
    const isDark = theme === 'dark';
    const modelIds = [...new Set(filteredHistory.map(h => h.modelId))];

    const series = modelIds.map(modelId => {
      const modelData = filteredHistory.filter(h => h.modelId === modelId);
      const model = models.find(m => m.id === modelId);
      const idx = models.findIndex(m => m.id === modelId);
      const color = idx < 0 ? MODEL_COLORS[0] : MODEL_COLORS[idx % MODEL_COLORS.length];

      return {
        name: model?.name || modelId,
        type: 'line',
        showSymbol: false,
        smooth: 0.3,
        lineStyle: { width: 2.5, color },
        itemStyle: { color },
        data: modelData.map(pt => [new Date(pt.timestamp).getTime(), pt.score])
      };
    });

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        backgroundColor: isDark ? '#1e293b' : '#ffffff',
        borderColor: isDark ? '#334155' : '#e2e8f0',
        textStyle: { color: isDark ? '#f1f5f9' : '#1e3a8a', fontSize: 12 },
        formatter: (params: Array<{ seriesName: string; value: [number, number]; color: string }>) => {
          if (!params.length) return '';
          const date = new Date(params[0].value[0]);
          const timeStr = `${date.getMonth() + 1}月${date.getDate()}日 ${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`;
          let html = `<div style="font-weight:600;margin-bottom:8px">${timeStr}</div>`;
          params.forEach(p => {
            html += `<div style="display:flex;align-items:center;gap:6px;margin:4px 0"><span style="width:10px;height:10px;border-radius:50%;background:${p.color}"></span><span>${p.seriesName}</span><span style="font-weight:600;margin-left:auto">${p.value[1]}</span></div>`;
          });
          return html;
        }
      },
      legend: { show: false },
      grid: { top: 20, bottom: 40, left: 50, right: 20 },
      xAxis: {
        type: 'time',
        splitLine: { show: true, lineStyle: { color: isDark ? '#334155' : '#f1f5f9', type: 'dashed' } },
        axisLine: { lineStyle: { color: isDark ? '#334155' : '#dbeafe' } },
        axisLabel: {
          color: isDark ? '#94a3b8' : '#64748b',
          fontSize: 11,
          formatter: (value: number) => {
            const d = new Date(value);
            const now = new Date();
            const diffDays = Math.floor((now.getTime() - d.getTime()) / (1000 * 60 * 60 * 24));
            if (diffDays < 1) {
              return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
            }
            return `${d.getMonth() + 1}/${d.getDate()}`;
          }
        }
      },
      yAxis: {
        type: 'value',
        min: (value: { min: number }) => Math.max(0, Math.floor(value.min * 0.9)),
        max: (value: { max: number }) => Math.min(100, Math.ceil(value.max * 1.05)),
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#f1f5f9', type: 'dashed' } },
        axisLine: { show: false },
        axisLabel: { color: isDark ? '#94a3b8' : '#64748b', fontSize: 11 }
      },
      series
    };
  }, [filteredHistory, models, theme]);

  /**
   * 单模型雷达图 ECharts 配置（用于右侧栏选中模型详情）
   * 过滤掉 4 个深度测试维度（memoryRetention / hallucinationRate / planCoherence / contextWindow）
   * 只显示核心 9 维度，值为 0-1 归一化数据，max=1
   */
  const getRadarChartOptions = (axes: Record<string, number | null>) => {
    const isDark = theme === 'dark';
    const deepTestKeys = ['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'];
    const indicators = Object.entries(AXES_ZH)
      .filter(([key]) => !deepTestKeys.includes(key) && axes[key] !== null && axes[key] !== undefined)
      .map(([_, name]) => ({ name, max: 1 }));

    const values = Object.entries(AXES_ZH)
      .filter(([key]) => !deepTestKeys.includes(key) && axes[key] !== null && axes[key] !== undefined)
      .map(([k]) => axes[k] as number);

    return {
      backgroundColor: 'transparent',
      radar: {
        indicator: indicators,
        shape: 'polygon',
        splitNumber: 4,
        axisName: { color: isDark ? '#94a3b8' : '#64748b', fontSize: 11 },
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#e2e8f0' } },
        splitArea: { show: false },
        axisLine: { lineStyle: { color: isDark ? '#334155' : '#e2e8f0' } }
      },
      series: [{
        type: 'radar',
        data: [{
          value: values,
          areaStyle: { color: 'rgba(59, 130, 246, 0.2)' },
          lineStyle: { color: '#3b82f6', width: 2 },
          itemStyle: { color: '#3b82f6' }
        }]
      }]
    };
  };

  /**
   * 全局智能指数趋势图 ECharts 配置
   * 将 globalIndex 按时间升序排列，绘制为带填充区域的平滑折线
   * y 轴为全局指数（0-100），x 轴为时间
   * 依赖：globalIndex / theme 任一变化时重新生成
   */
  const globalIndexChartOptions = useMemo(() => {
    const isDark = theme === 'dark';
    const sorted = [...globalIndex].sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());

    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis' },
      grid: { top: 10, bottom: 20, left: 40, right: 10 },
      xAxis: {
        type: 'time',
        splitLine: { show: false },
        axisLine: { show: false },
        axisLabel: {
          color: isDark ? '#94a3b8' : '#64748b',
          fontSize: 10,
          hideOverlap: true,
          formatter: (value: number) => {
            const d = new Date(value);
            return `${d.getHours()}:00`;
          }
        }
      },
      yAxis: {
        type: 'value',
        min: (value: { min: number }) => Math.max(0, Math.floor(value.min * 0.95)),
        max: (value: { max: number }) => Math.min(100, Math.ceil(value.max * 1.02)),
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#f1f5f9' } },
        axisLine: { show: false },
        axisLabel: { color: isDark ? '#94a3b8' : '#64748b', fontSize: 10 }
      },
      series: [{
        type: 'line',
        smooth: true,
        showSymbol: false,
        areaStyle: { color: 'rgba(59, 130, 246, 0.15)' },
        lineStyle: { color: '#3b82f6', width: 2 },
        data: sorted.map(g => [new Date(g.timestamp).getTime(), g.globalScore])
      }]
    };
  }, [globalIndex, theme]);

  // 当前选中模型的评分数据（用于右侧雷达图和详情面板），null 表示未选中
  const selectedModelData = selectedModel ? filteredScores.find(s => s.modelId === selectedModel) : null;

  /**
   * 模型得分柱状图 ECharts 配置（概览页最新数据展示）
   * 按综合得分降序排列，每根柱子使用 MODEL_COLORS 渐变着色
   * y 轴为得分（0-100），x 轴为模型名称（带 45 度旋转）
   * 依赖：filteredScores / theme 任一变化时重新生成
   */
  const latestBarChartOptions = useMemo(() => {
    const isDark = theme === 'dark';
    const sortedScores = [...filteredScores].sort((a, b) => b.score - a.score);

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        backgroundColor: isDark ? '#1e293b' : '#ffffff',
        borderColor: isDark ? '#334155' : '#e2e8f0',
        textStyle: { color: isDark ? '#f1f5f9' : '#1e3a8a' }
      },
      grid: { top: 20, bottom: 60, left: 50, right: 20 },
      xAxis: {
        type: 'category',
        data: sortedScores.map(s => s.modelName),
        axisLabel: { color: isDark ? '#94a3b8' : '#64748b', rotate: 45, fontSize: 10 },
        axisLine: { lineStyle: { color: isDark ? '#334155' : '#dbeafe' } }
      },
      yAxis: {
        type: 'value',
        min: (value: { min: number }) => Math.max(0, Math.floor(value.min * 0.9)),
        max: (value: { max: number }) => Math.min(100, Math.ceil(value.max * 1.05)),
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#f1f5f9', type: 'dashed' } },
        axisLine: { show: false },
        axisLabel: { color: isDark ? '#94a3b8' : '#64748b' }
      },
      series: [{
        type: 'bar',
        data: sortedScores.map((s, i) => ({
          value: s.score,
          itemStyle: {
            color: {
              type: 'linear',
              x: 0, y: 0, x2: 0, y2: 1,
              colorStops: [
                { offset: 0, color: MODEL_COLORS[i % MODEL_COLORS.length] },
                { offset: 1, color: `${MODEL_COLORS[i % MODEL_COLORS.length]}88` }
              ]
            },
            borderRadius: [4, 4, 0, 0]
          }
        })),
        barWidth: '60%'
      }]
    };
  }, [filteredScores, theme]);

  /**
   * 多模型对比雷达图 ECharts 配置（模型详情页对比区域）
   * 每个选中模型生成一条雷达数据系列，使用 MODEL_COLORS 区分
   * 同样过滤 4 个深度测试维度，最多支持 3 个模型同时对比
   * 模型数量不足 3 时右侧显示备选列表
   */
  const getCompareRadarOptions = () => {
    const isDark = theme === 'dark';
    const compareData = compareModels.map(id => filteredScores.find(s => s.modelId === id)).filter(Boolean) as Score[];
    if (compareData.length === 0) return null;

    const deepTestKeys = ['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'];
    const allAxes = Object.keys(AXES_ZH).filter(key => !deepTestKeys.includes(key));
    const indicators = allAxes.map(key => ({ name: AXES_ZH[key], max: 1 }));

    const seriesData = compareData.map((s, idx) => ({
      value: allAxes.map(k => s.axes[k] ?? 0),
      name: s.modelName,
      areaStyle: { color: `${MODEL_COLORS[idx]}22` },
      lineStyle: { color: MODEL_COLORS[idx], width: 2 },
      itemStyle: { color: MODEL_COLORS[idx] }
    }));

    return {
      backgroundColor: 'transparent',
      legend: {
        data: compareData.map(s => s.modelName),
        bottom: 0,
        textStyle: { color: isDark ? '#94a3b8' : '#64748b', fontSize: 11 }
      },
      radar: {
        indicator: indicators,
        shape: 'polygon',
        splitNumber: 4,
        radius: '65%',
        axisName: { color: isDark ? '#94a3b8' : '#64748b', fontSize: 10 },
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#e2e8f0' } },
        splitArea: { show: false },
        axisLine: { lineStyle: { color: isDark ? '#334155' : '#e2e8f0' } }
      },
      series: [{ type: 'radar', data: seriesData }]
    };
  };

  const toggleCompareModel = (modelId: string) => {
    setCompareModels(prev => {
      if (prev.includes(modelId)) {
        return prev.filter(id => id !== modelId);
      } else if (prev.length < 3) {
        return [...prev, modelId];
      }
      return prev;
    });
  };

  /**
   * 打开模型详情弹窗：设置目标模型 ID、重置维度为综合得分、发起历史数据请求
   */
  const openModelDetail = (modelId: string) => {
    setDetailModel(modelId);
    setDetailAxis('score');
    fetchModelHistory(modelId);
  };

  /**
   * 关闭模型详情弹窗：中止进行中的请求、清空 state、重置 AbortController
   */
  const closeModelDetail = () => {
    if (modelHistoryAbortRef.current) {
      modelHistoryAbortRef.current.abort();
      modelHistoryAbortRef.current = null;
    }
    setDetailModel(null);
    setModelHistory([]);
  };

  /**
   * 模型详情页历史趋势图 ECharts 配置
   * - detailAxis === 'score' 时展示综合得分趋势（0-100 原始值）
   * - detailAxis 为具体轴名称时展示该维度趋势（0-1 归一化值转换为百分比显示）
   * 数据源为 modelHistory（近 30 天），支持 Tooltip 显示具体数值
   */
  const getModelDetailChartOptions = () => {
    const isDark = theme === 'dark';
    if (modelHistory.length === 0) return null;

    const isAxisMode = detailAxis !== 'score';
    const data = modelHistory.map(pt => {
      const timestamp = new Date(pt.timestamp).getTime();
      if (isAxisMode) {
        const axisValue = pt.axes[detailAxis];
        return [timestamp, axisValue !== null && axisValue !== undefined ? (axisValue as number) * 100 : null];
      }
      return [timestamp, pt.score];
    }).filter(d => d[1] !== null);

    const axisLabel = isAxisMode ? AXES_ZH[detailAxis] || detailAxis : '综合得分';
    const color = '#3b82f6';

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        backgroundColor: isDark ? '#1e293b' : '#ffffff',
        borderColor: isDark ? '#334155' : '#e2e8f0',
        textStyle: { color: isDark ? '#f1f5f9' : '#1e3a8a', fontSize: 12 },
        formatter: (params: Array<{ value: [number, number] }>) => {
          if (!params.length) return '';
          const date = new Date(params[0].value[0]);
          const timeStr = `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`;
          const value = isAxisMode ? `${params[0].value[1].toFixed(1)}%` : params[0].value[1];
          return `<div style="font-weight:600;margin-bottom:4px">${timeStr}</div><div>${axisLabel}: <span style="font-weight:700">${value}</span></div>`;
        }
      },
      grid: { top: 20, bottom: 40, left: 50, right: 20 },
      xAxis: {
        type: 'time',
        splitLine: { show: true, lineStyle: { color: isDark ? '#334155' : '#f1f5f9', type: 'dashed' } },
        axisLine: { lineStyle: { color: isDark ? '#334155' : '#dbeafe' } },
        axisLabel: {
          color: isDark ? '#94a3b8' : '#64748b',
          fontSize: 11,
          formatter: (value: number) => {
            const d = new Date(value);
            return `${d.getMonth() + 1}/${d.getDate()}`;
          }
        }
      },
      yAxis: {
        type: 'value',
        min: (value: { min: number }) => Math.max(0, Math.floor(value.min - (isAxisMode ? 5 : value.min * 0.1))),
        max: (value: { max: number }) => Math.min(100, Math.ceil(value.max + (isAxisMode ? 5 : value.max * 0.05))),
        splitLine: { lineStyle: { color: isDark ? '#334155' : '#f1f5f9', type: 'dashed' } },
        axisLine: { show: false },
        axisLabel: {
          color: isDark ? '#94a3b8' : '#64748b',
          fontSize: 11,
          formatter: isAxisMode ? (v: number) => `${v}%` : undefined
        }
      },
      series: [{
        type: 'line',
        showSymbol: true,
        symbolSize: 6,
        smooth: 0.3,
        areaStyle: { color: `${color}15` },
        lineStyle: { width: 2.5, color },
        itemStyle: { color },
        data
      }]
    };
  };

  /**
   * 当前最新的全局智能指数（取 globalIndex 时间戳最大的一个）
   * 用于概览页头部统计卡片展示
   */
  const currentGlobal = useMemo(() => {
    if (globalIndex.length === 0) return null;
    return globalIndex.reduce((latest, g) =>
      new Date(g.timestamp).getTime() > new Date(latest.timestamp).getTime() ? g : latest
    );
  }, [globalIndex]);

  // 按类型提取推荐数据，用于右侧智能推荐面板展示
  const bestForCode = recommendations.find(r => r.type === 'best_for_code');
  const mostReliable = recommendations.find(r => r.type === 'most_reliable');
  const fastestResponse = recommendations.find(r => r.type === 'fastest_response');

  return (
    <div className="min-h-screen bg-bgApp text-textMain transition-colors duration-200">
      {/* ---------- 顶部导航栏 ---------- */}
      {/* 固定在页面顶部，包含 logo、版本号、同步状态、同步按钮、主题切换 */}
      <header className="sticky top-0 z-50 backdrop-blur-md bg-bgApp/90 border-b border-border">
        <div className="max-w-[1600px] mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-primary/10 flex items-center justify-center text-primary">
              <Activity size={18} />
            </div>
            <div>
              <span className="font-bold text-base">AIStupid 监控中心</span>
              <span className="text-[10px] text-textMuted ml-2 font-mono">v2.0</span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {syncStatus && (
              <div className="hidden md:flex items-center gap-3 text-[10px] text-textMuted mr-4">
                {dataUpdatedAt && (
                  <span>数据更新: {dataUpdatedAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</span>
                )}
                <span className="flex items-center gap-1">
                  <Clock size={12} />
                  下次同步: {new Date(syncStatus.nextSync).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}
                </span>
              </div>
            )}
            <button
              onClick={triggerSync}
              disabled={isSyncing}
              className="w-9 h-9 flex items-center justify-center border border-border rounded-lg bg-bgSurface text-textMuted hover:text-textMain transition-all cursor-pointer"
            >
              <RefreshCw size={16} className={isSyncing ? 'animate-spin' : ''} />
            </button>
            <button
              onClick={toggleTheme}
              className="w-9 h-9 flex items-center justify-center border border-border rounded-lg bg-bgSurface text-textMuted hover:text-textMain transition-all cursor-pointer"
            >
              {theme === 'light' ? <Moon size={16} /> : <Sun size={16} />}
            </button>
          </div>
        </div>
      </header>

      {/* ---------- 主内容区域 ---------- */}
      {/* 最大宽度 1600px，居中，内边距，垂直间距 */}
      <div className="max-w-[1600px] mx-auto px-4 py-6">
        {/* ---------- 概览卡片：全局指数、监控数、退化、警报 ---------- */}
        {/* 4 列网格（移动端 2 列），每卡片包含图标、标题、数值 */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          {/* 全局智能指数卡片 */}
          <div className="p-4 rounded-xl border border-border bg-bgSurface card-hover">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-primary/10 text-primary flex items-center justify-center flex-shrink-0">
                <Gauge size={20} />
              </div>
              <div>
                <div className="text-[10px] text-textMuted font-medium">全局智能指数</div>
                <div className="text-xl font-bold">{currentGlobal?.globalScore ?? '-'}<span className="text-sm font-normal text-textMuted ml-1">分</span></div>
              </div>
            </div>
          </div>

          {/* 监控模型数卡片 */}
          <div className="p-4 rounded-xl border border-border bg-bgSurface card-hover">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-success/10 text-success flex items-center justify-center flex-shrink-0">
                <Server size={20} />
              </div>
              <div>
                <div className="text-[10px] text-textMuted font-medium">监控模型数</div>
                <div className="text-xl font-bold">{filteredScores.length}<span className="text-sm font-normal text-textMuted ml-1">个</span></div>
              </div>
            </div>
          </div>

          {/* 性能退化警报卡片 */}
          <div className="p-4 rounded-xl border border-border bg-bgSurface card-hover">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-critical/10 text-critical flex items-center justify-center flex-shrink-0">
                <AlertTriangle size={20} />
              </div>
              <div>
                <div className="text-[10px] text-textMuted font-medium">性能退化警报</div>
                <div className="text-xl font-bold">{filteredDegradations.length}<span className="text-sm font-normal text-textMuted ml-1">次</span></div>
              </div>
            </div>
          </div>

          {/* 活跃警报卡片 */}
          <div className="p-4 rounded-xl border border-border bg-bgSurface card-hover">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-warning/10 text-warning flex items-center justify-center flex-shrink-0">
                <FileWarning size={20} />
              </div>
              <div>
                <div className="text-[10px] text-textMuted font-medium">活跃警报</div>
                <div className="text-xl font-bold">{filteredAlerts.length}<span className="text-sm font-normal text-textMuted ml-1">条</span></div>
              </div>
            </div>
          </div>
        </div>

        {/* ---------- 标签页导航：概览 / 模型详情 / 警报 / 厂商 ---------- */}
        {/* 底部边框，4 个标签，activeTab 高亮 */}
        <div className="flex items-center gap-1 mb-6 border-b border-border">
          {[
            { id: 'overview', label: '概览', icon: BarChart3 },
            { id: 'models', label: '模型详情', icon: Target },
            { id: 'alerts', label: '警报', icon: AlertTriangle },
            { id: 'providers', label: '厂商', icon: Users }
          ].map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id as typeof activeTab)}
              className={`flex items-center gap-2 px-4 py-3 text-sm font-medium transition-colors cursor-pointer ${
                activeTab === tab.id ? 'tab-active' : 'text-textMuted hover:text-textMain'
              }`}
            >
              <tab.icon size={16} />
              {tab.label}
            </button>
          ))}
        </div>

        {/* ---------- 概览标签页内容 ---------- */}
        {/* 两列布局：左侧 2/3（图表 + 排行榜），右侧 1/3（模型详情/推荐 + 全局趋势 + 退化警报） */}
        {activeTab === 'overview' && (
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
            {/* 左侧列：图表 + 排行榜 */}
            <div ref={leftColRef} className="lg:col-span-2 space-y-6">
              {/* 主图表卡片：period 决定展示柱状图或折线图 */}
              <div className="p-5 rounded-xl border border-border bg-bgSurface">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h2 className="text-base font-bold">{period === 'latest' ? '模型得分对比' : '性能历史趋势'}</h2>
                    <p className="text-[11px] text-textMuted">{period === 'latest' ? '当前各模型综合评分排名' : '模型能力波动走势'}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    {PERIOD_OPTIONS.map(opt => (
                      <button
                        key={opt.value}
                        onClick={() => setPeriod(opt.value)}
                        className={`px-3 py-1.5 text-xs font-medium rounded-lg transition-colors cursor-pointer ${
                          period === opt.value
                            ? 'bg-primary text-white'
                            : 'bg-bgApp text-textMuted hover:text-textMain'
                        }`}
                      >
                        {opt.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* 历史趋势模式下：模型选择器 */}
                {period !== 'latest' && (
                  <div className="mb-4 pb-4 border-b border-border/50">
                    <div className="flex items-center justify-between text-[10px] text-textMuted mb-2">
                      <span>选择展示的模型</span>
                      <div className="flex gap-2">
                        <button onClick={() => setVisibleModels(filteredModels.map(m => m.id))} className="text-primary hover:underline cursor-pointer">全选</button>
                        <button onClick={() => setVisibleModels([])} className="text-critical hover:underline cursor-pointer">清空</button>
                      </div>
                    </div>
                    <div className="flex flex-wrap gap-1.5 max-h-24 overflow-y-auto scrollbar-stable">
                      {filteredModels.map(m => {
                        const isVisible = visibleModels.includes(m.id);
                        const color = getModelColor(m.id);
                        return (
                          <button
                            key={m.id}
                            onClick={() => setVisibleModels(isVisible ? visibleModels.filter(id => id !== m.id) : [...visibleModels, m.id])}
                            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium border transition-all cursor-pointer ${isVisible ? 'opacity-100' : 'opacity-40'}`}
                            style={{
                              borderColor: isVisible ? color : 'var(--border-color)',
                              backgroundColor: isVisible ? `${color}15` : 'transparent',
                              color: isVisible ? color : 'var(--text-muted)'
                            }}
                          >
                            <span className="w-1.5 h-1.5 rounded-full" style={{ backgroundColor: isVisible ? color : '#9ca3af' }} />
                            {m.name}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}

                {/* 图表渲染区域：根据 period 和 loading 状态显示不同内容 */}
                <div className="h-72">
                  {period === 'latest' ? (
                    filteredScores.length > 0 ? (
                      <ReactECharts option={latestBarChartOptions} style={{ height: '100%', width: '100%' }} />
                    ) : (
                      <div className="flex items-center justify-center h-full text-textMuted text-sm">
                        <Activity className="animate-spin mr-2" size={16} /> 加载中...
                      </div>
                    )
                  ) : isLoadingHistory ? (
                    <div className="flex items-center justify-center h-full text-textMuted text-sm">
                      <Activity className="animate-spin mr-2" size={16} /> 加载历史数据...
                    </div>
                  ) : visibleModels.length === 0 ? (
                    <div className="flex items-center justify-center h-full text-textMuted text-sm">
                      请选择要展示的模型
                    </div>
                  ) : filteredHistory.length > 0 ? (
                    <ReactECharts option={historyChartOptions} style={{ height: '100%', width: '100%' }} notMerge={true} />
                  ) : (
                    <div className="flex items-center justify-center h-full text-textMuted text-sm">
                      所选模型暂无历史数据
                    </div>
                  )}
                </div>
              </div>

              {/* 排行榜表格：点击行选中模型，展示名称、厂商、得分、趋势、置信区间 */}
              <div className="p-5 rounded-xl border border-border bg-bgSurface">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h2 className="text-base font-bold">当前排行榜</h2>
                    <p className="text-[11px] text-textMuted">实时模型性能评分</p>
                  </div>
                  <input
                    type="text"
                    placeholder="搜索模型..."
                    value={searchQuery}
                    onChange={e => setSearchQuery(e.target.value)}
                    className="px-3 py-1.5 text-xs bg-bgApp border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/20 w-40"
                  />
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left">
                    <thead>
                      <tr className="border-b border-border text-[10px] text-textMuted uppercase tracking-wider">
                        <th className="pb-2 pr-4">模型</th>
                        <th className="pb-2 pr-4">厂商</th>
                        <th className="pb-2 pr-4">得分</th>
                        <th className="pb-2 pr-4">趋势</th>
                        <th className="pb-2 text-right">置信区间</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-border/50">
                      {filteredScores.map(s => (
                        <tr key={s.modelId} onClick={() => setSelectedModel(selectedModel === s.modelId ? null : s.modelId)} className="hover:bg-bgApp/50 transition-colors cursor-pointer">
                          <td className="py-2.5 pr-4"><span className="font-mono text-sm font-medium">{s.modelName}</span></td>
                          <td className="py-2.5 pr-4 text-sm text-textMuted">{PROVIDER_ZH[s.provider] || s.provider}</td>
                          <td className="py-2.5 pr-4"><span className="font-bold text-sm">{s.score}</span></td>
                          <td className="py-2.5 pr-4">
                            <span className={`text-xs font-medium ${s.trend === 'up' ? 'text-success' : s.trend === 'down' ? 'text-critical' : 'text-textMuted'}`}>
                              {s.trend === 'up' ? '↑ 上升' : s.trend === 'down' ? '↓ 下降' : '→ 稳定'}
                            </span>
                          </td>
                          <td className="py-2.5 text-right"><span className="font-mono text-[11px] text-textMuted">[{s.confidenceLower.toFixed(1)} - {s.confidenceUpper.toFixed(1)}]</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>

            {/* 右侧列：模型详情/智能推荐 + 全局趋势 + 退化警报 */}
            <div className="flex flex-col gap-6" style={leftColHeight ? { height: leftColHeight } : undefined}>
              {/* 选中模型详情卡片：雷达图 + 6 个核心维度小卡片 */}
              {selectedModelData ? (
                <div className="p-5 rounded-xl border border-border bg-bgSurface animate-fadeIn">
                  <div className="flex items-center justify-between mb-4">
                    <div>
                      <h3 className="font-bold text-sm">{selectedModelData.modelName}</h3>
                      <p className="text-[10px] text-textMuted">{PROVIDER_ZH[selectedModelData.provider] || selectedModelData.provider}</p>
                    </div>
                    <button onClick={() => setSelectedModel(null)} className="text-textMuted hover:text-textMain cursor-pointer"><EyeOff size={16} /></button>
                  </div>
                  <div className="text-center mb-4">
                    <div className="text-3xl font-bold text-primary">{selectedModelData.score}</div>
                    <div className="text-[10px] text-textMuted">综合得分</div>
                  </div>
                  <div className="h-52">
                    <ReactECharts option={getRadarChartOptions(selectedModelData.axes)} style={{ height: '100%', width: '100%' }} />
                  </div>
                  <div className="mt-4 grid grid-cols-3 gap-2 text-center">
                    {Object.entries(selectedModelData.axes)
                      .filter(([key, v]) => v !== null && !['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'].includes(key))
                      .slice(0, 6).map(([key, value]) => (
                      <div key={key} className="p-2 rounded-lg bg-bgApp">
                        <div className="text-xs font-bold">{((value as number) * 100).toFixed(0)}%</div>
                        <div className="text-[9px] text-textMuted">{AXES_ZH[key] || key}</div>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                /* 未选中模型时：智能推荐面板 */
                <div className="p-5 rounded-xl border border-border bg-bgSurface">
                  <h3 className="font-bold text-sm mb-4 flex items-center gap-2"><Sparkles size={16} className="text-accent" />智能推荐</h3>
                  <div className="space-y-3">
                    {bestForCode && bestForCode.modelName && (
                      <div className="p-3 rounded-lg bg-bgApp border border-border/50">
                        <div className="flex items-center gap-2 mb-1"><Award size={14} className="text-primary" /><span className="text-[10px] text-textMuted">最佳编程</span></div>
                        <div className="font-medium text-sm">{bestForCode.modelName}</div>
                        <div className="text-[10px] text-textMuted mt-1 line-clamp-2">{bestForCode.reason}</div>
                      </div>
                    )}
                    {mostReliable && mostReliable.modelName && (
                      <div className="p-3 rounded-lg bg-bgApp border border-border/50">
                        <div className="flex items-center gap-2 mb-1"><Shield size={14} className="text-success" /><span className="text-[10px] text-textMuted">最可靠</span></div>
                        <div className="font-medium text-sm">{mostReliable.modelName}</div>
                        <div className="text-[10px] text-textMuted mt-1 line-clamp-2">{mostReliable.reason}</div>
                      </div>
                    )}
                    {fastestResponse && fastestResponse.modelName && (
                      <div className="p-3 rounded-lg bg-bgApp border border-border/50">
                        <div className="flex items-center gap-2 mb-1"><Zap size={14} className="text-warning" /><span className="text-[10px] text-textMuted">最快响应</span></div>
                        <div className="font-medium text-sm">{fastestResponse.modelName}</div>
                        <div className="text-[10px] text-textMuted mt-1 line-clamp-2">{fastestResponse.reason}</div>
                      </div>
                    )}
                    {(() => {
                      const avoidNow = recommendations.find(r => r.type === 'avoid_now');
                      if (!avoidNow?.extraData) return null;
                      try {
                        const avoidList = JSON.parse(avoidNow.extraData) as Array<{id: string; name: string; reason: string}>;
                        if (avoidList.length === 0) return null;
                        return (
                          <div className="p-3 rounded-lg bg-critical/10 border border-critical/20">
                            <div className="flex items-center gap-2 mb-1"><XCircle size={14} className="text-critical" /><span className="text-[10px] text-textMuted">应避免 ({avoidList.length})</span></div>
                            {avoidList.slice(0, 2).map((item, i) => (
                              <div key={i} className="text-xs mt-1">
                                <span className="font-medium">{item.name}</span>
                                <span className="text-textMuted ml-1 text-[10px]">{item.reason}</span>
                              </div>
                            ))}
                            {avoidList.length > 2 && <div className="text-[10px] text-textMuted mt-1">还有 {avoidList.length - 2} 个...</div>}
                          </div>
                        );
                      } catch { return null; }
                    })()}
                  </div>
                </div>
              )}

              {/* 全局指数趋势图卡片 */}
              <div className="p-5 rounded-xl border border-border bg-bgSurface">
                <h3 className="font-bold text-sm mb-3">全局指数趋势</h3>
                <div className="h-32">
                  {globalIndex.length > 0 ? (
                    <ReactECharts option={globalIndexChartOptions} style={{ height: '100%', width: '100%' }} />
                  ) : (
                    <div className="flex items-center justify-center h-full text-textMuted text-xs">暂无数据</div>
                  )}
                </div>
              </div>

              {/* 性能退化警报列表：滚动区域，空状态显示 CheckCircle2 */}
              <div className="p-5 rounded-xl border border-border bg-bgSurface flex-1 flex flex-col min-h-0 overflow-hidden">
                <h3 className="font-bold text-sm mb-3 flex items-center gap-2 flex-shrink-0"><AlertTriangle size={14} className="text-critical" />性能退化警报</h3>
                {filteredDegradations.length > 0 ? (
                  <div className="space-y-2 overflow-y-auto scrollbar-stable flex-1 min-h-0">
                    {filteredDegradations.map((d, i) => (
                      <div key={i} className="p-3 rounded-lg bg-bgApp border border-border/50">
                        <div className="flex items-center justify-between mb-1">
                          <span className="font-medium text-xs">{d.modelName}</span>
                          <span className={`badge ${d.severity === 'critical' ? 'badge-critical' : d.severity === 'major' ? 'badge-warning' : 'badge-info'}`}>-{d.dropPercentage}%</span>
                        </div>
                        <p className="text-[10px] text-textMuted line-clamp-2">{d.message}</p>
                        <div className="text-[9px] text-textMuted mt-1">{new Date(d.detectedAt).toLocaleString('zh-CN')}</div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="flex-1 flex items-center justify-center text-textMuted">
                    <div className="text-center">
                      <CheckCircle2 className="mx-auto mb-2 text-success" size={24} />
                      <span className="text-xs">所有模型状态良好</span>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}

        {activeTab === 'models' && (
          <div className="space-y-6">
            {detailModel && (() => {
              const detailScore = filteredScores.find(s => s.modelId === detailModel);
              if (!detailScore) return null;
              return (
                <div className="p-5 rounded-xl border border-primary/30 bg-bgSurface ring-2 ring-primary/20">
                  <div className="flex items-center justify-between mb-4">
                    <div>
                      <h2 className="text-base font-bold">{detailScore.modelName} 历史趋势</h2>
                      <p className="text-[11px] text-textMuted">{PROVIDER_ZH[detailScore.provider] || detailScore.provider} · 近30天数据</p>
                    </div>
                    <button onClick={closeModelDetail} className="text-textMuted hover:text-textMain cursor-pointer p-2 rounded-lg hover:bg-bgApp transition-colors">
                      <EyeOff size={18} />
                    </button>
                  </div>
                  <div className="flex flex-wrap gap-1.5 mb-4">
                    <button
                      onClick={() => setDetailAxis('score')}
                      className={`px-3 py-1.5 text-[11px] font-medium rounded-lg transition-colors cursor-pointer ${detailAxis === 'score' ? 'bg-primary text-white' : 'bg-bgApp text-textMuted hover:text-textMain'}`}
                    >
                      综合得分
                    </button>
                    {Object.entries(AXES_ZH)
                      .filter(([key]) => !['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'].includes(key))
                      .map(([key, label]) => {
                      const hasData = detailScore.axes[key] !== null && detailScore.axes[key] !== undefined;
                      if (!hasData) return null;
                      return (
                        <button
                          key={key}
                          onClick={() => setDetailAxis(key)}
                          className={`px-3 py-1.5 text-[11px] font-medium rounded-lg transition-colors cursor-pointer ${detailAxis === key ? 'bg-primary text-white' : 'bg-bgApp text-textMuted hover:text-textMain'}`}
                        >
                          {label}
                        </button>
                      );
                    })}
                  </div>
                  <div className="h-64">
                    {isLoadingModelHistory ? (
                      <div className="flex items-center justify-center h-full text-textMuted text-sm">
                        <Activity className="animate-spin mr-2" size={16} /> 加载历史数据...
                      </div>
                    ) : modelHistory.length > 0 ? (() => {
                      const chartOpts = getModelDetailChartOptions();
                      return chartOpts ? <ReactECharts key={`${detailModel}-${detailAxis}`} option={chartOpts} style={{ height: '100%', width: '100%' }} notMerge={true} /> : null;
                    })() : (
                      <div className="flex items-center justify-center h-full text-textMuted text-sm">
                        暂无历史数据
                      </div>
                    )}
                  </div>
                  <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-3">
                    <div className="p-3 rounded-lg bg-bgApp text-center">
                      <div className="text-2xl font-bold text-primary">{detailScore.score}</div>
                      <div className="text-[10px] text-textMuted">当前得分</div>
                    </div>
                    <div className="p-3 rounded-lg bg-bgApp text-center">
                      <div className={`text-lg font-bold ${detailScore.trend === 'up' ? 'text-success' : detailScore.trend === 'down' ? 'text-critical' : 'text-textMuted'}`}>
                        {detailScore.trend === 'up' ? '↑ 上升' : detailScore.trend === 'down' ? '↓ 下降' : '→ 稳定'}
                      </div>
                      <div className="text-[10px] text-textMuted">趋势</div>
                    </div>
                    <div className="p-3 rounded-lg bg-bgApp text-center">
                      <div className="text-lg font-bold">{detailScore.confidenceLower.toFixed(1)} - {detailScore.confidenceUpper.toFixed(1)}</div>
                      <div className="text-[10px] text-textMuted">置信区间</div>
                    </div>
                    <div className="p-3 rounded-lg bg-bgApp text-center">
                      <div className="text-lg font-bold">{modelHistory.length}</div>
                      <div className="text-[10px] text-textMuted">数据点</div>
                    </div>
                  </div>
                </div>
              );
            })()}

            {compareModels.length > 0 && (
              <div className="p-5 rounded-xl border border-border bg-bgSurface">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <h2 className="text-base font-bold">模型对比</h2>
                    <p className="text-[11px] text-textMuted">已选择 {compareModels.length}/3 个模型</p>
                  </div>
                  <button onClick={() => setCompareModels([])} className="text-xs text-critical hover:underline cursor-pointer">清空对比</button>
                </div>
                <div className="flex gap-2 mb-4 flex-wrap">
                  {compareModels.map((id, idx) => {
                    const s = filteredScores.find(sc => sc.modelId === id);
                    return s ? (
                      <div key={id} className="flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-medium" style={{ backgroundColor: `${MODEL_COLORS[idx]}22`, color: MODEL_COLORS[idx], border: `1px solid ${MODEL_COLORS[idx]}` }}>
                        <span className="w-2 h-2 rounded-full" style={{ backgroundColor: MODEL_COLORS[idx] }} />
                        {s.modelName} ({s.score}分)
                        <button onClick={(e) => { e.stopPropagation(); toggleCompareModel(id); }} className="ml-1 w-5 h-5 flex items-center justify-center rounded-full hover:bg-black/10 cursor-pointer text-base font-bold">×</button>
                      </div>
                    ) : null;
                  })}
                </div>
                <div className={`${compareModels.length < 3 ? 'grid grid-cols-1 lg:grid-cols-3 gap-4' : 'flex justify-center'}`}>
                  <div className={`h-80 ${compareModels.length < 3 ? 'lg:col-span-2' : 'w-full max-w-2xl'}`}>
                    {(() => {
                      const opts = getCompareRadarOptions();
                      return opts ? <ReactECharts option={opts} style={{ height: '100%', width: '100%' }} /> : null;
                    })()}
                  </div>
                  {compareModels.length < 3 && (
                    <div className="p-3 rounded-lg bg-bgApp border border-border/50 max-h-80 overflow-y-auto scrollbar-stable">
                      <div className="text-xs font-medium text-textMuted mb-2">添加对比模型 (还可选 {3 - compareModels.length} 个)</div>
                      <div className="space-y-1">
                        {filteredScores.filter(s => !compareModels.includes(s.modelId)).map(s => (
                          <button
                            key={s.modelId}
                            onClick={() => toggleCompareModel(s.modelId)}
                            className="w-full flex items-center justify-between px-2 py-1.5 rounded hover:bg-bgSurface transition-colors text-left cursor-pointer"
                          >
                            <span className="text-xs font-medium truncate">{s.modelName}</span>
                            <span className="text-xs text-primary font-bold">{s.score}</span>
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
                <div className="mt-4 grid grid-cols-1 md:grid-cols-3 gap-4">
                  {compareModels.map((id, idx) => {
                    const s = filteredScores.find(sc => sc.modelId === id);
                    if (!s) return null;
                    return (
                      <div key={id} className="p-3 rounded-lg bg-bgApp border-l-4" style={{ borderLeftColor: MODEL_COLORS[idx] }}>
                        <div className="font-bold text-sm mb-2 text-center">{s.modelName}</div>
                        <div className="grid grid-cols-3 gap-1">
                          {Object.entries(s.axes).filter(([key, v]) => v !== null && v !== 0 && !['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'].includes(key)).slice(0, 9).map(([key, value]) => (
                            <div key={key} className="text-center">
                              <div className="text-[10px] font-bold">{((value as number) * 100).toFixed(0)}%</div>
                              <div className="text-[8px] text-textMuted">{AXES_ZH[key] || key}</div>
                            </div>
                          ))}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            <div>
              <div className="flex items-center justify-between mb-4">
                <div>
                  <h2 className="text-base font-bold">所有模型</h2>
                  <p className="text-[11px] text-textMuted">点击选择模型进行对比（最多3个）</p>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    placeholder="搜索模型..."
                    value={searchQuery}
                    onChange={e => setSearchQuery(e.target.value)}
                    className="px-3 py-1.5 text-xs bg-bgApp border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/20 w-32"
                  />
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5 mb-4">
                <button
                  onClick={() => setSortBy('score')}
                  className={`px-2.5 py-1 text-[11px] font-medium rounded-lg transition-colors cursor-pointer ${sortBy === 'score' ? 'bg-primary text-white' : 'bg-bgApp text-textMuted hover:text-textMain'}`}
                >
                  综合得分
                </button>
                {Object.entries(AXES_ZH).filter(([key]) => !['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'].includes(key)).map(([key, label]) => (
                  <button
                    key={key}
                    onClick={() => setSortBy(key)}
                    className={`px-2.5 py-1 text-[11px] font-medium rounded-lg transition-colors cursor-pointer ${sortBy === key ? 'bg-primary text-white' : 'bg-bgApp text-textMuted hover:text-textMain'}`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {filteredScores.map(s => {
                  const isComparing = compareModels.includes(s.modelId);
                  const compareIdx = compareModels.indexOf(s.modelId);
                  return (
                    <div
                      key={s.modelId}
                      onClick={() => toggleCompareModel(s.modelId)}
                      className={`p-5 rounded-xl border bg-bgSurface card-hover cursor-pointer transition-all ${isComparing ? 'ring-2' : 'border-border'}`}
                      style={isComparing ? { borderColor: MODEL_COLORS[compareIdx], boxShadow: `0 0 0 2px ${MODEL_COLORS[compareIdx]}33` } : {}}
                    >
                      <div className="flex items-center justify-between mb-3">
                        <div>
                          <h3 className="font-bold text-sm">{s.modelName}</h3>
                          <p className="text-[10px] text-textMuted">{PROVIDER_ZH[s.provider] || s.provider}</p>
                        </div>
                        <div className="flex items-center gap-2">
                          {isComparing && <span className="w-3 h-3 rounded-full" style={{ backgroundColor: MODEL_COLORS[compareIdx] }} />}
                          <div className="text-right">
                            <div className="text-2xl font-bold text-primary">{s.score}</div>
                            {s.standardError > 0 && <div className="text-[10px] text-textMuted">±{s.standardError.toFixed(1)}</div>}
                          </div>
                        </div>
                      </div>
                      <div className="h-40">
                        <ReactECharts option={getRadarChartOptions(s.axes)} style={{ height: '100%', width: '100%' }} />
                      </div>
                      <div className="mt-3 grid grid-cols-3 gap-1.5">
                        {Object.entries(s.axes).filter(([key, v]) => v !== null && v !== 0 && !['memoryRetention', 'hallucinationRate', 'planCoherence', 'contextWindow'].includes(key)).slice(0, 9).map(([key, value]) => (
                          <div key={key} className={`text-center p-1.5 rounded ${sortBy === key ? 'bg-amber-100 dark:bg-amber-900/30 ring-1 ring-amber-500' : 'bg-bgApp'}`}>
                            <div className={`text-[10px] font-bold ${sortBy === key ? 'text-amber-600 dark:text-amber-400' : ''}`}>{((value as number) * 100).toFixed(0)}%</div>
                            <div className={`text-[8px] truncate ${sortBy === key ? 'text-amber-600 dark:text-amber-400' : 'text-textMuted'}`}>{AXES_ZH[key] || key}</div>
                          </div>
                        ))}
                      </div>
                      <button
                        onClick={(e) => { e.stopPropagation(); openModelDetail(s.modelId); }}
                        className="mt-3 w-full py-2 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/20 rounded-lg transition-colors cursor-pointer flex items-center justify-center gap-1.5"
                      >
                        <TrendingUp size={14} />
                        查看历史趋势
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        )}

        {activeTab === 'alerts' && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="p-5 rounded-xl border border-border bg-bgSurface">
              <h3 className="font-bold text-base mb-4">性能退化</h3>
              {filteredDegradations.length > 0 ? (
                <div className="space-y-3">
                  {filteredDegradations.map((d, i) => (
                    <div key={i} className="p-4 rounded-lg bg-bgApp border border-border/50">
                      <div className="flex items-center justify-between mb-2">
                        <div>
                          <span className="font-bold text-sm">{d.modelName}</span>
                          <span className="text-textMuted text-xs ml-2">{PROVIDER_ZH[d.provider] || d.provider}</span>
                        </div>
                        <span className={`badge ${d.severity === 'critical' ? 'badge-critical' : d.severity === 'major' ? 'badge-warning' : 'badge-info'}`}>{SEVERITY_ZH[d.severity] || d.severity}</span>
                      </div>
                      <div className="flex items-center gap-2 mb-2">
                        <span className="px-2 py-0.5 text-[10px] rounded bg-primary/10 text-primary">{d.type}</span>
                      </div>
                      <p className="text-xs text-textMuted mb-2">{d.message}</p>
                      <div className="flex items-center justify-between text-[10px] text-textMuted">
                        <span>当前: {d.currentScore} / 基线: {d.baselineScore}</span>
                        <span>下降 {d.dropPercentage}%</span>
                      </div>
                      <div className="text-[10px] text-textMuted mt-2 flex items-center gap-1">
                        <Clock size={10} />
                        <span>检测时间: {new Date(d.detectedAt).toLocaleString('zh-CN')}</span>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="py-12 text-center text-textMuted">
                  <CheckCircle2 className="mx-auto mb-2 text-success" size={32} />
                  <span className="text-sm">暂无性能退化</span>
                </div>
              )}
            </div>

            <div className="p-5 rounded-xl border border-border bg-bgSurface">
              <h3 className="font-bold text-base mb-4">系统警报</h3>
              {filteredAlerts.length > 0 ? (
                <div className="space-y-3 max-h-[500px] overflow-y-auto scrollbar-stable">
                  {filteredAlerts.map((a, i) => (
                    <div key={i} className="p-4 rounded-lg bg-bgApp border border-border/50">
                      <div className="flex items-center justify-between mb-2">
                        <span className="font-bold text-sm">{a.modelName}</span>
                        <span className={`badge ${a.severity === 'critical' ? 'badge-critical' : a.severity === 'warning' ? 'badge-warning' : 'badge-info'}`}>{SEVERITY_ZH[a.severity] || a.severity}</span>
                      </div>
                      <p className="text-xs text-textMuted">{a.issue}</p>
                      <div className="text-[10px] text-textMuted mt-2">{new Date(a.detectedAt).toLocaleString('zh-CN')}</div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="py-12 text-center text-textMuted">
                  <CheckCircle2 className="mx-auto mb-2 text-success" size={32} />
                  <span className="text-sm">暂无警报</span>
                </div>
              )}
            </div>
          </div>
        )}

        {activeTab === 'providers' && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {providerReliability.map(p => (
              <div key={p.provider} className="p-5 rounded-xl border border-border bg-bgSurface card-hover">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="font-bold text-base">{PROVIDER_ZH[p.provider] || p.provider}</h3>
                  <span className={`badge ${p.isAvailable ? 'badge-success' : 'badge-critical'}`}>{p.isAvailable ? '在线' : '离线'}</span>
                </div>
                <div className="text-center mb-4">
                  <div className="text-3xl font-bold" style={{ color: p.trustScore >= 70 ? 'var(--success)' : p.trustScore >= 50 ? 'var(--warning)' : 'var(--critical)' }}>{p.trustScore}</div>
                  <div className="text-[10px] text-textMuted">信任评分</div>
                </div>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between"><span className="text-textMuted">活跃模型</span><span className="font-medium">{p.activeModels}</span></div>
                  <div className="flex justify-between"><span className="text-textMuted">顶级表现者</span><span className="font-medium text-primary">{p.topPerformers}</span></div>
                  <div className="flex justify-between"><span className="text-textMuted">总事故数</span><span className="font-medium">{p.totalIncidents}</span></div>
                  <div className="flex justify-between"><span className="text-textMuted">最近事故</span><span className="font-medium text-xs">{new Date(p.lastIncident).toLocaleDateString('zh-CN')}</span></div>
                  <div className="flex justify-between">
                    <span className="text-textMuted">趋势</span>
                    <span className={`font-medium ${p.trend === 'improving' ? 'text-success' : p.trend === 'unreliable' ? 'text-critical' : 'text-textMuted'}`}>
                      {p.trend === 'improving' ? '改善中' : p.trend === 'unreliable' ? '不稳定' : p.trend === 'moderate' ? '一般' : p.trend}
                    </span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

      </div>

    </div>
  );
}
