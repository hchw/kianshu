import type { CaseNode } from '../api/caseFlow'

/** Ancestor ids from the root down to (excluding) the node with `id`. */
export function caseAncestorIDs(root: CaseNode, id: string): string[] {
  const trail: string[] = []
  const walk = (n: CaseNode, acc: string[]): boolean => {
    if (n.id === id) {
      trail.push(...acc)
      return true
    }
    for (const c of n.children ?? []) {
      if (walk(c, [...acc, n.id])) return true
    }
    return false
  }
  walk(root, [])
  return trail
}

/** All descendants of a node, including itself. */
export function caseDescendants(root: CaseNode, id: string): CaseNode[] {
  const collect = (n: CaseNode, acc: CaseNode[]) => {
    acc.push(n)
    for (const c of n.children ?? []) collect(c, acc)
  }
  const found: CaseNode[] = []
  const walk = (n: CaseNode) => {
    if (n.id === id) {
      collect(n, found)
      return
    }
    for (const c of n.children ?? []) walk(c)
  }
  walk(root)
  return found
}

/**
 * Collapses a multi-selection to subtree roots (a selected node whose ancestor
 * is also selected is suppressed) and counts the distinct nodes covered.
 */
export function dedupeSubtreeRoots(root: CaseNode | null, ids: string[]): { roots: string[]; nodes: number } {
  if (!root || ids.length === 0) return { roots: [], nodes: 0 }
  const selected = new Set(ids)
  const roots = ids.filter((id) => !caseAncestorIDs(root, id).some((a) => selected.has(a)))
  const seen = new Set<string>()
  for (const r of roots) {
    for (const n of caseDescendants(root, r)) seen.add(n.id)
  }
  return { roots, nodes: seen.size }
}
