import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createTestSet, deleteTestSet, listTestSets, updateTestSet, type TestSet } from '../api/testset'
import { apiError } from '../api/client'
import AppLayout from '../components/layout/AppLayout'
import { SkeletonList } from '../components/feedback/Skeleton'
import { EmptyState } from '../components/feedback/EmptyState'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'
import { FolderOpen, Pencil } from 'lucide-react'
import PopConfirm from '../components/dialog/PopConfirm'

export default function TestSetList() {
  const nav = useNavigate()
  const toast = useToast()
  const [sets, setSets] = useState<TestSet[]>([])
  const [name, setName] = useState('')
  const [editingID, setEditingID] = useState<number | null>(null)
  const [editingName, setEditingName] = useState('')
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

  const remove = async (set: TestSet) => {
    try {
      await deleteTestSet(set.id)
      setSets((currentSets) => currentSets.filter((item) => item.id !== set.id))
      toast.success('测试集已删除')
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const startRename = (set: TestSet) => {
    setEditingID(set.id)
    setEditingName(set.name)
  }

  const commitRename = async () => {
    const id = editingID
    const nextName = editingName.trim()
    setEditingID(null)
    if (!id || !nextName) return
    const current = sets.find((set) => set.id === id)
    if (current?.name === nextName) return
    try {
      await updateTestSet(id, { name: nextName })
      setSets((currentSets) => currentSets.map((set) => set.id === id ? { ...set, name: nextName } : set))
      toast.success('测试集已重命名')
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
            <div
              key={s.id}
              className="card item"
              onClick={(e) => {
                if ((e.target as HTMLElement).closest('button, input, .popconfirm-anchor')) return
                nav(`/test-sets/${s.id}`)
              }}
            >
              <div className="flow-item-name">
                {editingID === s.id ? (
                  <input
                    autoFocus
                    value={editingName}
                    onChange={(e) => setEditingName(e.target.value)}
                    onClick={(e) => e.stopPropagation()}
                    onKeyDown={(e) => {
                      e.stopPropagation()
                      if (e.key === 'Enter') void commitRename()
                      else if (e.key === 'Escape') setEditingID(null)
                    }}
                    onBlur={() => void commitRename()}
                  />
                ) : <span className="strong">{s.name}</span>}
                {editingID !== s.id && <button className="link" title="重命名" onClick={(e) => { e.stopPropagation(); startRename(s) }}><Pencil size={14} aria-hidden="true" /></button>}
              </div>
              <span className="muted">{s.host || '未配置 Host'} · owner #{s.owner_id}</span>
              <PopConfirm
                danger
                title="删除测试集"
                message={`确认删除测试集「${s.name}」? 仅当测试集没有测试流时才能删除。`}
                confirmText="删除"
                onConfirm={() => void remove(s)}
              >
                <button className="link danger" onClick={(e) => e.stopPropagation()}>删除</button>
              </PopConfirm>
            </div>
          ))
        )}
      </div>
    </AppLayout>
  )
}
