import axios from 'axios'

const TOKEN_KEY = 'kianshu_token'

export const api = axios.create({
  baseURL: '/api',
})

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setToken(token: string) {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

api.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  console.log(`[api] -> ${config.method?.toUpperCase()} ${config.url}`, config.params ?? '', config.data ?? '')
  return config
})

api.interceptors.response.use(
  (resp) => {
    console.log(`[api] <- ${resp.status} ${resp.config.method?.toUpperCase()} ${resp.config.url}`, resp.data)
    return resp
  },
  (error) => {
    console.error(`[api] <- ${error.response?.status ?? 'ERR'} ${error.config?.method?.toUpperCase()} ${error.config?.url}`, error.response?.data ?? error.message)
    if (error.response?.status === 401) {
      setToken('')
      if (!window.location.pathname.startsWith('/login')) {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  },
)

export function apiError(e: unknown): string {
  const resp = (e as { response?: { data?: { error?: string } } })?.response?.data
  return resp?.error ?? '请求失败'
}
