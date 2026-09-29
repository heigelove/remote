export type WindowMode = {
  kind: 'main' | 'session'
  session_id?: number
  token?: string
  server_url?: string
}

function bridge(): any {
  return (window as any).go?.main?.App
}

export async function wailsApp(): Promise<any> {
  for (let i = 0; i < 80; i++) {
    const a = bridge()
    if (a) return a
    await new Promise((r) => setTimeout(r, 50))
  }
  throw new Error('Wails 接口未就绪')
}

export async function getWindowMode(): Promise<WindowMode> {
  try {
    const app = await wailsApp()
    return await app.GetWindowMode()
  } catch {
    return { kind: 'main', server_url: 'http://127.0.0.1:8080' }
  }
}
