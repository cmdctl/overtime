package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

type Invite struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Code      string         `gorm:"uniqueIndex;not null;size:64" json:"code"`
	FullName  string         `gorm:"not null;size:200" json:"full_name"`
	Role      Role           `gorm:"not null;size:20" json:"role"`
	Used      bool           `gorm:"default:false" json:"used"`
	CreatedBy uint           `gorm:"not null" json:"created_by"`
	Creator   User           `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	ExpiresAt time.Time      `gorm:"not null" json:"expires_at"`
	TeamID    *uint          `gorm:"index" json:"team_id"`
	Team      *Team          `gorm:"foreignKey:TeamID" json:"team,omitempty"`
	ProjectID *uint          `gorm:"index" json:"project_id"`
	Project   *Project       `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
}

func GenerateInviteCode() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (i *Invite) IsValid() bool {
	return !i.Used && time.Now().Before(i.ExpiresAt)
}
