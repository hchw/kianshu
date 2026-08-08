import { useMemo, useState, useEffect, useRef, useCallback } from 'react'
import { ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react'
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Position,
  useReactFlow,
  useNodesState,
  applyNodeChanges,
  type Node,
  type Edge,
  type OnNodesChange,
  type OnNodeDrag,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { FlowTree } from '../../api/flow'
import { layoutTree, linkAllowed, NODE_LABELS } from '../../lib/tree'
import NodePanel from './NodePanel'

interface Props {
  tree: FlowTree
  selected: string | null
  onSelect: (id: string | null) => void
  onTreeChange: (t: FlowTree) => void
  onSaved: () => void
  onDelete: (id: string) => void
  testSetID: number
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

export default function FlowCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <CanvasInner {...props} />
    </ReactFlowProvider>
  )
}

function CanvasInner({ tree, selected, onSelect, onTreeChange, onSaved, onDelete, testSetID }: Props) {
  const [paletteOpen, setPaletteOpen] = useState(() => localStorage.getItem(PALETTE_KEY) !== '0')
  const togglePalette = () =>
    setPaletteOpen((o) => {
      localStorage.setItem(PALETTE_KEY, o ? '0' : '1')
      return !o
    })
  const draggingRef = useRef(false)

  // 从 tree 计算节点（仅结构/坐标来源，不含拖拽中间态）
  const treeNodes: Node[] = useMemo(() => {
    const { positions, ordered } = layoutTree(tree)
    return ordered.map((id) => {
      const n = tree.nodes[id]
      const pos = positions.get(id)!
      const color = COLORS[n?.type ?? ''] ?? 'var(--node-default)'
      return {
        id,
        type: 'default',
        position: { x: pos.x, y: pos.y },
        className: 'flow-node',
        data: { label: `${NODE_LABELS[n?.type ?? ''] ?? n?.type}\n${id}` },
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
  }, [tree, selected])

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
          sourcePosition: Position.Bottom,
          targetPosition: Position.Top,
          animated: false,
        })
      }
    }
    return result
  }, [tree])

  const { fitView } = useReactFlow()

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
      alert(`无法连接: ${errs.map((e) => e.message).join('; ')}`)
      return
    }
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    next.nodes[conn.target].parent = conn.source
    onTreeChange(next)
    onSaved()
  }

  const onDrop = (ev: React.DragEvent) => {
    ev.preventDefault()
    const type = ev.dataTransfer.getData('node-type')
    if (!type) return
    const id = `n${Date.now().toString(36)}`
    const next: FlowTree = JSON.parse(JSON.stringify(tree))
    next.nodes[id] = {
      id,
      type,
      inputs: {},
      outputs: {},
      config: {},
    }
    onTreeChange(next)
    onSelect(id)
  }

  const onDragOver = (ev: React.DragEvent) => {
    ev.preventDefault()
  }

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
          {['api', 'assert', 'loop', 'try', 'cache-set', 'adapter'].map((t) => (
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
          onDelete={onDelete}
          onClose={() => onSelect(null)}
          testSetID={testSetID}
        />
      )}
      </div>
    </div>
  )
}
