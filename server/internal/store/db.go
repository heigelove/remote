package store

import (
	"fmt"
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/remote/server/internal/config"
	"github.com/remote/server/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DB.Driver {
	case "mysql":
		dialector = mysql.Open(cfg.DB.DSN)
	case "sqlite":
		dialector = sqlite.Open(cfg.DB.DSN)
	default:
		return nil, fmt.Errorf("unsupported db driver: %s", cfg.DB.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&model.User{},
		&model.Device{},
		&model.BindCode{},
		&model.RemoteSession{},
		&model.AuditLog{},
		&model.RefreshToken{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := seed(db, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

func seed(db *gorm.DB, cfg *config.Config) error {
	ensure := func(username, password, display, role string) error {
		var count int64
		if err := db.Model(&model.User{}).Where("username = ?", username).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		u := model.User{
			Username:     username,
			PasswordHash: string(hash),
			DisplayName:  display,
			Role:         role,
			Status:       model.StatusActive,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := db.Create(&u).Error; err != nil {
			return err
		}
		log.Printf("seeded user %s / %s (role=%s)", username, password, role)
		return nil
	}

	adminUser := cfg.Seed.AdminUsername
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPass := cfg.Seed.AdminPassword
	if adminPass == "" {
		adminPass = "admin123"
	}
	demoUser := cfg.Seed.DemoUsername
	if demoUser == "" {
		demoUser = "demo"
	}
	demoPass := cfg.Seed.DemoPassword
	if demoPass == "" {
		demoPass = "demo123"
	}

	if err := ensure(adminUser, adminPass, "Administrator", model.RoleAdmin); err != nil {
		return err
	}
	return ensure(demoUser, demoPass, "Demo User", model.RoleUser)
}
