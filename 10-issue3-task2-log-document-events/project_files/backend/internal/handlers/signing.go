package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"phase1-signora/backend/internal/models"
	"phase1-signora/backend/internal/services"
	"phase1-signora/backend/internal/utils"
)

type SigningHandler struct {
	DB        *pgxpool.Pool
	UploadDir string
	SignedDir string
	Mailer    *services.Mailer
}

type SubmitSignatureRequest struct {
	SignatureData string `json:"signatureData"`
}

// GetSigningInfo loads document and boxes for a given signing token
func (h SigningHandler) GetSigningInfo(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if strings.TrimSpace(token) == "" {
		utils.Error(w, 400, "Signing token is required")
		return
	}

	var signer models.Signer
	var docTitle, originalName, docStatus string
	var signedAt *time.Time

	err := h.DB.QueryRow(r.Context(), `
		SELECT s.id, s.document_id, s.name, s.email, s.token, s.token_expires_at, s.status, s.signed_at,
		       d.title, d.original_name, d.status
		FROM signers s
		JOIN documents d ON s.document_id = d.id
		WHERE s.token = $1
	`, token).Scan(
		&signer.ID, &signer.DocumentID, &signer.Name, &signer.Email, &signer.Token,
		&signer.TokenExpiresAt, &signer.Status, &signedAt,
		&docTitle, &originalName, &docStatus,
	)
	if err == pgx.ErrNoRows {
		utils.JSON(w, 404, map[string]string{
			"error": "Invalid signing link. The link does not exist or has been removed.",
			"state": "invalid",
		})
		return
	}
	if err != nil {
		utils.Error(w, 500, "Could not verify signing token")
		return
	}
	signer.SignedAt = signedAt

	// Check if already signed
	if signer.Status == "signed" {
		utils.JSON(w, 409, map[string]string{
			"error": "You have already completed signing this document.",
			"state": "signed",
		})
		return
	}

	// Check if expired
	if time.Now().After(signer.TokenExpiresAt) {
		utils.JSON(w, 410, map[string]string{
			"error": "This signing link has expired. Please contact the sender to request a new link.",
			"state": "expired",
		})
		return
	}
	if docStatus != "pending" {
		state := "closed"
		message := "This document is not currently accepting signatures."
		if docStatus == "draft" {
			state = "draft"
			message = "This document has not been sent for signing yet."
		}
		utils.JSON(w, 409, map[string]string{"error": message, "state": state})
		return
	}

	// Fetch boxes assigned to this signer
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, document_id, signer_id, page, x, y, width, height, signed
		FROM signature_boxes
		WHERE document_id = $1 AND signer_id = $2
		ORDER BY page ASC, y ASC
	`, signer.DocumentID, signer.ID)
	if err != nil {
		utils.Error(w, 500, "Could not load signature boxes")
		return
	}
	defer rows.Close()

	var boxes []models.SignatureBox
	for rows.Next() {
		var b models.SignatureBox
		var signerID *string
		if err := rows.Scan(&b.ID, &b.DocumentID, &signerID, &b.Page, &b.X, &b.Y, &b.Width, &b.Height, &b.Signed); err != nil {
			utils.Error(w, 500, "Could not read signature boxes")
			return
		}
		if !validBoxGeometry(b.Page, b.X, b.Y, b.Width, b.Height) || signerID == nil || *signerID != signer.ID {
			utils.Error(w, 422, "A signature box is invalid. Please contact the sender.")
			return
		}
		b.SignerID = signerID
		b.SignerName = signer.Name
		b.SignerEmail = signer.Email
		boxes = append(boxes, b)
	}
	if err := rows.Err(); err != nil {
		utils.Error(w, 500, "Could not read signature boxes")
		return
	}
	// Audit: record the first time this signer opens the link (not every refresh)
	performedBy := fmt.Sprintf("%s (%s)", signer.Name, signer.Email)
	var alreadyLogged bool
	if err := h.DB.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM audit_logs WHERE document_id=$1 AND action='DOCUMENT_VIEWED' AND performed_by=$2)
	`, signer.DocumentID, performedBy).Scan(&alreadyLogged); err != nil {
		utils.Error(w, 500, "Could not check document audit trail")
		return
	}
	if !alreadyLogged {
		_, err := h.DB.Exec(r.Context(), `
			INSERT INTO audit_logs (document_id, action, performed_by, details, ip_address)
			VALUES ($1, 'DOCUMENT_VIEWED', $2, 'Signer opened the document for review', $3)
		`, signer.DocumentID, performedBy, utils.ClientIP(r))
		if err != nil {
			utils.Error(w, 500, "Could not record document view")
			return
		}
	}

	utils.JSON(w, 200, map[string]any{
		"signer": map[string]any{
			"id":     signer.ID,
			"name":   signer.Name,
			"email":  signer.Email,
			"status": signer.Status,
		},
		"document": map[string]any{
			"id":           signer.DocumentID,
			"title":        docTitle,
			"originalName": originalName,
			"status":       docStatus,
		},
		"boxes":      boxes,
		"totalBoxes": len(boxes),
	})
}

// ServeSigningPDF streams the original PDF to the signer
func (h SigningHandler) ServeSigningPDF(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var storedName string
	err := h.DB.QueryRow(r.Context(), `
		SELECT d.stored_name
		FROM signers s
		JOIN documents d ON s.document_id = d.id
		WHERE s.token = $1 AND s.status = 'pending'
		  AND s.token_expires_at > NOW() AND d.status = 'pending'
	`, token).Scan(&storedName)
	if err == pgx.ErrNoRows {
		utils.Error(w, 404, "Document not found")
		return
	}
	if err != nil {
		utils.Error(w, 500, "Could not verify signing link")
		return
	}

	path := filepath.Join(h.UploadDir, filepath.Base(storedName))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, path)
}

// SubmitSignature processes the signer's submitted signature
func (h SigningHandler) SubmitSignature(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20) // signature images are small; reject huge bodies

	var in SubmitSignatureRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		utils.Error(w, 400, "Invalid request body")
		return
	}

	in.SignatureData = strings.TrimSpace(in.SignatureData)
	if err := validateSignatureData(in.SignatureData); err != nil {
		utils.Error(w, 400, err.Error())
		return
	}

	var signerID, docID, signerName, signerEmail, currentStatus string
	var expiresAt time.Time
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		utils.Error(w, 500, "Could not start signature transaction")
		return
	}
	defer tx.Rollback(r.Context())

	var documentStatus string
	err = tx.QueryRow(r.Context(), `
		SELECT s.id, s.document_id, s.name, s.email, s.status, s.token_expires_at, d.status
		FROM signers s
		JOIN documents d ON d.id = s.document_id
		WHERE s.token = $1
		FOR UPDATE OF s
	`, token).Scan(&signerID, &docID, &signerName, &signerEmail, &currentStatus, &expiresAt, &documentStatus)
	if err == pgx.ErrNoRows {
		utils.JSON(w, 404, map[string]string{"error": "Invalid signing link", "state": "invalid"})
		return
	}
	if err != nil {
		utils.Error(w, 500, "Could not verify signing token")
		return
	}

	if currentStatus == "signed" {
		utils.JSON(w, 409, map[string]string{
			"error": "You have already submitted your signature for this document.",
			"state": "signed",
		})
		return
	}
	if time.Now().After(expiresAt) {
		utils.JSON(w, 410, map[string]string{"error": "This signing link has expired.", "state": "expired"})
		return
	}
	if documentStatus != "pending" {
		state := "closed"
		message := "This document is not currently accepting signatures."
		if documentStatus == "draft" {
			state = "draft"
			message = "This document has not been sent for signing yet."
		}
		utils.JSON(w, 409, map[string]string{"error": message, "state": state})
		return
	}

	var assignedBoxes int
	if err := tx.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM signature_boxes
		WHERE document_id = $1 AND signer_id = $2
	`, docID, signerID).Scan(&assignedBoxes); err != nil {
		utils.Error(w, 500, "Could not verify assigned signature boxes")
		return
	}
	if assignedBoxes == 0 {
		utils.Error(w, 422, "No signature boxes are assigned to this signer. Please contact the sender.")
		return
	}

	// Persist the signature and its box states atomically; the token is locked against replay.
	now := time.Now()
	tag, err := tx.Exec(r.Context(), `
		UPDATE signers
		SET status = 'signed', signed_at = $1, signature_data = $2
		WHERE id = $3 AND status = 'pending' AND token = $4
	`, now, in.SignatureData, signerID, token)
	if err != nil {
		utils.Error(w, 500, "Could not save signature")
		return
	}
	if tag.RowsAffected() == 0 {
		utils.Error(w, 409, "You have already submitted your signature for this document.")
		return
	}

	// Mark all boxes for this signer as signed
	if _, err := tx.Exec(r.Context(), `
		UPDATE signature_boxes
		SET signed = true
		WHERE document_id = $1 AND signer_id = $2
	`, docID, signerID); err != nil {
		utils.Error(w, 500, "Could not update signature boxes")
		return
	}

	// Audit log (includes the signer's IP address)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO audit_logs (document_id, action, performed_by, details, ip_address)
		VALUES ($1, 'DOCUMENT_SIGNED', $2, 'Signer submitted electronic signature', $3)
	`, docID, fmt.Sprintf("%s (%s)", signerName, signerEmail), utils.ClientIP(r))
	if err != nil {
		utils.Error(w, 500, "Could not record signature audit event")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		utils.Error(w, 500, "Could not commit signature")
		return
	}

	// Check if all signers on this document have signed
	var pendingCount int
	err = h.DB.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM signers
		WHERE document_id = $1 AND status != 'signed'
	`, docID).Scan(&pendingCount)
	if err != nil {
		utils.Error(w, 500, "Signature was recorded, but signer status could not be verified")
		return
	}

	allCompleted := pendingCount == 0
	if allCompleted {
		// Do not report completion until the signed PDF and its certificate are written.
		if err := services.CompleteDocument(context.Background(), h.DB, h.Mailer, h.UploadDir, h.SignedDir, docID); err != nil {
			log.Printf("complete document %s: %v", docID, err)
			utils.Error(w, 500, "Signature was recorded, but PDF finalization failed. Please contact the sender to regenerate the signed PDF.")
			return
		}
	}

	utils.JSON(w, 200, map[string]any{
		"message":      "Signature recorded successfully!",
		"signerName":   signerName,
		"signedAt":     now,
		"allCompleted": allCompleted,
	})
}
