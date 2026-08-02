import { describe, expect, it } from 'vitest'
import { parseNodeResults, statusLabel } from './ResultsPanel'

describe('parseNodeResults', () => {
  it('returns empty array for empty string', () => {
    expect(parseNodeResults('')).toEqual([])
  })

  it('returns array as-is when input is JSON array', () => {
    const raw = JSON.stringify([{ node_id: 'n1', status: 'ok' }])
    expect(parseNodeResults(raw)).toEqual([{ node_id: 'n1', status: 'ok' }])
  })

  it('returns parsed.results when object has results array', () => {
    const raw = JSON.stringify({ results: [{ node_id: 'n1', status: 'ok' }] })
    expect(parseNodeResults(raw)).toEqual([{ node_id: 'n1', status: 'ok' }])
  })

  it('converts map-style object to array with node_id from key', () => {
    const raw = JSON.stringify({
      n1: { status: 'ok', output: {} },
      n2: { status: 'failed', error: 'boom' },
    })
    expect(parseNodeResults(raw)).toEqual([
      { node_id: 'n1', status: 'ok', output: {} },
      { node_id: 'n2', status: 'failed', error: 'boom' },
    ])
  })

  it('uses existing node_id in map value if present', () => {
    const raw = JSON.stringify({ k1: { node_id: 'explicit', status: 'ok' } })
    expect(parseNodeResults(raw)).toEqual([{ node_id: 'explicit', status: 'ok' }])
  })

  it('returns empty array for invalid JSON', () => {
    expect(parseNodeResults('not json')).toEqual([])
  })
})

describe('statusLabel', () => {
  it('maps ok to 通过', () => {
    expect(statusLabel('ok')).toBe('通过')
  })
  it('maps failed to 失败', () => {
    expect(statusLabel('failed')).toBe('失败')
  })
  it('maps soft-stop to 通过', () => {
    expect(statusLabel('soft-stop')).toBe('通过')
  })
  it('passes through unknown statuses', () => {
    expect(statusLabel('unknown')).toBe('unknown')
  })
})
