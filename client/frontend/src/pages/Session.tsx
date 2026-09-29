import { useEffect, useRef, useState } from 'react'
import { getToken, setServerURL, wsURL } from '../api'
import { WindowSetTitle, EventsOn, ClipboardGetText, ClipboardSetText } from '../../wailsjs/runtime/runtime'
import { keyPayload } from '../keys'
import { STREAM_QUALITIES, loadStreamQuality, saveStreamQuality } from '../quality'
import './session.css'

type DisplayInfo = {
  index: number
  x: number
  y: number
  w: number
  h: number
  primary?: boolean
  name?: string
}

function displayLabel(d: DisplayInfo) {
  const size = d.w && d.h ? ` ${d.w}×${d.h}` : ''
  return `${d.index + 1}${d.primary ? ' 主屏' : ''}${size}`
}

type SessionProps = {
  sessionId?: string
  token?: string
  serverURL?: string
  standalone?: boolean
}

function statusLabel(status: string, hasFrame: boolean) {
  if (status === 'connecting') return '正在连接'
  if (status === 'connected') return hasFrame ? '已连接 · 画面中' : '已连接 · 等待画面'
  if (status.startsWith('disconnected')) {
    const extra = status.slice('disconnected'.length)
    return extra ? `已断开${extra}` : '已断开'
  }
  if (status === 'error') return '连接出错'
  return status
}

function signalLabel(status: string, rtt: number | null, lastPongAt: number, lastFrameAt: number) {
  if (status === 'connecting') return '信号检测中'
  if (status !== 'connected') return '无信号'
  const now = Date.now()
  const pongAge = lastPongAt ? now - lastPongAt : 99999
  const frameAge = lastFrameAt ? now - lastFrameAt : 99999
  if (pongAge > 8000 && frameAge > 4000) return '信号中断'
  let rttAdj = rtt ?? 999
  if (frameAge > 2500) rttAdj = Math.max(rttAdj, 220)
  if (frameAge > 4500) return '信号弱 ▂▄  画面卡顿'
  const ms = Math.round(rttAdj)
  if (rttAdj < 80) return `信号强 ▂▄▆█  ${ms}ms`
  if (rttAdj < 180) return `信号中 ▂▄▆  ${ms}ms`
  if (rttAdj < 400) return `信号弱 ▂▄  ${ms}ms`
  return `信号差 ▂  ${ms}ms`
}

export default function Session({ sessionId: sessionIdProp, token: tokenProp, serverURL }: SessionProps) {
  const sessionId = sessionIdProp
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const pageRef = useRef<HTMLDivElement>(null)
  const stageRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const lastMoveAt = useRef(0)
  const layoutCanvas = useRef(() => {})
  const clipLastSent = useRef('')
  const clipFromRemote = useRef('')
  const rttEma = useRef<number | null>(null)
  const lastPongAt = useRef(0)
  const lastFrameAt = useRef(0)
  const [status, setStatus] = useState('connecting')
  const [hasFrame, setHasFrame] = useState(false)
  const [cursor, setCursor] = useState({ x: 0, y: 0, show: false, shape: 'arrow' })
  const [clickFlash, setClickFlash] = useState<{ x: number; y: number; id: number } | null>(null)
  const [quality, setQuality] = useState(loadStreamQuality)
  const [displays, setDisplays] = useState<DisplayInfo[]>([])
  const [monitor, setMonitor] = useState(-1)
  const [rttMs, setRttMs] = useState<number | null>(null)
  const [signalTick, setSignalTick] = useState(0)

  layoutCanvas.current = () => {
    const canvas = canvasRef.current
    const stage = stageRef.current
    if (!canvas || !stage) return
    const bw = canvas.width
    const bh = canvas.height
    if (!bw || !bh) return
    const cw = stage.clientWidth
    const ch = stage.clientHeight
    if (cw < 1 || ch < 1) return
    const scale = Math.min(cw / bw, ch / bh)
    canvas.style.width = `${bw * scale}px`
    canvas.style.height = `${bh * scale}px`
  }

  useEffect(() => {
    const title = `Remote  session #${sessionId || '-'}  ${statusLabel(status, hasFrame)}  ·  ${signalLabel(status, rttMs, lastPongAt.current, lastFrameAt.current)}`
    try {
      WindowSetTitle(title)
    } catch {
      document.title = title
    }
  }, [sessionId, status, hasFrame, rttMs, signalTick])

  useEffect(() => {
    const id = window.setInterval(() => setSignalTick((n) => n + 1), 1000)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    if (serverURL) setServerURL(serverURL)
    const token = tokenProp || getToken()
    if (!token || !sessionId) return

    let disposed = false
    const url = wsURL('/ws/session', { token, session_id: sessionId })
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws
    setStatus('connecting')
    setHasFrame(false)
    setDisplays([])
    setMonitor(-1)
    setRttMs(null)
    rttEma.current = null
    lastPongAt.current = 0
    lastFrameAt.current = 0

    ws.onopen = () => {
      if (!disposed) setStatus('connected')
      pageRef.current?.focus()
      try {
        ws.send(JSON.stringify({ type: 'quality', quality: loadStreamQuality() }))
        ws.send(JSON.stringify({ type: 'displays' }))
      } catch {
        // ignore
      }
    }
    ws.onclose = (ev) => {
      if (disposed) return
      setStatus(ev.code === 1000 ? 'disconnected' : `disconnected(${ev.code})`)
    }
    ws.onerror = () => {
      if (!disposed) setStatus('error')
    }
    ws.onmessage = async (ev) => {
      if (disposed) return
      if (typeof ev.data === 'string') {
        try {
          const msg = JSON.parse(ev.data) as {
            type?: string
            text?: string
            shape?: string
            t?: number
            items?: DisplayInfo[]
            selected?: number
          }
          if (msg.type === 'clipboard' && typeof msg.text === 'string') {
            clipFromRemote.current = msg.text
            clipLastSent.current = msg.text
            await ClipboardSetText(msg.text)
          }
          if (msg.type === 'cursor' && typeof msg.shape === 'string' && msg.shape) {
            setCursor((c) => ({ ...c, shape: msg.shape as string }))
          }
          if (msg.type === 'netpong' && typeof msg.t === 'number') {
            const rtt = Math.max(0, Date.now() - msg.t)
            lastPongAt.current = Date.now()
            const prev = rttEma.current
            const ema = prev == null ? rtt : prev * 0.6 + rtt * 0.4
            rttEma.current = ema
            setRttMs(ema)
          }
          if (msg.type === 'displays' && Array.isArray(msg.items)) {
            setDisplays(msg.items.filter((d) => d && typeof d.index === 'number'))
            if (typeof msg.selected === 'number') setMonitor(msg.selected)
          }
        } catch {
          // ignore
        }
        return
      }
      try {
        const blob = new Blob([ev.data], { type: 'image/jpeg' })
        const bmp = await createImageBitmap(blob)
        const canvas = canvasRef.current
        if (!canvas || disposed) {
          bmp.close()
          return
        }
        if (canvas.width !== bmp.width || canvas.height !== bmp.height) {
          canvas.width = bmp.width
          canvas.height = bmp.height
        }
        const ctx = canvas.getContext('2d')
        ctx?.drawImage(bmp, 0, 0)
        bmp.close()
        layoutCanvas.current()
        lastFrameAt.current = Date.now()
        setHasFrame(true)
      } catch {
        // ignore decode errors
      }
    }

    return () => {
      disposed = true
      wsRef.current = null
      if (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN) {
        ws.close(1000, 'leave')
      }
    }
  }, [sessionId, tokenProp, serverURL])

  useEffect(() => {
    if (status !== 'connected') return
    let stop = false
    const tick = async () => {
      if (stop) return
      try {
        const text = await ClipboardGetText()
        if (stop || text == null || text === '') return
        if (text === clipLastSent.current || text === clipFromRemote.current) return
        if (text.length > 256 * 1024) return
        const ws = wsRef.current
        if (!ws || ws.readyState !== WebSocket.OPEN) return
        clipLastSent.current = text
        ws.send(JSON.stringify({ type: 'clipboard', text }))
      } catch {
        // ignore
      }
    }
    tick()
    const id = window.setInterval(tick, 400)
    return () => {
      stop = true
      window.clearInterval(id)
    }
  }, [status, sessionId])

  useEffect(() => {
    if (status !== 'connected') return
    const ping = () => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'netping', t: Date.now() }))
    }
    ping()
    const id = window.setInterval(ping, 2000)
    return () => window.clearInterval(id)
  }, [status, sessionId])

  useEffect(() => {
    const stage = stageRef.current
    if (!stage) return
    const ro = new ResizeObserver(() => layoutCanvas.current())
    ro.observe(stage)
    layoutCanvas.current()
    return () => ro.disconnect()
  }, [])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const mapPoint = (clientX: number, clientY: number) => {
      const rect = canvas.getBoundingClientRect()
      const scaleX = canvas.width / Math.max(rect.width, 1)
      const scaleY = canvas.height / Math.max(rect.height, 1)
      return {
        x: Math.max(0, Math.min(canvas.width - 1, Math.round((clientX - rect.left) * scaleX))),
        y: Math.max(0, Math.min(canvas.height - 1, Math.round((clientY - rect.top) * scaleY))),
        cssX: clientX - rect.left,
        cssY: clientY - rect.top,
      }
    }

    const sendInput = (payload: Record<string, unknown>) => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        console.warn('[remoto] sendInput skipped, ws=', ws?.readyState)
        return false
      }
      ws.send(JSON.stringify({ type: 'input', ...payload }))
      return true
    }

    const buttonName = (button: number) => {
      if (button === 2) return 'right'
      if (button === 1) return 'middle'
      return 'left'
    }

    const flash = (cssX: number, cssY: number) => {
      const id = performance.now()
      setClickFlash({ x: cssX, y: cssY, id })
      window.setTimeout(() => {
        setClickFlash((cur) => (cur && cur.id === id ? null : cur))
      }, 180)
    }

    const down = (e: PointerEvent) => {
      e.preventDefault()
      e.stopPropagation()
      pageRef.current?.focus()
      try {
        canvas.setPointerCapture(e.pointerId)
      } catch {
        // ignore
      }
      const p = mapPoint(e.clientX, e.clientY)
      const button = buttonName(e.button)
      sendInput({ action: 'mousemove', x: p.x, y: p.y })
      if (sendInput({ action: 'mousedown', button, x: p.x, y: p.y })) {
        flash(p.cssX, p.cssY)
      }
    }
    const up = (e: PointerEvent) => {
      e.preventDefault()
      e.stopPropagation()
      const p = mapPoint(e.clientX, e.clientY)
      sendInput({ action: 'mouseup', button: buttonName(e.button), x: p.x, y: p.y })
      try {
        canvas.releasePointerCapture(e.pointerId)
      } catch {
        // ignore
      }
    }
    const move = (e: PointerEvent) => {
      const p = mapPoint(e.clientX, e.clientY)
      setCursor((c) => ({ ...c, x: p.cssX, y: p.cssY, show: true }))
      const now = performance.now()
      if (now - lastMoveAt.current < 16) return
      lastMoveAt.current = now
      sendInput({ action: 'mousemove', x: p.x, y: p.y })
    }
    const dbl = (e: MouseEvent) => {
      e.preventDefault()
    }
    const ctx = (e: Event) => {
      e.preventDefault()
      e.stopPropagation()
    }
    const wheel = (e: WheelEvent) => {
      e.preventDefault()
      const p = mapPoint(e.clientX, e.clientY)
      sendInput({ action: 'mousemove', x: p.x, y: p.y })
      sendInput({ action: 'wheel', delta: e.deltaY > 0 ? -120 : 120, x: p.x, y: p.y })
    }

    canvas.addEventListener('pointerdown', down)
    canvas.addEventListener('pointerup', up)
    canvas.addEventListener('pointercancel', up)
    canvas.addEventListener('pointermove', move)
    canvas.addEventListener('dblclick', dbl)
    canvas.addEventListener('contextmenu', ctx)
    canvas.addEventListener('wheel', wheel, { passive: false })
    return () => {
      canvas.removeEventListener('pointerdown', down)
      canvas.removeEventListener('pointerup', up)
      canvas.removeEventListener('pointercancel', up)
      canvas.removeEventListener('pointermove', move)
      canvas.removeEventListener('dblclick', dbl)
      canvas.removeEventListener('contextmenu', ctx)
      canvas.removeEventListener('wheel', wheel)
    }
  }, [sessionId, tokenProp, serverURL])

  useEffect(() => {
    const held = new Set<number>()
    const sendKey = (payload: Record<string, unknown>) => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      ws.send(JSON.stringify({ type: 'input', ...payload }))
    }
    const down = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName
      if (tag === 'SELECT' || tag === 'INPUT' || tag === 'TEXTAREA') return
      if (e.isComposing || e.key === 'Process') return
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation()
      // 组合键（Win/Ctrl/Alt、复制粘贴、输入法切换）由底层钩子转发，避免本机抢先处理。
      if (e.metaKey || e.ctrlKey || e.altKey || e.key === 'Meta' || e.key === 'Control' || e.key === 'Alt' || e.key === 'Process') return
      const payload = keyPayload(e, 'keydown')
      const vk = payload.vk as number
      if (vk) held.add(vk)
      sendKey(payload)
      pageRef.current?.focus()
    }
    const up = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName
      if (tag === 'SELECT' || tag === 'INPUT' || tag === 'TEXTAREA') return
      if (e.isComposing || e.key === 'Process') return
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation()
      if (e.metaKey || e.ctrlKey || e.altKey || e.key === 'Meta' || e.key === 'Control' || e.key === 'Alt' || e.key === 'Process') return
      const payload = keyPayload(e, 'keyup')
      const vk = payload.vk as number
      if (vk) held.delete(vk)
      sendKey(payload)
    }
    const blur = () => {
      for (const vk of held) {
        sendKey({ action: 'keyup', vk, ctrl: false, alt: false, shift: false, meta: false })
      }
      held.clear()
    }
    window.addEventListener('keydown', down, true)
    window.addEventListener('keyup', up, true)
    window.addEventListener('blur', blur)
    const unsubHotkey = EventsOn('session:hotkey', (data: Record<string, unknown>) => {
      if (!data) return
      const vk = Number(data.vk || 0)
      if (data.action === 'keydown' && vk) held.add(vk)
      if (data.action === 'keyup' && vk) held.delete(vk)
      sendKey(data)
    })
    return () => {
      unsubHotkey()
      window.removeEventListener('keydown', down, true)
      window.removeEventListener('keyup', up, true)
      window.removeEventListener('blur', blur)
    }
  }, [sessionId])

  function changeQuality(next: string) {
    const q = saveStreamQuality(next)
    setQuality(q)
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'quality', quality: q }))
    }
    pageRef.current?.focus()
  }

  function changeMonitor(next: number) {
    setMonitor(next)
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'monitor', index: next }))
    }
    pageRef.current?.focus()
  }

  return (
    <div
      ref={pageRef}
      className="session-page"
      tabIndex={0}
    >
      <div className="stage" ref={stageRef}>
        <div className="canvas-wrap">
          <canvas
            ref={canvasRef}
            className="remote-canvas"
            onPointerLeave={() => setCursor((c) => ({ ...c, show: false }))}
          />
          {cursor.show && hasFrame && (
            <div
              className={`local-cursor${cursor.shape === 'text' ? ' is-text' : ''}`}
              style={{ transform: `translate(${cursor.x}px, ${cursor.y}px)` }}
            >
              {cursor.shape === 'text' ? (
                <svg viewBox="0 0 12 24" aria-hidden="true">
                  <path
                    d="M1.5 1.5 H10.5 M6 1.5 V22.5 M1.5 22.5 H10.5"
                    fill="none"
                    stroke="#ededed"
                    strokeWidth="2.6"
                    strokeLinecap="square"
                  />
                  <path
                    d="M1.5 1.5 H10.5 M6 1.5 V22.5 M1.5 22.5 H10.5"
                    fill="none"
                    stroke="#1a1a1a"
                    strokeWidth="1.35"
                    strokeLinecap="square"
                  />
                </svg>
              ) : (
                <svg viewBox="0 0 22 28" aria-hidden="true">
                  <path
                    d="M1.2 1.2 L1.2 22.4 L6.4 17.6 L10.6 26.6 L14.6 24.8 L10.2 16.2 L17.8 16.2 Z"
                    fill="#1a1a1a"
                    stroke="#ededed"
                    strokeWidth="1.35"
                    strokeLinejoin="round"
                  />
                </svg>
              )}
            </div>
          )}
          {clickFlash && (
            <div
              className="click-flash"
              style={{ left: clickFlash.x, top: clickFlash.y }}
            />
          )}
        </div>
        {status !== 'connected' && (
          <div className="overlay muted">
            {status === 'connecting' ? '正在连接远程画面…' : status.startsWith('disconnected') ? '连接已断开' : '连接出错'}
          </div>
        )}
        {status === 'connected' && !hasFrame && (
          <div className="overlay muted">已连接，等待画面帧…</div>
        )}
        <div className="session-hud" onPointerDown={(e) => e.stopPropagation()}>
          <label>
            分辨率
            <select
              value={quality}
              onChange={(e) => changeQuality(e.target.value)}
            >
              {STREAM_QUALITIES.map((opt) => (
                <option key={opt.id} value={opt.id}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
          {displays.length > 1 && (
            <label>
              显示器
              <select
                value={monitor}
                onChange={(e) => changeMonitor(Number(e.target.value))}
              >
                <option value={-1}>全部</option>
                {displays.map((d) => (
                  <option key={d.index} value={d.index}>
                    {displayLabel(d)}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      </div>
    </div>
  )
}
