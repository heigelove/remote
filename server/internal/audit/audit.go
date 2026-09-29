package audit

import (
	"encoding/json"
	"time"

	"github.com/remote/server/internal/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

type Entry struct {
	ActorType    string
	ActorID      *uint64
	Action       string
	ResourceType string
	ResourceID   *uint64
	IP           string
	UserAgent    string
	Detail       map[string]interface{}
}

func (s *Service) Log(e Entry) {
	var detail datatypes.JSON
	if e.Detail != nil {
		if b, err := json.Marshal(e.Detail); err == nil {
			detail = b
		}
	}
	_ = s.db.Create(&model.AuditLog{
		ActorType:    e.ActorType,
		ActorID:      e.ActorID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		IP:           e.IP,
		UserAgent:    e.UserAgent,
		Detail:       detail,
		CreatedAt:    time.Now(),
	}).Error
}

func Ptr[T any](v T) *T { return &v }
