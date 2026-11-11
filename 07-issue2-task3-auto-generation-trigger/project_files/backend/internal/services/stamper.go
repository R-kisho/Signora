package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"phase1-signora/backend/internal/models"
)

type stampBox struct {
	SignerID      string  `json:"signerId"`
	Page          int     `json:"page"`
	X             float64 `json:"x"` // percent of page width, from the left
	Y             float64 `json:"y"` // percent of page height, from the TOP (browser coordinates)
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	SignerName    string  `json:"signerName"`
	SignerEmail   string  `json:"signerEmail"`
	SignatureData string  `json:"signatureData"`
	SignedAt      string  `json:"signedAt"`
}

type stampSigner struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	SignedAt string `json:"signedAt"`
	IP       string `json:"ip"`
}

// GenerateSignedPDF stamps every signature onto a COPY of the original PDF, appends a Certificate of
// Completion, saves it in signedDir and marks the document completed.
// It refuses to run until every signer has signed. The original file is never modified.
func GenerateSignedPDF(ctx context.Context, db *pgxpool.Pool, uploadDir, signedDir, documentID string) error {
	var storedName, title string
	if err := db.QueryRow(ctx, `SELECT stored_name, title FROM documents WHERE id=$1`, documentID).Scan(&storedName, &title); err != nil {
		return fmt.Errorf("document not found: %w", err)
	}

	var total, pending int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status <> 'signed') FROM signers WHERE document_id=$1
	`, documentID).Scan(&total, &pending); err != nil {
		return fmt.Errorf("could not count signers: %w", err)
	}
	if total == 0 || pending > 0 {
		return fmt.Errorf("not all signers have signed yet")
	}

	origPath := filepath.Join(uploadDir, filepath.Base(storedName))
	origBytes, err := os.ReadFile(origPath)
	if err != nil {
		return fmt.Errorf("cannot read original PDF: %w", err)
	}
	origSum := sha256.Sum256(origBytes)
	origHash := hex.EncodeToString(origSum[:])

	if err := os.MkdirAll(signedDir, 0755); err != nil {
		return fmt.Errorf("cannot create signed folder: %w", err)
	}
	signedName := "signed_" + filepath.Base(storedName)
	outPath := filepath.Join(signedDir, signedName)

	// Signature boxes (one signature image per signer, reused for each of their boxes)
	boxRows, err := db.Query(ctx, `
		SELECT COALESCE(s.id::text, ''), b.page, b.x, b.y, b.width, b.height,
		       COALESCE(s.name, ''), COALESCE(s.email, ''), COALESCE(s.signature_data, ''),
		       s.signed_at, COALESCE(s.status, '')
		FROM signature_boxes b
		LEFT JOIN signers s ON b.signer_id = s.id AND b.document_id = s.document_id
		WHERE b.document_id = $1
		ORDER BY b.page ASC, b.y ASC, b.x ASC
	`, documentID)
	if err != nil {
		return fmt.Errorf("could not load signature boxes: %w", err)
	}
	var boxes []stampBox
	for boxRows.Next() {
		var bx stampBox
		var signedAt *time.Time
		var signerStatus string
		if err := boxRows.Scan(&bx.SignerID, &bx.Page, &bx.X, &bx.Y, &bx.Width, &bx.Height,
			&bx.SignerName, &bx.SignerEmail, &bx.SignatureData, &signedAt, &signerStatus); err != nil {
			boxRows.Close()
			return fmt.Errorf("could not read signature box: %w", err)
		}
		if bx.SignerID == "" || signerStatus != "signed" || signedAt == nil || strings.TrimSpace(bx.SignatureData) == "" {
			boxRows.Close()
			return fmt.Errorf("signature box on page %d has no valid signed owner or signature", bx.Page)
		}
		bx.SignedAt = signedAt.UTC().Format("2006-01-02")
		boxes = append(boxes, bx)
	}
	if err := boxRows.Err(); err != nil {
		boxRows.Close()
		return fmt.Errorf("could not read signature boxes: %w", err)
	}
	boxRows.Close()
	if len(boxes) == 0 {
		return fmt.Errorf("document has no signature boxes to stamp")
	}

	// Signers with the IP they signed from (taken from the audit trail)
	signerRows, err := db.Query(ctx, `
		SELECT s.id, s.name, s.email, s.signed_at, s.status,
		       COALESCE((SELECT a.ip_address FROM audit_logs a
		                 WHERE a.document_id = s.document_id AND a.action = 'DOCUMENT_SIGNED'
		                   AND a.performed_by = s.name || ' (' || s.email || ')'
		                 ORDER BY a.created_at DESC LIMIT 1), '')
		FROM signers s
		WHERE s.document_id = $1
		ORDER BY s.created_at ASC
	`, documentID)
	if err != nil {
		return fmt.Errorf("could not load signers: %w", err)
	}
	var signers []stampSigner
	for signerRows.Next() {
		var sg stampSigner
		var signedAt *time.Time
		var status string
		if err := signerRows.Scan(&sg.ID, &sg.Name, &sg.Email, &signedAt, &status, &sg.IP); err != nil {
			signerRows.Close()
			return fmt.Errorf("could not read signer: %w", err)
		}
		if status != "signed" || signedAt == nil {
			signerRows.Close()
			return fmt.Errorf("signer %s has not completed signing", sg.ID)
		}
		sg.SignedAt = signedAt.UTC().Format(time.RFC3339)
		signers = append(signers, sg)
	}
	if err := signerRows.Err(); err != nil {
		signerRows.Close()
		return fmt.Errorf("could not read signers: %w", err)
	}
	signerRows.Close()
	if len(signers) == 0 {
		return fmt.Errorf("document has no signers")
	}

	// Audit trail for the certificate (oldest first)
	logRows, err := db.Query(ctx, `
		SELECT id, document_id, action, performed_by, COALESCE(details, ''), COALESCE(ip_address, ''), created_at
		FROM audit_logs WHERE document_id = $1 ORDER BY created_at ASC
	`, documentID)
	if err != nil {
		return fmt.Errorf("could not load audit trail: %w", err)
	}
	var logs []models.AuditLog
	for logRows.Next() {
		var l models.AuditLog
		if err := logRows.Scan(&l.ID, &l.DocumentID, &l.Action, &l.PerformedBy, &l.Details, &l.IPAddress, &l.CreatedAt); err != nil {
			logRows.Close()
			return fmt.Errorf("could not read audit trail: %w", err)
		}
		logs = append(logs, l)
	}
	if err := logRows.Err(); err != nil {
		logRows.Close()
		return fmt.Errorf("could not read audit trail: %w", err)
	}
	logRows.Close()

	// Stamper writes to a temp file first; we rename it so a half-written PDF is never served.
	tmpOut, err := os.CreateTemp(signedDir, "signed_*.pdf.tmp")
	if err != nil {
		return fmt.Errorf("temp output: %w", err)
	}
	tmpOutPath := tmpOut.Name()
	tmpOut.Close()
	defer os.Remove(tmpOutPath)

	payload := map[string]any{
		"originalPdfPath": origPath,
		"outputPdfPath":   tmpOutPath,
		"documentId":      documentID,
		"docTitle":        title,
		"originalSha256":  origHash,
		"boxes":           boxes,
		"signers":         signers,
		"auditLogs":       logs,
		"completedAt":     time.Now().UTC().Format(time.RFC3339),
	}

	tmpPayload, err := os.CreateTemp("", "stamp_*.json")
	if err != nil {
		return fmt.Errorf("temp payload: %w", err)
	}
	tmpPayloadPath := tmpPayload.Name()
	defer os.Remove(tmpPayloadPath)
	if err := json.NewEncoder(tmpPayload).Encode(payload); err != nil {
		tmpPayload.Close()
		return fmt.Errorf("encode payload: %w", err)
	}
	tmpPayload.Close()

	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	output, err := exec.CommandContext(runCtx, "node", findStampScript(), tmpPayloadPath).CombinedOutput()
	if err != nil {
		log.Printf("stamp_pdf error: %v (output: %s)", err, string(output))
		return fmt.Errorf("PDF stamper failed (is Node.js installed and `npm install` run in backend/?): %v", err)
	}

	signedBytes, err := os.ReadFile(tmpOutPath)
	if err != nil {
		return fmt.Errorf("could not read stamped PDF: %w", err)
	}
	if len(signedBytes) < 5 || string(signedBytes[:5]) != "%PDF-" {
		return fmt.Errorf("PDF stamper did not produce a valid PDF")
	}
	signedSum := sha256.Sum256(signedBytes)
	signedHash := hex.EncodeToString(signedSum[:])

	if err := os.Rename(tmpOutPath, outPath); err != nil {
		return fmt.Errorf("could not save signed PDF: %w", err)
	}
	_ = os.Chmod(outPath, 0644)

	// Mark completed. Only the first caller changes the status, so the audit row is written once.
	tag, err := db.Exec(ctx, `UPDATE documents SET signed_file_name = $1, status = 'completed' WHERE id = $2 AND status <> 'completed'`, signedName, documentID)
	if err != nil {
		return fmt.Errorf("could not update document: %w", err)
	}
	if tag.RowsAffected() > 0 {
		LogAudit(ctx, db, documentID, "DOCUMENT_COMPLETED", "System",
			"All signers signed. Signed PDF generated. SHA-256: "+signedHash, "")
	} else {
		_, _ = db.Exec(ctx, `UPDATE documents SET signed_file_name = $1 WHERE id = $2`, signedName, documentID)
	}
	return nil
}

func findStampScript() string {
	for _, p := range []string{
		filepath.Join("scripts", "stamp_pdf.js"),
		filepath.Join("backend", "scripts", "stamp_pdf.js"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("scripts", "stamp_pdf.js")
}
