import { useCallback, useEffect, useState } from 'react'
import { apiError } from '../../api/client'
import { deleteUnit, listUnits, type TestUnit } from '../../api/testset'
import { SkeletonList } from '../feedback/Skeleton'
import { EmptyState } from '../feedback/EmptyState'
import { ErrorNote } from '../feedback/ErrorNote'

interface Props {
  testSetID: number
}

function tryPretty(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

export default function UnitsBrowser({ testSetID }: Props) {
  const [units, setUnits] = useState<TestUnit[]>([])
  const [q, setQ] = useState('')
  const [tag, setTag] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const rows = await listUnits(testSetID, { tag: tag || undefined, q: q || undefined })
      setUnits(rows || [])
      setTags([...new Set(rows.map((u) => u.tag).filter(Boolean))])
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setLoading(false)
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

  const toggle = (id: number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
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
      {err && <ErrorNote>{err}</ErrorNote>}
      {loading ? (
        <SkeletonList count={3} lines={1} />
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th></th>
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
              <>
                <tr key={u.id} onClick={() => toggle(u.id)} style={{ cursor: 'pointer' }}>
                  <td className="muted">{expanded.has(u.id) ? '▾' : '▸'}</td>
                  <td className={`method ${u.method.toLowerCase()}`}>{u.method}</td>
                  <td className="mono">{u.path}</td>
                  <td className="mono">{u.slug}</td>
                  <td>{u.tag}</td>
                  <td>{u.name}</td>
                  <td>
                    <button className="link danger" onClick={(e) => { e.stopPropagation(); remove(u) }}>
                      删除
                    </button>
                  </td>
                </tr>
                {expanded.has(u.id) && (
                  <tr key={`${u.id}-detail`}>
                    <td colSpan={7}>
                      <div className="card sub mono small stack">
                        {u.name && <div className="strong">{u.name}</div>}
                        {u.params && u.params !== 'null' && (
                          <details open>
                            <summary>参数 (params)</summary>
                            <pre>{tryPretty(u.params)}</pre>
                          </details>
                        )}
                        {u.request_body && u.request_body !== 'null' && (
                          <details open>
                            <summary>请求体 (request_body)</summary>
                            <pre>{tryPretty(u.request_body)}</pre>
                          </details>
                        )}
                        {u.responses && u.responses !== 'null' && (
                          <details open>
                            <summary>响应 (responses)</summary>
                            <pre>{tryPretty(u.responses)}</pre>
                          </details>
                        )}
                        {u.security && u.security !== 'null' && (
                          <details open>
                            <summary>安全 (security)</summary>
                            <pre>{tryPretty(u.security)}</pre>
                          </details>
                        )}
                        {u.spec && (
                          <details>
                            <summary>原始 spec</summary>
                            <pre>{tryPretty(u.spec)}</pre>
                          </details>
                        )}
                      </div>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      )}
      {!loading && units.length === 0 && (
        <EmptyState compact title="暂无单元" hint="先导入 Swagger" />
      )}
    </div>
  )
}
