import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, Gauge, Globe, Moon, Sun, User, Zap } from 'lucide-react'
import { currentUser, clearSession, restoreSession } from '../../store/session'
import { useTheme } from '../../lib/theme'

interface Props {
  title?: React.ReactNode
  actions?: React.ReactNode
  full?: boolean
  children: React.ReactNode
}

function useCloseOnClickOutside(ref: React.RefObject<HTMLDivElement | null>, onClose: () => void) {
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose()
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [ref, onClose])
}

export default function AppLayout({ title, actions, full, children }: Props) {
  const nav = useNavigate()
  const user = restoreSession() ?? currentUser()
  const { theme, toggle } = useTheme()

  const [navOpen, setNavOpen] = useState(false)
  const [userOpen, setUserOpen] = useState(false)

  const navRef = useRef<HTMLDivElement>(null)
  const userRef = useRef<HTMLDivElement>(null)

  useCloseOnClickOutside(navRef, () => setNavOpen(false))
  useCloseOnClickOutside(userRef, () => setUserOpen(false))

  const logout = () => {
    clearSession()
    nav('/login')
  }

  return (
    <div className={full ? 'page full' : 'page'}>
      <header className="topbar">
        {/* 品牌区：点击名称回首页 */}
        <span className="brand" style={{cursor:'pointer'}} onClick={() => nav('/dashboard')}>鉴枢</span>

        {/* 导航下拉 */}
        <div className={`dropdown${navOpen ? ' open' : ''}`} ref={navRef}>
          <button className="dropdown-trigger" onClick={() => { setNavOpen((v) => !v); setUserOpen(false) }}>
            <Globe size={14} />
            导航
            <ChevronDown size={14} className="chevron" />
          </button>
          <div className="dropdown-menu">
            <button className="dropdown-item" onClick={() => { setNavOpen(false); nav('/dashboard') }}>
              <Gauge size={14} className="item-icon" />
              首页
            </button>
            <button className="dropdown-item" onClick={() => { setNavOpen(false); nav('/test-sets') }}>
              <Zap size={14} className="item-icon" />
              测试集列表
            </button>
            <button className="dropdown-item" onClick={() => { setNavOpen(false); nav('/providers') }}>
              <Globe size={14} className="item-icon" />
              Provider 管理
            </button>
          </div>
        </div>

        {/* 当前页面标题 */}
        {title && <span className="strong">{title}</span>}

        <div className="spacer" />

        {/* 页面级操作按钮 */}
        {actions}

        {/* 用户下拉 */}
        {user && (
          <div className={`dropdown${userOpen ? ' open' : ''}`} ref={userRef}>
            <button className="dropdown-trigger" onClick={() => { setUserOpen((v) => !v); setNavOpen(false) }}>
              <User size={14} />
              <span className="muted">{user.username}</span>
              <ChevronDown size={14} className="chevron" />
            </button>
            <div className="dropdown-menu right">
              <button className="dropdown-item" onClick={() => { toggle(); setUserOpen(false) }}>
                {theme === 'dark' ? <Sun size={14} className="item-icon" /> : <Moon size={14} className="item-icon" />}
                {theme === 'dark' ? '切到浅色' : '切到深色'}
              </button>
              <div className="dropdown-divider" />
              <button className="dropdown-item" onClick={() => { setUserOpen(false); logout() }}>
                退出登录
              </button>
            </div>
          </div>
        )}
      </header>
      <div className={full ? 'container full' : 'container'}>{children}</div>
    </div>
  )
}
