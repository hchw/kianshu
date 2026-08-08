import { AlertCircle } from 'lucide-react'

/** 行内错误提示：danger 语义 + 图标，不依赖颜色单独传达信息 */
export function ErrorNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="err">
      <AlertCircle size={14} aria-hidden="true" />
      <span>{children}</span>
    </p>
  )
}
