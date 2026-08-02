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
import ImportPanel from '../components/testset/ImportPanel'
import UnitsBrowser from '../components/testset/UnitsBrowser'
import MembersPanel from '../components/testset/MembersPanel'
import AppLayout from '../components/layout/AppLayout'

type Tab = 'overview' | 'import' | 'units' | 'members'

export default function TestSetDetail() {
  const { id } = useParams()
  const nav = useNavigate()
  const testSetID = Number(id)
  const [set, setSet] = useState<TestSet | null>(null)
  const [flows, setFlows] = useState<FlowSummary[]>([])
  const [host, setHost] = useState('')
  const [flowName, setFlowName] = useState('')
  const [tab, setTab] = useState<Tab>('overview')
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    try {
      const ts = await getTestSet(testSetID)
      setSet(ts)
      setHost(ts.host)
      setFlows((await listFlows(testSetID)) || [])
    } catch (e) {
      setErr(apiError(e))
    }
  }, [testSetID])

  useEffect(() => {
    load()
  }, [load])

  const saveHost = async () => {
    try {
      await updateTestSet(testSetID, host)
      await load()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!flowName.trim()) return
    try {
      const f = await createFlow(testSetID, flowName.trim())
      nav(`/flows/${f.id}`)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  if (!set) {
    return <div className="page container">{err || '加载中…'}</div>
  }

  return (
    <AppLayout title={set.name}>
      {err && <p className="err">{err}</p>}
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
                <button onClick={saveHost}>保存</button>
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
                <button type="submit">创建</button>
              </form>
              <div className="list">
                {flows.map((f) => (
                  <div key={f.id} className="card item" onClick={() => nav(`/flows/${f.id}`)}>
                    <span className="strong">{f.name}</span>
                    <span className="muted">打开编辑器 →</span>
                  </div>
                ))}
                {flows.length === 0 && <p className="muted">还没有测试流</p>}
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
