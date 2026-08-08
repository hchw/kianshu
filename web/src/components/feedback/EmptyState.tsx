import type { ReactNode } from 'react'
import { Inbox } from 'lucide-react'

interface Props {
  icon?: ReactNode
  title: string
  hint?: string
  action?: ReactNode
  compact?: boolean
}

export function EmptyState({ icon, title, hint, action, compact }: Props) {
  return (
    <div className={`empty-state${compact ? ' compact' : ''}`}>
      {icon ?? <Inbox size={28} strokeWidth={1.5} aria-hidden="true" />}
      <div className="strong">{title}</div>
      {hint && <div className="muted">{hint}</div>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  )
}
