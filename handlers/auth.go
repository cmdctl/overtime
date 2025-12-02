package handlers

import (
	"html/template"
	"net/http"
	"overtime/config"
	"overtime/database"
	"overtime/middleware"
	"overtime/models"
	"time"

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
	if err := database.GetDB().Where("code = ?", code).First(&invite).Error; err != nil {
		http.Error(w, "Invalid invite link", http.StatusBadRequest)
		return
	}

	if !invite.IsValid() {
		http.Error(w, "Invite link has expired or already been used", http.StatusBadRequest)
		return
	}

	data := map[string]interface{}{
		"Code":  code,
		"Role":  invite.Role,
		"Error": r.URL.Query().Get("error"),
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
		PasswordHash:       string(hashedPassword),
		Role:               invite.Role,
		MustChangePassword: false,
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

	var invites []models.Invite
	database.GetDB().Where("created_by = ?", user.ID).Order("created_at desc").Find(&invites)

	data := map[string]interface{}{
		"User":    user,
		"Invites": invites,
		"Error":   r.URL.Query().Get("error"),
		"Success": r.URL.Query().Get("success"),
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

	roleStr := r.FormValue("role")
	var role models.Role
	switch roleStr {
	case "EMPLOYEE":
		role = models.RoleEmployee
	case "HR":
		role = models.RoleHR
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
		Role:      role,
		CreatedBy: user.ID,
		ExpiresAt: time.Now().Add(h.config.InviteExpiration),
	}

	if err := database.GetDB().Create(&invite).Error; err != nil {
		http.Redirect(w, r, "/invites?error=Failed+to+create+invite", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/invites?success=Invite+created+successfully", http.StatusSeeOther)
}
