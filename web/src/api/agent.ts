import { api } from './client'

export interface AgentEvent {
  round: number
  tool: string
  args?: unknown
  result: unknown
}

export interface AgentResult {
  rounds: number
  limit_reached: boolean
  finished: boolean
  message?: string
  events: AgentEvent[]
}

export interface PauseQuestion {
  id: string
  type: string
  question: string
  options?: string[]
}

export interface PauseAnswer {
  question_id: string
  answer: string
}

export interface AgentSession {
  status: string
  messages: unknown[]
  pending_questions: PauseQuestion[]
}

export interface AgentSubmitReq {
  provider_id: number
  instruction: string
  selected_nodes?: string[]
  mode: 'edit' | 'generate'
}

export async function agentSession(flowID: number) {
  const { data } = await api.get<AgentSession>(`/flow/flows/${flowID}/agent/session`)
  return data
}

export async function agentSubmit(flowID: number, req: AgentSubmitReq) {
  const { data } = await api.post<AgentResult>(`/flow/flows/${flowID}/agent/submit`, req)
  return data
}

export async function agentResume(flowID: number, providerID: number, answers: PauseAnswer[]) {
  const { data } = await api.post<AgentResult>(`/flow/flows/${flowID}/agent/resume`, {
    provider_id: providerID,
    answers,
  })
  return data
}

export async function agentNew(flowID: number) {
  const { data } = await api.post(`/flow/flows/${flowID}/agent/new`)
  return data
}
