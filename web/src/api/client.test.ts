import { describe, expect, it } from 'vitest'
import { api, apiError, getToken, setToken } from './client'

describe('client token helpers', () => {
  it('round-trips the token in localStorage', () => {
    localStorage.clear()
    expect(getToken()).toBe('')
    setToken('tok-abc')
    expect(getToken()).toBe('tok-abc')
    setToken('')
    expect(getToken()).toBe('')
  })
})

describe('apiError', () => {
  it('extracts the backend error message', () => {
    const e = { response: { data: { error: '名称不能为空' } } }
    expect(apiError(e)).toBe('名称不能为空')
  })

  it('falls back to a generic message', () => {
    expect(apiError({ response: { data: {} } })).toBe('请求失败')
    expect(apiError(new Error('network'))).toBe('请求失败')
  })
})

describe('client interceptors', () => {
  it('is an axios instance pointed at /api', () => {
    expect(typeof api.get).toBe('function')
    expect(api.defaults.baseURL).toBe('/api')
  })
})
