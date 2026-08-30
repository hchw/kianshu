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
  params: string
  request_body: string
  responses: string
  security: string
  spec: string
  deleted_at: string | null
  created_at: string
  updated_at: string
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

export async function deleteTestSet(id: number) {
  await api.delete(`/test-sets/${id}`)
}

export async function getTestSet(id: number) {
  const { data } = await api.get<TestSet>(`/test-sets/${id}`)
  return data
}

export async function updateTestSet(id: number, updates: { name?: string; host?: string }) {
  const { data } = await api.patch<TestSet>(`/test-sets/${id}`, updates)
  return data
}

export interface MemberView {
  user_id: number
  username: string
  role: string
}

export interface UserBrief {
  id: number
  username: string
}

export async function listMembers(testSetID: number) {
  const { data } = await api.get<{ owner: MemberView; members: MemberView[] }>(`/test-sets/${testSetID}/members`)
  return data
}

export async function searchUsers(testSetID: number, q: string) {
  const { data } = await api.get<{ users: UserBrief[] }>(`/test-sets/${testSetID}/members/search`, { params: { q } })
  return data.users
}

export async function addMember(testSetID: number, userID: number, role: string) {
  const { data } = await api.post<MemberView>(`/test-sets/${testSetID}/members`, { user_id: userID, role })
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

export async function getUnit(testSetID: number, unitID: number) {
  const { data } = await api.get<TestUnit>(`/test-sets/${testSetID}/units/${unitID}`)
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
