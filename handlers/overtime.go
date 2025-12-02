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

	var entries []models.OvertimeEntry
	var totalHours float64

	db := database.GetDB()

	if user.CanViewAllOvertime() {
		db.Preload("User").Order("date desc").Limit(50).Find(&entries)
		db.Model(&models.OvertimeEntry{}).Select("COALESCE(SUM(hours), 0)").Scan(&totalHours)
	} else {
		db.Preload("User").Where("user_id = ?", user.ID).Order("date desc").Limit(50).Find(&entries)
		db.Model(&models.OvertimeEntry{}).Where("user_id = ?", user.ID).Select("COALESCE(SUM(hours), 0)").Scan(&totalHours)
	}

	data := map[string]interface{}{
		"User":       user,
		"Entries":    entries,
		"TotalHours": totalHours,
		"Error":      r.URL.Query().Get("error"),
		"Success":    r.URL.Query().Get("success"),
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
		"Entry": entry,
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

	currentYear := time.Now().Year()
	years := make([]int, 5)
	for i := 0; i < 5; i++ {
		years[i] = currentYear - i
	}

	data := map[string]interface{}{
		"User":         user,
		"Years":        years,
		"CurrentMonth": int(time.Now().Month()),
		"CurrentYear":  currentYear,
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

	var entries []models.OvertimeEntry
	database.GetDB().Preload("User").
		Where("date >= ? AND date < ?", startDate, endDate).
		Order("date asc, user_id asc").
		Find(&entries)

	filename := fmt.Sprintf("overtime_%d_%02d.csv", year, month)
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	writer.Write([]string{"Employee", "Date", "Hours", "Description"})

	// Write data
	for _, entry := range entries {
		writer.Write([]string{
			entry.User.Username,
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

	var entries []models.OvertimeEntry
	database.GetDB().Preload("User").Order("date desc").Find(&entries)

	// Group by user for summary
	userHours := make(map[string]float64)
	for _, entry := range entries {
		userHours[entry.User.Username] += entry.Hours
	}

	data := map[string]interface{}{
		"User":      user,
		"Entries":   entries,
		"UserHours": userHours,
	}
	h.templates["all-entries"].ExecuteTemplate(w, "base", data)
}
