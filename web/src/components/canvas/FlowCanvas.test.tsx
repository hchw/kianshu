import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import React from 'react'
import type { FlowTree } from '../../api/flow'

afterEach(cleanup)

const h = vi.hoisted(() => {
  const rfProps: { current: Record<string, any> } = { current: {} }
  const fitView = vi.fn()
  return { rfProps, fitView }
})

vi.mock('../../components/feedback/Toast', () => ({
  useToast: () => ({ toast: vi.fn(), success: vi.fn(), error: vi.fn() }),
}))

vi.mock('@xyflow/react', () => ({
  ReactFlow: (props: any) => {
    h.rfProps.current = props
    return React.createElement('div', { 'data-testid': 'reactflow' }, props.children)
  },
  ReactFlowProvider: ({ children }: { children: React.ReactNode }) =>
    React.createElement('div', null, children),
  Background: () => null,
  Position: { Top: 'top', Bottom: 'bottom', Left: 'left', Right: 'right' },
  useReactFlow: () => ({ fitView: h.fitView }),
  useNodesState: (init: any) => {
    const [nodes, setNodes] = React.useState(init)
    return [nodes, setNodes, () => {}]
  },
  applyNodeChanges: (_changes: any, nodes: any) => nodes,
}))

import FlowCanvas from './FlowCanvas'

function sampleTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2'] },
      n2: { id: 'n2', type: 'api', parent: 'n1' },
    },
  }
}

describe('FlowCanvas', () => {
  it('renders all edges with default type', () => {
    render(
      <FlowCanvas
        tree={sampleTree()}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={vi.fn()}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        testSetID={1}
        nodeResults={null}
      />,
    )
    const edges = h.rfProps.current.edges
    expect(edges.length).toBe(1)
    expect(edges[0].type).toBe('default')
  })

  it('commits position on drag stop and saves', () => {
    const onTreeChange = vi.fn()
    const onSaved = vi.fn()
    render(
      <FlowCanvas
        tree={sampleTree()}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={onSaved}
        testSetID={1}
        nodeResults={null}
      />,
    )
    const onNodeDragStop = h.rfProps.current.onNodeDragStop
    onNodeDragStop({}, { id: 'n2', position: { x: 12.5, y: 34.25 } })
    expect(onTreeChange).toHaveBeenCalledTimes(1)
    const next = onTreeChange.mock.calls[0][0] as FlowTree
    expect(next.nodes.n2.x).toBe(12.5)
    expect(next.nodes.n2.y).toBe(34.25)
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  it('persists the final position on drag stop', () => {
    const onTreeChange = vi.fn()
    const onSaved = vi.fn()
    render(
      <FlowCanvas
        tree={sampleTree()}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={onSaved}
        testSetID={1}
        nodeResults={null}
      />,
    )
    const onNodeDragStop = h.rfProps.current.onNodeDragStop
    onNodeDragStop({}, { id: 'n2', position: { x: 99, y: 88 } })
    expect(onTreeChange).toHaveBeenCalledTimes(1)
    const next = onTreeChange.mock.calls[0][0] as FlowTree
    expect(next.nodes.n2.x).toBe(99)
    expect(next.nodes.n2.y).toBe(88)
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  it('relayout clears all coordinates and re-fits the view', () => {
    const tree = sampleTree()
    tree.nodes.n1.x = 333
    tree.nodes.n1.y = 444
    const onTreeChange = vi.fn()
    render(
      <FlowCanvas
        tree={tree}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        testSetID={1}
        nodeResults={null}
      />,
    )
    fireEvent.click(screen.getByText('自动重排'))
    const call = onTreeChange.mock.calls.at(-1)![0] as FlowTree
    expect(call.nodes.n1.x).toBeUndefined()
    expect(call.nodes.n1.y).toBeUndefined()
    expect(h.fitView).toHaveBeenCalled()
  })

  it('onConnect reconciles children so the new edge renders immediately', () => {
    const tree: FlowTree = {
      start: 'n1',
      nodes: {
        n1: { id: 'n1', type: 'start', children: ['n2'] },
        n2: { id: 'n2', type: 'api', parent: 'n1' },
        n3: { id: 'n3', type: 'assert' },
      },
    }
    const onTreeChange = vi.fn()
    const onSaved = vi.fn()
    render(
      <FlowCanvas
        tree={tree}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={onSaved}
        onDelete={vi.fn()}
        testSetID={1}
        nodeResults={null}
      />,
    )
    h.rfProps.current.onConnect({ source: 'n2', target: 'n3' })
    const next = onTreeChange.mock.calls[0][0] as FlowTree
    expect(next.nodes.n3.parent).toBe('n2')
    expect(next.nodes.n2.children).toContain('n3')
    expect(onSaved).toHaveBeenCalled()
  })

  it('onDrop onto a node makes the new node its child (落点即父)', () => {
    const tree = sampleTree()
    const onTreeChange = vi.fn()
    const { container } = render(
      <FlowCanvas
        tree={tree}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        testSetID={1}
        nodeResults={null}
      />,
    )
    const canvas = container.querySelector('.canvas')!
    const nodeEl = document.createElement('div')
    nodeEl.className = 'react-flow__node'
    nodeEl.setAttribute('data-id', 'n2')
    canvas.appendChild(nodeEl)
    fireEvent.drop(nodeEl, { dataTransfer: { getData: () => 'api' } })
    expect(onTreeChange).toHaveBeenCalledTimes(1)
    const next = onTreeChange.mock.calls[0][0] as FlowTree
    const newId = Object.keys(next.nodes).find((id) => id !== 'n1' && id !== 'n2' && id !== 'n3')!
    expect(next.nodes[newId].parent).toBe('n2')
    expect(next.nodes.n2.children).toContain(newId)
  })

  it('onDrop onto blank canvas creates no node (决策 A)', () => {
    const tree = sampleTree()
    const onTreeChange = vi.fn()
    const { container } = render(
      <FlowCanvas
        tree={tree}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={onTreeChange}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        testSetID={1}
        nodeResults={null}
      />,
    )
    const canvas = container.querySelector('.canvas')!
    fireEvent.drop(canvas, { dataTransfer: { getData: () => 'api' } })
    expect(onTreeChange).not.toHaveBeenCalled()
  })
})
