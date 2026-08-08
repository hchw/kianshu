import { Moon, Sun } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { currentUser, clearSession, restoreSession } from '../../store/session'
import { useTheme } from '../../lib/theme'

interface Props {
  title?: React.ReactNode
  actions?: React.ReactNode
  full?: boolean
  children: React.ReactNode
}

export default function AppLayout({ title, actions, full, children }: Props) {
  const nav = useNavigate()
  const user = restoreSession() ?? currentUser()
  const { theme, toggle } = useTheme()

  const logout = () => {
    clearSession()
    nav('/login')
  }

  return (
    <div className={full ? 'page full' : 'page'}>
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
        <button
          className="ghost icon-btn"
          onClick={toggle}
          aria-label={theme === 'dark' ? '切到浅色' : '切到深色'}
          title={theme === 'dark' ? '切到浅色' : '切到深色'}
        >
          {theme === 'dark' ? <Sun size={16} aria-hidden="true" /> : <Moon size={16} aria-hidden="true" />}
        </button>
        {actions}
        {user && <span className="muted">{user.username}</span>}
        <button className="link" onClick={logout}>
          退出
        </button>
      </header>
      <div className={full ? 'container full' : 'container'}>{children}</div>
    </div>
  )
}
