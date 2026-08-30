import { api } from './client'

export interface GlobalRun {
  id: number; flow_id: number; test_set_id: number; test_set_name: string; flow_name: string; role: string
  version_id: number; version_no: number; status: string; node_results: string; started_at: string; finished_at: string
}
export interface RunQuery { test_set_id?: number; flow_id?: number; status?: string; from?: string; to?: string; page?: number; page_size?: number }
export async function listGlobalRuns(params: RunQuery) {
  const { data } = await api.get<{ runs: GlobalRun[]; total: number; page: number; page_size: number }>('/runs', { params })
  return data
}
