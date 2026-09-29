import { useEffect, useState } from 'react'
import { api, Device, getToken } from '../api'
import { wailsApp } from '../wails'
import { STREAM_QUALITIES, loadStreamQuality, saveStreamQuality } from '../quality'
import './home.css'

export default function DevicePanel() {
  const [devices, setDevices] = useState<Device[]>([])
  const [localUID, setLocalUID] = useState('')
  const [error, setError] = useState('')
  const [connecting, setConnecting] = useState<number | null>(null)
  const [locking, setLocking] = useState<number | null>(null)
  const [quality, setQuality] = useState(loadStreamQuality)

  function isLocal(device: Device) {
    return !!localUID && device.device_uid === localUID
  }

  async function load() {
    try {
      let uid = localUID
      try {
        const st = await (await wailsApp()).GetStatus()
        if (st?.device_uid) {
          uid = String(st.device_uid)
          setLocalUID(uid)
        }
      } catch {
        // ignore
      }
      const list = await api<Device[]>('/api/v1/devices')
      setDevices(
        [...list].sort((a, b) => {
          const al = uid && a.device_uid === uid ? 0 : 1
          const bl = uid && b.device_uid === uid ? 0 : 1
          return al - bl
        }),
      )
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }

  useEffect(() => {
    load()
    const once = setTimeout(load, 1500)
    const t = setInterval(load, 10000)
    return () => {
      clearTimeout(once)
      clearInterval(t)
    }
  }, [])

  async function connect(device: Device) {
    if (isLocal(device)) {
      setError('不能连接本机设备')
      return
    }
    if (device.status !== 1) {
      setError('设备离线，无法连接')
      return
    }
    setConnecting(device.id)
    try {
      const res = await api<{ session: { id: number }; room_id: string }>('/api/v1/sessions', {
        method: 'POST',
        body: JSON.stringify({ device_id: device.id, quality: saveStreamQuality(quality) }),
      })
      const token = getToken() || ''
      const app = await wailsApp()
      await app.OpenSessionWindow(String(res.session.id), token)
    } catch (e) {
      setError(e instanceof Error ? e.message : '连接失败')
    } finally {
      setConnecting(null)
    }
  }

  async function lock(device: Device) {
    if (device.status !== 1) {
      setError('设备离线，无法锁屏')
      return
    }
    setLocking(device.id)
    try {
      await api(`/api/v1/devices/${device.id}/lock`, { method: 'POST' })
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '锁屏失败')
    } finally {
      setLocking(null)
    }
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>我的设备</h2>
        <div className="section-tools">
          <label className="quality-field">
            画面
            <select
              value={quality}
              onChange={(e) => setQuality(saveStreamQuality(e.target.value))}
            >
              {STREAM_QUALITIES.map((opt) => (
                <option key={opt.id} value={opt.id}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
          <button className="btn" onClick={load}>刷新</button>
        </div>
      </div>
      {error && <div className="error">{error}</div>}
      <div className="device-grid">
        {[...devices]
          .sort((a, b) => Number(isLocal(b)) - Number(isLocal(a)))
          .map((d, i) => {
          const local = isLocal(d)
          return (
            <article key={d.id} className={`device-card${local ? ' local' : ''}`} style={{ animationDelay: `${i * 40}ms` }}>
              <div className="device-head">
                <h3>{local ? '本机' : d.name}</h3>
                <span>
                  <span className={`dot ${d.status === 1 ? 'online' : 'offline'}`} />
                  {d.status === 1 ? '在线' : '离线'}
                </span>
              </div>
              <div className="device-meta mono">{local && d.name ? d.name : d.os_version || '—'}</div>
              {local && d.os_version ? <div className="device-meta muted">{d.os_version}</div> : null}
              <div className="device-meta muted">
                最后在线 {d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : '—'}
              </div>
              {!local && (
                <div className="device-actions">
                  <button
                    className="btn primary"
                    disabled={connecting === d.id || d.status !== 1}
                    onClick={() => connect(d)}
                  >
                    {connecting === d.id ? '连接中…' : '远程连接'}
                  </button>
                  <button
                    className="btn"
                    disabled={d.status !== 1 || locking === d.id}
                    onClick={() => lock(d)}
                  >
                    {locking === d.id ? '锁屏中…' : '锁屏'}
                  </button>
                </div>
              )}
            </article>
          )
        })}
        {devices.length === 0 && (
          <div className="empty muted">暂无在线设备。登录后本机会自动作为被控端出现在列表中；其它电脑用同一账号登录即可。</div>
        )}
      </div>
    </section>
  )
}
