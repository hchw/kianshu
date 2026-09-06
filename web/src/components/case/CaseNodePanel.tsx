import { useEffect, useState } from 'react'
import type { CaseNode } from '../../api/caseFlow'
import { updateCaseNode, setCaseNodeStatus } from '../../api/caseFlow'
import { apiError } from '../../api/client'
import { useToast } from '../feedback/Toast'
import PopConfirm from '../dialog/PopConfirm'

type Props = { caseFlowID: number; revision: number; node: CaseNode; isRoot: boolean; onSaved: () => void; onDelete: (id: string) => void; onClose: () => void }

export default function CaseNodePanel({ caseFlowID, revision, node, isRoot, onSaved, onDelete, onClose }: Props) {
  const toast = useToast()
  const [title, setTitle] = useState(node.title)
  const [description, setDescription] = useState(node.description ?? '')
  const [precondition, setPrecondition] = useState(node.precondition ?? '')
  const [input, setInput] = useState(node.input ?? '')
  const [expected, setExpected] = useState(node.expected ?? '')
  const [busy, setBusy] = useState(false)
  useEffect(() => { setTitle(node.title); setDescription(node.description ?? ''); setPrecondition(node.precondition ?? ''); setInput(node.input ?? ''); setExpected(node.expected ?? '') }, [node])
  const save = async () => {
    setBusy(true)
    try { await updateCaseNode(caseFlowID, node.id, revision, { title, description, precondition, input, expected }); toast.success('节点已保存'); onSaved() }
    catch (e) { toast.error(apiError(e)) } finally { setBusy(false) }
  }
  const status = async (value: string) => {
    try { await setCaseNodeStatus(caseFlowID, node.id, revision, value); toast.success('状态已更新'); onSaved() }
    catch (e) { toast.error(apiError(e)) }
  }
  return <aside className="node-panel case-editor-node-panel">
    <div className="node-panel-head"><strong>用例节点</strong><button className="link" onClick={onClose}>✕</button></div>
    <div className="node-panel-body stack">
      <label>标题<input value={title} onChange={(e) => setTitle(e.target.value)} /></label>
      <label>描述<textarea value={description} onChange={(e) => setDescription(e.target.value)} /></label>
      <label>前置条件<textarea value={precondition} onChange={(e) => setPrecondition(e.target.value)} /></label>
      <label>输入<textarea value={input} onChange={(e) => setInput(e.target.value)} /></label>
      <label>预期结果<textarea value={expected} onChange={(e) => setExpected(e.target.value)} /></label>
      <div className="row tight"><span className="muted">覆盖状态</span><button className={node.status === 'covered' ? 'tag on' : 'tag'} onClick={() => void status('covered')}>已覆盖</button><button className={node.status === 'uncovered' ? 'tag on' : 'tag'} onClick={() => void status('uncovered')}>未覆盖</button></div>
      <button className="primary" disabled={busy} onClick={() => void save()}>{busy ? '保存中…' : '保存节点'}</button>
      {!isRoot && <PopConfirm danger message="确认删除该用例节点及其全部后代？" onConfirm={() => onDelete(node.id)}><button className="danger">删除节点</button></PopConfirm>}
    </div>
  </aside>
}
