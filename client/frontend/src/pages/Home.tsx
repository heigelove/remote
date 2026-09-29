import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { clearAuth, getStoredUser, getToken } from '../api'
import { wailsApp } from '../wails'
import AgentPanel from './AgentPanel'
import DevicePanel from './DevicePanel'
import LogPanel from './LogPanel'
import './home.css'

export default function Home() {
  const nav = useNavigate()
  const user = getStoredUser()
  const [tab, setTab] = useState<'remote' | 'agent' | 'logs'>('remote')

  useEffect(() => {
    const token = getToken()
    if (!token) return
    wailsApp()
      .then((app) => app.ApplyUserSession(token, ''))
      .catch(() => {})
  }, [])

  return (
    <div className="home fade-in">
      <header className="topbar">
        <div>
          <div className="logo">Remote</div>
          <div className="muted" style={{ fontSize: '0.85rem' }}>你好，{user?.display_name || user?.username}</div>
        </div>
        <div className="top-actions">
          <button
            className="btn ghost"
            onClick={async () => {
              try {
                await (await wailsApp()).ClearUserSession()
              } catch {
                // ignore
              }
              clearAuth()
              nav('/login')
            }}
          >
            退出
          </button>
        </div>
      </header>

      <div className="mode-tabs">
        <button className={tab === 'remote' ? 'active' : ''} onClick={() => setTab('remote')}>设备列表</button>
        <button className={tab === 'agent' ? 'active' : ''} onClick={() => setTab('agent')}>本机被控</button>
        <button className={tab === 'logs' ? 'active' : ''} onClick={() => setTab('logs')}>错误日志</button>
      </div>

      {tab === 'agent' ? (
        <AgentPanel />
      ) : tab === 'logs' ? (
        <LogPanel />
      ) : (
        <DevicePanel />
      )}
    </div>
  )
}
