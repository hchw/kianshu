import { useEffect, useState } from 'react'
import type { FlowTree, FlowNode, IOKey } from '../../api/flow'
import { NODE_LABELS } from '../../lib/tree'
import { getUnit, listUnits, type TestUnit } from '../../api/testset'
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
  const [inputsOverride, setInputsOverride] = useState<Record<string, IOKey> | null>(null)
  const [removedInputs, setRemovedInputs] = useState<Set<string>>(new Set())
  const [removedOutputs, setRemovedOutputs] = useState<Set<string>>(new Set())
  const [sourceOverrides, setSourceOverrides] = useState<Record<string, string>>({})
  const [unit, setUnit] = useState<TestUnit | null>(null)
  const [units, setUnits] = useState<TestUnit[]>([])
  const [jsonDrafts, setJsonDrafts] = useState<Record<string, string>>({})
  const [jsonErrors, setJsonErrors] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!node) return
    setConfig(typeof node.config === 'object' && node.config ? (node.config as Record<string, unknown>) : {})
    setInputsOverride(null)
    setRemovedInputs(new Set())
    setRemovedOutputs(new Set())
    setSourceOverrides({})
    setJsonDrafts({})
    setJsonErrors({})
    const ins: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.inputs ?? {})) ins[k] = v.desc ?? ''
    setInputs(ins)
    const outs: Record<string, string> = {}
    for (const [k, v] of Object.entries(node.outputs ?? {})) outs[k] = v.desc ?? ''
    setOutputs(outs)

    const unitID = Number(node.config && typeof node.config === 'object' ? (node.config as Record<string, unknown>).unit_id ?? 0 : 0)
    if (node.type === 'api' && testSetID) {
      setUnit(null)
      listUnits(testSetID)
        .then((items) => {
          setUnits(items)
          const selected = items.find((item) => item.id === unitID)
          if (selected) setUnit(selected)
          else if (unitID) getUnit(testSetID, unitID).then(setUnit).catch(() => setUnit(null))
        })
        .catch(() => { setUnits([]); setUnit(null) })
    } else {
      setUnits([])
      setUnit(null)
    }
  }, [nodeID, node, testSetID])

  if (!node) return null

  const parseJsonDrafts = (): Record<string, unknown> | null => {
    const parsed: Record<string, unknown> = {}
    for (const [key, raw] of Object.entries(jsonDrafts)) {
      try {
        parsed[key] = JSON.parse(raw)
      } catch {
        setJsonErrors((e) => ({ ...e, [key]: 'JSON 格式错误，请修正后再保存' }))
        return null
      }
    }
    return parsed
  }

  const save = () => {
    const parsedDrafts = parseJsonDrafts()
    if (!parsedDrafts) return
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    const n = next.nodes[nodeID]
    const nextConfig = { ...config, ...parsedDrafts }
    n.config = nextConfig
    // API 节点：若用户编辑过来源（inputsOverride），回写 node.inputs（保留 type/desc）
    if (node.type === 'api' && inputsOverride) {
      const ins: Record<string, IOKey> = {}
      for (const [k, io] of Object.entries(inputsOverride)) {
        const orig = (node.inputs ?? {})[k] ?? {}
        const merged = { ...orig, ...io }
        delete (merged as Record<string, unknown>).right
        ins[k] = { type: io.type ?? orig.type ?? 'primitive', desc: io.desc, source: io.source, from: io.from, in: io.in }
      }
      n.inputs = ins
    } else if (node.type !== 'api' || !node.inputs || Object.keys(node.inputs).length === 0) {
      const ins: Record<string, { type: string; desc?: string; source?: string; from?: string }> = {}
      for (const [k, v] of Object.entries(inputs)) {
        if (removedInputs.has(k)) continue
        const orig = (node.inputs ?? {})[k] ?? {}
        ins[k] = { ...orig, desc: v, source: sourceOverrides[k] ?? orig.source }
      }
      const outs: Record<string, { type: string; desc?: string; source?: string; from?: string }> = {}
      for (const [k, v] of Object.entries(outputs)) {
        if (removedOutputs.has(k)) continue
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
  const jsonValue = (field: string, fallback: unknown): string =>
    jsonDrafts[field] ?? (config[field] !== undefined ? JSON.stringify(config[field]) : JSON.stringify(fallback))
  const updateJsonDraft = (field: string, raw: string) => {
    setJsonDrafts((d) => ({ ...d, [field]: raw }))
    try {
      setKV(field, JSON.parse(raw))
      setJsonErrors((e) => ({ ...e, [field]: '' }))
    } catch {
      setJsonErrors((e) => ({ ...e, [field]: 'JSON 格式错误，离开输入框或保存前请修正' }))
    }
  }
  const blurJsonDraft = (field: string) => {
    const raw = jsonDrafts[field]
    if (raw === undefined) return
    try {
      setKV(field, JSON.parse(raw))
      setJsonErrors((e) => ({ ...e, [field]: '' }))
    } catch {
      setJsonErrors((e) => ({ ...e, [field]: 'JSON 格式错误，请修正后再保存' }))
    }
  }

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
    const toggleRemove = (kind: 'input' | 'output', k: string) => {
      const [cur, set] = kind === 'input' ? [removedInputs, setRemovedInputs] : [removedOutputs, setRemovedOutputs]
      const next = new Set(cur)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      set(next)
    }
    return (
      <div>
        <div className="muted">I/O 契约(仅描述,连线以 parent 为准)</div>
        {Object.keys(inputs).map((k) => {
          const removed = removedInputs.has(k)
          const src = sourceOverrides[k] ?? (node.inputs ?? {})[k]?.source ?? ''
          return (
            <div key={k} className={removed ? 'io-row removed' : 'io-row'}>
              <label>
                input.{k}
                <input
                  aria-label={`input ${k} desc`}
                  value={inputs[k]}
                  onChange={(e) => setInputs((s) => ({ ...s, [k]: e.target.value }))}
                />
              </label>
              <input
                aria-label={`input ${k} source`}
                placeholder="source(祖先输出键 / $cache.key)"
                value={src}
                onChange={(e) => setSourceOverrides((s) => ({ ...s, [k]: e.target.value }))}
              />
              <button
                type="button"
                aria-label={removed ? `restore input ${k}` : `delete input ${k}`}
                onClick={() => toggleRemove('input', k)}
              >
                {removed ? '恢复' : '删除'}
              </button>
            </div>
          )
        })}
        {Object.keys(outputs).map((k) => {
          const removed = removedOutputs.has(k)
          return (
            <div key={k} className={removed ? 'io-row removed' : 'io-row'}>
              <label>
                output.{k}
                <input
                  aria-label={`output ${k} desc`}
                  value={outputs[k]}
                  onChange={(e) => setOutputs((s) => ({ ...s, [k]: e.target.value }))}
                />
              </label>
              <button
                type="button"
                aria-label={removed ? `restore output ${k}` : `delete output ${k}`}
                onClick={() => toggleRemove('output', k)}
              >
                {removed ? '恢复' : '删除'}
              </button>
            </div>
          )
        })}
      </div>
    )
  }

  const renderConfig = () => {
    switch (node.type) {
      case 'api':
        return (
          <>
            <label>
              接口
              <select
                aria-label="API 接口"
                value={Number(config.unit_id ?? 0)}
                onChange={(e) => {
                  const id = Number(e.target.value)
                  setKV('unit_id', id)
                  setUnit(units.find((item) => item.id === id) ?? null)
                }}
              >
                <option value={0}>请选择接口</option>
                {units.map((item) => (
                  <option key={item.id} value={item.id}>{item.method} {item.path} · {item.name || item.slug}</option>
                ))}
              </select>
            </label>
            <details className="mono small" open={false}>
              <summary>Swagger 原始定义（只读）</summary>
              {unit ? (
                <div className="card sub mono small">
                  <div className="strong">{unit.method} {unit.path}</div>
                  <div className="muted">slug: {unit.slug}</div>
                  {unit.name && <div className="muted">{unit.name}</div>}
                  <div className="sep" />
                  {unit.security && unit.security !== 'null' && (
                    <div>
                      <div className="muted strong">鉴权 (security)</div>
                      <pre>{tryPretty(unit.security)}</pre>
                    </div>
                  )}
                  <div className="muted strong" style={{ marginTop: 6 }}>参数 ({unit.params === 'null' || !unit.params ? 0 : (JSON.parse(unit.params) ?? []).length})</div>
                  {unit.params && unit.params !== 'null' && (() => {
                    let arr: unknown[] = []
                    try {
                      arr = JSON.parse(unit.params)
                    } catch {
                      arr = []
                    }
                    return (
                      <table className="table small mono">
                        <thead>
                          <tr><th>名</th><th>位置</th><th>类型</th><th>必填</th><th>描述</th></tr>
                        </thead>
                        <tbody>
                          {arr.map((p, i) => {
                            const d = p as Record<string, unknown>
                            return (
                              <tr key={i}>
                                <td>{String(d.name ?? '')}</td>
                                <td>{String(d.in ?? '')}</td>
                                <td>{String(d.type ?? '')}</td>
                                <td>{d.required ? '是' : '-'}</td>
                                <td>{String(d.description ?? '')}</td>
                              </tr>
                            )
                          })}
                        </tbody>
                      </table>
                    )
                  })()}
                  <div className="muted strong" style={{ marginTop: 6 }}>响应 (responses)</div>
                  {unit.responses && unit.responses !== 'null' && (
                    <pre>{tryPretty(unit.responses)}</pre>
                  )}
                </div>
              ) : (
                <p className="muted small">（无单元快照）</p>
              )}
            </details>
            <ApiParamsEditor
              unit={unit}
              unitParams={typeof config.unit === 'object' && config.unit ? String((config.unit as Record<string, unknown>).params ?? '') : null}
              tree={tree}
              nodeID={nodeID}
              params={typeof config.params === 'object' && config.params ? (config.params as Record<string, unknown>) : {}}
              headers={typeof config.headers === 'object' && config.headers ? (config.headers as Record<string, unknown>) : {}}
              inputs={inputsOverride ?? node.inputs ?? {}}
              onParamsChange={(p) => setKV('params', p)}
              onHeadersChange={(h) => setKV('headers', h)}
              onInputsChange={(ins) => {
                // 把来源变更写入本地待保存的 I/O 契约（API 节点保存时回写 inputs）
                setInputsOverride(ins)
              }}
            />
            <details className="mono small">
              <summary>原始 JSON（params / headers / inputs）</summary>
              <pre>{JSON.stringify({ params: config.params ?? {}, headers: config.headers ?? {}, inputs: (inputsOverride ?? node.inputs) ?? {} }, null, 2)}</pre>
            </details>
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
              value={jsonValue('fallback', '')}
              onChange={(e) => updateJsonDraft('fallback', e.target.value)}
              onBlur={() => blurJsonDraft('fallback')}
            />
            {jsonErrors.fallback && <div className="err small">{jsonErrors.fallback}</div>}
          </label>
        )
      case 'cache-set':
        return (
          <>
            <label>
              writes (JSON: key → value)
              <textarea
                rows={5}
                value={jsonValue('writes', {})}
                onChange={(e) => updateJsonDraft('writes', e.target.value)}
                onBlur={() => blurJsonDraft('writes')}
              />
              {jsonErrors.writes && <div className="err small">{jsonErrors.writes}</div>}
            </label>
            <label>
              static (JSON, 固定字符串值)
              <textarea
                rows={3}
                value={jsonValue('static', {})}
                onChange={(e) => updateJsonDraft('static', e.target.value)}
                onBlur={() => blurJsonDraft('static')}
              />
              {jsonErrors.static && <div className="err small">{jsonErrors.static}</div>}
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
              value={jsonValue('params', {})}
              onChange={(e) => updateJsonDraft('params', e.target.value)}
              onBlur={() => blurJsonDraft('params')}
            />
            {jsonErrors.params && <div className="err small">{jsonErrors.params}</div>}
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

// ---- 祖先输出/cache 键收集（来源联想候选） ----

// collectAncestorIds 沿 parent 链收集 nodeID 的所有祖先节点 id（不含自身）。
function collectAncestorIds(tree: FlowTree, nodeID: string): string[] {
  const out: string[] = []
  let cur = tree.nodes[nodeID]?.parent
  const seen = new Set<string>()
  while (cur && tree.nodes[cur] && !seen.has(cur)) {
    seen.add(cur)
    out.push(cur)
    cur = tree.nodes[cur]?.parent
  }
  return out
}

// collectCacheKeys 收集给定祖先节点集合中 cache-set 节点写入的 cache 键
// （config.writes 对象的键）。
function collectCacheKeys(tree: FlowTree, ancestors: string[]): string[] {
  const keys: string[] = []
  for (const id of ancestors) {
    const an = tree.nodes[id]
    if (!an || an.type !== 'cache-set') continue
    const cfg = (an.config ?? {}) as Record<string, unknown>
    const writes = cfg.writes
    if (writes && typeof writes === 'object') {
      for (const k of Object.keys(writes as Record<string, unknown>)) keys.push(k)
    }
  }
  return keys
}

// collectAncestorKeys 收集祖先节点 outputs 键 + 祖先 cache-set 写入的 cache 键，
// 去重。用于来源联想候选。
export function collectAncestorKeys(tree: FlowTree, nodeID: string): string[] {
  const ancestors = collectAncestorIds(tree, nodeID)
  const keys: string[] = []
  const seen = new Set<string>()
  for (const id of ancestors) {
    const an = tree.nodes[id]
    if (!an) continue
    for (const k of Object.keys(an.outputs ?? {})) {
      if (!seen.has(k)) {
        seen.add(k)
        keys.push(k)
      }
    }
  }
  for (const k of collectCacheKeys(tree, ancestors)) {
    if (!seen.has(k)) {
      seen.add(k)
      keys.push(k)
    }
  }
  return keys
}

// resolveSourceCandidates 返回来源联想候选：祖先 outputs 键 + cache 键
// （前缀 $cache.）+ 节点现有 inputs source 值。
export function resolveSourceCandidates(tree: FlowTree, nodeID: string): string[] {
  const ancestors = collectAncestorIds(tree, nodeID)
  const out: string[] = []
  const seen = new Set<string>()
  const push = (v: string) => {
    if (v && !seen.has(v)) {
      seen.add(v)
      out.push(v)
    }
  }
  for (const id of ancestors) {
    const an = tree.nodes[id]
    if (!an) continue
    for (const k of Object.keys(an.outputs ?? {})) push(k)
  }
  for (const k of collectCacheKeys(tree, ancestors)) push('$cache.' + k)
  const n = tree.nodes[nodeID]
  for (const io of Object.values(n?.inputs ?? {})) {
    if (io?.source) push(io.source)
  }
  return out
}

// ---- ApiParamsEditor 按参数位置分组编辑执行参数 ----

// parseParamLocations 解析单元 Swagger params [{name, in}] 为 name → 位置映射。
// 不改变后端数据结构：仍写入 config.params，仅按定位分组展示给用户。
export function parseParamLocations(paramsJSON: string | null | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  if (!paramsJSON || paramsJSON === 'null') return out
  try {
    const arr = JSON.parse(paramsJSON)
    if (!Array.isArray(arr)) return out
    for (const p of arr) {
      if (p && typeof p.name === 'string' && p.name) out[p.name] = String(p.in ?? '')
    }
  } catch {
    /* ignore malformed */
  }
  return out
}

const PARAM_GROUP_META: Record<string, { label: string; hint: string }> = {
  header: { label: '请求头 (header)', hint: '发送到 HTTP 请求头，如认证/签名头' },
  query: { label: '查询参数 (query)', hint: '拼到 URL 查询串 ?k=v' },
  body: { label: '请求体 (body)', hint: '放进请求体字段（对应 unit 的 schema）' },
  path: { label: '路径参数 (path)', hint: '替换 URL 路径占位符 {name}' },
  secret: { label: '密钥 (secret)', hint: '认证密钥，随认证头发送' },
  default: { label: '其他（未声明位置）', hint: '后端按启发式路由：认证键→头 / 有请求体→body / 否则→query' },
}

const GROUP_ORDER = ['header', 'query', 'body', 'path', 'secret', 'default']

function groupOf(inLoc: string): string {
  return inLoc && PARAM_GROUP_META[inLoc] ? inLoc : 'default'
}

// ApiParamRow 是请求构造区中一行参数的可展示结构。它把三类来源合并到一处：
// - 来源 source（node.inputs[key].source）
// - 覆盖值 value（config.params[key]），存在即表示用户手动覆盖连线来源值
// - 显式请求头 explicit（config.headers[key]），强制进 HTTP 头，最高优先级
// - 是否 swagger 声明 declared（config.unit.params 里有该 key）
export interface ApiParamRow {
  key: string
  group: string
  declaredIn: string // swagger 声明的 in 位置（''=未声明）
  source?: string
  value?: unknown
  hasValue?: boolean
  explicit?: unknown
  isExplicitHeader?: boolean
  declared: boolean
}

// groupApiParams 合并 params / headers / inputs / unitParams 为按 in 分组展示结构。
// 不改变后端数据结构，仅供 UI 渲染分组。
export function groupApiParams(
  params: Record<string, unknown>,
  headers: Record<string, unknown>,
  inputs: Record<string, IOKey>,
  unitParams: string | null | undefined,
): Record<string, ApiParamRow[]> {
  const locations = parseParamLocations(unitParams)
  const groups: Record<string, ApiParamRow[]> = {}
  for (const group of GROUP_ORDER) groups[group] = []

  const addRow = (key: string, declIn: string, partial: Partial<ApiParamRow>) => {
    const g = groupOf(declIn)
    groups[g].push({
      key,
      group: g,
      declaredIn: declIn,
      declared: locations[key] !== undefined,
      ...partial,
    })
  }

  // 1) 显式请求头：config.headers（强制进 HTTP 头）
  for (const [k, v] of Object.entries(headers)) {
    addRow(k, 'header', { explicit: v, isExplicitHeader: true })
  }
  // 2) config.params：按 swagger 声明定位分组
  for (const [k, v] of Object.entries(params)) {
    // 扁平 body 字段优先使用输入绑定的位置，避免同名字段落入 default 后重复显示。
    addRow(k, inputs[k]?.in ?? locations[k] ?? '', { value: v, hasValue: true })
  }
  // 3) Swagger 已声明参数也要保留为行：清除覆盖值时不能把参数条目误删。
  // 这些参数可能尚未建立 inputs/source，但仍应允许用户继续编辑或选择来源。
  for (const [k, declIn] of Object.entries(locations)) {
    const g = groupOf(declIn)
    if (!groups[g].some((r) => r.key === k)) addRow(k, declIn, {})
  }
  // 4) node.inputs：连线来源（source），合并进对应行（若已存在）或新增
  for (const [k, io] of Object.entries(inputs)) {
    const existing = GROUP_ORDER.flatMap((name) => groups[name]).find((r) => r.key === k)
    if (existing) {
      existing.source = io.source
    } else {
      addRow(k, io.in ?? locations[k] ?? '', { source: io.source })
    }
  }
  return groups
}

function ApiParamsEditor({
  unit,
  unitParams,
  tree,
  nodeID,
  params,
  headers,
  inputs,
  onParamsChange,
  onHeadersChange,
  onInputsChange,
}: {
  unit: TestUnit | null
  unitParams?: string | null
  tree: FlowTree
  nodeID: string
  params: Record<string, unknown>
  headers: Record<string, unknown>
  inputs: Record<string, IOKey>
  onParamsChange: (next: Record<string, unknown>) => void
  onHeadersChange: (next: Record<string, unknown>) => void
  onInputsChange: (next: Record<string, IOKey>) => void
}) {
  const candidates = resolveSourceCandidates(tree, nodeID)
  const groups = groupApiParams(params, headers, inputs, unit?.params ?? unitParams)
  const [valueModes, setValueModes] = useState<Record<string, 'fixed' | 'output' | 'cache'>>({})
  useEffect(() => setValueModes({}), [nodeID])

  const parse = (raw: string): unknown => {
    const trimmed = raw.trim()
    if (trimmed === '') return ''
    try {
      return JSON.parse(trimmed)
    } catch {
      return raw
    }
  }
  const displayVal = (v: unknown): string => {
    if (v === null) return 'null'
    if (typeof v === 'object') return JSON.stringify(v)
    return String(v)
  }

  // 更新某一参数行的来源（写回 node.inputs[key].source）
  const setSource = (key: string, src: string, location?: IOKey['in']) => {
    const orig = inputs[key] ?? {}
    onInputsChange({ ...inputs, [key]: { ...orig, type: orig.type ?? 'primitive', source: src, ...(location ? { in: location } : {}) } })
 if (location === 'header' && key in headers) onHeadersChange({...headers, [key]: null})
  }
  const setLocation = (key: string, loc: string) => {
    const orig = inputs[key] ?? {}
    onInputsChange({ ...inputs, [key]: { ...orig, type: orig.type ?? 'primitive', in: loc as IOKey['in'] } })
  }
  // 更新覆盖值（按通道落点）：header/secret → config.headers，其余 → config.params；空值视为清除覆盖
  const setValue = (key: string, raw: string, channel: 'params' | 'headers') => {
    const trimmed = raw.trim()
    if (channel === 'headers') {
      const next = { ...headers }
      // 保留 null 声明，避免清空覆盖头后整行从编辑器消失；执行器会将 null 视为不覆盖来源。
      if (trimmed === '') next[key] = null
      else next[key] = parse(raw)
      onHeadersChange(next)
    } else {
      const next = { ...params }
      if (trimmed === '') delete next[key]
      else next[key] = parse(raw)
      onParamsChange(next)
    }
  }
  // 删除未声明参数：清 inputs + params + headers
  const removeUndeclared = (key: string) => {
    const ni = { ...inputs }
    delete ni[key]
    onInputsChange(ni)
    const np = { ...params }
    delete np[key]
    onParamsChange(np)
    if (key in headers) {
      const nh = { ...headers }
      delete nh[key]
      onHeadersChange(nh)
    }
  }
  const removeHeader = (key: string) => {
    const next = { ...headers }
    delete next[key]
    onHeadersChange(next)
  }

  // 动态添加区域
  const [draftLoc, setDraftLoc] = useState<string>('default')
  const [draftName, setDraftName] = useState('')
  const [draftValue, setDraftValue] = useState('')
  const commitDraft = () => {
    if (!draftName.trim()) return
    const key = draftName.trim()
    if (draftLoc === 'header' || draftLoc === 'secret') {
      // 显式请求头/密钥 → 走 config.headers，执行器强制进 HTTP 请求头
      onHeadersChange({ ...headers, [key]: parse(draftValue) })
    } else if (draftLoc === 'default') {
      // 其他：写入 config.params + 建立 source（留空）以可在此编辑来源
      if (!(key in params)) onParamsChange({ ...params, [key]: parse(draftValue) })
      if (!(key in inputs)) onInputsChange({ ...inputs, [key]: { type: 'primitive', source: '' } })
    } else {
      // query/body/path → 写入固定值，同时记录位置，确保后续切换为引用时参数不会丢失。
      if (!(key in params)) onParamsChange({ ...params, [key]: parse(draftValue) })
      if (!(key in inputs)) onInputsChange({ ...inputs, [key]: { type: 'primitive', source: '', in: draftLoc as IOKey['in'] } })
    }
    setDraftName('')
    setDraftValue('')
    setDraftLoc('default')
  }

  // 单一来源行的渲染（来源编辑框 + 祖先键联想 + 覆盖值编辑框）
  const renderSourceRow = (r: ApiParamRow) => {
    const isHeader = r.group === 'header' || r.group === 'secret'
    const hasOverride = isHeader ? r.key in headers && headers[r.key] !== null : r.hasValue
    const curVal = isHeader ? headers[r.key] : r.value
    const mode = valueModes[r.key] ?? (hasOverride ? 'fixed' : r.source?.startsWith('$cache.') ? 'cache' : r.source ? 'output' : 'fixed')
    return (
      <div key={r.key} className="param-row row" style={{ gap: 4, alignItems: 'center', flexWrap: 'wrap' }}>
        <span className="mono small param-key" title={r.declared ? `swagger ${r.declaredIn}` : '未在 swagger 声明'}>
          {r.key}
          {(isHeader && hasOverride) && <span className="badge">显式头</span>}
        </span>
        {inputs[r.key] && <select className="small" aria-label={`input ${r.key} location`} value={inputs[r.key].in ?? r.declaredIn ?? 'body'} onChange={(e) => setLocation(r.key, e.target.value)}>
          <option value="header">header</option><option value="body">body</option><option value="query">query</option><option value="path">path</option>
        </select>}
        <select
          className="small"
          aria-label={`value mode ${r.key}`}
          value={mode}
          onChange={(e) => {
            const next = e.target.value as 'fixed' | 'output' | 'cache'
            setValueModes((current) => ({ ...current, [r.key]: next }))
            if (next === 'fixed') {
              setSource(r.key, '')
            } else {
              // 先建立空的输入绑定再清除固定值，避免未声明参数从规范化列表消失。
              if (!inputs[r.key]) {
                onInputsChange({ ...inputs, [r.key]: { type: 'primitive', source: '', in: (r.declaredIn || (r.group === 'default' ? 'body' : r.group)) as IOKey['in'] } })
              }
              setValue(r.key, '', isHeader ? 'headers' : 'params')
            }
          }}
        >
          <option value="fixed">固定值</option>
          <option value="output">上游输出</option>
          <option value="cache">共享缓存</option>
        </select>
        {mode !== 'fixed' && <SourceAutocomplete
          value={r.source ?? ''}
          candidates={candidates.filter((candidate) => mode !== 'cache' || candidate.startsWith('$cache.'))}
          onSelect={(v) => setSource(r.key, v, isHeader ? 'header' : undefined)} data-source-key={r.key} />}
 {mode === 'fixed' && <input
          className={'mono small' + (hasOverride ? ' override' : '')}
          placeholder={isHeader ? '覆盖头 (可选,留空用来源)' : '覆盖值 (可选,留空用来源)'}
          value={hasOverride ? displayVal(curVal) : ''}
          onChange={(e) => setValue(r.key, e.target.value, isHeader ? 'headers' : 'params')}
          data-param-key={r.key}
          style={{ flex: 1, minWidth: 120 }}
          title={hasOverride ? (isHeader ? '已覆盖为显式请求头' : '已覆盖来源值') : ''}
        />}
        {hasOverride && <span className="badge">已覆盖</span>}
        {!r.declared && (
          <button className="link danger small" onClick={() => removeUndeclared(r.key)} title="删除此参数">
            删除
          </button>
        )}
        {isHeader && hasOverride && (
          <button className="link small" onClick={() => removeHeader(r.key)} title="移除显式请求头覆盖">
            ✕
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="api-params-editor">
      <div className="muted small">请求参数（按位置分组 · 来源可编辑 · 值可覆盖）</div>
      {GROUP_ORDER.map((g) => {
        const rows = groups[g]
        if (rows.length === 0 && g !== 'default') return null
        const meta = PARAM_GROUP_META[g === 'default' ? 'default' : g]
        return (
          <div key={g} className="card sub" style={{ marginTop: 6 }}>
            <div className="muted strong small">{meta.label}</div>
            <div className="muted small">{meta.hint}</div>
            {rows.length === 0 && <p className="muted small">（无）</p>}
            {rows.map(renderSourceRow)}
          </div>
        )
      })}
      <div className="card sub" style={{ marginTop: 6 }}>
        <div className="muted strong small">添加参数</div>
        <div className="row" style={{ gap: 4, alignItems: 'center', flexWrap: 'wrap' }}>
          <select value={draftLoc} onChange={(e) => setDraftLoc(e.target.value)} data-testid="api-param-loc">
            {['header', 'query', 'body', 'path', 'secret', 'default'].map((g) => (
              <option key={g} value={g}>
                {PARAM_GROUP_META[g].label}
              </option>
            ))}
          </select>
          <input
            className="mono small"
            placeholder="参数名"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            data-testid="api-param-name"
            style={{ width: 120 }}
          />
          <input
            className="mono small"
            placeholder="值（JSON 或字符串 / 显式头值）"
            value={draftValue}
            onChange={(e) => setDraftValue(e.target.value)}
            data-testid="api-param-value"
            style={{ flex: 1, minWidth: 120 }}
          />
          <button className="link small" onClick={commitDraft}>
            添加
          </button>
        </div>
      </div>
    </div>
  )
}

// SourceAutocomplete：来源编辑框，自由文本 + 祖先输出/cache 键联想补全。
// 用原生 input + <datalist> 实现轻量联想（过滤候选），不引入额外依赖。
function SourceAutocomplete({
  value,
  candidates,
  onSelect,
  'data-source-key': dsKey,
}: {
  value: string
  candidates: string[]
  onSelect: (v: string) => void
  'data-source-key'?: string
}) {
  const options = value && !candidates.includes(value) ? [value, ...candidates] : candidates
  return (
    <select
      className="mono small source-select"
      value={value}
      onChange={(e) => onSelect(e.target.value)}
      aria-label={`source candidates ${dsKey ?? ''}`}
      data-source-key={dsKey}
      title="选择祖先输出或缓存来源"
      style={{ minWidth: 160 }}
    >
      <option value="">选择来源</option>
      {options.map((c) => (
        <option key={c} value={c}>{c}</option>
      ))}
    </select>
  )
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
