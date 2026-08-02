import { useMemo } from 'react'
import { ReactFlow, Background, type Node, type Edge } from '@xyflow/react'
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
  testSetID: number
}

const COLORS: Record<string, string> = {
  start: '#2f7d46',
  api: '#2b6cb0',
  assert: '#b7791f',
  loop: '#6b46c1',
  try: '#805ad5',
  catch: '#c53030',
  'cache-set': '#718096',
  adapter: '#319795',
}

export default function FlowCanvas({ tree, selected, onSelect, onTreeChange, onSaved, testSetID }: Props) {
  const { positions, ordered } = useMemo(() => layoutTree(tree), [tree])

  const nodes: Node[] = ordered.map((id) => {
    const n = tree.nodes[id]
    const pos = positions.get(id)!
    const color = COLORS[n?.type ?? ''] ?? '#4a5568'
    return {
      id,
      position: { x: pos.x, y: pos.y },
      data: { label: `${NODE_LABELS[n?.type ?? ''] ?? n?.type}\n${id}`, color },
      style: {
        borderColor: selected === id ? color : '#cbd5e0',
        borderWidth: selected === id ? 3 : 1,
        borderRadius: 8,
        background: selected === id ? `${color}22` : '#fff',
        width: 180,
        color: '#1a202c',
      },
    }
  })

  const edges: Edge[] = []
  for (const id of ordered) {
    const n = tree.nodes[id]
    for (const c of n?.children ?? []) {
      edges.push({
        id: `${id}-${c}`,
        source: id,
        target: c,
        type: 'smoothstep',
        animated: false,
      })
    }
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
    <div className="canvas-wrap">
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
      <div className="canvas" onDrop={onDrop} onDragOver={onDragOver}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onConnect={onConnect}
          onNodeClick={(_, n) => onSelect(n.id)}
          onPaneClick={() => onSelect(null)}
          fitView
        >
          <Background gap={16} size={1} />
        </ReactFlow>
      </div>
      {selected && (
        <NodePanel
          tree={tree}
          nodeID={selected}
          onTreeChange={onTreeChange}
          onSaved={onSaved}
          onClose={() => onSelect(null)}
          testSetID={testSetID}
        />
      )}
    </div>
  )
}
