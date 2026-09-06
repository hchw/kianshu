import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Background,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import './CaseCanvas.css'
import type { CaseNode } from '../../api/caseFlow'
import CaseNodePanel from './CaseNodePanel'

type CaseCanvasProps = {
  caseFlowID: number
  root: CaseNode
  selected: string | null
  onSelect: (id: string | null) => void
  onDelete?: (id: string) => void
  onPositionChange?: (id: string, x: number, y: number) => void
  revision?: number
  onSaved?: () => void
}

/** 展平树并记录父子关系。 */
function flatten(root: CaseNode) {
  const nodes: CaseNode[] = []
  const parents = new Map<string, string>()
  const walk = (node: CaseNode, parent?: string) => {
    nodes.push(node)
    if (parent) parents.set(node.id, parent)
    node.children?.forEach((child) => walk(child, node.id))
  }
  walk(root)
  return { nodes, parents }
}

/**
 * 自左向右的树布局（XMind 风格）：
 * x = 深度 × 横向间距；叶子纵向依次排列，父节点纵向居中于子节点。
 */
function layoutCaseTree(root: CaseNode) {
  const positions = new Map<string, { x: number; y: number }>()
  const columnGap = 280
  const rowGap = 120
  let leafIndex = 0
  const place = (node: CaseNode, depth: number): number => {
    const children = node.children ?? []
    if (children.length === 0) {
      const y = leafIndex++ * rowGap
      positions.set(node.id, { x: depth * columnGap, y })
      return y
    }
    const childY = children.map((child) => place(child, depth + 1))
    const y = (childY[0] + childY[childY.length - 1]) / 2
    positions.set(node.id, { x: depth * columnGap, y })
    return y
  }
  place(root, 0)
  return positions
}

function CaseNodeView({ id, data }: NodeProps) {
  const node = data.node as CaseNode
  return (
    <>
      <Handle type="target" position={Position.Left} />
      <div className={`case-flow-node ${node.status === 'covered' ? 'covered' : ''}`}>
        <div className="case-flow-node-status" aria-label={node.status} />
        <strong>{node.title}</strong>
        {node.description && <span className="case-flow-node-desc">{node.description}</span>}
        <span className="muted mono">{node.children?.length ? '分解节点' : '测试用例'}</span>
        <span className="case-flow-node-id">{id}</span>
      </div>
      <Handle type="source" position={Position.Right} />
    </>
  )
}

function CanvasInner({ caseFlowID, root, selected, onSelect, onDelete, onPositionChange, revision = 0, onSaved = () => {} }: CaseCanvasProps) {
  const { nodes: treeNodes, parents } = useMemo(() => flatten(root), [root])
  const layout = useMemo(() => layoutCaseTree(root), [root])
  const initialNodes = useMemo<Node[]>(() => treeNodes.map((node) => ({
    id: node.id,
    type: 'caseNode',
    position: { x: node.x ?? layout.get(node.id)?.x ?? 0, y: node.y ?? layout.get(node.id)?.y ?? 0 },
    data: { node },
    className: selected === node.id ? 'case-flow-node-selected' : '',
  })), [treeNodes, selected, layout])
  const edges = useMemo<Edge[]>(() => Array.from(parents, ([child, parent]) => ({
    id: `${parent}-${child}`,
    source: parent,
    target: child,
    // 默认 bezier 曲线：自左向右的平滑弧线
  })), [parents])
  const [nodes, setNodes] = useState(initialNodes)
  const draggingRef = useRef(false)

  // 树变化（非拖拽中）时同步节点
  useEffect(() => {
    if (!draggingRef.current) setNodes(initialNodes)
  }, [initialNodes])

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    for (const c of changes) {
      if (c.type === 'position') {
        if (c.dragging === true) draggingRef.current = true
        else if (c.dragging === false) draggingRef.current = false
      }
    }
    setNodes((current) => applyNodeChanges(changes, current))
  }, [])

  const selectedNode = selected ? treeNodes.find((node) => node.id === selected) : null

  return (
    <div className="case-editor-main">
      <div className="case-canvas">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={{ caseNode: CaseNodeView }}
          onNodesChange={onNodesChange}
          onNodeClick={(_, node) => onSelect(node.id)}
          onNodeDragStop={(_, node) => onPositionChange?.(node.id, node.position.x, node.position.y)}
          onPaneClick={() => onSelect(null)}
          onNodesDelete={(deleted) => deleted.forEach((node) => onDelete?.(node.id))}
          selectionOnDrag
          panOnDrag
          selectionKeyCode="Control"
          multiSelectionKeyCode="Control"
          fitView
        >
          <Background gap={20} size={1} />
        </ReactFlow>
        <div className="case-canvas-hint">Ctrl/⌘ 多选 · 拖动节点调整画布位置</div>
      </div>
      {selectedNode && (
        <CaseNodePanel
          caseFlowID={caseFlowID}
          revision={revision}
          node={selectedNode}
          isRoot={selectedNode.id === root.id}
          onSaved={onSaved}
          onDelete={(id) => onDelete?.(id)}
          onClose={() => onSelect(null)}
        />
      )}
    </div>
  )
}

export default function CaseCanvas(props: CaseCanvasProps) {
  return <ReactFlowProvider><CanvasInner {...props} /></ReactFlowProvider>
}
