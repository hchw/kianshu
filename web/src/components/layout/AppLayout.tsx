import { useNavigate } from 'react-router-dom'
import { currentUser, clearSession, restoreSession } from '../../store/session'

interface Props {
  title?: React.ReactNode
  actions?: React.ReactNode
  children: React.ReactNode
}

export default function AppLayout({ title, actions, children }: Props) {
  const nav = useNavigate()
  const user = restoreSession() ?? currentUser()

  const logout = () => {
    clearSession()
    nav('/login')
  }

  return (
    <div className="page">
      <header className="topbar">
        <span className="brand">鉴枢</span>
        <button className="link" onClick={() => nav('/test-sets')}>
          测试集
        </button>
        <button className="link" onClick={() => nav('/providers')}>
          Provider
        </button>
        {title && <span className="strong">{title}</span>}
        <div className="spacer" />
        {actions}
        {user && <span className="muted">{user.username}</span>}
        <button className="link" onClick={logout}>
          退出
        </button>
      </header>
      <div className="container">{children}</div>
    </div>
  )
}
