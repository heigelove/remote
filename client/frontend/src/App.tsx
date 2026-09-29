import { useEffect, useState } from 'react'
import { HashRouter, Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { api, getToken, onAuthExpired, setServerURL } from './api'
import { getWindowMode, WindowMode } from './wails'
import Login from './pages/Login'
import Home from './pages/Home'
import Session from './pages/Session'
import './styles.css'

function Private({ children }: { children: React.ReactNode }) {
  if (!getToken()) return <Navigate to="/login" replace />
  return <>{children}</>
}

function AuthGuard() {
  const nav = useNavigate()
  useEffect(() => {
    return onAuthExpired(() => nav('/login', { replace: true }))
  }, [nav])
  useEffect(() => {
    if (!getToken()) return
    api('/api/v1/me').catch(() => {})
  }, [])
  return null
}

export default function App() {
  const [mode, setMode] = useState<WindowMode | null>(null)

  useEffect(() => {
    getWindowMode().then((m) => {
      if (m.server_url) setServerURL(m.server_url)
      setMode(m)
    })
  }, [])

  if (!mode) {
    return <div className="boot">加载中…</div>
  }

  if (mode.kind === 'session') {
    return (
      <Session
        sessionId={String(mode.session_id || '')}
        token={mode.token}
        serverURL={mode.server_url}
        standalone
      />
    )
  }

  return (
    <HashRouter>
      <AuthGuard />
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<Private><Home /></Private>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </HashRouter>
  )
}
