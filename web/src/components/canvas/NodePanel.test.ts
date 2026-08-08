import { describe, expect, it } from 'vitest'
import type { FlowTree, IOKey } from '../../api/flow'

function saveNode(tree: FlowTree, nodeID: string, inputs: Record<string, string>, outputs: Record<string, string>): FlowTree {
  const next: FlowTree = JSON.parse(JSON.stringify(tree))
  const n = next.nodes[nodeID]
  const ins: Record<string, IOKey> = {}
  for (const [k, v] of Object.entries(inputs)) {
    const orig = (tree.nodes[nodeID]?.inputs ?? {})[k] ?? {}
    ins[k] = { ...orig, desc: v }
  }
  const outs: Record<string, IOKey> = {}
  for (const [k, v] of Object.entries(outputs)) {
    const orig = (tree.nodes[nodeID]?.outputs ?? {})[k] ?? {}
    outs[k] = { ...orig, desc: v }
  }
  n.inputs = ins
  n.outputs = outs
  return next
}

describe('NodePanel IO preservation', () => {
  const tree: FlowTree = {
    start: 'n1',
    nodes: {
      n1: {
        id: 'n1',
        type: 'api',
        inputs: {
          authorization: { type: 'primitive', source: '$cache.token', desc: 'token' },
          user_id: { type: 'primitive', from: '$start.user_id', desc: 'user' },
        },
        outputs: {
          data: { type: 'object', desc: 'response' },
        },
      },
    },
  }

  it('preserves type and source when only desc is changed', () => {
    const next = saveNode(tree, 'n1', { authorization: 'new desc' }, { data: 'updated' })
    const input = next.nodes.n1.inputs!
    expect(input.authorization.type).toBe('primitive')
    expect(input.authorization.source).toBe('$cache.token')
    expect(input.authorization.desc).toBe('new desc')
  })

  it('preserves from field for backward compatibility', () => {
    const next = saveNode(tree, 'n1', { user_id: 'updated' }, {})
    const input = next.nodes.n1.inputs!
    expect(input.user_id.type).toBe('primitive')
    expect(input.user_id.from).toBe('$start.user_id')
    expect(input.user_id.desc).toBe('updated')
  })

  it('preserves output type when desc is changed', () => {
    const next = saveNode(tree, 'n1', {}, { data: 'updated' })
    const output = next.nodes.n1.outputs!
    expect(output.data.type).toBe('object')
    expect(output.data.desc).toBe('updated')
  })

  it('adds new IO key with desc only', () => {
    const next = saveNode(tree, 'n1', { new_input: 'new' }, { new_output: 'new' })
    expect(next.nodes.n1.inputs!.new_input.desc).toBe('new')
    expect(next.nodes.n1.outputs!.new_output.desc).toBe('new')
  })
})

describe('NodePanel auto-populated inputs', () => {
  it('inputs with type and source (no desc) are readable', () => {
    const tree: FlowTree = {
      start: 'n1',
      nodes: {
        n1: {
          id: 'n1',
          type: 'api',
          inputs: {
            Authorization: { type: 'primitive', source: '$cache.token' },
            id: { type: 'primitive' },
          },
        },
      },
    }
    const n1 = tree.nodes.n1
    expect(n1.inputs!.Authorization.type).toBe('primitive')
    expect(n1.inputs!.Authorization.source).toBe('$cache.token')
    expect(n1.inputs!.Authorization.desc).toBeUndefined()
    expect(n1.inputs!.id.type).toBe('primitive')
    expect(n1.inputs!.id.source).toBeUndefined()
  })

  it('empty inputs is handled without error', () => {
    const tree: FlowTree = {
      start: 'n1',
      nodes: {
        n1: {
          id: 'n1',
          type: 'api',
          inputs: {},
        },
      },
    }
    const keys = Object.keys(tree.nodes.n1.inputs ?? {})
    expect(keys).toHaveLength(0)
  })

  it('preserves auto-populated inputs structure on API nodes', () => {
    const tree: FlowTree = {
      start: 'n1',
      nodes: {
        n1: {
          id: 'n1',
          type: 'api',
          inputs: {
            Authorization: { type: 'primitive', source: '$cache.token' },
            username: { type: 'primitive' },
          },
        },
      },
    }
    // Auto-populated inputs are accessible directly from the node
    const n1 = tree.nodes.n1
    expect(n1.inputs!.Authorization.type).toBe('primitive')
    expect(n1.inputs!.Authorization.source).toBe('$cache.token')
    expect(n1.inputs!.username.type).toBe('primitive')
    // Non-auth params have no source
    expect(n1.inputs!.username.source).toBeUndefined()
  })
})

