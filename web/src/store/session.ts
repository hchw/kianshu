import { getToken, setToken } from '../api/client'

export interface SessionUser {
  id: number
  username: string
}

let user: SessionUser | null = null

export function currentUser(): SessionUser | null {
  return user
}

export function setSession(token: string, u: SessionUser) {
  setToken(token)
  user = u
  localStorage.setItem('kianshu_user', JSON.stringify(u))
}

export function restoreSession(): SessionUser | null {
  const raw = localStorage.getItem('kianshu_user')
  if (raw) {
    try {
      user = JSON.parse(raw)
    } catch {
      user = null
    }
  }
  return user
}

export function clearSession() {
  setToken('')
  user = null
  localStorage.removeItem('kianshu_user')
}

export function isAuthed(): boolean {
  return !!getToken()
}
