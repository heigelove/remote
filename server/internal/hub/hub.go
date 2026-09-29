package hub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Role string

const (
	RoleAgent  Role = "agent"
	RoleClient Role = "client"
)

type Conn struct {
	DeviceID  uint64
	SessionID uint64
	RoomID    string
	Role      Role
	WS        *websocket.Conn
	Send      chan []byte
	Closed    chan struct{}
}

type Hub struct {
	mu            sync.RWMutex
	agents        map[uint64]*Conn  // deviceID -> agent
	sessions      map[uint64]*Conn  // sessionID -> client
	roomAgent     map[string]uint64 // roomID -> deviceID
	roomSession   map[string]uint64 // roomID -> sessionID
	deviceSession map[uint64]uint64 // deviceID -> active sessionID
}

func New() *Hub {
	return &Hub{
		agents:        make(map[uint64]*Conn),
		sessions:      make(map[uint64]*Conn),
		roomAgent:     make(map[string]uint64),
		roomSession:   make(map[string]uint64),
		deviceSession: make(map[uint64]uint64),
	}
}

func (h *Hub) RegisterAgent(deviceID uint64, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.agents[deviceID]; ok {
		closeQuiet(old)
	}
	h.agents[deviceID] = c
}

func (h *Hub) UnregisterAgent(deviceID uint64, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.agents[deviceID]; ok && cur == c {
		delete(h.agents, deviceID)
	}
}

func (h *Hub) RegisterClient(sessionID uint64, roomID string, deviceID uint64, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.sessions[sessionID]; ok {
		closeQuiet(old)
	}
	h.sessions[sessionID] = c
	h.roomSession[roomID] = sessionID
	h.roomAgent[roomID] = deviceID
	h.deviceSession[deviceID] = sessionID
}

func (h *Hub) UnregisterClient(sessionID uint64, roomID string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.sessions[sessionID]; ok && cur == c {
		delete(h.sessions, sessionID)
		delete(h.roomSession, roomID)
		for did, sid := range h.deviceSession {
			if sid == sessionID {
				delete(h.deviceSession, did)
			}
		}
	}
}

func (h *Hub) ActiveSession(deviceID uint64) (uint64, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	sid, ok := h.deviceSession[deviceID]
	return sid, ok
}

func (h *Hub) IsAgentOnline(deviceID uint64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.agents[deviceID]
	return ok
}

func (h *Hub) KickAgent(deviceID uint64) {
	h.mu.Lock()
	c, ok := h.agents[deviceID]
	if ok {
		delete(h.agents, deviceID)
	}
	sid, hasSess := h.deviceSession[deviceID]
	var sess *Conn
	if hasSess {
		sess = h.sessions[sid]
	}
	h.mu.Unlock()
	if ok {
		closeQuiet(c)
	}
	if sess != nil {
		closeQuiet(sess)
	}
}

func (h *Hub) RelayToAgent(deviceID uint64, data []byte) bool {
	h.mu.RLock()
	c, ok := h.agents[deviceID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	return trySend(c, data)
}

func (h *Hub) RelayToSession(sessionID uint64, data []byte) bool {
	h.mu.RLock()
	c, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	return trySend(c, data)
}

func (h *Hub) FindSessionByRoom(roomID string) (uint64, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	id, ok := h.roomSession[roomID]
	return id, ok
}

func (h *Hub) NotifyAgentSession(deviceID uint64, payload interface{}) bool {
	b, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return h.RelayToAgent(deviceID, b)
}

func trySend(c *Conn, data []byte) bool {
	select {
	case c.Send <- data:
		return true
	case <-c.Closed:
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}

func closeQuiet(c *Conn) {
	select {
	case <-c.Closed:
	default:
		close(c.Closed)
	}
	_ = c.WS.Close()
}

func WritePump(c *Conn) {
	defer func() {
		_ = c.WS.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				return
			}
			_ = c.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WS.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}
		case <-c.Closed:
			return
		}
	}
}

// WriteJSONPump sends text JSON frames from Send channel when messages look like JSON text.
func WriteTextPump(c *Conn) {
	defer func() { _ = c.WS.Close() }()
	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				return
			}
			_ = c.WS.SetWriteDeadline(time.Now().Add(10 * time.Second))
			mt := websocket.BinaryMessage
			if len(msg) > 0 && msg[0] == '{' {
				mt = websocket.TextMessage
			}
			if err := c.WS.WriteMessage(mt, msg); err != nil {
				return
			}
		case <-c.Closed:
			return
		}
	}
}
