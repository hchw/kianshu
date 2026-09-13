import { Link } from 'react-router-dom'
import type { FlowCaseSources } from '../../api/caseFlow'

/** 执行流左侧栏的「关联用例」板块：来源用例流版本 + 已绑定用例清单。 */
export default function CaseLinkList({ sources }: { sources: FlowCaseSources | null }) {
  const binding = sources?.binding
  if (!sources?.bound || !binding) {
    return (
      <div className="case-link-list">
        <div className="case-link-title">关联用例</div>
        <div className="muted case-link-empty">未关联用例流</div>
      </div>
    )
  }
  const srcs = binding.sources ?? []
  const leaves = binding.leaves ?? []
  return (
    <div className="case-link-list">
      <div className="case-link-title">关联用例 · {leaves.length}</div>
      {srcs.map((s) => {
        const mine = leaves.filter((l) => l.case_flow_id === s.case_flow_id)
        return (
          <div key={s.case_flow_id} className="case-link-group">
            <Link className="case-link-source" to={`/case-flows/${s.case_flow_id}`} title={s.name}>
              {s.name} <span className="muted">v{s.version_no}</span>
            </Link>
            {mine.map((l) => (
              <div key={l.case_node_id} className="case-link-leaf" title={l.path || l.title}>
                {l.title}
              </div>
            ))}
          </div>
        )
      })}
    </div>
  )
}
