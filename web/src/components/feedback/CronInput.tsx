import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { describeCron } from '../../api/schedule'

interface Props {
  value: string
  onChange: (cron: string) => void
  error?: string
}

const SEC_OPTS = ['*', '0', '*/5', '*/10', '*/15', '*/20', '*/30', '5', '10', '15', '20', '25', '30', '35', '40', '45', '50', '55']
const MINUTE_OPTS = ['*', '0', '5', '10', '15', '20', '25', '30', '35', '40', '45', '50', '55', '*/5', '*/10', '*/15', '*/30']
const HOUR_OPTS = ['*', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12', '13', '14', '15', '16', '17', '18', '19', '20', '21', '22', '23', '*/2', '*/4', '*/6']
const DAY_OPTS = ['*', '1', '2', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12', '13', '14', '15', '16', '17', '18', '19', '20', '21', '22', '23', '24', '25', '26', '27', '28', '29', '30', '31']
const MONTH_OPTS = ['*', '1', '2', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12']
const DOW_OPTS = [
  { v: '*', label: '每天' },
  { v: '0', label: '周日' },
  { v: '1', label: '周一' },
  { v: '2', label: '周二' },
  { v: '3', label: '周三' },
  { v: '4', label: '周四' },
  { v: '5', label: '周五' },
  { v: '6', label: '周六' },
  { v: '1-5', label: '工作日' },
  { v: '0,6', label: '周末' },
]

const PRESETS: { label: string; cron: string }[] = [
  { label: '每30秒', cron: '*/30 * * * * *' },
  { label: '每分钟', cron: '* * * * *' },
  { label: '每5分钟', cron: '*/5 * * * *' },
  { label: '每30分钟', cron: '*/30 * * * *' },
  { label: '每小时整点', cron: '0 * * * *' },
  { label: '每天零点', cron: '0 0 * * *' },
  { label: '每天8点', cron: '0 8 * * *' },
  { label: '每天12点', cron: '0 12 * * *' },
  { label: '每天18点', cron: '0 18 * * *' },
  { label: '每周一零点', cron: '0 0 * * 1' },
  { label: '每月1号零点', cron: '0 0 1 * *' },
]

type Mode = 'preset' | 'visual' | 'text'

export default function CronInput({ value, onChange, error }: Props) {
  const [mode, setMode] = useState<Mode>('preset')
  const [desc, setDesc] = useState('')
  const [nextRuns, setNextRuns] = useState<string[]>([])
  const [valid, setValid] = useState(true)
  const debounce = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  // 当前是几段 cron
  const is6 = useMemo(() => value.trim().split(/\s+/).length === 6, [value])

  // 将 cron 拆成字段（支持 5 段和 6 段）
  const fields = useMemo(() => {
    const parts = value.trim().split(/\s+/)
    if (parts.length === 6) {
      return {
        second: parts[0] || '*',
        minute: parts[1] || '*',
        hour: parts[2] || '*',
        day: parts[3] || '*',
        month: parts[4] || '*',
        dow: parts[5] || '*',
      }
    }
    return {
      second: '*',
      minute: parts[0] || '*',
      hour: parts[1] || '*',
      day: parts[2] || '*',
      month: parts[3] || '*',
      dow: parts[4] || '*',
    }
  }, [value])

  // 查询 cron 描述
  const fetchDesc = useCallback(
    (cron: string) => {
      if (!cron.trim()) {
        setDesc('')
        setNextRuns([])
        setValid(true)
        return
      }
      describeCron(cron).then((r) => {
        setDesc(r.description)
        setNextRuns(r.next_runs ?? [])
        setValid(r.valid)
      }).catch(() => {
        setDesc('')
        setNextRuns([])
        setValid(true)
      })
    },
    [],
  )

  useEffect(() => {
    if (debounce.current) clearTimeout(debounce.current)
    debounce.current = setTimeout(() => fetchDesc(value), 300)
    return () => {
      if (debounce.current) clearTimeout(debounce.current)
    }
  }, [value, fetchDesc])

  const setField = (idx: number, v: string) => {
    const p = value.trim().split(/\s+/)
    const want6 = is6
    // 补齐到目标段数
    while (p.length < (want6 ? 6 : 5)) p.push('*')
    if (!want6 && p.length === 6) p.shift() // 从6段切回5段时，去掉秒
    p[idx] = v
    onChange(p.join(' '))
  }

  const toggleSeconds = () => {
    const p = value.trim().split(/\s+/)
    if (is6) {
      // 切回 5 段：去掉第一个字段
      p.shift()
      onChange(p.join(' '))
    } else {
      // 切换到 6 段：在前面加秒字段
      p.unshift('0')
      onChange(p.join(' '))
    }
  }

  const applyPreset = (cron: string) => {
    onChange(cron)
  }

  return (
    <div className="cron-input">
      {/* 模式切换 */}
      <div className="row tight" style={{ marginBottom: 8 }}>
        <button
          className={`link ${mode === 'preset' ? 'tag on' : 'tag'}`}
          onClick={() => setMode('preset')}
          type="button"
        >
          快速
        </button>
        <button
          className={`link ${mode === 'visual' ? 'tag on' : 'tag'}`}
          onClick={() => setMode('visual')}
          type="button"
        >
          可视化
        </button>
        <button
          className={`link ${mode === 'text' ? 'tag on' : 'tag'}`}
          onClick={() => setMode('text')}
          type="button"
        >
          手动
        </button>
      </div>

      {/* 模式内容 */}
      {mode === 'preset' && (
        <div className="cron-presets">
          {PRESETS.map((p) => (
            <button
              key={p.cron}
              type="button"
              className={`tag clickable ${value.trim() === p.cron ? 'on' : ''}`}
              onClick={() => applyPreset(p.cron)}
            >
              {p.label}
            </button>
          ))}
        </div>
      )}

      {mode === 'visual' && (
        <div className="cron-visual">
          {/* 秒级开关 */}
          <label className="row tight" style={{ marginBottom: 6, fontSize: 13, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={is6}
              onChange={toggleSeconds}
              style={{ width: 16, height: 16 }}
            />
            精确到秒（6段）
          </label>
          <div className="cron-fields">
            {is6 && (
              <label>
                秒
                <select value={fields.second} onChange={(e) => setField(0, e.target.value)}>
                  {SEC_OPTS.map((v) => (
                    <option key={v} value={v}>{v}</option>
                  ))}
                </select>
              </label>
            )}
            <label>
              分
              <select value={fields.minute} onChange={(e) => setField(is6 ? 1 : 0, e.target.value)}>
                {MINUTE_OPTS.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            </label>
            <label>
              时
              <select value={fields.hour} onChange={(e) => setField(is6 ? 2 : 1, e.target.value)}>
                {HOUR_OPTS.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            </label>
            <label>
              日
              <select value={fields.day} onChange={(e) => setField(is6 ? 3 : 2, e.target.value)}>
                {DAY_OPTS.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            </label>
            <label>
              月
              <select value={fields.month} onChange={(e) => setField(is6 ? 4 : 3, e.target.value)}>
                {MONTH_OPTS.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            </label>
            <label>
              周
              <select value={fields.dow} onChange={(e) => setField(is6 ? 5 : 4, e.target.value)}>
                {DOW_OPTS.map((o) => (
                  <option key={o.v} value={o.v}>{o.label}</option>
                ))}
              </select>
            </label>
          </div>
        </div>
      )}

      {mode === 'text' && (
        <input
          placeholder={is6 ? '秒 分 时 日 月 周  如: */30 * * * * *' : '分 时 日 月 周  如: 0 */2 * * *'}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={!valid || error ? 'err' : ''}
        />
      )}

      {/* 当前表达式 */}
      <div className="cron-current mono" style={{ marginTop: 8, fontSize: 13 }}>
        <span className="muted">表达式：</span>
        <span className={!valid ? 'err' : ''}>{value || '(空)'}</span>
        {is6 && <span className="badge" style={{ marginLeft: 6 }}>6段</span>}
      </div>

      {/* 描述 */}
      {desc && (
        <div className="cron-desc" style={{ marginTop: 4, fontSize: 13, color: valid ? 'var(--ok)' : 'var(--danger)' }}>
          {desc}
        </div>
      )}

      {/* 未来执行时间 */}
      {nextRuns.length > 0 && (
        <div className="cron-next" style={{ marginTop: 4 }}>
          <div className="muted" style={{ fontSize: 12, marginBottom: 2 }}>接下来 5 次执行：</div>
          {nextRuns.map((t, i) => (
            <div key={i} className="mono" style={{ fontSize: 12, color: 'var(--muted)' }}>
              {t}
            </div>
          ))}
        </div>
      )}

      {/* 外部传入的错误 */}
      {error && <div className="err" style={{ marginTop: 4, fontSize: 13 }}>{error}</div>}
    </div>
  )
}
