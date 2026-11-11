# Go API

## Requirements
- Go 1.22+
- PostgreSQL 14+

## Setup
1. Create database: `CREATE DATABASE signora_db;`
2. Run schema: `psql -U postgres -d signora_db -f schema.sql`
3. Copy `.env.example` to `.env` and edit values.
4. Load environment variables (PowerShell): `$env:DATABASE_URL="postgres://postgres:password@localhost:5432/signora_db?sslmode=disable"`; set `JWT_SECRET`, `FRONTEND_URL`, `UPLOAD_DIR` similarly.
5. `go mod tidy`
6. `go run ./cmd/server`

API runs at http://localhost:8080. Uploads are stored locally in `uploads/`.
