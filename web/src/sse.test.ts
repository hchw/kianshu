import { describe, expect, it } from 'vitest'
import { parseSSELine } from './sse'

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
