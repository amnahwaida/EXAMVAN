"""
EXAMVAN Server - REST API & Admin Panel
Version: 2.1.3
Platform: Flask + SQLite
"""

import os
import secrets
import string
import random
import sqlite3
import hashlib
import socket
import json
import csv
import io
from datetime import datetime, timezone
from functools import wraps

from flask import (
    Flask, request, jsonify, render_template,
    redirect, url_for, session, send_file, flash, abort
)
from werkzeug.utils import secure_filename

# ===== Configuration =====
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
STORAGE_DIR = os.path.join(BASE_DIR, 'storage')
DATABASE = os.environ.get('DATABASE_PATH', os.path.join(BASE_DIR, 'data', 'examvan.db'))
MAX_FILE_SIZE = 5 * 1024 * 1024  # 5MB

def localize_date_string(utc_str, tz_offset_min=None):
    """Localize database UTC string using a browser timezone offset in minutes."""
    if not utc_str:
        return '—'
    try:
        from datetime import timedelta
        # SQLite stores datetime strings in UTC (e.g., '2026-05-22 14:09:54')
        iso_str = utc_str.strip()
        if ' ' in iso_str:
            iso_str = iso_str.replace(' ', 'T')
        if not iso_str.endswith('Z'):
            iso_str += 'Z'
        
        # Parse as offset-aware UTC
        dt = datetime.fromisoformat(iso_str.replace('Z', '+00:00'))
        
        if tz_offset_min is not None:
            # tz_offset_min is from getTimezoneOffset() (local - UTC in minutes).
            # e.g., for UTC+7 it is -420.
            # Local time is UTC - offset_min minutes (so dt - (-420) = dt + 420 min).
            local_dt = dt - timedelta(minutes=tz_offset_min)
            return local_dt.strftime('%Y-%m-%d %H:%M:%S')
        else:
            # Default fallback format
            return dt.strftime('%Y-%m-%d %H:%M:%S UTC')
    except Exception as e:
        print("Localization error:", e)
        return utc_str
DEFAULT_ADMIN = {'username': 'admin', 'password': 'examvan2026'}

os.makedirs(STORAGE_DIR, exist_ok=True)
os.makedirs(os.path.dirname(DATABASE), exist_ok=True)

# ===== App Init =====
app = Flask(__name__)
app.secret_key = os.environ.get('EXAMVAN_SECRET', secrets.token_hex(32))
app.config['MAX_CONTENT_LENGTH'] = MAX_FILE_SIZE + 4096


# ===== Database =====
def get_db():
    """Get database connection with Row factory."""
    db = sqlite3.connect(DATABASE)
    db.execute('PRAGMA foreign_keys = ON')
    db.row_factory = sqlite3.Row
    return db


def init_db():
    """Initialize database tables and default admin user."""
    db = get_db()
    db.executescript('''
        CREATE TABLE IF NOT EXISTS exams (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            file_path TEXT NOT NULL,
            size_bytes INTEGER NOT NULL,
            token TEXT UNIQUE NOT NULL,
            questions_json TEXT,
            status TEXT DEFAULT 'active' CHECK(status IN ('active', 'inactive')),
            security_level TEXT DEFAULT 'medium' CHECK(security_level IN ('medium', 'low')),
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
        'android_version': '2.1.3',
        'webapp_version': '2.1.3'
    }
    for k, v in default_settings.items():
        existing_setting = db.execute('SELECT value FROM saas_settings WHERE key = ?', (k,)).fetchone()
        if not existing_setting:
            db.execute('INSERT INTO saas_settings (key, value) VALUES (?, ?)', (k, v))
        elif k in ('android_version', 'webapp_version') and existing_setting['value'] in ('2.1.0', '2.1.1', '2.1.2'):
            db.execute('UPDATE saas_settings SET value = ? WHERE key = ?', (v, k))
    db.commit()

    existing = db.execute(
        'SELECT id FROM admin_users WHERE username = ?',
        (DEFAULT_ADMIN['username'],)
    ).fetchone()

    if not existing:
        pw_hash = hashlib.sha256(DEFAULT_ADMIN['password'].encode()).hexdigest()
        db.execute(
            'INSERT INTO admin_users (username, password_hash) VALUES (?, ?)',
            (DEFAULT_ADMIN['username'], pw_hash)
        )
        db.commit()

    # Migrate: add token column if missing (for older databases)
    try:
        db.execute('SELECT token FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute('ALTER TABLE exams ADD COLUMN token TEXT')
        # Generate tokens for existing rows
        rows = db.execute('SELECT id FROM exams WHERE token IS NULL').fetchall()
        for row in rows:
            db.execute('UPDATE exams SET token = ? WHERE id = ?',
                       (generate_token(), row['id']))
        db.commit()

    # Migrate: add questions_json column if missing
    try:
        db.execute('SELECT questions_json FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute('ALTER TABLE exams ADD COLUMN questions_json TEXT')
        db.commit()

    # Migrate: add created_by column if missing
    try:
        db.execute('SELECT created_by FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute('ALTER TABLE exams ADD COLUMN created_by INTEGER DEFAULT 1')
        db.commit()

    # Migrate: add security_level column if missing
    try:
        db.execute('SELECT security_level FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE exams ADD COLUMN security_level TEXT DEFAULT 'medium'")
        db.commit()

    # Migrate: add start_time column if missing
    try:
        db.execute('SELECT start_time FROM submissions LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE submissions ADD COLUMN start_time TIMESTAMP")
        db.commit()

    # Migrate: add mac_address column if missing
    try:
        db.execute('SELECT mac_address FROM submissions LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE submissions ADD COLUMN mac_address TEXT")
        db.commit()

    # Migrate: add public_results column if missing
    try:
        db.execute('SELECT public_results FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE exams ADD COLUMN public_results INTEGER DEFAULT 1")
        db.commit()

    # Migrate: add show_answers column if missing
    try:
        db.execute('SELECT show_answers FROM exams LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE exams ADD COLUMN show_answers INTEGER DEFAULT 0")
        db.commit()

    # Migrate: add max_exams column to admin_users if missing
    try:
        db.execute('SELECT max_exams FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN max_exams INTEGER DEFAULT 3")
        db.commit()

    # Migrate: add max_pdf_size column to admin_users if missing (default 1MB)
    try:
        db.execute('SELECT max_pdf_size FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN max_pdf_size INTEGER DEFAULT 1048576")
        db.commit()

    # Migrate: add whatsapp_number to admin_users if missing
    try:
        db.execute('SELECT whatsapp_number FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN whatsapp_number TEXT")
        db.commit()

    # Migrate: add status to admin_users if missing
    try:
        db.execute('SELECT status FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN status TEXT DEFAULT 'active'")
        db.commit()

    # Migrate: add otp_code to admin_users if missing
    try:
        db.execute('SELECT otp_code FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN otp_code TEXT")
        db.commit()

    # Migrate: add expires_at to admin_users if missing
    try:
        db.execute('SELECT expires_at FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN expires_at TIMESTAMP")
        db.commit()

    # Migrate: add otp_expiry to admin_users if missing
    try:
        db.execute('SELECT otp_expiry FROM admin_users LIMIT 1')
    except sqlite3.OperationalError:
        db.execute("ALTER TABLE admin_users ADD COLUMN otp_expiry TIMESTAMP")
        db.commit()

    db.close()

# Initialize database on module import (safely creates tables under Gunicorn)
try:
    init_db()
except Exception as e:
    print(f"Error initializing database on startup: {e}")


# ===== Helpers =====
def get_saas_setting(key, default=''):
    db = get_db()
    row = db.execute('SELECT value FROM saas_settings WHERE key = ?', (key,)).fetchone()
    db.close()
    return row['value'] if row else default

def set_saas_setting(key, value):
    db = get_db()
    db.execute('INSERT OR REPLACE INTO saas_settings (key, value) VALUES (?, ?)', (key, str(value)))
    db.commit()
    db.close()

def send_whatsapp(target, message):
    import urllib.request
    import urllib.parse
    import json
    
    token = get_saas_setting('wa_api_token', '')
    if not token:
        print(f"WhatsApp Token not configured. Message to {target}: {message}")
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
            print(f"Fonnte Send WA Response: {res_json}")
            return res_json.get('status', False)
    except Exception as e:
        print(f"Fonnte Send WA Exception: {e}")
        return False

def generate_token(length=6):
    """Generate a unique uppercase alphanumeric token."""
    chars = string.ascii_uppercase + string.digits
    while True:
        token = ''.join(random.choices(chars, k=length))
        # Ensure uniqueness
        db = get_db()
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (token,)).fetchone()
        db.close()
        if not existing:
            return token

def admin_required(f):
    """Decorator to require admin login."""
    @wraps(f)
    def decorated(*args, **kwargs):
        if 'admin_id' not in session:
            if request.is_json or request.path.startswith('/admin/api'):
                return jsonify({'success': False, 'error': 'unauthorized'}), 401
            return redirect(url_for('admin_login'))
        # Check account expiry (skip for super admin)
        if session.get('admin_username') != 'admin':
            db = get_db()
            user = db.execute('SELECT expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
            db.close()
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
                return jsonify({'success': False, 'error': 'unauthorized'}), 401
            return redirect(url_for('admin_login'))
        if session.get('admin_username') != 'admin':
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


def calculate_submission_score(answers, questions):
    """Calculate the score for a student submission given their answers and the exam's questions config."""
    if not questions:
        return None
    try:
        earned_weight = 0.0
        for q in questions:
            # Normalize q_num: handle potential float representation (e.g. 1.0 -> "1")
            try:
                num_val = float(q['number'])
                if num_val.is_integer():
                    q_num = str(int(num_val))
                else:
                    q_num = str(q['number'])
            except Exception:
                q_num = str(q['number'])

            student_ans = answers.get(q_num)
            correct_ans = q.get('key')
            q_weight = float(q.get('weight', 1.0))
            partial_scoring = q.get('partial_scoring', False)

            earned_q_weight = 0.0
            if student_ans is not None and correct_ans is not None:
                if q['type'] in ['single_choice', 'true_false', 'short_answer']:
                    s_norm = ' '.join(str(student_ans).split()).upper()
                    c_norm = ' '.join(str(correct_ans).split()).upper()
                    if s_norm == c_norm:
                        earned_q_weight = q_weight
                elif q['type'] == 'multiple_choice':
                    if isinstance(student_ans, list) and isinstance(correct_ans, list):
                        if partial_scoring:
                            correct_set = set(str(x).upper() for x in correct_ans)
                            student_set = set(str(x).upper() for x in student_ans)
                            if correct_set:
                                correct_selected = sum(1 for x in student_set if x in correct_set)
                                incorrect_selected = sum(1 for x in student_set if x not in correct_set)
                                portion = max(0.0, (correct_selected - incorrect_selected) / len(correct_set))
                                earned_q_weight = portion * q_weight
                        else:
                            if sorted([str(x).upper() for x in student_ans]) == sorted([str(x).upper() for x in correct_ans]):
                                earned_q_weight = q_weight
                elif q['type'] == 'matching':
                    if isinstance(student_ans, dict) and isinstance(correct_ans, dict):
                        if partial_scoring:
                            if correct_ans:
                                correct_matches = 0
                                for k, v in correct_ans.items():
                                    if str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper():
                                        correct_matches += 1
                                portion = correct_matches / len(correct_ans)
                                earned_q_weight = portion * q_weight
                        else:
                            match = True
                            for k, v in correct_ans.items():
                                if str(student_ans.get(k, '')).strip().upper() != str(v).strip().upper():
                                    match = False
                                    break
                            if match:
                                earned_q_weight = q_weight
            
            earned_weight += earned_q_weight

        return round(earned_weight, 2)
    except Exception as e:
        print("Scoring calculation error:", e)
        return None


def get_local_ip():
    """Get the server's LAN IP address, offline-friendly."""
    # Try connecting to a non-routable address to determine the outgoing interface IP
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(1)
        # 10.255.255.255 does not require internet connection to resolve
        s.connect(('10.255.255.255', 1))
        ip = s.getsockname()[0]
        s.close()
        if ip and ip != '127.0.0.1' and not ip.startswith('127.'):
            return ip
    except Exception:
        pass

    # Fallback: inspect network interfaces by hostname
    try:
        hostname = socket.gethostname()
        ips = socket.gethostbyname_ex(hostname)[2]
        for ip in ips:
            if not ip.startswith('127.') and not ip.startswith('172.'):
                return ip
    except Exception:
        pass

    return '127.0.0.1'


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
    for f in os.listdir(STORAGE_DIR):
        fp = os.path.join(STORAGE_DIR, f)
        if os.path.isfile(fp):
            total += os.path.getsize(fp)
    return total


def format_iso_utc(date_str):
    """Convert SQLite YYYY-MM-DD HH:MM:SS string to ISO 8601 UTC format (YYYY-MM-DDTHH:MM:SSZ)."""
    if not date_str:
        return date_str
    if ' ' in date_str:
        return date_str.replace(' ', 'T') + 'Z'
    if 'T' in date_str or date_str.endswith('Z'):
        return date_str
    return date_str + 'Z'


# ===== REST API Endpoints =====

@app.route('/api/health')
def api_health():
    """Health check endpoint."""
    now = datetime.now(timezone.utc)
    required_version = get_saas_setting('android_version', '2.1.3')
    return jsonify({
        'status': 'ok',
        'version': '2.0',
        'required_app_version': required_version,
        'lan_mode': True,
        'timestamp': now.isoformat(),
        'server_time_utc': now.strftime('%Y-%m-%dT%H:%M:%SZ')
    })


@app.route('/api/time')
def api_time():
    """Return authoritative UTC server time for client synchronization."""
    now = datetime.now(timezone.utc)
    return jsonify({
        'utc': now.strftime('%Y-%m-%dT%H:%M:%SZ'),
        'unix': int(now.timestamp()),
        'timezone': 'UTC'
    })


@app.route('/api/exams')
def api_exams():
    """Get list of active exams."""
    db = get_db()
    exams = db.execute(
        'SELECT id, name, status, size_bytes, created_at '
        'FROM exams WHERE status = ? ORDER BY created_at DESC',
        ('active',)
    ).fetchall()

    data = []
    for exam in exams:
        data.append({
            'id': exam['id'],
            'name': exam['name'],
            'status': exam['status'],
            'size_mb': round(exam['size_bytes'] / (1024 * 1024), 2),
            'created_at': format_iso_utc(exam['created_at'])
        })

    db.close()
    return jsonify({'success': True, 'data': data})


@app.route('/api/exams/token/<token>')
def api_exam_by_token(token):
    """Get exam info by token. Used by Android app."""
    required_version = get_saas_setting('android_version', '2.1.3')
    client_version = request.headers.get('X-App-Version')
    if client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version or "v1.x"}). Silakan unduh EXAMVAN v{required_version} terbaru untuk dapat mengikuti ujian.'
        }), 426

    token = token.strip().upper()
    db = get_db()
    exam = db.execute(
        'SELECT id, name, status, size_bytes, token, questions_json, security_level, created_at '
        'FROM exams WHERE token = ? AND status = ?',
        (token, 'active')
    ).fetchone()
    db.close()

    if not exam:
        return jsonify({
            'success': False,
            'error': 'invalid_token',
            'message': 'Token tidak valid atau ujian sudah berakhir'
        }), 404

    # Process questions configuration
    questions_raw = exam['questions_json']
    questions = []
    if questions_raw:
        try:
            questions = json.loads(questions_raw)
            # Remove keys for security
            for q in questions:
                if 'key' in q:
                    del q['key']
        except Exception:
            pass

    # If no questions configured, return empty list (PDF-only mode)

    return jsonify({
        'success': True,
        'data': {
            'id': exam['id'],
            'name': exam['name'],
            'status': exam['status'],
            'token': exam['token'],
            'size_mb': round(exam['size_bytes'] / (1024 * 1024), 2),
            'questions': questions,
            'security_level': exam['security_level'] or 'medium',
            'created_at': format_iso_utc(exam['created_at'])
        }
    })


@app.route('/api/exams/<int:exam_id>/submit', methods=['POST'])
def api_submit_exam(exam_id):
    """Receive student exam submissions and auto-grade if keys exist."""
    required_version = get_saas_setting('android_version', '2.1.3')
    client_version = request.headers.get('X-App-Version')
    if client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version or "v1.x"}). Silakan unduh EXAMVAN v{required_version} terbaru untuk dapat mengumpulkan jawaban.'
        }), 426

    data = request.json or {}
    student_name = data.get('student_name', '').strip()
    exam_number = data.get('exam_number', '').strip()
    student_class = data.get('student_class', '').strip()
    answers = data.get('answers', {})  # Map of "number" -> answer value
    start_time_raw = data.get('start_time')
    mac_address = data.get('mac_address', '').strip() or None

    start_time = None
    if start_time_raw:
        try:
            # "2026-05-24T00:00:00Z" -> "2026-05-24 00:00:00"
            start_time = start_time_raw.replace('T', ' ').replace('Z', '')
        except Exception:
            start_time = start_time_raw

    if not student_name or not exam_number or not student_class:
        return jsonify({'success': False, 'message': 'Identitas siswa tidak lengkap'}), 400

    db = get_db()
    exam = db.execute('SELECT questions_json FROM exams WHERE id = ? AND status = ?', (exam_id, 'active')).fetchone()
    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    # Calculate score if questions exist
    score = None
    questions_raw = exam['questions_json']
    if questions_raw:
        try:
            questions = json.loads(questions_raw)
            score = calculate_submission_score(answers, questions)
        except Exception as e:
            print("Auto-grading error:", e)

    # Save submission
    db.execute(
        'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, answers_json, score, start_time, mac_address) '
        'VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
        (exam_id, student_name, exam_number, student_class, json.dumps(answers), score, start_time, mac_address)
    )
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': 'Jawaban berhasil dikirim',
        'score': score
    })


@app.route('/api/exams/<int:exam_id>/pdf')
def api_exam_pdf(exam_id):
    """Stream PDF file for an exam."""
    db = get_db()
    exam = db.execute(
        'SELECT * FROM exams WHERE id = ? AND status = ?',
        (exam_id, 'active')
    ).fetchone()
    db.close()

    if not exam:
        return jsonify({
            'success': False,
            'error': 'exam_not_found',
            'message': 'Ujian tidak tersedia atau sudah berakhir'
        }), 404

    file_path = os.path.join(STORAGE_DIR, exam['file_path'])
    if not os.path.exists(file_path):
        return jsonify({
            'success': False,
            'error': 'file_not_found',
            'message': 'File ujian tidak ditemukan di server'
        }), 404

    response = send_file(file_path, mimetype='application/pdf', as_attachment=False)
    response.headers['Content-Disposition'] = f'inline; filename="exam_{exam_id}.pdf"'
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    response.headers['X-Content-Type-Options'] = 'nosniff'
    response.headers['Accept-Ranges'] = 'bytes'
    return response


# ===== Admin Panel Routes =====

@app.route('/')
def index():
    """Render landing page."""
    return render_template('index.html')


@app.route('/download/apk')
def download_apk():
    """Download client Android APK."""
    apk_path = os.path.join(BASE_DIR, 'static', 'EXAMVAN.apk')
    return send_file(apk_path, as_attachment=True, download_name='EXAMVAN.apk')


@app.route('/download')
def download_page():
    """Render separate download page showing app and web versions."""
    android_ver = get_saas_setting('android_version', '2.1.0')
    webapp_ver = get_saas_setting('webapp_version', '2.1.0')
    apk_path = os.path.join(BASE_DIR, 'static', 'EXAMVAN.apk')
    file_size_mb = 0
    if os.path.exists(apk_path):
        file_size_mb = round(os.path.getsize(apk_path) / (1024 * 1024), 2)
    
    return render_template('download.html', 
                           android_version=android_ver, 
                           webapp_version=webapp_ver,
                           file_size_mb=file_size_mb)


@app.route('/admin/login', methods=['GET', 'POST'])
def admin_login():
    """Admin login page."""
    if 'admin_id' in session:
        return redirect(url_for('admin_dashboard'))

    if request.method == 'POST':
        username = request.form.get('username', '').strip().lower()
        password = request.form.get('password', '')

        db = get_db()
        pw_hash = hashlib.sha256(password.encode()).hexdigest()
        user = db.execute(
            'SELECT id, username, status FROM admin_users WHERE username = ? AND password_hash = ?',
            (username, pw_hash)
        ).fetchone()
        db.close()

        if user:
            if user['status'] == 'pending_otp':
                flash('Pendaftaran Anda membutuhkan konfirmasi OTP WhatsApp. Silakan verifikasi.', 'warning')
                return redirect(url_for('verify_otp', username=user['username']))
            elif user['status'] == 'suspended':
                flash('Akun Anda telah dinonaktifkan oleh administrator.', 'error')
                return redirect(url_for('admin_login'))

            # Check account expiry (skip for super admin)
            if user['username'] != 'admin':
                db2 = get_db()
                user_full = db2.execute('SELECT expires_at FROM admin_users WHERE id = ?', (user['id'],)).fetchone()
                db2.close()
                if user_full and user_full['expires_at']:
                    from datetime import timedelta
                    expires_at = datetime.strptime(user_full['expires_at'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
                    if datetime.now(timezone.utc) > expires_at:
                        flash('Masa aktif akun Anda telah habis. Silakan hubungi administrator.', 'error')
                        return redirect(url_for('admin_login'))

            session['admin_id'] = user['id']
            session['admin_username'] = user['username']
            return redirect(url_for('admin_dashboard'))
        else:
            flash('Username atau password salah', 'error')

    return render_template('login.html')


@app.route('/register', methods=['GET', 'POST'])
def register():
    """Register a new teacher account (SaaS)."""
    if 'admin_id' in session:
        return redirect(url_for('admin_dashboard'))

    if request.method == 'POST':
        username = request.form.get('username', '').strip().lower()
        password = request.form.get('password', '')
        whatsapp = request.form.get('whatsapp', '').strip()

        if not username or not password or not whatsapp:
            flash('Semua kolom wajib diisi', 'error')
            return render_template('register.html')

        if username == 'admin':
            flash('Username "admin" tidak dapat digunakan', 'error')
            return render_template('register.html')

        if len(username) < 3 or not username.isalnum():
            flash('Username minimal 3 karakter alfanumerik', 'error')
            return render_template('register.html')

        db = get_db()
        existing = db.execute('SELECT id FROM admin_users WHERE username = ?', (username,)).fetchone()
        if existing:
            db.close()
            flash('Username sudah digunakan', 'error')
            return render_template('register.html')

        pw_hash = hashlib.sha256(password.encode()).hexdigest()
        
        default_exams = int(get_saas_setting('default_max_exams', '3'))
        default_pdf = int(get_saas_setting('default_max_pdf_size', '1048576'))
        wa_enabled = get_saas_setting('wa_verification_enabled', '0') == '1'
        
        default_active_days = int(get_saas_setting('default_active_days', '1'))
        from datetime import timedelta
        expires_at = (datetime.now(timezone.utc) + timedelta(days=default_active_days)).strftime('%Y-%m-%d %H:%M:%S')

        if wa_enabled:
            otp = ''.join(random.choices(string.digits, k=6))
            otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')
            
            db.execute(
                'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, otp_code, otp_expiry, max_exams, max_pdf_size, expires_at) '
                'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (username, pw_hash, whatsapp, 'pending_otp', otp, otp_expiry, default_exams, default_pdf, expires_at)
            )
            db.commit()
            db.close()
            
            template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
            message = template.replace('{otp}', otp)
            send_whatsapp(whatsapp, message)
            
            flash('Registrasi berhasil! Masukkan kode OTP yang dikirim ke nomor WhatsApp Anda.', 'warning')
            return redirect(url_for('verify_otp', username=username))
        else:
            db.execute(
                'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, max_exams, max_pdf_size, expires_at) '
                'VALUES (?, ?, ?, ?, ?, ?, ?)',
                (username, pw_hash, whatsapp, 'active', default_exams, default_pdf, expires_at)
            )
            db.commit()
            db.close()
            
            flash('Registrasi berhasil! Silakan masuk dengan akun Anda.', 'success')
            return redirect(url_for('admin_login'))

    return render_template('register.html')


@app.route('/verify-otp', methods=['GET', 'POST'])
def verify_otp():
    """Verify registration OTP code."""
    username = request.args.get('username', '').strip().lower()
    
    if request.method == 'POST':
        username = request.form.get('username', '').strip().lower()
        otp_input = request.form.get('otp', '').strip()
        
        if not username or not otp_input:
            flash('Semua kolom wajib diisi', 'error')
            return render_template('verify_otp.html', username=username)
            
        db = get_db()
        user = db.execute(
            'SELECT id, otp_code, otp_expiry, whatsapp_number FROM admin_users WHERE username = ? AND status = ?',
            (username, 'pending_otp')
        ).fetchone()
        
        if not user:
            db.close()
            flash('Permintaan verifikasi tidak valid atau kedaluwarsa', 'error')
            return redirect(url_for('admin_login'))
            
        if user['otp_code'] != otp_input:
            db.close()
            flash('Kode OTP yang Anda masukkan salah', 'error')
            return render_template('verify_otp.html', username=username)
            
        now_str = datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S')
        if user['otp_expiry'] < now_str:
            otp = ''.join(random.choices(string.digits, k=6))
            from datetime import timedelta
            otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')
            
            db.execute(
                'UPDATE admin_users SET otp_code = ?, otp_expiry = ? WHERE id = ?',
                (otp, otp_expiry, user['id'])
            )
            db.commit()
            db.close()
            
            template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
            message = template.replace('{otp}', otp)
            send_whatsapp(user['whatsapp_number'], message)
            
            flash('Kode OTP telah kedaluwarsa. Kami telah mengirimkan kode OTP baru ke WhatsApp Anda.', 'warning')
            return render_template('verify_otp.html', username=username)
            
        db.execute('UPDATE admin_users SET status = ?, otp_code = NULL, otp_expiry = NULL WHERE id = ?', ('active', user['id']))
        db.commit()
        db.close()
        
        flash('Verifikasi nomor WhatsApp berhasil! Akun Anda aktif. Silakan login.', 'success')
        return redirect(url_for('admin_login'))
        
    return render_template('verify_otp.html', username=username)


@app.route('/resend-otp', methods=['POST'])
def resend_otp():
    """Resend OTP code to user's registered WhatsApp number."""
    username = request.form.get('username', '').strip().lower()
    
    if not username:
        return jsonify({'success': False, 'message': 'Username wajib diisi'}), 400
        
    db = get_db()
    user = db.execute(
        'SELECT id, whatsapp_number FROM admin_users WHERE username = ? AND status = ?',
        (username, 'pending_otp')
    ).fetchone()
    
    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'User tidak ditemukan atau sudah terverifikasi'}), 404
        
    otp = ''.join(random.choices(string.digits, k=6))
    from datetime import timedelta
    otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')
    
    db.execute(
        'UPDATE admin_users SET otp_code = ?, otp_expiry = ? WHERE id = ?',
        (otp, otp_expiry, user['id'])
    )
    db.commit()
    db.close()
    
    template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
    message = template.replace('{otp}', otp)
    send_whatsapp(user['whatsapp_number'], message)
    
    return jsonify({'success': True, 'message': 'Kode OTP baru berhasil dikirim via WhatsApp'})


@app.route('/admin/logout')
def admin_logout():
    """Admin logout."""
    session.clear()
    return redirect(url_for('admin_login'))


@app.route('/admin/dashboard')
@admin_required
def admin_dashboard():
    """Admin dashboard page."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == 'admin')
    
    if is_super_admin:
        exams = db.execute(
            'SELECT e.*, u.username as creator_name '
            'FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id '
            'ORDER BY e.created_at DESC'
        ).fetchall()
    else:
        exams = db.execute(
            'SELECT e.*, u.username as creator_name '
            'FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id '
            'WHERE e.created_by = ? '
            'ORDER BY e.created_at DESC',
            (session['admin_id'],)
        ).fetchall()

    total = len(exams)
    active = sum(1 for e in exams if e['status'] == 'active')
    inactive = total - active
    
    if is_super_admin:
        storage_bytes = get_storage_stats()
    else:
        storage_bytes = sum(e['size_bytes'] for e in exams if e['size_bytes'] is not None)
        
    net_info = get_network_info()

    # Get per-user limits
    if is_super_admin:
        user_max_pdf = MAX_FILE_SIZE
        user_max_exams = '∞'
        account_expires = None
    else:
        user_row = db.execute('SELECT max_exams, max_pdf_size, expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        user_max_pdf = user_row['max_pdf_size'] if (user_row and user_row['max_pdf_size']) else 1048576
        user_max_exams = user_row['max_exams'] if (user_row and user_row['max_exams']) else 3
        account_expires = user_row['expires_at'] if user_row else None

    db.close()
    return render_template(
        'dashboard.html',
        exams=exams,
        stats={
            'total': total,
            'active': active,
            'inactive': inactive,
            'storage_mb': round(storage_bytes / (1024 * 1024), 2),
        },
        net_info=net_info,
        local_ip=net_info['display_host'],
        admin_user=session.get('admin_username', 'Admin'),
        max_size_mb=round(user_max_pdf / (1024 * 1024), 1),
        max_exams=user_max_exams,
        account_expires=account_expires,
        active_page='dashboard'
    )

@app.route('/admin/create-exam')
@admin_required
def admin_create_exam_page():
    """Create exam page."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == 'admin')
    
    if is_super_admin:
        user_max_pdf = MAX_FILE_SIZE
        user_max_exams = '∞'
        account_expires = None
    else:
        user_row = db.execute('SELECT max_exams, max_pdf_size, expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        user_max_pdf = user_row['max_pdf_size'] if (user_row and user_row['max_pdf_size']) else 1048576
        user_max_exams = user_row['max_exams'] if (user_row and user_row['max_exams']) else 3
        account_expires = user_row['expires_at'] if user_row else None
        
    db.close()
    return render_template(
        'buat_ujian.html',
        admin_user=session.get('admin_username', 'Admin'),
        max_size_mb=round(user_max_pdf / (1024 * 1024), 1),
        max_exams=user_max_exams,
        account_expires=account_expires,
        active_page='create_exam'
    )


# ===== Admin API Routes =====

@app.route('/admin/api/upload', methods=['POST'])
@admin_required
def admin_upload():
    """Upload a new exam PDF."""
    name = request.form.get('name', '').strip()
    file = request.files.get('pdf_file')

    if not name:
        return jsonify({'success': False, 'message': 'Nama ujian wajib diisi'}), 400

    if not file or file.filename == '':
        return jsonify({'success': False, 'message': 'File PDF wajib dipilih'}), 400

    if file.content_type != 'application/pdf':
        return jsonify({'success': False, 'message': 'Hanya file PDF yang diizinkan'}), 400

    # Read file content to check size
    file_data = file.read()
    if len(file_data) > MAX_FILE_SIZE:
        return jsonify({
            'success': False,
            'message': f'Ukuran file melebihi batas server ({MAX_FILE_SIZE // (1024*1024)}MB)'
        }), 400

    db = get_db()
    custom_token = request.form.get('custom_token', '').strip().upper()
    
    # Check per-user limits (for non-super admin users)
    if session.get('admin_username') != 'admin':
        user = db.execute('SELECT max_exams, max_pdf_size FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        exam_limit = user['max_exams'] if (user and user['max_exams'] is not None) else 3
        pdf_limit = user['max_pdf_size'] if (user and user['max_pdf_size'] is not None) else 1048576
        
        # Check PDF size limit
        if len(file_data) > pdf_limit:
            pdf_limit_mb = round(pdf_limit / (1024 * 1024), 2)
            db.close()
            return jsonify({
                'success': False, 
                'message': f'Ukuran file melebihi batas akun Anda ({pdf_limit_mb}MB). Silakan hubungi Super Admin untuk menaikkan limit.'
            }), 403
        
        # Check exam count limit
        current_count = db.execute('SELECT COUNT(*) as count FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchone()['count']
        if current_count >= exam_limit:
            db.close()
            return jsonify({
                'success': False, 
                'message': f'Batas pembuatan ujian tercapai. Batas akun Anda adalah {exam_limit} ujian. Silakan hubungi Super Admin untuk menaikkan limit.'
            }), 403

    if custom_token:
        if len(custom_token) != 6 or not custom_token.isalnum():
            db.close()
            return jsonify({'success': False, 'message': 'Token kustom harus terdiri dari 6 karakter alfanumerik'}), 400
        
        # Check uniqueness
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (custom_token,)).fetchone()
        if existing:
            db.close()
            return jsonify({'success': False, 'message': 'Token kustom sudah digunakan oleh ujian lain'}), 400
        token = custom_token
    else:
        # Generate unique token
        token = generate_token()

    # Save file with secure name
    timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
    safe_name = secure_filename(file.filename)
    filename = f"{timestamp}_{safe_name}"
    file_path = os.path.join(STORAGE_DIR, filename)

    with open(file_path, 'wb') as f:
        f.write(file_data)

    # Save to database
    db.execute(
        'INSERT INTO exams (name, file_path, size_bytes, token, status, created_by) VALUES (?, ?, ?, ?, ?, ?)',
        (name, filename, len(file_data), token, 'active', session['admin_id'])
    )
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil diupload dengan token: {token}',
        'token': token
    })


@app.route('/admin/api/exams/create-from-editor', methods=['POST'])
@admin_required
def admin_create_exam_from_editor():
    """Create an exam directly from the question editor with generated PDF."""
    name = request.form.get('name', '').strip()
    questions_json = request.form.get('questions_json', '[]')
    custom_token = request.form.get('custom_token', '').strip().upper()
    file = request.files.get('pdf_file')

    if not name:
        return jsonify({'success': False, 'message': 'Nama ujian wajib diisi'}), 400

    if not file or file.filename == '':
        return jsonify({'success': False, 'message': 'File PDF hasil generate wajib dikirim'}), 400

    file_data = file.read()
    if len(file_data) > MAX_FILE_SIZE:
        return jsonify({'success': False, 'message': f'Ukuran file melebihi batas server ({MAX_FILE_SIZE // (1024*1024)}MB)'}), 400

    db = get_db()
    
    # Check user limits (same logic as upload)
    if session.get('admin_username') != 'admin':
        user = db.execute('SELECT max_exams, max_pdf_size FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        exam_limit = user['max_exams'] if (user and user['max_exams'] is not None) else 3
        pdf_limit = user['max_pdf_size'] if (user and user['max_pdf_size'] is not None) else 1048576
        
        if len(file_data) > pdf_limit:
            pdf_limit_mb = round(pdf_limit / (1024 * 1024), 2)
            db.close()
            return jsonify({'success': False, 'message': f'Ukuran file melebihi batas akun Anda ({pdf_limit_mb}MB). Silakan hubungi Super Admin.'}), 403
            
        current_count = db.execute('SELECT COUNT(*) as count FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchone()['count']
        if current_count >= exam_limit:
            db.close()
            return jsonify({'success': False, 'message': f'Batas pembuatan ujian tercapai ({exam_limit} ujian). Silakan hubungi Super Admin.'}), 403

    if custom_token:
        if len(custom_token) != 6 or not custom_token.isalnum():
            db.close()
            return jsonify({'success': False, 'message': 'Token kustom harus terdiri dari 6 karakter alfanumerik'}), 400
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (custom_token,)).fetchone()
        if existing:
            db.close()
            return jsonify({'success': False, 'message': 'Token kustom sudah digunakan oleh ujian lain'}), 400
        token = custom_token
    else:
        token = generate_token()

    # Save generated PDF file with secure name
    timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
    filename = f"{timestamp}_editor_exam.pdf"
    file_path = os.path.join(STORAGE_DIR, filename)

    with open(file_path, 'wb') as f:
        f.write(file_data)

    # Insert into exams table
    db.execute(
        'INSERT INTO exams (name, file_path, size_bytes, token, questions_json, status, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)',
        (name, filename, len(file_data), token, questions_json, 'active', session['admin_id'])
    )
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil dibuat dengan token: {token}',
        'token': token
    })






@app.route('/admin/exams/<int:exam_id>/pdf')
@admin_required
def admin_exam_pdf(exam_id):
    """View or download the exam PDF for admin."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return abort(403)
    
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    db.close()

    if not exam:
        return abort(404)

    file_path = os.path.join(STORAGE_DIR, exam['file_path'])
    if not os.path.exists(file_path):
        return abort(404)

    download = request.args.get('download', '0') == '1'
    
    response = send_file(
        file_path, 
        mimetype='application/pdf', 
        as_attachment=download,
        download_name=f"{exam['name']}.pdf" if download else None
    )
    
    if not download:
        response.headers['Content-Disposition'] = f'inline; filename="{exam["name"]}.pdf"'
    
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    response.headers['X-Content-Type-Options'] = 'nosniff'
    return response


@app.route('/admin/api/exams/<int:exam_id>/toggle', methods=['POST'])
@admin_required
def admin_toggle_exam(exam_id):
    """Toggle exam status between active and inactive."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    new_status = 'inactive' if exam['status'] == 'active' else 'active'
    db.execute('UPDATE exams SET status = ? WHERE id = ?', (new_status, exam_id))
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Status ujian diubah ke {new_status}',
        'new_status': new_status
    })


@app.route('/admin/api/exams/<int:exam_id>/toggle-public-results', methods=['POST'])
@admin_required
def admin_toggle_public_results(exam_id):
    """Toggle whether exam results are publicly accessible by students."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    # Toggle public_results: 1 (active) <=> 0 (inactive)
    current = exam['public_results']
    if current is None:
        current = 1
    new_val = 0 if current == 1 else 1
    db.execute('UPDATE exams SET public_results = ? WHERE id = ?', (new_val, exam_id))
    db.commit()
    db.close()

    status_str = 'diaktifkan' if new_val == 1 else 'dinonaktifkan'
    return jsonify({
        'success': True,
        'message': f'Halaman siswa berhasil {status_str}',
        'public_results': new_val
    })


@app.route('/admin/api/exams/<int:exam_id>/toggle-show-answers', methods=['POST'])
@admin_required
def admin_toggle_show_answers(exam_id):
    """Toggle whether correct answer keys are shown to students on the public results page."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    current = exam['show_answers']
    if current is None:
        current = 0
    new_val = 0 if current == 1 else 1
    db.execute('UPDATE exams SET show_answers = ? WHERE id = ?', (new_val, exam_id))
    db.commit()
    db.close()

    status_str = 'ditampilkan' if new_val == 1 else 'disembunyikan'
    return jsonify({
        'success': True,
        'message': f'Kunci jawaban berhasil {status_str} untuk siswa',
        'show_answers': new_val
    })


@app.route('/admin/api/exams/<int:exam_id>/edit', methods=['POST'])
@admin_required
def admin_edit_exam(exam_id):
    """Edit existing exam name and optionally replace its PDF file."""
    name = request.form.get('name', '').strip()
    file = request.files.get('pdf_file')

    if not name:
        return jsonify({'success': False, 'message': 'Nama ujian wajib diisi'}), 400

    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    # If new PDF file is uploaded
    if file and file.filename != '':
        if file.content_type != 'application/pdf':
            db.close()
            return jsonify({'success': False, 'message': 'Hanya file PDF yang diizinkan'}), 400

        file_data = file.read()
        if len(file_data) > MAX_FILE_SIZE:
            db.close()
            return jsonify({
                'success': False,
                'message': f'Ukuran file melebihi batas {MAX_FILE_SIZE // (1024*1024)}MB'
            }), 400

        # Delete old file from storage if exists
        old_file_path = os.path.join(STORAGE_DIR, exam['file_path'])
        if os.path.exists(old_file_path):
            try:
                os.remove(old_file_path)
            except Exception as e:
                app.logger.error(f"Error removing old PDF: {e}")

        # Save new file
        timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
        safe_name = secure_filename(file.filename)
        filename = f"{timestamp}_{safe_name}"
        file_path = os.path.join(STORAGE_DIR, filename)

        with open(file_path, 'wb') as f:
            f.write(file_data)

        # Update database with new name, path, and size
        db.execute(
            'UPDATE exams SET name = ?, file_path = ?, size_bytes = ? WHERE id = ?',
            (name, filename, len(file_data), exam_id)
        )
    else:
        # Just update name
        db.execute(
            'UPDATE exams SET name = ? WHERE id = ?',
            (name, exam_id)
        )

    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil diperbarui'
    })


@app.route('/admin/api/exams/<int:exam_id>', methods=['DELETE'])
@admin_required
def admin_delete_exam(exam_id):
    """Delete an exam and its PDF file."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    # Delete file from storage
    file_path = os.path.join(STORAGE_DIR, exam['file_path'])
    if os.path.exists(file_path):
        os.remove(file_path)

    # Delete from database
    db.execute('DELETE FROM exams WHERE id = ?', (exam_id,))
    db.commit()
    db.close()

    return jsonify({'success': True, 'message': 'Ujian berhasil dihapus'})


@app.route('/admin/exams/bulk-delete', methods=['POST'])
@admin_required
def admin_bulk_delete_exams():
    """Bulk delete exams."""
    data = request.get_json() or {}
    exam_ids = data.get('ids', [])
    if not exam_ids:
        return jsonify({'success': False, 'message': 'Tidak ada ujian yang dipilih'}), 400

    db = get_db()
    deleted_count = 0
    for exam_id in exam_ids:
        # Check ownership
        if not check_exam_ownership(db, exam_id):
            continue
        
        exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
        if not exam:
            continue

        # Delete file from storage
        file_path = os.path.join(STORAGE_DIR, exam['file_path'])
        if os.path.exists(file_path):
            try:
                os.remove(file_path)
            except Exception:
                pass

        # Delete from database
        db.execute('DELETE FROM exams WHERE id = ?', (exam_id,))
        deleted_count += 1

    db.commit()
    db.close()
    return jsonify({'success': True, 'message': f'{deleted_count} ujian berhasil dihapus'})


@app.route('/admin/exams/bulk-toggle', methods=['POST'])
@admin_required
def admin_bulk_toggle_exams():
    """Bulk change exam status (active/inactive)."""
    data = request.get_json() or {}
    exam_ids = data.get('ids', [])
    target_status = data.get('status', 'inactive')
    
    if not exam_ids:
        return jsonify({'success': False, 'message': 'Tidak ada ujian yang dipilih'}), 400
    if target_status not in ['active', 'inactive']:
        return jsonify({'success': False, 'message': 'Status tidak valid'}), 400

    db = get_db()
    updated_count = 0
    for exam_id in exam_ids:
        # Check ownership
        if not check_exam_ownership(db, exam_id):
            continue
        
        db.execute('UPDATE exams SET status = ? WHERE id = ?', (target_status, exam_id))
        updated_count += 1

    db.commit()
    db.close()
    return jsonify({'success': True, 'message': f'Status {updated_count} ujian berhasil diperbarui ke {target_status}'})


@app.route('/admin/api/exams/<int:exam_id>/regenerate-token', methods=['POST'])
@admin_required
def admin_regenerate_token(exam_id):
    """Regenerate token for an exam."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    new_token = generate_token()
    db.execute('UPDATE exams SET token = ? WHERE id = ?', (new_token, exam_id))
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Token ujian berhasil diperbarui: {new_token}',
        'token': new_token
    })


@app.route('/admin/api/exams/<int:exam_id>/custom-token', methods=['POST'])
@admin_required
def admin_custom_token(exam_id):
    """Set custom token for an exam."""
    data = request.get_json() or {}
    custom_token = data.get('token', '').strip().upper()

    if not custom_token:
        return jsonify({'success': False, 'message': 'Token kustom tidak boleh kosong'}), 400

    if len(custom_token) != 6 or not custom_token.isalnum():
        return jsonify({'success': False, 'message': 'Token kustom harus terdiri dari 6 karakter alfanumerik'}), 400

    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    # Check if this token is already in use by another exam
    existing = db.execute('SELECT id FROM exams WHERE token = ? AND id != ?', (custom_token, exam_id)).fetchone()
    if existing:
        db.close()
        return jsonify({'success': False, 'message': 'Token kustom sudah digunakan oleh ujian lain'}), 400

    db.execute('UPDATE exams SET token = ? WHERE id = ?', (custom_token, exam_id))
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Token ujian berhasil diubah menjadi: {custom_token}',
        'token': custom_token
    })



@app.route('/admin/api/stats')
@admin_required
def admin_stats():
    """Get dashboard statistics."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == 'admin')
    if is_super_admin:
        exams = db.execute('SELECT status, size_bytes FROM exams').fetchall()
    else:
        exams = db.execute('SELECT status, size_bytes FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchall()
    db.close()

    total = len(exams)
    active = sum(1 for e in exams if e['status'] == 'active')

    if is_super_admin:
        storage_bytes = get_storage_stats()
    else:
        storage_bytes = sum(e['size_bytes'] for e in exams if e['size_bytes'] is not None)

    return jsonify({
        'success': True,
        'data': {
            'total': total,
            'active': active,
            'inactive': total - active,
            'storage_mb': round(storage_bytes / (1024 * 1024), 2),
            'local_ip': get_network_info()['display_host']
        }
    })


@app.route('/admin/api/change-password', methods=['POST'])
@admin_required
def admin_change_password():
    """Change logged in admin/user password."""
    data = request.json or {}
    current_password = data.get('current_password', '')
    new_password = data.get('new_password', '')

    if not current_password or not new_password:
        return jsonify({'success': False, 'message': 'Semua field password wajib diisi'}), 400

    db = get_db()
    curr_hash = hashlib.sha256(current_password.encode()).hexdigest()
    user = db.execute(
        'SELECT id FROM admin_users WHERE id = ? AND password_hash = ?',
        (session['admin_id'], curr_hash)
    ).fetchone()

    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'Password saat ini salah'}), 400

    new_hash = hashlib.sha256(new_password.encode()).hexdigest()
    db.execute(
        'UPDATE admin_users SET password_hash = ? WHERE id = ?',
        (new_hash, session['admin_id'])
    )
    db.commit()
    db.close()

    return jsonify({'success': True, 'message': 'Password berhasil diperbarui'})


@app.route('/admin/users')
@super_admin_required
def admin_manage_users_page():
    """HTML page for super admin to manage other users (teachers) and set limits."""
    return render_template(
        'users.html',
        admin_user=session.get('admin_username', 'Admin'),
        active_page='users'
    )


@app.route('/admin/api/users', methods=['GET'])
@super_admin_required
def admin_list_users():
    """List all registered users (teachers) with exam count and limit."""
    db = get_db()
    users = db.execute('SELECT id, username, whatsapp_number, status, max_exams, max_pdf_size, expires_at, created_at FROM admin_users ORDER BY username ASC').fetchall()
    
    user_list = []
    for u in users:
        # Count exams created by this user
        count = db.execute('SELECT COUNT(*) as count FROM exams WHERE created_by = ?', (u['id'],)).fetchone()['count']
        user_list.append({
            'id': u['id'],
            'username': u['username'],
            'whatsapp_number': u['whatsapp_number'] or '',
            'status': u['status'] or 'active',
            'max_exams': u['max_exams'] if u['max_exams'] is not None else 3,
            'max_pdf_size': u['max_pdf_size'] if u['max_pdf_size'] is not None else 1048576,
            'expires_at': u['expires_at'] or '',
            'exam_count': count,
            'created_at': format_iso_utc(u['created_at'])
        })
    db.close()
    return jsonify({
        'success': True,
        'users': user_list
    })


@app.route('/admin/api/users', methods=['POST'])
@super_admin_required
def admin_create_user():
    """Create a new user (teacher) with exam limit."""
    data = request.json or {}
    username = data.get('username', '').strip().lower()
    password = data.get('password', '')
    whatsapp = data.get('whatsapp_number', '').strip()
    max_exams = data.get('max_exams', 3)
    max_pdf_size_mb = data.get('max_pdf_size_mb', 1)

    try:
        max_exams = int(max_exams)
    except (ValueError, TypeError):
        max_exams = 3

    try:
        max_pdf_size_mb = float(max_pdf_size_mb)
    except (ValueError, TypeError):
        max_pdf_size_mb = 1
    max_pdf_size = int(max_pdf_size_mb * 1024 * 1024)

    if not username or not password:
        return jsonify({'success': False, 'message': 'Username dan password wajib diisi'}), 400

    if username == 'admin':
        return jsonify({'success': False, 'message': 'Username "admin" sudah terdaftar sebagai Super Admin'}), 400

    db = get_db()
    existing = db.execute('SELECT id FROM admin_users WHERE username = ?', (username,)).fetchone()
    if existing:
        db.close()
        return jsonify({'success': False, 'message': 'Username sudah digunakan'}), 400

    pw_hash = hashlib.sha256(password.encode()).hexdigest()
    expires_at = data.get('expires_at', '').strip()
    if not expires_at:
        from datetime import timedelta
        default_active_days = int(get_saas_setting('default_active_days', '1'))
        expires_at = (datetime.now(timezone.utc) + timedelta(days=default_active_days)).strftime('%Y-%m-%d %H:%M:%S')

    db.execute(
        'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, max_exams, max_pdf_size, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)',
        (username, pw_hash, whatsapp, 'active', max_exams, max_pdf_size, expires_at)
    )
    db.commit()
    db.close()

    return jsonify({'success': True, 'message': f'User "{username}" berhasil dibuat'})


@app.route('/admin/api/users/<int:user_id>/edit', methods=['POST'])
@super_admin_required
def admin_edit_user(user_id):
    """Edit user's max_exams limit, whatsapp, status, and optionally their password."""
    data = request.json or {}
    max_exams = data.get('max_exams')
    max_pdf_size_mb = data.get('max_pdf_size_mb')
    whatsapp = data.get('whatsapp_number')
    status = data.get('status')
    password = data.get('password', '').strip()

    db = get_db()
    user = db.execute('SELECT username FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'User tidak ditemukan'}), 404

    if user['username'] == 'admin':
        db.close()
        return jsonify({'success': False, 'message': 'Super Admin "admin" tidak dapat diubah limitnya'}), 400

    if max_exams is not None:
        try:
            max_exams = int(max_exams)
        except (ValueError, TypeError):
            db.close()
            return jsonify({'success': False, 'message': 'Limit ujian harus berupa angka valid'}), 400
        db.execute('UPDATE admin_users SET max_exams = ? WHERE id = ?', (max_exams, user_id))

    if max_pdf_size_mb is not None:
        try:
            max_pdf_size_mb = float(max_pdf_size_mb)
        except (ValueError, TypeError):
            db.close()
            return jsonify({'success': False, 'message': 'Limit ukuran PDF harus berupa angka valid'}), 400
        max_pdf_size = int(max_pdf_size_mb * 1024 * 1024)
        db.execute('UPDATE admin_users SET max_pdf_size = ? WHERE id = ?', (max_pdf_size, user_id))

    if whatsapp is not None:
        db.execute('UPDATE admin_users SET whatsapp_number = ? WHERE id = ?', (whatsapp.strip(), user_id))

    if status is not None:
        if status in ['active', 'suspended', 'pending_otp']:
            db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (status, user_id))

    if password:
        pw_hash = hashlib.sha256(password.encode()).hexdigest()
        db.execute('UPDATE admin_users SET password_hash = ? WHERE id = ?', (pw_hash, user_id))

    if 'expires_at' in data:
        exp_val = data.get('expires_at')
        if exp_val:
            if ' ' not in exp_val:
                exp_val = f"{exp_val} 23:59:59"
            db.execute('UPDATE admin_users SET expires_at = ? WHERE id = ?', (exp_val, user_id))
        else:
            db.execute('UPDATE admin_users SET expires_at = NULL WHERE id = ?', (user_id,))

    db.commit()
    db.close()

    return jsonify({'success': True, 'message': f'Pengaturan user "{user["username"]}" berhasil diperbarui'})


@app.route('/admin/api/users/<int:user_id>/verify', methods=['POST'])
@super_admin_required
def admin_verify_user_manual(user_id):
    """Manually activate/verify a pending user."""
    db = get_db()
    user = db.execute('SELECT username, status FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'User tidak ditemukan'}), 404
        
    db.execute('UPDATE admin_users SET status = ?, otp_code = NULL, otp_expiry = NULL WHERE id = ?', ('active', user_id))
    db.commit()
    db.close()
    return jsonify({'success': True, 'message': f'User "{user["username"]}" berhasil diaktifkan secara manual'})


@app.route('/admin/api/users/<int:user_id>/toggle-status', methods=['POST'])
@super_admin_required
def admin_toggle_user_status(user_id):
    """Suspend or activate a user account."""
    db = get_db()
    user = db.execute('SELECT username, status FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'User tidak ditemukan'}), 404
        
    if user['username'] == 'admin':
        db.close()
        return jsonify({'success': False, 'message': 'Status Super Admin "admin" tidak dapat diubah'}), 400
        
    new_status = 'suspended' if user['status'] == 'active' else 'active'
    db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (new_status, user_id))
    db.commit()
    db.close()
    return jsonify({'success': True, 'message': f'Status user "{user["username"]}" berhasil diubah menjadi {new_status}'})


@app.route('/admin/api/saas-settings', methods=['GET', 'POST'])
@super_admin_required
def admin_saas_settings():
    """Get or update SaaS settings (WhatsApp verification gateway configs, default limits)."""
    if request.method == 'POST':
        data = request.json or {}
        wa_enabled = '1' if data.get('wa_verification_enabled') else '0'
        wa_token = data.get('wa_api_token', '').strip()
        wa_template = data.get('wa_otp_template', '').strip()
        default_exams = data.get('default_max_exams', '3')
        default_pdf_size_mb = data.get('default_max_pdf_size_mb', '1')
        default_active_days = data.get('default_active_days', '1')
        android_version = data.get('android_version', '2.1.3').strip()
        webapp_version = data.get('webapp_version', '2.1.3').strip()
        
        try:
            default_exams = int(default_exams)
        except (ValueError, TypeError):
            default_exams = 3
            
        try:
            default_pdf_size_mb = float(default_pdf_size_mb)
        except (ValueError, TypeError):
            default_pdf_size_mb = 1.0
            
        default_pdf_size = int(default_pdf_size_mb * 1024 * 1024)

        try:
            default_active_days = int(default_active_days)
        except (ValueError, TypeError):
            default_active_days = 1
        
        set_saas_setting('wa_verification_enabled', wa_enabled)
        set_saas_setting('wa_api_token', wa_token)
        if wa_template:
            set_saas_setting('wa_otp_template', wa_template)
        set_saas_setting('default_max_exams', str(default_exams))
        set_saas_setting('default_max_pdf_size', str(default_pdf_size))
        set_saas_setting('default_active_days', str(default_active_days))
        set_saas_setting('android_version', android_version)
        set_saas_setting('webapp_version', webapp_version)
        
        return jsonify({'success': True, 'message': 'Pengaturan SaaS berhasil diperbarui'})
        
    # GET settings
    settings = {
        'wa_verification_enabled': get_saas_setting('wa_verification_enabled', '0') == '1',
        'wa_api_token': get_saas_setting('wa_api_token', ''),
        'wa_otp_template': get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.'),
        'default_max_exams': int(get_saas_setting('default_max_exams', '3')),
        'default_max_pdf_size_mb': round(int(get_saas_setting('default_max_pdf_size', '1048576')) / (1024*1024), 2),
        'default_active_days': int(get_saas_setting('default_active_days', '1')),
        'android_version': get_saas_setting('android_version', '2.1.3'),
        'webapp_version': get_saas_setting('webapp_version', '2.1.3')
    }
    return jsonify({'success': True, 'settings': settings})


@app.route('/admin/api/users/<int:user_id>', methods=['DELETE'])
@super_admin_required
def admin_delete_user(user_id):
    """Delete a user and all their exams/files."""
    db = get_db()
    user = db.execute('SELECT username FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        db.close()
        return jsonify({'success': False, 'message': 'User tidak ditemukan'}), 404

    if user['username'] == 'admin':
        db.close()
        return jsonify({'success': False, 'message': 'Super Admin "admin" tidak dapat dihapus'}), 400

    # Delete exams and PDF files owned by this user
    exams = db.execute('SELECT file_path FROM exams WHERE created_by = ?', (user_id,)).fetchall()
    for e in exams:
        file_path = os.path.join(STORAGE_DIR, e['file_path'])
        if os.path.exists(file_path):
            try:
                os.remove(file_path)
            except Exception:
                pass

    db.execute('DELETE FROM exams WHERE created_by = ?', (user_id,))
    db.execute('DELETE FROM admin_users WHERE id = ?', (user_id,))
    db.commit()
    db.close()

    return jsonify({'success': True, 'message': f'User "{user["username"]}" beserta seluruh soalnya berhasil dihapus'})


@app.route('/admin/api/exams/<int:exam_id>/questions', methods=['GET', 'POST'])
@admin_required
def admin_exam_questions(exam_id):
    """Get or save questions configuration for an exam."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke ujian ini'}), 403
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    if request.method == 'GET':
        questions_raw = exam['questions_json']
        security_level = exam['security_level'] or 'medium'
        questions = []
        if questions_raw:
            try:
                questions = json.loads(questions_raw)
            except Exception:
                pass
        db.close()
        return jsonify({'success': True, 'questions': questions, 'security_level': security_level})

    else:
        # POST: Save questions configuration
        data = request.json or {}
        questions = data.get('questions', [])
        security_level = data.get('security_level', 'medium')

        if security_level not in ['medium', 'low']:
            security_level = 'medium'

        # Basic validation
        if not isinstance(questions, list):
            db.close()
            return jsonify({'success': False, 'message': 'Format data pertanyaan tidak valid'}), 400

        # Save to database
        db.execute(
            'UPDATE exams SET questions_json = ?, security_level = ? WHERE id = ?',
            (json.dumps(questions), security_level, exam_id)
        )
        
        # Recalculate scores for all existing submissions of this exam
        submissions = db.execute('SELECT id, answers_json FROM submissions WHERE exam_id = ?', (exam_id,)).fetchall()
        for sub in submissions:
            try:
                sub_answers = json.loads(sub['answers_json']) if sub['answers_json'] else {}
            except Exception:
                sub_answers = {}
            
            new_score = calculate_submission_score(sub_answers, questions)
            db.execute('UPDATE submissions SET score = ? WHERE id = ?', (new_score, sub['id']))
            
        db.commit()
        db.close()
        return jsonify({'success': True, 'message': 'Konfigurasi soal berhasil disimpan dan nilai siswa berhasil diperbarui'})


@app.route('/admin/submissions')
@admin_required
def admin_submissions():
    """Submissions overview page for admin."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == 'admin')
    
    if is_super_admin:
        submissions = db.execute(
            'SELECT s.*, e.name as exam_name '
            'FROM submissions s JOIN exams e ON s.exam_id = e.id '
            'ORDER BY s.created_at DESC'
        ).fetchall()
        exams = db.execute('SELECT id, name FROM exams ORDER BY created_at DESC').fetchall()
    else:
        submissions = db.execute(
            'SELECT s.*, e.name as exam_name '
            'FROM submissions s JOIN exams e ON s.exam_id = e.id '
            'WHERE e.created_by = ? '
            'ORDER BY s.created_at DESC',
            (session['admin_id'],)
        ).fetchall()
        exams = db.execute('SELECT id, name FROM exams WHERE created_by = ? ORDER BY created_at DESC', (session['admin_id'],)).fetchall()
        
    local_ip = get_network_info()['display_host']
    db.close()
    
    return render_template(
        'submissions.html',
        submissions=submissions,
        exams=exams,
        local_ip=local_ip,
        admin_user=session.get('admin_username', 'Admin'),
        active_page='submissions'
    )


@app.route('/admin/api/submissions/<int:submission_id>/detail')
@admin_required
def admin_submission_detail(submission_id):
    """Get detailed student answers compared with keys."""
    db = get_db()
    if not check_submission_ownership(db, submission_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke data ini'}), 403
    sub = db.execute(
        'SELECT s.*, e.name as exam_name, e.questions_json '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id '
        'WHERE s.id = ?', (submission_id,)
    ).fetchone()
    db.close()
    
    if not sub:
        return jsonify({'success': False, 'message': 'Hasil ujian tidak ditemukan'}), 404
        
    try:
        answers = json.loads(sub['answers_json'])
    except Exception:
        answers = {}
        
    try:
        questions = json.loads(sub['questions_json']) if sub['questions_json'] else []
    except Exception:
        questions = []
        
    return jsonify({
        'success': True,
        'submission_id': sub['id'],
        'student_name': sub['student_name'],
        'exam_number': sub['exam_number'],
        'student_class': sub['student_class'],
        'exam_name': sub['exam_name'],
        'score': sub['score'],
        'start_time': format_iso_utc(sub['start_time']) if sub['start_time'] else None,
        'mac_address': sub['mac_address'],
        'created_at': format_iso_utc(sub['created_at']),
        'answers': answers,
        'questions': questions
    })


@app.route('/admin/api/submissions/<int:submission_id>/export_detail')
@admin_required
def admin_export_submission_detail(submission_id):
    """Export a single student's detailed answers to CSV."""
    db = get_db()
    if not check_submission_ownership(db, submission_id):
        db.close()
        return abort(403)
    sub = db.execute(
        'SELECT s.*, e.name as exam_name, e.questions_json '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id '
        'WHERE s.id = ?', (submission_id,)
    ).fetchone()
    db.close()
    
    if not sub:
        return abort(404)
        
    try:
        answers = json.loads(sub['answers_json'])
    except Exception:
        answers = {}
        
    try:
        questions = json.loads(sub['questions_json']) if sub['questions_json'] else []
    except Exception:
        questions = []

    tz_offset = request.args.get('tz_offset', type=int)

    # Generate CSV in memory
    si = io.StringIO()
    cw = csv.writer(si)
    cw.writerow(['Detail Hasil Ujian Siswa'])
    cw.writerow(['Nama Ujian', sub['exam_name']])
    cw.writerow(['Nama Siswa', sub['student_name']])
    cw.writerow(['Nomor Ujian', sub['exam_number']])
    cw.writerow(['Kelas', sub['student_class']])
    cw.writerow(['Nilai Akhir', sub['score'] if sub['score'] is not None else 'Belum Dinilai'])
    cw.writerow(['Waktu Mulai', localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—'])
    cw.writerow(['Waktu Kumpul', localize_date_string(sub['created_at'], tz_offset)])
    cw.writerow(['MAC Address / ID Perangkat', sub['mac_address'] or '—'])
    cw.writerow([])
    cw.writerow(['No. Soal', 'Tipe Soal', 'Bobot Maks', 'Jawaban Siswa', 'Kunci Jawaban', 'Status', 'Poin Didapat'])
    
    for q in questions:
        # Normalize q_num: handle potential float representation (e.g. 1.0 -> "1")
        try:
            num_val = float(q['number'])
            q_num = str(int(num_val)) if num_val.is_integer() else str(q['number'])
        except Exception:
            q_num = str(q['number'])
        student_ans = answers.get(q_num)
        correct_ans = q.get('key')
        q_weight = float(q.get('weight', 1.0))
        partial_scoring = q.get('partial_scoring', False)
        
        # Calculate score status and points earned
        earned_q_weight = 0.0
        status_text = 'Salah ❌'
        
        # Student Answer Formatting
        if student_ans is not None:
            if q['type'] in ['single_choice', 'true_false', 'short_answer']:
                s_norm = ' '.join(str(student_ans).split()).upper()
                c_norm = ' '.join(str(correct_ans).split()).upper()
                if s_norm == c_norm:
                    earned_q_weight = q_weight
                    status_text = 'Benar ✔️'
            elif q['type'] == 'multiple_choice':
                if isinstance(student_ans, list) and isinstance(correct_ans, list):
                    if partial_scoring:
                        correct_set = set(str(x).upper() for x in correct_ans)
                        student_set = set(str(x).upper() for x in student_ans)
                        if correct_set:
                            correct_selected = sum(1 for x in student_set if x in correct_set)
                            incorrect_selected = sum(1 for x in student_set if x not in correct_set)
                            portion = max(0.0, (correct_selected - incorrect_selected) / len(correct_set))
                            earned_q_weight = portion * q_weight
                            if portion == 1.0:
                                status_text = 'Benar ✔️'
                            elif portion > 0.0:
                                status_text = 'Parsial ⚠️'
                            else:
                                status_text = 'Salah ❌'
                    else:
                        if sorted([str(x).upper() for x in student_ans]) == sorted([str(x).upper() for x in correct_ans]):
                            earned_q_weight = q_weight
                            status_text = 'Benar ✔️'
            elif q['type'] == 'matching':
                if isinstance(student_ans, dict) and isinstance(correct_ans, dict):
                    if partial_scoring:
                        if correct_ans:
                            correct_matches = 0
                            for k, v in correct_ans.items():
                                if str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper():
                                    correct_matches += 1
                            portion = correct_matches / len(correct_ans)
                            earned_q_weight = portion * q_weight
                            if portion == 1.0:
                                status_text = 'Benar ✔️'
                            elif portion > 0.0:
                                status_text = 'Parsial ⚠️'
                            else:
                                status_text = 'Salah ❌'
                    else:
                        match = True
                        for k, v in correct_ans.items():
                            if str(student_ans.get(k, '')).strip().upper() != str(v).strip().upper():
                                match = False
                                break
                        if match:
                            earned_q_weight = q_weight
                            status_text = 'Benar ✔️'
        
        # Format student answer to string
        student_ans_str = ''
        if isinstance(student_ans, list):
            student_ans_str = ', '.join(student_ans)
        elif isinstance(student_ans, dict):
            student_ans_str = ', '.join([f"{k}:{v}" for k, v in student_ans.items()])
        elif student_ans is not None:
            student_ans_str = str(student_ans)
            
        # Format correct answer to string
        correct_ans_str = ''
        if isinstance(correct_ans, list):
            correct_ans_str = ', '.join(correct_ans)
        elif isinstance(correct_ans, dict):
            correct_ans_str = ', '.join([f"{k}:{v}" for k, v in correct_ans.items()])
        elif correct_ans is not None:
            correct_ans_str = str(correct_ans)
            
        type_labels = {
            'single_choice': 'Pilihan Ganda',
            'multiple_choice': 'PG Kompleks',
            'true_false': 'Benar / Salah',
            'matching': 'Menjodohkan',
            'short_answer': 'Isian Singkat',
        }
        cw.writerow([
            q['number'],
            type_labels.get(q['type'], q['type']),
            q_weight,
            student_ans_str,
            correct_ans_str,
            status_text,
            round(earned_q_weight, 2)
        ])
        
    output = si.getvalue()
    si.close()
    
    filename = f"detail_jawaban_{sub['student_name'].replace(' ', '_')}_{sub['exam_number']}.csv"
    response = send_file(
        io.BytesIO(output.encode('utf-8-sig')),
        mimetype='text/csv',
        as_attachment=True,
        download_name=filename
    )
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    return response


@app.route('/admin/api/submissions/<int:submission_id>', methods=['DELETE'])
@admin_required
def admin_delete_submission(submission_id):
    """Delete a student submission."""
    db = get_db()
    if not check_submission_ownership(db, submission_id):
        db.close()
        return jsonify({'success': False, 'message': 'Akses ditolak: Anda tidak memiliki akses ke data ini'}), 403
    db.execute('DELETE FROM submissions WHERE id = ?', (submission_id,))
    db.commit()
    db.close()
    return jsonify({'success': True, 'message': 'Hasil ujian berhasil dihapus'})


@app.route('/admin/api/submissions/export')
@admin_required
def admin_export_submissions():
    """Export student submissions. Generates multi-sheet XLSX for specific exam, CSV for all."""
    exam_id = request.args.get('exam_id')
    tz_offset = request.args.get('tz_offset', type=int)

    db = get_db()
    is_super_admin = (session.get('admin_username') == 'admin')

    # --- Multi-sheet XLSX export for a specific exam ---
    if exam_id:
        # Verify ownership
        exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
        if not exam:
            db.close()
            return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404
        if not is_super_admin and exam['created_by'] != session['admin_id']:
            db.close()
            return jsonify({'success': False, 'message': 'Akses ditolak'}), 403

        submissions = db.execute(
            'SELECT * FROM submissions WHERE exam_id = ? ORDER BY student_class, student_name',
            (exam_id,)
        ).fetchall()
        db.close()

        try:
            questions = json.loads(exam['questions_json']) if exam['questions_json'] else []
        except Exception:
            questions = []

        return _generate_exam_xlsx(exam, submissions, questions, tz_offset)

    # --- Fallback: CSV export for all exams ---
    query = (
        'SELECT s.id, e.name as exam_name, s.student_name, s.exam_number, s.student_class, s.score, s.start_time, s.mac_address, s.created_at '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id'
    )
    conditions = []
    params = []

    if not is_super_admin:
        conditions.append('e.created_by = ?')
        params.append(session['admin_id'])

    if conditions:
        query += ' WHERE ' + ' AND '.join(conditions)

    query += ' ORDER BY s.created_at DESC'
    submissions = db.execute(query, params).fetchall()
    db.close()

    si = io.StringIO()
    cw = csv.writer(si)
    cw.writerow(['ID', 'Nama Ujian', 'Nama Siswa', 'Nomor Ujian', 'Kelas', 'Nilai', 'Waktu Mulai', 'Waktu Kumpul', 'MAC/ID Perangkat'])

    for row in submissions:
        cw.writerow([
            row['id'],
            row['exam_name'],
            row['student_name'],
            row['exam_number'],
            row['student_class'],
            row['score'] if row['score'] is not None else 'Belum Dinilai',
            localize_date_string(row['start_time'], tz_offset) if row['start_time'] else '—',
            localize_date_string(row['created_at'], tz_offset),
            row['mac_address'] or '—'
        ])

    output = si.getvalue()
    si.close()

    filename = f"hasil_ujian_{datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')}.csv"
    response = send_file(
        io.BytesIO(output.encode('utf-8-sig')),
        mimetype='text/csv',
        as_attachment=True,
        download_name=filename
    )
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    return response


def _generate_exam_xlsx(exam, submissions, questions, tz_offset=None):
    """Generate a professionally styled multi-sheet Excel workbook for a specific exam."""
    from openpyxl import Workbook
    from openpyxl.styles import Font, Alignment, PatternFill, Border, Side
    from openpyxl.utils import get_column_letter

    wb = Workbook()

    # ── Colour palette ──
    NAVY = '1e3a8a'
    WHITE = 'ffffff'
    LIGHT_BLUE = 'dbeafe'
    GREEN_BG = 'd1fae5'
    GREEN_FG = '065f46'
    RED_BG = 'fee2e2'
    RED_FG = '991b1b'
    YELLOW_BG = 'fef3c7'
    YELLOW_FG = '92400e'
    GRAY_BG = 'f3f4f6'
    GRAY_FG = '374151'

    # ── Reusable styles ──
    header_font = Font(bold=True, color=WHITE, size=11)
    header_fill = PatternFill(start_color=NAVY, end_color=NAVY, fill_type='solid')
    header_align = Alignment(horizontal='center', vertical='center', wrap_text=True)
    thin_border = Border(
        left=Side(style='thin', color='d1d5db'),
        right=Side(style='thin', color='d1d5db'),
        top=Side(style='thin', color='d1d5db'),
        bottom=Side(style='thin', color='d1d5db'),
    )
    center_align = Alignment(horizontal='center', vertical='center')
    left_align = Alignment(horizontal='left', vertical='center', wrap_text=True)
    title_font = Font(bold=True, size=14, color=NAVY)
    meta_label_font = Font(bold=True, size=11)
    meta_value_font = Font(size=11)

    def style_header_row(ws, row, col_count):
        for col_idx in range(1, col_count + 1):
            cell = ws.cell(row=row, column=col_idx)
            cell.font = header_font
            cell.fill = header_fill
            cell.alignment = header_align
            cell.border = thin_border

    def style_data_cell(ws, row, col, align='center'):
        cell = ws.cell(row=row, column=col)
        cell.border = thin_border
        cell.alignment = center_align if align == 'center' else left_align
        return cell

    def _sanitize_sheet_name(name):
        """Sanitize sheet name for Excel (max 31 chars, no invalid chars)."""
        for ch in ['\\', '/', '?', '*', ':', '[', ']']:
            name = name.replace(ch, '')
        return name[:31] if name else 'Sheet'

    # ══════════════════════════════════════════════
    # SHEET 1: RINGKASAN HASIL
    # ══════════════════════════════════════════════
    ws_summary = wb.active
    ws_summary.title = 'Ringkasan Hasil'
    ws_summary.sheet_properties.tabColor = NAVY

    # Title
    ws_summary.merge_cells('A1:G1')
    title_cell = ws_summary['A1']
    title_cell.value = f'RINGKASAN HASIL UJIAN — {exam["name"]}'
    title_cell.font = title_font
    title_cell.alignment = Alignment(horizontal='left', vertical='center')

    # Metadata
    meta_data = [
        ('Token Ujian:', exam['token'] or '—'),
        ('Total Peserta:', str(len(submissions))),
        ('Tanggal Export:', localize_date_string(datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S'), tz_offset)),
    ]
    for i, (label, value) in enumerate(meta_data):
        ws_summary.cell(row=3 + i, column=1, value=label).font = meta_label_font
        ws_summary.cell(row=3 + i, column=2, value=value).font = meta_value_font

    # Summary table header
    summary_headers = ['No.', 'Nomor Ujian', 'Nama Siswa', 'Kelas', 'Nilai Akhir', 'Status', 'Waktu Mulai', 'Waktu Pengumpulan', 'MAC/ID Perangkat']
    header_row = 7
    for col_idx, h in enumerate(summary_headers, 1):
        ws_summary.cell(row=header_row, column=col_idx, value=h)
    style_header_row(ws_summary, header_row, len(summary_headers))

    # Summary table data
    for i, sub in enumerate(submissions):
        row_num = header_row + 1 + i
        score = sub['score']
        score_display = round(score, 2) if score is not None else 'Belum Dinilai'
        status = 'Sudah Dinilai' if score is not None else 'Belum Dinilai'

        values = [i + 1, sub['exam_number'], sub['student_name'], sub['student_class'],
                  score_display, status,
                  localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—',
                  localize_date_string(sub['created_at'], tz_offset),
                  sub['mac_address'] or '—']
        for col_idx, val in enumerate(values, 1):
            cell = style_data_cell(ws_summary, row_num, col_idx,
                                   'center' if col_idx in [1, 4, 5, 6] else 'left')
            cell.value = val

            # Colour score column
            if col_idx == 5 and score is not None:
                cell.font = Font(bold=True)
            if col_idx == 6:
                if score is not None:
                    cell.fill = PatternFill(start_color=GREEN_BG, end_color=GREEN_BG, fill_type='solid')
                    cell.font = Font(color=GREEN_FG, bold=True)
                else:
                    cell.fill = PatternFill(start_color=GRAY_BG, end_color=GRAY_BG, fill_type='solid')
                    cell.font = Font(color=GRAY_FG)

    # Auto-width columns
    for col_idx in range(1, len(summary_headers) + 1):
        ws_summary.column_dimensions[get_column_letter(col_idx)].width = \
            max(14, len(summary_headers[col_idx - 1]) + 6)
    ws_summary.column_dimensions['C'].width = 28
    ws_summary.column_dimensions['G'].width = 22
    ws_summary.column_dimensions['H'].width = 22
    ws_summary.column_dimensions['I'].width = 24

    # ══════════════════════════════════════════════
    # PER-STUDENT SHEETS
    # ══════════════════════════════════════════════
    used_names = set()
    for sub in submissions:
        # Build unique sheet name
        base_name = f"{sub['exam_number']} {sub['student_name']}"
        sheet_name = _sanitize_sheet_name(base_name)
        # Handle duplicates
        if sheet_name in used_names:
            counter = 2
            while f"{sheet_name[:28]}_{counter}" in used_names:
                counter += 1
            sheet_name = f"{sheet_name[:28]}_{counter}"
        used_names.add(sheet_name)

        ws = wb.create_sheet(title=sheet_name)

        # Student info header
        ws.merge_cells('A1:G1')
        ws['A1'].value = 'DETAIL HASIL UJIAN SISWA'
        ws['A1'].font = title_font
        ws['A1'].alignment = Alignment(horizontal='left', vertical='center')

        info_rows = [
            ('Nama Siswa:', sub['student_name']),
            ('Nomor Ujian:', sub['exam_number']),
            ('Kelas:', sub['student_class']),
            ('Nilai Akhir:', round(sub['score'], 2) if sub['score'] is not None else 'Belum Dinilai'),
            ('Waktu Mulai:', localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—'),
            ('Waktu Pengumpulan:', localize_date_string(sub['created_at'], tz_offset)),
            ('MAC/ID Perangkat:', sub['mac_address'] or '—'),
        ]
        for i, (label, value) in enumerate(info_rows):
            ws.cell(row=3 + i, column=1, value=label).font = meta_label_font
            val_cell = ws.cell(row=3 + i, column=2, value=value)
            val_cell.font = meta_value_font
            if label == 'Nilai Akhir:' and sub['score'] is not None:
                val_cell.font = Font(bold=True, size=12, color=NAVY)

        # Question detail table
        detail_headers = ['No. Soal', 'Tipe Soal', 'Bobot Maks', 'Jawaban Siswa',
                          'Kunci Jawaban', 'Status', 'Poin Didapat']
        detail_header_row = 11
        for col_idx, h in enumerate(detail_headers, 1):
            ws.cell(row=detail_header_row, column=col_idx, value=h)
        style_header_row(ws, detail_header_row, len(detail_headers))

        try:
            student_answers = json.loads(sub['answers_json']) if sub['answers_json'] else {}
        except Exception:
            student_answers = {}

        data_row = detail_header_row + 1
        total_earned = 0.0
        total_max = 0.0

        for q in questions:
            # Normalize q_num: handle potential float representation (e.g. 1.0 -> "1")
            try:
                num_val = float(q['number'])
                q_num = str(int(num_val)) if num_val.is_integer() else str(q['number'])
            except Exception:
                q_num = str(q['number'])
            student_ans = student_answers.get(q_num)
            correct_ans = q.get('key')
            q_weight = float(q.get('weight', 1.0))
            partial_scoring = q.get('partial_scoring', False)
            total_max += q_weight

            # --- Evaluate ---
            earned = 0.0
            status_text = 'Belum Dijawab'

            # Format display strings
            def _fmt(val):
                if val is None:
                    return '—'
                if isinstance(val, list):
                    return ', '.join(str(x) for x in val)
                if isinstance(val, dict):
                    return ', '.join(f'{k} ➔ {v}' for k, v in val.items())
                return str(val)

            student_display = _fmt(student_ans) if student_ans is not None and student_ans != '' else '—'
            key_display = _fmt(correct_ans)

            if student_ans is not None and student_ans != '' and correct_ans is not None:
                if q['type'] in ['single_choice', 'true_false', 'short_answer']:
                    s_norm = ' '.join(str(student_ans).split()).upper()
                    c_norm = ' '.join(str(correct_ans).split()).upper()
                    if s_norm == c_norm:
                        earned = q_weight
                        status_text = 'Benar ✔️'
                    else:
                        status_text = 'Salah ❌'
                elif q['type'] == 'multiple_choice':
                    if isinstance(student_ans, list) and isinstance(correct_ans, list):
                        if partial_scoring:
                            cs = set(str(x).upper() for x in correct_ans)
                            ss = set(str(x).upper() for x in student_ans)
                            if cs:
                                cc = sum(1 for x in ss if x in cs)
                                ic = sum(1 for x in ss if x not in cs)
                                portion = max(0.0, (cc - ic) / len(cs))
                                earned = portion * q_weight
                                status_text = 'Benar ✔️' if portion >= 1.0 else ('Parsial ⚠️' if portion > 0 else 'Salah ❌')
                        else:
                            if sorted(str(x).upper() for x in student_ans) == sorted(str(x).upper() for x in correct_ans):
                                earned = q_weight
                                status_text = 'Benar ✔️'
                            else:
                                status_text = 'Salah ❌'
                elif q['type'] == 'matching':
                    if isinstance(student_ans, dict) and isinstance(correct_ans, dict):
                        if partial_scoring:
                            if correct_ans:
                                cm = sum(1 for k, v in correct_ans.items()
                                         if str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper())
                                portion = cm / len(correct_ans)
                                earned = portion * q_weight
                                status_text = 'Benar ✔️' if portion >= 1.0 else ('Parsial ⚠️' if portion > 0 else 'Salah ❌')
                        else:
                            match = all(
                                str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper()
                                for k, v in correct_ans.items()
                            )
                            if match:
                                earned = q_weight
                                status_text = 'Benar ✔️'
                            else:
                                status_text = 'Salah ❌'

            total_earned += earned

            # Type label mapping
            type_labels = {
                'single_choice': 'Pilihan Ganda',
                'multiple_choice': 'PG Kompleks',
                'true_false': 'Benar / Salah',
                'matching': 'Menjodohkan',
                'short_answer': 'Isian Singkat',
            }

            row_values = [
                q['number'],
                type_labels.get(q['type'], q['type']),
                q_weight,
                student_display,
                key_display,
                status_text,
                round(earned, 2),
            ]
            for col_idx, val in enumerate(row_values, 1):
                cell = style_data_cell(ws, data_row, col_idx,
                                       'center' if col_idx in [1, 3, 6, 7] else 'left')
                cell.value = val

                # Colour status column
                if col_idx == 6:
                    if 'Benar' in str(val):
                        cell.fill = PatternFill(start_color=GREEN_BG, end_color=GREEN_BG, fill_type='solid')
                        cell.font = Font(color=GREEN_FG, bold=True)
                    elif 'Parsial' in str(val):
                        cell.fill = PatternFill(start_color=YELLOW_BG, end_color=YELLOW_BG, fill_type='solid')
                        cell.font = Font(color=YELLOW_FG, bold=True)
                    elif 'Salah' in str(val):
                        cell.fill = PatternFill(start_color=RED_BG, end_color=RED_BG, fill_type='solid')
                        cell.font = Font(color=RED_FG, bold=True)
                    else:
                        cell.fill = PatternFill(start_color=GRAY_BG, end_color=GRAY_BG, fill_type='solid')
                        cell.font = Font(color=GRAY_FG)

            data_row += 1

        # Total row
        if questions:
            ws.merge_cells(start_row=data_row, start_column=1, end_row=data_row, end_column=2)
            total_label = style_data_cell(ws, data_row, 1, 'center')
            total_label.value = 'TOTAL'
            total_label.font = Font(bold=True, size=11, color=NAVY)

            total_max_cell = style_data_cell(ws, data_row, 3, 'center')
            total_max_cell.value = round(total_max, 2)
            total_max_cell.font = Font(bold=True)

            total_earned_cell = style_data_cell(ws, data_row, 7, 'center')
            total_earned_cell.value = round(total_earned, 2)
            total_earned_cell.font = Font(bold=True, size=12, color=NAVY)

            total_fill = PatternFill(start_color=LIGHT_BLUE, end_color=LIGHT_BLUE, fill_type='solid')
            for c in range(1, 8):
                style_data_cell(ws, data_row, c).fill = total_fill
                style_data_cell(ws, data_row, c).border = thin_border

        # Auto-width
        col_widths = [10, 16, 12, 24, 24, 18, 14]
        for idx, w in enumerate(col_widths, 1):
            ws.column_dimensions[get_column_letter(idx)].width = w

    # ── Save workbook to BytesIO ──
    output = io.BytesIO()
    wb.save(output)
    output.seek(0)

    safe_exam_name = ''.join(c for c in exam['name'] if c.isalnum() or c in ' _-').strip().replace(' ', '_')
    filename = f"Hasil_Ujian_{safe_exam_name}_{datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')}.xlsx"

    response = send_file(
        output,
        mimetype='application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        as_attachment=True,
        download_name=filename
    )
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    return response


# ===== Public Exam Results Routes =====

def evaluate_submission_answers(answers, questions):
    """Evaluate submission answers and return individual correctness status for each question."""
    evaluation = {}
    if not questions:
        return evaluation
    
    for q in questions:
        try:
            num_val = float(q['number'])
            if num_val.is_integer():
                q_num = str(int(num_val))
            else:
                q_num = str(q['number'])
        except Exception:
            q_num = str(q['number'])

        student_ans = answers.get(q_num)
        correct_ans = q.get('key')
        q_weight = float(q.get('weight', 1.0))
        partial_scoring = q.get('partial_scoring', False)

        earned = 0.0
        status_text = 'Salah ❌'
        status_class = 'incorrect'

        if student_ans is not None and correct_ans is not None:
            if q['type'] in ['single_choice', 'true_false', 'short_answer']:
                s_norm = ' '.join(str(student_ans).split()).upper()
                c_norm = ' '.join(str(correct_ans).split()).upper()
                if s_norm == c_norm:
                    earned = q_weight
                    status_text = 'Benar ✔️'
                    status_class = 'correct'
            elif q['type'] == 'multiple_choice':
                if isinstance(student_ans, list) and isinstance(correct_ans, list):
                    if partial_scoring:
                        correct_set = set(str(x).upper() for x in correct_ans)
                        student_set = set(str(x).upper() for x in student_ans)
                        if correct_set:
                            correct_selected = sum(1 for x in student_set if x in correct_set)
                            incorrect_selected = sum(1 for x in student_set if x not in correct_set)
                            portion = max(0.0, (correct_selected - incorrect_selected) / len(correct_set))
                            earned = portion * q_weight
                            if portion >= 1.0:
                                status_text = 'Benar ✔️'
                                status_class = 'correct'
                            elif portion > 0:
                                status_text = 'Parsial ⚠️'
                                status_class = 'partial'
                    else:
                        if sorted([str(x).upper() for x in student_ans]) == sorted([str(x).upper() for x in correct_ans]):
                            earned = q_weight
                            status_text = 'Benar ✔️'
                            status_class = 'correct'
            elif q['type'] == 'matching':
                if isinstance(student_ans, dict) and isinstance(correct_ans, dict):
                    if partial_scoring:
                        if correct_ans:
                            correct_matches = 0
                            for k, v in correct_ans.items():
                                if str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper():
                                    correct_matches += 1
                            portion = correct_matches / len(correct_ans)
                            earned = portion * q_weight
                            if portion >= 1.0:
                                status_text = 'Benar ✔️'
                                status_class = 'correct'
                            elif portion > 0:
                                status_text = 'Parsial ⚠️'
                                status_class = 'partial'
                    else:
                        match = True
                        for k, v in correct_ans.items():
                            if str(student_ans.get(k, '')).strip().upper() != str(v).strip().upper():
                                match = False
                                break
                        if match:
                            earned = q_weight
                            status_text = 'Benar ✔️'
                            status_class = 'correct'
        
        evaluation[q_num] = {
            'statusClass': status_class,
            'statusText': status_text,
            'earned': earned
        }
    return evaluation


@app.route('/<token>')
def short_token_hasil(token):
    """Short URL redirect to exam results page, e.g. /BSGRIJ."""
    token_upper = token.strip().upper()
    if len(token_upper) == 6 and token_upper.isalnum():
        db = get_db()
        exam = db.execute(
            'SELECT id, public_results FROM exams WHERE token = ?',
            (token_upper,)
        ).fetchone()
        db.close()
        if exam:
            is_logged_in = 'admin_id' in session
            if (exam['public_results'] if exam['public_results'] is not None else 1) == 0 and not is_logged_in:
                abort(403)
            return redirect(url_for('public_hasil', token=token_upper))
    
    # If not a valid token, let Flask's 404 handler redirect to index/login
    abort(404)


@app.route('/hasil/<token>')
def public_hasil(token):
    """Public page for students to view exam results without login."""
    token = token.strip().upper()
    db = get_db()
    exam = db.execute(
        'SELECT id, name, token, public_results, show_answers FROM exams WHERE token = ?',
        (token,)
    ).fetchone()

    if not exam:
        db.close()
        return render_template('hasil.html',
                               exam_name='Ujian Tidak Ditemukan',
                               token=token,
                               total_students=0,
                               error=True), 404

    is_logged_in = 'admin_id' in session
    if (exam['public_results'] if exam['public_results'] is not None else 1) == 0 and not is_logged_in:
        db.close()
        return render_template('hasil.html',
                               exam_name=exam['name'],
                               token=token,
                               total_students=0,
                               is_disabled=True), 403

    total = db.execute(
        'SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?',
        (exam['id'],)
    ).fetchone()['cnt']
    db.close()

    show_answers = (exam['show_answers'] if exam['show_answers'] is not None else 0) == 1

    return render_template('hasil.html',
                           exam_name=exam['name'],
                           token=exam['token'],
                           total_students=total,
                           is_logged_in=is_logged_in,
                           show_answers=show_answers)


@app.route('/api/hasil/<token>')
def api_public_hasil(token):
    """Public API: get exam results by token (no login required)."""
    token = token.strip().upper()
    db = get_db()
    exam = db.execute(
        'SELECT id, name, token, questions_json, public_results, show_answers FROM exams WHERE token = ?',
        (token,)
    ).fetchone()

    if not exam:
        db.close()
        return jsonify({
            'success': False,
            'message': 'Token ujian tidak valid atau ujian tidak ditemukan.'
        }), 404

    is_logged_in = 'admin_id' in session
    if (exam['public_results'] if exam['public_results'] is not None else 1) == 0 and not is_logged_in:
        db.close()
        return jsonify({
            'success': False,
            'message': 'Akses dinonaktifkan: Halaman hasil ujian untuk siswa dinonaktifkan oleh guru.'
        }), 403

    submissions = db.execute(
        'SELECT id, student_name, exam_number, student_class, answers_json, score, start_time, created_at '
        'FROM submissions WHERE exam_id = ? ORDER BY score DESC',
        (exam['id'],)
    ).fetchall()
    db.close()

    # Parse questions (include keys for answer checking on client)
    questions = []
    if exam['questions_json']:
        try:
            questions = json.loads(exam['questions_json'])
        except Exception:
            pass

    # Calculate max possible score
    max_score = 0
    for q in questions:
        max_score += float(q.get('weight', 1.0))

    # Evaluate answers and build submissions list
    subs_data = []
    for sub in submissions:
        try:
            answers = json.loads(sub['answers_json']) if sub['answers_json'] else {}
        except Exception:
            answers = {}

        subs_data.append({
            'id': sub['id'],
            'student_name': sub['student_name'],
            'exam_number': sub['exam_number'],
            'student_class': sub['student_class'],
            'answers': answers,
            'score': sub['score'],
            'max_score': max_score if max_score > 0 else None,
            'start_time': format_iso_utc(sub['start_time']) if sub['start_time'] else None,
            'created_at': format_iso_utc(sub['created_at']),
            'evaluated_answers': evaluate_submission_answers(answers, questions)
        })

    # Determine if keys should be shown
    show_answers_enabled = (exam['show_answers'] if exam['show_answers'] is not None else 0) == 1

    # If NOT logged in AND show_answers is disabled, strip correct 'key' from questions
    if not is_logged_in and not show_answers_enabled:
        for q in questions:
            if 'key' in q:
                del q['key']

    return jsonify({
        'success': True,
        'exam_name': exam['name'],
        'show_answers': show_answers_enabled,
        'token': exam['token'],
        'questions': questions,
        'max_score': max_score if max_score > 0 else None,
        'submissions': subs_data
    })


# ===== Error Handlers =====

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
    """Handle 404 errors."""
    if request.path.startswith('/api/'):
        return jsonify({
            'success': False,
            'error': 'not_found',
            'message': 'Endpoint tidak ditemukan'
        }), 404
    return redirect(url_for('index'))


# ===== Main =====
if __name__ == '__main__':
    local_ip = get_local_ip()
    port = int(os.environ.get('PORT', 5000))

    print(f"""
╔══════════════════════════════════════════════╗
║           EXAMVAN Server v2.1.3              ║
╠══════════════════════════════════════════════╣
║  Local:   http://127.0.0.1:{port}              ║
║  LAN:     http://{local_ip}:{port}          ║
║  Admin:   http://{local_ip}:{port}/admin/login  ║
╠══════════════════════════════════════════════╣
║  Default Login:                              ║
║  Username: admin                             ║
║  Password: examvan2026                       ║
╚══════════════════════════════════════════════╝
    """)

    app.run(host='0.0.0.0', port=port, debug=True)
