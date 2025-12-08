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

type OvertimeHandler struct {
	config    *config.Config
	templates map[string]*template.Template
}

func NewOvertimeHandler(cfg *config.Config, templates map[string]*template.Template) *OvertimeHandler {
	return &OvertimeHandler{
		config:    cfg,
		templates: templates,
	}
}

func (h *OvertimeHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get filter parameters
	teamIDStr := r.URL.Query().Get("team_id")
	projectIDStr := r.URL.Query().Get("project_id")
	monthStr := r.URL.Query().Get("month")
	yearStr := r.URL.Query().Get("year")

	var entries []models.OvertimeEntry
	var totalHours float64

	db := database.GetDB()

	// Build query based on user permissions
	query := db.Preload("User").Preload("User.Team").Preload("User.Project")

	if user.CanViewAllOvertime() {
		// Admin/HR can see all entries
	} else {
		query = query.Where("user_id = ?", user.ID)
	}

	// Apply team filter
	var selectedTeamID uint
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil && tid > 0 {
			selectedTeamID = uint(tid)
			query = query.Joins("JOIN users ON users.id = overtime_entries.user_id").
				Where("users.team_id = ?", selectedTeamID)
		}
	}

	// Apply project filter
	var selectedProjectID uint
	if projectIDStr != "" {
		if pid, err := strconv.ParseUint(projectIDStr, 10, 32); err == nil && pid > 0 {
			selectedProjectID = uint(pid)
			if teamIDStr == "" {
				query = query.Joins("JOIN users ON users.id = overtime_entries.user_id")
			}
			query = query.Where("users.project_id = ?", selectedProjectID)
		}
	}

	// Apply month/year filter
	var selectedMonth, selectedYear int
	currentYear := time.Now().Year()
	currentMonth := int(time.Now().Month())

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
		// Both month and year specified
		startDate := time.Date(selectedYear, time.Month(selectedMonth), 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(0, 1, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	} else if selectedMonth > 0 {
		// Only month specified - filter by month across all years
		query = query.Where("EXTRACT(MONTH FROM overtime_entries.date) = ?", selectedMonth)
	} else if selectedYear > 0 {
		// Only year specified - filter by year across all months
		startDate := time.Date(selectedYear, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(1, 0, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	}

	query.Order("overtime_entries.date desc").Limit(100).Find(&entries)

	// Calculate total hours for filtered entries
	for _, entry := range entries {
		totalHours += entry.Hours
	}

	// Get all teams and projects for filter dropdowns
	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	// Generate years for dropdown
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	data := map[string]interface{}{
		"User":              user,
		"Entries":           entries,
		"TotalHours":        totalHours,
		"Error":             r.URL.Query().Get("error"),
		"Success":           r.URL.Query().Get("success"),
		"Teams":             teams,
		"Projects":          projects,
		"SelectedTeamID":    selectedTeamID,
		"SelectedProjectID": selectedProjectID,
		"SelectedMonth":     selectedMonth,
		"SelectedYear":      selectedYear,
		"CurrentMonth":      currentMonth,
		"CurrentYear":       currentYear,
		"Years":             years,
	}
	h.templates["dashboard"].ExecuteTemplate(w, "base", data)
}

func (h *OvertimeHandler) NewEntryPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())

	var users []models.User
	if user.IsAdmin() {
		database.GetDB().Find(&users)
	}

	data := map[string]interface{}{
		"User":  user,
		"Users": users,
		"Error": r.URL.Query().Get("error"),
		"Today": time.Now().Format("2006-01-02"),
	}
	h.templates["overtime-form"].ExecuteTemplate(w, "base", data)
}

func (h *OvertimeHandler) CreateEntry(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/overtime/new?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	dateStr := r.FormValue("date")
	hoursStr := r.FormValue("hours")
	description := r.FormValue("description")
	userIDStr := r.FormValue("user_id")

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		http.Redirect(w, r, "/overtime/new?error=Invalid+date+format", http.StatusSeeOther)
		return
	}

	hours, err := strconv.ParseFloat(hoursStr, 64)
	if err != nil || hours <= 0 || hours > 24 {
		http.Redirect(w, r, "/overtime/new?error=Invalid+hours+(must+be+between+0+and+24)", http.StatusSeeOther)
		return
	}

	targetUserID := user.ID
	if userIDStr != "" && user.IsAdmin() {
		parsedID, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			targetUserID = uint(parsedID)
		}
	}

	if !user.CanManageOvertimeFor(targetUserID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	entry := models.OvertimeEntry{
		UserID:      targetUserID,
		Date:        date,
		Hours:       hours,
		Description: description,
	}

	if err := database.GetDB().Create(&entry).Error; err != nil {
		http.Redirect(w, r, "/overtime/new?error=Failed+to+create+entry", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard?success=Overtime+entry+created", http.StatusSeeOther)
}

func (h *OvertimeHandler) EditEntryPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Redirect(w, r, "/dashboard?error=Invalid+entry+ID", http.StatusSeeOther)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=Invalid+entry+ID", http.StatusSeeOther)
		return
	}

	var entry models.OvertimeEntry
	if err := database.GetDB().Preload("User").First(&entry, id).Error; err != nil {
		http.Redirect(w, r, "/dashboard?error=Entry+not+found", http.StatusSeeOther)
		return
	}

	if !user.CanManageOvertimeFor(entry.UserID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var users []models.User
	if user.IsAdmin() {
		database.GetDB().Find(&users)
	}

	data := map[string]interface{}{
		"User":  user,
		"Entry": &entry,
		"Users": users,
		"Error": r.URL.Query().Get("error"),
	}
	h.templates["overtime-edit"].ExecuteTemplate(w, "base", data)
}

func (h *OvertimeHandler) UpdateEntry(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=Invalid+entry+ID", http.StatusSeeOther)
		return
	}

	var entry models.OvertimeEntry
	if err := database.GetDB().First(&entry, id).Error; err != nil {
		http.Redirect(w, r, "/dashboard?error=Entry+not+found", http.StatusSeeOther)
		return
	}

	if !user.CanManageOvertimeFor(entry.UserID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	dateStr := r.FormValue("date")
	hoursStr := r.FormValue("hours")
	description := r.FormValue("description")

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/overtime/edit?id=%d&error=Invalid+date+format", id), http.StatusSeeOther)
		return
	}

	hours, err := strconv.ParseFloat(hoursStr, 64)
	if err != nil || hours <= 0 || hours > 24 {
		http.Redirect(w, r, fmt.Sprintf("/overtime/edit?id=%d&error=Invalid+hours", id), http.StatusSeeOther)
		return
	}

	entry.Date = date
	entry.Hours = hours
	entry.Description = description

	if err := database.GetDB().Save(&entry).Error; err != nil {
		http.Redirect(w, r, fmt.Sprintf("/overtime/edit?id=%d&error=Failed+to+update+entry", id), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard?success=Overtime+entry+updated", http.StatusSeeOther)
}

func (h *OvertimeHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=Invalid+entry+ID", http.StatusSeeOther)
		return
	}

	var entry models.OvertimeEntry
	if err := database.GetDB().First(&entry, id).Error; err != nil {
		http.Redirect(w, r, "/dashboard?error=Entry+not+found", http.StatusSeeOther)
		return
	}

	if !user.CanManageOvertimeFor(entry.UserID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := database.GetDB().Delete(&entry).Error; err != nil {
		http.Redirect(w, r, "/dashboard?error=Failed+to+delete+entry", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard?success=Overtime+entry+deleted", http.StatusSeeOther)
}

func (h *OvertimeHandler) ExportPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.CanExport() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	db := database.GetDB()

	currentYear := time.Now().Year()
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	data := map[string]interface{}{
		"User":         user,
		"Years":        years,
		"CurrentMonth": int(time.Now().Month()),
		"CurrentYear":  currentYear,
		"Teams":        teams,
		"Projects":     projects,
	}
	h.templates["export"].ExecuteTemplate(w, "base", data)
}

func (h *OvertimeHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.CanExport() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	monthStr := r.URL.Query().Get("month")
	yearStr := r.URL.Query().Get("year")
	teamIDStr := r.URL.Query().Get("team_id")
	projectIDStr := r.URL.Query().Get("project_id")

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

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0)

	db := database.GetDB()
	query := db.Preload("User").Preload("User.Team").Preload("User.Project").
		Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)

	// Apply team filter
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil && tid > 0 {
			query = query.Joins("JOIN users ON users.id = overtime_entries.user_id").
				Where("users.team_id = ?", tid)
		}
	}

	// Apply project filter
	if projectIDStr != "" {
		if pid, err := strconv.ParseUint(projectIDStr, 10, 32); err == nil && pid > 0 {
			if teamIDStr == "" {
				query = query.Joins("JOIN users ON users.id = overtime_entries.user_id")
			}
			query = query.Where("users.project_id = ?", pid)
		}
	}

	var entries []models.OvertimeEntry
	query.Order("overtime_entries.date asc, overtime_entries.user_id asc").Find(&entries)

	filename := fmt.Sprintf("overtime_%d_%02d.csv", year, month)
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

func (h *OvertimeHandler) AllEntriesPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if !user.CanViewAllOvertime() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get filter parameters
	teamIDStr := r.URL.Query().Get("team_id")
	projectIDStr := r.URL.Query().Get("project_id")
	monthStr := r.URL.Query().Get("month")
	yearStr := r.URL.Query().Get("year")

	db := database.GetDB()
	query := db.Preload("User").Preload("User.Team").Preload("User.Project")

	// Apply team filter
	var selectedTeamID uint
	if teamIDStr != "" {
		if tid, err := strconv.ParseUint(teamIDStr, 10, 32); err == nil && tid > 0 {
			selectedTeamID = uint(tid)
			query = query.Joins("JOIN users ON users.id = overtime_entries.user_id").
				Where("users.team_id = ?", selectedTeamID)
		}
	}

	// Apply project filter
	var selectedProjectID uint
	if projectIDStr != "" {
		if pid, err := strconv.ParseUint(projectIDStr, 10, 32); err == nil && pid > 0 {
			selectedProjectID = uint(pid)
			if teamIDStr == "" {
				query = query.Joins("JOIN users ON users.id = overtime_entries.user_id")
			}
			query = query.Where("users.project_id = ?", selectedProjectID)
		}
	}

	// Apply month/year filter
	var selectedMonth, selectedYear int
	currentYear := time.Now().Year()

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
		// Both month and year specified
		startDate := time.Date(selectedYear, time.Month(selectedMonth), 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(0, 1, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	} else if selectedMonth > 0 {
		// Only month specified - filter by month across all years
		query = query.Where("EXTRACT(MONTH FROM overtime_entries.date) = ?", selectedMonth)
	} else if selectedYear > 0 {
		// Only year specified - filter by year across all months
		startDate := time.Date(selectedYear, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(1, 0, 0)
		query = query.Where("overtime_entries.date >= ? AND overtime_entries.date < ?", startDate, endDate)
	}

	var entries []models.OvertimeEntry
	query.Order("overtime_entries.date desc").Find(&entries)

	// Group by user for summary
	userHours := make(map[string]float64)
	var totalHours float64
	for _, entry := range entries {
		userHours[entry.User.DisplayName()] += entry.Hours
		totalHours += entry.Hours
	}

	// Get all teams and projects for filter dropdowns
	var teams []models.Team
	var projects []models.Project
	db.Find(&teams)
	db.Find(&projects)

	// Generate years for dropdown
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	data := map[string]interface{}{
		"User":              user,
		"Entries":           entries,
		"UserHours":         userHours,
		"TotalHours":        totalHours,
		"Teams":             teams,
		"Projects":          projects,
		"SelectedTeamID":    selectedTeamID,
		"SelectedProjectID": selectedProjectID,
		"SelectedMonth":     selectedMonth,
		"SelectedYear":      selectedYear,
		"Years":             years,
	}
	h.templates["all-entries"].ExecuteTemplate(w, "base", data)
}
