package main

import (
	"html/template"
	"log"
	"net/http"
	"overtime/config"
	"overtime/database"
	"overtime/handlers"
	"overtime/middleware"
	"overtime/models"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize JWT secret
	middleware.SetJWTSecret(cfg.JWTSecret)

	// Initialize database
	if err := database.Init(cfg.DatabasePath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Parse templates - each page template paired with base
	templates := make(map[string]*template.Template)
	pages := []string{
		"login", "register", "change-password", "dashboard",
		"overtime-form", "overtime-edit", "invites", "export", "all-entries",
	}
	for _, page := range pages {
		templates[page] = template.Must(template.ParseFiles(
			"templates/base.html",
			"templates/"+page+".html",
		))
	}

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(cfg, templates)
	overtimeHandler := handlers.NewOvertimeHandler(cfg, templates)

	// Setup router
	router := chi.NewRouter()
	router.Use(chimiddleware.Logger)
	router.Use(chimiddleware.Recoverer)

	// Static files
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public routes
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	router.Get("/login", authHandler.LoginPage)
	router.Post("/login", authHandler.Login)
	router.Get("/register", authHandler.RegisterPage)
	router.Post("/register", authHandler.Register)

	// Protected routes
	router.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware)

		// Logout (doesn't need password change check)
		r.Get("/logout", authHandler.Logout)

		// Password change routes (accessible even when password change required)
		r.Get("/change-password", authHandler.ChangePasswordPage)
		r.Post("/change-password", authHandler.ChangePassword)

		// Routes that require password to be changed first
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequirePasswordChange)

			// Dashboard
			r.Get("/dashboard", overtimeHandler.Dashboard)

			// Overtime entries (all authenticated users can access)
			r.Get("/overtime/new", overtimeHandler.NewEntryPage)
			r.Post("/overtime/new", overtimeHandler.CreateEntry)
			r.Get("/overtime/edit", overtimeHandler.EditEntryPage)
			r.Post("/overtime/edit", overtimeHandler.UpdateEntry)
			r.Post("/overtime/delete", overtimeHandler.DeleteEntry)

			// Admin and HR only routes
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.RoleAdmin, models.RoleHR))
				r.Get("/overtime/all", overtimeHandler.AllEntriesPage)
				r.Get("/export", overtimeHandler.ExportPage)
				r.Get("/export/csv", overtimeHandler.ExportCSV)
			})

			// Admin only routes
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.RoleAdmin))
				r.Get("/invites", authHandler.InvitesPage)
				r.Post("/invites", authHandler.CreateInvite)
			})
		})
	})

	log.Printf("Server starting on port %s", cfg.ServerPort)
	log.Printf("Default admin credentials: admin / admin")
	log.Fatal(http.ListenAndServe(":"+cfg.ServerPort, router))
}
