import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { apiError } from '../api/client'
import {
  getDraft,
  listRuns,
  listVersions,
  saveEnable,
  updateDraft,
  validateDraft,
  type Draft,
  type FlowTree,
  type RunLog,
  type ValidationResult,
  type FlowVersion,
} from '../api/flow'
import { listProviders, type Provider } from '../api/providers'
import { parseTree, validateTreeShape } from '../lib/tree'
import AppLayout from '../components/layout/AppLayout'
import FlowCanvas from '../components/canvas/FlowCanvas'
import AgentDialog from '../components/dialog/AgentDialog'
import ResultsPanel from '../components/results/ResultsPanel'
import SchedulePanel from '../components/results/SchedulePanel'

export default function FlowEditor() {
  const { flowID } = useParams()
  const fid = Number(flowID)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [tree, setTree] = useState<FlowTree>({ start: '', nodes: {} })
  const [providers, setProviders] = useState<Provider[]>([])
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [versions, setVersions] = useState<FlowVersion[]>([])
  const [runs, setRuns] = useState<RunLog[]>([])
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const d = await getDraft(fid)
      setDraft(d)
      setTree(parseTree(d.tree))
      setVersions((await listVersions(fid)) || [])
      setRuns((await listRuns(fid)) || [])
    } catch (e) {
      setErr(apiError(e))
    }
  }, [fid])

  useEffect(() => {
    load()
    listProviders()
      .then(setProviders)
      .catch(() => setProviders([]))
  }, [load])

  const onTreeChange = (t: FlowTree) => {
    setTree(t)
    setValidation(validateLocal(t))
  }

  const validateLocal = (t: FlowTree): ValidationResult => {
    const errs = validateTreeShape(t)
    return {
      errors: errs.map((e) => ({ ...e, level: 'error' })),
      warnings: [],
    }
  }

  const restoreTree = (treeStr: string) => {
    try {
      const t = parseTree(treeStr)
      onTreeChange(t)
      save()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const save = async () => {
    setErr('')
    setBusy(true)
    try {
      if (draft) {
        await updateDraft(fid, draft.name, tree)
        setDraft(await getDraft(fid))
      }
      const v = await validateDraft(fid)
      setValidation(v)
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const enable = async () => {
    setErr('')
    setBusy(true)
    try {
      await saveEnable(fid)
      setVersions(await listVersions(fid))
    } catch (e) {
      const errMsg = (e as { response?: { status?: number } }).response?.status === 422
        ? '流校验失败,请先修复校验错误'
        : apiError(e)
      setErr(errMsg)
    } finally {
      setBusy(false)
    }
  }

  if (!draft) {
    return <div className="page container">{err || '加载中…'}</div>
  }

  return (
    <AppLayout
      title={draft.name}
      actions={
        <>
          <button className="link" onClick={save} disabled={busy}>
            保存草稿
          </button>
          <button className="link" onClick={enable} disabled={busy}>
            启用版本
          </button>
        </>
      }
    >
      {err && <p className="err">{err}</p>}
      {validation && validation.errors.length > 0 && (
        <div className="banner warn">
          校验错误 {validation.errors.length} 项:
          {validation.errors.slice(0, 5).map((e, i) => (
            <div key={i}>
              {e.node_id ?? ''} {e.message}
            </div>
          ))}
        </div>
      )}
      <div className="editor-grid">
        <FlowCanvas
          tree={tree}
          selected={selectedNode}
          onSelect={setSelectedNode}
          onTreeChange={onTreeChange}
          onSaved={() => save()}
          testSetID={draft.test_set_id}
        />
        <aside className="side">
          <AgentDialog
            flowID={fid}
            providers={providers}
            tree={tree}
            onChanged={() => load()}
          />
          <SchedulePanel flowID={fid} />
          <ResultsPanel
            flowID={fid}
            runs={runs}
            versions={versions}
            onChanged={async () => {
              setRuns(await listRuns(fid))
            }}
            onRestore={restoreTree}
          />
        </aside>
      </div>
    </AppLayout>
  )
}
