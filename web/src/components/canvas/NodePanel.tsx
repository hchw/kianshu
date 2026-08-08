import { useEffect, useState } from 'react'
import type { FlowTree, FlowNode, IOKey } from '../../api/flow'
import { NODE_LABELS } from '../../lib/tree'
import { getUnit, type TestUnit } from '../../api/testset'

interface Props {
  tree: FlowTree
  nodeID: string
  onTreeChange: (t: FlowTree) => void
  onSaved: () => void
  onDelete: (id: string) => void
  onClose: () => void
  testSetID: number
}

export default function NodePanel({ tree, nodeID, onTreeChange, onSaved, onDelete, onClose, testSetID }: Props) {
  const node: FlowNode | undefined = tree.nodes[nodeID]
  const [config, setConfig] = useState<Record<string, unknown>>({})
  const [inputs, setInputs] = useState<Record<string, string>>({})
  const [outputs, setOutputs] = useState<Record<string, string>>({})
  const [unit, setUnit] = useState<TestUnit | null>(null)

  useEffect(() => {
    if (!node) return
    setConfig(typeof node.config === 'object' && node.config ? (node.config as Record<string, unknown>) : {})
    const ins: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.inputs ?? {})) ins[k] = v.desc ?? ''
    setInputs(ins)
    const outs: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.outputs ?? {})) outs[k] = v.desc ?? ''
    setOutputs(outs)

    const unitID = Number(node.config && typeof node.config === 'object' ? (node.config as Record<string, unknown>).unit_id ?? 0 : 0)
    if (node.type === 'api' && testSetID && unitID) {
      setUnit(null)
      getUnit(testSetID, unitID)
        .then(setUnit)
        .catch(() => setUnit(null))
    } else {
      setUnit(null)
    }
  }, [nodeID, node, testSetID])

  if (!node) return null

  const save = () => {
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    const n = next.nodes[nodeID]
    n.config = config
    // 仅当用户编辑过 I/O 契约时（非 API 只读模式）才写入 inputs/outputs
    if (node.type !== 'api' || !node.inputs || Object.keys(node.inputs).length === 0) {
      const ins: Record<string, { type: string; desc?: string; source?: string; from?: string }> = {}
      for (const [k, v] of Object.entries(inputs)) {
        const orig = (node.inputs ?? {})[k] ?? {}
        ins[k] = { ...orig, desc: v }
      }
      const outs: Record<string, { type: string; desc?: string; source?: string; from?: string }> = {}
      for (const [k, v] of Object.entries(outputs)) {
        const orig = (node.outputs ?? {})[k] ?? {}
        outs[k] = { ...orig, desc: v }
      }
      n.inputs = ins as Record<string, IOKey>
      n.outputs = outs as Record<string, IOKey>
    }
    onTreeChange(next)
    onSaved()
  }

  const setKV = (field: string, value: unknown) => setConfig((c) => ({ ...c, [field]: value }))

  // I/O contract section: for API nodes, show read-only auto-populated inputs;
  // for other types, keep the editable inputs/outputs.
  const renderIO = () => {
    if (node.type === 'api' && node.inputs && Object.keys(node.inputs).length > 0) {
      return (
        <div>
          <div className="muted">I/O 契约（自动从 Swagger 生成，只读）</div>
          <table className="table small mono">
            <thead>
              <tr>
                <th>键</th>
                <th>类型</th>
                <th>来源</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(node.inputs).map(([k, v]) => (
                <tr key={k}>
                  <td>{k}</td>
                  <td>{v.type ?? '-'}</td>
                  <td className="muted">{v.source || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    }
    // Existing editable I/O for non-API nodes or API nodes without inputs.
    const hasIO = Object.keys(inputs).length > 0 || Object.keys(outputs).length > 0
    if (!hasIO) return null
    return (
      <div>
        <div className="muted">I/O 契约(仅描述,连线以 parent 为准)</div>
        {Object.keys(inputs).map((k) => (
          <label key={k}>
            input.{k}
            <input value={inputs[k]} onChange={(e) => setInputs((s) => ({ ...s, [k]: e.target.value }))} />
          </label>
        ))}
        {Object.keys(outputs).map((k) => (
          <label key={k}>
            output.{k}
            <input value={outputs[k]} onChange={(e) => setOutputs((s) => ({ ...s, [k]: e.target.value }))} />
          </label>
        ))}
      </div>
    )
  }

  const renderConfig = () => {
    switch (node.type) {
      case 'api':
        return (
          <>
            <label>
              unit_id
              <input
                type="number"
                value={Number(config.unit_id ?? 0)}
                onChange={(e) => setKV('unit_id', Number(e.target.value))}
              />
            </label>
            <label>
              执行参数 params (JSON)
              <textarea
                rows={4}
                value={config.params ? JSON.stringify(config.params) : '{}'}
                onChange={(e) => {
                  try {
                    setKV('params', JSON.parse(e.target.value))
                  } catch {
                    /* keep last valid */
                  }
                }}
              />
            </label>
            {unit && (
              <div className="card sub mono small">
                <div className="strong">{unit.method} {unit.path}</div>
                <div className="muted">slug: {unit.slug}</div>
                {unit.name && <div className="muted">{unit.name}</div>}
                {unit.params && unit.params !== 'null' && (
                  <details>
                    <summary>Swagger 原始定义 · 参数 (params)</summary>
                    <pre>{tryPretty(unit.params)}</pre>
                  </details>
                )}
                {unit.request_body && unit.request_body !== 'null' && (
                  <details>
                    <summary>Swagger 原始定义 · 请求体 (request_body)</summary>
                    <pre>{tryPretty(unit.request_body)}</pre>
                  </details>
                )}
                {unit.responses && unit.responses !== 'null' && (
                  <details>
                    <summary>响应 (responses)</summary>
                    <pre>{tryPretty(unit.responses)}</pre>
                  </details>
                )}
                {unit.security && unit.security !== 'null' && (
                  <details>
                    <summary>安全 (security)</summary>
                    <pre>{tryPretty(unit.security)}</pre>
                  </details>
                )}
              </div>
            )}
          </>
        )
      case 'adapter':
        return (
          <label>
            expr (JSONata)
            <textarea rows={3} value={(config.expr as string) ?? ''} onChange={(e) => setKV('expr', e.target.value)} />
          </label>
        )
      case 'assert':
        return (
          <label>
            assertions (JSON)
            <textarea
              rows={6}
              value={config.assertions ? JSON.stringify(config.assertions) : '[]'}
              onChange={(e) => {
                try {
                  setKV('assertions', JSON.parse(e.target.value))
                } catch {
                  /* keep last valid */
                }
              }}
            />
          </label>
        )
      case 'loop':
        return (
          <>
            <label>
              input
              <input value={(config.input as string) ?? ''} onChange={(e) => setKV('input', e.target.value)} />
            </label>
            <label>
              var
              <input value={(config.var as string) ?? ''} onChange={(e) => setKV('var', e.target.value)} />
            </label>
          </>
        )
      case 'catch':
        return (
          <label>
            fallback (JSON)
            <input
              value={config.fallback !== undefined ? JSON.stringify(config.fallback) : ''}
              onChange={(e) => {
                try {
                  setKV('fallback', JSON.parse(e.target.value))
                } catch {
                  /* keep last valid */
                }
              }}
            />
          </label>
        )
      case 'cache-set':
        return (
          <label>
            writes (JSON: key → jsonata expr)
            <textarea
              rows={5}
              value={config.writes ? JSON.stringify(config.writes) : '{}'}
              onChange={(e) => {
                try {
                  setKV('writes', JSON.parse(e.target.value))
                } catch {
                  /* keep last valid */
                }
              }}
            />
          </label>
        )
      case 'start':
        return (
          <label>
            params (JSON)
            <textarea
              rows={4}
              value={config.params ? JSON.stringify(config.params) : '{}'}
              onChange={(e) => {
                try {
                  setKV('params', JSON.parse(e.target.value))
                } catch {
                  /* keep last valid */
                }
              }}
            />
          </label>
        )
      default:
        return <p className="muted">该节点类型无配置</p>
    }
  }

  return (
    <div className="node-panel">
      <div className="node-panel-head">
        <span className="strong">
          {NODE_LABELS[node.type] ?? node.type} · {nodeID}
        </span>
        <button className="link" onClick={onClose}>
          ✕
        </button>
      </div>
      <div className="stack">
        {renderIO()}
        <div className="sep" />
        <div>{renderConfig()}</div>
        <button className="primary" onClick={save}>保存</button>
        {node.type !== 'start' && (
          <button
            className="link danger"
            onClick={() => {
              if (confirm(`删除节点 ${nodeID} 及其全部子节点?`)) onDelete(nodeID)
            }}
          >
            删除节点
          </button>
        )}
      </div>
    </div>
  )
}

function tryPretty(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}
