"""EXAMVAN routes — extracted from app.py for organization."""
import os, json, csv, io, re, secrets, string, hmac, hashlib
from datetime import datetime, timezone, timedelta

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, send_file, flash, abort
)
from werkzeug.utils import secure_filename
from werkzeug.security import generate_password_hash, check_password_hash

from app import (
    app, get_db, get_db_standalone, init_db,
    get_saas_setting, set_saas_setting, send_whatsapp,
    generate_token, _verify_password, generate_csrf_token,
    admin_required, super_admin_required, csrf_required, check_rate_limit,
    check_exam_ownership, check_submission_ownership,
    get_network_info, get_storage_stats, safe_storage_path,
    STORAGE_DIR, MAX_FILE_SIZE, BASE_DIR,
    DEFAULT_IDENTITY_FIELDS, ADMIN_USERNAME, VERSION,
)
from helpers import (
    localize_date_string, format_iso_utc, get_local_ip,
    _normalize_q_num, _evaluate_single_question,
    evaluate_answers_detailed, calculate_submission_score,
)

from app import logger as app_logger
logger = app_logger


def error_response(message, code=400):
    """Return a standardized error JSON response."""
    return jsonify({'success': False, 'message': message}), code


def success_response(data=None, message=None):
    """Return a standardized success JSON response."""
    resp = {'success': True}
    if data is not None:
        resp['data'] = data
    if message:
        resp['message'] = message
    return jsonify(resp)


def _mask_token(token, visible_chars=4):
    """Mask a token showing only the last N characters."""
    if not token or len(token) <= visible_chars + 4:
        return token
    return '*' * (len(token) - visible_chars) + token[-visible_chars:]


def _csv_safe(value):
    """Prevent CSV formula injection by prefixing dangerous chars with tab."""
    if isinstance(value, str) and value and value[0] in ('=', '+', '-', '@', '\t', '\r'):
        return '\t' + value
    return value


def _hash_otp(otp, username):
    """Hash OTP with per-user salt for secure storage.

    Menggunakan HMAC-SHA256 dengan server-side secret key sebagai pepper
    dan username sebagai salt. Hash disimpan di database, bukan plaintext OTP.
    """
    secret_pepper = app.secret_key
    salt = username.encode('utf-8')
    key = hashlib.sha256(salt + secret_pepper.encode('utf-8')).digest()
    return hmac.new(key, otp.encode('utf-8'), hashlib.sha256).hexdigest()


def _verify_otp(input_otp, stored_hash, username):
    """Verify OTP by comparing hash of input with stored hash.
    Uses hmac.compare_digest for constant-time comparison.
    """
    input_hash = _hash_otp(input_otp, username)
    return hmac.compare_digest(input_hash, stored_hash)


def _validate_pdf_upload(file_data, filename, content_type, max_size=None):
    """Validate uploaded PDF file data. Returns (is_valid, error_message)."""
    if max_size is None:
        max_size = MAX_FILE_SIZE

    # Check that the uploaded file looks like a PDF
    allowed_pdf_types = ['application/pdf', 'application/x-pdf', 'application/octet-stream']
    if content_type not in allowed_pdf_types and not (filename and filename.lower().endswith('.pdf')):
        return False, 'Hanya file PDF yang diizinkan'

    # Check the file header for %PDF magic bytes
    if not file_data.startswith(b'%PDF'):
        return False, 'File tidak valid (bukan PDF)'

    # Check size
    if len(file_data) > max_size:
        size_mb = max_size // (1024 * 1024)
        return False, f'Ukuran file melebihi batas {size_mb}MB'

    return True, None


@app.route('/api/health')
def api_health():
    """Health check endpoint."""
    now = datetime.now(timezone.utc)
    required_version = get_saas_setting('android_version', '2.1.9')
    return jsonify({
        'status': 'ok',
        'version': VERSION,
        'required_app_version': required_version,
        'lan_mode': True,
        'timestamp': now.isoformat(),
        'server_time_utc': now.strftime('%Y-%m-%dT%H:%M:%SZ'),
        'certificate_fingerprint': get_saas_setting('certificate_fingerprint', '') or None
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
    """Get list of active exams with pagination."""
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 50, type=int)
    per_page = min(max(per_page, 1), 200)

    db = get_db()

    # Count total active exams
    total = db.execute(
        'SELECT COUNT(*) as cnt FROM exams WHERE status = ?',
        ('active',)
    ).fetchone()['cnt']

    exams = db.execute(
        'SELECT id, name, status, size_bytes, token, created_at '
        'FROM exams WHERE status = ? ORDER BY created_at DESC LIMIT ? OFFSET ?',
        ('active', per_page, (page - 1) * per_page)
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

    total_pages = max(1, (total + per_page - 1) // per_page)

    return jsonify({
        'success': True,
        'data': data,
        'pagination': {
            'page': page,
            'per_page': per_page,
            'total': total,
            'total_pages': total_pages
        }
    })


@app.route('/api/exams/token/<token>')
def api_exam_by_token(token):
    """Get exam info by token. Used by Android app."""
    required_version = get_saas_setting('android_version', '2.1.9')
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
        'SELECT id, name, status, size_bytes, token, questions_json, security_level, strict_mode, identity_fields, created_at '
        'FROM exams WHERE token = ? AND status = ?',
        (token, 'active')
    ).fetchone()

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
        except Exception as e:
            logger.warning("Failed to parse questions_json for exam %s: %s", exam.get('id'), e)

    # Process identity fields configuration
    identity_fields = []
    if exam['identity_fields']:
        try:
            identity_fields = json.loads(exam['identity_fields'])
        except Exception as e:
            logger.warning("Failed to parse identity_fields for exam %s: %s", exam.get('id'), e)
    if not identity_fields:
        try:
            identity_fields = json.loads(DEFAULT_IDENTITY_FIELDS)
        except Exception:
            identity_fields = []

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
            'strict_mode': bool(exam['strict_mode']),
            'identity_fields': identity_fields,
            'created_at': format_iso_utc(exam['created_at'])
        }
    })


@app.route('/api/exams/<int:exam_id>/submit', methods=['POST'])
def api_submit_exam(exam_id):
    """Receive student exam submissions and auto-grade if keys exist."""
    if not check_rate_limit(f'submit:{exam_id}', max_attempts=10, window_seconds=60):
        return error_response('Terlalu banyak percobaan submit. Silakan coba lagi nanti.', 429)

    required_version = get_saas_setting('android_version', '2.1.9')
    client_version = request.headers.get('X-App-Version')
    if client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version or "v1.x"}). Silakan unduh EXAMVAN v{required_version} terbaru untuk dapat mengumpulkan jawaban.'
        }), 426

    data = request.json or {}
    # identity_data contains all dynamic identity fields (from new Android app)
    identity_data = data.get('identity_data')
    if identity_data and isinstance(identity_data, dict):
        identity_data_json = json.dumps(identity_data)
        student_name = str(identity_data.get('student_name', '')).strip()
        exam_number = str(identity_data.get('exam_number', '')).strip()
        student_class = str(identity_data.get('student_class', '')).strip()
    else:
        identity_data_json = None
        student_name = data.get('student_name', '').strip()
        exam_number = data.get('exam_number', '').strip()
        student_class = data.get('student_class', '').strip()

    answers = data.get('answers', {})
    start_time_raw = data.get('start_time')
    mac_address = data.get('mac_address', '').strip() or None

    start_time = None
    if start_time_raw:
        try:
            start_time = start_time_raw.replace('T', ' ').replace('Z', '')
        except Exception:
            start_time = start_time_raw

    if not student_name or not exam_number or not student_class:
        return error_response('Identitas siswa tidak lengkap', 400)

    db = get_db()
    exam = db.execute('SELECT questions_json FROM exams WHERE id = ? AND status = ?', (exam_id, 'active')).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    score = None
    questions_raw = exam['questions_json']
    if questions_raw:
        try:
            questions = json.loads(questions_raw)
            score = calculate_submission_score(answers, questions)
        except Exception as e:
            logger.error("Auto-grading error: %s", e)

    db.execute(
        'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, identity_data, answers_json, score, start_time, mac_address) '
        'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
        (exam_id, student_name, exam_number, student_class, identity_data_json, json.dumps(answers), score, start_time, mac_address)
    )
    db.commit()

    return jsonify({
        'success': True,
        'message': 'Jawaban berhasil dikirim',
        'score': score
    })


@app.route('/api/exams/<int:exam_id>/pdf')
def api_exam_pdf(exam_id):
    """Stream PDF file for an exam. Requires token query param to prevent probing."""
    token_param = request.args.get('token', '').strip().upper()
    if not token_param:
        return jsonify({
            'success': False,
            'error': 'token_required',
            'message': 'Parameter token diperlukan untuk mengakses file ujian'
        }), 401

    db = get_db()
    exam = db.execute(
        'SELECT * FROM exams WHERE id = ? AND status = ? AND token = ?',
        (exam_id, 'active', token_param)
    ).fetchone()

    if not exam:
        return jsonify({
            'success': False,
            'error': 'exam_not_found',
            'message': 'Ujian tidak tersedia, sudah berakhir, atau token tidak valid'
        }), 404

    file_path = safe_storage_path(exam['file_path'])
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

        # Rate limiting: max 5 attempts per 60 seconds per IP
        if not check_rate_limit('admin_login', max_attempts=5, window_seconds=60):
            flash('Terlalu banyak percobaan login. Silakan coba lagi dalam 60 detik.', 'error')
            return render_template('login.html')

        db = get_db()
        user = db.execute(
            'SELECT id, username, status, password_hash FROM admin_users WHERE username = ?',
            (username,)
        ).fetchone()

        if user and _verify_password(password, user['password_hash']):
            # Password hash sudah scrypt/pbkdf2 (SHA-256 legacy sudah tidak didukung).

            if user['status'] == 'pending_otp':
                flash('Pendaftaran Anda membutuhkan konfirmasi OTP WhatsApp. Silakan verifikasi.', 'warning')
                return redirect(url_for('verify_otp', username=user['username']))
            elif user['status'] == 'suspended':
                flash('Akun Anda telah dinonaktifkan oleh administrator.', 'error')
                return redirect(url_for('admin_login'))

            # Check account expiry (skip for super admin)
            if user['username'] != 'admin':
                user_full = db.execute('SELECT expires_at FROM admin_users WHERE id = ?', (user['id'],)).fetchone()
                if user_full and user_full['expires_at']:
                    expires_at = datetime.strptime(user_full['expires_at'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
                    if datetime.now(timezone.utc) > expires_at:
                        flash('Masa aktif akun Anda telah habis. Silakan hubungi administrator.', 'error')
                        return redirect(url_for('admin_login'))

            session['admin_id'] = user['id']
            session['admin_username'] = user['username']
            # Regenerate session ID to prevent session fixation
            session.regenerate()
            return redirect(url_for('admin_dashboard'))
        else:
            flash('Username atau password salah', 'error')

    return render_template('login.html')


@app.route('/register', methods=['GET', 'POST'])
@csrf_required
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

        if username == ADMIN_USERNAME:
            flash(f'Username "{ADMIN_USERNAME}" tidak dapat digunakan', 'error')
            return render_template('register.html')

        if len(username) < 3 or not username.isalnum():
            flash('Username minimal 3 karakter alfanumerik', 'error')
            return render_template('register.html')

        if len(password) < 8:
            flash('Password minimal 8 karakter', 'error')
            return render_template('register.html')

        # Validate Indonesian WhatsApp number (08xx or 62xx, 10-15 digits)
        wa_clean = whatsapp.replace('+', '').replace('-', '').replace(' ', '')
        if not ((wa_clean.startswith('08') or wa_clean.startswith('62')) and wa_clean.isdigit() and 10 <= len(wa_clean) <= 15):
            flash('Format nomor WhatsApp tidak valid. Gunakan nomor Indonesia (08xx atau 62xx, 10-15 digit)', 'error')
            return render_template('register.html')

        db = get_db()
        existing = db.execute('SELECT id FROM admin_users WHERE username = ?', (username,)).fetchone()
        if existing:
            flash('Username sudah digunakan', 'error')
            return render_template('register.html')

        pw_hash = generate_password_hash(password)

        default_exams = int(get_saas_setting('default_max_exams', '3'))
        default_pdf = int(get_saas_setting('default_max_pdf_size', '1048576'))
        default_drafts = int(get_saas_setting('default_max_drafts', '2'))
        default_draft_size = int(get_saas_setting('default_max_draft_size', '1048576'))
        wa_enabled = get_saas_setting('wa_verification_enabled', '0') == '1'

        default_active_days = int(get_saas_setting('default_active_days', '1'))
        expires_at = (datetime.now(timezone.utc) + timedelta(days=default_active_days)).strftime('%Y-%m-%d %H:%M:%S')

        if wa_enabled:
            otp = ''.join(secrets.choice(string.digits) for _ in range(6))
            otp_hash = _hash_otp(otp, username)
            otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')
            
            db.execute(
                'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, otp_code, otp_expiry, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) '
                'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (username, pw_hash, whatsapp, 'pending_otp', otp_hash, otp_expiry, default_exams, default_pdf, default_drafts, default_draft_size, expires_at)
            )
            db.commit()
            
            template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
            message = template.replace('{otp}', otp)
            send_whatsapp(whatsapp, message)
            
            flash('Registrasi berhasil! Masukkan kode OTP yang dikirim ke nomor WhatsApp Anda.', 'warning')
            return redirect(url_for('verify_otp', username=username))
        else:
            db.execute(
                'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) '
                'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (username, pw_hash, whatsapp, 'active', default_exams, default_pdf, default_drafts, default_draft_size, expires_at)
            )
            db.commit()
            
            flash('Registrasi berhasil! Silakan masuk dengan akun Anda.', 'success')
            return redirect(url_for('admin_login'))

    return render_template('register.html')


@app.route('/verify-otp', methods=['GET', 'POST'])
def verify_otp():
    """Verify registration OTP code."""
    username = request.args.get('username', '').strip().lower()
    
    if request.method == 'POST':
        # Rate limit: max 5 OTP attempts per 5 minutes per IP (brute force protection)
        if not check_rate_limit('verify_otp', max_attempts=5, window_seconds=300):
            flash('Terlalu banyak percobaan verifikasi. Silakan coba lagi nanti.', 'error')
            return render_template('verify_otp.html', username=username)

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
            flash('Permintaan verifikasi tidak valid atau kedaluwarsa', 'error')
            return redirect(url_for('admin_login'))

        if not _verify_otp(otp_input, user['otp_code'] or '', username):
            flash('Kode OTP yang Anda masukkan salah', 'error')
            return render_template('verify_otp.html', username=username)

        now_str = datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S')
        if user['otp_expiry'] < now_str:
            flash('Kode OTP telah kedaluwarsa. Silakan gunakan tombol "Kirim Ulang OTP" untuk mendapatkan kode baru.', 'error')
            return render_template('verify_otp.html', username=username)

        db.execute('UPDATE admin_users SET status = ?, otp_code = NULL, otp_expiry = NULL WHERE id = ?', ('active', user['id']))
        db.commit()

        flash('Verifikasi nomor WhatsApp berhasil! Akun Anda aktif. Silakan login.', 'success')
        return redirect(url_for('admin_login'))
        
    return render_template('verify_otp.html', username=username)


@app.route('/resend-otp', methods=['POST'])
@csrf_required
def resend_otp():
    """Resend OTP code to user's registered WhatsApp number."""
    # Rate limit: max 3 resend requests per 10 minutes per IP
    if not check_rate_limit('resend_otp', max_attempts=3, window_seconds=600):
        return error_response('Terlalu banyak permintaan kirim ulang OTP. Silakan coba lagi nanti.', 429)

    username = request.form.get('username', '').strip().lower()

    if not username:
        return error_response('Username wajib diisi', 400)

    db = get_db()
    user = db.execute(
        'SELECT id, whatsapp_number FROM admin_users WHERE username = ? AND status = ?',
        (username, 'pending_otp')
    ).fetchone()
    
    if not user:
        return error_response('User tidak ditemukan atau sudah terverifikasi', 404)

    otp = ''.join(secrets.choice(string.digits) for _ in range(6))
    otp_hash = _hash_otp(otp, username)
    otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')

    db.execute(
        'UPDATE admin_users SET otp_code = ?, otp_expiry = ? WHERE id = ?',
        (otp_hash, otp_expiry, user['id'])
    )
    db.commit()
    
    template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
    message = template.replace('{otp}', otp)
    send_whatsapp(user['whatsapp_number'], message)
    
    return jsonify({'success': True, 'message': 'Kode OTP baru berhasil dikirim via WhatsApp'})


@app.route('/admin/logout')
def admin_logout():
    """Admin logout."""
    session.clear()
    return redirect(url_for('admin_login'))


def _days_until_expiry(expires_at_str):
    """Calculate days remaining until account expiry. Returns int or None."""
    if not expires_at_str:
        return None
    try:
        expires = datetime.strptime(expires_at_str, '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
        remaining = (expires - datetime.now(timezone.utc)).days
        return max(remaining, 0)
    except Exception:
        return None


@app.route('/admin/dashboard')
@admin_required
def admin_dashboard():
    """Admin dashboard page with pagination & search."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == ADMIN_USERNAME)

    # Pagination & search params
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 20, type=int)
    search = request.args.get('search', '').strip()
    per_page = min(max(per_page, 5), 100)  # clamp 5-100

    # Build query conditions
    conditions = []
    params = []
    if not is_super_admin:
        conditions.append('e.created_by = ?')
        params.append(session['admin_id'])
    if search:
        conditions.append('(e.name LIKE ? OR e.token LIKE ? OR u.username LIKE ?)')
        search_param = f'%{search}%'
        params.extend([search_param, search_param, search_param])

    where_clause = (' WHERE ' + ' AND '.join(conditions)) if conditions else ''

    # Count total matching exams (JOIN admin_users for creator name search)
    count_sql = 'SELECT COUNT(*) as cnt FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id' + where_clause
    total = db.execute(count_sql, params).fetchone()['cnt']

    # Fetch paginated exams
    base_query = (
        'SELECT e.*, u.username as creator_name, '
        '(SELECT COUNT(*) FROM submissions s WHERE s.exam_id = e.id) as sub_count '
        'FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id'
    )
    order = ' ORDER BY e.created_at DESC'
    limit_offset = ' LIMIT ? OFFSET ?'
    exams = db.execute(base_query + where_clause + order + limit_offset, params + [per_page, (page - 1) * per_page]).fetchall()

    # Calculate stats (always from unfiltered data for accuracy)
    stats_conditions = []
    stats_params = []
    if not is_super_admin:
        stats_conditions.append('created_by = ?')
        stats_params.append(session['admin_id'])

    stats_where = (' WHERE ' + ' AND '.join(stats_conditions)) if stats_conditions else ''

    stats_total = db.execute(f'SELECT COUNT(*) as cnt FROM exams{stats_where}', stats_params).fetchone()['cnt']

    active_where = stats_where + (' AND ' if stats_conditions else ' WHERE ') + 'status = ?'
    active = db.execute(f'SELECT COUNT(*) as cnt FROM exams{active_where}', stats_params + ['active']).fetchone()['cnt']
    inactive = stats_total - active

    if is_super_admin:
        storage_bytes = get_storage_stats()
    else:
        storage_bytes = db.execute(f'SELECT COALESCE(SUM(size_bytes), 0) as total FROM exams e{stats_where}', stats_params).fetchone()['total']

    total_pages = max(1, (total + per_page - 1) // per_page)

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

    days_remaining = _days_until_expiry(account_expires)

    return render_template(
        'dashboard.html',
        exams=exams,
        stats={
            'total': total,
            'active': active,
            'inactive': inactive,
            'storage_mb': round(storage_bytes / (1024 * 1024), 2),
            'total_all': stats_total,
        },
        net_info=net_info,
        local_ip=net_info['display_host'],
        admin_user=session.get('admin_username', 'Admin'),
        max_size_mb=round(user_max_pdf / (1024 * 1024), 1),
        max_exams=user_max_exams,
        account_expires=account_expires,
        days_remaining=days_remaining,
        active_page='dashboard',
        # Pagination
        page=page,
        per_page=per_page,
        total_pages=total_pages,
        total_exams=total,
        search=search,
        search_active=bool(search),
    )

@app.route('/admin/api/upload', methods=['POST'])
@admin_required
def admin_upload():
    """Upload a new exam PDF."""
    name = request.form.get('name', '').strip()
    file = request.files.get('pdf_file')

    if not name:
        return error_response('Nama ujian wajib diisi', 400)

    if not file or file.filename == '':
        return error_response('File PDF wajib dipilih', 400)

    # Check that the uploaded file looks like a PDF
    file_data = file.read()
    is_valid, error_msg = _validate_pdf_upload(file_data, file.filename, file.content_type)
    if not is_valid:
        return error_response(error_msg, 400)

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
            return jsonify({
                'success': False, 
                'message': f'Ukuran file melebihi batas akun Anda ({pdf_limit_mb}MB). Silakan hubungi Super Admin untuk menaikkan limit.'
            }), 403
        
        # Check exam count limit
        current_count = db.execute('SELECT COUNT(*) as count FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchone()['count']
        if current_count >= exam_limit:
            return jsonify({
                'success': False, 
                'message': f'Batas pembuatan ujian tercapai. Batas akun Anda adalah {exam_limit} ujian. Silakan hubungi Super Admin untuk menaikkan limit.'
            }), 403

    if custom_token:
        if not re.match(r'^[A-Z0-9]{6}$', custom_token):
            return error_response('Token kustom harus terdiri dari 6 karakter alfanumerik', 400)

        # Check uniqueness
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (custom_token,)).fetchone()
        if existing:
            return error_response('Token kustom sudah digunakan oleh ujian lain', 400)
        token = custom_token
    else:
        # Generate unique token
        token = generate_token()

    # Save file with secure name
    timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
    safe_name = secure_filename(file.filename) or 'exam.pdf'
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

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil diupload dengan token: {token}',
        'token': token
    })


@app.route('/admin/api/exams/<int:exam_id>/pdf', methods=['GET'])
@admin_required
def admin_exam_pdf(exam_id):
    """View or download the exam PDF for admin."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return abort(403)
    
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return abort(404)

    file_path = safe_storage_path(exam['file_path'])
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
        safe_name = secure_filename(exam['name']) or 'exam'
        response.headers['Content-Disposition'] = f'inline; filename="{safe_name}.pdf"'
    
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    response.headers['X-Content-Type-Options'] = 'nosniff'
    return response


@app.route('/admin/api/exams/<int:exam_id>/toggle', methods=['POST'])
@admin_required
def admin_toggle_exam(exam_id):
    """Toggle exam status between active and inactive."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    new_status = 'inactive' if exam['status'] == 'active' else 'active'
    db.execute('UPDATE exams SET status = ? WHERE id = ?', (new_status, exam_id))
    db.commit()

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
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # Toggle public_results: 1 (active) <=> 0 (inactive)
    current = exam['public_results']
    if current is None:
        current = 1
    new_val = 0 if current == 1 else 1
    db.execute('UPDATE exams SET public_results = ? WHERE id = ?', (new_val, exam_id))
    db.commit()

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
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    current = exam['show_answers']
    if current is None:
        current = 0
    new_val = 0 if current == 1 else 1
    db.execute('UPDATE exams SET show_answers = ? WHERE id = ?', (new_val, exam_id))
    db.commit()

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
        return error_response('Nama ujian wajib diisi', 400)

    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # If new PDF file is uploaded
    if file and file.filename != '':
        file_data = file.read()
        is_valid, error_msg = _validate_pdf_upload(file_data, file.filename, file.content_type)
        if not is_valid:
            return error_response(error_msg, 400)

        # Delete old file from storage if exists
        try:
            old_file_path = safe_storage_path(exam['file_path'])
            if os.path.exists(old_file_path):
                try:
                    os.remove(old_file_path)
                except Exception as e:
                    logger.error("Error removing old PDF: %s", e)
        except (ValueError, KeyError):
            logger.error("Invalid file path for exam %s: %s", exam_id, exam.get('file_path'))

        # Save new file
        timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
        safe_name = secure_filename(file.filename) or 'exam.pdf'
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
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # Delete file from storage
    try:
        file_path = safe_storage_path(exam['file_path'])
        if os.path.exists(file_path):
            os.remove(file_path)
    except (ValueError, KeyError):
        logger.error("Invalid file path for exam %s: %s", exam_id, exam.get('file_path'))

    # Delete from database
    db.execute('DELETE FROM exams WHERE id = ?', (exam_id,))
    db.commit()

    return jsonify({'success': True, 'message': 'Ujian berhasil dihapus'})


@app.route('/admin/exams/bulk-delete', methods=['POST'])
@admin_required
def admin_bulk_delete_exams():
    """Bulk delete exams."""
    data = request.get_json() or {}
    exam_ids = data.get('ids', [])
    if not exam_ids:
        return error_response('Tidak ada ujian yang dipilih', 400)

    db = get_db()
    try:
        # Filter IDs by ownership for non-super admin
        if session.get('admin_username') != ADMIN_USERNAME:
            owned = db.execute(
                f'SELECT id FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)}) AND created_by = ?',
                exam_ids + [session['admin_id']]
            ).fetchall()
            exam_ids = [row['id'] for row in owned]
            if not exam_ids:
                return error_response('Tidak ada ujian yang dapat dihapus', 400)

        # Select file_paths in bulk
        rows = db.execute(
            f'SELECT file_path FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)})',
            exam_ids
        ).fetchall()

        # Delete files from storage
        for row in rows:
            try:
                fp = safe_storage_path(row['file_path'])
                if os.path.exists(fp):
                    os.remove(fp)
            except (ValueError, KeyError) as e:
                logger.warning("Invalid file path in bulk delete: %s", e)

        # Single DELETE
        db.execute(
            f'DELETE FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)})',
            exam_ids
        )
        db.commit()
        return jsonify({'success': True, 'message': f'{len(exam_ids)} ujian berhasil dihapus'})
    except Exception as e:
        db.rollback()
        logger.error("Bulk delete error: %s", e)
        return error_response('Terjadi kesalahan saat menghapus ujian', 500)


@app.route('/admin/exams/bulk-toggle', methods=['POST'])
@admin_required
def admin_bulk_toggle_exams():
    """Bulk change exam status (active/inactive)."""
    data = request.get_json() or {}
    exam_ids = data.get('ids', [])
    target_status = data.get('status', 'inactive')
    
    if not exam_ids:
        return error_response('Tidak ada ujian yang dipilih', 400)
    if target_status not in ['active', 'inactive']:
        return error_response('Status tidak valid', 400)

    db = get_db()
    try:
        # Filter IDs by ownership for non-super admin
        if session.get('admin_username') != ADMIN_USERNAME:
            owned = db.execute(
                f'SELECT id FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)}) AND created_by = ?',
                exam_ids + [session['admin_id']]
            ).fetchall()
            exam_ids = [row['id'] for row in owned]
            if not exam_ids:
                return error_response('Tidak ada ujian yang dapat diperbarui', 400)

        # Single UPDATE
        db.execute(
            f'UPDATE exams SET status = ? WHERE id IN ({",".join("?" for _ in exam_ids)})',
            [target_status] + exam_ids
        )
        db.commit()
        return jsonify({'success': True, 'message': f'Status {len(exam_ids)} ujian berhasil diperbarui ke {target_status}'})
    except Exception as e:
        db.rollback()
        logger.error("Bulk toggle error: %s", e)
        return error_response('Terjadi kesalahan saat memperbarui status ujian', 500)


@app.route('/admin/api/exams/<int:exam_id>/regenerate-token', methods=['POST'])
@admin_required
def admin_regenerate_token(exam_id):
    """Regenerate token for an exam."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    new_token = generate_token()
    db.execute('UPDATE exams SET token = ? WHERE id = ?', (new_token, exam_id))
    db.commit()

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
        return error_response('Token kustom tidak boleh kosong', 400)

    if not re.match(r'^[A-Z0-9]{6}$', custom_token):
        return error_response('Token kustom harus terdiri dari 6 karakter alfanumerik', 400)

    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # Check if this token is already in use by another exam
    existing = db.execute('SELECT id FROM exams WHERE token = ? AND id != ?', (custom_token, exam_id)).fetchone()
    if existing:
        return error_response('Token kustom sudah digunakan oleh ujian lain', 400)

    db.execute('UPDATE exams SET token = ? WHERE id = ?', (custom_token, exam_id))
    db.commit()

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
    is_super_admin = (session.get('admin_username') == ADMIN_USERNAME)
    if is_super_admin:
        exams = db.execute('SELECT status, size_bytes FROM exams').fetchall()
    else:
        exams = db.execute('SELECT status, size_bytes FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchall()

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
        return error_response('Semua field password wajib diisi', 400)

    if len(new_password) < 8:
        return error_response('Password baru minimal 8 karakter', 400)

    if len(new_password) > 128:
        return error_response('Password baru maksimal 128 karakter', 400)

    db = get_db()
    user = db.execute(
        'SELECT id, password_hash FROM admin_users WHERE id = ?',
        (session['admin_id'],)
    ).fetchone()

    if not user or not _verify_password(current_password, user['password_hash']):
        return error_response('Password saat ini salah', 400)

    new_hash = generate_password_hash(new_password)
    db.execute(
        'UPDATE admin_users SET password_hash = ? WHERE id = ?',
        (new_hash, session['admin_id'])
    )
    db.commit()

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
    """List all registered users (teachers) with exam count and limit. Supports pagination & search."""
    db = get_db()
    search = (request.args.get('search', '') or '').strip().lower()

    # Pagination params
    try:
        page = max(1, int(request.args.get('page', 1)))
    except (ValueError, TypeError):
        page = 1
    try:
        per_page = max(5, min(200, int(request.args.get('per_page', 10))))
    except (ValueError, TypeError):
        per_page = 10

    # Build WHERE clauses for search
    where_extra = ''
    params_extra = []
    if search:
        where_extra = 'AND (username LIKE ? OR whatsapp_number LIKE ?)'
        params_extra = [f'%{search}%', f'%{search}%']

    # Count total (excluding admin superuser)
    total_row = db.execute(
        'SELECT COUNT(*) as cnt FROM admin_users WHERE username != ? ' + where_extra,
        [ADMIN_USERNAME] + params_extra
    ).fetchone()
    total = total_row['cnt'] if total_row else 0
    total_pages = max(1, (total + per_page - 1) // per_page)
    page = min(page, total_pages) if total > 0 else 1
    offset = (page - 1) * per_page

    BASE_SELECT = (
        'SELECT u.id, u.username, u.whatsapp_number, u.status, '
        'u.max_exams, u.max_pdf_size, u.max_drafts, u.max_draft_size, '
        'u.expires_at, u.created_at, COUNT(e.id) as exam_count '
        'FROM admin_users u LEFT JOIN exams e ON e.created_by = u.id '
    )

    # Fetch admin (only if no search or admin matches search)
    admin_user = None
    if not search or ADMIN_USERNAME.startswith(search):
        admin_user = db.execute(
            BASE_SELECT + 'WHERE u.username = ? GROUP BY u.id',
            (ADMIN_USERNAME,)
        ).fetchone()

    # Fetch paginated teachers
    users = db.execute(
        BASE_SELECT
        + 'WHERE u.username != ? ' + where_extra
        + 'GROUP BY u.id ORDER BY u.username ASC LIMIT ? OFFSET ?',
        [ADMIN_USERNAME] + params_extra + [per_page, offset]
    ).fetchall()

    def _build_user(u):
        return {
            'id': u['id'],
            'username': u['username'],
            'whatsapp_number': u['whatsapp_number'] or '',
            'status': u['status'] or 'active',
            'max_exams': u['max_exams'] if u['max_exams'] is not None else 3,
            'max_pdf_size': u['max_pdf_size'] if u['max_pdf_size'] is not None else 1048576,
            'expires_at': u['expires_at'] or '',
            'exam_count': u['exam_count'],
            'created_at': format_iso_utc(u['created_at'])
        }

    user_list = []
    if admin_user and admin_user['username'] == ADMIN_USERNAME:
        user_list.append(_build_user(admin_user))
    for u in users:
        user_list.append(_build_user(u))

    return jsonify({
        'success': True,
        'users': user_list,
        'pagination': {
            'page': page,
            'per_page': per_page,
            'total': total,
            'total_pages': total_pages
        }
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
    max_drafts = data.get('max_drafts', 2)
    max_draft_size_mb = data.get('max_draft_size_mb', 1)

    try:
        max_exams = int(max_exams)
    except (ValueError, TypeError):
        max_exams = 3

    try:
        max_pdf_size_mb = float(max_pdf_size_mb)
    except (ValueError, TypeError):
        max_pdf_size_mb = 1
    max_pdf_size = int(max_pdf_size_mb * 1024 * 1024)

    try:
        max_drafts = int(max_drafts)
    except (ValueError, TypeError):
        max_drafts = 2

    try:
        max_draft_size_mb = float(max_draft_size_mb)
    except (ValueError, TypeError):
        max_draft_size_mb = 1
    max_draft_size = int(max_draft_size_mb * 1024 * 1024)

    if not username or not password:
        return error_response('Username dan password wajib diisi', 400)

    if username == ADMIN_USERNAME:
        return error_response(f'Username "{ADMIN_USERNAME}" sudah terdaftar sebagai Super Admin', 400)

    db = get_db()
    existing = db.execute('SELECT id FROM admin_users WHERE username = ?', (username,)).fetchone()
    if existing:
        return error_response('Username sudah digunakan', 400)

    pw_hash = generate_password_hash(password)
    expires_at = data.get('expires_at', '').strip()
    if not expires_at:
        default_active_days = int(get_saas_setting('default_active_days', '1'))
        expires_at = (datetime.now(timezone.utc) + timedelta(days=default_active_days)).strftime('%Y-%m-%d %H:%M:%S')

    db.execute(
        'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
        (username, pw_hash, whatsapp, 'active', max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at)
    )
    db.commit()

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
        return error_response('User tidak ditemukan', 404)

    if user['username'] == ADMIN_USERNAME:
        return error_response('Super Admin "admin" tidak dapat diubah limitnya', 400)

    if max_exams is not None:
        try:
            max_exams = int(max_exams)
        except (ValueError, TypeError):
            return error_response('Limit ujian harus berupa angka valid', 400)
        db.execute('UPDATE admin_users SET max_exams = ? WHERE id = ?', (max_exams, user_id))

    if max_pdf_size_mb is not None:
        try:
            max_pdf_size_mb = float(max_pdf_size_mb)
        except (ValueError, TypeError):
            return error_response('Limit ukuran PDF harus berupa angka valid', 400)
        max_pdf_size = int(max_pdf_size_mb * 1024 * 1024)
        db.execute('UPDATE admin_users SET max_pdf_size = ? WHERE id = ?', (max_pdf_size, user_id))

    max_drafts = data.get('max_drafts')
    max_draft_size_mb = data.get('max_draft_size_mb')

    if max_drafts is not None:
        try:
            max_drafts = int(max_drafts)
        except (ValueError, TypeError):
            return error_response('Limit draf harus berupa angka valid', 400)
        db.execute('UPDATE admin_users SET max_drafts = ? WHERE id = ?', (max_drafts, user_id))

    if max_draft_size_mb is not None:
        try:
            max_draft_size_mb = float(max_draft_size_mb)
        except (ValueError, TypeError):
            return error_response('Limit ukuran draf harus berupa angka valid', 400)
        max_draft_size = int(max_draft_size_mb * 1024 * 1024)
        db.execute('UPDATE admin_users SET max_draft_size = ? WHERE id = ?', (max_draft_size, user_id))

    if whatsapp is not None:
        db.execute('UPDATE admin_users SET whatsapp_number = ? WHERE id = ?', (whatsapp.strip(), user_id))

    if status is not None:
        if status in ['active', 'suspended', 'pending_otp']:
            db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (status, user_id))

    if password:
        pw_hash = generate_password_hash(password)
        db.execute('UPDATE admin_users SET password_hash = ? WHERE id = ?', (pw_hash, user_id))

    if 'expires_at' in data:
        exp_val = data.get('expires_at')
        if exp_val:
            if ' ' not in exp_val:
                exp_val = f"{exp_val} 23:59:59"
            # Validate date format before saving
            try:
                datetime.strptime(exp_val, '%Y-%m-%d %H:%M:%S')
            except (ValueError, TypeError):
                return error_response('Format tanggal expiry tidak valid. Gunakan format YYYY-MM-DD HH:MM:SS', 400)
            db.execute('UPDATE admin_users SET expires_at = ? WHERE id = ?', (exp_val, user_id))
        else:
            db.execute('UPDATE admin_users SET expires_at = NULL WHERE id = ?', (user_id,))

    db.commit()

    return jsonify({'success': True, 'message': f'Pengaturan user "{user["username"]}" berhasil diperbarui'})


@app.route('/admin/api/users/<int:user_id>/verify', methods=['POST'])
@super_admin_required
def admin_verify_user_manual(user_id):
    """Manually activate/verify a pending user."""
    db = get_db()
    user = db.execute('SELECT username, status FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)
        
    db.execute('UPDATE admin_users SET status = ?, otp_code = NULL, otp_expiry = NULL WHERE id = ?', ('active', user_id))
    db.commit()
    return jsonify({'success': True, 'message': f'User "{user["username"]}" berhasil diaktifkan secara manual'})


@app.route('/admin/api/users/<int:user_id>/toggle-status', methods=['POST'])
@super_admin_required
def admin_toggle_user_status(user_id):
    """Suspend or activate a user account."""
    db = get_db()
    user = db.execute('SELECT username, status FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)
        
    if user['username'] == ADMIN_USERNAME:
        return error_response('Status Super Admin "admin" tidak dapat diubah', 400)
        
    new_status = 'suspended' if user['status'] == 'active' else 'active'
    db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (new_status, user_id))
    db.commit()
    return jsonify({'success': True, 'message': f'Status user "{user["username"]}" berhasil diubah menjadi {new_status}'})


@app.route('/admin/api/saas-settings', methods=['GET', 'POST'])
@super_admin_required
def admin_saas_settings():
    """Get or update SaaS settings (WhatsApp verification gateway configs, default limits)."""
    if request.method == 'POST':
        data = request.json or {}
        wa_enabled = '1' if data.get('wa_verification_enabled') else '0'
        wa_token = data.get('wa_api_token', '').strip()
        # If the received token looks masked (starts with *), keep the existing value
        if wa_token.startswith('*'):
            wa_token = get_saas_setting('wa_api_token', '')
        wa_template = data.get('wa_otp_template', '').strip()
        default_exams = data.get('default_max_exams', '3')
        default_pdf_size_mb = data.get('default_max_pdf_size_mb', '1')
        default_active_days = data.get('default_active_days', '1')
        default_drafts = data.get('default_max_drafts', '2')
        default_draft_size_mb = data.get('default_max_draft_size_mb', '1')
        android_version = data.get('android_version', '2.1.9').strip()
        webapp_version = data.get('webapp_version', '2.1.9').strip()
        
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
            default_drafts = int(default_drafts)
        except (ValueError, TypeError):
            default_drafts = 2

        try:
            default_draft_size_mb = float(default_draft_size_mb)
        except (ValueError, TypeError):
            default_draft_size_mb = 1.0

        default_draft_size = int(default_draft_size_mb * 1024 * 1024)

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
        set_saas_setting('default_max_drafts', str(default_drafts))
        set_saas_setting('default_max_draft_size', str(default_draft_size))
        set_saas_setting('default_active_days', str(default_active_days))
        set_saas_setting('android_version', android_version)
        set_saas_setting('webapp_version', webapp_version)
        cert_fingerprint = data.get('certificate_fingerprint', '').strip()
        set_saas_setting('certificate_fingerprint', cert_fingerprint)

        return jsonify({'success': True, 'message': 'Pengaturan SaaS berhasil diperbarui'})
        
    # GET settings
    settings = {
        'wa_verification_enabled': get_saas_setting('wa_verification_enabled', '0') == '1',
        'wa_api_token': _mask_token(get_saas_setting('wa_api_token', '')),
        'wa_otp_template': get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.'),
        'default_max_exams': int(get_saas_setting('default_max_exams', '3')),
        'default_max_pdf_size_mb': round(int(get_saas_setting('default_max_pdf_size', '1048576')) / (1024*1024), 2),
        'default_max_drafts': int(get_saas_setting('default_max_drafts', '2')),
        'default_max_draft_size_mb': round(int(get_saas_setting('default_max_draft_size', '1048576')) / (1024*1024), 2),
        'default_active_days': int(get_saas_setting('default_active_days', '1')),
        'android_version': get_saas_setting('android_version', '2.1.9'),
        'webapp_version': get_saas_setting('webapp_version', '2.1.9'),
        'certificate_fingerprint': get_saas_setting('certificate_fingerprint', '')
    }
    return jsonify({'success': True, 'settings': settings})


@app.route('/admin/api/users/<int:user_id>', methods=['DELETE'])
@super_admin_required
def admin_delete_user(user_id):
    """Delete a user and all their exams/files."""
    db = get_db()
    user = db.execute('SELECT username FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)

    if user['username'] == ADMIN_USERNAME:
        return error_response('Super Admin "admin" tidak dapat dihapus', 400)

    # Delete exams and PDF files owned by this user
    exams = db.execute('SELECT file_path FROM exams WHERE created_by = ?', (user_id,)).fetchall()
    for e in exams:
        try:
            file_path = safe_storage_path(e['file_path'])
            if os.path.exists(file_path):
                try:
                    os.remove(file_path)
                except Exception as e:
                    logger.warning("Failed to remove file for user %s: %s", user_id, e)
        except (ValueError, KeyError):
            logger.error("Invalid file path for user %s exam: %s", user_id, e.get('file_path'))

    db.execute('DELETE FROM exams WHERE created_by = ?', (user_id,))
    db.execute('DELETE FROM admin_users WHERE id = ?', (user_id,))
    db.commit()

    return jsonify({'success': True, 'message': f'User "{user["username"]}" beserta seluruh soalnya berhasil dihapus'})


@app.route('/admin/api/exams/<int:exam_id>/questions', methods=['GET', 'POST'])
@admin_required
def admin_exam_questions(exam_id):
    """Get or save questions configuration for an exam."""
    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    if request.method == 'GET':
        questions_raw = exam['questions_json']
        security_level = exam['security_level'] or 'medium'
        strict_mode = bool(exam['strict_mode'])
        questions = []
        if questions_raw:
            try:
                questions = json.loads(questions_raw)
            except Exception as e:
                logger.warning("Failed to parse questions_json for exam %s: %s", exam_id, e)
        identity_fields = []
        if exam['identity_fields']:
            try:
                identity_fields = json.loads(exam['identity_fields'])
            except Exception as e:
                logger.warning("Failed to parse identity_fields for exam %s: %s", exam_id, e)
        return jsonify({
            'success': True,
            'questions': questions,
            'security_level': security_level,
            'strict_mode': strict_mode,
            'identity_fields': identity_fields
        })

    else:
        # POST: Save questions configuration
        data = request.json or {}
        questions = data.get('questions', [])
        security_level = data.get('security_level', 'medium')

        if security_level not in ['medium', 'low', 'high']:
            security_level = 'medium'

        # strict_mode is derived from security_level: High = strict
        strict_mode = 1 if security_level == 'high' else 0

        # Basic validation
        if not isinstance(questions, list):
            return error_response('Format data pertanyaan tidak valid', 400)

        identity_fields = data.get('identity_fields')
        if identity_fields is not None and isinstance(identity_fields, list):
            identity_fields_json = json.dumps(identity_fields)
        else:
            identity_fields_json = exam['identity_fields'] or DEFAULT_IDENTITY_FIELDS

        # Save to database
        db.execute(
            'UPDATE exams SET questions_json = ?, security_level = ?, strict_mode = ?, identity_fields = ? WHERE id = ?',
            (json.dumps(questions), security_level, strict_mode, identity_fields_json, exam_id)
        )
        
        # Recalculate scores for all existing submissions of this exam (paginated)
        total_subs = db.execute('SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?', (exam_id,)).fetchone()['cnt']
        page_size = 100
        for offset in range(0, total_subs, page_size):
            submissions = db.execute(
                'SELECT id, answers_json FROM submissions WHERE exam_id = ? ORDER BY id LIMIT ? OFFSET ?',
                (exam_id, page_size, offset)
            ).fetchall()
            for sub in submissions:
                try:
                    sub_answers = json.loads(sub['answers_json']) if sub['answers_json'] else {}
                except Exception as e:
                    logger.warning("Failed to parse answers for submission %s: %s", sub['id'], e)
                    sub_answers = {}

                new_score = calculate_submission_score(sub_answers, questions)
                db.execute('UPDATE submissions SET score = ? WHERE id = ?', (new_score, sub['id']))
            db.commit()

        db.commit()
        return jsonify({'success': True, 'message': 'Konfigurasi soal berhasil disimpan dan nilai siswa berhasil diperbarui'})


@app.route('/admin/submissions')
@admin_required
def admin_submissions():
    """Submissions overview page for admin with pagination."""
    db = get_db()
    is_super_admin = (session.get('admin_username') == ADMIN_USERNAME)

    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 25, type=int)
    exam_filter = request.args.get('exam_id', type=int)
    per_page = min(max(per_page, 5), 100)

    conditions = []
    params = []
    if not is_super_admin:
        conditions.append('e.created_by = ?')
        params.append(session['admin_id'])
    if exam_filter:
        conditions.append('s.exam_id = ?')
        params.append(exam_filter)

    where_clause = (' WHERE ' + ' AND '.join(conditions)) if conditions else ''

    count_sql = f'SELECT COUNT(*) as cnt FROM submissions s JOIN exams e ON s.exam_id = e.id{where_clause}'
    total_submissions = db.execute(count_sql, params).fetchone()['cnt']

    base_query = (
        'SELECT s.*, e.name as exam_name, e.questions_json '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id'
    )
    order = ' ORDER BY s.created_at DESC'
    limit_offset = ' LIMIT ? OFFSET ?'
    submissions = db.execute(base_query + where_clause + order + limit_offset, params + [per_page, (page - 1) * per_page]).fetchall()

    # Compute max_score and percentage per submission
    sub_data = []
    for sub in submissions:
        sub_max_score = None
        if sub['questions_json']:
            try:
                questions = json.loads(sub['questions_json'])
                sub_max_score = sum(float(q.get('weight', 1.0)) for q in questions)
            except Exception as e:
                logger.warning("Failed to parse questions for submission %s: %s", sub['id'], e)
        sub_data.append({
            'id': sub['id'],
            'exam_id': sub['exam_id'],
            'exam_name': sub['exam_name'],
            'student_name': sub['student_name'],
            'exam_number': sub['exam_number'],
            'student_class': sub['student_class'],
            'answers_json': sub['answers_json'],
            'score': sub['score'],
            'max_score': sub_max_score,
            'score_pct': round((sub['score'] / sub_max_score * 100), 1) if (sub['score'] is not None and sub_max_score) else None,
            'start_time': sub['start_time'],
            'created_at': sub['created_at'],
            'mac_address': sub['mac_address'],
        })

    if is_super_admin:
        exams = db.execute('SELECT id, name FROM exams ORDER BY created_at DESC').fetchall()
    else:
        exams = db.execute('SELECT id, name FROM exams WHERE created_by = ? ORDER BY created_at DESC', (session['admin_id'],)).fetchall()

    total_pages = max(1, (total_submissions + per_page - 1) // per_page)

    # Calculate max_score from exam questions (if filtered by exam)
    max_score = None
    if exam_filter:
        exam_row = db.execute('SELECT questions_json FROM exams WHERE id = ?', (exam_filter,)).fetchone()
        if exam_row and exam_row['questions_json']:
            try:
                questions = json.loads(exam_row['questions_json'])
                max_score = sum(float(q.get('weight', 1.0)) for q in questions)
            except Exception as e:
                logger.warning("Failed to parse questions for exam filter %s: %s", exam_filter, e)

    local_ip = get_network_info()['display_host']

    return render_template(
        'submissions.html',
        submissions=sub_data,
        exams=exams,
        local_ip=local_ip,
        admin_user=session.get('admin_username', 'Admin'),
        active_page='submissions',
        page=page,
        per_page=per_page,
        total_pages=total_pages,
        total_submissions=total_submissions,
        exam_filter_param=exam_filter or '',
    )


@app.route('/admin/api/submissions/<int:submission_id>/detail')
@admin_required
def admin_submission_detail(submission_id):
    """Get detailed student answers compared with keys."""
    db = get_db()
    if not check_submission_ownership(db, submission_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke data ini', 403)
    sub = db.execute(
        'SELECT s.*, e.name as exam_name, e.questions_json '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id '
        'WHERE s.id = ?', (submission_id,)
    ).fetchone()
    
    if not sub:
        return error_response('Hasil ujian tidak ditemukan', 404)
        
    try:
        answers = json.loads(sub['answers_json'])
    except Exception:
        answers = {}
        
    try:
        questions = json.loads(sub['questions_json']) if sub['questions_json'] else []
    except Exception:
        questions = []
        
    evaluated_answers = evaluate_answers_detailed(answers, questions)

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
        'questions': questions,
        'evaluated_answers': evaluated_answers
    })


@app.route('/admin/api/submissions/<int:submission_id>/export_detail')
@admin_required
def admin_export_submission_detail(submission_id):
    """Export a single student's detailed answers to CSV."""
    db = get_db()
    if not check_submission_ownership(db, submission_id):
        return abort(403)
    sub = db.execute(
        'SELECT s.*, e.name as exam_name, e.questions_json '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id '
        'WHERE s.id = ?', (submission_id,)
    ).fetchone()
    
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
    cw.writerow(['Nama Ujian', _csv_safe(sub['exam_name'])])
    cw.writerow(['Nama Siswa', _csv_safe(sub['student_name'])])
    cw.writerow(['Nomor Ujian', _csv_safe(sub['exam_number'])])
    cw.writerow(['Kelas', _csv_safe(sub['student_class'])])
    cw.writerow(['Nilai Akhir', sub['score'] if sub['score'] is not None else 'Belum Dinilai'])
    cw.writerow(['Waktu Mulai', localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—'])
    cw.writerow(['Waktu Kumpul', localize_date_string(sub['created_at'], tz_offset)])
    cw.writerow(['ID Perangkat', _csv_safe(sub['mac_address'] or '—')])
    cw.writerow([])
    cw.writerow(['No. Soal', 'Tipe Soal', 'Bobot Maks', 'Jawaban Siswa', 'Kunci Jawaban', 'Status', 'Poin Didapat'])
    
    evaluated = evaluate_answers_detailed(answers, questions)

    for q in questions:
        q_num = _normalize_q_num(q.get('number', ''))
        student_ans = answers.get(q_num)
        correct_ans = q.get('key')
        q_weight = float(q.get('weight', 1.0))

        # Use centralized evaluation engine
        eval_info = evaluated.get(q_num, {})
        earned_q_weight = eval_info.get('earned', 0.0)
        status_text = eval_info.get('statusText', 'Belum Dijawab')

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
            _csv_safe(student_ans_str),
            _csv_safe(correct_ans_str),
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
        return error_response('Akses ditolak: Anda tidak memiliki akses ke data ini', 403)
    db.execute('DELETE FROM submissions WHERE id = ?', (submission_id,))
    db.commit()
    return jsonify({'success': True, 'message': 'Hasil ujian berhasil dihapus'})


@app.route('/admin/api/submissions/export')
@admin_required
def admin_export_submissions():
    """Export student submissions. Generates multi-sheet XLSX for specific exam, CSV for all."""
    exam_id = request.args.get('exam_id')
    tz_offset = request.args.get('tz_offset', type=int)

    db = get_db()
    is_super_admin = (session.get('admin_username') == ADMIN_USERNAME)

    # --- Multi-sheet XLSX export for a specific exam ---
    if exam_id:
        # Verify ownership
        exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
        if not exam:
            return error_response('Ujian tidak ditemukan', 404)
        if not is_super_admin and exam['created_by'] != session['admin_id']:
            return error_response('Akses ditolak', 403)

        submissions = db.execute(
            'SELECT * FROM submissions WHERE exam_id = ? ORDER BY student_class, student_name',
            (exam_id,)
        ).fetchall()

        try:
            questions = json.loads(exam['questions_json']) if exam['questions_json'] else []
        except Exception:
            questions = []

        try:
            return _generate_exam_xlsx(exam, submissions, questions, tz_offset)
        except ImportError:
            # Fallback to CSV if openpyxl is not installed
            pass
        except Exception as e:
            logger.error("XLSX export error: %s", e)
            # Fallback to CSV for this specific exam

        # CSV fallback for specific exam
        si = io.StringIO()
        cw = csv.writer(si)
        cw.writerow(['ID', 'Nama Ujian', 'Nama Siswa', 'Nomor Ujian', 'Kelas', 'Nilai', 'Waktu Mulai', 'Waktu Kumpul', 'ID Perangkat'])
        for row in submissions:
            cw.writerow([
                row['id'], _csv_safe(exam['name']), _csv_safe(row['student_name']), _csv_safe(row['exam_number']),
                _csv_safe(row['student_class']), row['score'] if row['score'] is not None else 'Belum Dinilai',
                localize_date_string(row['start_time'], tz_offset) if row['start_time'] else '—',
                localize_date_string(row['created_at'], tz_offset),
                _csv_safe(row['mac_address'] or '—')
            ])
        output = si.getvalue(); si.close()
        safe_exam_name = ''.join(c for c in exam['name'] if c.isalnum() or c in ' _-').strip().replace(' ', '_')
        filename = f"Hasil_Ujian_{safe_exam_name}_{datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')}.csv"
        response = send_file(io.BytesIO(output.encode('utf-8-sig')), mimetype='text/csv', as_attachment=True, download_name=filename)
        response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
        return response

    # --- CSV export for all exams ---
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
    all_submissions = db.execute(query, params).fetchall()

    si = io.StringIO()
    cw = csv.writer(si)
    cw.writerow(['ID', 'Nama Ujian', 'Nama Siswa', 'Nomor Ujian', 'Kelas', 'Nilai', 'Waktu Mulai', 'Waktu Kumpul', 'ID Perangkat'])

    for row in all_submissions:
        cw.writerow([
            row['id'], _csv_safe(row['exam_name']), _csv_safe(row['student_name']), _csv_safe(row['exam_number']),
            _csv_safe(row['student_class']), row['score'] if row['score'] is not None else 'Belum Dinilai',
            localize_date_string(row['start_time'], tz_offset) if row['start_time'] else '—',
            localize_date_string(row['created_at'], tz_offset),
            _csv_safe(row['mac_address'] or '—')
        ])

    output = si.getvalue(); si.close()
    filename = f"hasil_ujian_{datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')}.csv"
    response = send_file(io.BytesIO(output.encode('utf-8-sig')), mimetype='text/csv', as_attachment=True, download_name=filename)
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    return response


def _sanitize_xlsx(value):
    """Prevent Excel formula injection by prefixing dangerous characters with a single quote."""
    if isinstance(value, str) and value and value[0] in ('=', '+', '-', '@', '\t', '\r'):
        return "'" + value
    return value


def _sanitize_sheet_name(name):
    """Sanitize sheet name for Excel (max 31 chars, no invalid chars)."""
    for ch in ['\\', '/', '?', '*', ':', '[', ']']:
        name = name.replace(ch, '')
    return name[:31] if name else 'Sheet'


def _set_column_widths(ws, widths):
    """Set column widths for a worksheet using an ordered list of widths."""
    from openpyxl.utils import get_column_letter
    for idx, w in enumerate(widths, 1):
        ws.column_dimensions[get_column_letter(idx)].width = w


def _excel_status_color(cell, status_text):
    """Apply color formatting to a status cell based on evaluation text (Benar/Salah/Parsial)."""
    from openpyxl.styles import Font, PatternFill
    if 'Benar' in status_text:
        cell.fill = PatternFill(start_color='d1fae5', end_color='d1fae5', fill_type='solid')
        cell.font = Font(color='065f46', bold=True)
    elif 'Parsial' in status_text:
        cell.fill = PatternFill(start_color='fef3c7', end_color='fef3c7', fill_type='solid')
        cell.font = Font(color='92400e', bold=True)
    elif 'Salah' in status_text:
        cell.fill = PatternFill(start_color='fee2e2', end_color='fee2e2', fill_type='solid')
        cell.font = Font(color='991b1b', bold=True)
    else:
        cell.fill = PatternFill(start_color='f3f4f6', end_color='f3f4f6', fill_type='solid')
        cell.font = Font(color='374151')


def _build_question_detail_row(ws, row_num, q_num, q, student_answers, evaluation):
    """Build a single question detail row. Returns (q_weight, earned) tuple."""
    from openpyxl.styles import Font, PatternFill, Alignment, Border, Side

    q_weight = float(q.get('weight', 1.0))
    eval_info = evaluation.get(q_num, {})
    earned = eval_info.get('earned', 0.0)
    status_text = eval_info.get('statusText', 'Belum Dijawab')

    student_ans = student_answers.get(q_num)
    correct_ans = q.get('key')

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

    type_labels = {
        'single_choice': 'Pilihan Ganda',
        'multiple_choice': 'PG Kompleks',
        'true_false': 'Benar / Salah',
        'matching': 'Menjodohkan',
        'short_answer': 'Isian Singkat',
    }

    thin_border = Border(
        left=Side(style='thin', color='d1d5db'),
        right=Side(style='thin', color='d1d5db'),
        top=Side(style='thin', color='d1d5db'),
        bottom=Side(style='thin', color='d1d5db'),
    )
    center_align = Alignment(horizontal='center', vertical='center')
    left_align = Alignment(horizontal='left', vertical='center', wrap_text=True)

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
        cell = ws.cell(row=row_num, column=col_idx)
        cell.border = thin_border
        cell.alignment = center_align if col_idx in [1, 3, 6, 7] else left_align
        cell.value = val
        if col_idx == 6:
            _excel_status_color(cell, str(val))

    return q_weight, earned


def _build_exam_summary_sheet(ws, exam, submissions, tz_offset):
    """Build Sheet 1: Ringkasan Hasil — title, metadata, and student summary table."""
    from openpyxl.styles import Font, Alignment, PatternFill, Border, Side
    from openpyxl.utils import get_column_letter
    from datetime import datetime, timezone

    NAVY = '1e3a8a'
    GREEN_BG = 'd1fae5'
    GREEN_FG = '065f46'
    GRAY_BG = 'f3f4f6'
    GRAY_FG = '374151'

    ws.title = 'Ringkasan Hasil'
    ws.sheet_properties.tabColor = NAVY

    thin_border = Border(
        left=Side(style='thin', color='d1d5db'),
        right=Side(style='thin', color='d1d5db'),
        top=Side(style='thin', color='d1d5db'),
        bottom=Side(style='thin', color='d1d5db'),
    )
    title_font = Font(bold=True, size=14, color=NAVY)
    meta_label_font = Font(bold=True, size=11)
    meta_value_font = Font(size=11)
    header_font = Font(bold=True, color='ffffff', size=11)
    header_fill = PatternFill(start_color=NAVY, end_color=NAVY, fill_type='solid')
    header_align = Alignment(horizontal='center', vertical='center', wrap_text=True)
    center_align = Alignment(horizontal='center', vertical='center')
    left_align = Alignment(horizontal='left', vertical='center', wrap_text=True)

    def _style_header_row(ws, row, col_count):
        for col_idx in range(1, col_count + 1):
            cell = ws.cell(row=row, column=col_idx)
            cell.font = header_font
            cell.fill = header_fill
            cell.alignment = header_align
            cell.border = thin_border

    def _style_data_cell(ws, row, col, align='center'):
        cell = ws.cell(row=row, column=col)
        cell.border = thin_border
        cell.alignment = center_align if align == 'center' else left_align
        return cell

    # Title
    ws.merge_cells('A1:G1')
    title_cell = ws['A1']
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
        ws.cell(row=3 + i, column=1, value=label).font = meta_label_font
        ws.cell(row=3 + i, column=2, value=value).font = meta_value_font

    # Summary table header
    summary_headers = ['No.', 'Nomor Ujian', 'Nama Siswa', 'Kelas', 'Nilai Akhir', 'Status', 'Waktu Mulai', 'Waktu Pengumpulan', 'ID Perangkat']
    header_row = 7
    for col_idx, h in enumerate(summary_headers, 1):
        ws.cell(row=header_row, column=col_idx, value=h)
    _style_header_row(ws, header_row, len(summary_headers))

    # Summary table data
    for i, sub in enumerate(submissions):
        row_num = header_row + 1 + i
        score = sub['score']
        score_display = round(score, 2) if score is not None else 'Belum Dinilai'
        status = 'Sudah Dinilai' if score is not None else 'Belum Dinilai'

        values = [i + 1, _sanitize_xlsx(sub['exam_number']), _sanitize_xlsx(sub['student_name']),
                  _sanitize_xlsx(sub['student_class']),
                  score_display, status,
                  localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—',
                  localize_date_string(sub['created_at'], tz_offset),
                  _sanitize_xlsx(sub['mac_address'] or '—')]
        for col_idx, val in enumerate(values, 1):
            cell = _style_data_cell(ws, row_num, col_idx,
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
        ws.column_dimensions[get_column_letter(col_idx)].width = \
            max(14, len(summary_headers[col_idx - 1]) + 6)
    ws.column_dimensions['C'].width = 28
    ws.column_dimensions['G'].width = 22
    ws.column_dimensions['H'].width = 22
    ws.column_dimensions['I'].width = 24


def _build_student_detail_sheet(wb, sub, questions, tz_offset, used_names):
    """Build a per-student detail sheet with info header and question-by-question results."""
    from openpyxl.styles import Font, Alignment, PatternFill, Border, Side
    from openpyxl.utils import get_column_letter

    NAVY = '1e3a8a'
    LIGHT_BLUE = 'dbeafe'

    # Build unique sheet name
    base_name = f"{sub['exam_number']} {sub['student_name']}"
    sheet_name = _sanitize_sheet_name(base_name)
    if sheet_name in used_names:
        counter = 2
        while f"{sheet_name[:28]}_{counter}" in used_names:
            counter += 1
        sheet_name = f"{sheet_name[:28]}_{counter}"
    used_names.add(sheet_name)

    ws = wb.create_sheet(title=sheet_name)

    thin_border = Border(
        left=Side(style='thin', color='d1d5db'),
        right=Side(style='thin', color='d1d5db'),
        top=Side(style='thin', color='d1d5db'),
        bottom=Side(style='thin', color='d1d5db'),
    )
    title_font = Font(bold=True, size=14, color=NAVY)
    meta_label_font = Font(bold=True, size=11)
    meta_value_font = Font(size=11)
    header_font = Font(bold=True, color='ffffff', size=11)
    header_fill = PatternFill(start_color=NAVY, end_color=NAVY, fill_type='solid')
    header_align = Alignment(horizontal='center', vertical='center', wrap_text=True)
    center_align = Alignment(horizontal='center', vertical='center')
    left_align = Alignment(horizontal='left', vertical='center', wrap_text=True)

    def _style_header_row(ws, row, col_count):
        for col_idx in range(1, col_count + 1):
            cell = ws.cell(row=row, column=col_idx)
            cell.font = header_font
            cell.fill = header_fill
            cell.alignment = header_align
            cell.border = thin_border

    def _style_data_cell(ws, row, col, align='center'):
        cell = ws.cell(row=row, column=col)
        cell.border = thin_border
        cell.alignment = center_align if align == 'center' else left_align
        return cell

    # Student info header
    ws.merge_cells('A1:G1')
    ws['A1'].value = 'DETAIL HASIL UJIAN SISWA'
    ws['A1'].font = title_font
    ws['A1'].alignment = Alignment(horizontal='left', vertical='center')

    info_rows = [
        ('Nama Siswa:', _sanitize_xlsx(sub['student_name'])),
        ('Nomor Ujian:', _sanitize_xlsx(sub['exam_number'])),
        ('Kelas:', _sanitize_xlsx(sub['student_class'])),
        ('Nilai Akhir:', round(sub['score'], 2) if sub['score'] is not None else 'Belum Dinilai'),
        ('Waktu Mulai:', localize_date_string(sub['start_time'], tz_offset) if sub['start_time'] else '—'),
        ('Waktu Pengumpulan:', localize_date_string(sub['created_at'], tz_offset)),
        ('ID Perangkat:', _sanitize_xlsx(sub['mac_address'] or '—')),
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
    _style_header_row(ws, detail_header_row, len(detail_headers))

    try:
        student_answers = json.loads(sub['answers_json']) if sub['answers_json'] else {}
    except Exception:
        student_answers = {}

    data_row = detail_header_row + 1
    total_earned = 0.0
    total_max = 0.0

    xlsx_evaluation = evaluate_answers_detailed(student_answers, questions)

    for q in questions:
        q_num = _normalize_q_num(q.get('number', ''))
        q_weight, earned = _build_question_detail_row(
            ws, data_row, q_num, q, student_answers, xlsx_evaluation
        )
        total_max += q_weight
        total_earned += earned
        data_row += 1

    # Total row
    if questions:
        ws.merge_cells(start_row=data_row, start_column=1, end_row=data_row, end_column=2)
        total_label = _style_data_cell(ws, data_row, 1, 'center')
        total_label.value = 'TOTAL'
        total_label.font = Font(bold=True, size=11, color=NAVY)

        total_max_cell = _style_data_cell(ws, data_row, 3, 'center')
        total_max_cell.value = round(total_max, 2)
        total_max_cell.font = Font(bold=True)

        total_earned_cell = _style_data_cell(ws, data_row, 7, 'center')
        total_earned_cell.value = round(total_earned, 2)
        total_earned_cell.font = Font(bold=True, size=12, color=NAVY)

        total_fill = PatternFill(start_color=LIGHT_BLUE, end_color=LIGHT_BLUE, fill_type='solid')
        for c in range(1, 8):
            _style_data_cell(ws, data_row, c).fill = total_fill
            _style_data_cell(ws, data_row, c).border = thin_border

    # Auto-width
    _set_column_widths(ws, [10, 16, 12, 24, 24, 18, 14])


def _generate_exam_xlsx(exam, submissions, questions, tz_offset=None):
    """Generate a professionally styled multi-sheet Excel workbook for a specific exam."""
    from openpyxl import Workbook
    from datetime import datetime, timezone

    wb = Workbook()

    # Sheet 1: Summary
    _build_exam_summary_sheet(wb.active, exam, submissions, tz_offset)

    # Per-student sheets
    used_names = set()
    for sub in submissions:
        _build_student_detail_sheet(wb, sub, questions, tz_offset, used_names)

    # Save workbook to BytesIO
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
        return render_template('hasil.html',
                               exam_name='Ujian Tidak Ditemukan',
                               token=token,
                               total_students=0,
                               error=True), 404

    is_logged_in = 'admin_id' in session
    if (exam['public_results'] if exam['public_results'] is not None else 1) == 0 and not is_logged_in:
        return render_template('hasil.html',
                               exam_name=exam['name'],
                               token=token,
                               total_students=0,
                               is_disabled=True), 403

    total = db.execute(
        'SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?',
        (exam['id'],)
    ).fetchone()['cnt']

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
        return jsonify({
            'success': False,
            'message': 'Token ujian tidak valid atau ujian tidak ditemukan.'
        }), 404

    is_logged_in = 'admin_id' in session
    if (exam['public_results'] if exam['public_results'] is not None else 1) == 0 and not is_logged_in:
        return jsonify({
            'success': False,
            'message': 'Akses dinonaktifkan: Halaman hasil ujian untuk siswa dinonaktifkan oleh guru.'
        }), 403

    # Pagination params
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 100, type=int)
    per_page = min(max(per_page, 1), 500)

    # Count total submissions
    total = db.execute(
        'SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?',
        (exam['id'],)
    ).fetchone()['cnt']

    # Fetch paginated submissions
    submissions = db.execute(
        'SELECT id, student_name, exam_number, student_class, answers_json, score, start_time, created_at '
        'FROM submissions WHERE exam_id = ? ORDER BY score DESC LIMIT ? OFFSET ?',
        (exam['id'], per_page, (page - 1) * per_page)
    ).fetchall()

    # Parse questions (include keys for answer checking on client)
    questions = []
    if exam['questions_json']:
        try:
            questions = json.loads(exam['questions_json'])
        except Exception as e:
            logger.warning("Failed to parse questions_json for exam %s: %s", exam.get('id'), e)

    # Calculate max possible score
    max_score = 0
    for q in questions:
        max_score += float(q.get('weight', 1.0))

    # Build submissions list (no answer details in public API)
    subs_data = []
    for sub in submissions:
        subs_data.append({
            'id': sub['id'],
            'student_name': sub['student_name'],
            'exam_number': sub['exam_number'],
            'student_class': sub['student_class'],
            'score': sub['score'],
            'max_score': max_score if max_score > 0 else None,
            'start_time': format_iso_utc(sub['start_time']) if sub['start_time'] else None,
            'created_at': format_iso_utc(sub['created_at'])
        })

    # Determine if keys should be shown
    show_answers_enabled = (exam['show_answers'] if exam['show_answers'] is not None else 0) == 1

    # If NOT logged in AND show_answers is disabled, strip correct 'key' from questions
    if not is_logged_in and not show_answers_enabled:
        for q in questions:
            if 'key' in q:
                del q['key']

    total_pages = max(1, (total + per_page - 1) // per_page)

    return jsonify({
        'success': True,
        'exam_name': exam['name'],
        'show_answers': show_answers_enabled,
        'token': exam['token'],
        'questions': questions,
        'max_score': max_score if max_score > 0 else None,
        'submissions': subs_data,
        'pagination': {
            'page': page,
            'per_page': per_page,
            'total': total,
            'total_pages': total_pages
        }
    })


