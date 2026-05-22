import { useEffect, useState, useMemo, useRef, useLayoutEffect } from 'react';
import ReactECharts from 'echarts-for-react';
import {
  Sun, Moon, RefreshCw, Activity, TrendingUp,
  AlertTriangle, Shield, CheckCircle2, Zap, Clock, EyeOff, BarChart3,
  Gauge, Users, Server, FileWarning, Award, Target, Sparkles, XCircle
} from 'lucide-react';

// Types
interface Model {
  id: string;
  name: string;
  provider: string;
  vendor: string;
  isReasoning: boolean;
  isNew: boolean;
  isStale: boolean;
  status: string;
  standardError: number;
}

interface Score {
  modelId: string;
  modelName: string;
  provider: string;
  score: number;
  trend: string;
  confidenceLower: number;
  confidenceUpper: number;
  standardError: number;
  timestamp: string;
  axes: Record<string, number | null>;
}

interface HistoryPoint {
  modelId: string;
  modelName: string;
  score: number;
  timestamp: string;
  suite: string;
  axes: Record<string, number | null>;
}

interface Degradation {
  modelId: string;
  modelName: string;
  provider: string;
  currentScore: number;
  baselineScore: number;
  dropPercentage: number;
  zScore: string;
  severity: string;
  detectedAt: string;
  message: string;
  type: string;
}

interface Alert {
  modelName: string;
  provider: string;
  issue: string;
  severity: string;
  detectedAt: string;
}

interface GlobalIndex {
  timestamp: string;
  globalScore: number;
  modelsCount: number;
  trend: string;
  performingWell: number;
  totalModels: number;
}

interface ProviderReliability {
  provider: string;
  trustScore: number;
  totalIncidents: number;
  incidentsPerMonth: number;
  avgRecoveryHours: string;
  lastIncident: string;
  trend: string;
  activeModels: number;
  topPerformers: number;
  isAvailable: boolean;
}

interface Recommendation {
  type: string;
  modelId: string;
  modelName: string;
  vendor: string;
  score: number;
  reason: string;
  evidence: string;
  extraData?: string;
}

interface SyncStatus {
  lastSync: string;
  nextSync: string;
}

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

const PROVIDER_ZH: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  xai: 'xAI',
  kimi: 'Kimi',
  glm: 'GLM',
  deepseek: 'DeepSeek'
};

const SEVERITY_ZH: Record<string, string> = {
  critical: '严重',
  major: '重要',
  warning: '警告',
  minor: '轻微'
};

const PERIOD_OPTIONS = [
  { value: 'latest', label: '最新' },
  { value: '24h', label: '24小时' },
  { value: '7d', label: '7天' },
  { value: '14d', label: '14天' },
  { value: '30d', label: '30天' }
];

const MODEL_COLORS = [
  '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6',
  '#06b6d4', '#ec4899', '#84cc16', '#f97316', '#6366f1',
  '#14b8a6', '#a855f7', '#22c55e', '#e11d48', '#0ea5e9',
  '#d946ef', '#eab308', '#64748b', '#fb7185', '#2dd4bf'
];

export default function App() {
  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    const saved = localStorage.getItem('theme');
    if (saved) return saved as 'light' | 'dark';
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  });

  const [models, setModels] = useState<Model[]>([]);
  const [scores, setScores] = useState<Score[]>([]);
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const [degradations, setDegradations] = useState<Degradation[]>([]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [globalIndex, setGlobalIndex] = useState<GlobalIndex[]>([]);
  const [providerReliability, setProviderReliability] = useState<ProviderReliability[]>([]);
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);

  const [period, setPeriod] = useState('latest');
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  const [visibleModels, setVisibleModels] = useState<string[]>([]);
  const [blockedModels, setBlockedModels] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [activeTab, setActiveTab] = useState<'overview' | 'models' | 'alerts' | 'providers'>('overview');
  const [isSyncing, setIsSyncing] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [compareModels, setCompareModels] = useState<string[]>([]);
  const [sortBy, setSortBy] = useState<string>('score');
  const [detailModel, setDetailModel] = useState<string | null>(null);
  const [detailAxis, setDetailAxis] = useState<string>('score');
  const [modelHistory, setModelHistory] = useState<HistoryPoint[]>([]);
  const [leftColHeight, setLeftColHeight] = useState<number | null>(null);
  const leftColRef = useRef<HTMLDivElement>(null);

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
      return () => observer.disconnect();
    }
  }, [activeTab, scores.length, period]);

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
    const next = theme === 'light' ? 'dark' : 'light';
    setTheme(next);
    document.documentElement.classList.toggle('dark', next === 'dark');
    localStorage.setItem('theme', next);
  };

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
  }, []);

  const fetchAll = async () => {
    if (period !== 'latest') {
      setIsLoadingHistory(true);
    }
    try {
      const requests = [
        fetch('/api/models'),
        fetch('/api/scores?period=latest'),
        fetch('/api/degradations'),
        fetch('/api/alerts'),
        fetch('/api/global-index'),
        fetch('/api/provider-reliability'),
        fetch('/api/recommendations'),
        fetch('/api/sync-status'),
        fetch('/api/config')
      ];
      if (period !== 'latest') {
        requests.push(fetch(`/api/scores?period=${period}`));
      }

      const responses = await Promise.all(requests);
      const [modelsRes, latestScoresRes, degradRes, alertsRes, globalRes, provRes, recRes, syncRes, configRes] = responses;

      setModels(await modelsRes.json());
      setScores(await latestScoresRes.json());

      if (period !== 'latest' && responses[9]) {
        setHistory(await responses[9].json());
      } else {
        setHistory([]);
      }

      setDegradations(await degradRes.json());
      setAlerts(await alertsRes.json());
      setGlobalIndex(await globalRes.json());
      setProviderReliability(await provRes.json());
      setRecommendations(await recRes.json());
      setSyncStatus(await syncRes.json());

      const config = await configRes.json();
      setBlockedModels(config.blocked_models || []);
    } catch (e) {
      console.error('Fetch error:', e);
    } finally {
      setIsLoadingHistory(false);
    }
  };

  useEffect(() => {
    fetchAll();
    const configInterval = setInterval(async () => {
      try {
        const res = await fetch('/api/config');
        const data = await res.json();
        setBlockedModels(data.blocked_models || []);
      } catch {}
    }, 5000);
    return () => clearInterval(configInterval);
  }, []);

  useEffect(() => {
    fetchAll();
  }, [period]);

  const [visibleModelsInitialized, setVisibleModelsInitialized] = useState(false);

  useEffect(() => {
    if (models.length > 0 && !visibleModelsInitialized) {
      setVisibleModels([]);
      setVisibleModelsInitialized(true);
    }
  }, [models, blockedModels, visibleModelsInitialized]);

  const triggerSync = async () => {
    setIsSyncing(true);
    try {
      await fetch('/api/sync-now', { method: 'POST' });
      await fetchAll();
    } catch { alert('同步失败'); }
    setIsSyncing(false);
  };

  const filteredModels = useMemo(() => {
    return models
      .filter(m => !blockedModels.includes(m.id))
      .filter(m => m.name.toLowerCase().includes(searchQuery.toLowerCase()))
      .sort((a, b) => sortModelName(a.name, b.name));
  }, [models, blockedModels, searchQuery]);

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

  const filteredHistory = useMemo(() => {
    return history.filter(h =>
      !blockedModels.includes(h.modelId) &&
      visibleModels.includes(h.modelId)
    );
  }, [history, blockedModels, visibleModels]);

  const filteredDegradations = useMemo(() => {
    const visibleModelIds = new Set(filteredScores.map(s => s.modelId));
    return degradations.filter(d => !blockedModels.includes(d.modelId) && visibleModelIds.has(d.modelId));
  }, [degradations, blockedModels, filteredScores]);

  const filteredAlerts = useMemo(() => {
    const visibleNames = new Set(filteredScores.map(s => s.modelName));
    const blockedNames = models.filter(m => blockedModels.includes(m.id)).map(m => m.name);
    return alerts.filter(a => !blockedNames.includes(a.modelName) && visibleNames.has(a.modelName));
  }, [alerts, models, blockedModels, filteredScores]);

  const getModelColor = (modelId: string) => {
    const idx = models.findIndex(m => m.id === modelId);
    return MODEL_COLORS[idx % MODEL_COLORS.length];
  };

  const getHistoryChartOptions = () => {
    const isDark = theme === 'dark';
    const modelIds = [...new Set(filteredHistory.map(h => h.modelId))];

    const series = modelIds.map(modelId => {
      const modelData = filteredHistory.filter(h => h.modelId === modelId);
      const model = models.find(m => m.id === modelId);
      const color = getModelColor(modelId);

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
  };

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

  const getGlobalIndexChartOptions = () => {
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
  };

  const selectedModelData = selectedModel ? filteredScores.find(s => s.modelId === selectedModel) : null;

  const getLatestBarChartOptions = () => {
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
  };

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
    if (compareModels.includes(modelId)) {
      setCompareModels(compareModels.filter(id => id !== modelId));
    } else if (compareModels.length < 3) {
      setCompareModels([...compareModels, modelId]);
    }
  };

  const fetchModelHistory = async (modelId: string) => {
    try {
      const res = await fetch(`/api/model/history?id=${modelId}&days=30`);
      const data = await res.json();
      setModelHistory(data);
    } catch (e) {
      console.error('Fetch model history error:', e);
      setModelHistory([]);
    }
  };

  const openModelDetail = (modelId: string) => {
    setDetailModel(modelId);
    setDetailAxis('score');
    fetchModelHistory(modelId);
  };

  const closeModelDetail = () => {
    setDetailModel(null);
    setModelHistory([]);
  };

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
        min: isAxisMode ? 0 : (value: { min: number }) => Math.max(0, Math.floor(value.min * 0.9)),
        max: isAxisMode ? 100 : (value: { max: number }) => Math.min(100, Math.ceil(value.max * 1.05)),
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

  const currentGlobal = globalIndex[0];
  const bestForCode = recommendations.find(r => r.type === 'best_for_code');
  const mostReliable = recommendations.find(r => r.type === 'most_reliable');
  const fastestResponse = recommendations.find(r => r.type === 'fastest_response');

  return (
    <div className="min-h-screen bg-bgApp text-textMain transition-colors duration-200">
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
              <div className="hidden md:flex items-center gap-2 text-[10px] text-textMuted mr-4">
                <Clock size={12} />
                <span>下次同步: {new Date(syncStatus.nextSync).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</span>
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

      <div className="max-w-[1600px] mx-auto px-4 py-6">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <div className="p-4 rounded-xl border border-border bg-bgSurface card-hover">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-primary/10 text-primary flex items-center justify-center flex-shrink-0">
                <Gauge size={20} />
              </div>
              <div>
                <div className="text-[10px] text-textMuted font-medium">全局智能指数</div>
                <div className="text-xl font-bold">{currentGlobal?.globalScore || '-'}<span className="text-sm font-normal text-textMuted ml-1">分</span></div>
              </div>
            </div>
          </div>

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

        {activeTab === 'overview' && (
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
            <div ref={leftColRef} className="lg:col-span-2 space-y-6">
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

                <div className="h-72">
                  {period === 'latest' ? (
                    filteredScores.length > 0 ? (
                      <ReactECharts option={getLatestBarChartOptions()} style={{ height: '100%', width: '100%' }} />
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
                    <ReactECharts key={`${period}-${visibleModels.join(',')}`} option={getHistoryChartOptions()} style={{ height: '100%', width: '100%' }} notMerge={true} />
                  ) : (
                    <div className="flex items-center justify-center h-full text-textMuted text-sm">
                      所选模型暂无历史数据
                    </div>
                  )}
                </div>
              </div>

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

            <div className="flex flex-col gap-6" style={leftColHeight ? { height: leftColHeight } : undefined}>
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
                    {Object.entries(selectedModelData.axes).filter(([_, v]) => v !== null).slice(0, 6).map(([key, value]) => (
                      <div key={key} className="p-2 rounded-lg bg-bgApp">
                        <div className="text-xs font-bold">{((value as number) * 100).toFixed(0)}%</div>
                        <div className="text-[9px] text-textMuted">{AXES_ZH[key] || key}</div>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
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

              <div className="p-5 rounded-xl border border-border bg-bgSurface">
                <h3 className="font-bold text-sm mb-3">全局指数趋势</h3>
                <div className="h-32">
                  {globalIndex.length > 0 ? (
                    <ReactECharts option={getGlobalIndexChartOptions()} style={{ height: '100%', width: '100%' }} />
                  ) : (
                    <div className="flex items-center justify-center h-full text-textMuted text-xs">暂无数据</div>
                  )}
                </div>
              </div>

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
                    {modelHistory.length > 0 ? (
                      getModelDetailChartOptions() && <ReactECharts key={`${detailModel}-${detailAxis}`} option={getModelDetailChartOptions()!} style={{ height: '100%', width: '100%' }} notMerge={true} />
                    ) : (
                      <div className="flex items-center justify-center h-full text-textMuted text-sm">
                        <Activity className="animate-spin mr-2" size={16} /> 加载历史数据...
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
                    {getCompareRadarOptions() && <ReactECharts option={getCompareRadarOptions()!} style={{ height: '100%', width: '100%' }} />}
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
                          {Object.entries(s.axes).filter(([_, v]) => v !== null && v !== 0).slice(0, 9).map(([key, value]) => (
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
                {Object.entries(AXES_ZH).slice(0, 9).map(([key, label]) => (
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
                        {Object.entries(s.axes).filter(([_, v]) => v !== null && v !== 0).slice(0, 9).map(([key, value]) => (
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
