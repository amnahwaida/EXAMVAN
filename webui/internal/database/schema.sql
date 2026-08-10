-- EXAMVAN PostgreSQL Schema
-- Complete DDL with ALL columns (31 migrations baked in).
-- Run once on fresh install or migration from SQLite.

-- ============================================================
-- instansi (reference table for unique instansi names)
-- ============================================================
CREATE TABLE IF NOT EXISTS instansi (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    code        TEXT UNIQUE,
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- admin_users
-- ============================================================
CREATE TABLE IF NOT EXISTS admin_users (
    id              SERIAL PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    name            TEXT DEFAULT '',
    email           TEXT DEFAULT '',
    password_hash   TEXT NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    status          TEXT DEFAULT 'active'
                    CHECK (status IN ('active', 'suspended', 'pending_otp')),
    instansi        TEXT DEFAULT '',
    instansi_id     INTEGER REFERENCES instansi(id),
    role            TEXT DEFAULT '["guru"]',
    max_exams       INTEGER DEFAULT 3,
    max_pdf_size    INTEGER DEFAULT 1048576,
    max_concurrent_exams INTEGER DEFAULT 2,
    max_storage_size BIGINT DEFAULT 52428800,
    whatsapp_number TEXT DEFAULT '',
    expires_at      TIMESTAMPTZ,
    otp_code        TEXT,
    otp_expiry      TIMESTAMPTZ,
    otp_attempts    INT NOT NULL DEFAULT 0,
    suspended_by_cascade BOOLEAN DEFAULT FALSE,
    -- True when the account was CREATED by an operator (a school sub-account).
    -- Such accounts may never claim/activate vouchers: their package, quota
    -- and expiry come exclusively from the school package the operator
    -- manages (see the sub-account voucher policy in README.md). Origin-based
    -- and immutable: set once at creation by the CreateUser handler, never
    -- changed afterwards. False for superadmin-created, self-registered and
    -- legacy/imported accounts.
    operator_created BOOLEAN NOT NULL DEFAULT FALSE,
    -- ID of the operator (admin_users.id) that CREATED this account — the
    -- precise per-operator attribution behind operator_created. NULL for
    -- superadmin-created, self-registered and legacy accounts (whose creator
    -- predates this column and cannot be recovered). ON DELETE SET NULL:
    -- deleting the creating operator must never block the delete (DeleteUser
    -- only cascades within the operator's instansi; a sub-account a
    -- SuperAdmin moved to another instansi would otherwise trip the FK) — the
    -- orphaned sub-account simply falls back to the legacy shared-bucket
    -- counting (created_by IS NULL).
    created_by      INTEGER REFERENCES admin_users(id) ON DELETE SET NULL
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
    status          TEXT DEFAULT 'inactive'
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
    token_mode          TEXT DEFAULT 'dynamic'
                        CHECK (token_mode IN ('static', 'dynamic')),
    token_reset_interval INTEGER,
    token_last_reset_at  TIMESTAMPTZ,
    active_token        TEXT NOT NULL DEFAULT '',
    exam_started_at     TIMESTAMPTZ,
    congrats_message    TEXT,
    auto_approve        BOOLEAN NOT NULL DEFAULT FALSE
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
-- exam_approvals
-- ============================================================
CREATE TABLE IF NOT EXISTS exam_approvals (
    id                 SERIAL PRIMARY KEY,
    exam_id            INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    mac_address        TEXT NOT NULL,
    student_name       TEXT DEFAULT '',
    exam_number        TEXT DEFAULT '',
    student_class      TEXT DEFAULT '',
    identity_data      TEXT DEFAULT '',
    status             TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at         TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exam_id, mac_address)
);

-- ============================================================
-- admin_audit_logs (jejak aksi admin yang sensitif — siapa, apa, kapan)
-- ============================================================
-- Rows are immutable append-only records. user_id/exam_id use ON DELETE SET
-- NULL (an audit trail must survive the deletion of the actor or the exam);
-- username and detail keep a denormalized snapshot for display after that.
CREATE TABLE IF NOT EXISTS admin_audit_logs (
    id          SERIAL PRIMARY KEY,
    user_id     INTEGER REFERENCES admin_users(id) ON DELETE SET NULL,
    username    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    exam_id     INTEGER REFERENCES exams(id) ON DELETE SET NULL,
    detail      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Most recent action per exam (e.g. "last auto-approve toggle" hint) + FK
-- lookups by actor.
CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_exam_time ON admin_audit_logs(exam_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_user_id ON admin_audit_logs(user_id);

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

-- Index — admin_users.instansi_id (FK ke instansi). Dipakai oleh migrasi
-- UpdateInstansi (`UPDATE admin_users SET instansi = ... WHERE instansi_id =
-- <id>`) saat operator menetapkan/merename instansi sekolah, dan melengkapi
-- audit FK: setiap kolom FK yang di-query punya index pendukung.
CREATE INDEX IF NOT EXISTS idx_admin_users_instansi_id ON admin_users(instansi_id);

-- Populate instansi reference table
INSERT INTO instansi (name)
SELECT DISTINCT instansi FROM admin_users u
WHERE instansi != '' AND instansi IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM instansi i WHERE i.name = u.instansi);

-- Update instansi_id from instansi name
UPDATE admin_users u SET instansi_id = i.id
FROM instansi i WHERE u.instansi = i.name AND u.instansi_id IS NULL;

-- ============================================================
-- Migration: token_mode + token_reset_interval + token_last_reset_at (safe to re-run)
-- ============================================================
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_mode TEXT DEFAULT 'dynamic';
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_reset_interval INTEGER;
ALTER TABLE exams ADD COLUMN IF NOT EXISTS token_last_reset_at TIMESTAMPTZ;
ALTER TABLE exams ADD COLUMN IF NOT EXISTS active_token TEXT NOT NULL DEFAULT '';
ALTER TABLE exams ADD COLUMN IF NOT EXISTS exam_started_at TIMESTAMPTZ;
-- Custom congratulations message shown on the Android page after a student
-- submits (configurable from the "Atur Soal & Kunci Jawaban" modal). Free
-- text, rendered plain on the client. Safe to re-run on every boot.
ALTER TABLE exams ADD COLUMN IF NOT EXISTS congrats_message TEXT;
-- Server-side auto-approve: when TRUE, RequestApproval approves every device
-- immediately (works even when no pengawas monitoring page is open).
ALTER TABLE exams ADD COLUMN IF NOT EXISTS auto_approve BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS email TEXT DEFAULT '';
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS max_storage_size BIGINT DEFAULT 52428800;
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS otp_attempts INT NOT NULL DEFAULT 0;

-- Widen max_pdf_size to BIGINT so large limits (e.g. the sekolah_unggulan
-- package or a custom voucher setting multi-GB sizes) fit; INTEGER overflows
-- above ~2 GB. Guarded so it only rewrites once (not on every boot). Safe to
-- re-run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'admin_users' AND column_name = 'max_pdf_size' AND data_type <> 'bigint') THEN
        ALTER TABLE admin_users ALTER COLUMN max_pdf_size TYPE BIGINT;
    END IF;
END $$;

-- ============================================================
-- Migration: dedicated concurrent-exam quota (max_concurrent_exams)
-- ============================================================
-- A dedicated column for the maximum simultaneously-RUNNING exams, separate
-- from the total exam quota. Called it out explicitly because older builds
-- mislabelled the (unused) draft-soal quota as "Ujian Serentak". Only NULL
-- rows are touched, so re-running schema.sql on every boot never clobbers
-- manually adjusted limits.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS max_concurrent_exams INTEGER;
UPDATE admin_users
SET max_concurrent_exams = COALESCE(max_concurrent_exams, 2)
WHERE max_concurrent_exams IS NULL;
ALTER TABLE admin_users ALTER COLUMN max_concurrent_exams SET DEFAULT 2;

-- New exams default to 'inactive': an uploaded exam is only joinable after the
-- admin explicitly activates AND starts it (status='active' + exam_started_at).
-- Safe to re-run on every boot; only alters the column default, not existing rows.
ALTER TABLE exams ALTER COLUMN status SET DEFAULT 'inactive';

-- Set active_token = token for existing rows where active_token is empty
UPDATE exams SET active_token = token WHERE active_token = '' OR active_token IS NULL;
UPDATE exams SET token_mode = 'dynamic' WHERE token_mode IS NULL;
UPDATE exams SET token_reset_interval = 5 WHERE token_reset_interval IS NULL;
ALTER TABLE student_access_logs ADD COLUMN IF NOT EXISTS identity_data TEXT;
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS name TEXT DEFAULT '';

-- ============================================================
-- Scaling & performance optimizations (Step 15 Priority #3)
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_access_logs_exam_time ON student_access_logs(exam_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_submissions_exam_mac ON submissions(exam_id, mac_address);
CREATE INDEX IF NOT EXISTS idx_exams_active ON exams(id) WHERE status = 'active';
-- Partial index supporting the stale-approval purge (PurgeStaleExamApprovals):
-- the ended-exam rules scan exams by end_time < now() - grace, so indexing
-- only the rows that can ever match (end_time set) keeps that scan bounded as
-- exams accumulate. Same idempotent IF NOT EXISTS style as its neighbours.
CREATE INDEX IF NOT EXISTS idx_exams_end_time ON exams(end_time) WHERE end_time IS NOT NULL;

ALTER TABLE submissions SET (autovacuum_vacuum_scale_factor = 0.01);
ALTER TABLE student_access_logs SET (autovacuum_vacuum_scale_factor = 0.01);

-- ============================================================
-- Package column & pricing/payment removals
-- ============================================================
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS package TEXT DEFAULT 'free';

-- The self-serve purchasing flow, subscription "transactions", and the DOKU
-- payment-gateway integration were removed. Vouchers are now the only
-- mechanism for granting packages/quota, so drop the legacy tables that are
-- no longer referenced by the codebase. Safe to re-run.
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS pricing_plans;

-- Clean up legacy SaaS settings that belong to the removed features.
DELETE FROM saas_settings
WHERE key LIKE 'price_%'
   OR key LIKE 'doku_%'
   OR key = 'pricing_page_enabled';

-- ============================================================
-- system_apps
-- ============================================================
CREATE TABLE IF NOT EXISTS system_apps (
    id            SERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    platform      TEXT NOT NULL,
    version       TEXT NOT NULL,
    file_path     TEXT NOT NULL,
    size_bytes    BIGINT DEFAULT 0,
    created_at    TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name, platform, version)
);

-- ============================================================
-- Vouchers & Redemptions
-- ============================================================
CREATE TABLE IF NOT EXISTS vouchers (
    id              SERIAL PRIMARY KEY,
    code            TEXT UNIQUE NOT NULL,
    package         TEXT NOT NULL,
    duration_type   TEXT NOT NULL DEFAULT 'bulanan',
    max_usage       INT NOT NULL DEFAULT 1,
    used_count      INT NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    notes           TEXT DEFAULT '',
    created_by      INT REFERENCES admin_users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS voucher_redemptions (
    id           SERIAL PRIMARY KEY,
    voucher_id   INT NOT NULL REFERENCES vouchers(id) ON DELETE CASCADE,
    user_id      INT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    redeemed_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(voucher_id, user_id)
);

-- ============================================================
-- Multiple-package selection for claimed vouchers
-- ============================================================
-- Each claimed voucher becomes a selectable "package" for the user, holding an
-- entitlement snapshot (quota + role) captured at claim time. ONLY the active
-- package consumes lifetime: remaining_seconds decreases while a package is
-- active, inactive packages are paused automatically and resume automatically
-- when activated (there is no user-facing pause/resume). Safe to re-run.
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ; -- legacy, converted & dropped below
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS package TEXT NOT NULL DEFAULT '';
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS max_exams BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS max_pdf_size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS max_concurrent_exams BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS max_storage_size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT '';
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS max_users BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS remaining_seconds BIGINT NOT NULL DEFAULT 0;
ALTER TABLE voucher_redemptions ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ;

-- At most one active package per user.
CREATE UNIQUE INDEX IF NOT EXISTS idx_voucher_redemptions_one_active
    ON voucher_redemptions(user_id) WHERE is_active;

-- Index — FK voucher_redemptions (voucher_id → vouchers, user_id →
-- admin_users). voucher_id dipakai di setiap pengecekan used_count saat
-- redeem (`WHERE voucher_id = $1`), riwayat pemakaian per voucher
-- (`GET /admin/api/vouchers/:id/redemptions`), dan join backfill legacy.
-- user_id dipakai pada seluruh lookup paket per akun (daftar "Paket yang
-- Sudah Anda Klaim", guard entitlement, job expiry/cascade). Index penuh di
-- user_id melengkapi index parsial one-active di atas untuk query yang
-- tidak memfilter is_active. Safe to re-run on every boot.
CREATE INDEX IF NOT EXISTS idx_voucher_redemptions_voucher_id ON voucher_redemptions(voucher_id);
CREATE INDEX IF NOT EXISTS idx_voucher_redemptions_user_id ON voucher_redemptions(user_id);

-- ---------------------------------------------------------------------------
-- One-time conversion of redemptions created under the older absolute-expiry
-- model (expires_at fixed at claim time, latest claim always active). Under the
-- old model a row's lifetime started when it was claimed and never paused, so:
--   * the (single) active row still holds what is left of expires_at now;
--   * every paused row still holds its full duration (it never consumed time).
-- Guarded by rows still carrying expires_at; the legacy column is dropped
-- afterwards, so this block runs exactly once per install.
DO $$
DECLARE
    any_legacy BOOLEAN;
BEGIN
    SELECT EXISTS (SELECT 1 FROM voucher_redemptions WHERE expires_at IS NOT NULL) INTO any_legacy;
    IF any_legacy THEN
        -- Rebuild the quota/package/role snapshot for rows created before the
        -- snapshot columns existed (they carry an empty package label).
        UPDATE voucher_redemptions r
        SET package = CASE WHEN v.is_custom THEN COALESCE(NULLIF(TRIM(v.custom_label), ''), 'custom') ELSE v.package END,
            max_exams = CASE
                WHEN v.is_custom THEN GREATEST(COALESCE(v.custom_max_exams, 0), 1)
                ELSE (CASE v.package
                    WHEN 'guru' THEN 1 WHEN 'individu' THEN 2
                    WHEN 'sekolah_kecil' THEN 3 WHEN 'sekolah_menengah' THEN 5
                    WHEN 'sekolah_besar' THEN 10 WHEN 'sekolah_unggulan' THEN 99999
                    ELSE 1 END)
            END,
max_pdf_size = CASE
                    WHEN v.is_custom THEN GREATEST(COALESCE(v.custom_max_pdf_size, 0), 1)
                    ELSE (CASE v.package
                        WHEN 'guru' THEN 10*1024*1024 WHEN 'individu' THEN 30*1024*1024
                        WHEN 'sekolah_kecil' THEN 50*1024*1024 WHEN 'sekolah_menengah' THEN 200*1024*1024
                        WHEN 'sekolah_besar' THEN 500::bigint*1024*1024 WHEN 'sekolah_unggulan' THEN 99999::bigint*1024*1024
                        ELSE 1*1024*1024 END)
                END,
                max_concurrent_exams = CASE
                    WHEN v.is_custom THEN GREATEST(
                        COALESCE(v.custom_max_concurrent_exams, 0),
                        COALESCE(v.custom_max_exams, 1), 1)
                    ELSE (CASE v.package
                        WHEN 'guru' THEN 1 WHEN 'individu' THEN 2
                        WHEN 'sekolah_kecil' THEN 3 WHEN 'sekolah_menengah' THEN 5
                        WHEN 'sekolah_besar' THEN 10 WHEN 'sekolah_unggulan' THEN 99999
                        ELSE 1 END)
                END,
                max_storage_size = CASE
                    WHEN v.is_custom THEN GREATEST(COALESCE(v.custom_max_storage_size, 0), 1)
                    ELSE (CASE v.package
                        WHEN 'guru' THEN 100*1024*1024 WHEN 'individu' THEN 300*1024*1024
                        WHEN 'sekolah_kecil' THEN 500*1024*1024 WHEN 'sekolah_menengah' THEN 2000*1024*1024
                        WHEN 'sekolah_besar' THEN 5000::bigint*1024*1024 WHEN 'sekolah_unggulan' THEN 999999::bigint*1024*1024
                        ELSE 50*1024*1024 END)
                END,
            role = CASE WHEN v.is_custom THEN COALESCE(v.custom_role, '')
                        WHEN v.package IN ('sekolah_kecil','sekolah_menengah','sekolah_besar','sekolah_unggulan')
                            THEN '["operator"]'
                        ELSE '' END
        FROM vouchers v
        WHERE r.voucher_id = v.id AND r.package = '';

        -- Freeze the remaining lifetime for every legacy row.
        UPDATE voucher_redemptions r
        SET remaining_seconds = CASE
                WHEN t.total_seconds IS NULL THEN GREATEST(EXTRACT(EPOCH FROM (r.expires_at - now()))::bigint, 0)
                WHEN r.is_active THEN GREATEST(LEAST(t.total_seconds, EXTRACT(EPOCH FROM (r.expires_at - now()))::bigint), 0)
                ELSE t.total_seconds
            END,
            activated_at = CASE WHEN r.is_active THEN now() ELSE NULL END
        FROM (
            SELECT r2.id,
                   CASE
                       WHEN v.duration_type = 'bulanan' THEN 30 * 86400
                       WHEN v.duration_type = 'semester' THEN 180 * 86400
                       WHEN v.duration_type = 'tahunan' THEN 365 * 86400
                       WHEN v.duration_type ~ '^[0-9]+$' THEN (v.duration_type::bigint) * 86400
                       ELSE 30 * 86400
                   END AS total_seconds
            FROM voucher_redemptions r2
            JOIN vouchers v ON r2.voucher_id = v.id
            WHERE r2.expires_at IS NOT NULL
        ) t
        WHERE r.id = t.id;

        -- The legacy column is fully converted; drop it.
    END IF;

    -- Fresh installs created the legacy column a few lines up but have no rows
    -- to convert; drop it unconditionally so it never lingers.
    EXECUTE 'ALTER TABLE voucher_redemptions DROP COLUMN IF EXISTS expires_at';
END $$;

-- Custom voucher entitlement (SuperAdmin-defined limits/role, independent of
-- the fixed packages). When is_custom = true, redemption applies these values
-- instead of packageEntitlement(). Safe to re-run.
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS is_custom BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_label TEXT DEFAULT '';
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_max_exams INT DEFAULT 0;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_max_concurrent_exams INT DEFAULT 0;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_max_pdf_size BIGINT DEFAULT 0;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_max_storage_size BIGINT DEFAULT 0;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_max_users BIGINT DEFAULT 0;
ALTER TABLE vouchers ADD COLUMN IF NOT EXISTS custom_role TEXT DEFAULT '';

-- ============================================================
-- Migration: remove unused legacy draft quotas
-- ============================================================
-- max_drafts / max_draft_size (and their voucher counterparts) were never
-- enforced anywhere in the codebase; they existed only as a leftover from
-- before the dedicated max_concurrent_exams quota. Drop them entirely.
ALTER TABLE admin_users DROP COLUMN IF EXISTS max_drafts;
ALTER TABLE admin_users DROP COLUMN IF EXISTS max_draft_size;
ALTER TABLE vouchers DROP COLUMN IF EXISTS custom_max_drafts;
ALTER TABLE vouchers DROP COLUMN IF EXISTS custom_max_draft_size;

CREATE INDEX IF NOT EXISTS idx_vouchers_code ON vouchers(code);

-- Index — vouchers.created_by (FK ke admin_users, ON DELETE SET NULL).
-- Dipakai oleh JOIN daftar voucher (`LEFT JOIN admin_users u ON v.created_by =
-- u.id`, models/voucher.go) untuk menampilkan username pembuat voucher.
-- Safe to re-run on every boot.
CREATE INDEX IF NOT EXISTS idx_vouchers_created_by ON vouchers(created_by);

-- ============================================================
-- Migration: Unique Code per Instansi
-- ============================================================
ALTER TABLE instansi DROP CONSTRAINT IF EXISTS instansi_name_key;
ALTER TABLE instansi ADD COLUMN IF NOT EXISTS code TEXT UNIQUE;
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS instansi_code TEXT;

-- Auto-assign instansi code to any instansi lacking a code
UPDATE instansi
SET code = 'SCH-' || UPPER(SUBSTRING(MD5(id::text || name || RANDOM()::text), 1, 4)) || '-' || UPPER(SUBSTRING(MD5(id::text || RANDOM()::text), 1, 4))
WHERE code IS NULL OR code = '';

-- Sync admin_users instansi_code from instansi table
UPDATE admin_users u
SET instansi_code = i.code
FROM instansi i
WHERE u.instansi_id = i.id AND (u.instansi_code IS NULL OR u.instansi_code = '');

-- ============================================================
-- Package quotas configuration (SuperAdmin-editable)
-- ============================================================
-- Default quotas for the fixed packages, editable by SuperAdmin on
-- /admin/packages. NEW voucher claims snapshot these values at redeem time;
-- existing redemptions keep their own snapshot. The seed mirrors
-- packageEntitlement() in Go and the one-time redemption backfill. Sizes are
-- in bytes; role is a JSON array of roles (serialized). Safe to re-run: any
-- edited row is preserved via ON CONFLICT DO NOTHING.
CREATE TABLE IF NOT EXISTS package_settings (
    pkg_key              TEXT PRIMARY KEY,
    label                TEXT        NOT NULL DEFAULT '',
    max_exams            BIGINT      NOT NULL DEFAULT 1,
    max_pdf_size         BIGINT      NOT NULL DEFAULT 1048576,
    max_concurrent_exams BIGINT      NOT NULL DEFAULT 1,
    max_storage_size     BIGINT      NOT NULL DEFAULT 52428800,
    max_users            BIGINT      NOT NULL DEFAULT 0,
    role                 TEXT        NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Sub-account quota for existing installs: the column is added with default 0
-- ("unlimited"). On the first boot after this migration every row still holds
-- the fresh 0, so a single backfill plants the school-package defaults; later
-- SuperAdmin edits (including an intentional 0 = unlimited) are preserved
-- because the guard below only fires while no row has a non-zero value.
ALTER TABLE package_settings ADD COLUMN IF NOT EXISTS max_users BIGINT NOT NULL DEFAULT 0;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM package_settings WHERE max_users <> 0) THEN
        UPDATE package_settings SET max_users = CASE pkg_key
            WHEN 'sekolah_kecil'    THEN 10
            WHEN 'sekolah_menengah' THEN 25
            WHEN 'sekolah_besar'    THEN 50
            ELSE 0 END;
    END IF;
END $$;

INSERT INTO package_settings
    (pkg_key, label, max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
VALUES
    ('free',             'Free / Trial',           1, (1::bigint)*1024*1024, 1, (50::bigint)*1024*1024, 0, ''),
    ('guru',             'Paket Guru',             1, (10::bigint)*1024*1024, 1, (100::bigint)*1024*1024, 0, ''),
    ('individu',         'Paket Individu',         2, (30::bigint)*1024*1024, 2, (300::bigint)*1024*1024, 0, ''),
    ('sekolah_kecil',    'Paket Sekolah Kecil',    3, (50::bigint)*1024*1024, 3, (500::bigint)*1024*1024, 10, '["operator"]'),
    ('sekolah_menengah', 'Paket Sekolah Menengah', 5, (200::bigint)*1024*1024, 5, (2000::bigint)*1024*1024, 25, '["operator"]'),
    ('sekolah_besar',    'Paket Sekolah Besar',    10, (500::bigint)*1024*1024, 10, (5000::bigint)*1024*1024, 50, '["operator"]'),
    ('sekolah_unggulan', 'Paket Sekolah Unggulan', 99999, (99999::bigint)*1024*1024, 99999, (999999::bigint)*1024*1024, 0, '["operator"]')
ON CONFLICT (pkg_key) DO NOTHING;

-- Backfill the sub-account quota into snapshots claimed before max_users
-- existed: paused/fresh rows fall back to the package_settings default for
-- the package label they carry (custom labels simply stay 0 = unlimited).
-- Must run AFTER the package_settings table is created and seeded above, so a
-- fresh install (and every boot — the UPDATE is idempotent) can re-run it.
UPDATE voucher_redemptions r
SET max_users = ps.max_users
FROM package_settings ps
WHERE r.max_users = 0 AND r.package = ps.pkg_key;

-- ============================================================
-- Migration: exam tombstone marker (tombstoned_at)
-- ============================================================
-- Set when the school's operator is cut off (voucher switch or manual
-- suspension) and the exam's active-but-unstarted status is auto-inactivated
-- (policy B tombstone); cleared when the exam is (re)activated. Lets the
-- admin UI tell an auto-tombstoned exam apart from a manually inactivated
-- one. Safe to re-run on every boot.
ALTER TABLE exams ADD COLUMN IF NOT EXISTS tombstoned_at TIMESTAMPTZ;

-- ============================================================
-- Migration: suspension freeze (suspended_at)
-- ============================================================
-- Records when an account was suspended so reactivation can extend
-- admin_users.expires_at by the suspension duration — the package clock must
-- not keep burning while the user is locked out. Safe to re-run.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;

-- ============================================================
-- Migration: package-granted roles tracked separately
-- ============================================================
-- Roles granted by the currently-ACTIVE package are stored in admin_users.
-- package_role so they can be removed again when the user switches to a
-- package that does not grant them: a sekolah package's operator role must not
-- linger after the user activates a guru voucher. Roles from other sources
-- (admin-granted operator, delegated pengawas, the base guru role) live in
-- base_role — the roles the user holds independently of packages — so an
-- admin-granted operator survives a package switch even when it coincides
-- with what the package grants. A SuperAdmin's role is never modified.
-- Safe to re-run on every boot.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS package_role TEXT NOT NULL DEFAULT '';
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS base_role TEXT NOT NULL DEFAULT '';

-- IP address the account was registered from, used to enforce a per-IP
-- registration cap (anti mass-registration defense-in-depth, layered on top
-- of Cloudflare Turnstile). Empty for legacy/imported rows.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS registered_ip TEXT NOT NULL DEFAULT '';

-- ============================================================
-- Migration: sub-account voucher policy (operator_created)
-- ============================================================
-- Marks accounts CREATED BY an operator (school sub-accounts). The CreateUser
-- handler sets the flag when the caller is an operator; it is never modified
-- afterwards (origin-based, not current-role-based). RedeemVoucherHandler and
-- ActivateVoucherHandler reject these accounts with 403, so a sub-account can
-- never claim or activate a voucher — its package/quota/expiry are managed by
-- the school package the operator holds. Legacy/imported rows stay false.
-- Safe to re-run on every boot.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS operator_created BOOLEAN NOT NULL DEFAULT FALSE;

-- ============================================================
-- Migration: sub-account creator attribution (created_by)
-- ============================================================
-- Precise per-operator attribution for the shared "personal" bucket: the
-- operator_created flag alone cannot tell WHICH operator created a
-- sub-account, so quota counting and the UpdateInstansi migration had to treat
-- every personal-bucket sub-account as one shared pool (the documented
-- approximation). created_by = admin_users.id of the creating operator makes
-- both precise: a personal-bucket operator's quota counts only its OWN
-- sub-accounts, and the school-instansi migration moves only its own. NULL
-- for accounts created before this column existed — their creator is
-- unknowable, so they keep the conservative shared-bucket fallback (counted
-- against every personal operator, migrated by none). Safe to re-run on every
-- boot.
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES admin_users(id) ON DELETE SET NULL;

-- Speed up the per-operator sub-account lookups on the shared "personal"
-- bucket: loadOperatorAccountQuota counts `created_by = <operator id>` for
-- every sub-account quota check (CreateUser enforcement + billing page), and
-- UpdateInstansi migrates `created_by = <operator id>` sub-accounts when a
-- school instansi is claimed. Both become index scans instead of full-table
-- scans once an operator's sub-account list grows large. (The school-instansi
-- count filters on instansi instead, which is a different access path; the
-- personal bucket is the one where created_by is the selective predicate.)
-- Safe to re-run on every boot.
CREATE INDEX IF NOT EXISTS idx_admin_users_created_by ON admin_users(created_by);

-- Keep package_role in sync with the active redemption's snapshot role on
-- every boot. This also migrates rows created under the old accumulate-forever
-- model: the active package's granted roles become tracked and removable the
-- next time the user switches packages.
UPDATE admin_users u
SET package_role = COALESCE(r.role, '')
FROM voucher_redemptions r
WHERE r.user_id = u.id AND r.is_active;

-- ============================================================
-- Migration: unique email (enforce no duplicate accounts per address)
-- ============================================================
-- admin_users.email did not previously have a uniqueness guarantee. Legacy
-- rows may hold duplicates (e.g. created through non-registration paths).
-- Before adding the partial unique index we collapse case-variant / exact
-- duplicates to their lowest-id row (the original account), blanking the
-- email on the later duplicates so the index creation cannot fail and the
-- duplicate account is no longer matched by GetUserByEmail. Only rows with a
-- non-empty email and a lower-id twin are affected — genuinely-unique and
-- empty-email rows are untouched. Safe to re-run on every boot.
DO $$
BEGIN
    UPDATE admin_users d
    SET email = ''
    FROM (
        SELECT LOWER(email) AS le, MIN(id) AS keep_id
        FROM admin_users
        WHERE email <> ''
        GROUP BY LOWER(email)
        HAVING COUNT(*) > 1
    ) dup
    WHERE d.id <> dup.keep_id
      AND LOWER(d.email) = dup.le
      AND d.email <> '';
END $$;

-- Email is unique only when non-empty, so accounts that never provide an
-- email (legacy rows DEFAULT '') do not collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS uq_admin_users_email
    ON admin_users (LOWER(email)) WHERE email <> '';

