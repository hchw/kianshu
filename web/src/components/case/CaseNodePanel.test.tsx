import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import CaseNodePanel from './CaseNodePanel'
import { ToastProvider } from '../feedback/Toast'
import type { CaseNode } from '../../api/caseFlow'

vi.mock('../../api/caseFlow', async (orig) => {
  const mod = await orig<typeof import('../../api/caseFlow')>()
  return { ...mod, updateCaseNode: vi.fn().mockResolvedValue({}), setCaseNodeStatus: vi.fn().mockResolvedValue({}) }
})

const node: CaseNode = { id: 'n1', title: '登录成功', status: 'uncovered', children: [] }

function UI(props: { isRoot?: boolean }) {
  return (
    <ToastProvider>
      <CaseNodePanel caseFlowID={1} revision={3} node={node} isRoot={props.isRoot ?? false} onSaved={() => {}} onDelete={() => {}} onClose={() => {}} />
    </ToastProvider>
  )
}

describe('CaseNodePanel', () => {
  it('渲染全部字段与操作按钮', () => {
    cleanup()
    render(<UI />)
    expect(screen.getByText('用例节点')).toBeTruthy()
    expect(screen.getByLabelText('标题')).toBeTruthy()
    expect(screen.getByLabelText('描述')).toBeTruthy()
    expect(screen.getByLabelText('前置条件')).toBeTruthy()
    expect(screen.getByLabelText('输入')).toBeTruthy()
    expect(screen.getByLabelText('预期结果')).toBeTruthy()
    expect(screen.getByRole('button', { name: '保存节点' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '删除节点' })).toBeTruthy()
  })

  it('保存时提交全部字段', async () => {
    cleanup()
    render(<UI />)
    const { updateCaseNode } = await import('../../api/caseFlow')
    fireEvent.change(screen.getByLabelText('描述'), { target: { value: '验证登录' } })
    fireEvent.click(screen.getByRole('button', { name: '保存节点' }))
    await vi.waitFor(() => {
      expect(updateCaseNode).toHaveBeenCalledWith(1, 'n1', 3, expect.objectContaining({ title: '登录成功', description: '验证登录' }))
    })
  })

  it('根节点不显示删除按钮', () => {
    cleanup()
    render(<UI isRoot />)
    expect(screen.queryByRole('button', { name: '删除节点' })).toBeNull()
  })
})
