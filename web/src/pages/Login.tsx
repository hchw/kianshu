import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { login } from '../api/auth'
import { apiError } from '../api/client'
import { setSession } from '../store/session'

export default function Login() {
  const nav = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const res = await login(username, password)
      setSession(res.token, { id: res.id, username: res.username })
      nav('/test-sets')
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-page">
      <form className="card auth-card" onSubmit={submit}>
        <h1>鉴枢</h1>
        <p className="muted">集成测试流编辑器</p>
        <input
          placeholder="用户名"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
        />
        <input
          placeholder="密码"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />
        {err && <p className="err">{err}</p>}
        <button disabled={busy} type="submit">
          {busy ? '登录中…' : '登录'}
        </button>
        <button type="button" className="link" onClick={() => nav('/register')}>
          没有账号?去注册
        </button>
      </form>
    </div>
  )
}
