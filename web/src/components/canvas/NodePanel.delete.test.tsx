import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import React from 'react'

vi.mock('../../api/testset', () => ({
  getUnit: () => Promise.resolve(null),
}))

import NodePanel from './NodePanel'

function panelTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2'] },
      n2: { id: 'n2', type: 'assert', parent: 'n1' },
    },
  }
}

function renderPanel(nodeID: string, onDelete = vi.fn()) {
  render(
    <NodePanel
      tree={panelTree()}
      nodeID={nodeID}
      onTreeChange={vi.fn()}
      onSaved={vi.fn()}
      onDelete={onDelete}
      onClose={vi.fn()}
      testSetID={1}
    />,
  )
  return onDelete
}

describe('NodePanel delete button', () => {
  afterEach(cleanup)

  it('hides the delete button for the start node', () => {
    renderPanel('n1')
    expect(screen.queryByText('删除节点')).toBeNull()
  })

  it('shows the delete button for non-start nodes and confirms before deleting', () => {
    const onDelete = renderPanel('n2')
    const btn = screen.getByText('删除节点')
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    fireEvent.click(btn)
    expect(onDelete).not.toHaveBeenCalled()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    fireEvent.click(btn)
    expect(onDelete).toHaveBeenCalledWith('n2')
  })
})
