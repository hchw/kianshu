import { describe, expect, it } from 'vitest'
import {
  applyToolMutation,
  deleteNode,
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
      n3: { id: 'n3', type: 'api', parent: 'n1', children: ['n4', 'c1'] },
      n4: { id: 'n4', type: 'assert', parent: 'n3' },
      c1: { id: 'c1', type: 'cache-set', parent: 'n3' },
    },
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

  it('places cache-set nodes in tree', () => {
    const r = layoutTree(sampleTree())
    // c1 is connected under n3, so it's at layer+1
    const c = r.positions.get('c1')!
    expect(c.layer).toBe(2) // n3 children: n4 and c1 both at layer 2
    expect(r.ordered).toContain('c1')
  })

  it('keeps user-placed coordinates without consuming grid slots', () => {
    const t: FlowTree = {
      start: 'n1',
      nodes: {
        n1: { id: 'n1', type: 'start', x: 999, y: 555, children: ['n2', 'n3'] },
        n2: { id: 'n2', type: 'api', parent: 'n1' },
        n3: { id: 'n3', type: 'api', parent: 'n1', children: ['n4'] },
        n4: { id: 'n4', type: 'assert', parent: 'n3' },
      },
    }
    const r = layoutTree(t)
    expect(r.positions.get('n1')).toEqual({ x: 999, y: 555, layer: 0 })
    // unplaced siblings still get the first slots of their layer
    expect(r.positions.get('n2')!.y).toBe(0)
    expect(r.positions.get('n3')!.y).toBe(120)
    expect(r.positions.get('n4')!.x).toBe(520)
  })

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

  it('allows cache-set in tree links', () => {
    const t = sampleTree()
    // c1 already has parent=n3 from sampleTree, should be valid
    expect(validateTreeShape(t).some((e) => e.code === 'cache.linked')).toBe(false)
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

  it('allows cache-set links', () => {
    const t = sampleTree()
    t.nodes.c2 = { id: 'c2', type: 'cache-set' }
    expect(linkAllowed(t, 'n4', 'c2')).toEqual([])
  })

  it('allows reparenting and removes the old parent', () => {
    const t = sampleTree()
    const errs = linkAllowed(t, 'n2', 'n4')
    expect(errs).toEqual([])
    // 模拟 UI 实际改父后，reconcileChildren 应从旧父摘除、挂到新父
    t.nodes.n4.parent = 'n2'
    reconcileChildren(t)
    expect(t.nodes.n4.parent).toBe('n2')
    expect(t.nodes.n3.children).not.toContain('n4')
    expect(t.nodes.n2.children).toContain('n4')
  })

  it('rejects a link that would create a cycle', () => {
    // n3 是 n4 的祖先；把 n4 连到 n3 会让 n3 成为自己的后代
    const errs = linkAllowed(sampleTree(), 'n4', 'n3')
    expect(errs.some((e) => e.code === 'tree.cycle')).toBe(true)
  })

  it('allows a valid new link', () => {
    const t = sampleTree()
    t.nodes.n5 = { id: 'n5', type: 'api' }
    expect(linkAllowed(t, 'n4', 'n5')).toEqual([])
  })
})

describe('deleteNode', () => {
  it('deletes a leaf and drops it from the parent children', () => {
    const t = deleteNode(sampleTree(), 'n2')
    expect(t.nodes.n2).toBeUndefined()
    expect(t.nodes.n1.children).toEqual(['n3'])
  })

  it('cascades deletion over the whole subtree', () => {
    const t = deleteNode(sampleTree(), 'n3')
    expect(t.nodes.n3).toBeUndefined()
    expect(t.nodes.n4).toBeUndefined()
    expect(t.nodes.n1.children).toEqual(['n2'])
    expect(t.nodes.n1).toBeDefined()
  })

  it('refuses to delete the start node', () => {
    const t = deleteNode(sampleTree(), 'n1')
    expect(t.nodes.n1).toBeDefined()
    expect(t.nodes.n2).toBeDefined()
    expect(t.nodes.n3).toBeDefined()
  })

  it('leaves unrelated nodes untouched', () => {
    const t = deleteNode(sampleTree(), 'n2')
    expect(t.nodes.n3).toBeDefined()
    expect(t.nodes.n4).toBeDefined()
    // c1 is under n3, not affected by n2 deletion
    expect(t.nodes.n3.children ?? []).toContain('c1')
  })
})

describe('applyToolMutation', () => {
  const base = sampleTree()

  it('returns original tree for non-mutating tools', () => {
    const ev = { kind: 'tool' as const, round: 1, tool: 'list_units', result: { ok: true } }
    expect(applyToolMutation(base, ev)).toBe(base)
  })

  it('returns original tree when result.ok is false', () => {
    const ev = { kind: 'tool' as const, round: 1, tool: 'create_node', result: { ok: false, error: 'boom' } }
    expect(applyToolMutation(base, ev)).toBe(base)
  })

  it('create_node adds node and updates parent children', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'create_node',
      result: {
        ok: true,
        data: { id: 'n5', type: 'api', parent: 'n2' },
      },
    }
    const next = applyToolMutation(base, ev)
    expect(next.nodes.n5).toBeDefined()
    expect(next.nodes.n5!.type).toBe('api')
    expect(next.nodes.n5!.parent).toBe('n2')
    expect(next.nodes.n2.children).toContain('n5')
  })

  it('create_node adds cache-set to tree', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'create_node',
      result: {
        ok: true,
        data: { id: 'c2', type: 'cache-set', parent: 'n2' },
      },
    }
    const next = applyToolMutation(base, ev)
    expect(next.nodes.c2).toBeDefined()
    expect(next.nodes.c2!.type).toBe('cache-set')
    expect(next.nodes.n2.children).toContain('c2')
  })

  it('update_node replaces existing node', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'update_node',
      result: {
        ok: true,
        data: { id: 'n2', type: 'api', inputs: { auth: { type: 'string', source: '$cache.token' } } },
      },
    }
    const next = applyToolMutation(base, ev)
    expect(next.nodes.n2.inputs?.auth?.source).toBe('$cache.token')
    // 不影响其他节点
    expect(next.nodes.n1).toBeDefined()
  })

  it('update_node returns original if node does not exist', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'update_node',
      result: { ok: true, data: { id: 'ghost', type: 'api' } },
    }
    expect(applyToolMutation(base, ev)).toBe(base)
  })

  it('delete_node removes node and subtree', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'delete_node',
      result: { ok: true, data: { deleted: 'n3' } },
    }
    const next = applyToolMutation(base, ev)
    expect(next.nodes.n3).toBeUndefined()
    expect(next.nodes.n4).toBeUndefined() // subtree
    expect(next.nodes.n1.children).toEqual(['n2'])
  })

  it('link_nodes sets parent and appends to children', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'link_nodes',
      result: { ok: true, data: { linked: ['n2', 'n4'] } },
    }
    const next = applyToolMutation(base, ev)
    expect(next.nodes.n4.parent).toBe('n2')
    expect(next.nodes.n2.children).toContain('n4')
  })

  it('link_nodes is idempotent', () => {
    const ev = {
      kind: 'tool' as const,
      round: 1,
      tool: 'link_nodes',
      result: { ok: true, data: { linked: ['n1', 'n2'] } },
    }
    // n2 already has n1 as parent in sampleTree
    const next = applyToolMutation(base, ev)
    expect(next.nodes.n2.parent).toBe('n1')
    // children should not duplicate
    const n2In = next.nodes.n1.children!.filter((c) => c === 'n2').length
    expect(n2In).toBe(1)
  })
})
