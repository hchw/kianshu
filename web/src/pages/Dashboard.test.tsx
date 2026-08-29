import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from './Dashboard'

vi.mock('../api/dashboard', () => ({ getDashboard: vi.fn(async () => ({
  summary: { test_sets: 0, flows: 0, units: 0, recent_runs: 0, failed_runs: 0, enabled_schedules: 0 },
  onboarding: [{ key: 'test-set', title: '创建测试集', description: '开始', done: false, target: '/test-sets' }],
  recent_work: [], attention_items: [],
})) }))

vi.mock('../store/session', () => ({ currentUser: () => ({ id: 1, username: '测试用户' }), restoreSession: () => ({ id: 1, username: '测试用户' }), clearSession: vi.fn() }))

describe('Dashboard', () => {
  it('shows onboarding and empty state for a new user', async () => {
    render(<MemoryRouter><Dashboard /></MemoryRouter>)
    await waitFor(() => expect(screen.getByText('创建测试集')).toBeTruthy())
    expect(screen.getByText('还没有最近工作，创建一个测试集开始吧。')).toBeTruthy()
  })
})
