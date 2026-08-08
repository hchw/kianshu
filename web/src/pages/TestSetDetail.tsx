import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { apiError } from '../api/client'
import {
  createFlow,
  getTestSet,
  listFlows,
  updateTestSet,
  type FlowSummary,
  type TestSet,
} from '../api/testset'
import { deleteFlow } from '../api/flow'
import ImportPanel from '../components/testset/ImportPanel'
import UnitsBrowser from '../components/testset/UnitsBrowser'
import MembersPanel from '../components/testset/MembersPanel'
import AppLayout from '../components/layout/AppLayout'
import { PageSpinner } from '../components/feedback/PageSpinner'
import { SkeletonList } from '../components/feedback/Skeleton'
import { EmptyState } from '../components/feedback/EmptyState'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'
import { GitBranch } from 'lucide-react'

type Tab = 'overview' | 'import' | 'units' | 'members'

export default function TestSetDetail() {
  const { id } = useParams()
  const nav = useNavigate()
  const toast = useToast()
  const testSetID = Number(id)
  const [set, setSet] = useState<TestSet | null>(null)
  const [flows, setFlows] = useState<FlowSummary[]>([])
  const [host, setHost] = useState('')
  const [flowName, setFlowName] = useState('')
  const [tab, setTab] = useState<Tab>('overview')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const ts = await getTestSet(testSetID)
      setSet(ts)
      setHost(ts.host)
      setFlows((await listFlows(testSetID)) || [])
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setLoading(false)
    }
  }, [testSetID])

  useEffect(() => {
    load()
  }, [load])

  const saveHost = async () => {
    try {
      await updateTestSet(testSetID, host)
      toast.success('Host 已保存')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!flowName.trim()) return
    try {
      const f = await createFlow(testSetID, flowName.trim())
      toast.success('测试流已创建')
      nav(`/flows/${f.id}`)
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const removeFlow = async (f: FlowSummary) => {
    if (!confirm(`删除测试流「${f.name}」? 将一并删除其版本、运行记录与定时调度,不可恢复。`)) return
    try {
      await deleteFlow(f.id)
      toast.success('测试流已删除')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  if (!set) {
    return (
      <div className="page container">
        {err ? <ErrorNote>{err}</ErrorNote> : <PageSpinner />}
      </div>
    )
  }

  return (
    <AppLayout title={set.name}>
      {err && <ErrorNote>{err}</ErrorNote>}
      <nav className="tabs">
        <button className={tab === 'overview' ? 'tab on' : 'tab'} onClick={() => setTab('overview')}>
          概览
        </button>
        <button className={tab === 'import' ? 'tab on' : 'tab'} onClick={() => setTab('import')}>
          导入
        </button>
        <button className={tab === 'units' ? 'tab on' : 'tab'} onClick={() => setTab('units')}>
          测试单元
        </button>
        <button className={tab === 'members' ? 'tab on' : 'tab'} onClick={() => setTab('members')}>
          成员
        </button>
      </nav>

      {tab === 'overview' && (
        <div className="stack">
          <div className="card">
            <h3>基础信息</h3>
            <div className="row">
              <input placeholder="Host 地址" value={host} onChange={(e) => setHost(e.target.value)} />
              <button onClick={saveHost} className="primary">
                保存
              </button>
            </div>
          </div>
          <div className="card">
            <h3>测试流</h3>
            <form className="row" onSubmit={create}>
              <input
                placeholder="新测试流名称"
                value={flowName}
                onChange={(e) => setFlowName(e.target.value)}
              />
              <button type="submit" className="primary">
                创建
              </button>
            </form>
            <div className="list">
              {loading ? (
                <SkeletonList count={2} />
              ) : flows.length === 0 ? (
                <EmptyState
                  compact
                  icon={<GitBranch size={24} strokeWidth={1.5} aria-hidden="true" />}
                  title="还没有测试流"
                  hint="输入名称创建第一个测试流"
                />
              ) : (
                flows.map((f) => (
                  <div key={f.id} className="card item" onClick={() => nav(`/flows/${f.id}`)}>
                    <span className="strong">{f.name}</span>
                    <span className="muted">打开编辑器 →</span>
                    <button
                      className="link danger"
                      onClick={(e) => {
                        e.stopPropagation()
                        removeFlow(f)
                      }}
                    >
                      删除
                    </button>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}

      {tab === 'import' && <ImportPanel testSetID={testSetID} onImported={load} />}
      {tab === 'units' && <UnitsBrowser testSetID={testSetID} />}
      {tab === 'members' && <MembersPanel testSetID={testSetID} ownerID={set.owner_id} />}
    </AppLayout>
  )
}
