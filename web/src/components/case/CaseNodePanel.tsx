import { useEffect, useState } from 'react'
import type { CaseNode } from '../../api/caseFlow'
import { listCaseNodeFlows, updateCaseNode, setCaseNodeStatus, type CaseNodeFlow, type CaseNodeStatus } from '../../api/caseFlow'
import { apiError } from '../../api/client'
import { useToast } from '../feedback/Toast'
import PopConfirm from '../dialog/PopConfirm'

type Props = { caseFlowID: number; revision: number; node: CaseNode; isRoot: boolean; onSaved: () => void; onDelete: (id: string) => void; onClose: () => void }

const RUN_LABEL: Record<string, string> = { not_run: '未运行', passed: '通过', failed: '失败' }
const RUN_CLASS: Record<string, string> = { not_run: '', passed: 'ok', failed: 'danger' }

export default function CaseNodePanel({ caseFlowID, revision, node, isRoot, onSaved, onDelete, onClose }: Props) {
  const toast = useToast()
  const [title, setTitle] = useState(node.title)
  const [description, setDescription] = useState(node.description ?? '')
  const [precondition, setPrecondition] = useState(node.precondition ?? '')
  const [input, setInput] = useState(node.input ?? '')
  const [expected, setExpected] = useState(node.expected ?? '')
  const [busy, setBusy] = useState(false)
  const [impl, setImpl] = useState<CaseNodeFlow[]>([])
  const [run, setRun] = useState<CaseNodeStatus | null>(null)

  useEffect(() => {
    setTitle(node.title); setDescription(node.description ?? ''); setPrecondition(node.precondition ?? ''); setInput(node.input ?? ''); setExpected(node.expected ?? '')
    let alive = true
    listCaseNodeFlows(caseFlowID, node.id)
      .then((data) => { if (alive) { setImpl(data.flows ?? []); setRun(data.node ?? null) } })
      .catch(() => { if (alive) { setImpl([]); setRun(null) } })
    return () => { alive = false }
  }, [caseFlowID, node])

  const save = async () => {
    setBusy(true)
    try { await updateCaseNode(caseFlowID, node.id, revision, { title, description, precondition, input, expected }); toast.success('节点已保存'); onSaved() }
    catch (e) { toast.error(apiError(e)) } finally { setBusy(false) }
  }
  const status = async (value: string) => {
    try { await setCaseNodeStatus(caseFlowID, node.id, revision, value); toast.success('状态已更新'); onSaved() }
    catch (e) { toast.error(apiError(e)) }
  }

  const runResult = run?.last_run_result ?? 'not_run'
  return <aside className="node-panel case-editor-node-panel">
    <div className="node-panel-head"><strong>用例节点</strong><button className="link" onClick={onClose}>✕</button></div>
    <div className="node-panel-body stack">
      <label>标题<input value={title} onChange={(e) => setTitle(e.target.value)} /></label>
      <label>描述<textarea value={description} onChange={(e) => setDescription(e.target.value)} /></label>
      <label>前置条件<textarea value={precondition} onChange={(e) => setPrecondition(e.target.value)} /></label>
      <label>输入<textarea value={input} onChange={(e) => setInput(e.target.value)} /></label>
      <label>预期结果<textarea value={expected} onChange={(e) => setExpected(e.target.value)} /></label>
      <button className="primary" disabled={busy} onClick={() => void save()}>{busy ? '保存中…' : '保存节点'}</button>

      <section className="stack">
        <div className="muted">实现状态（用户维护）</div>
        <div className="row tight">
          <button className={node.status === 'covered' ? 'tag on' : 'tag'} onClick={() => void status('covered')}>已覆盖</button>
          <button className={node.status === 'uncovered' ? 'tag on' : 'tag'} onClick={() => void status('uncovered')}>未覆盖</button>
        </div>
      </section>

      <section className="stack">
        <div className="muted">最近执行</div>
        <div className={`row tight ${RUN_CLASS[runResult] ?? ''}`}>
          <span className={`tag ${RUN_CLASS[runResult] ?? ''}`}>{RUN_LABEL[runResult] ?? runResult}</span>
          {run?.last_run_at && <span className="muted mono">{new Date(run.last_run_at).toLocaleString()}</span>}
        </div>
      </section>

      <section className="stack">
        <div className="muted">实现执行流</div>
        {impl.length === 0 ? (
          <div className="muted">还没有实现执行流</div>
        ) : (
          <div className="list">
            {impl.map((f) => (
              <div key={f.flow_version_id} className="row">
                <span className="strong">{f.flow_name} v{f.version_no}</span>
                {f.enabled && <span className="tag on">启用</span>}
                <a className="link" href={`/flows/${f.flow_id}`}>打开</a>
              </div>
            ))}
          </div>
        )}
      </section>

      {!isRoot && <PopConfirm danger message="确认删除该用例节点及其全部后代？" onConfirm={() => onDelete(node.id)}><button className="danger">删除节点</button></PopConfirm>}
    </div>
  </aside>
}
