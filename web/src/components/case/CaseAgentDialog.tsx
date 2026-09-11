import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentEvent } from '../../api/agent'
import { apiError } from '../../api/client'
import { openSSE } from '../../sse'
import { useToast } from '../feedback/Toast'
import './CaseAgentDialog.css'
import type { Provider } from '../../api/providers'
import type { CaseNode } from '../../api/caseFlow'
import { caseAgentCompress, caseAgentNew, caseAgentSession } from '../../api/caseFlow'

type Props = { caseFlowID: number; providers: Provider[]; tree: CaseNode | null; onChanged: () => void; onTreePreview: (tree: CaseNode) => void }
type Message = { role: string; content?: string }
type PauseQuestion = { id: string; type: string; question: string; options?: string[] }

export default function CaseAgentDialog({ caseFlowID, providers, tree, onChanged, onTreePreview }: Props) {
  const toast = useToast()
  const [providerID, setProviderID] = useState(0)
  const [thinking, setThinking] = useState('disabled')
  const [instruction, setInstruction] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [liveText, setLiveText] = useState('')
  const [liveReasoning, setLiveReasoning] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [questions, setQuestions] = useState<PauseQuestion[]>([])
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [status, setStatus] = useState('active')
  const treeRef = useRef(tree)
  treeRef.current = tree
  const abortRef = useRef<{ abort: () => void } | null>(null)

  useEffect(() => { setProviderID((current) => current || providers[0]?.id || 0) }, [providers])
  const refresh = useCallback(async () => {
    try {
      const session = await caseAgentSession(caseFlowID)
      try { setMessages(JSON.parse(session.messages || '[]') as Message[]) } catch { setMessages([]) }
      setStatus(session.status)
      try { setQuestions(session.pending_questions ? JSON.parse(session.pending_questions) as PauseQuestion[] : []) } catch { setQuestions([]) }
    } catch (e) { setError(apiError(e)) }
  }, [caseFlowID])
  useEffect(() => { void refresh(); return () => abortRef.current?.abort() }, [refresh])

  const submit = () => {
    if (!providerID || !instruction.trim() || busy) return
    setBusy(true); setError(''); setEvents([]); setLiveText(''); setLiveReasoning('')
    const text = instruction.trim()
    setInstruction(''); setMessages((current) => [...current, { role: 'user', content: text }])
    abortRef.current = openSSE(`/api/case-flows/${caseFlowID}/agent/submit`, { provider_id: providerID, instruction: text, thinking }, {
      onEvent: (event) => {
        if (event.kind === 'text') setLiveText((current) => current + (event.text ?? ''))
        else if (event.kind === 'reasoning') setLiveReasoning((current) => current + (event.text ?? ''))
        else {
          setEvents((current) => [...current, event]); setLiveText(''); setLiveReasoning('')
          const data = (event.result as { data?: { root?: CaseNode } } | undefined)?.data
          if (event.tool && data?.root) { treeRef.current = data.root; onTreePreview(data.root) }
          if (event.tool) onChanged()
        }
      },
      onDone: async () => { setBusy(false); await refresh(); onChanged() },
      onError: (message) => { setBusy(false); setError(message) },
      onDisconnect: () => { setBusy(false); setError('连接中断，会话历史已保留，可以继续发送') },
      onAbort: () => setBusy(false),
    })
  }

  const resume = () => {
    const selected = questions.filter((question) => answers[question.id]?.trim()).map((question) => ({ question_id: question.id, answer: answers[question.id].trim() }))
    if (!providerID || selected.length === 0 || busy) return
    setBusy(true); setError(''); setEvents([])
    abortRef.current = openSSE(`/api/case-flows/${caseFlowID}/agent/resume`, { provider_id: providerID, answers: selected, thinking }, {
      onEvent: (event) => {
        if (event.kind === 'text') setLiveText((current) => current + (event.text ?? ''))
        else if (event.kind === 'reasoning') setLiveReasoning((current) => current + (event.text ?? ''))
        else {
          setEvents((current) => [...current, event]); setLiveText(''); setLiveReasoning('')
          const data = (event.result as { data?: { root?: CaseNode } } | undefined)?.data
          if (event.tool && data?.root) { treeRef.current = data.root; onTreePreview(data.root) }
          if (event.tool) onChanged()
        }
      },
      onDone: async () => { setBusy(false); setAnswers({}); await refresh(); onChanged() },
      onError: (message) => { setBusy(false); setError(message) },
      onDisconnect: () => { setBusy(false); setError('连接中断，会话历史已保留') },
      onAbort: () => setBusy(false),
    })
  }

  const reset = async () => {
 try {
  abortRef.current?.abort()
  setQuestions([])
  setAnswers({})
  setStatus('active')
  setMessages([])
  setEvents([])
  setLiveText('')
  setLiveReasoning('')
  setError('')
  await caseAgentNew(caseFlowID)
  await refresh()
  toast.success('已开始新会话')
 } catch (e) { setError(apiError(e)) }
}
  const compress = async () => { try { await caseAgentCompress(caseFlowID); await refresh(); toast.success('会话已压缩') } catch (e) { setError(apiError(e)) } }

  return <div className="card side-card case-agent-dialog">
    <div className="row"><h3 style={{ flex: 1 }}>用例生成助手</h3><button className="link" onClick={reset}>新会话</button><button className="link" onClick={compress}>压缩</button></div>
    <div className="row tight"><label className="muted">Provider</label><select value={providerID} onChange={(e) => setProviderID(Number(e.target.value))}>{providers.map((p) => <option key={p.id} value={p.id}>{p.name} ({p.model})</option>)}</select><label className="muted">思考</label><select value={thinking} onChange={(e) => setThinking(e.target.value)}><option value="disabled">关闭</option><option value="low">低</option><option value="high">高</option><option value="max">最高</option></select></div>
    <div className="case-agent-history">{messages.map((message, index) => <div className={`agent-message ${message.role}`} key={index}><strong>{message.role === 'user' ? '你' : 'AI'}</strong><div>{message.content}</div></div>)}{liveReasoning && <details open><summary>思考中…</summary><pre>{liveReasoning}</pre></details>}{liveText && <div className="agent-message assistant"><strong>AI</strong><div>{liveText}</div></div>}{events.map((event, index) => <details className="agent-event" key={index}><summary><span>第 {event.round} 轮</span> {event.tool ? `工具：${event.tool}` : '已完成一轮工具调用'}</summary>{event.args !== undefined && <pre>参数：{typeof event.args === 'string' ? event.args : JSON.stringify(event.args, null, 2)}</pre>}<pre>结果：{typeof event.result === 'string' ? event.result : JSON.stringify(event.result, null, 2)}</pre></details>)}</div>
    {error && <div className="err">{error}</div>}
    <textarea rows={3} value={instruction} onChange={(e) => setInstruction(e.target.value)} placeholder="例如：为支付流程补充正常、边界和异常测试用例" disabled={busy} />
    <div className="row tight"><button className="primary" onClick={submit} disabled={busy || !providerID || !instruction.trim()}>{busy ? '生成中…' : '发送并生成'}</button>{busy && <button className="danger" onClick={() => abortRef.current?.abort()}>停止</button>}</div>
    {status === 'paused' && questions.length > 0 && <div className="card sub case-agent-questions"><strong>需要确认</strong>{questions.map((question) => <div className="stack" key={question.id}><span className="muted">{question.type}：{question.question}</span>{question.options?.length ? <div className="node-tags">{question.options.map((option) => <button className={answers[question.id] === option ? 'tag on' : 'tag'} key={option} onClick={() => setAnswers((current) => ({ ...current, [question.id]: option }))}>{option}</button>)}</div> : <input value={answers[question.id] ?? ''} onChange={(e) => setAnswers((current) => ({ ...current, [question.id]: e.target.value }))} placeholder="回答" />}</div>)}<button className="primary" onClick={resume} disabled={busy}>提交回答</button></div>}
  </div>
}
