package handlers

import (
	"html/template"
	"net/http"
	"strconv"
	"time"

	"overtime/config"
	"overtime/database"
	"overtime/middleware"
	"overtime/models"

	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	config    *config.Config
	templates map[string]*template.Template
}

func NewAuthHandler(cfg *config.Config, templates map[string]*template.Template) *AuthHandler {
	return &AuthHandler{
		config:    cfg,
		templates: templates,
	}
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Error": r.URL.Query().Get("error"),
	}
	h.templates["login"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	var user models.User
	if err := database.GetDB().Where("username = ?", username).First(&user).Error; err != nil {
		http.Redirect(w, r, "/login?error=Invalid+credentials", http.StatusSeeOther)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+credentials", http.StatusSeeOther)
		return
	}

	token, err := middleware.GenerateToken(&user, h.config.JWTExpiration)
	if err != nil {
		http.Redirect(w, r, "/login?error=Failed+to+generate+token", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.config.JWTExpiration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	if user.MustChangePassword {
		http.Redirect(w, r, "/change-password", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *AuthHandler) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	data := map[string]interface{}{
		"User":  user,
		"Error": r.URL.Query().Get("error"),
	}
	h.templates["change-password"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/change-password?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		http.Redirect(w, r, "/change-password?error=Current+password+is+incorrect", http.StatusSeeOther)
		return
	}

	if newPassword != confirmPassword {
		http.Redirect(w, r, "/change-password?error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	if len(newPassword) < 5 {
		http.Redirect(w, r, "/change-password?error=Password+must+be+at+least+5+characters", http.StatusSeeOther)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Redirect(w, r, "/change-password?error=Failed+to+hash+password", http.StatusSeeOther)
		return
	}

	user.PasswordHash = string(hashedPassword)
	user.MustChangePassword = false
	if err := database.GetDB().Save(user).Error; err != nil {
		http.Redirect(w, r, "/change-password?error=Failed+to+update+password", http.StatusSeeOther)
		return
	}

	// Regenerate token with updated user info
	token, err := middleware.GenerateToken(user, h.config.JWTExpiration)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.config.JWTExpiration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Invalid invite link", http.StatusBadRequest)
		return
	}

	var invite models.Invite
	if err := database.GetDB().Preload("Team").Preload("Project").Where("code = ?", code).First(&invite).Error; err != nil {
		http.Error(w, "Invalid invite link", http.StatusBadRequest)
		return
	}

	if !invite.IsValid() {
		http.Error(w, "Invite link has expired or already been used", http.StatusBadRequest)
		return
	}

	data := map[string]interface{}{
		"Code":     code,
		"FullName": invite.FullName,
		"Role":     invite.Role,
		"Team":     invite.Team,
		"Project":  invite.Project,
		"Error":    r.URL.Query().Get("error"),
	}
	h.templates["register"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	var invite models.Invite
	if err := database.GetDB().Where("code = ?", code).First(&invite).Error; err != nil {
		http.Error(w, "Invalid invite link", http.StatusBadRequest)
		return
	}

	if !invite.IsValid() {
		http.Error(w, "Invite link has expired or already been used", http.StatusBadRequest)
		return
	}

	if len(username) < 3 {
		http.Redirect(w, r, "/register?code="+code+"&error=Username+must+be+at+least+3+characters", http.StatusSeeOther)
		return
	}

	if password != confirmPassword {
		http.Redirect(w, r, "/register?code="+code+"&error=Passwords+do+not+match", http.StatusSeeOther)
		return
	}

	if len(password) < 5 {
		http.Redirect(w, r, "/register?code="+code+"&error=Password+must+be+at+least+5+characters", http.StatusSeeOther)
		return
	}

	// Check if username already exists
	var existingUser models.User
	if err := database.GetDB().Where("username = ?", username).First(&existingUser).Error; err == nil {
		http.Redirect(w, r, "/register?code="+code+"&error=Username+already+exists", http.StatusSeeOther)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Redirect(w, r, "/register?code="+code+"&error=Failed+to+create+account", http.StatusSeeOther)
		return
	}

	user := models.User{
		Username:           username,
		FullName:           invite.FullName,
		PasswordHash:       string(hashedPassword),
		Role:               invite.Role,
		MustChangePassword: false,
		TeamID:             invite.TeamID,
		ProjectID:          invite.ProjectID,
	}

	if err := database.GetDB().Create(&user).Error; err != nil {
		http.Redirect(w, r, "/register?code="+code+"&error=Failed+to+create+account", http.StatusSeeOther)
		return
	}

	// User set their own password during registration, no need to change it
	database.GetDB().Model(&user).Update("must_change_password", false)

	// Mark invite as used
	invite.Used = true
	database.GetDB().Save(&invite)

	// Generate token and log user in
	token, err := middleware.GenerateToken(&user, h.config.JWTExpiration)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.config.JWTExpiration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) InvitesPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.CanCreateInvites() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	var invites []models.Invite
	db.Preload("Team").Preload("Project").Where("created_by = ?", user.ID).Order("created_at desc").Find(&invites)

	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	data := map[string]interface{}{
		"User":     user,
		"BaseURL":  h.config.BaseURL,
		"Invites":  invites,
		"Teams":    teams,
		"Projects": projects,
		"Error":    r.URL.Query().Get("error"),
		"Success":  r.URL.Query().Get("success"),
	}
	h.templates["invites"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.CanCreateInvites() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/invites?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	fullName := r.FormValue("full_name")
	if fullName == "" {
		http.Redirect(w, r, "/invites?error=Full+name+is+required", http.StatusSeeOther)
		return
	}

	roleStr := r.FormValue("role")
	var role models.Role
	switch roleStr {
	case "EMPLOYEE":
		role = models.RoleEmployee
	case "HR":
		role = models.RoleHR
	case "ADMIN":
		role = models.RoleAdmin
	default:
		http.Redirect(w, r, "/invites?error=Invalid+role", http.StatusSeeOther)
		return
	}

	code, err := models.GenerateInviteCode()
	if err != nil {
		http.Redirect(w, r, "/invites?error=Failed+to+generate+invite+code", http.StatusSeeOther)
		return
	}

	invite := models.Invite{
		Code:      code,
		FullName:  fullName,
		Role:      role,
		CreatedBy: user.ID,
		ExpiresAt: time.Now().Add(h.config.InviteExpiration),
	}

	// Handle team assignment
	teamIDStr := r.FormValue("team_id")
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil && tid > 0 {
			teamID := uint(tid)
			invite.TeamID = &teamID
		}
	}

	// Handle project assignment
	projectIDStr := r.FormValue("project_id")
	if projectIDStr != "" {
		if pid, err := strconv.ParseUint(projectIDStr, 10, 32); err == nil && pid > 0 {
			projectID := uint(pid)
			invite.ProjectID = &projectID
		}
	}

	if err := database.GetDB().Create(&invite).Error; err != nil {
		http.Redirect(w, r, "/invites?error=Failed+to+create+invite", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/invites?success=Invite+created+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	// Get filter parameters
	teamFilter := r.URL.Query().Get("team")
	projectFilter := r.URL.Query().Get("project")

	// Build query with filters
	query := db.Preload("Team").Preload("Project").Order("created_at desc")

	if teamFilter != "" {
		if teamID, err := strconv.ParseUint(teamFilter, 10, 32); err == nil {
			query = query.Where("team_id = ?", teamID)
		}
	}

	if projectFilter != "" {
		if projectID, err := strconv.ParseUint(projectFilter, 10, 32); err == nil {
			query = query.Where("project_id = ?", projectID)
		}
	}

	var users []models.User
	query.Find(&users)

	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	data := map[string]interface{}{
		"User":          user,
		"Users":         users,
		"Teams":         teams,
		"Projects":      projects,
		"TeamFilter":    teamFilter,
		"ProjectFilter": projectFilter,
		"Error":         r.URL.Query().Get("error"),
		"Success":       r.URL.Query().Get("success"),
	}
	h.templates["users"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) EditUserPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Redirect(w, r, "/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	db := database.GetDB()

	var editUser models.User
	if err := db.Preload("Team").Preload("Project").First(&editUser, id).Error; err != nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	data := map[string]interface{}{
		"User":     user,
		"EditUser": &editUser,
		"Teams":    teams,
		"Projects": projects,
		"Error":    r.URL.Query().Get("error"),
	}
	h.templates["user-edit"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	db := database.GetDB()

	var editUser models.User
	if err := db.First(&editUser, id).Error; err != nil {
		http.Redirect(w, r, "/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Update full name
	fullName := r.FormValue("full_name")
	if fullName != "" {
		editUser.FullName = fullName
	}

	// Update role
	roleStr := r.FormValue("role")
	switch roleStr {
	case "EMPLOYEE":
		editUser.Role = models.RoleEmployee
	case "HR":
		editUser.Role = models.RoleHR
	case "ADMIN":
		editUser.Role = models.RoleAdmin
	}

	// Update team
	teamIDStr := r.FormValue("team_id")
	if teamIDStr == "" {
		editUser.TeamID = nil
	} else {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil {
			teamID := uint(tid)
			editUser.TeamID = &teamID
		}
	}

	// Update project
	projectIDStr := r.FormValue("project_id")
	if projectIDStr == "" {
		editUser.ProjectID = nil
	} else {
		if pid, err := strconv.ParseUint(projectIDStr, 10, 32); err == nil {
			projectID := uint(pid)
			editUser.ProjectID = &projectID
		}
	}

	if err := db.Save(&editUser).Error; err != nil {
		http.Redirect(w, r, "/users/edit?id="+idStr+"&error=Failed+to+update+user", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?success=User+updated+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/users?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	// Prevent self-deletion
	if uint(id) == user.ID {
		http.Redirect(w, r, "/users?error=Cannot+delete+your+own+account", http.StatusSeeOther)
		return
	}

	db := database.GetDB()

	// Delete user's overtime entries first
	if err := db.Where("user_id = ?", id).Delete(&models.OvertimeEntry{}).Error; err != nil {
		http.Redirect(w, r, "/users?error=Failed+to+delete+user+entries", http.StatusSeeOther)
		return
	}

	// Delete the user (soft delete since User has DeletedAt)
	if err := db.Delete(&models.User{}, id).Error; err != nil {
		http.Redirect(w, r, "/users?error=Failed+to+delete+user", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/users?success=User+deleted+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) TeamsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	var teams []models.Team
	db.Find(&teams)

	data := map[string]interface{}{
		"User":    user,
		"Teams":   teams,
		"Error":   r.URL.Query().Get("error"),
		"Success": r.URL.Query().Get("success"),
	}
	h.templates["teams"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/teams?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/teams?error=Team+name+is+required", http.StatusSeeOther)
		return
	}

	team := models.Team{Name: name}
	if err := database.GetDB().Create(&team).Error; err != nil {
		http.Redirect(w, r, "/teams?error=Failed+to+create+team", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/teams?success=Team+created+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/teams?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/teams?error=Invalid+team+ID", http.StatusSeeOther)
		return
	}

	db := database.GetDB()

	// Check if any users are assigned to this team
	var userCount int64
	db.Model(&models.User{}).Where("team_id = ?", id).Count(&userCount)
	if userCount > 0 {
		http.Redirect(w, r, "/teams?error=Cannot+delete+team+with+assigned+users", http.StatusSeeOther)
		return
	}

	if err := db.Delete(&models.Team{}, id).Error; err != nil {
		http.Redirect(w, r, "/teams?error=Failed+to+delete+team", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/teams?success=Team+deleted+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) ProjectsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	var projects []models.Project
	db.Find(&projects)

	data := map[string]interface{}{
		"User":     user,
		"Projects": projects,
		"Error":    r.URL.Query().Get("error"),
		"Success":  r.URL.Query().Get("success"),
	}
	h.templates["projects"].ExecuteTemplate(w, "base", data)
}

func (h *AuthHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/projects?error=Project+name+is+required", http.StatusSeeOther)
		return
	}

	project := models.Project{Name: name}
	if err := database.GetDB().Create(&project).Error; err != nil {
		http.Redirect(w, r, "/projects?error=Failed+to+create+project", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/projects?success=Project+created+successfully", http.StatusSeeOther)
}

func (h *AuthHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/projects?error=Invalid+project+ID", http.StatusSeeOther)
		return
	}

	db := database.GetDB()

	// Check if any users are assigned to this project
	var userCount int64
	db.Model(&models.User{}).Where("project_id = ?", id).Count(&userCount)
	if userCount > 0 {
		http.Redirect(w, r, "/projects?error=Cannot+delete+project+with+assigned+users", http.StatusSeeOther)
		return
	}

	if err := db.Delete(&models.Project{}, id).Error; err != nil {
		http.Redirect(w, r, "/projects?error=Failed+to+delete+project", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/projects?success=Project+deleted+successfully", http.StatusSeeOther)
}
