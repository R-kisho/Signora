CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(120) NOT NULL,
  email VARCHAR(255) NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS documents (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title VARCHAR(200) NOT NULL,
  original_name TEXT NOT NULL,
  stored_name TEXT NOT NULL UNIQUE,
  signed_file_name TEXT,
  file_size BIGINT NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending','completed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_documents_user_created ON documents(user_id, created_at DESC);

-- Signers table
CREATE TABLE IF NOT EXISTS signers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  name VARCHAR(120) NOT NULL,
  email VARCHAR(255) NOT NULL,
  token VARCHAR(64) UNIQUE NOT NULL,
  token_expires_at TIMESTAMPTZ NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'signed')),
  signed_at TIMESTAMPTZ,
  signature_data TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_document_signer_email UNIQUE (document_id, email)
);

CREATE INDEX IF NOT EXISTS idx_signers_token ON signers(token);
CREATE INDEX IF NOT EXISTS idx_signers_document_id ON signers(document_id);

-- Signature boxes placed on documents
CREATE TABLE IF NOT EXISTS signature_boxes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  signer_id UUID REFERENCES signers(id) ON DELETE SET NULL,
  page INT NOT NULL DEFAULT 1,
  x DOUBLE PRECISION NOT NULL,
  y DOUBLE PRECISION NOT NULL,
  width DOUBLE PRECISION NOT NULL,
  height DOUBLE PRECISION NOT NULL,
  signed BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_signature_boxes_document_id ON signature_boxes(document_id);
CREATE INDEX IF NOT EXISTS idx_signature_boxes_signer_id ON signature_boxes(signer_id);

-- Audit logs for timeline and tracking
CREATE TABLE IF NOT EXISTS audit_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  action VARCHAR(50) NOT NULL,
  performed_by VARCHAR(255) NOT NULL,
  details TEXT,
  ip_address VARCHAR(45),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_document_created ON audit_logs(document_id, created_at ASC);

-- Email tracking + one-time completion email (safe to run on an existing database)
ALTER TABLE signers   ADD COLUMN IF NOT EXISTS email_sent BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE signers   ADD COLUMN IF NOT EXISTS email_sent_at TIMESTAMPTZ;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS completion_emailed_at TIMESTAMPTZ;
