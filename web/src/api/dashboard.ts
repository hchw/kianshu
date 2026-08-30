import { api } from './client'

export interface DashboardSummary { test_sets: number; flows: number; units: number; recent_runs: number; successful_runs: number; failed_runs: number; enabled_schedules: number; providers: number; models: number }
export interface DashboardStep { key: string; title: string; description: string; done: boolean; target: string }
export interface DashboardRecentWork { kind: string; id: number; name: string; test_set_id: number; test_set_name: string; updated_at: string; role: 'owner' | 'edit' | 'read'; target: string }
export interface DashboardAttention { kind: string; title: string; description: string; target: string; can_edit: boolean }
export interface DashboardData { summary: DashboardSummary; onboarding: DashboardStep[]; recent_work: DashboardRecentWork[]; attention_items: DashboardAttention[] }

export async function getDashboard() { const { data } = await api.get<DashboardData>('/dashboard'); return data }
