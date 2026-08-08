import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { register } from '../api/auth'
import { apiError } from '../api/client'
import { BusyButton } from '../components/feedback/BusyButton'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'

export default function Register() {
  const nav = useNavigate()
  const toast = useToast()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    if (password !== confirm) {
      const msg = '两次密码不一致'
      setErr(msg)
      return
    }
    setBusy(true)
    try {
      await register(username, password)
      toast.success('注册成功,请登录')
      nav('/login')
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
      <form className="card auth-card" onSubmit={submit}>
        <h1>注册</h1>
        <input
          placeholder="用户名(至少 3 字符)"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
        />
        <input
          placeholder="密码(至少 6 字符)"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
        />
        <input
          placeholder="确认密码"
          type="password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="new-password"
        />
        {err && <ErrorNote>{err}</ErrorNote>}
        <BusyButton type="submit" className="primary" busy={busy}>
          {busy ? '注册中…' : '注册'}
        </BusyButton>
        <button type="button" className="link" onClick={() => nav('/login')}>
          已有账号?去登录
        </button>
      </form>
    </div>
  )
}
