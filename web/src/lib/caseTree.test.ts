import { describe, expect, it } from 'vitest'
import { dedupeSubtreeRoots } from './caseTree'
import type { CaseNode } from '../api/caseFlow'

const tree: CaseNode = {
  id: 'root', title: '根', status: 'uncovered', children: [
    { id: 'login', title: '登录场景', status: 'uncovered', children: [
      { id: 'ok', title: '正常登录', status: 'uncovered' },
      { id: 'bad', title: '异常密码', status: 'uncovered' },
    ] },
    { id: 'order', title: '下单', status: 'uncovered' },
  ],
}

describe('dedupeSubtreeRoots', () => {
  it('suppresses nested roots and counts covered nodes', () => {
    const { roots, nodes } = dedupeSubtreeRoots(tree, ['login', 'ok'])
    expect(roots).toEqual(['login'])
    expect(nodes).toBe(3)
  })

  it('keeps disjoint roots', () => {
    const { roots, nodes } = dedupeSubtreeRoots(tree, ['ok', 'order'])
    expect(roots).toEqual(['ok', 'order'])
    expect(nodes).toBe(2)
  })

  it('returns empty for no selection', () => {
    expect(dedupeSubtreeRoots(tree, [])).toEqual({ roots: [], nodes: 0 })
    expect(dedupeSubtreeRoots(null, ['x'])).toEqual({ roots: [], nodes: 0 })
  })
})
