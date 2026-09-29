import { FormEvent, useEffect, useState } from 'react'
import { api } from '../api'

type UserRow = {
  id: number
  username: string
  display_name: string
  status: number
  created_at: string
  device_count: number
}

type Page = { items: UserRow[]; total: number; page: number; page_size: number }

const emptyForm = { username: '', display_name: '', password: '' }

export default function Users() {
  const [q, setQ] = useState('')
  const [data, setData] = useState<Page | null>(null)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState(emptyForm)
  const [saving, setSaving] = useState(false)
  const [busyId, setBusyId] = useState<number | null>(null)

  async function load(search = q) {
    try {
      const d = await api<Page>(`/api/v1/admin/users?page=1&page_size=50&q=${encodeURIComponent(search)}`)
      setData(d)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }

  useEffect(() => { load() }, [])

  async function toggle(u: UserRow) {
    const status = u.status === 1 ? 0 : 1
    setBusyId(u.id)
    try {
      await api(`/api/v1/admin/users/${u.id}/status`, {
        method: 'PUT',
        body: JSON.stringify({ status }),
      })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败')
    } finally {
      setBusyId(null)
    }
  }

  async function remove(u: UserRow) {
    if (!confirm(`确认删除用户「${u.username}」？其设备绑定也会一并删除。`)) return
    setBusyId(u.id)
    try {
      await api(`/api/v1/admin/users/${u.id}`, { method: 'DELETE' })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除失败')
    } finally {
      setBusyId(null)
    }
  }

  async function createUser(e: FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      await api('/api/v1/admin/users', {
        method: 'POST',
        body: JSON.stringify({
          username: form.username.trim(),
          display_name: form.display_name.trim(),
          password: form.password,
        }),
      })
      setForm(emptyForm)
      setShowCreate(false)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <h1 className="page-title">用户</h1>
      <div className="toolbar">
        <input className="input" style={{ maxWidth: 260 }} placeholder="搜索用户名" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn" onClick={() => load()}>搜索</button>
        <div className="spacer" />
        <span className="muted">共 {data?.total ?? 0} 人</span>
        <button className="btn primary" onClick={() => { setError(''); setShowCreate(true) }}>添加用户</button>
      </div>
      {error && <div className="error" style={{ marginBottom: '0.75rem' }}>{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>用户名</th>
              <th>显示名</th>
              <th>设备数</th>
              <th>状态</th>
              <th>注册时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(data?.items ?? []).map((u) => (
              <tr key={u.id}>
                <td className="mono">{u.id}</td>
                <td>{u.username}</td>
                <td>{u.display_name}</td>
                <td>{u.device_count}</td>
                <td>
                  <span className={`dot ${u.status === 1 ? 'online' : 'disabled'}`} />
                  {u.status === 1 ? '正常' : '禁用'}
                </td>
                <td className="muted">{new Date(u.created_at).toLocaleString()}</td>
                <td>
                  <div className="row-actions">
                    <button className={`btn ${u.status === 1 ? 'danger' : ''}`} disabled={busyId === u.id} onClick={() => toggle(u)}>
                      {u.status === 1 ? '禁用' : '启用'}
                    </button>
                    <button className="btn danger" disabled={busyId === u.id} onClick={() => remove(u)}>删除</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showCreate && (
        <div className="modal-backdrop" onClick={() => !saving && setShowCreate(false)}>
          <form className="modal-card" onClick={(e) => e.stopPropagation()} onSubmit={createUser}>
            <h2>添加用户</h2>
            <label className="field">
              <span>用户名</span>
              <input
                className="input"
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder="3–64 位字母、数字或下划线"
                autoFocus
                required
              />
            </label>
            <label className="field">
              <span>显示名</span>
              <input
                className="input"
                value={form.display_name}
                onChange={(e) => setForm({ ...form, display_name: e.target.value })}
                placeholder="可留空，默认与用户名相同"
              />
            </label>
            <label className="field">
              <span>密码</span>
              <input
                className="input"
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                placeholder="至少 6 位"
                required
              />
            </label>
            <div className="modal-actions">
              <button type="button" className="btn" disabled={saving} onClick={() => setShowCreate(false)}>取消</button>
              <button className="btn primary" disabled={saving}>{saving ? '创建中…' : '创建'}</button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
