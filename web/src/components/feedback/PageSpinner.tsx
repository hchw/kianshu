export function Spinner() {
  return <span className="spinner" aria-hidden="true" />
}

export function PageSpinner({ label = '加载中…' }: { label?: string }) {
  return (
    <div className="page-spinner" role="status">
      <Spinner />
      {label && <span className="muted">{label}</span>}
    </div>
  )
}
