"""
EXAMVAN Server - REST API & Admin Panel
Version: 2.2.0
Platform: Flask + SQLite
"""

import os
import secrets
import string
import sqlite3
import hashlib
import socket
import json
import csv
import io
import threading
import urllib.request
import urllib.parse
from datetime import datetime, timezone, timedelta
from functools import wraps

from flask import (
    Flask, request, jsonify, render_template,
    redirect, url_for, session, send_file, flash, abort, g
)
from werkzeug.utils import secure_filename
from werkzeug.security import generate_password_hash, check_password_hash
from helpers import (
    localize_date_string, format_iso_utc, get_local_ip,
    _normalize_q_num, _evaluate_single_question,
    evaluate_answers_detailed, calculate_submission_score
)

import logging
from logging.handlers import RotatingFileHandler

from dotenv import load_dotenv

# ===== Configuration =====
BASE_DIR = os.path.dirname(os.path.abspath(__file__))

# Muat .env dari root project (parent directory) sebelum env vars dibaca
dotenv_path = os.path.join(BASE_DIR, '..', '.env')
if os.path.isfile(dotenv_path):
    load_dotenv(dotenv_path)
else:
    # Logger belum siap — pakai print sebagai fallback
    import sys
    print(f"⚠️  .env tidak ditemukan di {os.path.abspath(dotenv_path)} — fallback ke env vars sistem", file=sys.stderr)

STORAGE_DIR = os.path.join(BASE_DIR, 'storage')
_db_env = os.environ.get('DATABASE_PATH', '')
if _db_env and not os.path.isabs(_db_env):
    DATABASE = os.path.join(BASE_DIR, _db_env)
elif _db_env:
    DATABASE = _db_env
else:
    DATABASE = os.path.join(BASE_DIR, 'data', 'examvan.db')
MAX_FILE_SIZE = 5 * 1024 * 1024  # 5MB

VERSION = '2.2.1'
_css_hash = None  # populated lazily by inject_csrf_token for cache busting
ADMIN_USERNAME = os.environ.get('EXAMVAN_ADMIN_USER', 'superadmin')
# ADMIN_PASSWORD must be set via env var; if missing, a random password is generated at init
ADMIN_PASSWORD = os.environ.get('EXAMVAN_ADMIN_PASS', '')
DEFAULT_IDENTITY_FIELDS = json.dumps([
    {'key': 'student_name', 'label': 'Nama', 'required': True},
    {'key': 'exam_number', 'label': 'Nomor Ujian', 'required': True},
    {'key': 'student_class', 'label': 'Kelas', 'required': True},
])

os.makedirs(STORAGE_DIR, exist_ok=True)
os.makedirs(os.path.dirname(DATABASE), exist_ok=True)

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(name)s: %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
    handlers=[
        logging.StreamHandler(),
        RotatingFileHandler(os.path.join(BASE_DIR, 'data', 'examvan.log'), maxBytes=5*1024*1024, backupCount=3)
    ]
)
logger = logging.getLogger('examvan')


# ===== App Init =====
app = Flask(__name__)
app.secret_key = os.environ.get('EXAMVAN_SECRET', secrets.token_hex(32))
# Validate secret key: reject known placeholder
if app.secret_key == 'change_this_to_a_random_secret_key_min_32_chars':
    logger.critical("EXAMVAN_SECRET is set to the KNOWN PLACEHOLDER value! Session forgery risk. Set a unique secret in .env and restart immediately.")
    import sys
    sys.exit(1)
app.config['MAX_CONTENT_LENGTH'] = MAX_FILE_SIZE + 4096

# ===== Session Security =====
app.config['SESSION_COOKIE_SECURE'] = False  # default False; overridden by X-Forwarded-Proto in after_request
app.config['SESSION_COOKIE_HTTPONLY'] = True
app.config['SESSION_COOKIE_SAMESITE'] = 'Lax'
app.config['SESSION_COOKIE_NAME'] = 'examvan_session'
# Session cookie lifetime: 24 jam
app.config['PERMANENT_SESSION_LIFETIME'] = timedelta(hours=24)


# ===== Database =====
def get_db():
    """Get database connection with Row factory, request-scoped via g."""
    if 'db' not in g:
        g.db = sqlite3.connect(DATABASE)
        g.db.execute('PRAGMA foreign_keys = ON')
        g.db.execute('PRAGMA journal_mode=WAL')
        g.db.row_factory = sqlite3.Row
    return g.db


@app.teardown_appcontext
def close_db(exception):
    db = g.pop('db', None)
    if db is not None:
        db.close()


def get_db_standalone():
    """Get a standalone DB connection (outside request context, e.g. init_db)."""
    db = sqlite3.connect(DATABASE)
    db.execute('PRAGMA foreign_keys = ON')
    db.execute('PRAGMA journal_mode=WAL')
    db.row_factory = sqlite3.Row
    return db


def init_db():
    """Initialize database tables and default admin user."""
    db = get_db_standalone()
    schema_sql = '''
        CREATE TABLE IF NOT EXISTS exams (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            file_path TEXT NOT NULL,
            size_bytes INTEGER NOT NULL,
            token TEXT UNIQUE NOT NULL,
            questions_json TEXT,
            status TEXT DEFAULT 'active' CHECK(status IN ('active', 'inactive')),
            security_level TEXT DEFAULT 'medium' CHECK(security_level IN ('medium', 'low', 'high')),
            strict_mode INTEGER DEFAULT 0,
            public_results INTEGER DEFAULT 1,
            show_answers INTEGER DEFAULT 0,
            created_by INTEGER DEFAULT 1,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS admin_users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            status TEXT DEFAULT 'active',
            instansi TEXT DEFAULT 'personal',
            role TEXT DEFAULT 'guru',
            max_exams INTEGER DEFAULT 3,
            max_pdf_size INTEGER DEFAULT 1048576,
            max_drafts INTEGER DEFAULT 2,
            max_draft_size INTEGER DEFAULT 1048576,
            whatsapp_number TEXT,
            expires_at TIMESTAMP,
            otp_code TEXT,
            otp_expiry TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS submissions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            exam_id INTEGER NOT NULL,
            student_name TEXT NOT NULL,
            exam_number TEXT NOT NULL,
            student_class TEXT NOT NULL,
            answers_json TEXT NOT NULL,
            score REAL,
            start_time TIMESTAMP,
            mac_address TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(exam_id) REFERENCES exams(id) ON DELETE CASCADE
        );

        CREATE TABLE IF NOT EXISTS saas_settings (
            key TEXT PRIMARY KEY,
            value TEXT
        );

        CREATE TABLE IF NOT EXISTS rate_limits (
            key TEXT PRIMARY KEY,
            timestamps TEXT NOT NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_rate_limits_key ON rate_limits(key);

        CREATE TABLE IF NOT EXISTS exam_pengawas (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            exam_id INTEGER NOT NULL,
            user_id INTEGER NOT NULL,
            UNIQUE(exam_id, user_id),
            FOREIGN KEY(exam_id) REFERENCES exams(id) ON DELETE CASCADE,
            FOREIGN KEY(user_id) REFERENCES admin_users(id) ON DELETE CASCADE
        );

        CREATE TABLE IF NOT EXISTS student_access_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            exam_id INTEGER NOT NULL,
            submission_id INTEGER,
            student_identifier TEXT NOT NULL,
            student_name TEXT,
            exam_number TEXT,
            student_class TEXT,
            event TEXT NOT NULL CHECK(event IN ('login','heartbeat','logout')),
            ip_address TEXT,
            device_info TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(exam_id) REFERENCES exams(id) ON DELETE CASCADE,
            FOREIGN KEY(submission_id) REFERENCES submissions(id) ON DELETE SET NULL
        );

        CREATE INDEX IF NOT EXISTS idx_access_logs_exam ON student_access_logs(exam_id);
        CREATE INDEX IF NOT EXISTS idx_access_logs_identifier ON student_access_logs(student_identifier);
        CREATE INDEX IF NOT EXISTS idx_access_logs_exam_identifier ON student_access_logs(exam_id, student_identifier);
        CREATE INDEX IF NOT EXISTS idx_exam_pengawas_exam ON exam_pengawas(exam_id);
        CREATE INDEX IF NOT EXISTS idx_exam_pengawas_user ON exam_pengawas(user_id);
    '''
    try:
        db.executescript(schema_sql)
    except sqlite3.OperationalError as e:
        logger.error(f"Schema creation failed: {e}")
        raise

    # Seed default SaaS settings if not present
    default_settings = {
        'wa_verification_enabled': '0',
        'wa_api_token': '',
        'wa_otp_template': 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.',
        'default_max_exams': '3',
        'default_max_pdf_size': '1048576',
        'default_active_days': '1',
        'default_max_drafts': '2',
        'default_max_draft_size': '1048576',
        'android_version': '2.2.0',
        'webapp_version': '2.2.0',
        'certificate_fingerprint': ''
    }
    for k, v in default_settings.items():
        existing_setting = db.execute('SELECT value FROM saas_settings WHERE key = ?', (k,)).fetchone()
        if not existing_setting:
            db.execute('INSERT INTO saas_settings (key, value) VALUES (?, ?)', (k, v))
        elif k in ('android_version', 'webapp_version') and existing_setting['value'] in ('2.1.0', '2.1.1', '2.1.2', '2.1.3', '2.1.4', '2.1.5', '2.1.6', '2.1.7', '2.1.8', '2.1.9'):
            db.execute('UPDATE saas_settings SET value = ? WHERE key = ?', (v, k))
    db.commit()

    admin_username = os.environ.get('EXAMVAN_ADMIN_USER', 'superadmin')
    admin_password = os.environ.get('EXAMVAN_ADMIN_PASS', '')

    existing = db.execute(
        'SELECT id FROM admin_users WHERE username = ?',
        (admin_username,)
    ).fetchone()

    if not existing:
        if not admin_password:
            # Generate a secure random password if no env var is set
            admin_password = secrets.token_hex(16)
            logger.warning(
                f"No EXAMVAN_ADMIN_PASS env var set. "
                f"Random password generated for '{admin_username}'. "
                f"Set EXAMVAN_ADMIN_PASS in environment and restart."
            )
        pw_hash = generate_password_hash(admin_password)
        db.execute(
            'INSERT INTO admin_users (username, password_hash, role, instansi) VALUES (?, ?, ?, ?)',
            (admin_username, pw_hash, 'superadmin', 'owner')
        )
        db.commit()
        logger.info(f"Default admin user '{admin_username}' created")
    elif admin_password:
        # Sync password from env var if it changed
        current_hash = db.execute(
            'SELECT password_hash FROM admin_users WHERE id = ?',
            (existing['id'],)
        ).fetchone()['password_hash']
        if not check_password_hash(current_hash, admin_password):
            new_hash = generate_password_hash(admin_password)
            db.execute(
                'UPDATE admin_users SET password_hash = ? WHERE id = ?',
                (new_hash, existing['id'])
            )
            db.commit()
            logger.info(f"Admin password updated from env var for '{admin_username}'")

    # ===== Migration tracking =====
    db.execute('''CREATE TABLE IF NOT EXISTS _migrations (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT UNIQUE NOT NULL,
        applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )''')
    db.commit()

    applied = {row['name'] for row in db.execute('SELECT name FROM _migrations').fetchall()}

    migrations = [
        ('add_token_to_exams', 'ALTER TABLE exams ADD COLUMN token TEXT'),
        ('add_questions_json_to_exams', 'ALTER TABLE exams ADD COLUMN questions_json TEXT'),
        ('add_created_by_to_exams', 'ALTER TABLE exams ADD COLUMN created_by INTEGER DEFAULT 1'),
        ('add_security_level_to_exams', "ALTER TABLE exams ADD COLUMN security_level TEXT DEFAULT 'medium'"),
        ('add_start_time_to_submissions', 'ALTER TABLE submissions ADD COLUMN start_time TIMESTAMP'),
        ('add_mac_address_to_submissions', 'ALTER TABLE submissions ADD COLUMN mac_address TEXT'),
        ('add_public_results_to_exams', 'ALTER TABLE exams ADD COLUMN public_results INTEGER DEFAULT 1'),
        ('add_show_answers_to_exams', 'ALTER TABLE exams ADD COLUMN show_answers INTEGER DEFAULT 0'),
        ('add_max_exams_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN max_exams INTEGER DEFAULT 3'),
        ('add_max_pdf_size_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN max_pdf_size INTEGER DEFAULT 1048576'),
        ('add_max_drafts_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN max_drafts INTEGER DEFAULT 2'),
        ('add_max_draft_size_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN max_draft_size INTEGER DEFAULT 1048576'),
        ('add_whatsapp_number_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN whatsapp_number TEXT'),
        ('add_status_to_admin_users', "ALTER TABLE admin_users ADD COLUMN status TEXT DEFAULT 'active'"),
        ('add_otp_code_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN otp_code TEXT'),
        ('add_expires_at_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN expires_at TIMESTAMP'),
        ('add_otp_expiry_to_admin_users', 'ALTER TABLE admin_users ADD COLUMN otp_expiry TIMESTAMP'),
        ('add_identity_fields_to_exams', 'ALTER TABLE exams ADD COLUMN identity_fields TEXT'),
        ('add_identity_data_to_submissions', 'ALTER TABLE submissions ADD COLUMN identity_data TEXT'),
        ('add_panel_color_to_exams', 'ALTER TABLE exams ADD COLUMN panel_color TEXT'),
        ('add_start_time_to_exams', 'ALTER TABLE exams ADD COLUMN start_time TEXT'),
        ('add_end_time_to_exams', 'ALTER TABLE exams ADD COLUMN end_time TEXT'),
        ('add_instansi_to_admin_users', "ALTER TABLE admin_users ADD COLUMN instansi TEXT DEFAULT ''"),
        ('add_role_to_admin_users', "ALTER TABLE admin_users ADD COLUMN role TEXT DEFAULT 'guru'"),
        ('set_superadmin_role', "UPDATE admin_users SET role = 'superadmin' WHERE role = 'guru' AND (username = 'superadmin' OR username = 'admin')"),
        ('set_instansi_default_personal', f"UPDATE admin_users SET instansi = 'owner' WHERE (instansi IS NULL OR instansi = '') AND (username = '{admin_username}' OR username = 'admin')"),
        ('set_instansi_default_others', "UPDATE admin_users SET instansi = 'personal' WHERE instansi IS NULL OR instansi = ''"),
        ('convert_role_to_json',
         f"UPDATE admin_users SET role = '[\"guru\"]' WHERE role = 'guru' AND username != '{admin_username}' AND username != 'admin'"),
        ('convert_role_to_json_pengawas',
         "UPDATE admin_users SET role = '[\"pengawas\"]' WHERE role = 'pengawas'"),
        ('add_delegated_to_to_exams', 'ALTER TABLE exams ADD COLUMN delegated_to INTEGER REFERENCES admin_users(id)'),
        # strict_mode is defined in CREATE TABLE, no ALTER needed
    ]

    for name, sql in migrations:
        if name in applied:
            continue
        try:
            db.execute(sql)
            if name == 'add_token_to_exams':
                # Generate tokens for existing rows from older databases
                chars = string.ascii_uppercase + string.digits
                rows = db.execute('SELECT id FROM exams WHERE token IS NULL').fetchall()
                for row in rows:
                    token = ''.join(secrets.choice(chars) for _ in range(6))
                    db.execute('UPDATE exams SET token = ? WHERE id = ?',
                               (token, row['id']))
            db.execute('INSERT INTO _migrations (name) VALUES (?)', (name,))
            db.commit()
            logger.info(f"Migration '{name}' applied")
        except sqlite3.OperationalError:
            # Column likely already exists; record migration as done
            try:
                db.execute('INSERT INTO _migrations (name) VALUES (?)', (name,))
            except sqlite3.IntegrityError:
                pass
            db.commit()

    # ===== Ensure admin user has correct role =====
    # Safety check: always ensure the admin user has role='superadmin'
    # This runs on every startup to handle edge cases:
    # - Old DB where admin was created before role column existed
    # - Partially applied migrations
    # - Username rename from 'admin' to 'superadmin'
    try:
        # 1. Try to find admin by configured username
        admin_row = db.execute(
            "SELECT id, role, username FROM admin_users WHERE username = ?",
            (admin_username,)
        ).fetchone()

        # 2. If not found, try the old 'admin' username and rename it
        if not admin_row and admin_username != 'admin':
            old_admin = db.execute(
                "SELECT id, role, username FROM admin_users WHERE username = 'admin'"
            ).fetchone()
            if old_admin:
                db.execute(
                    "UPDATE admin_users SET username = ?, role = 'superadmin' WHERE id = ?",
                    (admin_username, old_admin['id'])
                )
                db.commit()
                logger.info(f"Renamed admin user 'admin' → '{admin_username}', role → 'superadmin'")
                admin_row = db.execute(
                    "SELECT id, role, username FROM admin_users WHERE id = ?",
                    (old_admin['id'],)
                ).fetchone()

        # 3. Ensure role is 'superadmin'
        if admin_row and admin_row['role'] != 'superadmin':
            db.execute(
                "UPDATE admin_users SET role = 'superadmin' WHERE id = ?",
                (admin_row['id'],)
            )
            db.commit()
            logger.info(f"Admin user '{admin_row['username']}' role → 'superadmin'")

        # 4. Also fix any remaining 'admin' username with wrong role
        if admin_username != 'admin':
            db.execute(
                "UPDATE admin_users SET role = 'superadmin' WHERE username = 'admin' AND role != 'superadmin'"
            )
            db.commit()

        logger.info(f"Admin user check complete: '{admin_username}' is ready")

        # ===== Ensure all users have instansi set =====
        try:
            cur = db.execute(
                "UPDATE admin_users SET instansi = 'personal' WHERE instansi IS NULL OR instansi = ''"
            )
            if cur.rowcount:
                logger.info(f"Set instansi='personal' for {cur.rowcount} user(s)")
        except Exception as e:
            logger.error(f"Instansi default check failed: {e}")
    except Exception as e:
        logger.error(f"Admin role check failed: {e}")

    db.close()

# Lazy init: skip at import time for Gunicorn worker safety
# Will init on first request via a before_request handler
_init_done = False

@app.before_request
def _lazy_init():
    global _init_done
    if not _init_done:
        try:
            init_db()
            _init_done = True
        except Exception as e:
            logger.error(f"Error initializing database on first request: {e}")


# ===== Rate Limiter (SQLite-backed) =====
import time
import json

def check_rate_limit(key, max_attempts=5, window_seconds=300):
    """SQLite-backed rate limiter (works across Gunicorn workers)."""
    now = time.time()
    ip = request.access_route[0] if request.access_route else request.remote_addr or 'unknown'
    store_key = f"{key}:{ip}"

    db = get_db()
    row = db.execute('SELECT timestamps FROM rate_limits WHERE key = ?', (store_key,)).fetchone()
    timestamps = json.loads(row['timestamps']) if row else []

    # Clean old entries
    timestamps = [t for t in timestamps if now - t < window_seconds]

    if len(timestamps) >= max_attempts:
        db.execute('INSERT OR REPLACE INTO rate_limits (key, timestamps) VALUES (?, ?)',
                   (store_key, json.dumps(timestamps)))
        db.commit()
        return False

    timestamps.append(now)
    db.execute('INSERT OR REPLACE INTO rate_limits (key, timestamps) VALUES (?, ?)',
               (store_key, json.dumps(timestamps)))
    db.commit()
    return True


# ===== CSRF Protection =====
def generate_csrf_token():
    """Generate or retrieve CSRF token from session."""
    if 'csrf_token' not in session:
        session['csrf_token'] = secrets.token_hex(32)
    return session['csrf_token']


def _validate_csrf():
    """Validate CSRF token for state-changing requests. Returns None or (status, response)."""
    if request.method in ('POST', 'PUT', 'DELETE'):
        token = request.headers.get('X-CSRF-Token') or request.form.get('csrf_token')
        expected = session.get('csrf_token')
        if not expected or not token or token != expected:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'invalid_csrf', 'message': 'CSRF token tidak valid. Silakan refresh halaman.'}), 403
            flash('CSRF token tidak valid. Silakan coba lagi.', 'error')
            return redirect(url_for('admin_dashboard'))
    return None

def csrf_required(f):
    """Decorator to require valid CSRF token on state-changing requests."""
    @wraps(f)
    def decorated(*args, **kwargs):
        result = _validate_csrf()
        if result:
            return result
        return f(*args, **kwargs)
    return decorated


@app.context_processor
def inject_csrf_token():
    """Inject CSRF token and version into all templates."""
    global _css_hash
    if _css_hash is None:
        try:
            css_path = os.path.join(BASE_DIR, 'static', 'css', 'tailwind', 'output.css')
            if os.path.exists(css_path):
                mtime = int(os.path.getmtime(css_path))
                _css_hash = hex(mtime)[2:8]
            else:
                _css_hash = ''
        except Exception:
            _css_hash = ''
    return {'csrf_token': generate_csrf_token(), 'version': VERSION + _css_hash}


# ===== Helpers =====
def get_saas_setting(key, default=''):
    db = get_db()
    row = db.execute('SELECT value FROM saas_settings WHERE key = ?', (key,)).fetchone()
    return row['value'] if row else default

def set_saas_setting(key, value):
    db = get_db()
    db.execute('INSERT OR REPLACE INTO saas_settings (key, value) VALUES (?, ?)', (key, str(value)))
    db.commit()

def send_whatsapp(target, message):
    token = get_saas_setting('wa_api_token', '')
    if not token:
        logger.warning(f"WhatsApp Token not configured. Would send to {target}: {len(message)} chars")
        return False
        
    url = "https://api.fonnte.com/send"
    
    clean_target = ''.join(c for c in target if c.isdigit())
    if clean_target.startswith('0'):
        clean_target = '62' + clean_target[1:]
        
    data = urllib.parse.urlencode({
        'target': clean_target,
        'message': message,
        'countryCode': '62'
    }).encode('utf-8')
    
    req = urllib.request.Request(url, data=data)
    req.add_header('Authorization', token)
    
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            res_data = response.read().decode('utf-8')
            res_json = json.loads(res_data)
            logger.info(f"Fonnte Send WA Response: {res_json}")
            return res_json.get('status', False)
    except Exception as e:
        logger.error(f"Fonnte Send WA Exception: {e}")
        return False

def _verify_password(password, stored_hash):
    """Verify password against stored hash. Only supports werkzeug scrypt/pbkdf2 (SHA-256 legacy removed)."""
    if not stored_hash:
        return False
    if stored_hash.startswith(('scrypt:', 'pbkdf2:')):
        return check_password_hash(stored_hash, password)
    # Legacy SHA-256 hash: tidak lagi didukung. User harus reset password.
    return False

def generate_token(length=6, db=None):
    """Generate a unique uppercase alphanumeric token with collision protection."""
    chars = string.ascii_uppercase + string.digits
    should_close = False
    if db is None:
        db = get_db_standalone()
        should_close = True
    try:
        max_attempts = 100
        for _ in range(max_attempts):
            token = ''.join(secrets.choice(chars) for _ in range(length))
            existing = db.execute('SELECT id FROM exams WHERE token = ?', (token,)).fetchone()
            if not existing:
                return token
        # Last resort: increase length to avoid collision
        token = ''.join(secrets.choice(chars) for _ in range(length + 2))
        return token
    finally:
        if should_close:
            db.close()

def admin_required(f):
    """Decorator to require admin login + CSRF check for state-changing methods."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if 'admin_id' not in session:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'unauthorized', 'message': 'Silakan login terlebih dahulu'}), 401
            return redirect(url_for('admin_login'))
        # CSRF check via shared helper
        result = _validate_csrf()
        if result:
            return result
        # Check account expiry from session cache (set at login)
        if not session.get('is_super_admin') and not session.get('is_operator'):
            expires_at = session.get('expires_at')
            if expires_at:
                expires_dt = datetime.strptime(expires_at, '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
                if datetime.now(timezone.utc) > expires_dt:
                    session.clear()
                    if request.is_json or request.path.startswith('/admin/api'):
                        return jsonify({'success': False, 'error': 'expired', 'message': 'Masa aktif akun Anda telah habis'}), 403
                    flash('Masa aktif akun Anda telah habis. Silakan hubungi administrator.', 'error')
                    return redirect(url_for('admin_login'))
        return f(*args, **kwargs)
    return decorated


def super_admin_required(f):
    """Decorator to require super admin (username: admin) login."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if 'admin_id' not in session:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'unauthorized', 'message': 'Silakan login terlebih dahulu'}), 401
            return redirect(url_for('admin_login'))
        if not session.get('is_super_admin'):
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'forbidden', 'message': 'Akses khusus Super Admin'}), 403
            return abort(403)
        return f(*args, **kwargs)
    return decorated


def admin_management_required(f):
    """Decorator for user management routes. Allows superadmin and operator."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if 'admin_id' not in session:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'unauthorized', 'message': 'Silakan login terlebih dahulu'}), 401
            return redirect(url_for('admin_login'))
        if not session.get('is_super_admin') and not session.get('is_operator'):
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'forbidden', 'message': 'Akses khusus Admin'}), 403
            return abort(403)
        return f(*args, **kwargs)
    return decorated


@app.context_processor
def inject_user_roles():
    """Inject user roles into all templates for nav visibility."""
    instansi = ''
    if 'admin_id' in session:
        try:
            row = get_db().execute(
                'SELECT instansi FROM admin_users WHERE id = ?',
                (session['admin_id'],)
            ).fetchone()
            if row and row['instansi']:
                instansi = row['instansi']
        except Exception:
            pass
    if session.get('is_super_admin'):
        return dict(user_roles=['superadmin'], is_guru=True, is_pengawas=True, is_operator=False, is_privileged=True, admin_instansi=instansi)
    if session.get('is_operator'):
        return dict(user_roles=['operator'], is_guru=True, is_pengawas=True, is_operator=True, is_privileged=True, admin_instansi=instansi)
    roles = session.get('user_roles', ['guru'])
    return dict(user_roles=roles, is_guru='guru' in roles, is_pengawas='pengawas' in roles, is_operator=False, is_privileged=False, admin_instansi=instansi)


def check_exam_ownership(db, exam_id):
    """Check if current user is allowed to manage the given exam."""
    if session.get('is_super_admin') or session.get('is_operator'):
        return True
    exam = db.execute('SELECT created_by FROM exams WHERE id = ?', (exam_id,)).fetchone()
    return exam is not None and exam['created_by'] == session['admin_id']


def check_submission_ownership(db, submission_id):
    """Check if current user is allowed to manage the given submission."""
    if session.get('is_super_admin') or session.get('is_operator'):
        return True
    sub = db.execute(
        'SELECT e.created_by FROM submissions s JOIN exams e ON s.exam_id = e.id WHERE s.id = ?',
        (submission_id,)
    ).fetchone()
    return sub is not None and sub['created_by'] == session['admin_id']


def safe_storage_path(file_path):
    """Validate and resolve a storage path, preventing directory traversal.

    Resolves symlinks and checks that the final resolved path is within STORAGE_DIR.
    """
    # Reject empty or None paths
    if not file_path:
        raise ValueError("Empty path")
    # Reject absolute paths passed directly
    if os.path.isabs(file_path):
        raise ValueError("Path traversal detected: absolute path not allowed")
    # Join with storage dir and normalize
    full_path = os.path.normpath(os.path.join(STORAGE_DIR, file_path))
    # Resolve symlinks for both paths (use realpath for STORAGE_DIR too)
    try:
        real_full = os.path.realpath(full_path)
        real_storage = os.path.realpath(STORAGE_DIR)
    except OSError:
        # If resolution fails, fall back to normpath check
        real_full = full_path
        real_storage = os.path.normpath(STORAGE_DIR)
    if not real_full.startswith(real_storage):
        raise ValueError("Path traversal detected")
    return real_full


def get_network_info():
    """Get dynamic network info (domain/IP, endpoint, protocol) based on request context."""
    # 1. Detect protocol (scheme)
    scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
    
    # 2. Detect host (domain or IP + port)
    host = request.headers.get('X-Forwarded-Host', request.host)
    
    # 3. Detect if using Cloudflare
    is_cloudflare = ('CF-Connecting-IP' in request.headers or 
                     'CF-Ray' in request.headers or 
                     'cf-visitor' in request.headers or
                     scheme == 'https')
    
    display_host = host
    
    # Fallback if accessed via localhost or internal docker hostname
    if display_host.startswith(('localhost', '127.0.0.1', 'examvan-server', '172.')):
        lan_ip = get_local_ip()
        if not lan_ip.startswith('172.'):
            display_host = f"{lan_ip}:5000"
            
    # Clean port if using Cloudflare or standard HTTPS
    if is_cloudflare or scheme == 'https':
        display_host = display_host.split(':')[0]
        scheme = 'https'
        
    api_endpoint = f"{scheme}://{display_host}/api/exams"
    
    return {
        'display_host': display_host,
        'api_endpoint': api_endpoint,
        'protocol': 'HTTPS (Cloudflare)' if is_cloudflare else 'HTTP (LAN)',
        'is_cloudflare': is_cloudflare
    }


def get_storage_stats():
    """Get total storage used by PDFs."""
    total = 0
    with os.scandir(STORAGE_DIR) as entries:
        for entry in entries:
            if entry.is_file():
                total += entry.stat().st_size
    return total


@app.after_request
def add_security_headers(response):
    # Dynamically set SESSION_COOKIE_SECURE based on connection
    if request.headers.get('X-Forwarded-Proto', request.scheme) == 'https':
        app.config['SESSION_COOKIE_SECURE'] = True

    response.headers['Content-Security-Policy'] = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'self'; form-action 'self'"
    response.headers['X-Content-Type-Options'] = 'nosniff'
    response.headers['X-Frame-Options'] = 'DENY'
    response.headers['X-XSS-Protection'] = '1; mode=block'
    response.headers['Access-Control-Allow-Origin'] = '*'
    response.headers['Access-Control-Allow-Methods'] = 'GET, POST, PUT, DELETE, OPTIONS'
    response.headers['Access-Control-Allow-Headers'] = 'Content-Type, X-CSRF-Token, X-App-Version, X-Exam-Token'
    # HSTS: only set when HTTPS is detected
    if request.headers.get('X-Forwarded-Proto', request.scheme) == 'https':
        response.headers['Strict-Transport-Security'] = 'max-age=31536000; includeSubDomains'
    return response


# ===== REST API Endpoints =====
# ===== Error Handlers =====

# Import routes after all helpers & decorators are defined so that
# routes.py can import from app.py without a circular-reference issue.
# Routes register themselves via @app.route(...) decorators.
import routes

@app.errorhandler(413)
def too_large(e):
    """Handle file too large error."""
    return jsonify({
        'success': False,
        'error': 'file_too_large',
        'message': f'File melebihi batas maksimal {MAX_FILE_SIZE // (1024*1024)}MB'
    }), 413


@app.errorhandler(404)
def not_found(e):
    """Handle 404 errors - render a proper page instead of redirecting to root."""
    if request.path.startswith('/api/'):
        return jsonify({
            'success': False,
            'error': 'not_found',
            'message': 'Endpoint tidak ditemukan'
        }), 404
    # Check if user is trying to access admin pages without login
    if request.path.startswith('/admin/'):
        return redirect(url_for('admin_login'))
    return render_template('index.html'), 404


@app.errorhandler(500)
def internal_error(e):
    """Handle unhandled exceptions with logging."""
    logger.error(f"INTERNAL SERVER ERROR: {e}")
    if request.path.startswith('/api/'):
        return jsonify({
            'success': False,
            'error': 'internal_error',
            'message': 'Terjadi kesalahan internal server. Silakan coba lagi.'
        }), 500
    return redirect(url_for('admin_dashboard'))


# ===== Main =====
if __name__ == '__main__':
    port = int(os.environ.get('PORT', 5000))
    app.run(host='0.0.0.0', port=port)
