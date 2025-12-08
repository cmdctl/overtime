package models

import (
	"time"

	"gorm.io/gorm"
)

// TeamSupervisor represents a team assignment for a supervisor
// The supervisor's project is stored on User.ProjectID
// This table tracks which teams within that project the supervisor can view
type TeamSupervisor struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	UserID    uint           `gorm:"not null;index" json:"user_id"`
	User      *User          `gorm:"foreignKey:UserID" json:"user,omitempty"`
	TeamID    uint           `gorm:"not null;index" json:"team_id"`
	Team      *Team          `gorm:"foreignKey:TeamID" json:"team,omitempty"`
}
