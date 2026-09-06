import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import * as Tabs from '@radix-ui/react-tabs'
import AppLayout from '../components/layout/AppLayout'
import PopConfirm from '../components/dialog/PopConfirm'
import { apiError } from '../api/client'
import { useToast } from '../components/feedback/Toast'
import { listTestSets, listFlows, type TestSet, type FlowSummary } from '../api/testset'
import { deleteFlow, renameFlow } from '../api/flow'
import { listCaseFlows, renameCaseFlow, deleteCaseFlow, type CaseFlow } from '../api/caseFlow'

type Group = { testSet: TestSet; caseFlows: CaseFlow[]; flows: FlowSummary[] }

/** 单条流：改名（行内编辑）+ 删除（确认弹窗）+ 打开编辑器。 */
function FlowRow({ name, url, openLabel, editing, onEditStart, onCommitRename, onDelete }: {
  name: string
  url: string
  openLabel: string
  editing: boolean
  onEditStart: () => void
  onCommitRename: (name: string) => Promise<void>
  onDelete: () => Promise<void>
}) {
  const [draft, setDraft] = useState(name)
  useEffect(() => { if (editing) setDraft(name) }, [editing, name])
  if (editing) {
    return (
      <form className="row" onSubmit={(e) => { e.preventDefault(); void onCommitRename(draft.trim()) }}>
        <input value={draft} onChange={(e) => setDraft(e.target.value)} autoFocus />
        <button type="submit" className="primary">保存</button>
        <button type="button" className="link" onClick={onEditStart}>取消</button>
      </form>
    )
  }
  return (
    <div className="card item row">
      <Link to={url}><span className="strong">{name}</span></Link>
      <span style={{ flex: 1 }} />
      <button className="link" onClick={() => { setDraft(name); onEditStart() }}>改名</button>
      <PopConfirm danger message={`确认删除「${name}」？该操作不可恢复。`} onConfirm={() => onDelete()}>
        <button className="link danger">删除</button>
      </PopConfirm>
      <Link className="link" to={url}>{openLabel} →</Link>
    </div>
  )
}

function GroupCard({ testSet, items, emptyText, editingKey, setEditingKey, itemURL, openLabel, commitRename, remove }: {
  testSet: TestSet
  items: { id: number; name: string }[]
  emptyText: string
  editingKey: string | null
  setEditingKey: (k: string | null) => void
  itemURL: (id: number) => string
  openLabel: string
  commitRename: (id: number, name: string) => Promise<void>
  remove: (id: number) => Promise<void>
}) {
  return (
    <section className="card">
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <Link to={`/test-sets/${testSet.id}`}><strong>{testSet.name}</strong></Link>
        <span className="muted">{items.length} 个</span>
      </div>
      {items.length === 0 ? <div className="muted">{emptyText}</div> : (
        <div className="list">
          {items.map((f) => {
            const key = `${testSet.id}-${f.id}`
            return (
              <FlowRow
                key={key}
                name={f.name}
                url={itemURL(f.id)}
                openLabel={openLabel}
                editing={editingKey === key}
                onEditStart={() => setEditingKey(editingKey === key ? null : key)}
                onCommitRename={async (name) => { setEditingKey(null); await commitRename(f.id, name) }}
                onDelete={() => remove(f.id)}
              />
            )
          })}
        </div>
      )}
    </section>
  )
}

/** 用例总览：两个页签（Radix Tabs）— 用例流 / 执行流，按测试集分组，支持改名与删除。 */
export default function Cases() {
  const toast = useToast()
  const [groups, setGroups] = useState<Group[]>([])
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const load = async () => {
    try {
      const sets: TestSet[] = await listTestSets()
      const settled = await Promise.all(sets.map(async (testSet) => {
        const [caseFlows, flows] = await Promise.all([
          listCaseFlows(testSet.id).catch(() => [] as CaseFlow[]),
          listFlows(testSet.id).catch(() => [] as FlowSummary[]),
        ])
        return { testSet, caseFlows, flows }
      }))
      setGroups(settled)
      setErr('')
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const renameCase = async (id: number, name: string) => { await renameCaseFlow(id, name) }
  const removeCaseFlow = async (id: number) => { await deleteCaseFlow(id) }
  const renameFlowByID = async (id: number, name: string) => { await renameFlow(id, name) }
  const removeFlow = async (id: number) => { await deleteFlow(id) }

  const keyword = q.trim().toLowerCase()
  const visible = groups
    .map((g) => ({ ...g, caseFlows: g.caseFlows.filter((f) => !keyword || f.name.toLowerCase().includes(keyword)), flows: g.flows.filter((f) => !keyword || f.name.toLowerCase().includes(keyword)) }))
    .filter((g) => !keyword || g.caseFlows.length > 0 || g.flows.length > 0 || g.testSet.name.toLowerCase().includes(keyword))

  const totalCaseFlows = groups.reduce((sum, g) => sum + g.caseFlows.length, 0)
  const totalFlows = groups.reduce((sum, g) => sum + g.flows.length, 0)

  if (err) return <AppLayout title="用例总览"><div className="err">{err}</div></AppLayout>

  return (
    <AppLayout title="用例总览">
      <Tabs.Root defaultValue="cases">
        <Tabs.List className="tabs-list" aria-label="用例总览页签">
          <Tabs.Trigger value="cases" className="tabs-trigger">用例流（{totalCaseFlows}）</Tabs.Trigger>
          <Tabs.Trigger value="flows" className="tabs-trigger">执行流（{totalFlows}）</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="cases" className="tabs-content stack">
          <div className="row"><input placeholder="搜索用例流 / 测试集" value={q} onChange={(e) => setQ(e.target.value)} /></div>
          {visible.map((g) => (
            <GroupCard key={g.testSet.id} testSet={g.testSet} items={g.caseFlows} emptyText="该测试集暂无用例流" editingKey={editingKey} setEditingKey={setEditingKey} itemURL={(id) => `/case-flows/${id}`} openLabel="打开用例流编辑器" commitRename={renameCase} remove={removeCaseFlow} />
          ))}
          {visible.length === 0 && <div className="muted">没有匹配的测试集</div>}
        </Tabs.Content>
        <Tabs.Content value="flows" className="tabs-content stack">
          <div className="row"><input placeholder="搜索执行流 / 测试集" value={q} onChange={(e) => setQ(e.target.value)} /></div>
          {visible.map((g) => (
            <GroupCard key={g.testSet.id} testSet={g.testSet} items={g.flows} emptyText="该测试集暂无执行流" editingKey={editingKey} setEditingKey={setEditingKey} itemURL={(id) => `/flows/${id}`} openLabel="打开执行流编辑器" commitRename={renameFlowByID} remove={removeFlow} />
          ))}
          {visible.length === 0 && <div className="muted">没有匹配的测试集</div>}
        </Tabs.Content>
      </Tabs.Root>
      {loading && <div className="muted">加载中…</div>}
    </AppLayout>
  )
}
