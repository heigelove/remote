import { FormEvent, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, setAuth, TokenPair, AdminUser } from '../api'
import './login.css'

export default function Login() {
  const nav = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('admin123')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const data = await api<{ user: AdminUser; token: TokenPair }>('/api/v1/admin/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
      setAuth(data.token, data.user)
      nav('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card fade-in" onSubmit={onSubmit}>
        <div className="login-brand">Remote</div>
        <p className="muted">服务端管理后台</p>
        <label className="field">
          <span>用户名</span>
          <input className="input" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        </label>
        <label className="field">
          <span>密码</span>
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && <div className="error">{error}</div>}
        <button className="btn primary" style={{ width: '100%', marginTop: '1rem' }} disabled={loading}>
          {loading ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  )
}
