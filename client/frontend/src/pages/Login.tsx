import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, setAuth, setServerURL, TokenPair, User } from '../api'
import { wailsApp } from '../wails'
import './login.css'

export default function Login() {
  const nav = useNavigate()
  const [server, setServer] = useState('http://127.0.0.1:8080')
  const [username, setUsername] = useState('demo')
  const [password, setPassword] = useState('demo123')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    wailsApp()
      .then((app) => app.GetWindowMode())
      .then((m: { server_url?: string }) => {
        if (m?.server_url) {
          setServer(m.server_url)
          setServerURL(m.server_url)
        }
      })
      .catch(() => {})
  }, [])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      let base = server.trim()
      try {
        const app = await wailsApp()
        base = await app.SetServerURL(base)
      } catch {
        // browser fallback
      }
      setServerURL(base)
      const data = await api<{ user: User; token: TokenPair }>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
      setAuth(data.token, data.user)
      try {
        const app = await wailsApp()
        await app.ApplyUserSession(data.token.access_token, data.token.refresh_token)
      } catch {
        // Home 会再尝试用同一 token 把本机上线
      }
      nav('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-atmosphere" />
      <form className="login-card fade-in" onSubmit={onSubmit}>
        <h1 className="brand">Remote</h1>
        <p className="tagline">控制端 + 被控端 · 连接你的 Windows 设备</p>
        <label className="field">
          <span>服务器地址</span>
          <input className="input" value={server} onChange={(e) => setServer(e.target.value)} placeholder="http://127.0.0.1:8080" />
        </label>
        <label className="field">
          <span>用户名</span>
          <input className="input" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        </label>
        <label className="field">
          <span>密码</span>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && <div className="error">{error}</div>}
        <button className="btn primary" style={{ width: '100%', marginTop: '1.25rem' }} disabled={loading}>
          {loading ? '登录中…' : '进入控制台'}
        </button>
      </form>
    </div>
  )
}
