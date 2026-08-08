import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createTestSet, listTestSets, type TestSet } from '../api/testset'
import { apiError } from '../api/client'
import AppLayout from '../components/layout/AppLayout'
import { SkeletonList } from '../components/feedback/Skeleton'
import { EmptyState } from '../components/feedback/EmptyState'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'
import { FolderOpen } from 'lucide-react'

export default function TestSetList() {
  const nav = useNavigate()
  const toast = useToast()
  const [sets, setSets] = useState<TestSet[]>([])
  const [name, setName] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)

  const load = async () => {
    setLoading(true)
    try {
      setSets((await listTestSets()) || [])
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setLoading(false)
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
      toast.success('测试集已创建')
      nav(`/test-sets/${ts.id}`)
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  return (
    <AppLayout>
      <h1>测试集</h1>
      {err && <ErrorNote>{err}</ErrorNote>}
      <form className="row" onSubmit={create}>
        <input placeholder="新测试集名称" value={name} onChange={(e) => setName(e.target.value)} />
        <button type="submit" className="primary">
          创建
        </button>
      </form>
      <div className="list">
        {loading ? (
          <SkeletonList count={3} />
        ) : sets.length === 0 ? (
          <EmptyState icon={<FolderOpen size={28} strokeWidth={1.5} aria-hidden="true" />} title="还没有测试集" hint="创建第一个吧" />
        ) : (
          sets.map((s) => (
            <div key={s.id} className="card item" onClick={() => nav(`/test-sets/${s.id}`)}>
              <span className="strong">{s.name}</span>
              <span className="muted">
                {s.host || '未配置 Host'} · owner #{s.owner_id}
              </span>
            </div>
          ))
        )}
      </div>
    </AppLayout>
  )
}
