package database

import (
	"log"
	"overtime/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init(dsn string) error {
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return err
	}

	// Auto migrate the schema
	err = DB.AutoMigrate(&models.User{}, &models.OvertimeEntry{}, &models.Invite{})
	if err != nil {
		return err
	}

	// Seed default admin if not exists
	if err := seedDefaultAdmin(); err != nil {
		return err
	}

	return nil
}

func seedDefaultAdmin() error {
	var count int64
	DB.Model(&models.User{}).Where("username = ?", "admin").Count(&count)
	if count > 0 {
		return nil
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	admin := models.User{
		Username:           "admin",
		FullName:           "Administrator",
		PasswordHash:       string(hashedPassword),
		Role:               models.RoleAdmin,
		MustChangePassword: true,
	}

	result := DB.Create(&admin)
	if result.Error != nil {
		return result.Error
	}

	log.Println("Default admin user created (username: admin, password: admin)")
	return nil
}

func GetDB() *gorm.DB {
	return DB
}
