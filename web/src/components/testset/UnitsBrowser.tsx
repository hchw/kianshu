import { useCallback, useEffect, useState } from 'react'
import { apiError } from '../../api/client'
import { deleteUnit, listUnits, type TestUnit } from '../../api/testset'

interface Props {
  testSetID: number
}

export default function UnitsBrowser({ testSetID }: Props) {
  const [units, setUnits] = useState<TestUnit[]>([])
  const [q, setQ] = useState('')
  const [tag, setTag] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    try {
      const rows = await listUnits(testSetID, { tag: tag || undefined, q: q || undefined })
      setUnits(rows || [])
      setTags([...new Set(rows.map((u) => u.tag).filter(Boolean))])
    } catch (e) {
      setErr(apiError(e))
    }
  }, [testSetID, q, tag])

  useEffect(() => {
    load()
  }, [load])

  const remove = async (u: TestUnit) => {
    if (!confirm(`删除单元 ${u.slug}?`)) return
    try {
      await deleteUnit(testSetID, u.id)
      load()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  return (
    <div className="card stack">
      <h3>测试单元</h3>
      <div className="row tight">
        <input placeholder="搜索名称/路径/slug" value={q} onChange={(e) => setQ(e.target.value)} />
        <select value={tag} onChange={(e) => setTag(e.target.value)}>
          <option value="">全部 tag</option>
          {tags.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>
      {err && <p className="err">{err}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>方法</th>
            <th>路径</th>
            <th>slug</th>
            <th>tag</th>
            <th>名称</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {units.map((u) => (
            <tr key={u.id}>
              <td className={`method ${u.method.toLowerCase()}`}>{u.method}</td>
              <td className="mono">{u.path}</td>
              <td className="mono">{u.slug}</td>
              <td>{u.tag}</td>
              <td>{u.name}</td>
              <td>
                <button className="link danger" onClick={() => remove(u)}>
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {units.length === 0 && <p className="muted">暂无单元,先导入 Swagger</p>}
    </div>
  )
}
