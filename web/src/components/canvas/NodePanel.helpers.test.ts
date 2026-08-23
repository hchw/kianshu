import { describe, expect, it } from 'vitest'
import type { FlowTree } from '../../api/flow'
import { collectAncestorKeys, resolveSourceCandidates, groupApiParams } from './NodePanel'

function makeTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n_login'], config: { params: { username: 'u', password: 'p' } } },
      n_login: {
        id: 'n_login',
        type: 'api',
        parent: 'n1',
        children: ['n_cache'],
        outputs: { token: { type: 'string' } },
        config: { unit_id: 1 },
      },
      n_cache: {
        id: 'n_cache',
        type: 'cache-set',
        parent: 'n_login',
        children: ['n_sign'],
        config: { writes: { refresh: 'body.refresh', token: 'body.token' } },
      },
      n_sign: {
        id: 'n_sign',
        type: 'api',
        parent: 'n_cache',
        inputs: {
          'X-Timestamp': { type: 'integer', source: 'sign_ts' },
          auth: { type: 'string', source: '$cache.token' },
        },
        config: {
          unit_id: 2,
          unit: {
            method: 'POST',
            path: '/utils/sign',
            params: '[{"name":"X-Timestamp","in":"header","required":true,"type":"integer"},{"name":"X-Nonce","in":"header","type":"string"}]',
          },
        },
      },
    },
  }
}

describe('collectAncestorKeys', () => {
  it('collects ancestor outputs keys and cache keys, deduped', () => {
    const keys = collectAncestorKeys(makeTree(), 'n_sign')
    expect(keys).toEqual(['token', 'refresh'])
    // 去重后只保留一次 token
    expect(keys.filter((k) => k === 'token')).toHaveLength(1)
  })

  it('returns empty for root', () => {
    expect(collectAncestorKeys(makeTree(), 'n1')).toEqual([])
  })
})

describe('resolveSourceCandidates', () => {
  it('merges ancestor outputs, cache keys and existing sources', () => {
    const cands = resolveSourceCandidates(makeTree(), 'n_sign')
    // 祖先 outputs: token (from n_login)
    expect(cands).toContain('token')
    // cache 键带前缀
    expect(cands).toContain('$cache.refresh')
    expect(cands).toContain('$cache.token')
    // 已有 source
    expect(cands).toContain('sign_ts')
    expect(cands).toContain('$cache.token')
  })
})

describe('groupApiParams', () => {
  it('groups by declared in, marks override and explicit header', () => {
    const tree = makeTree()
    const n = tree.nodes.n_sign!
    const groups = groupApiParams(
      { 'X-Timestamp': '1780000000' },
      { 'X-Api-Key': 'k123' },
      n.inputs ?? {},
      (n.config as { unit?: { params?: string } }).unit?.params,
    )
    // header 组包含声明参数 X-Timestamp（已覆盖值）和显式头 X-Api-Key
    const headerKeys = groups.header!.map((r) => r.key)
    expect(headerKeys).toContain('X-Timestamp')
    expect(headerKeys).toContain('X-Api-Key')
    const tsRow = groups.header!.find((r) => r.key === 'X-Timestamp')!
    expect(tsRow.hasValue).toBe(true)
    expect(tsRow.value).toBe('1780000000')
    expect(tsRow.declared).toBe(true)
    const apiKeyRow = groups.header!.find((r) => r.key === 'X-Api-Key')!
    expect(apiKeyRow.isExplicitHeader).toBe(true)
    expect(apiKeyRow.explicit).toBe('k123')
  })

  it('routes undeclared param to other group and preserves source', () => {
    const tree = makeTree()
    const n = tree.nodes.n_sign!
    const groups = groupApiParams({}, {}, n.inputs ?? {}, '')
    const otherRow = groups.default!.find((r) => r.key === 'auth')!
    expect(otherRow).toBeDefined()
    expect(otherRow.declared).toBe(false)
    expect(otherRow.source).toBe('$cache.token')
  })
})
