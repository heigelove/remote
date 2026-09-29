import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { clearAuth, getStoredUser } from '../api'
import './layout.css'

const links = [
  { to: '/', label: '概览', end: true },
  { to: '/users', label: '用户' },
  { to: '/devices', label: '设备' },
  { to: '/logs', label: '日志' },
]

export default function Layout() {
  const nav = useNavigate()
  const user = getStoredUser()
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">Remote</div>
        <div className="brand-sub">Admin</div>
        <nav>
          {links.map((l) => (
            <NavLink key={l.to} to={l.to} end={l.end} className={({ isActive }) => (isActive ? 'nav active' : 'nav')}>
              {l.label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <div className="muted" style={{ fontSize: '0.85rem' }}>{user?.username}</div>
          <button
            className="btn"
            style={{ width: '100%', marginTop: 8 }}
            onClick={() => {
              clearAuth()
              nav('/login')
            }}
          >
            退出
          </button>
        </div>
      </aside>
      <main className="main fade-in">
        <Outlet />
      </main>
    </div>
  )
}
