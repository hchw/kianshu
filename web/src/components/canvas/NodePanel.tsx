import { useEffect, useState } from 'react'
import type { FlowTree, FlowNode, IOKey } from '../../api/flow'
import { NODE_LABELS } from '../../lib/tree'
import { getUnit, type TestUnit } from '../../api/testset'
import PopConfirm from '../dialog/PopConfirm'

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
            <label>
              自定义请求头 headers (JSON: 头名 → 值)
              <textarea
                rows={3}
                value={config.headers ? JSON.stringify(config.headers) : '{}'}
                onChange={(e) => {
                  try {
                    setKV('headers', JSON.parse(e.target.value))
                  } catch {
                    /* keep last valid */
                  }
                }}
              />
            </label>
            <div className="muted small">
              自定义头会覆盖同名的自动认证头。值以 '=' 开头为 JSONata 表达式
              (对当前输入求值,如 {"\"=token\""}、{"\"=$cache.x\""});否则为字面量
              (字符串/数字/布尔)。适用于 LLM 等第三方接口的专有头(如 Content-Type、X-Api-Key)。
            </div>
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
          <>
            <label>
              expr (JSONata)
              <textarea rows={3} value={(config.expr as string) ?? ''} onChange={(e) => setKV('expr', e.target.value)} />
            </label>
            <div className="muted small">
              若上游是 API 节点,响应为信封 {'{'}"status_code":200,"body":...{'}'}。
              用 $.body.xxx 访问响应字段,如 $.body.token、$count($.body.items)。
            </div>
          </>
        )
      case 'assert':
        return <AssertEditor config={config} onChange={setKV} />
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
          <>
            <label>
              writes (JSON: key → value)
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
            <label>
              static (JSON, 固定字符串值)
              <textarea
                rows={3}
                value={config.static ? JSON.stringify(config.static) : '{}'}
                onChange={(e) => {
                  try {
                    setKV('static', JSON.parse(e.target.value))
                  } catch {
                    /* keep last valid */
                  }
                }}
              />
            </label>
            <div className="muted small">
              上游 API 节点输出信封 {'{'}"status_code":200,"body":...{'}'}。
              value 为字符串 → JSONata 表达式求值(如 "body.token")；
              非字符串 → 直接当字面量；固定字符串放 static,
              表达式用 $static.xxx 引用。
            </div>
          </>
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
          <PopConfirm danger message={`删除节点 ${nodeID} 及其全部子节点?`} onConfirm={() => onDelete(nodeID)}>
            <button className="link danger">删除节点</button>
          </PopConfirm>
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

// ---- AssertEditor 结构化断言编辑器 ----

interface AssertionItem {
  field: string
  op: string
  expected: string
}

const OP_LABELS: Record<string, string> = {
  eq: '等于 (==)',
  ne: '不等于 (!=)',
  contains: '包含',
  gt: '大于 (>)',
  lt: '小于 (<)',
}

const FIELD_SUGGESTIONS = ['status_code', 'body.']

function AssertEditor({
  config,
  onChange,
}: {
  config: Record<string, unknown>
  onChange: (field: string, value: unknown) => void
}) {
  const raw = Array.isArray(config.assertions) ? (config.assertions as AssertionItem[]) : []

  const setItems = (items: AssertionItem[]) => onChange('assertions', items)

  const update = (i: number, patch: Partial<AssertionItem>) => {
    const next = raw.map((item, idx) => (idx === i ? { ...item, ...patch } : item))
    setItems(next)
  }

  const add = () => setItems([...raw, { field: 'status_code', op: 'eq', expected: '200' }])
  const remove = (i: number) => setItems(raw.filter((_, idx) => idx !== i))

  return (
    <div className="assert-editor">
      <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
        <span className="muted small">断言列表</span>
        <button className="link small" onClick={add}>
          + 添加断言
        </button>
      </div>
      {raw.length === 0 && <p className="muted small">暂无断言,API 节点仅验证网络请求是否成功</p>}
      {raw.map((a, i) => (
        <div key={i} className="card sub" style={{ marginTop: 6 }}>
          <div className="row" style={{ gap: 4, alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              className="mono small"
              style={{ width: 120 }}
              placeholder="字段路径"
              list="assert-fields"
              value={a.field}
              onChange={(e) => update(i, { field: e.target.value })}
            />
            <select
              className="small"
              value={a.op}
              onChange={(e) => update(i, { op: e.target.value })}
            >
              {Object.entries(OP_LABELS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
            <input
              className="mono small"
              style={{ flex: 1, minWidth: 80 }}
              placeholder={'期望值：数字200 / 字符串"abc"或裸abc / true/false / null / [1,2]'}
              value={a.expected}
              onChange={(e) => update(i, { expected: e.target.value })}
            />
            <button className="link danger small" onClick={() => remove(i)}>
              ✕
            </button>
          </div>
        </div>
      ))}
      <datalist id="assert-fields">
        {FIELD_SUGGESTIONS.map((s) => (
          <option key={s} value={s} />
        ))}
      </datalist>
      <div className="muted small" style={{ marginTop: 6 }}>
        API 响应信封: {'{'}"status_code": 200, "body": ...{'}'}。字段路径用点分隔,如
        status_code、body.token、body.0.name。运算符: eq/ne/contains/gt/lt。
      </div>
    </div>
  )
}
