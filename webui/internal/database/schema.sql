-- EXAMVAN PostgreSQL Schema
-- Complete DDL with ALL columns (31 migrations baked in).
-- Run once on fresh install or migration from SQLite.

-- ============================================================
-- instansi (reference table for unique instansi names)
-- ============================================================
CREATE TABLE IF NOT EXISTS instansi (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Add instansi_id column if it doesn't exist (for existing tables)
-- MUST be before any INSERT/UPDATE referencing this column
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS instansi_id INTEGER REFERENCES instansi(id);
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS suspended_by_cascade BOOLEAN DEFAULT FALSE;

INSERT INTO instansi (name)
SELECT DISTINCT instansi FROM admin_users WHERE instansi != '' AND instansi IS NOT NULL
ON CONFLICT (name) DO NOTHING;

-- Update instansi_id from instansi name
UPDATE admin_users u SET instansi_id = i.id
FROM instansi i WHERE u.instansi = i.name AND u.instansi_id IS NULL;

-- ============================================================
-- admin_users
-- ============================================================
CREATE TABLE IF NOT EXISTS admin_users (
    id              SERIAL PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    status          TEXT DEFAULT 'active'
                    CHECK (status IN ('active', 'suspended', 'pending_otp')),
    instansi        TEXT DEFAULT '',
    instansi_id     INTEGER REFERENCES instansi(id),
    role            TEXT DEFAULT '["guru"]',
    max_exams       INTEGER DEFAULT 3,
    max_pdf_size    INTEGER DEFAULT 1048576,
    max_drafts      INTEGER DEFAULT 2,
    max_draft_size  INTEGER DEFAULT 1048576,
    whatsapp_number TEXT DEFAULT '',
    expires_at      TIMESTAMPTZ,
    otp_code        TEXT,
    otp_expiry      TIMESTAMPTZ
);
