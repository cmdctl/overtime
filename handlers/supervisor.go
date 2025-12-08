package handlers

import (
	"encoding/csv"
	"fmt"
	"html/template"
	"net/http"
	"overtime/config"
	"overtime/database"
	"overtime/middleware"
	"overtime/models"
	"strconv"
	"time"
)

type SupervisorHandler struct {
	config    *config.Config
	templates map[string]*template.Template
}

func NewSupervisorHandler(cfg *config.Config, templates map[string]*template.Template) *SupervisorHandler {
	return &SupervisorHandler{
		config:    cfg,
		templates: templates,
	}
}

// SupervisorsPage shows the supervisor management page (admin only)
func (h *SupervisorHandler) SupervisorsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	var assignments []models.TeamSupervisor
	db.Preload("User").Preload("User.Project").Preload("Team").Find(&assignments)

	// Get all users with SUPERVISOR role
	var supervisors []models.User
	db.Preload("Project").Where("role = ?", models.RoleSupervisor).Find(&supervisors)

	var teams []models.Team
	db.Find(&teams)

	data := map[string]interface{}{
		"User":        user,
		"Assignments": assignments,
		"Supervisors": supervisors,
		"Teams":       teams,
		"Error":       r.URL.Query().Get("error"),
		"Success":     r.URL.Query().Get("success"),
	}
	h.templates["supervisors"].ExecuteTemplate(w, "base", data)
}

// AssignSupervisor assigns a supervisor to a team
func (h *SupervisorHandler) AssignSupervisor(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/supervisors?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	userIDStr := r.FormValue("user_id")
	teamIDStr := r.FormValue("team_id")

	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/supervisors?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	teamID, err := strconv.ParseUint(teamIDStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/supervisors?error=Invalid+team+ID", http.StatusSeeOther)
		return
	}

	// Verify the user is a supervisor with a project assigned
	var supervisor models.User
	if err := database.GetDB().First(&supervisor, userID).Error; err != nil {
		http.Redirect(w, r, "/supervisors?error=User+not+found", http.StatusSeeOther)
		return
	}
	if !supervisor.IsSupervisor() {
		http.Redirect(w, r, "/supervisors?error=User+is+not+a+supervisor", http.StatusSeeOther)
		return
	}
	if supervisor.ProjectID == nil {
		http.Redirect(w, r, "/supervisors?error=Supervisor+has+no+project+assigned", http.StatusSeeOther)
		return
	}

	// Check if assignment already exists
	var existingCount int64
	database.GetDB().Model(&models.TeamSupervisor{}).
		Where("user_id = ? AND team_id = ?", userID, teamID).
		Count(&existingCount)
	if existingCount > 0 {
		http.Redirect(w, r, "/supervisors?error=Assignment+already+exists", http.StatusSeeOther)
		return
	}

	assignment := models.TeamSupervisor{
		UserID: uint(userID),
		TeamID: uint(teamID),
	}

	if err := database.GetDB().Create(&assignment).Error; err != nil {
		http.Redirect(w, r, "/supervisors?error=Failed+to+create+assignment", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/supervisors?success=Team+assigned+to+supervisor+successfully", http.StatusSeeOther)
}

// RemoveSupervisorAssignment removes a supervisor's team assignment
func (h *SupervisorHandler) RemoveSupervisorAssignment(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/supervisors?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/supervisors?error=Invalid+assignment+ID", http.StatusSeeOther)
		return
	}

	if err := database.GetDB().Delete(&models.TeamSupervisor{}, id).Error; err != nil {
		http.Redirect(w, r, "/supervisors?error=Failed+to+remove+assignment", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/supervisors?success=Team+assignment+removed+successfully", http.StatusSeeOther)
}

// getAuthorizedTeams returns the teams a supervisor is authorized to view
func (h *SupervisorHandler) getAuthorizedTeams(userID uint) []models.Team {
	db := database.GetDB()

	var assignments []models.TeamSupervisor
	db.Preload("Team").Where("user_id = ?", userID).Find(&assignments)

	teams := make([]models.Team, 0, len(assignments))
	for _, a := range assignments {
		if a.Team != nil {
			teams = append(teams, *a.Team)
		}
	}

	return teams
}

// getAuthorizedTeamIDs returns the team IDs a supervisor is authorized to view
func (h *SupervisorHandler) getAuthorizedTeamIDs(userID uint) []uint {
	db := database.GetDB()

	var assignments []models.TeamSupervisor
	db.Where("user_id = ?", userID).Find(&assignments)

	teamIDs := make([]uint, 0, len(assignments))
	for _, a := range assignments {
		teamIDs = append(teamIDs, a.TeamID)
	}

	return teamIDs
}

// SupervisorDashboard shows the supervisor's view of their assigned teams' overtime
func (h *SupervisorHandler) SupervisorDashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsSupervisor() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reload user with project
	db := database.GetDB()
	db.Preload("Project").First(user, user.ID)

	if user.ProjectID == nil {
		data := map[string]interface{}{
			"User":  user,
			"Error": "You are not assigned to a project. Please contact an administrator.",
		}
		h.templates["supervisor-dashboard"].ExecuteTemplate(w, "base", data)
		return
	}

	// Get supervisor's authorized teams
	teams := h.getAuthorizedTeams(user.ID)

	if len(teams) == 0 {
		data := map[string]interface{}{
			"User":    user,
			"Project": user.Project,
			"Error":   "You are not assigned to supervise any teams. Please contact an administrator.",
		}
		h.templates["supervisor-dashboard"].ExecuteTemplate(w, "base", data)
		return
	}

	// Get filter parameters
	teamIDStr := r.URL.Query().Get("team_id")
	monthStr := r.URL.Query().Get("month")
	yearStr := r.URL.Query().Get("year")

	// Get authorized team IDs
	authorizedTeamIDs := h.getAuthorizedTeamIDs(user.ID)

	var selectedTeamID uint
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil {
			// Verify the team is authorized
			for _, id := range authorizedTeamIDs {
				if id == uint(tid) {
					selectedTeamID = uint(tid)
					break
				}
			}
		}
	}

	// Build query for entries
	var entries []models.OvertimeEntry
	var totalHours float64
	userHours := make(map[string]float64)

	query := db.Preload("User").Preload("User.Team").Preload("User.Project").
		Joins("JOIN users ON users.id = overtime_entries.user_id").
		Where("users.project_id = ?", *user.ProjectID)

	// Filter by team(s)
	if selectedTeamID > 0 {
		query = query.Where("users.team_id = ?", selectedTeamID)
	} else {
		query = query.Where("users.team_id IN ?", authorizedTeamIDs)
	}

	// Apply month/year filter
	var selectedMonth, selectedYear int

	if monthStr != "" {
		if m, err := strconv.Atoi(monthStr); err == nil && m >= 1 && m <= 12 {
			selectedMonth = m
		}
	}
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil && y >= 2000 && y <= 2100 {
			selectedYear = y
		}
	}

	// Apply date filters
	if selectedMonth > 0 && selectedYear > 0 {
		startDate := time.Date(selectedYear, time.Month(selectedMonth), 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(0, 1, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	} else if selectedMonth > 0 {
		query = query.Where("EXTRACT(MONTH FROM overtime_entries.date) = ?", selectedMonth)
	} else if selectedYear > 0 {
		startDate := time.Date(selectedYear, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(1, 0, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	}

	query.Order("overtime_entries.date desc").Find(&entries)

	// Calculate totals
	for _, entry := range entries {
		userHours[entry.User.DisplayName()] += entry.Hours
		totalHours += entry.Hours
	}

	// Generate years for dropdown
	currentYear := time.Now().Year()
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	data := map[string]interface{}{
		"User":           user,
		"Project":        user.Project,
		"Teams":          teams,
		"SelectedTeamID": selectedTeamID,
		"Entries":        entries,
		"UserHours":      userHours,
		"TotalHours":     totalHours,
		"SelectedMonth":  selectedMonth,
		"SelectedYear":   selectedYear,
		"Years":          years,
		"Error":          r.URL.Query().Get("error"),
		"Success":        r.URL.Query().Get("success"),
	}
	h.templates["supervisor-dashboard"].ExecuteTemplate(w, "base", data)
}

// SupervisorExportPage shows the export page for supervisors
func (h *SupervisorHandler) SupervisorExportPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsSupervisor() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reload user with project
	db := database.GetDB()
	db.Preload("Project").First(user, user.ID)

	if user.ProjectID == nil {
		data := map[string]interface{}{
			"User":  user,
			"Error": "You are not assigned to a project.",
		}
		h.templates["supervisor-export"].ExecuteTemplate(w, "base", data)
		return
	}

	// Get supervisor's authorized teams
	teams := h.getAuthorizedTeams(user.ID)

	if len(teams) == 0 {
		data := map[string]interface{}{
			"User":    user,
			"Project": user.Project,
			"Error":   "You are not assigned to supervise any teams.",
		}
		h.templates["supervisor-export"].ExecuteTemplate(w, "base", data)
		return
	}

	currentYear := time.Now().Year()
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	data := map[string]interface{}{
		"User":         user,
		"Project":      user.Project,
		"Teams":        teams,
		"Years":        years,
		"CurrentMonth": int(time.Now().Month()),
		"CurrentYear":  currentYear,
	}
	h.templates["supervisor-export"].ExecuteTemplate(w, "base", data)
}

// SupervisorExportCSV exports overtime data for supervisor's assigned teams
func (h *SupervisorHandler) SupervisorExportCSV(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.IsSupervisor() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reload user with project
	db := database.GetDB()
	db.Preload("Project").First(user, user.ID)

	if user.ProjectID == nil {
		http.Error(w, "No project assigned", http.StatusForbidden)
		return
	}

	teamIDStr := r.URL.Query().Get("team_id")
	monthStr := r.URL.Query().Get("month")
	yearStr := r.URL.Query().Get("year")

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		http.Error(w, "Invalid month", http.StatusBadRequest)
		return
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2000 || year > 2100 {
		http.Error(w, "Invalid year", http.StatusBadRequest)
		return
	}

	// Get authorized team IDs
	authorizedTeamIDs := h.getAuthorizedTeamIDs(user.ID)
	if len(authorizedTeamIDs) == 0 {
		http.Error(w, "No teams assigned", http.StatusForbidden)
		return
	}

	var selectedTeamID uint
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil {
			// Verify the team is authorized
			for _, id := range authorizedTeamIDs {
				if id == uint(tid) {
					selectedTeamID = uint(tid)
					break
				}
			}
		}
	}

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0)

	query := db.Preload("User").Preload("User.Team").Preload("User.Project").
		Joins("JOIN users ON users.id = overtime_entries.user_id").
		Where("users.project_id = ?", *user.ProjectID)

	// Filter by team(s)
	if selectedTeamID > 0 {
		query = query.Where("users.team_id = ?", selectedTeamID)
	} else {
		query = query.Where("users.team_id IN ?", authorizedTeamIDs)
	}

	var entries []models.OvertimeEntry
	query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate).
		Order("overtime_entries.date asc, overtime_entries.user_id asc").
		Find(&entries)

	// Build filename
	var filename string
	if selectedTeamID > 0 {
		var team models.Team
		db.First(&team, selectedTeamID)
		filename = fmt.Sprintf("overtime_%s_%s_%d_%02d.csv", team.Name, user.Project.Name, year, month)
	} else {
		filename = fmt.Sprintf("overtime_all-teams_%s_%d_%02d.csv", user.Project.Name, year, month)
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	writer.Write([]string{"Employee", "Team", "Project", "Date", "Hours", "Description"})

	// Write data
	for _, entry := range entries {
		teamName := ""
		projectName := ""
		if entry.User.Team != nil {
			teamName = entry.User.Team.Name
		}
		if entry.User.Project != nil {
			projectName = entry.User.Project.Name
		}
		writer.Write([]string{
			entry.User.DisplayName(),
			teamName,
			projectName,
			entry.Date.Format("2006-01-02"),
			fmt.Sprintf("%.2f", entry.Hours),
			entry.Description,
		})
	}
}
