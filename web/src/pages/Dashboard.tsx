import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertTriangle, ArrowRight, CheckCircle2, FolderOpen, Gauge, Layers3, RefreshCw, Settings2, Workflow } from 'lucide-react'
import AppLayout from '../components/layout/AppLayout'
import { getDashboard, type DashboardData } from '../api/dashboard'
import { apiError } from '../api/client'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { SkeletonList } from '../components/feedback/Skeleton'
import { currentUser } from '../store/session'

const timeText = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))

export default function Dashboard() {
  const nav = useNavigate(); const user = currentUser(); const [data, setData] = useState<DashboardData>(); const [err, setErr] = useState(''); const [loading, setLoading] = useState(true)
  const load = async () => { setLoading(true); setErr(''); try { setData(await getDashboard()) } catch (e) { setErr(apiError(e)) } finally { setLoading(false) } }
  useEffect(() => { load() }, [])
  return <AppLayout title="首页" actions={<button className="primary" onClick={() => nav('/test-sets')}>创建测试集</button>}>
    <div className="dashboard-head"><div><h1>早上好，{user?.username ?? '用户'}</h1><p className="muted">这里是你的接口集成测试工作台。</p></div></div>
    {err && <><ErrorNote>{err}</ErrorNote><button onClick={load}><RefreshCw size={14} /> 重试</button></>}
    {loading ? <SkeletonList count={4} /> : data && <>
      <div className="dashboard-stats">{[[FolderOpen, '可访问测试集', data.summary.test_sets], [Workflow, '测试流', data.summary.flows], [Layers3, '接口单元', data.summary.units], [Gauge, '近 7 天执行', data.summary.recent_runs]].map(([Icon, label, value]) => { const I = Icon as typeof FolderOpen; return <div className="card dashboard-stat" key={String(label)}><I size={20} /><div><strong>{String(value)}</strong><span className="muted">{String(label)}</span></div></div> })}</div>
      <div className="dashboard-columns"><section className="card dashboard-section"><h2>当前建议</h2>{data.onboarding.filter((s) => !s.done).slice(0, 1).map((s) => <button className="dashboard-action" key={s.key} onClick={() => nav(s.target)}><div><strong>{s.title}</strong><p className="muted">{s.description}</p></div><ArrowRight size={17} /></button>)}{data.onboarding.every((s) => s.done) && <p className="ok"><CheckCircle2 size={15} /> 基础配置已完成，继续编辑最近的测试流吧。</p>}</section><section className="card dashboard-section"><h2><AlertTriangle size={17} /> 需要关注</h2>{data.attention_items.length === 0 ? <p className="muted">目前没有需要处理的问题。</p> : data.attention_items.slice(0, 4).map((a) => <button className="dashboard-attention" key={`${a.kind}-${a.target}`} onClick={() => nav(a.target)}><div><strong>{a.title}</strong><p className="muted">{a.description}</p></div><ArrowRight size={15} /></button>)}</section></div>
      <section className="dashboard-section"><div className="dashboard-section-title"><h2>最近工作</h2><button className="link" onClick={() => nav('/test-sets')}>查看全部</button></div>{data.recent_work.length === 0 ? <div className="card empty-dashboard"><Settings2 size={24} /><p>还没有最近工作，创建一个测试集开始吧。</p></div> : <div className="list">{data.recent_work.map((w) => <button className="card dashboard-work" key={`${w.kind}-${w.id}`} onClick={() => nav(w.target)}><div><strong>{w.name}</strong><span className="muted">{w.test_set_name} · {w.role === 'read' ? '只读' : w.role === 'owner' ? '所有者' : '可编辑'}</span></div><span className="muted">{timeText(w.updated_at)}</span></button>)}</div>}</section>
    </>}
  </AppLayout>
}
