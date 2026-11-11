package services

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LogAudit records one event in the document audit trail.
// ip may be empty (system events); it is then stored as NULL.
func LogAudit(ctx context.Context, db *pgxpool.Pool, docID, action, by, details, ip string) {
	var ipArg any
	if ip != "" {
		ipArg = ip
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO audit_logs (document_id, action, performed_by, details, ip_address)
		VALUES ($1, $2, $3, $4, $5)
	`, docID, action, by, details, ipArg); err != nil {
		log.Printf("audit insert failed for document %s action %s: %v", docID, action, err)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
