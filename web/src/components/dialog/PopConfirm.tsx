import {
  cloneElement,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { AlertTriangle } from 'lucide-react'

interface Props {
  /** 询问内容（可含换行/加粗） */
  message: ReactNode
  /** 可选标题 */
  title?: ReactNode
  /** 可选输入框：提供时弹窗内渲染输入框，确认时把输入值传给 onConfirm */
  input?: { defaultValue?: string; placeholder?: string }
  /** 确认按钮文案，默认「确定」 */
  confirmText?: string
  /** 取消按钮文案，默认「取消」 */
  cancelText?: string
  /** 是否为危险操作（红色确认按钮 + 警示图标） */
  danger?: boolean
  /** 弹出方向，默认在按钮下方 */
  placement?: 'bottom' | 'top'
  /** 确认回调；有 input 时携带输入值 */
  onConfirm: (value?: string) => void
  /** 触发元素（通常是 button） */
  children: ReactElement<any>
}

/**
 * 同主题的小确认弹框：点击按钮后在其附近弹出，替代原生 confirm() 的霸屏白框。
 * 通过 portal 渲染到 body，按触发元素位置自动定位并对视口做边缘修正。
 * 提供 input prop 时变为带输入框的弹窗（复制/改名等场景），Enter 提交。
 */
export default function PopConfirm({
  message,
  title,
  input,
  confirmText = '确定',
  cancelText = '取消',
  danger = false,
  placement = 'bottom',
  onConfirm,
  children,
}: Props) {
  const [open, setOpen] = useState(false)
  const [value, setValue] = useState(input?.defaultValue ?? '')
  const [pos, setPos] = useState({ left: 0, top: 0 })
  const anchorRef = useRef<HTMLSpanElement>(null)
  const popRef = useRef<HTMLDivElement>(null)

  const updatePosRef = useRef<() => void>(() => {})
  updatePosRef.current = () => {
    const a = anchorRef.current?.getBoundingClientRect()
    if (!a) return
    const w = popRef.current?.offsetWidth ?? 260
    const h = popRef.current?.offsetHeight ?? 130
    let left = a.left + a.width / 2 - w / 2
    left = Math.max(8, Math.min(left, window.innerWidth - w - 8))
    let top = placement === 'top' ? a.top - h - 8 : a.bottom + 8
    if (top + h > window.innerHeight - 8) top = window.innerHeight - h - 8
    if (top < 8) top = 8
    setPos({ left, top })
  }

  useLayoutEffect(() => {
    if (open) updatePosRef.current()
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (popRef.current?.contains(t) || anchorRef.current?.contains(t)) return
      setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    const onScroll = () => updatePosRef.current()
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', onScroll)
    window.addEventListener('scroll', onScroll, true)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onScroll)
      window.removeEventListener('scroll', onScroll, true)
    }
  }, [open])

  const trigger = cloneElement(children, {
    onClick: (e: React.MouseEvent) => {
      e.stopPropagation()
      ;(children.props as { onClick?: (e: React.MouseEvent) => void }).onClick?.(e)
      setOpen((o) => !o)
      if (input) setValue(input.defaultValue ?? '')
    },
  })

  const inputRef = useRef<HTMLInputElement>(null)

  useLayoutEffect(() => {
    if (open && input) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [open, input])

  const confirm = () => {
    setOpen(false)
    onConfirm(input ? value : undefined)
  }

  return (
    <>
      <span ref={anchorRef} className="popconfirm-anchor">
        {trigger}
      </span>
      {open &&
        createPortal(
          <div
            ref={popRef}
            className={`popconfirm${danger ? ' danger' : ''}`}
            style={{ left: pos.left, top: pos.top }}
            role="dialog"
            aria-modal="false"
          >
            <div className="popconfirm-body">
              {danger && (
                <span className="popconfirm-icon" aria-hidden="true">
                  <AlertTriangle size={16} />
                </span>
              )}
              <div className="popconfirm-text">
                {title && <div className="popconfirm-title">{title}</div>}
                <div className="popconfirm-msg">{message}</div>
              </div>
            </div>
            {input && (
              <input
                ref={inputRef}
                className="popconfirm-input"
                value={value}
                placeholder={input.placeholder}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.stopPropagation()
                    confirm()
                  }
                }}
              />
            )}
            <div className="popconfirm-actions">
              <button className="ghost" onClick={() => setOpen(false)}>
                {cancelText}
              </button>
              <button className={danger ? 'danger' : 'primary'} onClick={confirm}>
                {confirmText}
              </button>
            </div>
          </div>,
          document.body,
        )}
    </>
  )
}
