import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  createBackgroundDoc,
  createCaseFlow,
  deleteBackgroundDoc,
  deleteCaseFlow,
  listBackgroundDocs,
  listCaseFlows,
  renameCaseFlow,
  updateBackgroundDoc,
  type BackgroundDocument,
  type CaseFlow,
} from '../../api/caseFlow'
import { apiError } from '../../api/client'
import { useToast } from '../feedback/Toast'
import { EmptyState } from '../feedback/EmptyState'
import PopConfirm from '../dialog/PopConfirm'

export default function CaseFlowPanel({ testSetID }: { testSetID: number }) {
  const nav = useNavigate()
  const toast = useToast()
  const [docs, setDocs] = useState<BackgroundDocument[]>([])
  const [caseFlows, setCaseFlows] = useState<CaseFlow[]>([])
  const [name, setName] = useState('')
  const [content, setContent] = useState('')
  const [cfName, setCfName] = useState('')
  const [editingDocID, setEditingDocID] = useState<number | null>(null)
  const [editingCfID, setEditingCfID] = useState<number | null>(null)
  const [editingCfName, setEditingCfName] = useState('')
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    try {
      setDocs(await listBackgroundDocs(testSetID))
      setCaseFlows(await listCaseFlows(testSetID))
    } catch (e) {
      setErr(apiError(e))
    }
  }, [testSetID])

  useEffect(() => {
    void load()
  }, [load])

  const addDoc = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !content.trim()) return
    try {
      await createBackgroundDoc(testSetID, name.trim(), content.trim())
      setName('')
      setContent('')
      toast.success('背景文档已创建')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const addCaseFlow = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!cfName.trim()) return
    try {
      const cf = await createCaseFlow(testSetID, cfName.trim(), [{ kind: 'all' }])
      toast.success('用例流已创建')
      nav(`/case-flows/${cf.id}`)
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const startEditDoc = (doc: BackgroundDocument) => {
    setEditingDocID(doc.id)
    setName(doc.name)
    setContent(doc.content)
  }

  const saveDoc = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingDocID || !name.trim() || !content.trim()) return
    try {
      await updateBackgroundDoc(testSetID, editingDocID, { name: name.trim(), content: content.trim() })
      setEditingDocID(null)
      setName('')
      setContent('')
      toast.success('背景文档已保存')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const removeDoc = async (doc: BackgroundDocument) => {
    try {
      await deleteBackgroundDoc(testSetID, doc.id)
      toast.success('背景文档已删除')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const saveCfRename = async (cf: CaseFlow) => {
    const trimmed = editingCfName.trim()
    if (!trimmed) return
    try {
      await renameCaseFlow(cf.id, trimmed)
      toast.success('用例流已重命名')
      setEditingCfID(null)
      await load()
    } catch (e) { setErr(apiError(e)) }
  }
  const removeCf = async (cf: CaseFlow) => {
    try {
      await deleteCaseFlow(cf.id)
      toast.success('用例流已删除')
      await load()
    } catch (e) { setErr(apiError(e)) }
  }
  return (
    <div className="stack">
      {err && <div className="err">{err}</div>}
      <div className="card">
        <h3>背景文档</h3>
        <form className="stack" onSubmit={editingDocID ? saveDoc : addDoc}>
          <input placeholder="文档名称" value={name} onChange={(e) => setName(e.target.value)} />
          <textarea placeholder="文档内容" value={content} onChange={(e) => setContent(e.target.value)} />
          <button type="submit" className="primary">{editingDocID ? '保存文档' : '创建文档'}</button>
          {editingDocID && <button className="link" onClick={() => { setEditingDocID(null); setName(''); setContent('') }}>取消编辑</button>}
        </form>
        <div className="list">
          {docs.length === 0 ? (
            <EmptyState compact title="还没有背景文档" hint="创建后可作为用例生成来源" />
          ) : (
            docs.map((d) => (
              <div key={d.id} className="card item row">
                <span className="strong">{d.name}</span>
                <button className="link" onClick={() => startEditDoc(d)}>编辑</button>
                <button className="link danger" onClick={() => removeDoc(d)}>删除</button>
              </div>
            ))
          )}
        </div>
      </div>
      <div className="card">
        <h3>用例流</h3>
        <form className="row" onSubmit={addCaseFlow}>
          <input placeholder="新用例流名称" value={cfName} onChange={(e) => setCfName(e.target.value)} />
          <button type="submit" className="primary">创建</button>
        </form>
        <div className="list">
          {caseFlows.map((cf) => (
                  <div key={cf.id} className="card item row">
                    {editingCfID === cf.id ? (
                      <form className="row" onSubmit={(e) => { e.preventDefault(); void saveCfRename(cf) }}>
                        <input value={editingCfName} onChange={(e) => setEditingCfName(e.target.value)} autoFocus />
                        <button type="submit" className="primary">保存</button>
                        <button type="button" className="link" onClick={() => setEditingCfID(null)}>取消</button>
                      </form>
                    ) : (
                      <>
                        <span className="strong" style={{ cursor: 'pointer' }} onClick={() => nav(`/case-flows/${cf.id}`)}>{cf.name}</span>
                        <button className="link" onClick={() => { setEditingCfID(cf.id); setEditingCfName(cf.name) }}>改名</button>
                        <PopConfirm danger message={`确认删除用例流「${cf.name}」？该操作不可恢复。`} onConfirm={() => void removeCf(cf)}>
                          <button className="link danger">删除</button>
                        </PopConfirm>
                        <button className="link" onClick={() => nav(`/case-flows/${cf.id}`)}>打开编辑器 →</button>
                      </>
                    )}
                  </div>
                ))}
        </div>
      </div>
    </div>
  )
}
