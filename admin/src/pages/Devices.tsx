import { useEffect, useState } from 'react'
import { api } from '../api'

type Device = {
  id: number
  user_id: number
  device_uid: string
  name: string
  os_version: string
  status: number
  last_ip: string
  last_seen_at?: string
  user?: { username: string }
}

type Page = { items: Device[]; total: number }

export default function Devices() {
  const [online, setOnline] = useState('')
  const [data, setData] = useState<Page | null>(null)
  const [error, setError] = useState('')

  async function load() {
    try {
      const qs = new URLSearchParams({ page: '1', page_size: '50' })
      if (online !== '') qs.set('online', online)
      const d = await api<Page>(`/api/v1/admin/devices?${qs}`)
      setData(d)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }

  useEffect(() => { load() }, [online])

  async function remove(id: number) {
    if (!confirm('确认解绑删除该设备？')) return
    await api(`/api/v1/admin/devices/${id}`, { method: 'DELETE' })
    load()
  }

  return (
    <div>
      <h1 className="page-title">设备</h1>
      <div className="toolbar">
        <select className="input" style={{ maxWidth: 160 }} value={online} onChange={(e) => setOnline(e.target.value)}>
          <option value="">全部状态</option>
          <option value="1">在线</option>
          <option value="0">离线</option>
        </select>
        <button className="btn" onClick={load}>刷新</button>
        <div className="spacer" />
        <span className="muted">共 {data?.total ?? 0} 台</span>
      </div>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>UID</th>
              <th>归属用户</th>
              <th>系统</th>
              <th>状态</th>
              <th>IP</th>
              <th>最后心跳</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(data?.items ?? []).map((d) => (
              <tr key={d.id}>
                <td>{d.name}</td>
                <td className="mono">{d.device_uid}</td>
                <td>{d.user?.username ?? d.user_id}</td>
                <td className="muted">{d.os_version}</td>
                <td>
                  <span className={`dot ${d.status === 1 ? 'online' : 'offline'}`} />
                  {d.status === 1 ? '在线' : '离线'}
                </td>
                <td className="mono">{d.last_ip || '—'}</td>
                <td className="muted">{d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : '—'}</td>
                <td>
                  <button className="btn danger" onClick={() => remove(d.id)}>解绑</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
