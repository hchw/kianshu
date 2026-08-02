import { useCallback, useEffect, useState } from 'react'
import { apiError } from '../api/client'
import {
  createProvider,
  deleteProvider,
  listProviders,
  testProvider,
  updateProvider,
  type Provider,
} from '../api/providers'
import AppLayout from '../components/layout/AppLayout'

interface FormState {
  name: string
  base_url: string
  api_key: string
  model: string
}

const emptyForm: FormState = { name: '', base_url: '', api_key: '', model: '' }

interface TestResult {
  ok: boolean
  error?: string
}

export default function Providers() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [form, setForm] = useState<FormState>(emptyForm)
  const [editing, setEditing] = useState<number | null>(null)
  const [testResult, setTestResult] = useState<Record<number, TestResult>>({})
  const [testing, setTesting] = useState<number | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setProviders((await listProviders()) || [])
    } catch (e) {
      setErr(apiError(e))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const startEdit = (p: Provider) => {
    setEditing(p.id)
    setForm({ name: p.name, base_url: p.base_url, api_key: '', model: p.model })
    setErr('')
  }

  const cancelEdit = () => {
    setEditing(null)
    setForm(emptyForm)
    setErr('')
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    if (!form.name.trim() || !form.base_url.trim()) {
      setErr('名称与 base_url 必填')
      return
    }
    setBusy(true)
    try {
      const req = {
        name: form.name.trim(),
        base_url: form.base_url.trim(),
        api_key: form.api_key,
        model: form.model.trim() || undefined,
      }
      if (editing !== null) {
        await updateProvider(editing, req)
      } else {
        await createProvider(req)
      }
      cancelEdit()
      await load()
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }

  const remove = async (p: Provider) => {
    if (!confirm(`删除 Provider "${p.name}"?`)) return
    setErr('')
    try {
      await deleteProvider(p.id)
      await load()
    } catch (e) {
      setErr(apiError(e))
    }
  }

  const test = async (p: Provider) => {
    setTesting(p.id)
    setErr('')
    try {
      const res = await testProvider(p.id)
      setTestResult((cur) => ({ ...cur, [p.id]: res }))
    } catch (e) {
      setTestResult((cur) => ({ ...cur, [p.id]: { ok: false, error: apiError(e) } }))
    } finally {
      setTesting(null)
    }
  }

  return (
    <AppLayout title="Provider">
      <h1>LLM Provider</h1>
      {err && <p className="err">{err}</p>}

      <form className="card" onSubmit={submit}>
        <div className="row tight">
          <input
            placeholder="名称"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <input
            placeholder="Base URL"
            value={form.base_url}
            onChange={(e) => setForm({ ...form, base_url: e.target.value })}
          />
          <input
            placeholder="API Key"
            type="password"
            value={form.api_key}
            onChange={(e) => setForm({ ...form, api_key: e.target.value })}
          />
          <input
            placeholder="模型(可选)"
            value={form.model}
            onChange={(e) => setForm({ ...form, model: e.target.value })}
          />
        </div>
        <div className="row">
          <button type="submit" disabled={busy}>
            {busy ? '保存中…' : editing !== null ? '保存修改' : '创建'}
          </button>
          {editing !== null && (
            <button type="button" className="link" onClick={cancelEdit}>
              取消
            </button>
          )}
        </div>
      </form>

      <div className="list">
        {providers.map((p) => (
          <div key={p.id} className="card item">
            <div>
              <span className="strong">{p.name}</span>
              <div className="muted">
                {p.base_url} · {p.model || '无模型'} · {p.enabled ? '启用' : '停用'}
              </div>
              {testResult[p.id] && (
                <div className={testResult[p.id].ok ? 'muted' : 'err'}>
                  {testResult[p.id].ok ? '连通成功' : `测试失败: ${testResult[p.id].error}`}
                </div>
              )}
            </div>
            <div className="row tight">
              <button onClick={() => test(p)} disabled={testing === p.id}>
                {testing === p.id ? '测试中…' : '测试'}
              </button>
              <button onClick={() => startEdit(p)}>编辑</button>
              <button className="link danger" onClick={() => remove(p)}>
                删除
              </button>
            </div>
          </div>
        ))}
        {providers.length === 0 && <p className="muted">还没有 Provider,配置第一个吧</p>}
      </div>
    </AppLayout>
  )
}
