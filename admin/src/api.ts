const TOKEN_KEY = 'remoto_admin_token'
const USER_KEY = 'remoto_admin_user'

export type TokenPair = {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

export type AdminUser = {
  id: number
  username: string
  display_name: string
  role: string
  status: number
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setAuth(token: TokenPair, user: AdminUser) {
  localStorage.setItem(TOKEN_KEY, token.access_token)
  localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

export function getStoredUser(): AdminUser | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try { return JSON.parse(raw) } catch { return null }
}

type ApiBody<T> = { code: number; message: string; data: T }

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(path, { ...init, headers })
  const body = (await res.json()) as ApiBody<T>
  if (!res.ok || body.code !== 0) {
    throw new Error(body.message || res.statusText)
  }
  return body.data
}
