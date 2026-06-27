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
    otp_expiry      TIMESTAMPTZ,
    suspended_by_cascade BOOLEAN DEFAULT FALSE
);

-- ============================================================
-- exams
-- ============================================================
CREATE TABLE IF NOT EXISTS exams (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    size_bytes      BIGINT DEFAULT 0,
    token           TEXT NOT NULL UNIQUE,
    questions_json  TEXT,
    status          TEXT DEFAULT 'active'
                    CHECK (status IN ('active', 'inactive')),
    security_level  TEXT DEFAULT 'medium'
                    CHECK (security_level IN ('low', 'medium', 'high')),
    strict_mode     INTEGER DEFAULT 0,
    public_results  INTEGER DEFAULT 1,
    show_answers    INTEGER DEFAULT 1,
    created_by      INTEGER REFERENCES admin_users(id),
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    identity_fields TEXT,
    panel_color     TEXT,
    start_time      TIMESTAMPTZ,
    end_time        TIMESTAMPTZ,
    delegated_to    INTEGER REFERENCES admin_users(id),
    token_mode          TEXT DEFAULT 'static'
                        CHECK (token_mode IN ('static', 'dynamic')),
    token_reset_interval INTEGER,
    token_last_reset_at  TIMESTAMPTZ,
    active_token        TEXT NOT NULL DEFAULT '',
    exam_started_at     TIMESTAMPTZ
);

-- ============================================================
-- exam_pengawas (junction table)
-- ============================================================
CREATE TABLE IF NOT EXISTS exam_pengawas (
    id      SERIAL PRIMARY KEY,
    exam_id INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    UNIQUE(exam_id, user_id)
);

-- ============================================================
-- submissions
-- ============================================================
CREATE TABLE IF NOT EXISTS submissions (
    id            SERIAL PRIMARY KEY,
    exam_id       INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    student_name  TEXT DEFAULT '',
    exam_number   TEXT DEFAULT '',
    student_class TEXT DEFAULT '',
    answers_json  TEXT,
    score         DOUBLE PRECISION,
    start_time    TEXT,
    mac_address   TEXT DEFAULT '',
    created_at    TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    identity_data TEXT
);

-- ============================================================
-- saas_settings
-- ============================================================
CREATE TABLE IF NOT EXISTS saas_settings (
    id    SERIAL PRIMARY KEY,
    key   TEXT NOT NULL UNIQUE,
    value TEXT NOT NULL DEFAULT ''
);

-- ============================================================
-- student_access_logs
-- ============================================================
CREATE TABLE IF NOT EXISTS student_access_logs (
    id                 SERIAL PRIMARY KEY,
    exam_id            INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    submission_id      INTEGER REFERENCES submissions(id) ON DELETE SET NULL,
    student_identifier TEXT DEFAULT '',
    student_name       TEXT DEFAULT '',
    exam_number        TEXT DEFAULT '',
    student_class      TEXT DEFAULT '',
    event              TEXT NOT NULL,
    ip_address         TEXT DEFAULT '',
    device_info        TEXT DEFAULT '',
    created_at         TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- Indexes for frequently queried foreign key columns
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_exams_created_by ON exams(created_by);
CREATE INDEX IF NOT EXISTS idx_exams_delegated_to ON exams(delegated_to);
CREATE INDEX IF NOT EXISTS idx_exam_pengawas_exam_id ON exam_pengawas(exam_id);
CREATE INDEX IF NOT EXISTS idx_exam_pengawas_user_id ON exam_pengawas(user_id);
CREATE INDEX IF NOT EXISTS idx_submissions_exam_id ON submissions(exam_id);
CREATE INDEX IF NOT EXISTS idx_student_access_logs_exam_id ON student_access_logs(exam_id);
CREATE INDEX IF NOT EXISTS idx_student_access_logs_identifier ON student_access_logs(student_identifier);
CREATE INDEX IF NOT EXISTS idx_saas_settings_key ON saas_settings(key);

-- ============================================================
-- Migration: instansi_id + suspended_by_cascade (safe to re-run)
-- ============================================================
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS instansi_id INTEGER REFERENCES instansi(id);
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS suspended_by_cascade BOOLEAN DEFAULT FALSE;

-- Populate instansi reference table
INSERT INTO instansi (name)
SELECT DISTINCT instansi FROM admin_users WHERE instansi != '' AND instansi IS NOT NULL
ON CONFLICT (name) DO NOTHING;

-- Update instansi_id from instansi name
UPDATE admin_users u SET instansi_id = i.id
FROM instansi i WHERE u.instansi = i.name AND u.instansi_id IS NULL;

-- ============================================================
-- Migration: token_mode + token_reset_interval + token_last_reset_at (safe to re-run)
-- ============================================================
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_mode TEXT DEFAULT 'static';
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_reset_interval INTEGER;
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_last_reset_at TIMESTAMPTZ;
ALTER TABLE exams ADD COLUMN IF NOT EXISTS active_token TEXT NOT NULL DEFAULT '';
ALTER TABLE exams ADD COLUMN IF NOT EXISTS exam_started_at TIMESTAMPTZ;

-- Set active_token = token for existing rows where active_token is empty
UPDATE exams SET active_token = token WHERE active_token = '' OR active_token IS NULL;
