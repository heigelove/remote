package session

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/remote/server/internal/hub"
	"github.com/remote/server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	db  *gorm.DB
	hub *hub.Hub
}

func NewService(db *gorm.DB, h *hub.Hub) *Service {
	return &Service{db: db, hub: h}
}

type StartResult struct {
	Session *model.RemoteSession `json:"session"`
	RoomID  string               `json:"room_id"`
}

func (s *Service) Start(userID, deviceID uint64, quality string) (*StartResult, error) {
	var device model.Device
	if err := s.db.Where("id = ? AND user_id = ?", deviceID, userID).First(&device).Error; err != nil {
		return nil, errors.New("device not found")
	}
	if !s.hub.IsAgentOnline(device.ID) && device.Status != model.DeviceOnline {
		return nil, errors.New("device offline")
	}
	if quality == "" {
		quality = "1080p"
	}
	quality = normalizeSessionQuality(quality)
	roomID := uuid.NewString()
	sess := &model.RemoteSession{
		UserID:    userID,
		DeviceID:  deviceID,
		StartedAt: time.Now(),
		Quality:   quality,
		RoomID:    roomID,
	}
	if err := s.db.Create(sess).Error; err != nil {
		return nil, err
	}

	_ = s.hub.NotifyAgentSession(deviceID, map[string]interface{}{
		"type":       "session.start",
		"session_id": sess.ID,
		"room_id":    roomID,
		"quality":    quality,
	})

	return &StartResult{Session: sess, RoomID: roomID}, nil
}

func normalizeSessionQuality(q string) string {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "native", "original", "source", "raw":
		return "native"
	case "720p", "hd", "720":
		return "720p"
	case "480p", "sd", "480":
		return "480p"
	default:
		return "1080p"
	}
}

func (s *Service) End(sessionID uint64, reason string) error {
	now := time.Now()
	return s.db.Model(&model.RemoteSession{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"ended_at":   now,
		"end_reason": reason,
	}).Error
}

func (s *Service) Get(id uint64) (*model.RemoteSession, error) {
	var sess model.RemoteSession
	if err := s.db.First(&sess, id).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Service) AddBytes(sessionID uint64, n int64) {
	_ = s.db.Model(&model.RemoteSession{}).Where("id = ?", sessionID).
		UpdateColumn("bytes_relayed", gorm.Expr("bytes_relayed + ?", n)).Error
}
