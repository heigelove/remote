package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type AgentRuntime struct {
	mu         sync.Mutex
	running    atomic.Bool
	stopCh     chan struct{}
	status     string
	lastError  string
	streaming  atomic.Bool
	frames     atomic.Int64
	clipSend   func(string)
	clipSet    chan string
	clipIgnore string
	emitJSON   func(map[string]interface{})
	lastCursor atomic.Value
}

var runtimeAgent = &AgentRuntime{status: "stopped"}

func (a *AgentRuntime) Status() map[string]interface{} {
	cfg := getConfig()
	return map[string]interface{}{
		"status":      a.status,
		"running":     a.running.Load(),
		"streaming":   a.streaming.Load(),
		"frames":      a.frames.Load(),
		"last_error":  a.lastError,
		"device_uid":  cfg.DeviceUID,
		"device_name": cfg.DeviceName,
		"server_url":  cfg.ServerURL,
		"bound":       cfg.DeviceToken != "",
		"logged_in":   cfg.AccessToken != "",
		"auto_start":  cfg.AutoStart,
		"version":     agentVersion,
		"locked":      isWorkstationLocked(),
	}
}

func (a *AgentRuntime) setStatus(s, errMsg string) {
	a.mu.Lock()
	a.status = s
	a.lastError = errMsg
	a.mu.Unlock()
	if errMsg != "" {
		LogError("status=%s err=%s", s, errMsg)
	}
}

func (a *AgentRuntime) Start() error {
	cfg := getConfig()
	if cfg.DeviceToken == "" {
		return fmt.Errorf("本机尚未随账号上线，请先登录")
	}
	if a.running.Load() {
		return nil
	}
	a.stopCh = make(chan struct{})
	a.running.Store(true)
	a.setStatus("connecting", "")
	startInputWorker()
	go ensureInputServiceRunning()
	go a.loop()
	return nil
}

func (a *AgentRuntime) Stop() {
	if !a.running.Load() {
		return
	}
	a.running.Store(false)
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
	a.streaming.Store(false)
	a.setStatus("stopped", "")
}

func (a *AgentRuntime) loop() {
	for a.running.Load() {
		err := a.runOnce()
		if !a.running.Load() {
			break
		}
		a.setStatus("reconnecting", err.Error())
		select {
		case <-a.stopCh:
			return
		case <-time.After(3 * time.Second):
		}
	}
	a.setStatus("stopped", "")
}

type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsConn) writeJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.conn.WriteJSON(v)
}

func (w *wsConn) writeBinary(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (a *AgentRuntime) runOnce() error {
	cfg := getConfig()
	u, err := url.Parse(cfg.ServerURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/agent"
	u.RawQuery = ""
	q := u.Query()
	q.Set("device_token", cfg.DeviceToken)
	u.RawQuery = q.Encode()

	raw, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer raw.Close()
	raw.SetReadLimit(1024 * 1024)
	ws := &wsConn{conn: raw}
	a.setStatus("online", "")
	LogInfo("connected to %s", u.Host)

	a.mu.Lock()
	a.clipSend = func(text string) {
		_ = ws.writeJSON(map[string]interface{}{"type": "clipboard", "text": text})
	}
	a.emitJSON = func(v map[string]interface{}) {
		_ = ws.writeJSON(v)
	}
	a.clipSet = make(chan string, 2)
	clipSet := a.clipSet
	a.lastCursor.Store("")
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.clipSend = nil
		a.emitJSON = nil
		a.clipSet = nil
		a.mu.Unlock()
	}()

	stop := make(chan struct{})
	defer close(stop)

	go a.clipboardLoop(stop, clipSet)

	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-a.stopCh:
				_ = raw.Close()
				return
			case <-t.C:
				_ = ws.writeJSON(map[string]string{"type": "ping"})
			}
		}
	}()

	go func() {
		t := time.NewTicker(125 * time.Millisecond) // ~8fps
		defer t.Stop()
		var fails int
		lastLog := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-a.stopCh:
				return
			case <-t.C:
				if !a.streaming.Load() {
					continue
				}
				frame, err := captureJPEG(streamJPEGQuality())
				if err != nil {
					fails++
					if time.Since(lastLog) > 3*time.Second {
						a.setStatus("online", fmt.Sprintf("capture: %v (fails=%d)", err, fails))
						lastLog = time.Now()
					}
					continue
				}
				if err := ws.writeBinary(frame); err != nil {
					return
				}
				a.frames.Add(1)
				fails = 0
				a.pushCursorShape()
			}
		}
	}()

	for {
		select {
		case <-a.stopCh:
			return fmt.Errorf("stopped")
		default:
		}
		mt, data, err := raw.ReadMessage()
		if err != nil {
			return err
		}
		if mt != websocket.TextMessage {
			if len(data) == 0 || data[0] != '{' {
				continue
			}
			LogInfo("ws treating binary as json len=%d", len(data))
		}
		var msg map[string]interface{}
		if json.Unmarshal(data, &msg) != nil {
			LogWarn("ws json unmarshal failed len=%d", len(data))
			continue
		}
		t, _ := msg["type"].(string)
		switch t {
		case "session.start":
			a.streaming.Store(true)
			a.setStatus("streaming", "")
			a.lastCursor.Store("")
			if q, _ := msg["quality"].(string); q != "" {
				setStreamQuality(q)
			}
			LogInfo("session start %v", msg["session_id"])
			_ = ws.writeJSON(map[string]interface{}{"type": "session.ack", "session_id": msg["session_id"]})
			go a.pushClipboardNow()
			go a.pushCursorShape()
			go a.pushDisplays()
		case "session.end":
			a.streaming.Store(false)
			a.setStatus("online", "")
			LogInfo("session end")
		case "quality":
			if q, _ := msg["quality"].(string); q != "" {
				setStreamQuality(q)
			}
		case "displays":
			a.pushDisplays()
		case "monitor":
			if idx, ok := jsonInt(msg["index"]); ok {
				setSelectedDisplay(idx)
			}
			a.pushDisplays()
		case "netping":
			a.mu.Lock()
			fn := a.emitJSON
			a.mu.Unlock()
			if fn != nil {
				fn(map[string]interface{}{"type": "netpong", "t": msg["t"]})
			}
		case "input":
			handleInput(msg)
		case "lock":
			LogInfo("lock command received")
			go lockWorkstation()
		case "clipboard":
			if text, ok := msg["text"].(string); ok {
				a.queueClipboard(text)
			}
		default:
			if t != "pong" {
				LogInfo("ws ignore type=%q keys=%d", t, len(msg))
			}
		}
	}
}

func (a *AgentRuntime) queueClipboard(text string) {
	text = clipTrim(text)
	a.mu.Lock()
	a.clipIgnore = text
	ch := a.clipSet
	a.mu.Unlock()
	if ch == nil {
		go func() { _ = setClipboardText(text) }()
		return
	}
	select {
	case ch <- text:
	default:
	}
}

func (a *AgentRuntime) pushDisplays() {
	a.mu.Lock()
	fn := a.emitJSON
	a.mu.Unlock()
	if fn == nil {
		return
	}
	items := listDisplays()
	LogInfo("displays n=%d selected=%d", len(items), selectedDisplayIndex())
	fn(map[string]interface{}{
		"type":     "displays",
		"items":    items,
		"selected": selectedDisplayIndex(),
	})
}

func (a *AgentRuntime) pushCursorShape() {
	shape := currentCursorShape()
	prev, _ := a.lastCursor.Load().(string)
	if shape == prev {
		return
	}
	a.lastCursor.Store(shape)
	a.mu.Lock()
	fn := a.emitJSON
	a.mu.Unlock()
	if fn != nil {
		fn(map[string]interface{}{"type": "cursor", "shape": shape})
	}
}

func (a *AgentRuntime) sendClip(text string) {
	a.mu.Lock()
	fn := a.clipSend
	a.mu.Unlock()
	if fn != nil {
		fn(text)
	}
}

func (a *AgentRuntime) pushClipboardNow() {
	text, err := getClipboardText()
	if err != nil || text == "" {
		return
	}
	text = clipTrim(text)
	a.mu.Lock()
	a.clipIgnore = text
	a.mu.Unlock()
	a.sendClip(text)
}

func (a *AgentRuntime) clipboardLoop(stop <-chan struct{}, clipSet <-chan string) {
	goruntime.LockOSThread()
	var lastSeq uint32
	var lastSent string
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case text := <-clipSet:
			text = clipTrim(text)
			if err := setClipboardText(text); err != nil {
				LogWarn("set clipboard: %v", err)
				continue
			}
			lastSent = text
			lastSeq = clipboardSequence()
		case <-t.C:
			if !a.streaming.Load() {
				continue
			}
			seq := clipboardSequence()
			if seq == lastSeq {
				continue
			}
			lastSeq = seq
			text, err := getClipboardText()
			if err != nil {
				continue
			}
			text = clipTrim(text)
			a.mu.Lock()
			ignore := a.clipIgnore
			a.mu.Unlock()
			if text == "" || text == lastSent || text == ignore {
				lastSent = text
				continue
			}
			lastSent = text
			a.sendClip(text)
		}
	}
}
