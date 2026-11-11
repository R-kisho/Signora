package models

import "time"

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

type Document struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	Title          string    `json:"title"`
	OriginalName   string    `json:"originalName"`
	StoredName     string    `json:"storedName,omitempty"`
	SignedFileName *string   `json:"signedFileName,omitempty"`
	FileSize       int64     `json:"fileSize"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Signer struct {
	ID             string     `json:"id"`
	DocumentID     string     `json:"documentId"`
	Name           string     `json:"name"`
	Email          string     `json:"email"`
	Token          string     `json:"token"`
	TokenExpiresAt time.Time  `json:"tokenExpiresAt"`
	Status         string     `json:"status"` // "pending" | "signed"
	SignedAt       *time.Time `json:"signedAt,omitempty"`
	SignatureData  *string    `json:"signatureData,omitempty"`
	EmailSent      bool       `json:"emailSent"`
	EmailSentAt    *time.Time `json:"emailSentAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type SignatureBox struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"documentId"`
	SignerID    *string   `json:"signerId"`
	SignerName  string    `json:"signerName,omitempty"`
	SignerEmail string    `json:"signerEmail,omitempty"`
	Page        int       `json:"page"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	Width       float64   `json:"width"`
	Height      float64   `json:"height"`
	Signed      bool      `json:"signed"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AuditLog struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"documentId"`
	Action      string    `json:"action"`
	PerformedBy string    `json:"performedBy"`
	Details     string    `json:"details"`
	IPAddress   string    `json:"ipAddress,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type DocumentStatusSummary struct {
	Document     Document       `json:"document"`
	Signers      []Signer       `json:"signers"`
	Boxes        []SignatureBox `json:"boxes"`
	AuditLogs    []AuditLog     `json:"auditLogs"`
	TotalSigners int            `json:"totalSigners"`
	SignedCount  int            `json:"signedCount"`
	Progress     int            `json:"progress"` // 0-100%
	HasSignedPDF bool           `json:"hasSignedPdf"`
}
