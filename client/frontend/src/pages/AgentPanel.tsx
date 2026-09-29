import { useEffect, useState } from 'react'
import { wailsApp } from '../wails'
import './agent.css'

type Status = {
  status?: string
  running?: boolean
  streaming?: boolean
  frames?: number
  last_error?: string
  device_uid?: string
  device_name?: string
  server_url?: string
  bound?: boolean
  auto_start?: boolean
  version?: string
  locked?: boolean
  log_path?: string
  service_status?: string
  service_ready?: boolean
}

export default function AgentPanel() {
  const [st, setSt] = useState<Status>({})
  const [err, setErr] = useState('')
  const [svcErr, setSvcErr] = useState('')

  async function refresh() {
    try {
      const app = await wailsApp()
      const next: Status = await app.GetStatus()
      setSt(next)
      setErr(next.last_error || '')
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    refresh()
    const a = setInterval(refresh, 2000)
    return () => clearInterval(a)
  }, [])

  const dotClass = st.streaming ? 'stream' : st.running ? 'on' : st.last_error ? 'err' : ''

  return (
    <div className="agent-panel">
      <div className="agent-card">
        <div className="agent-status">
          <span className={`agent-dot ${dotClass}`} />
          <strong>{(st.status || '-') + (st.locked ? ' · 已锁屏' : '')}</strong>
        </div>
        <div className="agent-meta">
          {`设备: ${st.device_name || '-'} (${st.device_uid || '-'})\n服务器: ${st.server_url || '-'}\n随当前登录账号上线: ${st.bound ? '是' : '否'} · 帧计数: ${st.frames || 0} · v${st.version || ''}`}
        </div>
        {err && <div className="error">{err}</div>}
        <div className="agent-row">
          <button className="btn primary" disabled={!!st.running} onClick={async () => {
            try { await (await wailsApp()).Start(); await refresh() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) }
          }}>启动被控连接</button>
          <button className="btn" disabled={!st.running} onClick={async () => {
            try { await (await wailsApp()).Stop(); await refresh() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) }
          }}>停止</button>
          <button className="btn" onClick={() => { refresh() }}>刷新状态</button>
        </div>
      </div>

      <div className="agent-card">
        <label className="agent-check">
          <input type="checkbox" checked={!!st.auto_start} onChange={async (e) => {
            const enable = e.target.checked
            try {
              await (await wailsApp()).SetAutoStart(enable)
              await refresh()
            } catch (err) {
              setErr(err instanceof Error ? err.message : String(err))
            }
          }} />
          开机自动启动 Remote
        </label>
        <div className="agent-meta" style={{ marginTop: '0.75rem' }}>
          {`锁屏输入服务: ${st.service_status || '-'} · ${st.service_ready ? '管道就绪' : '管道未通'}${st.locked ? ' · 当前已锁屏' : ''}`}
        </div>
        <div className="agent-row">
          <button className="btn primary" onClick={async () => {
            setSvcErr('')
            try {
              await (await wailsApp()).InstallInputService()
              setSvcErr('已发起安装/重启（请在 UAC 中点“是”），稍后刷新状态')
              setTimeout(refresh, 2500)
            } catch (e) {
              setSvcErr(e instanceof Error ? e.message : String(e))
            }
          }}>安装锁屏输入服务</button>
          <button className="btn" onClick={async () => {
            setSvcErr('')
            try {
              await (await wailsApp()).UninstallInputService()
              setSvcErr('已发起卸载，稍后刷新状态')
              setTimeout(refresh, 2500)
            } catch (e) {
              setSvcErr(e instanceof Error ? e.message : String(e))
            }
          }}>卸载服务</button>
        </div>
        <p className="muted" style={{ marginTop: '0.75rem', fontSize: '0.85rem' }}>锁屏密码输入需要 SYSTEM 服务。请点一次「安装锁屏输入服务」（UAC 允许）。之后被控连接启动时会自动拉起服务。</p>
        {svcErr && <div className="error">{svcErr}</div>}
      </div>
    </div>
  )
}
