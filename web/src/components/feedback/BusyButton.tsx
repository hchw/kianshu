import type { ButtonHTMLAttributes, ReactNode } from 'react'

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  busy?: boolean
  children: ReactNode
}

/** 异步提交按钮：busy 时显示内置 spinner 并禁用，防止重复提交 */
export function BusyButton({ busy = false, disabled, children, ...rest }: Props) {
  return (
    <button {...rest} disabled={busy || disabled}>
      {busy && <span className="btn-spinner" aria-hidden="true" />}
      {children}
    </button>
  )
}
