import { describe, expect, it } from 'vitest'
import { parseParamLocations } from './NodePanel'

describe('parseParamLocations', () => {
  it('parses name->in from swagger params', () => {
    const loc = parseParamLocations(
      '[{"name":"Authorization","in":"header","type":"string"},{"name":"id","in":"path","type":"integer","required":true},{"name":"payload","in":"body","type":"object"}]',
    )
    expect(loc).toEqual({ Authorization: 'header', id: 'path', payload: 'body' })
  })

  it('handles empty/null/malformed input', () => {
    expect(parseParamLocations('')).toEqual({})
    expect(parseParamLocations('null')).toEqual({})
    expect(parseParamLocations('not json')).toEqual({})
    expect(parseParamLocations('{}')).toEqual({})
  })

  it('keeps empty in as empty string for undeclared keys', () => {
    const loc = parseParamLocations('[{"name":"q","in":""}]')
    expect(loc.q).toBe('')
  })
})
