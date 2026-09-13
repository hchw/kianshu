import { api } from './client'

export interface BackgroundDocument {
  id: number
  test_set_id: number
  name: string
  content: string
  created_by: number
}

export interface CaseSourceInput {
  kind: 'document' | 'all' | 'tag'
  document_id?: number
  tags?: string[]
}

export interface CaseFlow {
  id: number
  test_set_id: number
  name: string
  created_by: number
}

export interface CaseNode {
  id: string
  title: string
  description?: string
  precondition?: string
  input?: string
  expected?: string
  status: string
  x?: number
  y?: number
  children?: CaseNode[]
}

export interface CaseTree {
  root: CaseNode | null
}

export interface CaseDraft {
  id: number
  case_flow_id: number
  tree: string
  revision: number
}

export interface CaseTreeView {
  draft: CaseDraft
  tree: CaseTree
}

export interface CaseFlowVersion {
  id: number
  case_flow_id: number
  version_no: number
  tree: string
  sources: string
  created_by: number
  created_at: string
}

export interface CaseSource {
  id: number
  case_flow_id: number
  kind: string
  document_id: number
  scope: string
}

export async function listBackgroundDocs(testSetID: number) {
  const { data } = await api.get<{ documents: BackgroundDocument[] }>(`/test-sets/${testSetID}/background-documents`)
  return data.documents
}

export async function createBackgroundDoc(testSetID: number, name: string, content: string) {
  const { data } = await api.post<BackgroundDocument>(`/test-sets/${testSetID}/background-documents`, { name, content })
  return data
}

export async function updateBackgroundDoc(testSetID: number, docID: number, payload: { name?: string; content?: string }) {
  const { data } = await api.patch<BackgroundDocument>(`/test-sets/${testSetID}/background-documents/${docID}`, payload)
  return data
}

export async function deleteBackgroundDoc(testSetID: number, docID: number) {
  const { data } = await api.delete(`/test-sets/${testSetID}/background-documents/${docID}`)
  return data
}

export async function listCaseFlows(testSetID: number) {
  const { data } = await api.get<{ case_flows: CaseFlow[] }>(`/test-sets/${testSetID}/case-flows`)
  return data.case_flows
}

export async function createCaseFlow(testSetID: number, name: string, sources: CaseSourceInput[]) {
  const { data } = await api.post<CaseFlow>(`/test-sets/${testSetID}/case-flows`, { name, sources })
  return data
}

export async function getCaseTreeView(caseFlowID: number) {
  const { data } = await api.get<CaseTreeView>(`/case-flows/${caseFlowID}/draft`)
  return data
}

export async function getCaseFlow(caseFlowID: number) {
  const { data } = await api.get<CaseFlow>(`/case-flows/${caseFlowID}`)
  return data
}

export async function addCaseNode(caseFlowID: number, revision: number, parentID: string, title: string) {
  const { data } = await api.post<CaseDraft>(`/case-flows/${caseFlowID}/nodes`, { revision, parent_id: parentID, title })
  return data
}

export type CaseNodeUpdate = Partial<Pick<CaseNode, 'title' | 'description' | 'precondition' | 'input' | 'expected' | 'x' | 'y'>>

export async function updateCaseNode(caseFlowID: number, nodeID: string, revision: number, update: CaseNodeUpdate) {
  const { data } = await api.patch<CaseDraft>(`/case-flows/${caseFlowID}/nodes/${nodeID}`, { revision, ...update })
  return data
}

export async function deleteCaseNode(caseFlowID: number, nodeID: string, revision: number) {
  const { data } = await api.delete<CaseDraft>(`/case-flows/${caseFlowID}/nodes/${nodeID}`, { data: { revision } })
  return data
}

export async function setCaseNodeStatus(caseFlowID: number, nodeID: string, revision: number, status: string) {
  const { data } = await api.patch<CaseDraft>(`/case-flows/${caseFlowID}/nodes/${nodeID}/status`, { revision, status })
  return data
}

export async function saveCaseFlowVersion(caseFlowID: number) {
  const { data } = await api.post<CaseFlowVersion>(`/case-flows/${caseFlowID}/versions`)
  return data
}

export async function listCaseFlowVersions(caseFlowID: number) {
  const { data } = await api.get<{ versions: CaseFlowVersion[] }>(`/case-flows/${caseFlowID}/versions`)
  return data.versions
}

export async function restoreCaseFlowVersion(caseFlowID: number, versionNo: number) {
  const { data } = await api.post<CaseDraft>(`/case-flows/${caseFlowID}/versions/${versionNo}/restore`)
  return data
}

export async function listCaseSources(caseFlowID: number) {
  const { data } = await api.get<{ sources: CaseSource[] }>(`/case-flows/${caseFlowID}/sources`)
  return data.sources
}

export async function addCaseSource(caseFlowID: number, source: CaseSourceInput) {
  const { data } = await api.post(`/case-flows/${caseFlowID}/sources`, source)
  return data
}

export async function removeCaseSource(caseFlowID: number, sourceID: number) {
  const { data } = await api.delete(`/case-flows/${caseFlowID}/sources/${sourceID}`)
  return data
}

export async function renameCaseFlow(caseFlowID: number, name: string) {
  const { data } = await api.patch<CaseFlow>(`/case-flows/${caseFlowID}`, { name })
  return data
}

export async function deleteCaseFlow(caseFlowID: number) {
  await api.delete(`/case-flows/${caseFlowID}`)
}

export async function duplicateCaseFlow(caseFlowID: number, name?: string) {
  const { data } = await api.post<CaseFlow>(`/case-flows/${caseFlowID}/duplicate`, { name })
  return data
}

export async function listCaseNodeFlows(caseFlowID: number, nodeID: string) {
  const { data } = await api.get<{ node: CaseNodeStatus; flows: CaseNodeFlow[] }>(`/case-flows/${caseFlowID}/nodes/${nodeID}/flows`)
  return data
}

export interface CaseNodeStatus {
  status: string
  last_run_result: string
  last_run_at?: string
}

export interface CaseNodeFlow {
  flow_id: number
  flow_name: string
  flow_version_id: number
  version_no: number
  anchor_node_id: string
  case_version_no: number
  enabled: boolean
}

/** 一个来源用例流 + 版本 + 选中的子树根。 */
export interface CaseSelection {
  case_flow_id: number
  version_no?: number
  root_ids: string[]
}

/** 基于用例流创建执行流（绑定来源，生成前即可追溯）。 */
export async function createFlowFromCases(testSetID: number, name: string, selections: CaseSelection[], systemPrompt?: string) {
  const { data } = await api.post<{ id: number; name: string }>(`/test-sets/${testSetID}/flows`, {
    name,
    system_prompt: systemPrompt,
    case_selections: selections,
  })
  return data
}

/** 对已绑定的执行流运行用例驱动的生成（SSE 由调用方处理）。 */
export async function generateFlowFromCases(flowID: number, providerID: number, instruction?: string) {
  const { data } = await api.post(`/flow/flows/${flowID}/generate-from-cases`, { provider_id: providerID, instruction })
  return data
}

/** 保存并启用版本，同时固化用例映射与覆盖状态。 */
export async function saveFlowFromCases(flowID: number) {
  const { data } = await api.post(`/flow/flows/${flowID}/save-from-cases`)
  return data
}

export interface FlowCaseSources {
  bound: boolean
  binding?: {
    sources: { case_flow_id: number; name: string; version_no: number }[]
    leaves: { case_flow_id: number; case_node_id: string; title: string; path?: string }[]
  }
}

/** 执行流 → 来源用例流/版本/用例。 */
export async function getFlowCaseSources(flowID: number) {
  const { data } = await api.get<FlowCaseSources>(`/flow/flows/${flowID}/case-sources`)
  return data
}

export async function downloadCaseFlowXMind(caseFlowID: number, version?: number) {
  const { data, headers } = await api.get<Blob>(`/case-flows/${caseFlowID}/export`, {
    params: version ? { version } : undefined,
    responseType: 'blob',
  })
  const disposition = String(headers['content-disposition'] ?? '')
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  const fallback = disposition.match(/filename="?([^";]+)"?/i)?.[1]
  const filename = encoded ? decodeURIComponent(encoded) : (fallback ?? `case-flow-${caseFlowID}.xmind`)
  const url = URL.createObjectURL(data)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export interface CaseAgentSession {
  status: string
  messages: string
  pending_questions?: string
}

export async function caseAgentSession(caseFlowID: number) {
  const { data } = await api.get<CaseAgentSession>(`/case-flows/${caseFlowID}/agent/session`)
  return data
}

export async function caseAgentNew(caseFlowID: number) {
  const { data } = await api.post(`/case-flows/${caseFlowID}/agent/new`)
  return data
}

export async function caseAgentCompress(caseFlowID: number) {
  const { data } = await api.post(`/case-flows/${caseFlowID}/agent/compress`)
  return data
}

export async function caseAgentResume(caseFlowID: number, providerID: number, answers: { question_id: string; answer: string }[], thinking?: string) {
  const { data } = await api.post(`/case-flows/${caseFlowID}/agent/resume`, { provider_id: providerID, answers, thinking })
  return data
}

export async function submitCaseAgent(caseFlowID: number, providerID: number, instruction: string, thinking?: string) {
  const { data } = await api.post(`/case-flows/${caseFlowID}/agent/submit`, { provider_id: providerID, instruction, thinking })
  return data
}
