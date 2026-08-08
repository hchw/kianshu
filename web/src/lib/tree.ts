import type { FlowTree, FlowNode } from '../api/flow'
import type { AgentEvent } from '../api/agent'

export interface LayoutPosition {
  x: number
  y: number
  layer: number
}

export interface LayoutResult {
  positions: Map<string, LayoutPosition>
  ordered: string[]
}

const W = 260
const H = 120

export function parseTree(json: string | null | undefined): FlowTree {
  if (!json) return { start: '', nodes: {} }
  try {
    return JSON.parse(json) as FlowTree
  } catch {
    return { start: '', nodes: {} }
  }
}

// layoutTree places nodes on a level grid: level-order traversal assigns each
// node x = layer*W and y = ordinal-in-layer*H. Nodes with explicit x/y
// coordinates (user-placed) keep them and consume no grid slot. cache-set
// nodes are laid out in a side column and never linked.
export function layoutTree(tree: FlowTree): LayoutResult {
  const positions = new Map<string, LayoutPosition>()
  const ordered: string[] = []
  const isCache = new Set(tree.cacheSets ?? [])

  const inLayer: string[] = []
  const visited = new Set<string>()
  const counts = new Map<number, number>()
  const visit = (id: string, layer: number) => {
    if (isCache.has(id) || visited.has(id)) return
    visited.add(id)
    ordered.push(id)
    inLayer.push(id)
    const n = tree.nodes[id]
    if (n && n.x != null && n.y != null) {
      positions.set(id, { x: n.x, y: n.y, layer })
    } else {
      positions.set(id, { x: layer * W, y: (counts.get(layer) ?? 0) * H, layer })
      counts.set(layer, (counts.get(layer) ?? 0) + 1)
    }
  }

  visit(tree.start, 0)
  for (let i = 0; i < inLayer.length; i++) {
    const n = tree.nodes[inLayer[i]]
    const layer = positions.get(inLayer[i])!.layer
    for (const c of n?.children ?? []) {
      visit(c, layer + 1)
    }
  }

  let side = 0
  for (const id of isCache) {
    const n = tree.nodes[id]
    if (n && n.x != null && n.y != null) {
      positions.set(id, { x: n.x, y: n.y, layer: -1 })
    } else {
      positions.set(id, { x: (maxLayer(positions) + 1) * W, y: side * H, layer: -1 })
      side++
    }
    ordered.push(id)
  }
  return { positions, ordered }
}

function maxLayer(positions: Map<string, LayoutPosition>): number {
  let m = 0
  for (const p of positions.values()) {
    if (p.layer > m) m = p.layer
  }
  return m
}

// collectSubtree gathers id plus every descendant reachable through children.
function collectSubtree(tree: FlowTree, id: string): Set<string> {
  const seen = new Set<string>()
  const stack = [id]
  while (stack.length) {
    const cur = stack.pop()!
    if (seen.has(cur)) continue
    seen.add(cur)
    for (const c of tree.nodes[cur]?.children ?? []) stack.push(c)
  }
  return seen
}

// deleteNode removes a node and its whole subtree from the tree, and drops the
// reference from its parent's children. The start node is protected: deleting
// it would break the single-root invariant, so the tree is returned unchanged.
export function deleteNode(tree: FlowTree, id: string): FlowTree {
  if (tree.start === id) return tree
  const n = tree.nodes[id]
  if (!n) return tree
  const gone = collectSubtree(tree, id)
  const nodes: Record<string, FlowNode> = {}
  for (const [k, v] of Object.entries(tree.nodes)) {
    if (gone.has(k)) continue
    nodes[k] = { ...v }
  }
  if (n.parent && nodes[n.parent]) {
    nodes[n.parent] = {
      ...nodes[n.parent],
      children: (nodes[n.parent].children ?? []).filter((c) => c !== id),
    }
  }
  return { ...tree, nodes }
}

// reconcileChildren rebuilds children lists from parent pointers, making the
// parent encoding authoritative exactly like the backend's tree.reconcileChildren.
export function reconcileChildren(tree: FlowTree): FlowTree {
  for (const n of Object.values(tree.nodes)) {
    if (n) n.children = []
  }
  for (const [id, n] of Object.entries(tree.nodes)) {
    if (!n || !n.parent) continue
    const p = tree.nodes[n.parent]
    if (!p) continue
    if (!p.children!.includes(id)) p.children!.push(id)
  }
  return tree
}

export interface TreeError {
  node_id?: string
  code: string
  message: string
}

// validateTreeShape mirrors the backend's ValidateTreeShape rules.
export function validateTreeShape(tree: FlowTree): TreeError[] {
  reconcileChildren(tree)
  const errs: TreeError[] = []
  const isCache = new Set(tree.cacheSets ?? [])
  const nodes = tree.nodes ?? {}

  if (!nodes[tree.start] || !nodes[tree.start]!.id) {
    return [{ code: 'tree.start_missing', message: '缺少 start 根节点' }]
  }
  for (const [id, n] of Object.entries(nodes)) {
    if (!n) {
      errs.push({ node_id: id, code: 'node.nil', message: '节点为空' })
      continue
    }
    if (id === tree.start) continue
    if (isCache.has(id)) {
      if (n.parent || (n.children && n.children.length > 0)) {
        errs.push({ node_id: id, code: 'cache.linked', message: 'cache-set 节点不得参与树连线' })
      }
      continue
    }
    if (!n.parent) {
      errs.push({ node_id: id, code: 'tree.no_parent', message: '非 start 节点缺少父节点' })
      continue
    }
    if (!nodes[n.parent]) {
      errs.push({ node_id: id, code: 'node.parent_missing', message: '父节点不存在: ' + n.parent })
    }
  }

  const WHITE = 0
  const GREY = 1
  const BLACK = 2
  const color = new Map<string, number>()
  const stack: string[] = [tree.start]
  while (stack.length > 0) {
    const id = stack[stack.length - 1]
    if ((color.get(id) ?? WHITE) === WHITE) {
      color.set(id, GREY)
      const n = nodes[id]
      for (const c of n?.children ?? []) {
        if (color.get(c) === GREY) {
          errs.push({ node_id: c, code: 'tree.cycle', message: '检测到环路' })
          continue
        }
        if ((color.get(c) ?? WHITE) === WHITE) stack.push(c)
      }
    } else {
      color.set(id, BLACK)
      stack.pop()
    }
  }
  for (const id of Object.keys(nodes)) {
    if (color.get(id) !== BLACK && !isCache.has(id)) {
      errs.push({ node_id: id, code: 'tree.disconnected', message: '节点不属于 start 根树' })
    }
  }
  return errs
}

// linkRules reports whether a parent-child link is structurally allowed:
// no self link, cache-set never linked, no cycle, and every node has one parent.
export function linkAllowed(tree: FlowTree, parent: string, child: string): TreeError[] {
  const errs: TreeError[] = []
  if (!parent || !child || parent === child) {
    errs.push({ code: 'link.self', message: '不能连接自身' })
    return errs
  }
  const nodes = tree.nodes ?? {}
  const isCache = new Set(tree.cacheSets ?? [])
  if (isCache.has(parent) || isCache.has(child)) {
    errs.push({ code: 'cache.linked', message: 'cache-set 节点不得参与连线' })
    return errs
  }
  if (!nodes[parent] || !nodes[child]) {
    errs.push({ code: 'link.missing', message: '节点不存在' })
    return errs
  }
  if (nodes[child].parent && nodes[child].parent !== parent) {
    errs.push({ code: 'link.multi_parent', message: '每个节点只能有一个父节点' })
    return errs
  }
  const candidate: FlowTree = JSON.parse(JSON.stringify(tree))
  candidate.nodes[child].parent = parent
  reconcileChildren(candidate)
  for (const e of validateTreeShape(candidate)) {
    if (e.code === 'tree.cycle') errs.push(e)
  }
  return errs
}

export const NODE_LABELS: Record<string, string> = {
  start: '启动',
  api: 'API',
  assert: '断言',
  loop: '循环',
  try: 'try',
  catch: 'catch',
  'cache-set': '缓存',
  adapter: '转换',
}

// applyToolMutation replays a single agent tool event on a flow tree,
// returning a new tree with the mutation applied (or the original tree
// unchanged for read-only tools / non-mutating events).
export function applyToolMutation(tree: FlowTree, ev: AgentEvent): FlowTree {
  if (!ev.tool || !ev.result || typeof ev.result !== 'object') return tree

  const result = ev.result as { ok?: boolean; data?: unknown; error?: string }
  if (!result.ok || !result.data) return tree

  switch (ev.tool) {
    case 'create_node': {
      const node = result.data as FlowNode
      if (!node.id) return tree
      const next: FlowTree = {
        ...tree,
        nodes: { ...tree.nodes, [node.id]: { ...node } },
      }
      // 若节点已有 parent（后端 AddChild 已设置），同步更新父的 children
      if (node.parent && next.nodes[node.parent]) {
        const parent = next.nodes[node.parent]
        const existing = parent.children ?? []
        if (!existing.includes(node.id)) {
          next.nodes[node.parent] = { ...parent, children: [...existing, node.id] }
        }
      }
      // cache-set 类型额外加入 cacheSets
      if (node.type === 'cache-set') {
        const cs = new Set(tree.cacheSets ?? [])
        cs.add(node.id)
        next.cacheSets = [...cs]
      }
      return next
    }
    case 'update_node': {
      const node = result.data as FlowNode
      if (!node.id || !tree.nodes[node.id]) return tree
      return {
        ...tree,
        nodes: { ...tree.nodes, [node.id]: { ...node } },
      }
    }
    case 'delete_node': {
      const data = result.data as { deleted?: string }
      if (!data.deleted) return tree
      return deleteNode(tree, data.deleted)
    }
    case 'link_nodes': {
      const data = result.data as { linked?: string[] }
      if (!data.linked || data.linked.length !== 2) return tree
      const [parentId, childId] = data.linked
      if (!tree.nodes[parentId] || !tree.nodes[childId]) return tree
      const next: FlowTree = {
        ...tree,
        nodes: { ...tree.nodes },
      }
      next.nodes[childId] = { ...next.nodes[childId], parent: parentId }
      const parent = next.nodes[parentId]
      const existing = parent.children ?? []
      if (!existing.includes(childId)) {
        next.nodes[parentId] = { ...parent, children: [...existing, childId] }
      }
      return next
    }
    default:
      // 只读工具（get_flow, list_units, filter_units, validate_flow）不变异
      return tree
  }
}
