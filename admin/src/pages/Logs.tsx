import { useEffect, useState } from 'react'
import { api } from '../api'

type Log = {
  id: number
  actor_type: string
  actor_id?: number
  action: string
  resource_type: string
  resource_id?: number
  ip: string
  detail?: unknown
  created_at: string
}

type Page = { items: Log[]; total: number }

export default function Logs() {
  const [action, setAction] = useState('')
  const [data, setData] = useState<Page | null>(null)
  const [detail, setDetail] = useState<Log | null>(null)
  const [error, setError] = useState('')

  async function load() {
    try {
      const qs = new URLSearchParams({ page: '1', page_size: '50' })
      if (action) qs.set('action', action)
      const d = await api<Page>(`/api/v1/admin/logs?${qs}`)
      setData(d)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }

  useEffect(() => { load() }, [])

  return (
    <div>
      <h1 className="page-title">日志</h1>
      <div className="toolbar">
        <input className="input" style={{ maxWidth: 220 }} placeholder="动作筛选，如 login" value={action} onChange={(e) => setAction(e.target.value)} />
        <button className="btn" onClick={load}>筛选</button>
        <div className="spacer" />
        <span className="muted">共 {data?.total ?? 0} 条</span>
      </div>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>操作者</th>
              <th>动作</th>
              <th>资源</th>
              <th>IP</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(data?.items ?? []).map((l) => (
              <tr key={l.id}>
                <td className="muted">{new Date(l.created_at).toLocaleString()}</td>
                <td className="mono">{l.actor_type}{l.actor_id ? `#${l.actor_id}` : ''}</td>
                <td>{l.action}</td>
                <td className="mono">{l.resource_type || '—'}{l.resource_id ? `#${l.resource_id}` : ''}</td>
                <td className="mono">{l.ip || '—'}</td>
                <td>
                  <button className="btn" onClick={() => setDetail(l)}>详情</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {detail && (
        <div className="table-wrap" style={{ marginTop: '1rem', padding: '1rem' }}>
          <div className="toolbar">
            <strong>日志 #{detail.id}</strong>
            <div className="spacer" />
            <button className="btn" onClick={() => setDetail(null)}>关闭</button>
          </div>
          <pre className="mono" style={{ whiteSpace: 'pre-wrap', margin: 0, color: 'var(--text-muted)' }}>
            {JSON.stringify(detail.detail ?? {}, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}
