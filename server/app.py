"""
EXAMVAN Server - REST API & Admin Panel
Version: 2.1.9
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

# ===== Configuration =====
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
STORAGE_DIR = os.path.join(BASE_DIR, 'storage')
DATABASE = os.environ.get('DATABASE_PATH', os.path.join(BASE_DIR, 'data', 'examvan.db'))
MAX_FILE_SIZE = 5 * 1024 * 1024  # 5MB

VERSION = '2.1.9'
ADMIN_USERNAME = os.environ.get('EXAMVAN_ADMIN_USER', 'admin')
# ADMIN_PASSWORD must be set via env var; if missing, a random password is generated at init
ADMIN_PASSWORD = os.environ.get('EXAMVAN_ADMIN_PASS', '')
DEFAULT_IDENTITY_FIELDS = json.dumps([
    {'key': 'student_name', 'label': 'Nama Siswa', 'required': True},
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
app.config['MAX_CONTENT_LENGTH'] = MAX_FILE_SIZE + 4096


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
    db.executescript('''
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
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
    ''')

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
        'android_version': '2.1.9',
        'webapp_version': '2.1.9',
        'certificate_fingerprint': ''
    }
    for k, v in default_settings.items():
        existing_setting = db.execute('SELECT value FROM saas_settings WHERE key = ?', (k,)).fetchone()
        if not existing_setting:
            db.execute('INSERT INTO saas_settings (key, value) VALUES (?, ?)', (k, v))
        elif k in ('android_version', 'webapp_version') and existing_setting['value'] in ('2.1.0', '2.1.1', '2.1.2', '2.1.3', '2.1.4', '2.1.5', '2.1.6', '2.1.7', '2.1.8'):
            db.execute('UPDATE saas_settings SET value = ? WHERE key = ?', (v, k))
    db.commit()

    admin_username = os.environ.get('EXAMVAN_ADMIN_USER', 'admin')
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
            'INSERT INTO admin_users (username, password_hash) VALUES (?, ?)',
            (admin_username, pw_hash)
        )
        db.commit()
        logger.info(f"Default admin user '{admin_username}' created")

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
        # strict_mode is defined in CREATE TABLE, no ALTER needed
        # ('add_strict_mode_to_exams', ...) — removed as duplicate
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

    db.close()

# Initialize database on module import (safely creates tables under Gunicorn)
try:
    init_db()
except Exception as e:
    logger.error(f"Error initializing database on startup: {e}")


# ===== Rate Limiter (in-memory, bounded) =====
import time
from collections import OrderedDict
# Bounded LRU-like store: max 10_000 entries, oldest evicted automatically
_RATE_LIMIT_MAX_ENTRIES = 10_000
_rate_limit_store = OrderedDict()
_RATE_LIMIT_LOCK = threading.Lock()

def check_rate_limit(key, max_attempts=5, window_seconds=300):
    """
    Simple in-memory rate limiter with bounded store (thread-safe).
    Returns True if request is allowed, False if rate limited.
    key: unique identifier (e.g. f"otp:{ip}")
    max_attempts: max requests in the window
    window_seconds: time window in seconds
    """
    now = time.time()
    ip = request.access_route[0] if request.access_route else request.remote_addr or 'unknown'
    store_key = f"{key}:{ip}"

    with _RATE_LIMIT_LOCK:
        # Evict oldest entries if store is too large
        while len(_rate_limit_store) >= _RATE_LIMIT_MAX_ENTRIES:
            _rate_limit_store.popitem(last=False)

        # Get existing timestamps for this key (or empty list)
        timestamps = _rate_limit_store.get(store_key, [])

        # Clean old entries
        timestamps = [t for t in timestamps if now - t < window_seconds]

        # Check limit
        if len(timestamps) >= max_attempts:
            _rate_limit_store[store_key] = timestamps
            return False

        timestamps.append(now)
        if timestamps:
            _rate_limit_store[store_key] = timestamps
        return True


# ===== CSRF Protection =====
def generate_csrf_token():
    """Generate or retrieve CSRF token from session."""
    if 'csrf_token' not in session:
        session['csrf_token'] = secrets.token_hex(32)
    return session['csrf_token']


def csrf_required(f):
    """Decorator to require valid CSRF token on state-changing requests."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if request.method in ('POST', 'PUT', 'DELETE'):
            token = request.headers.get('X-CSRF-Token') or request.form.get('csrf_token')
            expected = session.get('csrf_token')
            if not expected or not token or token != expected:
                if request.is_json or request.path.startswith('/admin/api'):
                    return jsonify({'success': False, 'error': 'invalid_csrf', 'message': 'CSRF token tidak valid. Silakan refresh halaman.'}), 403
                flash('CSRF token tidak valid. Silakan coba lagi.', 'error')
                return redirect(url_for('admin_dashboard'))
        return f(*args, **kwargs)
    return decorated


@app.context_processor
def inject_csrf_token():
    """Inject CSRF token and version into all templates."""
    return {'csrf_token': generate_csrf_token(), 'version': VERSION}


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
    import urllib.request
    import urllib.parse
    import json
    
    token = get_saas_setting('wa_api_token', '')
    if not token:
        logger.warning(f"WhatsApp Token not configured. Message to {target}: {message}")
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
    """Verify password against stored hash. Supports both legacy SHA-256 and werkzeug hashes."""
    if stored_hash.startswith(('scrypt:', 'pbkdf2:')):
        return check_password_hash(stored_hash, password)
    return hashlib.sha256(password.encode()).hexdigest() == stored_hash

def generate_token(length=6, db=None):
    """Generate a unique uppercase alphanumeric token with collision protection."""
    chars = string.ascii_uppercase + string.digits
    max_attempts = 100
    for _ in range(max_attempts):
        token = ''.join(secrets.choice(chars) for _ in range(length))
        if db is None:
            db = get_db_standalone()
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (token,)).fetchone()
        if not existing:
            return token
    # Last resort: increase length to avoid collision
    token = ''.join(secrets.choice(chars) for _ in range(length + 2))
    return token

def admin_required(f):
    """Decorator to require admin login + CSRF check for state-changing methods."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if 'admin_id' not in session:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'unauthorized', 'message': 'Silakan login terlebih dahulu'}), 401
            return redirect(url_for('admin_login'))
        # CSRF check for state-changing methods (POST, PUT, DELETE)
        if request.method in ('POST', 'PUT', 'DELETE'):
            token = request.headers.get('X-CSRF-Token') or request.form.get('csrf_token')
            expected = session.get('csrf_token')
            if not expected or not token or token != expected:
                if request.is_json or request.path.startswith('/admin/api'):
                    return jsonify({'success': False, 'error': 'invalid_csrf', 'message': 'CSRF token tidak valid. Silakan refresh halaman.'}), 403
                flash('CSRF token tidak valid. Silakan coba lagi.', 'error')
                return redirect(url_for('admin_dashboard'))
        # Check account expiry (skip for super admin)
        if session.get('admin_username') != ADMIN_USERNAME:
            db = get_db()
            user = db.execute('SELECT expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
            if user and user['expires_at']:
                expires_at = datetime.strptime(user['expires_at'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
                if datetime.now(timezone.utc) > expires_at:
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
        if session.get('admin_username') != ADMIN_USERNAME:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'forbidden', 'message': 'Akses khusus Super Admin'}), 403
            return abort(403)
        return f(*args, **kwargs)
    return decorated


def check_exam_ownership(db, exam_id):
    """Check if current user is allowed to manage the given exam."""
    if session.get('admin_username') == 'admin':
        return True
    exam = db.execute('SELECT created_by FROM exams WHERE id = ?', (exam_id,)).fetchone()
    return exam is not None and exam['created_by'] == session['admin_id']


def check_submission_ownership(db, submission_id):
    """Check if current user is allowed to manage the given submission."""
    if session.get('admin_username') == 'admin':
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
    response.headers['Content-Security-Policy'] = "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'"
    response.headers['X-Content-Type-Options'] = 'nosniff'
    response.headers['X-Frame-Options'] = 'DENY'
    response.headers['X-XSS-Protection'] = '1; mode=block'
    response.headers['Access-Control-Allow-Origin'] = '*'
    response.headers['Access-Control-Allow-Methods'] = 'GET, POST, PUT, DELETE, OPTIONS'
    response.headers['Access-Control-Allow-Headers'] = 'Content-Type, X-CSRF-Token, X-App-Version, X-Exam-Token'
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
