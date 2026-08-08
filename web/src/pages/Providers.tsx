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
import { BusyButton } from '../components/feedback/BusyButton'
import { SkeletonList } from '../components/feedback/Skeleton'
import { EmptyState } from '../components/feedback/EmptyState'
import { ErrorNote } from '../components/feedback/ErrorNote'
import { useToast } from '../components/feedback/Toast'
import { Plug } from 'lucide-react'

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
  const toast = useToast()
  const [providers, setProviders] = useState<Provider[]>([])
  const [form, setForm] = useState<FormState>(emptyForm)
  const [editing, setEditing] = useState<number | null>(null)
  const [testResult, setTestResult] = useState<Record<number, TestResult>>({})
  const [testing, setTesting] = useState<number | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setProviders((await listProviders()) || [])
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setLoading(false)
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
      const msg = '名称与 base_url 必填'
      setErr(msg)
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
        toast.success('Provider 已更新')
      } else {
        await createProvider(req)
        toast.success('Provider 已创建')
      }
      cancelEdit()
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (p: Provider) => {
    if (!confirm(`删除 Provider "${p.name}"?`)) return
    setErr('')
    try {
      await deleteProvider(p.id)
      toast.success('Provider 已删除')
      await load()
    } catch (e) {
      const msg = apiError(e)
      setErr(msg)
      toast.error(msg)
    }
  }

  const test = async (p: Provider) => {
    setTesting(p.id)
    setErr('')
    try {
      const res = await testProvider(p.id)
      setTestResult((cur) => ({ ...cur, [p.id]: res }))
    } catch (e) {
      const msg = apiError(e)
      setTestResult((cur) => ({ ...cur, [p.id]: { ok: false, error: msg } }))
      toast.error(msg)
    } finally {
      setTesting(null)
    }
  }

  return (
    <AppLayout title="Provider">
      <h1>LLM Provider</h1>
      {err && <ErrorNote>{err}</ErrorNote>}

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
          <BusyButton type="submit" className="primary" busy={busy}>
            {busy ? '保存中…' : editing !== null ? '保存修改' : '创建'}
          </BusyButton>
          {editing !== null && (
            <button type="button" className="link" onClick={cancelEdit}>
              取消
            </button>
          )}
        </div>
      </form>

      <div className="list">
        {loading ? (
          <SkeletonList count={3} />
        ) : providers.length === 0 ? (
          <EmptyState
            icon={<Plug size={28} strokeWidth={1.5} aria-hidden="true" />}
            title="还没有 Provider"
            hint="在上方表单配置第一个吧"
          />
        ) : (
          providers.map((p) => (
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
                <BusyButton className="ghost" onClick={() => test(p)} busy={testing === p.id}>
                  {testing === p.id ? '测试中…' : '测试'}
                </BusyButton>
                <button className="ghost" onClick={() => startEdit(p)}>
                  编辑
                </button>
                <button className="link danger" onClick={() => remove(p)}>
                  删除
                </button>
              </div>
            </div>
          ))
        )}
      </div>
    </AppLayout>
  )
}
