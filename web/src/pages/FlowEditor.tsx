import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { apiError } from '../api/client'
import {
  getDraft,
  listRuns,
  listVersions,
  saveEnable,
  subscribeRuns,
  updateDraft,
  validateDraft,
  type Draft,
  type FlowTree,
  type RunLog,
  type ValidationResult,
  type FlowVersion,
} from '../api/flow'
import { listProviders, type Provider } from '../api/providers'
import { parseNodeResults } from '../components/results/ResultsPanel'
import type { NodeRunStatus } from '../components/canvas/FlowCanvas'
import { parseTree, validateTreeShape, deleteNode } from '../lib/tree'
import AppLayout from '../components/layout/AppLayout'
import FlowCanvas from '../components/canvas/FlowCanvas'
import AgentDialog from '../components/dialog/AgentDialog'
import ResultsPanel from '../components/results/ResultsPanel'
import SchedulePanel from '../components/results/SchedulePanel'
import { PageSpinner } from '../components/feedback/PageSpinner'
import { BusyButton } from '../components/feedback/BusyButton'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'

export default function FlowEditor() {
  const { flowID } = useParams()
  const fid = Number(flowID)
  const toast = useToast()
  const [draft, setDraft] = useState<Draft | null>(null)
  const [tree, setTree] = useState<FlowTree>({ start: '', nodes: {} })
  // treeRef 始终持有最新树：setTree 是异步的，save() 直接读 state 闭包
  // 会保存到旧树（拖动/重排/删除后立即保存的路径都会踩坑）。
  const treeRef = useRef(tree)
  const [providers, setProviders] = useState<Provider[]>([])
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [versions, setVersions] = useState<FlowVersion[]>([])
  const [runs, setRuns] = useState<RunLog[]>([])
  const [runsTotal, setRunsTotal] = useState(0)
  const [runsPage, setRunsPage] = useState(1)
  const runsPageSize = 20
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [sideOpen, setSideOpen] = useState(() => localStorage.getItem('kianshu_side_open') !== '0')
  const toggleSide = () =>
    setSideOpen((o) => {
      localStorage.setItem('kianshu_side_open', o ? '0' : '1')
      return !o
    })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastNodeResults, setLastNodeResults] = useState<Record<string, NodeRunStatus> | null>(null)

  const load = useCallback(async () => {
    try {
      const d = await getDraft(fid)
      setDraft(d)
      setTree(parseTree(d.tree))
      setVersions((await listVersions(fid)) || [])
      const runsResp = await listRuns(fid, 1, runsPageSize)
      setRuns(runsResp.runs || [])
      setRunsTotal(runsResp.total || 0)
      setRunsPage(1)
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

  // SSE: subscribe to real-time run updates
  useEffect(() => {
    const sub = subscribeRuns(fid, (run) => {
      setRuns((prev) => {
        // Avoid duplicate: the run we just triggered via trialRun/runVersion
        // may also come through SSE, so dedup by id.
        if (prev.some((r) => r.id === run.id)) return prev
        return [run, ...prev]
      })
      setRunsTotal((t) => t + 1)
    })
    return () => sub.close()
  }, [fid])

  // 从最新 run 的 node_results 提取节点执行状态，用于画布节点上的状态点渲染
  const prevRunsTopId = useRef<number>(0)
  useEffect(() => {
    if (runs.length === 0) return
    const latest = runs[0]
    if (latest.id === prevRunsTopId.current) return
    prevRunsTopId.current = latest.id
    const parsed = parseNodeResults(latest.node_results)
    const map: Record<string, NodeRunStatus> = {}
    for (const nr of parsed) {
      map[nr.node_id] = nr
    }
    setLastNodeResults(map)
  }, [runs])

  const loadRunsPage = async (p: number) => {
    setRunsPage(p)
    try {
      const resp = await listRuns(fid, p, runsPageSize)
      setRuns(resp.runs || [])
      setRunsTotal(resp.total || 0)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const onTreeChange = (t: FlowTree) => {
    treeRef.current = t
    setTree(t)
    setValidation(validateLocal(t))
  }

  // onTreePreview 仅更新画布，不触发校验——agent 中间状态可能暂时不完整
  const onTreePreview = (t: FlowTree) => {
    treeRef.current = t
    setTree(t)
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
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const save = async () => {
    setErr('')
    setBusy(true)
    try {
      if (draft) {
        await updateDraft(fid, draft.name, treeRef.current)
        setDraft(await getDraft(fid))
      }
      const v = await validateDraft(fid)
      setValidation(v)
      toast.success('草稿已保存')
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
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
      toast.success('版本已启用')
    } catch (e) {
      const is422 =
        typeof e === 'object' &&
        e !== null &&
        'response' in e &&
        typeof e.response === 'object' &&
        e.response !== null &&
        'status' in e.response &&
        e.response.status === 422
      const errMsg = is422 ? '流校验失败,请先修复校验错误' : apiError(e)
      setErr(errMsg)
      toast.error(errMsg)
    } finally {
      setBusy(false)
    }
  }

  if (!draft) {
    return (
      <div className="page container">
        {err ? <ErrorNote>{err}</ErrorNote> : <PageSpinner />}
      </div>
    )
  }

  const handleDeleteNode = (id: string) => {
    onTreeChange(deleteNode(treeRef.current, id))
    setSelectedNode(null)
    save()
  }

  return (
    <AppLayout
      full
      title={draft.name}
      actions={
        <>
          <BusyButton className="primary" onClick={save} busy={busy}>
            保存草稿
          </BusyButton>
          <BusyButton className="ghost" onClick={enable} busy={busy}>
            启用版本
          </BusyButton>
        </>
      }
    >
      {err && <ErrorNote>{err}</ErrorNote>}
      {(validation?.errors ?? []).length > 0 && (
        <div className="banner warn">
          校验错误 {(validation?.errors ?? []).length} 项:
          {(validation?.errors ?? []).slice(0, 5).map((e, i) => (
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
          onDelete={handleDeleteNode}
          testSetID={draft.test_set_id}
          nodeResults={lastNodeResults}
        />
        <aside className={sideOpen ? 'side' : 'side collapsed'}>
          <button
            className="rail-toggle"
            onClick={toggleSide}
            title={sideOpen ? '收起右侧面板' : '展开右侧面板'}
            aria-label={sideOpen ? '收起右侧面板' : '展开右侧面板'}
          >
            {sideOpen ? (
              <ChevronRight size={16} aria-hidden="true" />
            ) : (
              <ChevronLeft size={16} aria-hidden="true" />
            )}
          </button>
          <div className="side-body">
            <AgentDialog
              flowID={fid}
              providers={providers}
              tree={tree}
              onChanged={() => load()}
              onTreePreview={onTreePreview}
            />
            <SchedulePanel flowID={fid} />
            <ResultsPanel
              flowID={fid}
              runs={runs}
              totalRuns={runsTotal}
              page={runsPage}
              pageSize={runsPageSize}
              versions={versions}
              onPageChange={loadRunsPage}
              onChanged={async () => {
                const resp = await listRuns(fid, runsPage, runsPageSize)
                setRuns(resp.runs || [])
                setRunsTotal(resp.total || 0)
              }}
              onRestore={restoreTree}
            />
          </div>
          {!sideOpen && <span className="rail-label">面板</span>}
        </aside>
      </div>
    </AppLayout>
  )
}
