import { api } from './client'

export interface IOKey {
  type: string
  desc?: string
  source?: string
  from?: string
}

export interface FlowNode {
  id: string
  type: string
  parent?: string
  children?: string[]
  inputs?: Record<string, IOKey>
  outputs?: Record<string, IOKey>
  config?: unknown
  /** Optional canvas coordinates; absent = auto-layout (pure presentation). */
  x?: number
  y?: number
}

export interface FlowTree {
  start: string
  nodes: Record<string, FlowNode>
}

export interface Draft {
  flow_id: number
  test_set_id: number
  name: string
  tree: string
  system_prompt: string
  thinking?: string
}

export interface FlowVersion {
  id: number
  flow_id: number
  version_no: number
  tree: string
  system_prompt: string
  enabled: boolean
  created_by: number
  created_at: string
}

export interface ValidationErrorItem {
  node_id?: string
  code: string
  level: string
  message: string
  expected_format?: string
}

export interface ValidationResult {
  /** 后端可能返回 null（Go nil slice 序列化），消费端必须防御。 */
  errors: ValidationErrorItem[] | null
  warnings: ValidationErrorItem[] | null
}

export async function getDraft(flowID: number) {
  const { data } = await api.get<Draft>(`/flow/flows/${flowID}/draft`)
  return data
}

export async function updateDraft(
  flowID: number,
  name: string,
  tree: FlowTree,
  systemPrompt?: string,
  thinking?: string,
) {
  const { data } = await api.put<Draft>(`/flow/flows/${flowID}/draft`, {
    name,
    tree,
    system_prompt: systemPrompt,
    thinking,
  })
  return data
}

export async function updateFlowThinking(flowID: number, thinking: string) {
  const { data } = await api.patch(`/flow/flows/${flowID}/thinking`, {
    thinking,
  })
  return data
}

export async function validateDraft(flowID: number) {
  const { data } = await api.post<ValidationResult>(`/flow/flows/${flowID}/draft/validate`)
  return data
}

export async function deleteFlow(flowID: number) {
  const { data } = await api.delete(`/flow/flows/${flowID}`)
  return data
}

export async function duplicateFlow(flowID: number, name: string) {
  const { data } = await api.post(`/flow/flows/${flowID}/duplicate`, { name })
  return data
}

export async function renameFlow(flowID: number, name: string) {
  const { data } = await api.patch(`/flow/flows/${flowID}`, { name })
  return data
}

export async function trialRun(flowID: number) {
  const { data } = await api.post(`/flow/flows/${flowID}/draft/trial-run`)
  return data
}

export async function saveEnable(flowID: number) {
  const { data } = await api.post<FlowVersion>(`/flow/flows/${flowID}/versions`)
  return data
}

export async function listVersions(flowID: number) {
  const { data } = await api.get<{ versions: FlowVersion[] }>(`/flow/flows/${flowID}/versions`)
  return data.versions
}

export async function getVersion(flowID: number, versionNo: number) {
  const { data } = await api.get<FlowVersion>(`/flow/flows/${flowID}/versions/${versionNo}`)
  return data
}

export async function runVersion(flowID: number, versionNo: number) {
  const { data } = await api.post(`/flow/flows/${flowID}/versions/${versionNo}/run`)
  return data
}

export interface RunLog {
  id: number
  flow_id: number
  version_id: number
  version_no: number
  status: string
  tree: string
  node_results: string
  started_at: string
  finished_at: string
}

export async function listRuns(flowID: number, page = 1, pageSize = 20) {
  const { data } = await api.get<{ runs: RunLog[]; total: number; page: number; page_size: number }>(
    `/flow/flows/${flowID}/runs`,
    { params: { page, page_size: pageSize } },
  )
  return data
}

export async function getRun(flowID: number, runID: number) {
  const { data } = await api.get<RunLog>(`/flow/flows/${flowID}/runs/${runID}`)
  return data
}

/** SSE connection that streams new execution logs in real time. */
export function subscribeRuns(
  flowID: number,
  onRun: (run: RunLog) => void,
): { close: () => void } {
  const controller = new AbortController()
  const token = localStorage.getItem('kianshu_token') ?? ''

  const run = async () => {
    try {
      const resp = await fetch(`/api/flow/flows/${flowID}/runs/subscribe`, {
        headers: {
          Authorization: `Bearer ${token}`,
          Accept: 'text/event-stream',
        },
        signal: controller.signal,
      })
      if (!resp.ok || !resp.body) return
      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        let idx: number
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          for (const line of frame.split('\n')) {
            if (!line.startsWith('data: ')) continue
            const payload = line.slice(6)
            try {
              onRun(JSON.parse(payload))
            } catch {
              // ignore non-JSON
            }
          }
        }
      }
    } catch {
      // connection closed or aborted
    }
  }
  run()
  return { close: () => controller.abort() }
}
