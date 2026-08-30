import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertTriangle, ArrowRight, CheckCircle2, FolderOpen, Gauge, RefreshCw, Settings2, Bot, FlaskConical } from 'lucide-react'
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
      <div className="dashboard-stats">
        <button className="card dashboard-stat" onClick={() => nav('/test-sets')}><FolderOpen size={20} /><div><strong>测试资产</strong><span className="muted">测试集 {data.summary.test_sets} · 测试流 {data.summary.flows}</span></div></button>
        <div className="card dashboard-stat dashboard-stat-disabled"><FlaskConical size={20} /><div><strong>接口用例</strong><span className="muted">用例数：即将上线</span><span className="muted">接口数：{data.summary.units}</span></div></div>
        <button className="card dashboard-stat" onClick={() => nav('/providers')}><Bot size={20} /><div><strong>Provider / 模型</strong><span className="muted">Provider {data.summary.providers} · 模型 {data.summary.models}</span></div></button>
        <button className="card dashboard-stat" onClick={() => nav('/runs')}><Gauge size={20} /><div><strong>执行情况</strong><span className="muted">近 7 天：{data.summary.recent_runs} 次</span><span className="muted">成功 {data.summary.successful_runs} · 失败 {data.summary.failed_runs}</span></div></button>
      </div>
      <div className="dashboard-columns"><section className="card dashboard-section"><h2>当前建议</h2>{data.onboarding.filter((s) => !s.done).slice(0, 1).map((s) => <button className="dashboard-action" key={s.key} onClick={() => nav(s.target)}><div><strong>{s.title}</strong><p className="muted">{s.description}</p></div><ArrowRight size={17} /></button>)}{data.onboarding.every((s) => s.done) && <p className="ok"><CheckCircle2 size={15} /> 基础配置已完成，继续编辑最近的测试流吧。</p>}</section><section className="card dashboard-section"><h2><AlertTriangle size={17} /> 需要关注</h2>{data.attention_items.length === 0 ? <p className="muted">目前没有需要处理的问题。</p> : data.attention_items.slice(0, 4).map((a) => <button className="dashboard-attention" key={`${a.kind}-${a.target}`} onClick={() => nav(a.target)}><div><strong>{a.title}</strong><p className="muted">{a.description}</p></div><ArrowRight size={15} /></button>)}</section></div>
      <section className="dashboard-section"><div className="dashboard-section-title"><h2>最近工作</h2><button className="link" onClick={() => nav('/test-sets')}>查看全部</button></div>{data.recent_work.length === 0 ? <div className="card empty-dashboard"><Settings2 size={24} /><p>还没有最近工作，创建一个测试集开始吧。</p></div> : <div className="list">{data.recent_work.map((w) => <button className="card dashboard-work" key={`${w.kind}-${w.id}`} onClick={() => nav(w.target)}><div><strong>{w.name}</strong><span className="muted">{w.test_set_name} · {w.role === 'read' ? '只读' : w.role === 'owner' ? '所有者' : '可编辑'}</span></div><span className="muted">{timeText(w.updated_at)}</span></button>)}</div>}</section>
    </>}
  </AppLayout>
}
