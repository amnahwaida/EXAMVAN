"""EXAMVAN routes — Authentication (login, register, OTP, logout)."""
import secrets
import string
import hmac
from datetime import datetime, timezone, timedelta

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, flash
)
from werkzeug.security import generate_password_hash

from app import (
    app, get_db, _verify_password,
    get_saas_setting, send_whatsapp,
    csrf_required, check_rate_limit,
    ADMIN_USERNAME,
)
from helpers import parse_roles, display_roles
from routes._shared import error_response, sanitize_student_input


# ===== Admin Login =====

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
            'SELECT id, username, status, password_hash, role FROM admin_users WHERE username = ?',
            (username,)
        ).fetchone()

        if user and _verify_password(password, user['password_hash']):
            # Auto-upgrade legacy SHA-256 hash to werkzeug scrypt
            if not user['password_hash'].startswith(('scrypt:', 'pbkdf2:')):
                new_hash = generate_password_hash(password)
                db.execute('UPDATE admin_users SET password_hash = ? WHERE id = ?', (new_hash, user['id']))
                db.commit()

            if user['status'] == 'pending_otp':
                flash('Pendaftaran Anda membutuhkan konfirmasi OTP WhatsApp. Silakan verifikasi.', 'warning')
                return redirect(url_for('verify_otp', username=user['username']))
            elif user['status'] == 'suspended':
                flash('Akun Anda telah dinonaktifkan oleh administrator.', 'error')
                return redirect(url_for('admin_login'))

            # Check account expiry (skip for super admin)
            if user['username'] != ADMIN_USERNAME:
                user_full = db.execute('SELECT expires_at FROM admin_users WHERE id = ?', (user['id'],)).fetchone()
                if user_full and user_full['expires_at']:
                    expires_at = datetime.strptime(user_full['expires_at'], '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
                    if datetime.now(timezone.utc) > expires_at:
                        flash('Masa aktif akun Anda telah habis. Silakan hubungi administrator.', 'error')
                        return redirect(url_for('admin_login'))

            session['admin_id'] = user['id']
            session['admin_username'] = user['username']
            db_role = user['role'] if 'role' in user.keys() else 'guru'
            user_roles = parse_roles(db_role)
            is_admin_user = (user['username'] == ADMIN_USERNAME or user['username'] == 'admin' or 'superadmin' in user_roles)
            is_operator_user = 'operator' in user_roles
            if is_admin_user:
                session['admin_role'] = 'superadmin'
                session['is_super_admin'] = True
                session['is_operator'] = False
                if db_role != 'superadmin':
                    try:
                        db.execute('UPDATE admin_users SET role = ? WHERE id = ?', ('superadmin', user['id']))
                        db.commit()
                    except Exception:
                        pass
            elif is_operator_user:
                session['admin_role'] = 'operator'
                session['is_super_admin'] = False
                session['is_operator'] = True
                session['user_roles'] = user_roles
            else:
                session['admin_role'] = display_roles(db_role)
                session['is_super_admin'] = False
                session['is_operator'] = False
                session['user_roles'] = user_roles
            # Store expires_at in session cache (skip for admin/operator users)
            if not is_admin_user and not is_operator_user:
                user_full = db.execute('SELECT expires_at FROM admin_users WHERE id = ?', (user['id'],)).fetchone()
                if user_full and user_full['expires_at']:
                    session['expires_at'] = user_full['expires_at']
            # Regenerate session ID to prevent session fixation
            session.modified = True
            return redirect(url_for('admin_dashboard'))
        else:
            flash('Username atau password salah', 'error')

    return render_template('login.html')


# ===== Registration =====

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
            otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')

            db.execute(
                'INSERT INTO admin_users (username, password_hash, whatsapp_number, status, otp_code, otp_expiry, instansi, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) '
                'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (username, pw_hash, whatsapp, 'pending_otp', otp, otp_expiry, 'personal', default_exams, default_pdf, default_drafts, default_draft_size, expires_at)
            )
            db.commit()

            template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
            message = template.replace('{otp}', otp)
            send_whatsapp(whatsapp, message)

            flash('Registrasi berhasil! Masukkan kode OTP yang dikirim ke nomor WhatsApp Anda.', 'warning')
            return redirect(url_for('verify_otp', username=username))
        else:
            db.execute(
                "INSERT INTO admin_users (username, password_hash, whatsapp_number, status, instansi, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (username, pw_hash, whatsapp, 'active', 'personal', default_exams, default_pdf, default_drafts, default_draft_size, expires_at)
            )
            db.commit()

            flash('Registrasi berhasil! Silakan masuk dengan akun Anda.', 'success')
            return redirect(url_for('admin_login'))

    return render_template('register.html')


# ===== OTP Verification =====

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

        if not hmac.compare_digest(str(user['otp_code'] or ''), str(otp_input)):
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


# ===== Resend OTP =====

@app.route('/resend-otp', methods=['POST'])
@csrf_required
def resend_otp():
    """Resend OTP code to user's registered WhatsApp number."""
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
    otp_expiry = (datetime.now(timezone.utc) + timedelta(minutes=5)).strftime('%Y-%m-%d %H:%M:%S')

    db.execute(
        'UPDATE admin_users SET otp_code = ?, otp_expiry = ? WHERE id = ?',
        (otp, otp_expiry, user['id'])
    )
    db.commit()

    template = get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.')
    message = template.replace('{otp}', otp)
    send_whatsapp(user['whatsapp_number'], message)

    return jsonify({'success': True, 'message': 'Kode OTP baru berhasil dikirim via WhatsApp'})


# ===== Logout =====

@app.route('/admin/logout')
def admin_logout():
    """Admin logout."""
    session.clear()
    return redirect(url_for('admin_login'))
