import { useEffect, useState } from 'react'
import { api } from '../api'

type Stats = {
  user_count: number
  online_devices: number
  total_devices: number
  today_sessions: number
}

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    const load = () =>
      api<Stats>('/api/v1/admin/stats')
        .then((d) => alive && setStats(d))
        .catch((e) => alive && setError(e.message))
    load()
    const t = setInterval(load, 15000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  return (
    <div>
      <h1 className="page-title">概览</h1>
      {error && <div className="error">{error}</div>}
      <div className="card-grid">
        <div className="stat-card">
          <div className="label">用户数</div>
          <div className="value">{stats?.user_count ?? '—'}</div>
        </div>
        <div className="stat-card">
          <div className="label">在线设备</div>
          <div className="value">{stats?.online_devices ?? '—'}</div>
        </div>
        <div className="stat-card">
          <div className="label">设备总数</div>
          <div className="value">{stats?.total_devices ?? '—'}</div>
        </div>
        <div className="stat-card">
          <div className="label">今日会话</div>
          <div className="value">{stats?.today_sessions ?? '—'}</div>
        </div>
      </div>
    </div>
  )
}
