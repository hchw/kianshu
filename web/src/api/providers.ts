import { api } from './client'

export interface Provider {
  id: number
  name: string
  base_url: string
  model: string
  enabled: boolean
}

export interface ProviderReq {
  name: string
  base_url: string
  api_key?: string
  model?: string
  enabled?: boolean
}

export async function listProviders() {
  const { data } = await api.get<{ providers: Provider[] }>('/providers')
  return data.providers
}

export async function createProvider(req: ProviderReq) {
  const { data } = await api.post<Provider>('/providers', req)
  return data
}

export async function updateProvider(id: number, req: ProviderReq) {
  const { data } = await api.patch<Provider>(`/providers/${id}`, req)
  return data
}

export async function deleteProvider(id: number) {
  const { data } = await api.delete(`/providers/${id}`)
  return data
}

export async function testProvider(id: number) {
  const { data } = await api.post<{ ok: boolean; error?: string }>(`/providers/${id}/test`)
  return data
}
