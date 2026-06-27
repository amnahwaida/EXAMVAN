-- EXAMVAN PostgreSQL Schema
-- Complete DDL with ALL columns (31 migrations baked in).
-- Run once on fresh install or migration from SQLite.

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

-- ============================================================
-- exams
-- ============================================================
CREATE TABLE IF NOT EXISTS exams (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    size_bytes      INTEGER NOT NULL DEFAULT 0,
    token           TEXT NOT NULL UNIQUE,
    questions_json  TEXT,
    status          TEXT DEFAULT 'active'
                    CHECK (status IN ('active', 'inactive')),
    security_level  TEXT DEFAULT 'medium'
                    CHECK (security_level IN ('medium', 'low', 'high')),
    strict_mode     INTEGER DEFAULT 0,
    public_results  INTEGER DEFAULT 1,
    show_answers    INTEGER DEFAULT 0,
    identity_fields TEXT,
    panel_color     TEXT DEFAULT '#6366f1',
    start_time      TIMESTAMPTZ,
    end_time        TIMESTAMPTZ,
    delegated_to    INTEGER REFERENCES admin_users(id),
    created_by      INTEGER DEFAULT 1 REFERENCES admin_users(id),
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_exams_token ON exams(token);
CREATE INDEX IF NOT EXISTS idx_exams_created_by ON exams(created_by);
CREATE INDEX IF NOT EXISTS idx_exams_status ON exams(status);

-- ============================================================
-- submissions
-- ============================================================
CREATE TABLE IF NOT EXISTS submissions (
    id              SERIAL PRIMARY KEY,
    exam_id         INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    student_name    TEXT NOT NULL,
    exam_number     TEXT,
    student_class   TEXT,
    answers_json    TEXT,
    score           REAL,
    start_time      TIMESTAMPTZ,
    mac_address     TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    identity_data   TEXT
);

CREATE INDEX IF NOT EXISTS idx_submissions_exam ON submissions(exam_id);
CREATE INDEX IF NOT EXISTS idx_submissions_student ON submissions(student_name);

-- ============================================================
-- exam_pengawas (junction table)
-- ============================================================
CREATE TABLE IF NOT EXISTS exam_pengawas (
    id      SERIAL PRIMARY KEY,
    exam_id INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    UNIQUE (exam_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_exam_pengawas_exam ON exam_pengawas(exam_id);
CREATE INDEX IF NOT EXISTS idx_exam_pengawas_user ON exam_pengawas(user_id);

-- ============================================================
-- student_access_logs
-- ============================================================
CREATE TABLE IF NOT EXISTS student_access_logs (
    id                  SERIAL PRIMARY KEY,
    exam_id             INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    submission_id       INTEGER REFERENCES submissions(id) ON DELETE SET NULL,
    student_identifier  TEXT NOT NULL,
    student_name        TEXT,
    exam_number         TEXT,
    student_class       TEXT,
    event               TEXT NOT NULL
                        CHECK (event IN ('login', 'logout', 'heartbeat')),
    ip_address          TEXT DEFAULT '',
    device_info         TEXT DEFAULT '',
    created_at          TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_access_logs_exam ON student_access_logs(exam_id);
CREATE INDEX IF NOT EXISTS idx_access_logs_identifier ON student_access_logs(student_identifier);
CREATE INDEX IF NOT EXISTS idx_access_logs_exam_identifier ON student_access_logs(exam_id, student_identifier);

-- ============================================================
-- saas_settings
-- ============================================================
CREATE TABLE IF NOT EXISTS saas_settings (
    key     TEXT PRIMARY KEY,
    value   TEXT
);

-- ============================================================
-- rate_limits
-- ============================================================
CREATE TABLE IF NOT EXISTS rate_limits (
    key         TEXT PRIMARY KEY,
    timestamps  TEXT,
    updated_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- _migrations (tracking table — kept for reference, unused in PG)
-- ============================================================
CREATE TABLE IF NOT EXISTS _migrations (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    applied_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
