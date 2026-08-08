export function SkeletonCard({ lines = 2 }: { lines?: number }) {
  return (
    <div className="skeleton-card">
      <div className="skeleton-block skeleton-title" />
      {Array.from({ length: lines }, (_, i) => (
        <div key={i} className="skeleton-block" />
      ))}
    </div>
  )
}

export function SkeletonList({ count = 3, lines = 2 }: { count?: number; lines?: number }) {
  return (
    <div className="skeleton-list">
      {Array.from({ length: count }, (_, i) => (
        <SkeletonCard key={i} lines={lines} />
      ))}
    </div>
  )
}
