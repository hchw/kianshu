import { useCallback, useEffect, useState } from 'react'
import { apiError } from '../../api/client'
import {
  createSchedule,
  deleteSchedule,
  listSchedules,
  setScheduleEnabled,
  type FlowSchedule,
} from '../../api/schedule'
import { BusyButton } from '../feedback/BusyButton'
import { EmptyState } from '../feedback/EmptyState'
import { ErrorNote } from '../feedback/ErrorNote'
import { Clock } from 'lucide-react'

interface Props {
  flowID: number
}

export default function SchedulePanel({ flowID }: Props) {
  const [schedules, setSchedules] = useState<FlowSchedule[]>([])
  const [cron, setCron] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setSchedules((await listSchedules(flowID)) || [])
    } catch (e) {
      setErr(apiError(e))
    }
  }, [flowID])

  useEffect(() => {
    load()
  }, [load])

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!cron.trim()) return
    setErr('')
    setBusy(true)
    try {
      await createSchedule(flowID, cron.trim())
      setCron('')
      await load()
    } catch (err) {
      setErr(apiError(err))
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (s: FlowSchedule) => {
    setErr('')
    try {
      await setScheduleEnabled(flowID, s.id, !s.enabled)
      await load()
    } catch (err) {
      setErr(apiError(err))
    }
  }

  const remove = async (s: FlowSchedule) => {
    setErr('')
    try {
      await deleteSchedule(flowID, s.id)
      await load()
    } catch (err) {
      setErr(apiError(err))
    }
  }

  return (
    <div className="card side-card">
      <h3>定时调度</h3>
      <form className="row tight" onSubmit={add}>
        <input placeholder="cron 表达式" value={cron} onChange={(e) => setCron(e.target.value)} />
        <BusyButton type="submit" className="primary" busy={busy}>
          添加
        </BusyButton>
      </form>
      {err && <ErrorNote>{err}</ErrorNote>}
      <div className="list">
        {schedules.map((s) => (
          <div key={s.id} className="run-row">
            <span className={`badge ${s.enabled ? 'success' : ''}`}>{s.enabled ? '启用' : '暂停'}</span>
            <span className="mono">{s.cron}</span>
            <button className="link" onClick={() => toggle(s)}>
              {s.enabled ? '暂停' : '恢复'}
            </button>
            <button className="link danger" onClick={() => remove(s)}>
              删除
            </button>
          </div>
        ))}
        {schedules.length === 0 && (
          <EmptyState compact icon={<Clock size={20} strokeWidth={1.5} aria-hidden="true" />} title="暂无定时任务" />
        )}
      </div>
    </div>
  )
}
