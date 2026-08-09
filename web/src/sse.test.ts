import { describe, expect, it, vi } from 'vitest'
import { openSSE, parseSSELine } from './sse'

// 模拟 fetch 返回指定状态码的响应
function mockFetch(status: number, body?: ReadableStream) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      body,
      text: async () => 'error body',
    }),
  )
}

describe('parseSSELine', () => {
  it('parses an event frame', () => {
    const r = parseSSELine('data: {"round":1,"tool":"list_units","result":[]}')
    expect(r).toEqual({ event: { round: 1, tool: 'list_units', result: [] } })
  })

  it('recognizes the done marker', () => {
    expect(parseSSELine('data: [DONE]')).toEqual({ done: true })
  })

  it('ignores non-data lines and garbage', () => {
    expect(parseSSELine('event: error')).toBeNull()
    expect(parseSSELine('data: not json')).toBeNull()
    expect(parseSSELine('')).toBeNull()
  })
})

describe('openSSE', () => {
  it('passes the HTTP status to onError for non-2xx responses', async () => {
    mockFetch(404)
    const onError = vi.fn()
    const onDone = vi.fn()
    openSSE('/subscribe', {}, { onEvent: vi.fn(), onDone, onError, onDisconnect: vi.fn(), onAbort: vi.fn() })
    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalledWith('error body', 404)
      expect(onDone).toHaveBeenCalled()
    })
  })
})
