package handlers

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"phase1-signora/backend/internal/middleware"
	"phase1-signora/backend/internal/utils"
	"strings"
)

type DocumentHandler struct {
	DB        *pgxpool.Pool
	UploadDir string
}

const maxUpload = 20 << 20

func (h DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024*1024)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		utils.Error(w, 413, "File is too large. Maximum size is 20MB")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		utils.Error(w, 400, "Document title is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		utils.Error(w, 400, "PDF file is required")
		return
	}
	defer file.Close()
	if header.Size > maxUpload {
		utils.Error(w, 413, "File is too large. Maximum size is 20MB")
		return
	}
	if strings.ToLower(filepath.Ext(header.Filename)) != ".pdf" {
		utils.Error(w, 400, "Only PDF files are accepted")
		return
	}
	head := make([]byte, 512)
	n, _ := file.Read(head)
	if n < 5 || string(head[:5]) != "%PDF-" {
		utils.Error(w, 400, "Uploaded file is not a valid PDF")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		utils.Error(w, 500, "Could not read PDF")
		return
	}
	if err = os.MkdirAll(h.UploadDir, 0755); err != nil {
		utils.Error(w, 500, "Storage is unavailable")
		return
	}
	id := uuid.NewString()
	stored := id + ".pdf"
	dest := filepath.Join(h.UploadDir, stored)
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		utils.Error(w, 500, "Could not save PDF")
		return
	}
	size, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dest)
		utils.Error(w, 500, "Could not save PDF")
		return
	}
	var docID string
	err = h.DB.QueryRow(r.Context(), `INSERT INTO documents(id,user_id,title,original_name,stored_name,file_size,status) VALUES($1,$2,$3,$4,$5,$6,'draft') RETURNING id`, id, userID, title, filepath.Base(header.Filename), stored, size).Scan(&docID)
	if err != nil {
		_ = os.Remove(dest)
		utils.Error(w, 500, "Could not save document details")
		return
	}
	_, _ = h.DB.Exec(r.Context(), `
		INSERT INTO audit_logs (document_id, action, performed_by, details)
		VALUES ($1, 'DOCUMENT_UPLOADED', 'Sender', $2)
	`, docID, fmt.Sprintf("Document uploaded: %s (%s)", title, filepath.Base(header.Filename)))
	utils.JSON(w, 201, map[string]any{"message": "PDF uploaded successfully", "document": map[string]any{"id": docID, "title": title, "originalName": filepath.Base(header.Filename), "fileSize": size, "status": "draft"}})
}
func (h DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(r.Context(), `SELECT id,user_id,title,original_name,file_size,status,created_at FROM documents WHERE user_id=$1 ORDER BY created_at DESC`, middleware.CurrentUserID(r))
	if err != nil {
		utils.Error(w, 500, "Could not load documents")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, title, name, status string
		var size int64
		var created any
		if err = rows.Scan(&id, &uid, &title, &name, &size, &status, &created); err != nil {
			utils.Error(w, 500, "Could not read documents")
			return
		}
		items = append(items, map[string]any{"id": id, "userId": uid, "title": title, "originalName": name, "fileSize": size, "status": status, "createdAt": created})
	}
	utils.JSON(w, 200, map[string]any{"documents": items})
}
func (h DocumentHandler) View(w http.ResponseWriter, r *http.Request)     { h.serveFile(w, r, false) }
func (h DocumentHandler) Download(w http.ResponseWriter, r *http.Request) { h.serveFile(w, r, true) }
func (h DocumentHandler) serveFile(w http.ResponseWriter, r *http.Request, download bool) {
	id := r.PathValue("id")
	var stored, name string
	err := h.DB.QueryRow(r.Context(), `SELECT stored_name,original_name FROM documents WHERE id=$1 AND user_id=$2`, id, middleware.CurrentUserID(r)).Scan(&stored, &name)
	if err == pgx.ErrNoRows {
		utils.Error(w, 404, "Document not found")
		return
	}
	if err != nil {
		utils.Error(w, 500, "Could not load document")
		return
	}
	path := filepath.Join(h.UploadDir, filepath.Base(stored))
	w.Header().Set("Content-Type", "application/pdf")
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(name, "\"", "")))
	} else {
		w.Header().Set("Content-Disposition", "inline")
	}
	http.ServeFile(w, r, path)
}
func (h DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var stored string
	err := h.DB.QueryRow(r.Context(), `DELETE FROM documents WHERE id=$1 AND user_id=$2 RETURNING stored_name`, id, middleware.CurrentUserID(r)).Scan(&stored)
	if err == pgx.ErrNoRows {
		utils.Error(w, 404, "Document not found")
		return
	}
	if err != nil {
		utils.Error(w, 500, "Could not delete document")
		return
	}
	_ = os.Remove(filepath.Join(h.UploadDir, filepath.Base(stored)))
	utils.JSON(w, 200, map[string]string{"message": "Document deleted successfully"})
}
