import { useEffect, useState } from 'react'
import type { FlowTree, FlowNode, IOKey } from '../../api/flow'
import { NODE_LABELS } from '../../lib/tree'

interface Props {
  tree: FlowTree
  nodeID: string
  onTreeChange: (t: FlowTree) => void
  onSaved: () => void
  onClose: () => void
}

export default function NodePanel({ tree, nodeID, onTreeChange, onSaved, onClose }: Props) {
  const node: FlowNode | undefined = tree.nodes[nodeID]
  const [config, setConfig] = useState<Record<string, unknown>>({})
  const [inputs, setInputs] = useState<Record<string, string>>({})
  const [outputs, setOutputs] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!node) return
    setConfig(typeof node.config === 'object' && node.config ? (node.config as Record<string, unknown>) : {})
    const ins: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.inputs ?? {})) ins[k] = v.desc ?? ''
    setInputs(ins)
    const outs: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.outputs ?? {})) outs[k] = v.desc ?? ''
    setOutputs(outs)
  }, [nodeID, node])

  if (!node) return null

  const save = () => {
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    const n = next.nodes[nodeID]
    n.config = config
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
    onTreeChange(next)
    onSaved()
  }

  const setKV = (field: string, value: unknown) => setConfig((c) => ({ ...c, [field]: value }))

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
        <div className="sep" />
        <div>{renderConfig()}</div>
        <button onClick={save}>保存</button>
      </div>
    </div>
  )
}
