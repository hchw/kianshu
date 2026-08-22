import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentEvent, AgentSession, PauseAnswer, PauseQuestion } from '../../api/agent'
import { agentCompress, agentNew, agentSession } from '../../api/agent'
import { updateFlowThinking } from '../../api/flow'
import { apiError } from '../../api/client'
import type { FlowTree } from '../../api/flow'
import type { Provider } from '../../api/providers'
import { openSSE } from '../../sse'
import { NODE_LABELS, applyToolMutation } from '../../lib/tree'
import { BusyButton } from '../feedback/BusyButton'
import { ErrorNote } from '../feedback/ErrorNote'
import PopConfirm from './PopConfirm'

interface Props {
  flowID: number
  providers: Provider[]
  tree: FlowTree
  systemPrompt?: string
  initialThinking?: string
  onChanged: () => void
  onTreePreview: (t: FlowTree) => void
}

interface Message {
  role: string
  content?: string
  tool_calls?: unknown[]
  tool_call_id?: string
}

export default function AgentDialog({ flowID, providers, tree, systemPrompt, initialThinking, onChanged, onTreePreview }: Props) {
  const [providerID, setProviderID] = useState(0)
  const [instruction, setInstruction] = useState('')
  const [mode, setMode] = useState<'edit' | 'generate'>('generate')
  const [thinking, setThinking] = useState(initialThinking ?? 'disabled')
  // 只在 initialThinking 真正变化（切换流/刷新后重新加载草稿）时同步刷新。
  // 保存优先级：用户手动选择最高，其次是后端回退；此处仅作初值兜底。
  const lastInitial = useRef(initialThinking)
  useEffect(() => {
    if (lastInitial.current !== initialThinking) {
      lastInitial.current = initialThinking
      setThinking(initialThinking ?? 'disabled')
    }
  }, [initialThinking])
  const [selected, setSelected] = useState<string[]>([])
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [messages, setMessages] = useState<Message[]>([])
  const [questions, setQuestions] = useState<PauseQuestion[]>([])
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [status, setStatus] = useState('active')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [roundTexts, setRoundTexts] = useState<Record<number, string>>({})
  const [liveRound, setLiveRound] = useState<number | null>(null)
  const [liveText, setLiveText] = useState('')
  // 深度思考内容：每轮一个可折叠块，展示-only(不回传给 LLM)。
  const [roundReasonings, setRoundReasonings] = useState<Record<number, string>>({})
  const [liveReasoning, setLiveReasoning] = useState('')
  const liveRoundRef = useRef<number | null>(null)
  const liveTextRef = useRef('')
  const liveReasoningRef = useRef('')
  const abortRef = useRef<{ abort: () => void } | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const subRef = useRef<{ abort: () => void } | null>(null)
  const treeRef = useRef(tree)
  treeRef.current = tree
  const onChangedRef = useRef(onChanged)
  onChangedRef.current = onChanged
  const onTreePreviewRef = useRef(onTreePreview)
  onTreePreviewRef.current = onTreePreview

  useEffect(() => {
    setProviderID((cur) => cur || providers[0]?.id || 0)
  }, [providers])

  const refreshSession = useCallback(async () => {
    try {
      const s = await agentSession(flowID)
      setSession(s)
      return s
    } catch (e) {
      setErr(apiError(e))
      return null
    }
  }, [flowID])

  // 订阅正在运行中的 agent 事件流（页面刷新后断线重连）
  const subscribeRun = useCallback(() => {
    subRef.current?.abort()
    const url = `/api/flow/flows/${flowID}/agent/subscribe?since=0`
    // 深克隆当前树作为快照
    let workingTree: FlowTree = JSON.parse(JSON.stringify(treeRef.current))
    const snapshot = workingTree

    subRef.current = openSSE(url, {}, {
      onEvent: (ev) => {
        if (ev.kind === 'round') {
          liveRoundRef.current = ev.round
          liveTextRef.current = ''
          liveReasoningRef.current = ''
          setLiveRound(ev.round)
          setLiveText('')
          setLiveReasoning('')
        } else if (ev.kind === 'text') {
          liveTextRef.current += ev.text ?? ''
          if (liveRoundRef.current != null) {
            setLiveRound(liveRoundRef.current)
            setLiveText(liveTextRef.current)
          }
        } else if (ev.kind === 'reasoning') {
          liveReasoningRef.current += ev.text ?? ''
          if (liveRoundRef.current != null) {
            setLiveRound(liveRoundRef.current)
            setLiveReasoning(liveReasoningRef.current)
          }
        } else {
          if (liveRoundRef.current != null && liveTextRef.current) {
            setRoundTexts((r) => ({ ...r, [liveRoundRef.current!]: liveTextRef.current }))
          }
          if (liveRoundRef.current != null && liveReasoningRef.current) {
            setRoundReasonings((r) => ({
              ...r,
              [liveRoundRef.current!]: liveReasoningRef.current,
            }))
          }
          liveRoundRef.current = null
          liveTextRef.current = ''
          liveReasoningRef.current = ''
          setLiveRound(null)
          setLiveText('')
          setLiveReasoning('')
          setEvents((cur) => [...cur, ev])
          if (ev.tool && ev.result) {
            const next = applyToolMutation(workingTree, ev)
            if (next !== workingTree) {
              workingTree = next
              onTreePreviewRef.current(next)
            }
          }
        }
        scrollBottom()
      },
      onDone: async () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
        await refreshSession()
        onChangedRef.current()
      },
      onError: (msg, status) => {
        // 404 表示后端没有正在运行的 Agent 任务（断线重连时属正常情况），不在页面上报错
        if (status === 404) return
        setErr(msg)
        onTreePreviewRef.current(snapshot)
      },
      onDisconnect: () => {
        // 连接中断但运行可能还在继续，尝试刷新会话
        refreshSession()
      },
      onAbort: () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
      },
    })
  }, [flowID, refreshSession])

  useEffect(() => {
    const init = async () => {
      const s = await refreshSession()
      // 如果会话正在运行中（页面刷新后），自动重连 SSE
      if (s && s.status === 'active' && s.messages && (s.messages as Message[]).length > 0) {
        subscribeRun()
      }
    }
    init()
    return () => {
      abortRef.current?.abort()
      subRef.current?.abort()
    }
  }, [refreshSession, subscribeRun])

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
    setRoundTexts({})
    setLiveRound(null)
    setLiveText('')
    liveRoundRef.current = null
    liveTextRef.current = ''
    setMessages((cur) => [...cur, { role: 'user', content: instruction.trim() }])
    scrollBottom()

    // 深克隆当前树作为快照，用于失败/中止/断连时恢复
    const snapshot: FlowTree = JSON.parse(JSON.stringify(tree))
    // workingTree 追踪 agent 逐步变异后的中间状态
    let workingTree: FlowTree = snapshot

    const body = {
      provider_id: providerID,
      instruction: instruction.trim(),
      selected_nodes: selected.length ? selected : undefined,
      mode,
      system_prompt: systemPrompt,
    }
    const url = `/api/flow/flows/${flowID}/agent/submit`
    abortRef.current = openSSE(url, body, {
      onEvent: (ev) => {
        if (ev.kind === 'round') {
          liveRoundRef.current = ev.round
          liveTextRef.current = ''
          liveReasoningRef.current = ''
          setLiveRound(ev.round)
          setLiveText('')
          setLiveReasoning('')
        } else if (ev.kind === 'text') {
          liveTextRef.current += ev.text ?? ''
          if (liveRoundRef.current != null) {
            setLiveRound(liveRoundRef.current)
            setLiveText(liveTextRef.current)
          }
        } else if (ev.kind === 'reasoning') {
          liveReasoningRef.current += ev.text ?? ''
          if (liveRoundRef.current != null) {
            setLiveRound(liveRoundRef.current)
            setLiveReasoning(liveReasoningRef.current)
          }
        } else {
          // tool event – finalize this round's accumulated assistant text
          if (liveRoundRef.current != null && liveTextRef.current) {
            setRoundTexts((r) => ({ ...r, [liveRoundRef.current!]: liveTextRef.current }))
          }
          if (liveRoundRef.current != null && liveReasoningRef.current) {
            setRoundReasonings((r) => ({
              ...r,
              [liveRoundRef.current!]: liveReasoningRef.current,
            }))
          }
          liveRoundRef.current = null
          liveTextRef.current = ''
          liveReasoningRef.current = ''
          setLiveRound(null)
          setLiveText('')
          setLiveReasoning('')
          setEvents((cur) => [...cur, ev])

          // 变异类工具事件 → 本地回放到画布
          if (ev.tool && ev.result) {
            const next = applyToolMutation(workingTree, ev)
            if (next !== workingTree) {
              workingTree = next
              onTreePreview(next)
            }
          }
        }
        scrollBottom()
      },
      onDone: async () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
        setBusy(false)
        setInstruction('')
        await refreshSession()
        onChanged()
      },
      onError: (msg) => {
        setErr(msg)
        setBusy(false)
        onTreePreview(snapshot)
      },
      onDisconnect: () => {
        setErr('连接中断,可重试;会话历史已保留,恢复上下文。')
        setBusy(false)
        onTreePreview(snapshot)
      },
      onAbort: async () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
        setBusy(false)
        onTreePreview(snapshot)
        await refreshSession()
        onChanged()
      },
    })
  }

  const resume = (answers: PauseAnswer[]) => {
    if (!providerID) {
      setErr('请先在 Provider 配置中启用一个 LLM Provider')
      return
    }
    setErr('')
    setBusy(true)
    setEvents([])
    setRoundTexts({})
    setLiveRound(null)
    setLiveText('')
    liveRoundRef.current = null
    liveTextRef.current = ''
    scrollBottom()

    // 深克隆当前树作为快照，用于失败/中止/断连时恢复
    const snapshot: FlowTree = JSON.parse(JSON.stringify(tree))
    let workingTree: FlowTree = snapshot

    const url = `/api/flow/flows/${flowID}/agent/resume`
    abortRef.current = openSSE(url, { provider_id: providerID, answers }, {
      onEvent: (ev) => {
        if (ev.kind === 'round') {
          liveRoundRef.current = ev.round
          liveTextRef.current = ''
          setLiveRound(ev.round)
          setLiveText('')
        } else if (ev.kind === 'text') {
          liveTextRef.current += ev.text ?? ''
          if (liveRoundRef.current != null) {
            setLiveRound(liveRoundRef.current)
            setLiveText(liveTextRef.current)
          }
        } else {
          // tool event – finalize this round's accumulated assistant text
          if (liveRoundRef.current != null && liveTextRef.current) {
            setRoundTexts((r) => ({ ...r, [liveRoundRef.current!]: liveTextRef.current }))
          }
          liveRoundRef.current = null
          liveTextRef.current = ''
          setLiveRound(null)
          setLiveText('')
          setEvents((cur) => [...cur, ev])

          // 变异类工具事件 → 本地回放到画布
          if (ev.tool && ev.result) {
            const next = applyToolMutation(workingTree, ev)
            if (next !== workingTree) {
              workingTree = next
              onTreePreview(next)
            }
          }
        }
        scrollBottom()
      },
      onDone: async () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
        setBusy(false)
        await refreshSession()
        onChanged()
      },
      onError: (msg) => {
        setErr(msg)
        setBusy(false)
        onTreePreview(snapshot)
      },
      onDisconnect: () => {
        setErr('连接中断,可重试;会话历史已保留,恢复上下文。')
        setBusy(false)
        onTreePreview(snapshot)
      },
      onAbort: async () => {
        setLiveRound(null)
        setLiveText('')
        liveRoundRef.current = null
        liveTextRef.current = ''
        setBusy(false)
        onTreePreview(snapshot)
        await refreshSession()
        onChanged()
      },
    })
  }

  const newSession = async () => {
    try {
      await agentNew(flowID)
      await refreshSession()
      setEvents([])
      onChanged()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const compressSession = async () => {
    try {
      await agentCompress(flowID)
      await refreshSession()
      setEvents([])
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
        <PopConfirm danger message="清空当前会话历史?" onConfirm={newSession}>
                <button className="link">新会话</button>
              </PopConfirm>
      </div>
      <div className="row tight">
        <label className="muted">深度思考</label>
        <select
          value={thinking}
          onChange={(e) => {
            const v = e.target.value
            setThinking(v)
            updateFlowThinking(flowID, v)
              .then(() => {
                console.log(`[thinking] 已保存 flow=${flowID} thinking=${v}`)
              })
              .catch((err) => {
                // 保存失败也要让用户看到，避免"选了却没生效"的困惑。
                console.error(`[thinking] 保存失败 flow=${flowID}:`, err)
                setErr('深度思考偏好保存失败，本次仍按所选级别生效')
              })
          }}
        >
          <option value="disabled">关闭</option>
          <option value="unset">不设置</option>
          <option value="low">低</option>
          <option value="high">高</option>
          <option value="max">最高</option>
        </select>
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
            {NODE_LABELS[tree.nodes[id]?.type ?? ''] ?? tree.nodes[id]?.type} · {id}
          </button>
        ))}
      </div>
      <textarea
        rows={3}
        placeholder={mode === 'generate' ? '描述要生成的测试流,如:登录后查询订单列表' : '描述要修改的内容'}
        value={instruction}
        onChange={(e) => setInstruction(e.target.value)}
      />
      <div className="row tight">
        <BusyButton className="primary" onClick={submit} busy={busy}>
          {busy ? '执行中…' : '提交'}
        </BusyButton>
        {busy && (
          <button className="danger" onClick={() => abortRef.current?.abort()}>
            停止
          </button>
        )}
      </div>
      {err && <ErrorNote>{err}</ErrorNote>}
      <div className="log-box" ref={boxRef}>
        {messages
          .filter((m) => m.role === 'user' || m.role === 'assistant')
          .map((m, i) => (
            <div key={i} className={`chat-msg ${m.role}`}>
              <div className="chat-role">{m.role === 'user' ? '用户' : '助手'}</div>
              {m.content && <div>{m.content}</div>}
            </div>
          ))}
        {events.map((ev, i) => {
          const isFirstInRound = i === 0 || events[i - 1].round !== ev.round
          const rText = roundTexts[ev.round]
          const rReason = roundReasonings[ev.round]
          return (
            <div key={`ev-${i}`}>
              {isFirstInRound && (rText || rReason) && (
                <div className="chat-msg assistant">
                  <div className="chat-role">助手 #{ev.round}</div>
                  {rReason && (
                    <details className="chat-reasoning">
                      <summary>深度思考</summary>
                      <div className="chat-text mono">{rReason}</div>
                    </details>
                  )}
                  {rText && <div className="chat-text">{rText}</div>}
                </div>
              )}
              <div className="chat-msg tool">
                <div className="chat-role">
                  #{ev.round} {ev.tool}
                </div>
                <div className="mono">{JSON.stringify(ev.result)}</div>
              </div>
            </div>
          )
        })}
        {liveRound != null && (
          <div className="chat-msg assistant live">
            <div className="chat-role">助手 #{liveRound}</div>
            {liveReasoning && (
              <details className="chat-reasoning" open={liveReasoning.length > 0}>
                <summary>深度思考</summary>
                <div className="chat-text mono">{liveReasoning}</div>
              </details>
            )}
            <div className="chat-text">{liveText}</div>
          </div>
        )}
      </div>
      <div className="row tight">
        <PopConfirm danger message="压缩会话历史将保留系统提示和操作摘要,去除冗余的工具调用记录以节省 token。继续?" onConfirm={compressSession}>
                <button className="link" disabled={busy}>压缩对话</button>
              </PopConfirm>
      </div>
      {status === 'paused' && questions.length > 0 && (
        <div className="card sub">
          <div className="strong">需要确认</div>
          {questions.map((q) => (
            <div key={q.id} className="stack">
              <div className="muted">
                {q.type}: {q.question}
              </div>
              {q.options?.length ? (
                <div className="node-tags">
                  {q.options.map((o) => (
                    <button
                      key={o}
                      className={answers[q.id] === o ? 'tag on' : 'tag'}
                      onClick={() => setAnswers((a) => ({ ...a, [q.id]: o }))}
                    >
                      {o}
                    </button>
                  ))}
                </div>
              ) : (
                <input
                  value={answers[q.id] ?? ''}
                  onChange={(e) => setAnswers((a) => ({ ...a, [q.id]: e.target.value }))}
                  placeholder="回答"
                />
              )}
            </div>
          ))}
          <button
            className="primary"
            disabled={busy}
            onClick={() => {
              const ans: PauseAnswer[] = questions
                .filter((q) => answers[q.id]?.trim())
                .map((q) => ({ question_id: q.id, answer: answers[q.id].trim() }))
              if (ans.length === 0) return
              resume(ans)
              setAnswers({})
            }}
          >
            {busy ? '提交中…' : '提交全部回答'}
          </button>
        </div>
      )}
    </div>
  )
}


