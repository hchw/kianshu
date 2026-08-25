import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { FlowTree } from '../../api/flow'
import type { TestUnit } from '../../api/testset'

const unit: TestUnit = {
  id: 1,
  method: 'POST',
  path: '/utils/sign',
  slug: 'sign',
  tag: '工具',
  name: '签名',
  params: '[{"name":"X-Timestamp","in":"header","type":"integer"},{"name":"payload","in":"body","type":"object"}]',
  request_body: '',
  responses: '',
  security: 'null',
  spec: 'null',
  deleted_at: null,
  created_at: '',
  updated_at: '',
}

vi.mock('../../api/testset', () => ({
  getUnit: () => Promise.resolve(unit),
 listUnits: () => Promise.resolve([unit]),
}))

import NodePanel from './NodePanel'

function apiTree(): FlowTree {
  return {
    start: 'n1',
    nodes: {
      n1: { id: 'n1', type: 'start', children: ['n2'] },
      n2: {
        id: 'n2',
        type: 'api',
        parent: 'n1',
        inputs: {
          'X-Timestamp': { type: 'integer', source: 'sign_ts' },
          payload: { type: 'object', source: 'sign_body' },
        },
        config: {
          unit_id: 1,
          unit: {
            method: 'POST',
            path: '/utils/sign',
            params:
              '[{"name":"X-Timestamp","in":"header","type":"integer"},{"name":"payload","in":"body","type":"object"}]',
          },
          params: {},
          headers: {},
        },
      },
    },
  }
}

function findRow(key: string): HTMLElement {
  return [...document.querySelectorAll<HTMLElement>('.param-row')].find((row) => row.textContent?.includes(key))!
}

describe('ApiParamsEditor 位置落点（params=body）', () => {
  afterEach(cleanup)

  it('header 覆盖写 config.headers；body 覆盖写 config.params', async () => {
    const onChange = vi.fn()
    render(
      <NodePanel
        tree={apiTree()}
        nodeID="n2"
        onTreeChange={onChange}
        onSaved={vi.fn()}
        onDelete={vi.fn()}
        onClose={vi.fn()}
        testSetID={1}
      />,
    )
    await act(async () => {})

    // 切换固定值模式后，header/body 字段各自写入对应通道
    const headerRow = findRow('X-Timestamp')
    fireEvent.change(headerRow.querySelector('select[aria-label="value mode X-Timestamp"]')!, { target: { value: 'fixed' } })
    fireEvent.change(headerRow.querySelector('input[data-param-key="X-Timestamp"]')!, { target: { value: '=sign_ts' } })

    const bodyRow = findRow('payload')
    fireEvent.change(bodyRow.querySelector('select[aria-label="value mode payload"]')!, { target: { value: 'fixed' } })
    fireEvent.change(bodyRow.querySelector('input[data-param-key="payload"]')!, { target: { value: '{"a":1}' } })

    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    const saved = onChange.mock.calls[onChange.mock.calls.length - 1][0] as FlowTree
    const node = saved.nodes.n2
    const headers = (node.config as Record<string, any>).headers ?? {}
    const params = (node.config as Record<string, any>).params ?? {}
    expect(headers['X-Timestamp']).toBe('=sign_ts')
    expect(params['X-Timestamp']).toBeUndefined()
    expect(params.payload).toEqual({ a: 1 })
  })
})