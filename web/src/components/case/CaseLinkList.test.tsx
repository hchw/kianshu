import { describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import CaseLinkList from './CaseLinkList'
import type { FlowCaseSources } from '../../api/caseFlow'

const renderWith = (sources: FlowCaseSources | null) =>
  render(
    <MemoryRouter>
      <CaseLinkList sources={sources} />
    </MemoryRouter>,
  )

describe('CaseLinkList', () => {
  it('shows an empty hint when the flow is not bound', () => {
    renderWith({ bound: false })
    expect(screen.getByText('未关联用例流')).toBeTruthy()
    cleanup()
  })

  it('lists the source flow version and its bound cases', () => {
    renderWith({
      bound: true,
      binding: {
        sources: [{ case_flow_id: 7, name: '登录用例流', version_no: 2 }],
        leaves: [{ case_flow_id: 7, case_node_id: 'n1', title: '正常登录' }],
      },
    })
    expect(screen.getByText(/登录用例流/)).toBeTruthy()
    expect(screen.getByText('v2')).toBeTruthy()
    expect(screen.getByText('正常登录')).toBeTruthy()
    expect(screen.getByText('关联用例 · 1')).toBeTruthy()
    cleanup()
  })
})
