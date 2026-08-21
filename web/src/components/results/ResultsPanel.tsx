import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
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
  totalRuns: number
  page: number
  pageSize: number
  versions: FlowVersion[]
  onPageChange: (page: number) => void
  onChanged: () => void
  /** 恢复时回填树（可选带回版本快照中的流级文档，语义同 UpdateDraft：undefined 不改动） */
  onRestore: (tree: string, systemPrompt?: string) => void
}

interface NodeResult {
  node_id: string
  status: string
  input?: unknown
  output?: unknown
  error?: string
}

interface PopoverState {
  run: RunLog
  x: number
  y: number
  detail: NodeResult[]
  loading: boolean
}

export default function ResultsPanel({
  flowID,
  runs,
  totalRuns,
  page,
  pageSize,
  versions,
  onPageChange,
  onChanged,
  onRestore,
}: Props) {
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [popover, setPopover] = useState<PopoverState | null>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  // 标记刚打开的弹窗，让同一次点击产生的 mousedown 不要立即关掉它
  const skipCloseRef = useRef(false)

  const totalPages = Math.max(1, Math.ceil(totalRuns / pageSize))

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

  const loadRunDetail = useCallback(
    async (r: RunLog, anchorEl: HTMLElement) => {
      const rect = anchorEl.getBoundingClientRect()
      // 弹窗显示在记录左侧，垂直居中对齐该行
      const x = rect.left - 376 // 360px 宽度 + 16px 间距
      const y = rect.top + rect.height / 2
      skipCloseRef.current = true
      setPopover({ run: r, x, y, detail: [], loading: true })
      try {
        const full = await getRun(flowID, r.id)
        setPopover((prev) =>
          prev?.run.id === r.id
            ? { ...prev, detail: parseNodeResults(full.node_results), loading: false }
            : prev,
        )
      } catch (e) {
        setPopover((prev) =>
          prev?.run.id === r.id ? { ...prev, detail: [], loading: false } : prev,
        )
        setErr(apiError(e))
      }
    },
    [flowID],
  )

  const restoreVer = async (v: FlowVersion) => {
    try {
      const full = await getVersion(flowID, v.version_no)
      onRestore(full.tree, full.system_prompt)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const restoreRun = (r: RunLog) => {
    if (r.tree) onRestore(r.tree)
  }

  // 点击弹窗外部关闭（但点击 run-row 时不关，让行自己的 onClick 处理）
  useEffect(() => {
    if (!popover) return
    const handler = (e: MouseEvent) => {
      if (skipCloseRef.current) {
        skipCloseRef.current = false
        return
      }
      const target = e.target as HTMLElement
      // 点击其他 run-row：不关闭，让 click 事件切换选中行
      if (target.closest('.run-row')) return
      // 点击弹窗内部：不关闭
      if (popoverRef.current?.contains(target)) return
      setPopover(null)
    }
    document.addEventListener('mousedown', handler, true)
    return () => document.removeEventListener('mousedown', handler, true)
  }, [popover])

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
      <div className="muted">
        历史执行
        {totalRuns > 0 && (
          <span style={{ marginLeft: 8 }}>
            ({totalRuns}条)
          </span>
        )}
      </div>
      <div className="list">
        {runs.map((r) => (
          <div
            key={r.id}
            className="run-row"
            onClick={(e) => loadRunDetail(r, e.currentTarget)}
          >
            {createBadge(r.status)}{' '}
            <span className="mono">v{r.version_no || '草稿'}</span>
            <span className="muted">{new Date(r.started_at).toLocaleTimeString()}</span>
            <button
              className="link"
              onClick={(e) => {
                e.stopPropagation()
                restoreRun(r)
              }}
            >
              恢复
            </button>
          </div>
        ))}
        {runs.length === 0 && (
          <EmptyState
            compact
            icon={<History size={20} strokeWidth={1.5} aria-hidden="true" />}
            title="暂无执行"
          />
        )}
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="pagination">
          <button disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
            上一页
          </button>
          <span className="muted">
            {page}/{totalPages}
          </span>
          <button disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>
            下一页
          </button>
        </div>
      )}

      {/* 弹窗通过 Portal 渲染到 body，彻底避开卡片 transform 导致的 fixed 定位偏移 */}
      {popover &&
        createPortal(
          <div
            ref={popoverRef}
            className="run-popover"
            style={{
              left: Math.max(8, popover.x),
              top: Math.max(8, Math.min(popover.y - 200, window.innerHeight - 440)),
            }}
          >
            <div className="run-popover-head">
              <span className="strong">执行日志 #{popover.run.id}</span>
              <button className="link" onClick={() => setPopover(null)}>
                ✕
              </button>
            </div>
            {popover.loading ? (
              <div className="muted" style={{ padding: 8 }}>加载中...</div>
            ) : popover.detail.length > 0 ? (
              <div className="log-box">
                {popover.detail.map((n) => (
                  <div key={n.node_id} className="run-row">
                    {createBadge(n.status)}
                    <span className="mono" style={{ fontSize: 11 }}>{n.node_id}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="muted" style={{ padding: 8 }}>无节点结果</div>
            )}
            <details style={{ marginTop: 4 }}>
              <summary className="muted" style={{ fontSize: 11, cursor: 'pointer' }}>
                原始数据
              </summary>
              <pre className="mono" style={{ fontSize: 10, maxHeight: 180, overflow: 'auto' }}>
                {JSON.stringify(popover.detail, null, 2)}
              </pre>
            </details>
          </div>,
          document.body,
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
