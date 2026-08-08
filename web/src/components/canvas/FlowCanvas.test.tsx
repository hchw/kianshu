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

vi.mock('@xyflow/react', () => ({
  ReactFlow: (props: any) => {
    h.rfProps.current = props
    return React.createElement('div', { 'data-testid': 'reactflow' }, props.children)
  },
  ReactFlowProvider: ({ children }: { children: React.ReactNode }) =>
    React.createElement('div', null, children),
  Background: () => null,
  useReactFlow: () => ({ fitView: h.fitView }),
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
  it('renders all edges as bezier curves', () => {
    render(
      <FlowCanvas
        tree={sampleTree()}
        selected={null}
        onSelect={vi.fn()}
        onTreeChange={vi.fn()}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        testSetID={1}
      />,
    )
    const edges = h.rfProps.current.edges
    expect(edges.length).toBe(1)
    expect(edges[0].type).toBe('bezier')
  })

  it('mirrors position changes into the tree without saving', () => {
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
      />,
    )
    const onNodesChange = h.rfProps.current.onNodesChange
    onNodesChange([{ type: 'position', id: 'n2', position: { x: 12.5, y: 34.25 }, dragging: true }])
    expect(onTreeChange).toHaveBeenCalledTimes(1)
    const next = onTreeChange.mock.calls[0][0] as FlowTree
    expect(next.nodes.n2.x).toBe(12.5)
    expect(next.nodes.n2.y).toBe(34.25)
    expect(onSaved).not.toHaveBeenCalled()
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
      />,
    )
    fireEvent.click(screen.getByText('自动重排'))
    const call = onTreeChange.mock.calls.at(-1)![0] as FlowTree
    expect(call.nodes.n1.x).toBeUndefined()
    expect(call.nodes.n1.y).toBeUndefined()
    expect(h.fitView).toHaveBeenCalled()
  })
})
