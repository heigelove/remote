import { useEffect, useState } from 'react'
import { wailsApp } from '../wails'
import './agent.css'

export default function LogPanel() {
  const [logs, setLogs] = useState<string[]>([])
  const [logPath, setLogPath] = useState('')
  const [err, setErr] = useState('')

  async function refreshLogs() {
    try {
      const app = await wailsApp()
      const lines: string[] = await app.GetRecentLogs(200)
      setLogs(lines || [])
      const path: string = await app.GetLogPath()
      if (path) setLogPath(path)
      setErr('')
    } catch (e) {
      setLogs([e instanceof Error ? e.message : String(e)])
    }
  }

  useEffect(() => {
    refreshLogs()
    const t = setInterval(refreshLogs, 3000)
    return () => clearInterval(t)
  }, [])

  return (
    <div className="agent-panel">
      <div className="agent-card">
        <div className="agent-log-head">
          <strong>错误日志</strong>
          <div className="agent-row" style={{ margin: 0 }}>
            <button className="btn" onClick={refreshLogs}>刷新</button>
            <button className="btn" onClick={async () => {
              try { await (await wailsApp()).OpenLogFolder() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) }
            }}>打开目录</button>
            <button className="btn" onClick={async () => {
              await (await wailsApp()).ClearRecentLogs()
              await refreshLogs()
            }}>清空缓冲</button>
          </div>
        </div>
        <div className="agent-meta" style={{ marginTop: '0.5rem' }}>{logPath ? `文件: ${logPath}` : ''}</div>
        {err && <div className="error">{err}</div>}
        <pre className="agent-log agent-log-full">{logs.length ? logs.join('\n') : '暂无日志'}</pre>
      </div>
    </div>
  )
}
