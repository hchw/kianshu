import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createTestSet, listTestSets, type TestSet } from '../api/testset'
import { apiError } from '../api/client'
import AppLayout from '../components/layout/AppLayout'

export default function TestSetList() {
  const nav = useNavigate()
  const [sets, setSets] = useState<TestSet[]>([])
  const [name, setName] = useState('')
  const [err, setErr] = useState('')

  const load = async () => {
    try {
      setSets((await listTestSets()) || [])
    } catch (e) {
      setErr(apiError(e))
    }
  }

  useEffect(() => {
    load()
  }, [])

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    if (!name.trim()) return
    try {
      const ts = await createTestSet(name.trim())
      nav(`/test-sets/${ts.id}`)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  return (
    <AppLayout>
      <h1>测试集</h1>
      {err && <p className="err">{err}</p>}
      <form className="row" onSubmit={create}>
        <input placeholder="新测试集名称" value={name} onChange={(e) => setName(e.target.value)} />
        <button type="submit">创建</button>
      </form>
      <div className="list">
        {(sets || []).map((s) => (
          <div key={s.id} className="card item" onClick={() => nav(`/test-sets/${s.id}`)}>
            <span className="strong">{s.name}</span>
            <span className="muted">
              {s.host || '未配置 Host'} · owner #{s.owner_id}
            </span>
          </div>
        ))}
        {sets.length === 0 && <p className="muted">还没有测试集,创建第一个吧</p>}
      </div>
    </AppLayout>
  )
}
