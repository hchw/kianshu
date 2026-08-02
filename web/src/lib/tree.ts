import type { FlowTree } from '../api/flow'

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
// node x = layer*W and y = ordinal-in-layer*H. cache-set nodes are laid out in
// a side column and never linked.
export function layoutTree(tree: FlowTree): LayoutResult {
  const positions = new Map<string, LayoutPosition>()
  const ordered: string[] = []
  const isCache = new Set(tree.cacheSets ?? [])

  const inLayer: string[] = []
  const counts = new Map<number, number>()
  const visit = (id: string, layer: number) => {
    if (isCache.has(id) || positions.has(id)) return
    positions.set(id, { x: layer * W, y: (counts.get(layer) ?? 0) * H, layer })
    counts.set(layer, (counts.get(layer) ?? 0) + 1)
    ordered.push(id)
    inLayer.push(id)
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
    positions.set(id, { x: (maxLayer(positions) + 1) * W, y: side * H, layer: -1 })
    side++
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
