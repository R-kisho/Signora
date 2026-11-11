package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"

	"phase1-signora/backend/internal/config"
	"phase1-signora/backend/internal/database"
	"phase1-signora/backend/internal/handlers"
	"phase1-signora/backend/internal/middleware"
	"phase1-signora/backend/internal/services"
)

func main() {
	loadEnvFile()
	cfg := config.Load()
	db := database.Connect(cfg.DatabaseURL)
	defer db.Close()

	if err := os.MkdirAll(cfg.UploadDir, 0755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.SignedDir, 0755); err != nil {
		log.Fatal(err)
	}

	mailer := &services.Mailer{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Pass: cfg.SMTPPass}
	if !mailer.Configured() {
		log.Println("WARNING: SMTP_USER / SMTP_PASS are not set, so no emails will be sent (signing links are still returned by the API).")
	}

	auth := handlers.AuthHandler{DB: db, Secret: cfg.JWTSecret}
	docs := handlers.DocumentHandler{DB: db, UploadDir: cfg.UploadDir}
	signers := handlers.SignersHandler{DB: db, FrontendURL: cfg.FrontendURL, Mailer: mailer}
	boxes := handlers.BoxesHandler{DB: db}
	status := handlers.StatusHandler{DB: db, UploadDir: cfg.UploadDir, SignedDir: cfg.SignedDir, Mailer: mailer}
	signing := handlers.SigningHandler{DB: db, UploadDir: cfg.UploadDir, SignedDir: cfg.SignedDir, Mailer: mailer}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Public Auth endpoints
	mux.HandleFunc("POST /api/auth/register", auth.Register)
	mux.HandleFunc("POST /api/auth/login", auth.Login)

	// Public Signing endpoints (token-based access)
	mux.HandleFunc("GET /api/signing/{token}", signing.GetSigningInfo)
	mux.HandleFunc("GET /api/signing/{token}/pdf", signing.ServeSigningPDF)
	mux.HandleFunc("POST /api/signing/{token}/submit", signing.SubmitSignature)

	// Protected endpoints (require user JWT)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/auth/me", auth.Me)

	// Documents
	protected.HandleFunc("POST /api/documents/upload", docs.Upload)
	protected.HandleFunc("GET /api/documents", docs.List)
	protected.HandleFunc("GET /api/documents/{id}/view", docs.View)
	protected.HandleFunc("GET /api/documents/{id}/download", docs.Download)
	protected.HandleFunc("DELETE /api/documents/{id}", docs.Delete)

	// Signers
	protected.HandleFunc("GET /api/documents/{id}/signers", signers.List)
	protected.HandleFunc("POST /api/documents/{id}/signers", signers.Add)
	protected.HandleFunc("DELETE /api/documents/{id}/signers/{signerId}", signers.Delete)
	protected.HandleFunc("POST /api/documents/{id}/send", signers.Send)

	// Signature Boxes
	protected.HandleFunc("GET /api/documents/{id}/boxes", boxes.List)
	protected.HandleFunc("POST /api/documents/{id}/boxes", boxes.Save)

	// Status, Audit Trail & Completed Download
	protected.HandleFunc("GET /api/documents/{id}/status", status.GetStatus)
	protected.HandleFunc("GET /api/documents/{id}/audit", status.GetAuditLogs)
	protected.HandleFunc("GET /api/documents/{id}/signed-download", status.DownloadSigned)
	protected.HandleFunc("POST /api/documents/{id}/regenerate", status.Regenerate)

	// Attach JWT middleware to protected routes
	mux.Handle("/api/auth/me", middleware.Auth(cfg.JWTSecret)(protected))
	mux.Handle("/api/documents", middleware.Auth(cfg.JWTSecret)(protected))
	mux.Handle("/api/documents/", middleware.Auth(cfg.JWTSecret)(protected))

	handler := cors(cfg.FrontendURL, mux)
	log.Printf("Signora Go API running on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, handler))
}

func cors(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loadEnvFile() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, value)
			}
		}
	}
}
