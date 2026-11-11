package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Gmail rejects messages above ~25 MB (base64 adds ~33%), so larger PDFs are not attached.
const maxAttachmentBytes = 15 * 1024 * 1024

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFilename(s string) string {
	s = strings.Trim(unsafeFileChars.ReplaceAllString(s, "_"), "_")
	if s == "" {
		return "document"
	}
	return s
}

// CompleteDocument generates the signed PDF and then emails it to everyone.
// A PDF failure is recorded in the audit trail and returned; the document is NOT marked completed.
func CompleteDocument(ctx context.Context, db *pgxpool.Pool, mailer *Mailer, uploadDir, signedDir, documentID string) error {
	if err := GenerateSignedPDF(ctx, db, uploadDir, signedDir, documentID); err != nil {
		LogAudit(ctx, db, documentID, "PDF_GENERATION_FAILED", "System", truncate(err.Error(), 300), "")
		return err
	}
	sendCompletionEmails(ctx, db, mailer, signedDir, documentID)
	return nil
}

// sendCompletionEmails emails the signed PDF to the owner and every signer.
// completion_emailed_at is claimed atomically so concurrent submits cannot send duplicates.
func sendCompletionEmails(ctx context.Context, db *pgxpool.Pool, mailer *Mailer, signedDir, documentID string) {
	var title, ownerName, ownerEmail, signedName string
	err := db.QueryRow(ctx, `
		UPDATE documents d
		SET completion_emailed_at = NOW()
		FROM users u
		WHERE d.id = $1 AND u.id = d.user_id
		  AND d.completion_emailed_at IS NULL AND d.signed_file_name IS NOT NULL
		RETURNING d.title, u.name, u.email, d.signed_file_name
	`, documentID).Scan(&title, &ownerName, &ownerEmail, &signedName)
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Printf("completion email claim error: %v", err)
		}
		return // already sent (or nothing to send)
	}

	type recipient struct{ name, email string }
	recipients := []recipient{{ownerName, ownerEmail}}
	seen := map[string]bool{strings.ToLower(ownerEmail): true}

	rows, err := db.Query(ctx, `SELECT name, email FROM signers WHERE document_id = $1 ORDER BY created_at ASC`, documentID)
	if err == nil {
		for rows.Next() {
			var n, e string
			if rows.Scan(&n, &e) == nil && !seen[strings.ToLower(e)] {
				seen[strings.ToLower(e)] = true
				recipients = append(recipients, recipient{n, e})
			}
		}
		rows.Close()
	}

	var atts []Attachment
	attached := false
	signedPath := filepath.Join(signedDir, filepath.Base(signedName))
	if st, statErr := os.Stat(signedPath); statErr == nil && st.Size() <= maxAttachmentBytes {
		atts = []Attachment{{Filename: safeFilename(title) + "_signed.pdf", Path: signedPath}}
		attached = true
	}

	sent := 0
	for _, rc := range recipients {
		subject, htmlBody, textBody := CompletionEmail(rc.name, title, attached)
		if sendErr := mailer.Send(rc.email, subject, htmlBody, textBody, atts); sendErr != nil {
			LogAudit(ctx, db, documentID, "EMAIL_FAILED", "System",
				truncate(fmt.Sprintf("Completion email to %s failed: %v", rc.email, sendErr), 300), "")
			continue
		}
		sent++
	}
	LogAudit(ctx, db, documentID, "COMPLETION_EMAIL_SENT", "System",
		fmt.Sprintf("Signed PDF emailed to %d of %d recipient(s)", sent, len(recipients)), "")

	if sent == 0 { // allow a retry (e.g. after fixing SMTP settings)
		_, _ = db.Exec(ctx, `UPDATE documents SET completion_emailed_at = NULL WHERE id = $1`, documentID)
	}
}
