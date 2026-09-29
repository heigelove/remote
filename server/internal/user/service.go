package user

import (
	"errors"
	"time"

	"github.com/remote/server/internal/auth"
	"github.com/remote/server/internal/model"
	"gorm.io/gorm"
)

type Service struct {
	db     *gorm.DB
	jwtSvc *auth.Service
}

func NewService(db *gorm.DB, jwtSvc *auth.Service) *Service {
	return &Service{db: db, jwtSvc: jwtSvc}
}

func (s *Service) Login(username, password string) (*model.User, *auth.TokenPair, error) {
	var u model.User
	if err := s.db.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("invalid credentials")
		}
		return nil, nil, err
	}
	if u.Status != model.StatusActive {
		return nil, nil, errors.New("user disabled")
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		return nil, nil, errors.New("invalid credentials")
	}
	pair, refreshHash, err := s.jwtSvc.Issue(u.ID, u.Username, u.Role)
	if err != nil {
		return nil, nil, err
	}
	_ = s.db.Create(&model.RefreshToken{
		UserID:    u.ID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().Add(time.Duration(168) * time.Hour),
		CreatedAt: time.Now(),
	}).Error
	return &u, pair, nil
}

func (s *Service) Refresh(refreshToken string) (*auth.TokenPair, error) {
	claims, err := s.jwtSvc.Parse(refreshToken)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}
	hash := auth.HashToken(refreshToken)
	var rt model.RefreshToken
	if err := s.db.Where("token_hash = ? AND user_id = ?", hash, claims.UserID).First(&rt).Error; err != nil {
		return nil, errors.New("refresh token revoked")
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, errors.New("refresh token expired")
	}
	var u model.User
	if err := s.db.First(&u, claims.UserID).Error; err != nil {
		return nil, err
	}
	if u.Status != model.StatusActive {
		return nil, errors.New("user disabled")
	}
	pair, newHash, err := s.jwtSvc.Issue(u.ID, u.Username, u.Role)
	if err != nil {
		return nil, err
	}
	_ = s.db.Model(&rt).Updates(map[string]interface{}{
		"token_hash": newHash,
		"expires_at": time.Now().Add(168 * time.Hour),
	}).Error
	return pair, nil
}

func (s *Service) GetByID(id uint64) (*model.User, error) {
	var u model.User
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
