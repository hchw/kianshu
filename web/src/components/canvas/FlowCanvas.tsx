import { useMemo, useState, useEffect, useRef, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react'
import { useToast } from '../feedback/Toast'
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Handle,
  Position,
  useReactFlow,
  useNodesState,
  applyNodeChanges,
  type Node,
  type Edge,
  type NodeProps,
  type OnNodesChange,
  type OnNodeDrag,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { FlowTree } from '../../api/flow'
import { layoutTree, linkAllowed, reconcileChildren, NODE_LABELS } from '../../lib/tree'
import { statusLabel } from '../results/ResultsPanel'
import NodePanel from './NodePanel'

export interface NodeRunStatus {
  node_id: string
  status: string
  input?: unknown
  output?: unknown
  error?: string
  started_at?: string
  finished_at?: string
}

interface Props {
  tree: FlowTree
  selected: string | null
  onSelect: (id: string | null) => void
  onTreeChange: (t: FlowTree) => void
  onSaved: () => void
  onDelete?: (id: string) => void
  testSetID: number
  /** 最近一次试运行的节点结果，key 为 node_id */
  nodeResults: Record<string, NodeRunStatus> | null
}

const COLORS: Record<string, string> = {
  start: 'var(--ok)',
  api: 'var(--primary)',
  assert: 'var(--put)',
  loop: 'var(--node-loop)',
  try: 'var(--node-try)',
  catch: 'var(--danger)',
  'cache-set': 'var(--muted)',
  adapter: 'var(--node-adapter)',
}

const PALETTE_KEY = 'kianshu_palette_open'

// 模块级 ref：自定义节点组件通过此 ref 调用画布的 popover 打开逻辑
type PopoverOpener = (nodeId: string, result: NodeRunStatus, anchor: HTMLElement) => void
const gPopoverOpener: { current: PopoverOpener | null } = { current: null }

function KianshuNode(props: NodeProps) {
  const data = props.data as Record<string, unknown>
  const label = data.label as string
  const nr = data.nodeResult as NodeRunStatus | undefined
  const status = data.status as string | undefined
  const dotRef = useRef<HTMLDivElement>(null)

  const dotColor =
    status === 'failed'
      ? 'var(--danger)'
      : status === 'ok' || status === 'soft-stop'
        ? 'var(--ok)'
        : undefined

  return (
    <>
      <Handle type="target" position={Position.Top} />
      {dotColor && nr && (
        <div
          ref={dotRef}
          className="node-status-dot"
          style={{ background: dotColor }}
          title={`${statusLabel(status!)} — 点击查看详情`}
          onClick={(e) => {
            e.stopPropagation()
            if (dotRef.current && gPopoverOpener.current) {
              gPopoverOpener.current(props.id, nr, dotRef.current)
            }
          }}
        />
      )}
      <div style={{ whiteSpace: 'pre-line', textAlign: 'center' }}>{label}</div>
      <Handle type="source" position={Position.Bottom} />
    </>
  )
}

export default function FlowCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <CanvasInner {...props} />
    </ReactFlowProvider>
  )
}

function CanvasInner({ tree, selected, onSelect, onTreeChange, onSaved, onDelete, testSetID, nodeResults }: Props) {
  const deleteNode = onDelete ?? (() => {})
  const [paletteOpen, setPaletteOpen] = useState(() => localStorage.getItem(PALETTE_KEY) !== '0')
  const togglePalette = () =>
    setPaletteOpen((o) => {
      localStorage.setItem(PALETTE_KEY, o ? '0' : '1')
      return !o
    })
  const toast = useToast()
  const draggingRef = useRef(false)

  // 节点状态点 popover
  const [dotPopover, setDotPopover] = useState<{
    nodeId: string
    result: NodeRunStatus
    x: number
    y: number
  } | null>(null)
  const dotPopoverRef = useRef<HTMLDivElement>(null)
  const skipDotCloseRef = useRef(false)

  // 从 tree 计算节点（仅结构/坐标来源，不含拖拽中间态）
  const treeNodes: Node[] = useMemo(() => {
    const { positions, ordered } = layoutTree(tree)
    return ordered.map((id) => {
      const n = tree.nodes[id]
      const pos = positions.get(id)!
      const color = COLORS[n?.type ?? ''] ?? 'var(--node-default)'
      const nr = nodeResults?.[id]
      return {
        id,
        type: 'kianshu',
        position: { x: pos.x, y: pos.y },
        className: `flow-node type-${n?.type ?? ''}`,
        data: {
          label: `${NODE_LABELS[n?.type ?? ''] ?? n?.type}\n${id}`,
          nodeType: n?.type ?? '',
          status: nr?.status ?? undefined,
          nodeResult: nr ?? undefined,
          style: {
            width: 120,
            borderColor: selected === id ? 'var(--primary)' : 'var(--border)',
            borderWidth: selected === id ? 2 : 1,
            borderRadius: 'var(--radius-10)',
            background: 'var(--surface)',
            color: 'var(--text)',
            boxShadow: selected === id ? 'var(--shadow-md)' : 'var(--shadow-sm)',
          } as unknown as Record<string, string>,
        },
        style: {
          ...({ '--node-color': color } as Record<string, string>),
          width: 120,
          borderColor: selected === id ? 'var(--primary)' : 'var(--border)',
          borderWidth: selected === id ? 2 : 1,
          borderRadius: 'var(--radius-10)',
          background: 'var(--surface)',
          color: 'var(--text)',
          boxShadow: selected === id ? 'var(--shadow-md)' : 'var(--shadow-sm)',
        },
      }
    })
  }, [tree, selected, nodeResults])

  const [nodes, setNodes] = useNodesState(treeNodes)

  // tree 变更（非拖拽）时同步到 ReactFlow 内部状态
  useEffect(() => {
    if (!draggingRef.current) {
      setNodes(treeNodes)
    }
  }, [treeNodes, setNodes])

  // 包装 onNodesChange：拖拽期间通过 applyNodeChanges 更新局部位置
  const onNodesChange: OnNodesChange = useCallback(
    (changes) => {
      // 检测是否进入/退出拖拽
      for (const c of changes) {
        if (c.type === 'position') {
          if (c.dragging === true) draggingRef.current = true
          else if (c.dragging === false) draggingRef.current = false
        }
      }
      setNodes((nds) => applyNodeChanges(changes, nds))
    },
    [setNodes],
  )

  // 边线计算：遍历 tree 的 children 关系
  const edges: Edge[] = useMemo(() => {
    const result: Edge[] = []
    for (const [id, n] of Object.entries(tree.nodes)) {
      for (const c of n?.children ?? []) {
        result.push({
          id: `${id}-${c}`,
          source: id,
          target: c,
          type: 'default',
          animated: false,
        })
      }
    }
    return result
  }, [tree])

  const { fitView } = useReactFlow()

  // 点击弹窗外关闭
  useEffect(() => {
    if (!dotPopover) return
    const handler = (e: MouseEvent) => {
      if (skipDotCloseRef.current) {
        skipDotCloseRef.current = false
        return
      }
      const target = e.target as HTMLElement
      if (target.closest('.node-status-dot')) return
      if (dotPopoverRef.current?.contains(target)) return
      setDotPopover(null)
    }
    document.addEventListener('mousedown', handler, true)
    return () => document.removeEventListener('mousedown', handler, true)
  }, [dotPopover])

  // 仅在首次挂载时适配视口，不在每次拖拽/更新时重复触发
  useEffect(() => {
    fitView({ padding: 0.2 })
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const onNodeDragStop: OnNodeDrag = (_, node) => {
    const n = tree.nodes[node.id]
    if (!n) return
    const next: FlowTree = { ...tree, nodes: { ...tree.nodes } }
    next.nodes[node.id] = { ...n, x: node.position.x, y: node.position.y }
    onTreeChange(next)
    onSaved()
  }

  // Drop every user-placed coordinate so layoutTree falls back to the grid,
  // then re-fit the viewport to the freshly laid-out tree.
  const relayout = () => {
    const next: FlowTree = { ...tree, nodes: { ...tree.nodes } }
    for (const id of Object.keys(next.nodes)) {
      const { x: _x, y: _y, ...rest } = next.nodes[id]
      next.nodes[id] = rest
    }
    onTreeChange(next)
    onSaved()
    fitView({ padding: 0.2 })
  }

  const onConnect = (conn: { source: string; target: string }) => {
    const errs = linkAllowed(tree, conn.source, conn.target)
    if (errs.length > 0) {
      toast.error(`无法连接: ${errs.map((e) => e.message).join('; ')}`)
      return
    }
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    next.nodes[conn.target].parent = conn.source
    reconcileChildren(next)
    onTreeChange(next)
    onSaved()
  }

  const onDrop = (ev: React.DragEvent) => {
    ev.preventDefault()
    const type = ev.dataTransfer.getData('node-type')
    if (!type) return
    // 路线 C：从落点 DOM 上溯 .react-flow__node 取 data-id 作父节点
    const el = (ev.target as HTMLElement).closest('.react-flow__node')
    const parentId = el?.getAttribute('data-id')
    // 决策 A：落在空白（无命中节点）则不建节点，避免孤儿
    if (!parentId || !tree.nodes[parentId]) return
    const id = `n${Date.now().toString(36)}`
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    const config = type === 'catch' ? { fallback: {} } : {}
    next.nodes[id] = {
      id,
      type,
      inputs: {},
      outputs: {},
      config,
      parent: parentId,
    }
    reconcileChildren(next)
    onTreeChange(next)
    onSelect(id)
  }

  const onDragOver = (ev: React.DragEvent) => {
    ev.preventDefault()
  }

  // 打开节点状态弹窗
  const openDotPopover = (nodeId: string, result: NodeRunStatus, anchor: HTMLElement) => {
    const rect = anchor.getBoundingClientRect()
    skipDotCloseRef.current = true
    setDotPopover({
      nodeId,
      result,
      x: Math.min(rect.left, window.innerWidth - 316),
      y: Math.max(8, Math.min(rect.bottom + 4, window.innerHeight - 340)),
    })
  }

  // 将 openDotPopover 挂到模块级 ref 供 KianshuNode 调用
  useEffect(() => {
    gPopoverOpener.current = openDotPopover
    return () => {
      gPopoverOpener.current = null
    }
  })

  const nodeTypes = useMemo(
    () => ({
      kianshu: KianshuNode,
    }),
    [],
  )

  return (
    <div className="editor-main">
      <aside className={paletteOpen ? 'palette-rail' : 'palette-rail collapsed'}>
        <button
          className="rail-toggle"
          onClick={togglePalette}
          title={paletteOpen ? '收起节点面板' : '展开节点面板'}
          aria-label={paletteOpen ? '收起节点面板' : '展开节点面板'}
        >
          {paletteOpen ? (
            <ChevronLeft size={16} aria-hidden="true" />
          ) : (
            <ChevronRight size={16} aria-hidden="true" />
          )}
        </button>
        <div className="palette">
          {['api', 'assert', 'loop', 'try', 'cache-set', 'adapter', 'catch'].map((t) => (
            <div
              key={t}
              className="palette-item"
              draggable
              onDragStart={(e) => e.dataTransfer.setData('node-type', t)}
            >
              + {NODE_LABELS[t]}
            </div>
          ))}
        </div>
        {!paletteOpen && <span className="rail-label">节点</span>}
      </aside>
      <div className="canvas-wrap">
        <div className="canvas" onDrop={onDrop} onDragOver={onDragOver}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onConnect={onConnect}
          onNodesChange={onNodesChange}
          onNodeDragStop={onNodeDragStop}
          onNodeClick={(_, n) => onSelect(n.id)}
          onPaneClick={() => onSelect(null)}
        >
          <Background gap={16} size={1} />
        </ReactFlow>
        <button className="ghost canvas-relayout" onClick={relayout} title="放弃手动摆放，恢复自动布局">
          <RotateCcw size={14} aria-hidden="true" />
          自动重排
        </button>
      </div>
      {selected && (
        <NodePanel
          tree={tree}
          nodeID={selected}
          onTreeChange={onTreeChange}
          onSaved={onSaved}
          onDelete={deleteNode}
          onClose={() => onSelect(null)}
          testSetID={testSetID}
        />
      )}
      </div>

      {/* 节点状态点 popover（portal 到 body，避开 ReactFlow transform） */}
      {dotPopover &&
        createPortal(
          <div
            ref={dotPopoverRef}
            className="node-log-popover"
            style={{ left: dotPopover.x, top: dotPopover.y }}
          >
            <div className="popover-head">
              <span className="strong mono">{dotPopover.nodeId}</span>
              <span className={`badge ${dotPopover.result.status === 'failed' ? 'failed' : 'ok'}`}>
                {statusLabel(dotPopover.result.status)}
              </span>
              <button className="link" onClick={() => setDotPopover(null)}>
                ✕
              </button>
            </div>
            {dotPopover.result.error && (
              <div className="popover-section">
                <div className="muted" style={{ fontSize: 11, marginBottom: 4 }}>错误</div>
                <pre style={{ color: 'var(--danger)' }}>{dotPopover.result.error}</pre>
              </div>
            )}
            {dotPopover.result.input !== undefined && (
              <div className="popover-section">
                <div className="muted" style={{ fontSize: 11, marginBottom: 4 }}>输入</div>
                <pre>{safeStringify(dotPopover.result.input)}</pre>
              </div>
            )}
            {dotPopover.result.output !== undefined && (
              <div className="popover-section">
                <div className="muted" style={{ fontSize: 11, marginBottom: 4 }}>输出</div>
                <pre>{safeStringify(dotPopover.result.output)}</pre>
              </div>
            )}
            {!dotPopover.result.error &&
              dotPopover.result.input === undefined &&
              dotPopover.result.output === undefined && (
                <div className="muted" style={{ padding: 8 }}>无详细数据</div>
              )}
          </div>,
          document.body,
        )}
    </div>
  )
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}
