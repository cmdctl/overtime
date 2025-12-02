package models

import (
	"time"

	"gorm.io/gorm"
)

type Role string

const (
	RoleAdmin    Role = "ADMIN"
	RoleHR       Role = "HR"
	RoleEmployee Role = "EMPLOYEE"
)

type User struct {
	ID                 uint           `gorm:"primaryKey" json:"id"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
	Username           string         `gorm:"uniqueIndex;not null;size:100" json:"username"`
	FullName           string         `gorm:"not null;size:200" json:"full_name"`
	PasswordHash       string         `gorm:"not null" json:"-"`
	Role               Role           `gorm:"not null;size:20" json:"role"`
	MustChangePassword bool           `gorm:"default:true" json:"must_change_password"`
	OvertimeEntries    []OvertimeEntry `gorm:"foreignKey:UserID" json:"overtime_entries,omitempty"`
}

func (u *User) DisplayName() string {
	if u.FullName != "" {
		return u.FullName
	}
	return u.Username
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsHR() bool {
	return u.Role == RoleHR
}

func (u *User) IsEmployee() bool {
	return u.Role == RoleEmployee
}

func (u *User) CanManageOvertimeFor(userID uint) bool {
	if u.IsAdmin() {
		return true
	}
	return u.ID == userID
}

func (u *User) CanViewAllOvertime() bool {
	return u.IsAdmin() || u.IsHR()
}

func (u *User) CanExport() bool {
	return u.IsAdmin() || u.IsHR()
}

func (u *User) CanCreateInvites() bool {
	return u.IsAdmin()
}
