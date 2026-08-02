import { api } from './client'

export interface TestSet {
  id: number
  name: string
  owner_id: number
  host: string
  created_at: string
}

export interface TestUnit {
  id: number
  method: string
  path: string
  slug: string
  tag: string
  name: string
  security: string
  deleted_at: string | null
}

export interface FlowSummary {
  id: number
  name: string
}

export async function listTestSets() {
  const { data } = await api.get<{ test_sets: TestSet[] }>('/test-sets')
  return data.test_sets
}

export async function createTestSet(name: string) {
  const { data } = await api.post<TestSet>('/test-sets', { name })
  return data
}

export async function getTestSet(id: number) {
  const { data } = await api.get<TestSet>(`/test-sets/${id}`)
  return data
}

export async function updateTestSet(id: number, host: string) {
  const { data } = await api.patch<TestSet>(`/test-sets/${id}`, { host })
  return data
}

export async function addMember(testSetID: number, userID: number, role: string) {
  const { data } = await api.post(`/test-sets/${testSetID}/members`, { user_id: userID, role })
  return data
}

export async function removeMember(testSetID: number, userID: number) {
  const { data } = await api.delete(`/test-sets/${testSetID}/members/${userID}`)
  return data
}

export async function importSwagger(testSetID: number, source: string, content: string, confirm = false) {
  const { data } = await api.post(`/test-sets/${testSetID}/imports`, { source, content, confirm })
  return data
}

export async function listUnits(testSetID: number, filter?: { tag?: string; q?: string }) {
  const params: Record<string, string> = {}
  if (filter?.tag) params.tag = filter.tag
  if (filter?.q) params.q = filter.q
  const { data } = await api.get<{ units: TestUnit[] }>(`/test-sets/${testSetID}/units`, { params })
  return data.units
}

export async function deleteUnit(testSetID: number, unitID: number) {
  const { data } = await api.delete(`/test-sets/${testSetID}/units/${unitID}`)
  return data
}

export async function listFlows(testSetID: number) {
  const { data } = await api.get<{ flows: FlowSummary[] }>(`/test-sets/${testSetID}/flows`)
  return data.flows
}

export async function createFlow(testSetID: number, name: string) {
  const { data } = await api.post<FlowSummary>(`/test-sets/${testSetID}/flows`, { name })
  return data
}
