import { api } from './client'

export interface FlowSchedule {
  id: number
  flow_id: number
  test_set_id: number
  cron: string
  enabled: boolean
  job_id: string
  created_at: string
  updated_at: string
}

export async function listSchedules(flowID: number) {
  const { data } = await api.get<{ schedules: FlowSchedule[] }>(`/flow/flows/${flowID}/schedules`)
  return data.schedules
}

export async function createSchedule(flowID: number, cron: string) {
  const { data } = await api.post<FlowSchedule>(`/flow/flows/${flowID}/schedules`, { cron })
  return data
}

export async function updateSchedule(flowID: number, scheduleID: number, cron: string) {
  const { data } = await api.patch<FlowSchedule>(`/flow/flows/${flowID}/schedules/${scheduleID}`, { cron })
  return data
}

export async function setScheduleEnabled(flowID: number, scheduleID: number, enabled: boolean) {
  const { data } = await api.patch<FlowSchedule>(`/flow/flows/${flowID}/schedules/${scheduleID}/enabled`, { enabled })
  return data
}

export async function deleteSchedule(flowID: number, scheduleID: number) {
  const { data } = await api.delete(`/flow/flows/${flowID}/schedules/${scheduleID}`)
  return data
}
