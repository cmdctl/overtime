package models

import (
	"time"
)

type Team struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `gorm:"uniqueIndex;not null;size:100" json:"name"`
	Users     []User    `gorm:"foreignKey:TeamID" json:"users,omitempty"`
}
