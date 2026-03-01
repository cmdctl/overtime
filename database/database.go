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
	err = DB.AutoMigrate(&models.Team{}, &models.Project{}, &models.User{}, &models.OvertimeEntry{}, &models.Invite{}, &models.TeamSupervisor{})
	if err != nil {
		return err
	}

	if err := backfillOvertimeEntries(); err != nil {
		log.Printf("Warning: failed to backfill overtime entries: %v", err)
	}

	// Seed default admin if not exists
	if err := seedDefaultAdmin(); err != nil {
		return err
	}

	return nil
}

func backfillOvertimeEntries() error {
	var entries []models.OvertimeEntry
	DB.Where("username = '' OR username IS NULL").Find(&entries)
	if len(entries) == 0 {
		return nil
	}

	log.Printf("Backfilling %d overtime entries with denormalized user/team/project data", len(entries))

	userCache := make(map[uint]*models.User)
	for i := range entries {
		entry := &entries[i]
		u, ok := userCache[entry.UserID]
		if !ok {
			u = &models.User{}
			if err := DB.Preload("Team").Preload("Project").First(u, entry.UserID).Error; err != nil {
				log.Printf("Warning: could not load user %d for entry %d: %v", entry.UserID, entry.ID, err)
				continue
			}
			userCache[entry.UserID] = u
		}
		entry.Username = u.DisplayName()
		if u.Team != nil {
			entry.TeamName = u.Team.Name
		}
		if u.Project != nil {
			entry.ProjectName = u.Project.Name
		}
		DB.Save(entry)
	}
	log.Printf("Backfill complete")
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
