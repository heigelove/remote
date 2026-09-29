let serverURL = 'http://127.0.0.1:8080'

const TOKEN_KEY = 'remoto_client_token'
const REFRESH_KEY = 'remoto_client_refresh'
const USER_KEY = 'remoto_client_user'

export type TokenPair = {
  access_token: string
  refresh_token: string
  expires_in: number
}

export type User = {
  id: number
  username: string
  display_name: string
  role: string
}

export type Device = {
  id: number
  device_uid: string
  name: string
  os_version: string
  status: number
  last_ip: string
  last_seen_at?: string
  agent_version?: string
}

export function getServerURL() {
  return serverURL
}

export function setServerURL(url: string) {
  serverURL = url.replace(/\/$/, '')
}

export function getToken() {
  return localStorage.getItem(TOKEN_KEY)
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_KEY)
}

export function setAuth(token: TokenPair, user?: User | null) {
  authExpired = false
  localStorage.setItem(TOKEN_KEY, token.access_token)
  if (token.refresh_token) localStorage.setItem(REFRESH_KEY, token.refresh_token)
  if (user) localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(USER_KEY)
}

export function getStoredUser(): User | null {
  try {
    return JSON.parse(localStorage.getItem(USER_KEY) || 'null')
  } catch {
    return null
  }
}

type ApiBody<T> = { code: number; message: string; data: T }

type AuthExpiredHandler = () => void
const authExpiredHandlers = new Set<AuthExpiredHandler>()
let authExpired = false
let refreshInFlight: Promise<boolean> | null = null

export function onAuthExpired(handler: AuthExpiredHandler) {
  authExpiredHandlers.add(handler)
  return () => {
    authExpiredHandlers.delete(handler)
  }
}

function isAuthFailure(status: number, message: string) {
  if (status === 401) return true
  const m = (message || '').toLowerCase()
  return (
    m.includes('invalid token') ||
    m.includes('token is expired') ||
    m.includes('token has expired') ||
    m.includes('expired') && m.includes('token') ||
    m.includes('missing authorization') ||
    m.includes('invalid refresh token') ||
    m.includes('refresh token')
  )
}

async function tryRefresh(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight
  const refreshToken = getRefreshToken()
  if (!refreshToken) return false

  refreshInFlight = (async () => {
    try {
      const res = await fetch(`${serverURL}/api/v1/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      })
      const body = (await res.json()) as ApiBody<TokenPair>
      if (!res.ok || body.code !== 0 || !body.data?.access_token) return false
      setAuth(body.data)
      return true
    } catch {
      return false
    } finally {
      refreshInFlight = null
    }
  })()
  return refreshInFlight
}

function expireAuth() {
  if (authExpired) return
  authExpired = true
  clearAuth()
  import('./wails')
    .then(({ wailsApp }) => wailsApp())
    .then(async (app) => {
      try {
        const mode = await app.GetWindowMode()
        if (mode?.kind === 'session') {
          await app.CloseWindow()
          return
        }
      } catch {
        // ignore
      }
      try {
        await app.ClearUserSession()
      } catch {
        // ignore
      }
    })
    .catch(() => {})
  authExpiredHandlers.forEach((fn) => fn())
  if (!location.hash.includes('/login')) {
    location.hash = '#/login'
  }
}

export async function api<T>(path: string, init: RequestInit = {}, retried = false): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(`${serverURL}${path}`, { ...init, headers })
  let body: ApiBody<T>
  try {
    body = (await res.json()) as ApiBody<T>
  } catch {
    throw new Error(res.statusText || '请求失败')
  }

  const authPath = path.includes('/auth/login') || path.includes('/auth/refresh')
  if (isAuthFailure(res.status, body.message) && !authPath) {
    if (!retried) {
      const ok = await tryRefresh()
      if (ok) return api<T>(path, init, true)
    }
    expireAuth()
    throw new Error('登录已过期，请重新登录')
  }

  if (!res.ok || body.code !== 0) throw new Error(body.message || res.statusText)
  return body.data
}

export function wsURL(path: string, params: Record<string, string>) {
  const base = new URL(serverURL)
  const proto = base.protocol === 'https:' ? 'wss:' : 'ws:'
  const qs = new URLSearchParams(params).toString()
  return `${proto}//${base.host}${path}?${qs}`
}
