package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	StatusDisabled = 0
	StatusActive   = 1

	DeviceOffline = 0
	DeviceOnline  = 1
)

type User struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	DisplayName  string    `gorm:"size:128" json:"display_name"`
	Role         string    `gorm:"size:16;not null;default:user" json:"role"`
	Status       int8      `gorm:"not null;default:1" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Device struct {
	ID            uint64     `gorm:"primaryKey" json:"id"`
	UserID        uint64     `gorm:"index;not null" json:"user_id"`
	DeviceUID     string     `gorm:"size:64;uniqueIndex;not null" json:"device_uid"`
	Name          string     `gorm:"size:128" json:"name"`
	OSVersion     string     `gorm:"size:64" json:"os_version"`
	AgentVersion  string     `gorm:"size:32" json:"agent_version"`
	LastIP        string     `gorm:"size:45" json:"last_ip"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	Status        int8       `gorm:"not null;default:0" json:"status"`
	AuthTokenHash string     `gorm:"size:255;not null" json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

type BindCode struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	UserID    uint64    `gorm:"index;not null" json:"user_id"`
	Code      string    `gorm:"size:16;uniqueIndex;not null" json:"code"`
	ExpiresAt time.Time `gorm:"index;not null" json:"expires_at"`
	Used      bool      `gorm:"not null;default:false" json:"used"`
	CreatedAt time.Time `json:"created_at"`
}

type RemoteSession struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	UserID      uint64     `gorm:"index;not null" json:"user_id"`
	DeviceID    uint64     `gorm:"index;not null" json:"device_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	Quality     string     `gorm:"size:16" json:"quality"`
	BytesRelayed int64     `gorm:"not null;default:0" json:"bytes_relayed"`
	EndReason   string     `gorm:"size:64" json:"end_reason"`
	RoomID      string     `gorm:"size:64;index" json:"room_id"`
}

type AuditLog struct {
	ID           uint64         `gorm:"primaryKey" json:"id"`
	ActorType    string         `gorm:"size:16;index" json:"actor_type"`
	ActorID      *uint64        `gorm:"index" json:"actor_id"`
	Action       string         `gorm:"size:64;index" json:"action"`
	ResourceType string         `gorm:"size:32" json:"resource_type"`
	ResourceID   *uint64        `json:"resource_id"`
	IP           string         `gorm:"size:45" json:"ip"`
	UserAgent    string         `gorm:"size:255" json:"user_agent"`
	Detail       datatypes.JSON `json:"detail"`
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
}

type RefreshToken struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	UserID    uint64    `gorm:"index;not null" json:"user_id"`
	TokenHash string    `gorm:"size:255;uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time `gorm:"index;not null" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
