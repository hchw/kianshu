import { useCallback, useEffect, useRef, useState } from 'react'
import { apiError } from '../../api/client'
import { addMember, listMembers, removeMember, searchUsers, type MemberView, type UserBrief } from '../../api/testset'
import { ErrorNote } from '../feedback/ErrorNote'
import PopConfirm from '../dialog/PopConfirm'

interface Props {
  testSetID: number
  ownerID: number
}

export default function MembersPanel({ testSetID, ownerID }: Props) {
  const [owner, setOwner] = useState<MemberView | null>(null)
  const [members, setMembers] = useState<MemberView[]>([])
  const [role, setRole] = useState('edit')
  const [err, setErr] = useState('')
  const [query, setQuery] = useState('')
  const [suggestions, setSuggestions] = useState<UserBrief[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const [selectedUser, setSelectedUser] = useState<UserBrief | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    try {
      const data = await listMembers(testSetID)
      setOwner(data.owner)
      setMembers(data.members)
    } catch (e) {
      setErr(apiError(e))
    }
  }, [testSetID])

  useEffect(() => {
    load()
  }, [load])

  // 点击外部关闭下拉
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(e.target as Node) &&
        inputRef.current &&
        !inputRef.current.contains(e.target as Node)
      ) {
        setShowSuggestions(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const doSearch = useCallback(
    async (q: string) => {
      if (q.trim().length === 0) {
        setSuggestions([])
        setShowSuggestions(false)
        return
      }
      try {
        const users = await searchUsers(testSetID, q.trim())
        setSuggestions(users)
        setShowSuggestions(users.length > 0)
      } catch {
        setSuggestions([])
      }
    },
    [testSetID],
  )

  const add = async () => {
    setErr('')
    if (!selectedUser) return
    try {
      const mv = await addMember(testSetID, selectedUser.id, role)
      setMembers((cur) => {
        const idx = cur.findIndex((m) => m.user_id === mv.user_id)
        if (idx >= 0) {
          const next = [...cur]
          next[idx] = mv
          return next
        }
        return [...cur, mv]
      })
      setQuery('')
      setSelectedUser(null)
      setSuggestions([])
      setShowSuggestions(false)
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const remove = async (uid: number, _name: string) => {
      try {
      await removeMember(testSetID, uid)
      setMembers((cur) => cur.filter((m) => m.user_id !== uid))
    } catch (e) {
      setErr(apiError(e))
    }
  }

  return (
    <div className="card stack">
      <h3>成员权限</h3>
      <div className="row tight" style={{ position: 'relative' }}>
        <div style={{ flex: 1, position: 'relative' }}>
          <input
            ref={inputRef}
            placeholder="搜索用户名..."
            value={selectedUser ? selectedUser.username : query}
            onChange={(e) => {
              setSelectedUser(null)
              setQuery(e.target.value)
              doSearch(e.target.value)
            }}
            onFocus={() => {
              if (suggestions.length > 0) setShowSuggestions(true)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setShowSuggestions(false)
              }
            }}
          />
          {showSuggestions && suggestions.length > 0 && (
            <div
              ref={dropdownRef}
              className="dropdown"
              style={{
                position: 'absolute',
                top: '100%',
                left: 0,
                right: 0,
                background: 'var(--bg)',
                border: '1px solid var(--border)',
                borderRadius: '0 0 6px 6px',
                maxHeight: 200,
                overflowY: 'auto',
                zIndex: 100,
                boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
              }}
            >
              {suggestions.map((u) => (
                <div
                  key={u.id}
                  className="dropdown-item"
                  style={{
                    padding: '6px 10px',
                    cursor: 'pointer',
                    borderBottom: '1px solid var(--border)',
                  }}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    setSelectedUser(u)
                    setQuery('')
                    setShowSuggestions(false)
                  }}
                >
                  {u.username} <span className="muted">#{u.id}</span>
                </div>
              ))}
            </div>
          )}
        </div>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="edit">edit</option>
          <option value="read">read</option>
        </select>
        <button className="primary" onClick={add} disabled={!selectedUser}>
          添加
        </button>
      </div>
      {err && <ErrorNote>{err}</ErrorNote>}
      <table className="table">
        <thead>
          <tr>
            <th>用户</th>
            <th>角色</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {owner && (
            <tr>
              <td>
                {owner.username} <span className="muted">#{owner.user_id}</span>
              </td>
              <td>{owner.role}</td>
              <td></td>
            </tr>
          )}
          {members.map((m) => (
            <tr key={m.user_id}>
              <td>
                {m.username} <span className="muted">#{m.user_id}</span>
              </td>
              <td>{m.role}</td>
              <td>
                <PopConfirm danger message={`移除成员「${m.username}」?`} onConfirm={() => remove(m.user_id, m.username)}>
              <button className="link danger">移除</button>
            </PopConfirm>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted">owner 可通过搜索用户名添加 read/edit 成员</p>
    </div>
  )
}
