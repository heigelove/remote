package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/remote/server/internal/admin"
	"github.com/remote/server/internal/audit"
	"github.com/remote/server/internal/auth"
	"github.com/remote/server/internal/device"
	"github.com/remote/server/internal/hub"
	"github.com/remote/server/internal/middleware"
	"github.com/remote/server/internal/model"
	"github.com/remote/server/internal/session"
	"github.com/remote/server/internal/user"
	"github.com/remote/server/pkg/response"
	"gorm.io/gorm"
)

// Package api exposes HTTP/WebSocket handlers for the Remote server.

type Server struct {
	DB       *gorm.DB
	JWT      *auth.Service
	Users    *user.Service
	Devices  *device.Service
	Sessions *session.Service
	Admin    *admin.Service
	Audit    *audit.Service
	Hub      *hub.Hub
}

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
}

func (s *Server) RegisterRoutes(r *gin.Engine) {
	r.Use(middleware.CORS())
	r.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "up"})
	})

	api := r.Group("/api/v1")
	{
		api.POST("/auth/login", s.login)
		api.POST("/auth/refresh", s.refresh)

		authed := api.Group("")
		authed.Use(middleware.Auth(s.JWT))
		{
			authed.GET("/me", s.me)
			authed.GET("/devices", s.listMyDevices)
			authed.POST("/devices/bind-code", s.createBindCode)
			authed.POST("/devices/register-self", s.registerSelf)
			authed.POST("/devices/:id/lock", s.lockDevice)
			authed.POST("/sessions", s.startSession)
		}

		api.POST("/devices/register", s.registerDevice)
		api.PUT("/devices/:id/heartbeat", s.heartbeat)

		adminAPI := api.Group("/admin")
		{
			adminAPI.POST("/auth/login", s.adminLogin)
			adm := adminAPI.Group("")
			adm.Use(middleware.AdminAuth(s.JWT))
			{
				adm.GET("/stats", s.adminStats)
				adm.GET("/users", s.adminListUsers)
				adm.POST("/users", s.adminCreateUser)
				adm.GET("/users/:id", s.adminGetUser)
				adm.PUT("/users/:id/status", s.adminSetUserStatus)
				adm.DELETE("/users/:id", s.adminDeleteUser)
				adm.GET("/devices", s.adminListDevices)
				adm.GET("/devices/:id", s.adminGetDevice)
				adm.DELETE("/devices/:id", s.adminDeleteDevice)
				adm.GET("/logs", s.adminListLogs)
			}
		}
	}

	r.GET("/ws/agent", s.wsAgent)
	r.GET("/ws/session", s.wsSession)
}

func clientIP(c *gin.Context) string {
	return c.ClientIP()
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	u, pair, err := s.Users.Login(req.Username, req.Password)
	if err != nil {
		s.Audit.Log(audit.Entry{
			ActorType: "user", Action: "login.fail", IP: clientIP(c), UserAgent: c.Request.UserAgent(),
			Detail: map[string]interface{}{"username": req.Username, "error": err.Error()},
		})
		response.Unauthorized(c, err.Error())
		return
	}
	if u.Role == model.RoleAdmin {
		// allow admin to use client login too, but typically users only
	}
	s.Audit.Log(audit.Entry{
		ActorType: "user", ActorID: audit.Ptr(u.ID), Action: "login",
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"user": u, "token": pair})
}

func (s *Server) refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		response.BadRequest(c, "refresh_token required")
		return
	}
	pair, err := s.Users.Refresh(req.RefreshToken)
	if err != nil {
		response.Unauthorized(c, err.Error())
		return
	}
	response.OK(c, pair)
}

func (s *Server) me(c *gin.Context) {
	u, err := s.Users.GetByID(middleware.UserID(c))
	if err != nil {
		response.NotFound(c, "user not found")
		return
	}
	response.OK(c, u)
}

func (s *Server) listMyDevices(c *gin.Context) {
	list, err := s.Devices.ListByUser(middleware.UserID(c))
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	for i := range list {
		if s.Hub.IsAgentOnline(list[i].ID) {
			list[i].Status = model.DeviceOnline
		}
	}
	response.OK(c, list)
}

func (s *Server) createBindCode(c *gin.Context) {
	bc, err := s.Devices.CreateBindCode(middleware.UserID(c))
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "user", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "device.bind_code", ResourceType: "bind_code", ResourceID: audit.Ptr(bc.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, bc)
}

func (s *Server) registerDevice(c *gin.Context) {
	var req struct {
		BindCode     string `json:"bind_code"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		DeviceUID    string `json:"device_uid"`
		Name         string `json:"name"`
		OSVersion    string `json:"os_version"`
		AgentVersion string `json:"agent_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	res, err := s.Devices.Register(device.RegisterInput{
		BindCode: req.BindCode, Username: req.Username, Password: req.Password,
		DeviceUID: req.DeviceUID, Name: req.Name,
		OSVersion: req.OSVersion, AgentVersion: req.AgentVersion, IP: clientIP(c),
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	method := "bind_code"
	if req.Username != "" {
		method = "password"
	}
	s.Audit.Log(audit.Entry{
		ActorType: "agent", Action: "device.register",
		ResourceType: "device", ResourceID: audit.Ptr(res.Device.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
		Detail: map[string]interface{}{"device_uid": req.DeviceUID, "method": method},
	})
	response.OK(c, res)
}

func (s *Server) registerSelf(c *gin.Context) {
	var req struct {
		DeviceUID    string `json:"device_uid"`
		Name         string `json:"name"`
		OSVersion    string `json:"os_version"`
		AgentVersion string `json:"agent_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	res, err := s.Devices.Register(device.RegisterInput{
		UserID:       middleware.UserID(c),
		DeviceUID:    req.DeviceUID,
		Name:         req.Name,
		OSVersion:    req.OSVersion,
		AgentVersion: req.AgentVersion,
		IP:           clientIP(c),
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "user", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "device.register", ResourceType: "device", ResourceID: audit.Ptr(res.Device.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
		Detail: map[string]interface{}{"device_uid": req.DeviceUID, "method": "user_session"},
	})
	response.OK(c, res)
}

func (s *Server) heartbeat(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	token := c.GetHeader("X-Device-Token")
	if token == "" {
		token = c.Query("device_token")
	}
	_, raw, err := device.ParseDeviceToken(token)
	if err != nil {
		// allow raw token with id in path
		raw = token
	}
	if err := s.Devices.Heartbeat(id, raw, clientIP(c)); err != nil {
		response.Unauthorized(c, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (s *Server) startSession(c *gin.Context) {
	var req struct {
		DeviceID uint64 `json:"device_id"`
		Quality  string `json:"quality"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceID == 0 {
		response.BadRequest(c, "device_id required")
		return
	}
	res, err := s.Sessions.Start(middleware.UserID(c), req.DeviceID, req.Quality)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "user", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "session.start", ResourceType: "session", ResourceID: audit.Ptr(res.Session.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
		Detail: map[string]interface{}{"device_id": req.DeviceID},
	})
	response.OK(c, res)
}

func (s *Server) lockDevice(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		response.BadRequest(c, "invalid device id")
		return
	}
	d, err := s.Devices.GetOwned(middleware.UserID(c), id)
	if err != nil {
		response.NotFound(c, "device not found")
		return
	}
	if !s.Hub.IsAgentOnline(d.ID) {
		response.BadRequest(c, "设备离线，无法锁屏")
		return
	}
	if !s.Hub.NotifyAgentSession(d.ID, map[string]interface{}{"type": "lock"}) {
		response.ServerError(c, "锁屏命令发送失败")
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "user", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "device.lock", ResourceType: "device", ResourceID: audit.Ptr(d.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"ok": true})
}

func (s *Server) adminLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	u, pair, err := s.Users.Login(req.Username, req.Password)
	if err != nil {
		s.Audit.Log(audit.Entry{
			ActorType: "admin", Action: "login.fail", IP: clientIP(c), UserAgent: c.Request.UserAgent(),
			Detail: map[string]interface{}{"username": req.Username},
		})
		response.Unauthorized(c, err.Error())
		return
	}
	if u.Role != model.RoleAdmin {
		s.Audit.Log(audit.Entry{
			ActorType: "admin", ActorID: audit.Ptr(u.ID), Action: "login.fail",
			IP: clientIP(c), UserAgent: c.Request.UserAgent(),
			Detail: map[string]interface{}{"reason": "not admin"},
		})
		response.Forbidden(c, "admin role required")
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "admin", ActorID: audit.Ptr(u.ID), Action: "login",
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"user": u, "token": pair})
}

func (s *Server) adminStats(c *gin.Context) {
	st, err := s.Admin.Stats()
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.OK(c, st)
}

func (s *Server) adminListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	res, err := s.Admin.ListUsers(c.Query("q"), page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.OK(c, res)
}

func (s *Server) adminGetUser(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	u, err := s.Admin.GetUser(id)
	if err != nil {
		response.NotFound(c, "user not found")
		return
	}
	response.OK(c, u)
}

func (s *Server) adminSetUserStatus(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req struct {
		Status int8 `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "status required")
		return
	}
	if err := s.Admin.SetUserStatus(id, req.Status); err != nil {
		response.ServerError(c, err.Error())
		return
	}
	action := "admin.enable_user"
	if req.Status == model.StatusDisabled {
		action = "admin.disable_user"
	}
	s.Audit.Log(audit.Entry{
		ActorType: "admin", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: action, ResourceType: "user", ResourceID: audit.Ptr(id),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"ok": true})
}

func (s *Server) adminCreateUser(c *gin.Context) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	u, err := s.Admin.CreateUser(req.Username, req.Password, req.DisplayName)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "admin", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "admin.create_user", ResourceType: "user", ResourceID: audit.Ptr(u.ID),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
		Detail: map[string]interface{}{"username": u.Username},
	})
	response.OK(c, u)
}

func (s *Server) adminDeleteUser(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	if id == middleware.UserID(c) {
		response.BadRequest(c, "cannot delete yourself")
		return
	}
	devices, err := s.Admin.ListUserDeviceIDs(id)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	if err := s.Admin.DeleteUser(id); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	for _, did := range devices {
		s.Hub.KickAgent(did)
	}
	s.Audit.Log(audit.Entry{
		ActorType: "admin", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "admin.delete_user", ResourceType: "user", ResourceID: audit.Ptr(id),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"ok": true})
}

func (s *Server) adminListDevices(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	userID, _ := strconv.ParseUint(c.Query("user_id"), 10, 64)
	var onlinePtr *int
	if v := c.Query("online"); v != "" {
		n, _ := strconv.Atoi(v)
		onlinePtr = &n
	}
	res, err := s.Admin.ListDevices(userID, onlinePtr, page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.OK(c, res)
}

func (s *Server) adminGetDevice(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	d, err := s.Admin.GetDevice(id)
	if err != nil {
		response.NotFound(c, "device not found")
		return
	}
	response.OK(c, d)
}

func (s *Server) adminDeleteDevice(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := s.Admin.DeleteDevice(id); err != nil {
		response.ServerError(c, err.Error())
		return
	}
	s.Audit.Log(audit.Entry{
		ActorType: "admin", ActorID: audit.Ptr(middleware.UserID(c)),
		Action: "admin.delete_device", ResourceType: "device", ResourceID: audit.Ptr(id),
		IP: clientIP(c), UserAgent: c.Request.UserAgent(),
	})
	response.OK(c, gin.H{"ok": true})
}

func (s *Server) adminListLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	actorID, _ := strconv.ParseUint(c.Query("actor_id"), 10, 64)
	res, err := s.Admin.ListLogs(c.Query("action"), actorID, page, pageSize)
	if err != nil {
		response.ServerError(c, err.Error())
		return
	}
	response.OK(c, res)
}

func (s *Server) wsAgent(c *gin.Context) {
	token := c.Query("device_token")
	d, err := s.Devices.AuthenticateFullToken(token)
	if err != nil {
		response.Unauthorized(c, err.Error())
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1024 * 1024)
	hc := &hub.Conn{
		DeviceID: d.ID,
		Role:     hub.RoleAgent,
		WS:       conn,
		Send:     make(chan []byte, 64),
		Closed:   make(chan struct{}),
	}
	s.Hub.RegisterAgent(d.ID, hc)
	s.Devices.SetOnline(d.ID, true, clientIP(c))
	defer func() {
		s.Hub.UnregisterAgent(d.ID, hc)
		s.Devices.SetOnline(d.ID, false, "")
		close(hc.Closed)
	}()

	go hub.WriteTextPump(hc)

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if mt == websocket.TextMessage {
			var msg map[string]interface{}
			if json.Unmarshal(data, &msg) == nil {
				t, _ := msg["type"].(string)
				if t == "ping" {
					_ = trySendJSON(hc, map[string]string{"type": "pong"})
				} else if t == "clipboard" || t == "cursor" || t == "netpong" || t == "displays" {
					if sid, ok := s.Hub.ActiveSession(d.ID); ok {
						if t == "clipboard" {
							if text, _ := msg["text"].(string); len(text) > 256*1024 {
								msg["text"] = text[:256*1024]
								if b, err := json.Marshal(msg); err == nil {
									data = b
								}
							}
						}
						_ = s.Hub.RelayToSession(sid, data)
					}
				}
			}
			continue
		}
		sid, ok := s.Hub.ActiveSession(d.ID)
		if !ok {
			continue
		}
		s.Sessions.AddBytes(sid, int64(len(data)))
		s.Hub.RelayToSession(sid, data)
	}
}

func trySendJSON(c *hub.Conn, v interface{}) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case c.Send <- b:
		return true
	case <-c.Closed:
		return false
	default:
		return false
	}
}

func (s *Server) wsSession(c *gin.Context) {
	token := c.Query("token")
	claims, err := s.JWT.Parse(token)
	if err != nil {
		response.Unauthorized(c, "invalid token")
		return
	}
	sessionID, _ := strconv.ParseUint(c.Query("session_id"), 10, 64)
	sess, err := s.Sessions.Get(sessionID)
	if err != nil || sess.UserID != claims.UserID {
		response.Forbidden(c, "session not found")
		return
	}
	if sess.EndedAt != nil {
		response.BadRequest(c, "session ended")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1024 * 1024)
	hc := &hub.Conn{
		DeviceID:  sess.DeviceID,
		SessionID: sess.ID,
		RoomID:    sess.RoomID,
		Role:      hub.RoleClient,
		WS:        conn,
		Send:      make(chan []byte, 64),
		Closed:    make(chan struct{}),
	}
	s.Hub.RegisterClient(sess.ID, sess.RoomID, sess.DeviceID, hc)
	defer func() {
		s.Hub.UnregisterClient(sess.ID, sess.RoomID, hc)
		_ = s.Sessions.End(sess.ID, "client_disconnect")
		_ = s.Hub.NotifyAgentSession(sess.DeviceID, map[string]interface{}{
			"type": "session.end", "session_id": sess.ID, "room_id": sess.RoomID,
		})
		s.Audit.Log(audit.Entry{
			ActorType: "user", ActorID: audit.Ptr(claims.UserID),
			Action: "session.end", ResourceType: "session", ResourceID: audit.Ptr(sess.ID),
			IP: clientIP(c),
		})
		close(hc.Closed)
	}()

	// tell agent which session is active for frame routing
	_ = s.Hub.NotifyAgentSession(sess.DeviceID, map[string]interface{}{
		"type": "session.start", "session_id": sess.ID, "room_id": sess.RoomID, "quality": sess.Quality,
	})

	go hub.WriteTextPump(hc)

	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		if mt == websocket.TextMessage {
			// control / input JSON -> agent
			var msg map[string]interface{}
			if json.Unmarshal(data, &msg) == nil {
				msg["session_id"] = sess.ID
				if t, _ := msg["type"].(string); t == "input" {
					action, _ := msg["action"].(string)
					if action != "mousemove" {
						log.Printf("relay input session=%d device=%d action=%v button=%v x=%v y=%v",
							sess.ID, sess.DeviceID, action, msg["button"], msg["x"], msg["y"])
					}
				}
				b, _ := json.Marshal(msg)
				if !s.Hub.RelayToAgent(sess.DeviceID, b) {
					if t, _ := msg["type"].(string); t == "input" {
						action, _ := msg["action"].(string)
						if action != "mousemove" {
							log.Printf("relay input FAILED session=%d device=%d (agent offline?)", sess.ID, sess.DeviceID)
						}
					}
				}
			}
			continue
		}
	}
}
