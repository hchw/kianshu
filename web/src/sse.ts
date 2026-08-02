import type { AgentEvent } from './api/agent'

export interface SSESessionHandlers {
  onEvent: (ev: AgentEvent) => void
  onDone: () => void
  onError: (msg: string) => void
  onDisconnect: () => void
}

// openSSE consumes the /agent/submit endpoint as Server-Sent Events via fetch,
// parsing `data: <json>` frames. [DONE] terminates the stream; a network error
// triggers onDisconnect so the caller can offer session-history recovery.
export function openSSE(
  url: string,
  body: Record<string, unknown>,
  h: SSESessionHandlers,
): { abort: () => void } {
  const controller = new AbortController()

  const run = async () => {
    try {
      const resp = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'text/event-stream',
          Authorization: `Bearer ${localStorage.getItem('kianshu_token') ?? ''}`,
        },
        body: JSON.stringify(body),
        signal: controller.signal,
      })
      if (!resp.ok || !resp.body) {
        const errText = await resp.text().catch(() => '')
        h.onError(errText || `请求失败 (${resp.status})`)
        h.onDone()
        return
      }
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
          handleFrame(frame, h)
        }
      }
      h.onDone()
    } catch (e) {
      if ((e as Error).name === 'AbortError') return
      h.onError((e as Error).message)
      h.onDisconnect()
    }
  }
  run()
  return { abort: () => controller.abort() }
}

function handleFrame(frame: string, h: SSESessionHandlers) {
  for (const line of frame.split('\n')) {
    if (!line.startsWith('data: ')) continue
    const payload = line.slice(6)
    if (payload === '[DONE]') {
      h.onDone()
      continue
    }
    try {
      h.onEvent(JSON.parse(payload))
    } catch {
      // non-JSON data frame; ignore
    }
  }
}

// parseSSELine is the pure parser used by unit tests.
export function parseSSELine(line: string): { event: AgentEvent } | { done: true } | null {
  if (!line.startsWith('data: ')) return null
  const payload = line.slice(6)
  if (payload === '[DONE]') return { done: true }
  try {
    return { event: JSON.parse(payload) }
  } catch {
    return null
  }
}
