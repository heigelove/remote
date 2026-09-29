package device

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/remote/server/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type RegisterInput struct {
	UserID       uint64 // when set, register under this user (JWT self-register)
	BindCode     string
	Username     string
	Password     string
	DeviceUID    string
	Name         string
	OSVersion    string
	AgentVersion string
	IP           string
}

type RegisterResult struct {
	Device      *model.Device `json:"device"`
	DeviceToken string        `json:"device_token"`
}

func (s *Service) CreateBindCode(userID uint64) (*model.BindCode, error) {
	code, err := randomCode(6)
	if err != nil {
		return nil, err
	}
	bc := &model.BindCode{
		UserID:    userID,
		Code:      strings.ToUpper(code),
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Used:      false,
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(bc).Error; err != nil {
		return nil, err
	}
	return bc, nil
}

func (s *Service) resolveOwner(in RegisterInput) (userID uint64, bind *model.BindCode, err error) {
	if in.BindCode != "" {
		var bc model.BindCode
		if err := s.db.Where("code = ? AND used = ?", strings.ToUpper(in.BindCode), false).First(&bc).Error; err != nil {
			return 0, nil, errors.New("invalid bind code")
		}
		if time.Now().After(bc.ExpiresAt) {
			return 0, nil, errors.New("bind code expired")
		}
		return bc.UserID, &bc, nil
	}
	if in.Username != "" && in.Password != "" {
		var u model.User
		if err := s.db.Where("username = ?", in.Username).First(&u).Error; err != nil {
			return 0, nil, errors.New("invalid credentials")
		}
		if u.Status != model.StatusActive {
			return 0, nil, errors.New("user disabled")
		}
		if u.Role != model.RoleUser && u.Role != model.RoleAdmin {
			return 0, nil, errors.New("invalid credentials")
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
			return 0, nil, errors.New("invalid credentials")
		}
		// Admin may also bind devices to self for testing; prefer normal users.
		return u.ID, nil, nil
	}
	return 0, nil, errors.New("provide bind_code or username/password")
}

func (s *Service) Register(in RegisterInput) (*RegisterResult, error) {
	if in.DeviceUID == "" {
		return nil, errors.New("device_uid required")
	}
	var userID uint64
	var bind *model.BindCode
	var err error
	if in.UserID != 0 {
		userID = in.UserID
	} else {
		userID, bind, err = s.resolveOwner(in)
		if err != nil {
			return nil, err
		}
	}

	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var device model.Device
	err = s.db.Where("device_uid = ?", in.DeviceUID).First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		name := in.Name
		if name == "" {
			name = in.DeviceUID
		}
		device = model.Device{
			UserID:        userID,
			DeviceUID:     in.DeviceUID,
			Name:          name,
			OSVersion:     in.OSVersion,
			AgentVersion:  in.AgentVersion,
			LastIP:        in.IP,
			LastSeenAt:    &now,
			Status:        model.DeviceOffline,
			AuthTokenHash: string(hash),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.db.Create(&device).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if device.UserID != userID {
			return nil, errors.New("device already bound to another user")
		}
		updates := map[string]interface{}{
			"auth_token_hash": string(hash),
			"os_version":      in.OSVersion,
			"agent_version":   in.AgentVersion,
			"last_ip":         in.IP,
			"last_seen_at":    now,
			"updated_at":      now,
		}
		if in.Name != "" {
			updates["name"] = in.Name
		}
		if err := s.db.Model(&device).Updates(updates).Error; err != nil {
			return nil, err
		}
		_ = s.db.First(&device, device.ID).Error
	}

	if bind != nil {
		_ = s.db.Model(bind).Update("used", true).Error
	}
	fullToken := FormatDeviceToken(device.ID, token)
	return &RegisterResult{Device: &device, DeviceToken: fullToken}, nil
}

func (s *Service) ListByUser(userID uint64) ([]model.Device, error) {
	var list []model.Device
	err := s.db.Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func (s *Service) Heartbeat(deviceID uint64, token, ip string) error {
	d, err := s.AuthenticateDevice(deviceID, token)
	if err != nil {
		return err
	}
	now := time.Now()
	return s.db.Model(d).Updates(map[string]interface{}{
		"last_seen_at": now,
		"last_ip":      ip,
		"status":       model.DeviceOnline,
		"updated_at":   now,
	}).Error
}

func (s *Service) SetOnline(deviceID uint64, online bool, ip string) {
	now := time.Now()
	status := model.DeviceOffline
	if online {
		status = model.DeviceOnline
	}
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": now,
	}
	if online {
		updates["last_seen_at"] = now
		if ip != "" {
			updates["last_ip"] = ip
		}
	}
	_ = s.db.Model(&model.Device{}).Where("id = ?", deviceID).Updates(updates).Error
}

func (s *Service) AuthenticateDevice(deviceID uint64, rawToken string) (*model.Device, error) {
	var d model.Device
	if err := s.db.First(&d, deviceID).Error; err != nil {
		return nil, errors.New("device not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(d.AuthTokenHash), []byte(rawToken)) != nil {
		return nil, errors.New("invalid device token")
	}
	return &d, nil
}

func (s *Service) AuthenticateFullToken(full string) (*model.Device, error) {
	id, raw, err := ParseDeviceToken(full)
	if err != nil {
		return nil, err
	}
	return s.AuthenticateDevice(id, raw)
}

func (s *Service) Get(id uint64) (*model.Device, error) {
	var d model.Device
	if err := s.db.First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) GetOwned(userID, deviceID uint64) (*model.Device, error) {
	var d model.Device
	if err := s.db.Where("id = ? AND user_id = ?", deviceID, userID).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func FormatDeviceToken(deviceID uint64, raw string) string {
	return fmt.Sprintf("%d.%s", deviceID, raw)
}

func ParseDeviceToken(full string) (uint64, string, error) {
	parts := strings.SplitN(full, ".", 2)
	if len(parts) != 2 {
		return 0, "", errors.New("invalid token")
	}
	var id uint64
	if _, err := fmt.Sscanf(parts[0], "%d", &id); err != nil {
		return 0, "", err
	}
	return id, parts[1], nil
}

func randomCode(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
