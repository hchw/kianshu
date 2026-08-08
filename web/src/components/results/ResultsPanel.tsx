import { useCallback, useEffect, useState } from 'react'
import type { FlowVersion, RunLog } from '../../api/flow'
import { getRun, getVersion, runVersion, trialRun } from '../../api/flow'
import { apiError } from '../../api/client'
import { BusyButton } from '../feedback/BusyButton'
import { EmptyState } from '../feedback/EmptyState'
import { ErrorNote } from '../feedback/ErrorNote'
import { History } from 'lucide-react'

interface Props {
  flowID: number
  runs: RunLog[]
  versions: FlowVersion[]
  onChanged: () => void
  onRestore: (tree: string) => void
}

interface NodeResult {
  node_id: string
  status: string
  input?: unknown
  output?: unknown
  error?: string
}

export default function ResultsPanel({ flowID, runs, versions, onChanged, onRestore }: Props) {
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [latest, setLatest] = useState<NodeResult[]>([])
  const [detail, setDetail] = useState<RunLog | null>(null)

  const run = async (fn: () => Promise<unknown>) => {
    setErr('')
    setBusy(true)
    try {
      await fn()
      onChanged()
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const trial = () => run(() => trialRun(flowID))

  const runVer = (v: FlowVersion) => run(() => runVersion(flowID, v.version_no))

  const loadRun = useCallback(async (r: RunLog) => {
    try {
      const full = await getRun(flowID, r.id)
      setDetail(full)
      setLatest(parseNodeResults(full.node_results))
    } catch (e) {
      setErr(apiError(e))
    }
  }, [flowID])

  const restoreVer = async (v: FlowVersion) => {
    try {
      const full = await getVersion(flowID, v.version_no)
      onRestore(full.tree)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const restoreRun = (r: RunLog) => {
    if (r.tree) onRestore(r.tree)
  }

  useEffect(() => {
    if (runs.length > 0) loadRun(runs[0])
  }, [runs, loadRun])

  const createBadge = (status: string) => (
    <span className={`badge ${statusBadge(status)}`}>{statusLabel(status)}</span>
  )

  return (
    <div className="card side-card">
      <h3>执行结果</h3>
      <div className="row tight">
        <BusyButton className="primary" onClick={trial} busy={busy}>
          试运行草稿
        </BusyButton>
      </div>
      {err && <ErrorNote>{err}</ErrorNote>}
      {versions.length > 0 && (
        <div className="muted">
          版本:
          {versions.map((v) => (
            <span key={v.id} className="run-row" style={{ display: 'inline-flex' }}>
              {v.enabled ? '✓' : ''} v{v.version_no}
              <button className="link" onClick={() => runVer(v)}>
                运行
              </button>
              <button className="link" onClick={() => restoreVer(v)}>
                恢复
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="muted">历史执行</div>
      <div className="list">
        {runs.map((r) => (
          <div key={r.id} className="run-row" onClick={() => loadRun(r)}>
            {createBadge(r.status)} <span className="mono">v{r.version_no || '草稿'}</span>
            <span className="muted">{new Date(r.started_at).toLocaleTimeString()}</span>
            <button className="link" onClick={(e) => { e.stopPropagation(); restoreRun(r) }}>
              恢复
            </button>
          </div>
        ))}
        {runs.length === 0 && (
          <EmptyState compact icon={<History size={20} strokeWidth={1.5} aria-hidden="true" />} title="暂无执行" />
        )}
      </div>
      {latest.length > 0 && (
        <div className="badges">
          {latest.map((n) => (
            <span
              key={n.node_id}
              className={`badge ${n.status} clickable`}
              title={JSON.stringify({ input: n.input, output: n.output, error: n.error })}
              onClick={() => setDetail(detail && { ...detail })}
            >
              {n.node_id} {createBadge(n.status)}
            </span>
          ))}
        </div>
      )}
      {detail && (
        <div className="card sub">
          <div className="strong">执行日志 #{detail.id}</div>
          <div className="mono small">
            {JSON.stringify(parseNodeResults(detail.node_results), null, 2)}
          </div>
        </div>
      )}
    </div>
  )
}

export function parseNodeResults(raw: string): NodeResult[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) return parsed
    if (parsed && typeof parsed === 'object') {
      if (Array.isArray(parsed.results)) return parsed.results
      return Object.entries(parsed).map(([key, val]) => ({
        ...(val as Record<string, unknown>),
        node_id: (val as { node_id?: string })?.node_id ?? key,
        status: (val as { status?: string })?.status ?? 'unknown',
      }))
    }
    return []
  } catch {
    return []
  }
}

export function statusLabel(status: string): string {
  switch (status) {
    case 'ok':
      return '通过'
    case 'failed':
      return '失败'
    case 'soft-stop':
      return '通过'
    default:
      return status
  }
}

function statusBadge(status: string): string {
  switch (status) {
    case 'ok':
    case 'soft-stop':
      return 'ok'
    case 'failed':
      return 'failed'
    default:
      return status
  }
}
