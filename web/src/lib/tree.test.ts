import { describe, expect, it } from 'vitest'
import {
  layoutTree,
  linkAllowed,
  parseTree,
  reconcileChildren,
  validateTreeShape,
  type LayoutResult,
} from '../lib/tree'
import type { FlowTree } from '../api/flow'

function sampleTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2', 'n3'] },
      n2: { id: 'n2', type: 'api', parent: 'n1' },
      n3: { id: 'n3', type: 'api', parent: 'n1', children: ['n4'] },
      n4: { id: 'n4', type: 'assert', parent: 'n3' },
      c1: { id: 'c1', type: 'cache-set' },
    },
    cacheSets: ['c1'],
  }
}

describe('parseTree', () => {
  it('parses valid JSON', () => {
    const t = parseTree('{"start":"a","nodes":{"a":{"id":"a","type":"start"}}}')
    expect(t.start).toBe('a')
    expect(t.nodes.a.type).toBe('start')
  })

  it('returns empty tree on garbage', () => {
    expect(parseTree('not json')).toEqual({ start: '', nodes: {} })
    expect(parseTree(null)).toEqual({ start: '', nodes: {} })
  })
})

describe('layoutTree', () => {
  it('assigns layer-based x and ordinal y', () => {
    const r: LayoutResult = layoutTree(sampleTree())
    expect(r.positions.get('n1')!.x).toBe(0)
    expect(r.positions.get('n2')!.x).toBe(260)
    expect(r.positions.get('n3')!.x).toBe(260)
    expect(r.positions.get('n4')!.x).toBe(520)
    expect(r.positions.get('n1')!.y).toBe(0)
    expect(r.positions.get('n2')!.y).toBe(0)
    expect(r.positions.get('n3')!.y).toBe(120)
  })

  it('places cache-set nodes aside without linking', () => {
    const r = layoutTree(sampleTree())
    const c = r.positions.get('c1')!
    expect(c.x).toBeGreaterThan(r.positions.get('n4')!.x)
    expect(r.ordered).toContain('c1')
  })
})

describe('validateTreeShape', () => {
  it('accepts a valid tree', () => {
    expect(validateTreeShape(sampleTree())).toEqual([])
  })

  it('detects missing start', () => {
    const t = sampleTree()
    t.start = ''
    expect(validateTreeShape(t)[0].code).toBe('tree.start_missing')
  })

  it('detects missing parent', () => {
    const t = sampleTree()
    delete t.nodes.n2.parent
    expect(validateTreeShape(t).some((e) => e.code === 'tree.no_parent')).toBe(true)
  })

  it('detects a cycle', () => {
    const t = sampleTree()
    t.nodes.n1.parent = 'n4'
    expect(validateTreeShape(t).some((e) => e.code === 'tree.cycle')).toBe(true)
  })

  it('detects disconnected nodes', () => {
    const t = sampleTree()
    t.nodes.n2.parent = ''
    expect(validateTreeShape(t).some((e) => e.code === 'tree.disconnected')).toBe(true)
  })

  it('rejects cache-set participation in links', () => {
    const t = sampleTree()
    t.nodes.c1.parent = 'n1'
    expect(validateTreeShape(t).some((e) => e.code === 'cache.linked')).toBe(true)
  })
})

describe('reconcileChildren', () => {
  it('rebuilds children from parent pointers', () => {
    const t: FlowTree = {
      start: 'a',
      nodes: {
        a: { id: 'a', type: 'start' },
        b: { id: 'b', type: 'api', parent: 'a' },
      },
    }
    reconcileChildren(t)
    expect(t.nodes.a.children).toEqual(['b'])
  })
})

describe('linkAllowed', () => {
  it('rejects self links', () => {
    expect(linkAllowed(sampleTree(), 'n1', 'n1').some((e) => e.code === 'link.self')).toBe(true)
  })

  it('rejects cache-set links', () => {
    expect(linkAllowed(sampleTree(), 'n1', 'c1').some((e) => e.code === 'cache.linked')).toBe(true)
  })

  it('rejects a second parent', () => {
    expect(linkAllowed(sampleTree(), 'n2', 'n4').some((e) => e.code === 'link.multi_parent')).toBe(true)
  })

  it('allows a valid new link', () => {
    const t = sampleTree()
    t.nodes.n5 = { id: 'n5', type: 'api' }
    expect(linkAllowed(t, 'n4', 'n5')).toEqual([])
  })
})
