import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  createFlowFromCases,
  listCaseFlowVersions,
  saveCaseFlowVersion,
  type CaseFlowVersion,
} from '../../api/caseFlow'
import { apiError } from '../../api/client'
import { useToast } from '../feedback/Toast'
import { BusyButton } from '../feedback/BusyButton'

type Props = {
  testSetID: number
  caseFlowID: number
  caseFlowName: string
  roots: string[]
  nodeCount: number
  onClose: () => void
}

/**
 * 创建执行流对话框：只做一件事——把选中的用例子树绑定到一个新的执行流草稿。
 * 生成执行流发生在执行流编辑页（按用例生成），不在这里阻塞。
 */
export default function CreateFlowDialog({ testSetID, caseFlowID, caseFlowName, roots, nodeCount, onClose }: Props) {
  const nav = useNavigate()
  const toast = useToast()
  const [versions, setVersions] = useState<CaseFlowVersion[]>([])
  const [versionNo, setVersionNo] = useState(0)
  const [name, setName] = useState(`${caseFlowName} 执行实现`)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const loadVersions = async () => {
    try {
      const vs = await listCaseFlowVersions(caseFlowID)
      setVersions(vs)
      if (vs.length > 0) setVersionNo(vs[0].version_no)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  useEffect(() => {
    void loadVersions()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const saveVersion = async () => {
    setBusy(true)
    setErr('')
    try {
      const v = await saveCaseFlowVersion(caseFlowID)
      toast.success(`已保存版本 v${v.version_no}`)
      await loadVersions()
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const submit = async () => {
    if (!name.trim() || versionNo === 0) return
    setBusy(true)
    setErr('')
    try {
      const flow = await createFlowFromCases(testSetID, name.trim(), [
        { case_flow_id: caseFlowID, version_no: versionNo, root_ids: roots },
      ])
      toast.success('已创建执行流，去编辑页继续生成')
      nav(`/flows/${flow.id}`)
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const noVersion = versions.length === 0

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true">
      <div className="card" style={{ maxWidth: 520, width: '92%' }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h3>创建执行流</h3>
          <button className="link" onClick={onClose} disabled={busy} aria-label="关闭">✕</button>
        </div>
        {err && <div className="err">{err}</div>}
        {noVersion ? (
          <div className="stack">
            <div className="banner warn">
              「{caseFlowName}」还没有已保存版本，需要先保存一个用例流版本作为执行流的来源。
            </div>
            <div className="row">
              <BusyButton className="primary" busy={busy} onClick={() => void saveVersion()}>
                保存当前草稿为版本
              </BusyButton>
            </div>
          </div>
        ) : (
          <div className="stack">
            <label>
              来源用例流版本
              <select value={versionNo} onChange={(e) => setVersionNo(Number(e.target.value))}>
                {versions.map((v) => (
                  <option key={v.id} value={v.version_no}>v{v.version_no}</option>
                ))}
              </select>
            </label>
            <label>
              执行流名称
              <input value={name} onChange={(e) => setName(e.target.value)} />
            </label>
            <div className="muted">将绑定 {roots.length} 个子树根 · 展开 {nodeCount} 个用例节点</div>
            <div className="muted">ⓘ 创建后进入执行流编辑页，在那里按用例生成节点；只有「启用版本」成功后才会标记用例已覆盖。</div>
          </div>
        )}
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button className="link" onClick={onClose} disabled={busy}>取消</button>
          <BusyButton
            className="primary"
            busy={busy}
            disabled={noVersion || versionNo === 0 || !name.trim()}
            onClick={() => void submit()}
          >
            创建执行流
          </BusyButton>
        </div>
      </div>
    </div>
  )
}
