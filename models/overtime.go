package models

import (
	"time"

	"gorm.io/gorm"
)

type OvertimeEntry struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	UserID      uint           `gorm:"not null;index" json:"user_id"`
	User        User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Date        time.Time      `gorm:"not null;type:date" json:"date"`
	Hours       float64        `gorm:"not null" json:"hours"`
	Description string         `gorm:"size:500" json:"description"`
}

type OvertimeFilter struct {
	UserID uint
	Month  int
	Year   int
}
