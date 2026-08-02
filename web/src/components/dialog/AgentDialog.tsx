import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentEvent, AgentSession, PauseAnswer, PauseQuestion } from '../../api/agent'
import { agentNew, agentResume, agentSession } from '../../api/agent'
import { apiError } from '../../api/client'
import type { FlowTree } from '../../api/flow'
import type { Provider } from '../../api/providers'
import { openSSE } from '../../sse'
import { NODE_LABELS } from '../../lib/tree'

interface Props {
  flowID: number
  providers: Provider[]
  tree: FlowTree
  onChanged: () => void
}

interface Message {
  role: string
  content?: string
  tool_calls?: unknown[]
  tool_call_id?: string
}

export default function AgentDialog({ flowID, providers, tree, onChanged }: Props) {
  const [providerID, setProviderID] = useState(0)
  const [instruction, setInstruction] = useState('')
  const [mode, setMode] = useState<'edit' | 'generate'>('generate')
  const [selected, setSelected] = useState<string[]>([])
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [messages, setMessages] = useState<Message[]>([])
  const [questions, setQuestions] = useState<PauseQuestion[]>([])
  const [status, setStatus] = useState('active')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const abortRef = useRef<{ abort: () => void } | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setProviderID((cur) => cur || providers[0]?.id || 0)
  }, [providers])

  const refreshSession = useCallback(async () => {
    try {
      const s = await agentSession(flowID)
      setSession(s)
    } catch (e) {
      setErr(apiError(e))
    }
  }, [flowID])

  useEffect(() => {
    refreshSession()
    return () => abortRef.current?.abort()
  }, [refreshSession])

  const setSession = (s: AgentSession) => {
    setStatus(s.status)
    setMessages((s.messages as Message[]) ?? [])
    setQuestions(s.pending_questions ?? [])
  }

  const scrollBottom = () => {
    boxRef.current?.scrollTo({ top: boxRef.current.scrollHeight })
  }

  const toggleNode = (id: string) => {
    setSelected((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))
  }

  const submit = () => {
    if (!instruction.trim()) return
    if (!providerID) {
      setErr('请先在 Provider 配置中启用一个 LLM Provider')
      return
    }
    setErr('')
    setBusy(true)
    setEvents([])
    setMessages((cur) => [...cur, { role: 'user', content: instruction.trim() }])
    scrollBottom()
    const body = {
      provider_id: providerID,
      instruction: instruction.trim(),
      selected_nodes: selected.length ? selected : undefined,
      mode,
    }
    const url = `/api/flow/flows/${flowID}/agent/submit`
    abortRef.current = openSSE(url, body, {
      onEvent: (ev) => {
        setEvents((cur) => [...cur, ev])
        scrollBottom()
      },
      onDone: async () => {
        setBusy(false)
        setInstruction('')
        await refreshSession()
        onChanged()
      },
      onError: (msg) => {
        setErr(msg)
        setBusy(false)
      },
      onDisconnect: () => {
        setErr('连接中断,可重试;会话历史已保留,恢复上下文。')
        setBusy(false)
      },
    })
  }

  const resume = async (answers: PauseAnswer[]) => {
    setBusy(true)
    setErr('')
    try {
      const res = await agentResume(flowID, providerID, answers)
      setEvents(res.events)
      await refreshSession()
      onChanged()
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const newSession = async () => {
    if (!confirm('清空当前会话历史?')) return
    try {
      await agentNew(flowID)
      await refreshSession()
      setEvents([])
      onChanged()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const nodeIDs = Object.keys(tree.nodes ?? {})

  return (
    <div className="card side-card">
      <h3>生成助手</h3>
      <div className="row tight">
        <label className="muted">Provider</label>
        <select value={providerID} onChange={(e) => setProviderID(Number(e.target.value))}>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name} ({p.model})
            </option>
          ))}
        </select>
      </div>
      <div className="row tight">
        <label className="muted">模式</label>
        <select value={mode} onChange={(e) => setMode(e.target.value as 'edit' | 'generate')}>
          <option value="generate">生成</option>
          <option value="edit">编辑</option>
        </select>
        <button className="link" onClick={newSession}>
          新会话
        </button>
      </div>
      <div className="muted">限定节点范围</div>
      <div className="node-tags">
        <button
          className={selected.length === nodeIDs.length ? 'tag on' : 'tag'}
          onClick={() => setSelected(nodeIDs.length ? [...nodeIDs] : [])}
        >
          全部
        </button>
        {nodeIDs.map((id) => (
          <button
            key={id}
            className={selected.includes(id) ? 'tag on' : 'tag'}
            onClick={() => toggleNode(id)}
            title={id}
          >
            {NODE_LABELS[tree.nodes[id]?.type ?? ''] ?? tree.nodes[id]?.type}
          </button>
        ))}
      </div>
      <textarea
        rows={3}
        placeholder={mode === 'generate' ? '描述要生成的测试流,如:登录后查询订单列表' : '描述要修改的内容'}
        value={instruction}
        onChange={(e) => setInstruction(e.target.value)}
      />
      <button onClick={submit} disabled={busy}>
        {busy ? '执行中…' : '提交'}
      </button>
      {err && <p className="err">{err}</p>}
      <div className="log-box" ref={boxRef}>
        {messages
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => (
            <div key={i} className={`chat-msg ${m.role}`}>
              <div className="chat-role">{m.role === 'user' ? '用户' : '助手'}</div>
              {m.content && <div>{m.content}</div>}
            </div>
          ))}
        {events.map((ev, i) => (
          <div key={`ev-${i}`} className="chat-msg tool">
            <div className="chat-role">
              #{ev.round} {ev.tool}
            </div>
            <div className="mono">{JSON.stringify(ev.result)}</div>
          </div>
        ))}
      </div>
      {status === 'paused' && questions.length > 0 && (
        <div className="card sub">
          <div className="strong">需要确认</div>
          {questions.map((q) => (
            <QuestionRow key={q.id} q={q} onSubmit={(a) => resume([{ question_id: q.id, answer: a }])} />
          ))}
        </div>
      )}
    </div>
  )
}

function QuestionRow({ q, onSubmit }: { q: PauseQuestion; onSubmit: (answer: string) => void }) {
  const [answer, setAnswer] = useState('')
  return (
    <div className="stack">
      <div className="muted">
        {q.type}: {q.question}
      </div>
      {q.options?.length ? (
        <div className="node-tags">
          {q.options.map((o) => (
            <button key={o} className="tag" onClick={() => onSubmit(o)}>
              {o}
            </button>
          ))}
        </div>
      ) : (
        <div className="row tight">
          <input value={answer} onChange={(e) => setAnswer(e.target.value)} placeholder="回答" />
          <button onClick={() => onSubmit(answer)}>提交</button>
        </div>
      )}
    </div>
  )
}
