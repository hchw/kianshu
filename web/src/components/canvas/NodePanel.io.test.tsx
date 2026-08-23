import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { FlowTree, IOKey } from '../../api/flow'

vi.mock('../../api/testset', () => ({ getUnit: () => Promise.resolve(null) }))

import NodePanel from './NodePanel'

function adapterTree(
  inputs?: Record<string, IOKey>,
  outputs?: Record<string, IOKey>,
): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2'] },
      n2: {
        id: 'n2',
        type: 'adapter',
        parent: 'n1',
        inputs,
        outputs,
        config: { expr: '$x' },
      },
    },
  }
}

function apiTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2'] },
      n2: {
        id: 'n2',
        type: 'api',
        parent: 'n1',
        inputs: { username: { type: 'string', source: 'username' } },
        config: { unit_id: 5 },
      },
    },
  }
}

function renderPanel(tree: FlowTree, nodeID = 'n2') {
  const onChange = vi.fn()
  render(
    <NodePanel
      tree={tree}
      nodeID={nodeID}
      onTreeChange={onChange}
      onSaved={vi.fn()}
      onDelete={vi.fn()}
      onClose={vi.fn()}
      testSetID={1}
    />,
  )
  return onChange
}

function savedTree(onChange: ReturnType<typeof vi.fn>): FlowTree {
  const call = onChange.mock.calls[onChange.mock.calls.length - 1]
  return call?.[0] as FlowTree
}

describe('NodePanel editable I/O: source 编辑', () => {
  afterEach(cleanup)

  it('编辑输入 source 并保存 → node.inputs[key].source 更新且 type 不变', () => {
    const onChange = renderPanel(
      adapterTree({ auth: { type: 'string', source: '$cache.token' } }),
    )
    const src = screen.getByLabelText('input auth source')
    fireEvent.change(src, { target: { value: '$cache.refresh_token' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    const io = savedTree(onChange).nodes.n2.inputs!.auth
    expect(io.source).toBe('$cache.refresh_token')
    expect(io.type).toBe('string')
  })

  it('source 输入框初始值来自 node.inputs[key].source', () => {
    renderPanel(adapterTree({ auth: { type: 'string', source: '$cache.token' } }))
    expect((screen.getByLabelText('input auth source') as HTMLInputElement).value).toBe(
      '$cache.token',
    )
  })
})

describe('NodePanel editable I/O: 删除键', () => {
  afterEach(cleanup)

  it('删除输入键并保存 → 键及其 source 从 node.inputs 消失，其余键保留', () => {
    const onChange = renderPanel(
      adapterTree({
        auth: { type: 'string', source: '$cache.token' },
        username: { type: 'string', source: 'username' },
      }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'delete input auth' }))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    const inputs = savedTree(onChange).nodes.n2.inputs ?? {}
    expect(inputs.auth).toBeUndefined()
    expect(inputs.username).toBeDefined()
  })

  it('删除输出键并保存 → 键从 node.outputs 消失', () => {
    const onChange = renderPanel(
      adapterTree(undefined, { result: { type: 'string' }, token: { type: 'string' } }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'delete output result' }))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    const outputs = savedTree(onChange).nodes.n2.outputs ?? {}
    expect(outputs.result).toBeUndefined()
    expect(outputs.token).toBeDefined()
  })

  it('删除后可恢复（保存前再点一次恢复）→ 键仍保留', () => {
    const onChange = renderPanel(adapterTree({ auth: { type: 'string', source: '$cache.token' } }))
    fireEvent.click(screen.getByRole('button', { name: 'delete input auth' }))
    fireEvent.click(screen.getByRole('button', { name: 'restore input auth' }))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    expect(savedTree(onChange).nodes.n2.inputs!.auth).toBeDefined()
  })
})

describe('NodePanel api 只读 I/O 契约段不受影响', () => {
  afterEach(cleanup)

  it('api 节点带自动 inputs → 只读列表展示，无删除/source 编辑控件', () => {
    renderPanel(apiTree())
    expect(screen.getByText('I/O 契约（自动从 Swagger 生成，只读）')).toBeDefined()
    expect(screen.queryByLabelText('input username source')).toBeNull()
    expect(screen.queryByLabelText('input username desc')).toBeNull()
    expect(screen.queryByRole('button', { name: 'delete input username' })).toBeNull()
  })
})