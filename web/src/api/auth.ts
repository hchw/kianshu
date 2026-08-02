import { api } from './client'

export interface AuthResult {
  token: string
  id: number
  username: string
}

export async function register(username: string, password: string) {
  const { data } = await api.post('/auth/register', { username, password })
  return data
}

export async function login(username: string, password: string) {
  const { data } = await api.post<AuthResult>('/auth/login', { username, password })
  return data
}

export async function logout() {
  await api.post('/auth/logout')
}
