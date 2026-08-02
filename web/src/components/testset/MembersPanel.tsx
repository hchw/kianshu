import { useState } from 'react'
import { apiError } from '../../api/client'
import { addMember, removeMember } from '../../api/testset'

interface Props {
  testSetID: number
  ownerID: number
}

interface MemberRow {
  user_id: number
  role: string
}

export default function MembersPanel({ testSetID, ownerID }: Props) {
  const [members, setMembers] = useState<MemberRow[]>([])
  const [userID, setUserID] = useState('')
  const [role, setRole] = useState('edit')
  const [err, setErr] = useState('')

  const add = async () => {
    setErr('')
    if (!userID.trim()) return
    try {
      await addMember(testSetID, Number(userID), role)
      setUserID('')
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const remove = async (uid: number) => {
    if (!confirm(`移除成员 #${uid}?`)) return
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
      <div className="row tight">
        <input placeholder="用户 ID" value={userID} onChange={(e) => setUserID(e.target.value)} />
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="edit">edit</option>
          <option value="read">read</option>
        </select>
        <button onClick={add}>添加</button>
      </div>
      {err && <p className="err">{err}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>用户 ID</th>
            <th>角色</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>#{ownerID}</td>
            <td>owner</td>
            <td></td>
          </tr>
          {members.map((m) => (
            <tr key={m.user_id}>
              <td>#{m.user_id}</td>
              <td>{m.role}</td>
              <td>
                <button className="link danger" onClick={() => remove(m.user_id)}>
                  移除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="muted">owner 可通过用户 ID 添加 read/edit 成员</p>
    </div>
  )
}
