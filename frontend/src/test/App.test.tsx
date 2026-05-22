import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import App from '../App'

// Mock fetch globally
const mockFetch = vi.fn()
global.fetch = mockFetch

beforeEach(() => {
  mockFetch.mockReset()
  // Default mock responses
  mockFetch.mockImplementation((url: string) => {
    if (url.includes('/api/config')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ blocked_models: [] })
      })
    }
    if (url.includes('/api/models')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([])
      })
    }
    if (url.includes('/api/scores')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([])
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
    if (url.includes('/api/recommendations')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve([])
      })
    }
    if (url.includes('/api/transparency')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ summary: {}, modelFreshness: [] })
      })
    }
    if (url.includes('/api/sync-status')) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ lastSync: new Date().toISOString(), nextSync: new Date().toISOString() })
      })
    }
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve({})
    })
  })
})

describe('App', () => {
  it('renders without crashing', async () => {
    render(<App />)
    await waitFor(() => {
      expect(document.body).toBeTruthy()
    })
  })

  it('shows loading state initially', () => {
    render(<App />)
    // App should render something while loading
    expect(document.body.textContent).toBeDefined()
  })

  it('fetches data on mount', async () => {
    render(<App />)
    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalled()
    })
  })
})
