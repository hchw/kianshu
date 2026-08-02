import { useRef, useState } from 'react'
import { apiError } from '../../api/client'
import { importSwagger } from '../../api/testset'

interface Props {
  testSetID: number
  onImported: () => void
}

export default function ImportPanel({ testSetID, onImported }: Props) {
  const [source, setSource] = useState('')
  const [content, setContent] = useState('')
  const [err, setErr] = useState('')
  const [issues, setIssues] = useState<string[]>([])
  const [needConf, setNeedConf] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const doImport = async (confirm = false) => {
    setErr('')
    setNeedConf(false)
    setIssues([])
    try {
      await importSwagger(testSetID, source || 'manual', content, confirm)
      setContent('')
      onImported()
    } catch (e) {
      const resp = (e as { response?: { data?: { need_confirmation?: boolean; issues?: string[]; error?: string } } }).response
      if (resp?.data?.need_confirmation) {
        setNeedConf(true)
        setIssues(resp.data.issues ?? [])
      }
      setErr(resp?.data?.error ?? apiError(e))
    }
  }

  const onFile = (f: File | null) => {
    if (!f) return
    const reader = new FileReader()
    reader.onload = () => setContent(String(reader.result ?? ''))
    reader.readAsText(f)
  }

  return (
    <div className="card stack">
      <h3>导入 Swagger / OpenAPI</h3>
      <div className="row tight">
        <input placeholder="来源标识(如 petstore)" value={source} onChange={(e) => setSource(e.target.value)} />
        <button onClick={() => fileRef.current?.click()}>选择文件</button>
        <input
          ref={fileRef}
          type="file"
          accept=".json,.yaml,.yml"
          style={{ display: 'none' }}
          onChange={(e) => onFile(e.target.files?.[0] ?? null)}
        />
      </div>
      <textarea
        rows={10}
        placeholder="粘贴 Swagger JSON…"
        value={content}
        onChange={(e) => setContent(e.target.value)}
      />
      <button onClick={() => doImport()} disabled={!content.trim()}>
        导入
      </button>
      {needConf && issues.length > 0 && (
        <div className="stack">
          <p className="err">文档不完全标准,确认后将继续导入:</p>
          <ul style={{ margin: 0, paddingLeft: '1.2em' }}>
            {issues.map((r, i) => <li key={i}>{r}</li>)}
          </ul>
          <button onClick={() => doImport(true)}>仍然导入</button>
        </div>
      )}
      {err && !needConf && <p className="err">{err}</p>}
    </div>
  )
}
