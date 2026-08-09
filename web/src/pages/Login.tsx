import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { login } from '../api/auth'
import { apiError } from '../api/client'
import { setSession } from '../store/session'
import { BusyButton } from '../components/feedback/BusyButton'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'

export default function Login() {
  const nav = useNavigate()
  const toast = useToast()
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
      toast.success('登录成功')
      nav('/test-sets')
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-page">
      <div className="auth-brand">
        <img src="/kianshu.png" alt="鉴枢" />
        <h2>集成测试流编辑器</h2>
      </div>
      <div className="auth-form">
      <form className="card auth-card" onSubmit={submit}>
        <h1>鉴枢</h1>
        <p className="muted">登录你的账号</p>
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
        {err && <ErrorNote>{err}</ErrorNote>}
        <BusyButton type="submit" className="primary" busy={busy}>
          {busy ? '登录中…' : '登录'}
        </BusyButton>
        <button type="button" className="link" onClick={() => nav('/register')}>
          没有账号?去注册
        </button>
      </form>
      </div>
    </div>
  )
}
