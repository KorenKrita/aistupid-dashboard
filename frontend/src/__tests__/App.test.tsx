import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import App from '../App'

// Mock echarts-for-react to prevent canvas rendering issues in jsdom and inspect chart options
vi.mock('echarts-for-react', () => {
  return {
    default: (props: any) => (
      <div
        data-testid="mock-echarts"
        data-options={JSON.stringify(props.option)}
      />
    )
  }
})

// Mock data matching backend API shapes
const mockModels = [
  {
    id: 'gpt-4o',
    name: 'GPT-4o',
    provider: 'openai',
    vendor: 'openai',
    isReasoning: false,
    isNew: false,
    isStale: false,
    status: 'active',
    standardError: 0.05
  },
  {
    id: 'claude-3-5-sonnet',
    name: 'Claude 3.5 Sonnet',
    provider: 'anthropic',
    vendor: 'anthropic',
    isReasoning: false,
    isNew: false,
    isStale: false,
    status: 'active',
    standardError: 0.04
  },
  {
    id: 'blocked-model',
    name: 'Blocked Model',
    provider: 'openai',
    vendor: 'openai',
    isReasoning: false,
    isNew: false,
    isStale: false,
    status: 'active',
    standardError: 0.05
  }
]

const mockScores = [
  {
    modelId: 'gpt-4o',
    modelName: 'GPT-4o',
    provider: 'openai',
    score: 0.88,
    trend: 'up',
    confidenceLower: 0.85,
    confidenceUpper: 0.91,
    standardError: 0.03,
    timestamp: '2026-05-22T00:00:00Z',
    axes: {
      correctness: 0.9,
      complexity: 0.8,
      codeQuality: 0.9,
      efficiency: 0.85,
      stability: 0.9,
      edgeCases: 0.8,
      debugging: 0.85,
      format: 0.95,
      safety: 0.98,
      memoryRetention: 0.7,
      hallucinationRate: 0.1,
      planCoherence: 0.6,
      contextWindow: 0.8
    }
  },
  {
    modelId: 'claude-3-5-sonnet',
    modelName: 'Claude 3.5 Sonnet',
    provider: 'anthropic',
    score: 0.92,
    trend: 'up',
    confidenceLower: 0.9,
    confidenceUpper: 0.94,
    standardError: 0.02,
    timestamp: '2026-05-22T00:00:00Z',
    axes: {
      correctness: 0.95,
      complexity: 0.85,
      codeQuality: 0.95,
      efficiency: 0.9,
      stability: 0.95,
      edgeCases: 0.85,
      debugging: 0.9,
      format: 0.98,
      safety: 0.99,
      memoryRetention: 0.8,
      hallucinationRate: 0.05,
      planCoherence: 0.7,
      contextWindow: 0.9
    }
  },
  {
    modelId: 'blocked-model',
    modelName: 'Blocked Model',
    provider: 'openai',
    score: 0.5,
    trend: 'down',
    confidenceLower: 0.45,
    confidenceUpper: 0.55,
    standardError: 0.05,
    timestamp: '2026-05-22T00:00:00Z',
    axes: {
      correctness: 0.5,
      complexity: 0.5,
      codeQuality: 0.5,
      efficiency: 0.5,
      stability: 0.5,
      edgeCases: 0.5,
      debugging: 0.5,
      format: 0.5,
      safety: 0.5,
      memoryRetention: 0.5,
      hallucinationRate: 0.5,
      planCoherence: 0.5,
      contextWindow: 0.5
    }
  }
]

const mockHistory24h = [
  {
    modelId: 'gpt-4o',
    modelName: 'GPT-4o',
    score: 0.87,
    timestamp: '2026-05-21T12:00:00Z',
    suite: 'current',
    axes: { correctness: 0.89, complexity: 0.79, codeQuality: 0.89 }
  },
  {
    modelId: 'claude-3-5-sonnet',
    modelName: 'Claude 3.5 Sonnet',
    score: 0.91,
    timestamp: '2026-05-21T12:00:00Z',
    suite: 'current',
    axes: { correctness: 0.94, complexity: 0.84, codeQuality: 0.94 }
  }
]

const mockModelHistory = [
  {
    modelId: 'gpt-4o',
    modelName: 'GPT-4o',
    score: 0.86,
    timestamp: '2026-05-20T00:00:00Z',
    suite: 'current',
    axes: {
      correctness: 0.88,
      complexity: 0.78,
      codeQuality: 0.88,
      memoryRetention: 0.7,
      hallucinationRate: 0.1,
      planCoherence: 0.6,
      contextWindow: 0.8
    }
  },
  {
    modelId: 'gpt-4o',
    modelName: 'GPT-4o',
    score: 0.88,
    timestamp: '2026-05-22T00:00:00Z',
    suite: 'current',
    axes: {
      correctness: 0.9,
      complexity: 0.8,
      codeQuality: 0.9,
      memoryRetention: 0.7,
      hallucinationRate: 0.1,
      planCoherence: 0.6,
      contextWindow: 0.8
    }
  }
]

const mockConfig = {
  blocked_models: ['blocked-model']
}

// Mock fetch globally
const mockFetch = vi.fn()
globalThis.fetch = mockFetch as typeof fetch

const defaultMockFetchImplementation = (url: string) => {
  if (url.includes('/api/config')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve(mockConfig)
    })
  }
  if (url.includes('/api/models')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve(mockModels)
    })
  }
  if (url.includes('/api/scores?period=latest')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve(mockScores)
    })
  }
  if (url.includes('/api/scores?period=24h')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve(mockHistory24h)
    })
  }
  if (url.includes('/api/degradations')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve([])
    })
  }
  if (url.includes('/api/alerts')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve([])
    })
  }
  if (url.includes('/api/global-index')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve([])
    })
  }
  if (url.includes('/api/provider-reliability')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve([])
    })
  }
  if (url.includes('/api/recommendations')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve([])
    })
  }
  if (url.includes('/api/sync-status')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ lastSync: '2026-05-22T00:00:00Z', nextSync: '2026-05-22T00:10:00Z' })
    })
  }
  if (url.includes('/api/model/history')) {
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve(mockModelHistory)
    })
  }
  return Promise.resolve({
    ok: true,
    json: () => Promise.resolve({})
  })
}

beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockImplementation(defaultMockFetchImplementation)
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('App - API and Fetch Integration Tests', () => {
  it('should show loading state initially while requests are pending', async () => {
    // Return a promise that does not resolve immediately to keep it in loading state
    let resolveConfig: any
    const pendingPromise = new Promise((resolve) => {
      resolveConfig = resolve
    })

    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/api/config')) {
        return pendingPromise.then(() => ({
          ok: true,
          json: () => Promise.resolve(mockConfig)
        }))
      }
      return defaultMockFetchImplementation(url)
    })

    render(<App />)

    // Check loading indicator (加载中...)
    expect(screen.getByText(/加载中/i)).toBeInTheDocument()

    // Resolve the pending promise and wait for loading to finish
    resolveConfig({ ok: true })
    await waitFor(() => {
      expect(screen.queryByText(/加载中/i)).not.toBeInTheDocument()
    })
  })

  it('should handle API endpoints and render correct mock data on mount', async () => {
    render(<App />)

    await waitFor(() => {
      // Check that standard active models are rendered on the screen
      expect(screen.getByText('GPT-4o')).toBeInTheDocument()
      expect(screen.getByText('Claude 3.5 Sonnet')).toBeInTheDocument()
    })

    // Verify all major API endpoints were queried
    const calledUrls = mockFetch.mock.calls.map(call => call[0])
    expect(calledUrls.some(url => url.includes('/api/models'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/scores?period=latest'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/degradations'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/alerts'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/global-index'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/provider-reliability'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/recommendations'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/sync-status'))).toBe(true)
    expect(calledUrls.some(url => url.includes('/api/config'))).toBe(true)
  })

  it('should filter out blocked models correctly', async () => {
    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('GPT-4o')).toBeInTheDocument()
    })

    // 'Blocked Model' was marked blocked in config.blocked_models. It should NOT render.
    expect(screen.queryByText('Blocked Model')).not.toBeInTheDocument()
  })

  it('should change period parameter and trigger a new historical scores request', async () => {
    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('GPT-4o')).toBeInTheDocument()
    })

    // Click on 24 hours period button
    const button24h = screen.getByRole('button', { name: /24小时/i })
    fireEvent.click(button24h)

    // Wait for the new API call
    await waitFor(() => {
      const calledUrls = mockFetch.mock.calls.map(call => call[0])
      expect(calledUrls.some(url => url.includes('/api/scores?period=24h'))).toBe(true)
    })
  })

  it('should handle API errors gracefully and log them without crashing', async () => {
    // Make /api/scores fail
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/api/scores?period=latest')) {
        return Promise.reject(new Error('Network error'))
      }
      return defaultMockFetchImplementation(url)
    })

    render(<App />)

    // Verify it registers the console.error
    await waitFor(() => {
      expect(console.error).toHaveBeenCalledWith('Fetch error:', expect.any(Error))
    })
  })

  it('should filter out the 4 deep test dimensions from radar charts and detail view', async () => {
    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('GPT-4o')).toBeInTheDocument()
    })

    // Click on '模型详情' tab to view model cards and radar charts
    const detailTabButton = screen.getByRole('button', { name: /模型详情/i })
    fireEvent.click(detailTabButton)

    // Wait for the detail view / cards to render
    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /查看历史趋势/i }).length).toBeGreaterThan(0)
    })

    // Find radar charts in the document
    const radars = screen.getAllByTestId('mock-echarts')
    expect(radars.length).toBeGreaterThan(0)

    // Analyze radar option configurations
    radars.forEach(radar => {
      const optionsStr = radar.getAttribute('data-options')
      if (optionsStr) {
        const options = JSON.parse(optionsStr)
        // If it's a radar chart (has radar indicators)
        if (options.radar && options.radar.indicator) {
          const indicatorNames = options.radar.indicator.map((ind: any) => ind.name)
          // Indicator names should not contain the localized names of the 4 deep test dimensions
          // memoryRetention -> 记忆保持, hallucinationRate -> 幻觉率, planCoherence -> 规划连贯, contextWindow -> 上下文窗口
          expect(indicatorNames).not.toContain('记忆保持')
          expect(indicatorNames).not.toContain('幻觉率')
          expect(indicatorNames).not.toContain('规划连贯')
          expect(indicatorNames).not.toContain('上下文窗口')

          // Should display core 9 dimensions
          expect(indicatorNames).toContain('正确性')
          expect(indicatorNames).toContain('代码质量')
          expect(indicatorNames).toContain('安全性')
        }
      }
    })

    // Find the '查看历史趋势' button for GPT-4o and click it to open details view
    const trendButtons = screen.getAllByRole('button', { name: /查看历史趋势/i })
    fireEvent.click(trendButtons[0]) // click the first one (for GPT-4o/Claude)

    await waitFor(() => {
      // Check details container appears, it should query history
      const calledUrls = mockFetch.mock.calls.map(call => call[0])
      expect(calledUrls.some(url => url.includes('/api/model/history'))).toBe(true)
    })

    // Ensure the 4 deep test dimension buttons are NOT rendered under detailed view axis selector
    expect(screen.queryByRole('button', { name: '记忆保持' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '幻觉率' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '规划连贯' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '上下文窗口' })).not.toBeInTheDocument()

    // The core 9 dimension buttons should be rendered
    expect(screen.getAllByRole('button', { name: '正确性' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('button', { name: '代码质量' }).length).toBeGreaterThan(0)
  })
})
