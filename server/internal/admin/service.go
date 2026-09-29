package admin

import (
	"errors"
	"strings"
	"time"

	"github.com/remote/server/internal/auth"
	"github.com/remote/server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type PageResult struct {
	Items    interface{} `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

type Stats struct {
	UserCount       int64 `json:"user_count"`
	OnlineDevices   int64 `json:"online_devices"`
	TotalDevices    int64 `json:"total_devices"`
	TodaySessions   int64 `json:"today_sessions"`
}

func (s *Service) Stats() (*Stats, error) {
	var st Stats
	_ = s.db.Model(&model.User{}).Where("role = ?", model.RoleUser).Count(&st.UserCount).Error
	_ = s.db.Model(&model.Device{}).Where("status = ?", model.DeviceOnline).Count(&st.OnlineDevices).Error
	_ = s.db.Model(&model.Device{}).Count(&st.TotalDevices).Error
	start := time.Now().Truncate(24 * time.Hour)
	_ = s.db.Model(&model.RemoteSession{}).Where("started_at >= ?", start).Count(&st.TodaySessions).Error
	return &st, nil
}

type UserRow struct {
	model.User
	DeviceCount int64 `json:"device_count"`
}

func (s *Service) ListUsers(q string, page, pageSize int) (*PageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	tx := s.db.Model(&model.User{}).Where("role = ?", model.RoleUser)
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}
	var users []model.User
	if err := tx.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, err
	}
	rows := make([]UserRow, 0, len(users))
	for _, u := range users {
		var cnt int64
		_ = s.db.Model(&model.Device{}).Where("user_id = ?", u.ID).Count(&cnt).Error
		rows = append(rows, UserRow{User: u, DeviceCount: cnt})
	}
	return &PageResult{Items: rows, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Service) GetUser(id uint64) (*UserRow, error) {
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, err
	}
	var cnt int64
	_ = s.db.Model(&model.Device{}).Where("user_id = ?", u.ID).Count(&cnt).Error
	return &UserRow{User: u, DeviceCount: cnt}, nil
}

func (s *Service) SetUserStatus(id uint64, status int8) error {
	return s.db.Model(&model.User{}).Where("id = ? AND role = ?", id, model.RoleUser).
		Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error
}

func (s *Service) CreateUser(username, password, displayName string) (*model.User, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if !validUsername(username) {
		return nil, errors.New("用户名需为 3–64 位字母、数字或下划线")
	}
	if len(password) < 6 || len(password) > 72 {
		return nil, errors.New("密码长度需为 6–72 位")
	}
	if displayName == "" {
		displayName = username
	}
	if len(displayName) > 128 {
		return nil, errors.New("显示名过长")
	}
	var n int64
	if err := s.db.Model(&model.User{}).Where("username = ?", username).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errors.New("用户名已存在")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	u := model.User{
		Username:     username,
		PasswordHash: hash,
		DisplayName:  displayName,
		Role:         model.RoleUser,
		Status:       model.StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func (s *Service) ListUserDeviceIDs(userID uint64) ([]uint64, error) {
	var ids []uint64
	err := s.db.Model(&model.Device{}).Where("user_id = ?", userID).Pluck("id", &ids).Error
	return ids, err
}

func (s *Service) DeleteUser(id uint64) error {
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return err
	}
	if u.Role == model.RoleAdmin {
		return errors.New("不能删除管理员")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.Device{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.BindCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.RefreshToken{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ? AND role = ?", id, model.RoleUser).Delete(&model.User{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("用户不存在")
		}
		return nil
	})
}

func (s *Service) ListDevices(userID uint64, online *int, page, pageSize int) (*PageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	tx := s.db.Model(&model.Device{}).Preload("User")
	if userID > 0 {
		tx = tx.Where("user_id = ?", userID)
	}
	if online != nil {
		tx = tx.Where("status = ?", *online)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}
	var list []model.Device
	if err := tx.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	return &PageResult{Items: list, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Service) GetDevice(id uint64) (*model.Device, error) {
	var d model.Device
	if err := s.db.Preload("User").First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) DeleteDevice(id uint64) error {
	return s.db.Delete(&model.Device{}, id).Error
}

func (s *Service) ListLogs(action string, actorID uint64, page, pageSize int) (*PageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	tx := s.db.Model(&model.AuditLog{})
	if action != "" {
		tx = tx.Where("action = ?", action)
	}
	if actorID > 0 {
		tx = tx.Where("actor_id = ?", actorID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}
	var list []model.AuditLog
	if err := tx.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	return &PageResult{Items: list, Total: total, Page: page, PageSize: pageSize}, nil
}
