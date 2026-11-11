package handlers

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"phase1-signora/backend/internal/services"
)

// logAudit writes an audit trail entry (see services.LogAudit).
func logAudit(ctx context.Context, db *pgxpool.Pool, docID, action, by, details, ip string) {
	services.LogAudit(ctx, db, docID, action, by, details, ip)
}

// userLabel returns "Name (email)" for the logged-in sender so audit rows show a real user.
func userLabel(ctx context.Context, db *pgxpool.Pool, userID string) string {
	var name, email string
	if err := db.QueryRow(ctx, `SELECT name, email FROM users WHERE id=$1`, userID).Scan(&name, &email); err != nil {
		return "Sender"
	}
	return name + " (" + email + ")"
}

// shorten keeps error text readable in API messages and audit rows.
func shorten(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
