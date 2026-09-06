import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  caseFlowExportURL,
  deleteCaseNode,
  getCaseTreeView,
  listCaseFlowVersions,
  listCaseSources,
  restoreCaseFlowVersion,
  saveCaseFlowVersion,
  updateCaseNode,
  type CaseNode,
  type CaseSource,
  type CaseFlowVersion,
} from '../api/caseFlow'
import { apiError } from '../api/client'
import { listProviders, type Provider } from '../api/providers'
import AppLayout from '../components/layout/AppLayout'
import CaseAgentDialog from '../components/case/CaseAgentDialog'
import CaseCanvas from '../components/case/CaseCanvas'
import { useToast } from '../components/feedback/Toast'

export default function CaseFlowEditor() {
  const { caseFlowID } = useParams()
  const id = Number(caseFlowID)
  const toast = useToast()
  const [tree, setTree] = useState<CaseNode | null>(null)
  const [revision, setRevision] = useState(0)
  const [sources, setSources] = useState<CaseSource[]>([])
  const [versions, setVersions] = useState<CaseFlowVersion[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    try {
      const view = await getCaseTreeView(id)
      setTree(view.tree.root)
      setRevision(view.draft.revision)
      setSources(await listCaseSources(id))
      setVersions(await listCaseFlowVersions(id))
    } catch (e) {
      setErr(apiError(e))
    }
  }, [id])

  useEffect(() => { void load(); void listProviders().then(setProviders).catch(() => setProviders([])) }, [load])


  const savePosition = async (nodeID: string, x: number, y: number) => {
    try { await updateCaseNode(id, nodeID, revision, { x, y }); await load() }
    catch (e) { toast.error(apiError(e)) }
  }

  const deleteNode = async (nodeID: string) => {
    if (nodeID === tree?.id || !window.confirm('确认删除该节点及其全部后代？')) return
    try {
      await deleteCaseNode(id, nodeID, revision)
      setSelected(null)
      toast.success('节点子树已删除')
      await load()
    } catch (e) { toast.error(apiError(e)) }
  }

  const saveVersion = async () => {
    try { await saveCaseFlowVersion(id); toast.success('版本已保存'); await load() }
    catch (e) { toast.error(apiError(e)) }
  }

  const restore = async (versionNo: number) => {
    if (!window.confirm(`确认恢复版本 ${versionNo}？当前草稿将被替换。`)) return
    try { await restoreCaseFlowVersion(id, versionNo); toast.success('已恢复到该版本'); await load() }
    catch (e) { toast.error(apiError(e)) }
  }

  const exportURL = caseFlowExportURL(id)

  return (
    <AppLayout full title="用例流编辑器">
      {err && <div className="err">{err}</div>}
      <div className="editor-grid">
        {tree ? <CaseCanvas caseFlowID={id} root={tree} selected={selected} onSelect={setSelected} onPositionChange={savePosition} onDelete={deleteNode} revision={revision} onSaved={load} /> : <div className="muted">暂无树</div>}
        <aside className="side">
          <div className="side-body">
            <CaseAgentDialog caseFlowID={id} providers={providers} tree={tree} onChanged={load} onTreePreview={(next) => setTree(next)} />
            <section className="card side-card">
              <h3>版本</h3>
              <button className="primary" onClick={saveVersion}>保存当前草稿为新版本</button>
              <div className="list">{versions.map((v) => <div key={v.id} className="card item row"><span className="strong">版本 {v.version_no}</span><button className="link" onClick={() => restore(v.version_no)}>恢复</button><a className="link" href={`${exportURL.url}&version=${v.version_no}`}>导出</a></div>)}</div>
              <a className="link" href={exportURL.url}>导出当前草稿</a>
            </section>
            <section className="card side-card">
              <h3>来源</h3>
              <div className="list">{sources.map((source) => <div key={source.id} className="row"><span className="mono">{source.kind}</span>{source.document_id ? <span>文档 #{source.document_id}</span> : null}</div>)}</div>
            </section>
          </div>
        </aside>
      </div>
    </AppLayout>
  )
}
