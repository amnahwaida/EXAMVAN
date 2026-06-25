"""EXAMVAN routes — Admin dashboard, upload, stats, settings."""
import os
import json
import re
from datetime import datetime, timezone, timedelta

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, flash, abort
)
from werkzeug.utils import secure_filename
from werkzeug.security import generate_password_hash

from app import (
    app, get_db,
    get_saas_setting, set_saas_setting,
    generate_token, _verify_password,
    admin_required, super_admin_required, csrf_required, check_rate_limit,
    check_exam_ownership,
    get_network_info, get_storage_stats,
    STORAGE_DIR, MAX_FILE_SIZE, BASE_DIR,
    DEFAULT_IDENTITY_FIELDS, ADMIN_USERNAME, VERSION,
)
from helpers import (
    format_iso_utc, get_local_ip,
    parse_roles,
)
from routes._shared import (
    error_response, success_response, mask_token,
    validate_pdf_upload, days_until_expiry,
)

from app import logger as app_logger
logger = app_logger


# ===== Landing Page =====

@app.route('/')
def index():
    """Render landing page."""
    return render_template('index.html')


# ===== Admin Dashboard =====

@app.route('/admin/dashboard')
@admin_required
def admin_dashboard():
    """Admin dashboard page with pagination & search."""
    if not session.get('is_super_admin') and not session.get('is_operator') and 'guru' not in session.get('user_roles', []):
        flash('Akses ditolak. Halaman ini khusus untuk Guru.', 'error')
        return redirect(url_for('admin_pengawas'))
    db = get_db()
    is_super_admin = session.get('is_super_admin', False)
    is_operator = session.get('is_operator', False)

    # Pagination & search params
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 20, type=int)
    search = request.args.get('search', '').strip()
    per_page = min(max(per_page, 5), 100)

    # Build query conditions
    conditions = []
    params = []
    if is_super_admin:
        pass
    elif is_operator:
        op_row = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        op_instansi = op_row['instansi'] if op_row else ''
        conditions.append('(e.created_by = ? OR e.created_by IN (SELECT id FROM admin_users WHERE instansi = ?))')
        params.extend([session['admin_id'], op_instansi])
    else:
        user_role_row = db.execute('SELECT role FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        user_roles = parse_roles(user_role_row['role']) if user_role_row else ['guru']
        is_pengawas = 'pengawas' in user_roles
        is_guru = 'guru' in user_roles
        if is_pengawas and is_guru:
            conditions.append('(e.created_by = ? OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?))')
            params.extend([session['admin_id'], session['admin_id']])
        elif is_pengawas:
            conditions.append('e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?)')
            params.append(session['admin_id'])
        else:
            conditions.append('e.created_by = ?')
            params.append(session['admin_id'])

    if search:
        conditions.append('(e.name LIKE ? OR e.token LIKE ? OR u.username LIKE ?)')
        search_param = f'%{search}%'
        params.extend([search_param, search_param, search_param])

    where_clause = (' WHERE ' + ' AND '.join(conditions)) if conditions else ''

    # Count total matching exams
    count_sql = 'SELECT COUNT(*) as cnt FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id' + where_clause
    total = db.execute(count_sql, params).fetchone()['cnt']

    # Fetch paginated exams
    base_query = (
        'SELECT e.*, u.username as creator_name, '
        'd.username as delegated_name, '
        '(SELECT COUNT(*) FROM submissions s WHERE s.exam_id = e.id) as sub_count '
        'FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id '
        'LEFT JOIN admin_users d ON e.delegated_to = d.id'
    )
    order = ' ORDER BY e.created_at DESC'
    limit_offset = ' LIMIT ? OFFSET ?'
    exams = db.execute(base_query + where_clause + order + limit_offset, params + [per_page, (page - 1) * per_page]).fetchall()

    # Fetch pengawas for each exam
    exam_pengawas_map = {}
    if exams:
        exam_ids = [e['id'] for e in exams]
        placeholders = ','.join('?' * len(exam_ids))
        pengawas_rows = db.execute(
            f'SELECT ep.exam_id, u.username FROM exam_pengawas ep JOIN admin_users u ON ep.user_id = u.id WHERE ep.exam_id IN ({placeholders}) ORDER BY u.username',
            exam_ids
        ).fetchall()
        for row in pengawas_rows:
            eid = row['exam_id']
            if eid not in exam_pengawas_map:
                exam_pengawas_map[eid] = []
            exam_pengawas_map[eid].append(row['username'])

    # Calculate stats: count active/inactive across ALL visible exams (same scope as table, without search)
    stats_where = ''
    stats_params = []
    if is_super_admin:
        pass
    elif is_operator:
        op_row = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        op_instansi = op_row['instansi'] if op_row else ''
        stats_where = ' WHERE (created_by = ? OR created_by IN (SELECT id FROM admin_users WHERE instansi = ?))'
        stats_params = [session['admin_id'], op_instansi]
    else:
        user_role_row = db.execute('SELECT role FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        user_roles = parse_roles(user_role_row['role']) if user_role_row else ['guru']
        is_pengawas = 'pengawas' in user_roles
        is_guru = 'guru' in user_roles
        if is_pengawas and is_guru:
            stats_where = ' WHERE (created_by = ? OR id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?))'
            stats_params = [session['admin_id'], session['admin_id']]
        elif is_pengawas:
            stats_where = ' WHERE id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?)'
            stats_params = [session['admin_id']]
        else:
            stats_where = ' WHERE created_by = ?'
            stats_params = [session['admin_id']]

    if stats_params:
        stats_total = db.execute(f'SELECT COUNT(*) as cnt FROM exams{stats_where}', stats_params).fetchone()['cnt']
        active = db.execute(f'SELECT COUNT(*) as cnt FROM exams{stats_where} AND status = ?', stats_params + ['active']).fetchone()['cnt']
    else:
        stats_total = db.execute('SELECT COUNT(*) as cnt FROM exams').fetchone()['cnt']
        active = db.execute('SELECT COUNT(*) as cnt FROM exams WHERE status = ?', ['active']).fetchone()['cnt']
    inactive = stats_total - active

    if is_super_admin or is_operator:
        storage_bytes = get_storage_stats()
    else:
        if stats_params:
            storage_bytes = db.execute(f'SELECT COALESCE(SUM(size_bytes), 0) as total FROM exams e{stats_where}', stats_params).fetchone()['total']
        else:
            storage_bytes = db.execute('SELECT COALESCE(SUM(size_bytes), 0) as total FROM exams').fetchone()['total']

    total_pages = max(1, (total + per_page - 1) // per_page)

    net_info = get_network_info()

    # Get per-user limits
    if is_super_admin or is_operator:
        user_max_pdf = MAX_FILE_SIZE
        user_max_exams = '∞'
        account_expires = None
    else:
        user_row = db.execute('SELECT max_exams, max_pdf_size, expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        user_max_pdf = user_row['max_pdf_size'] if (user_row and user_row['max_pdf_size'] is not None) else 1048576
        user_max_exams = user_row['max_exams'] if (user_row and user_row['max_exams'] is not None) else 3
        account_expires = user_row['expires_at'] if user_row else None

    days_rem = days_until_expiry(account_expires)

    resp = app.make_response(render_template('dashboard.html',
        exams=exams,
        exam_pengawas_map=exam_pengawas_map,
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
        admin_role=session.get('admin_role', 'guru'),
        admin_id=session.get('admin_id', 0),
        max_size_mb=round(user_max_pdf / (1024 * 1024), 1),
        max_exams=user_max_exams,
        account_expires=account_expires,
        days_remaining=days_rem,
        active_page='dashboard',
        page=page,
        per_page=per_page,
        total_pages=total_pages,
        total_exams=total,
        search=search,
        search_active=bool(search),
    ))
    resp.headers['Cache-Control'] = 'no-cache, no-store, must-revalidate'
    resp.headers['Pragma'] = 'no-cache'
    resp.headers['Expires'] = '0'
    return resp


# ===== Upload Exam PDF =====

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

    file_data = file.read()
    is_valid, error_msg = validate_pdf_upload(file_data, file.filename, file.content_type)
    if not is_valid:
        return error_response(error_msg, 400)

    db = get_db()
    custom_token = request.form.get('custom_token', '').strip().upper()

    # Check per-user limits (for non-super admin/operator users)
    if not session.get('is_super_admin') and not session.get('is_operator'):
        user = db.execute('SELECT max_exams, max_pdf_size FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        exam_limit = user['max_exams'] if (user and user['max_exams'] is not None) else 3
        pdf_limit = user['max_pdf_size'] if (user and user['max_pdf_size'] is not None) else 1048576

        if len(file_data) > pdf_limit:
            if pdf_limit == 0:
                msg = 'Anda tidak memiliki izin untuk mengupload PDF. Silakan hubungi Super Admin untuk mendapatkan akses.'
            else:
                pdf_limit_mb = round(pdf_limit / (1024 * 1024), 2)
                msg = f'Ukuran file melebihi batas akun Anda ({pdf_limit_mb}MB). Silakan hubungi Super Admin untuk menaikkan limit.'
            return jsonify({'success': False, 'message': msg}), 403

        current_count = db.execute('SELECT COUNT(*) as count FROM exams WHERE created_by = ?', (session['admin_id'],)).fetchone()['count']
        if current_count >= exam_limit:
            return jsonify({
                'success': False,
                'message': f'Batas pembuatan ujian tercapai. Batas akun Anda adalah {exam_limit} ujian. Silakan hubungi Super Admin untuk menaikkan limit.'
            }), 403

    if custom_token:
        if not re.match(r'^[A-Z0-9]{8}$', custom_token):
            return error_response('Token kustom harus terdiri dari 8 karakter alfanumerik', 400)
        existing = db.execute('SELECT id FROM exams WHERE token = ?', (custom_token,)).fetchone()
        if existing:
            return error_response('Token kustom sudah digunakan oleh ujian lain', 400)
        token = custom_token
    else:
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


# ===== Stats API =====

@app.route('/admin/api/stats')
@admin_required
def admin_stats():
    """Get dashboard statistics (scoped to user's visible exams)."""
    db = get_db()
    is_super_admin = session.get('is_super_admin', False)
    is_operator = session.get('is_operator', False)
    admin_id = session['admin_id']

    if is_super_admin:
        exams = db.execute('SELECT status, size_bytes FROM exams').fetchall()
        storage_bytes = get_storage_stats()
    elif is_operator:
        op_row = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (admin_id,)).fetchone()
        op_instansi = op_row['instansi'] if op_row else ''
        exams = db.execute(
            'SELECT status, size_bytes FROM exams WHERE created_by = ? OR created_by IN (SELECT id FROM admin_users WHERE instansi = ?)',
            (admin_id, op_instansi)
        ).fetchall()
        storage_bytes = get_storage_stats()
    else:
        user_role_row = db.execute('SELECT role FROM admin_users WHERE id = ?', (admin_id,)).fetchone()
        user_roles = parse_roles(user_role_row['role']) if user_role_row else ['guru']
        is_pengawas = 'pengawas' in user_roles
        is_guru = 'guru' in user_roles

        if is_pengawas and is_guru:
            exams = db.execute(
                'SELECT status, size_bytes FROM exams WHERE created_by = ? OR id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?)',
                (admin_id, admin_id)
            ).fetchall()
        elif is_pengawas:
            exams = db.execute(
                'SELECT status, size_bytes FROM exams WHERE id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?)',
                (admin_id,)
            ).fetchall()
        else:
            exams = db.execute(
                'SELECT status, size_bytes FROM exams WHERE created_by = ?',
                (admin_id,)
            ).fetchall()
        storage_bytes = sum(e['size_bytes'] for e in exams if e['size_bytes'] is not None)

    total = len(exams)
    active = sum(1 for e in exams if e['status'] == 'active')

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


# ===== Change Password =====

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


# ===== SaaS Settings (Super Admin) =====

def _handle_saas_settings_get():
    """GET handler for SaaS settings."""
    settings = {
        'wa_verification_enabled': get_saas_setting('wa_verification_enabled', '0') == '1',
        'wa_api_token': mask_token(get_saas_setting('wa_api_token', '')),
        'wa_otp_template': get_saas_setting('wa_otp_template', 'Kode OTP EXAMVAN Anda: {otp}. Berlaku selama 5 menit.'),
        'default_max_exams': int(get_saas_setting('default_max_exams', '3')),
        'default_max_pdf_size_mb': round(int(get_saas_setting('default_max_pdf_size', '1048576')) / (1024*1024), 2),
        'default_max_drafts': int(get_saas_setting('default_max_drafts', '2')),
        'default_max_draft_size_mb': round(int(get_saas_setting('default_max_draft_size', '1048576')) / (1024*1024), 2),
        'default_active_days': int(get_saas_setting('default_active_days', '1')),
        'android_version': get_saas_setting('android_version', '2.2.0'),
        'webapp_version': get_saas_setting('webapp_version', '2.2.0'),
        'certificate_fingerprint': get_saas_setting('certificate_fingerprint', '')
    }
    return jsonify({'success': True, 'settings': settings})


def _handle_saas_settings_post(data):
    """POST handler for SaaS settings."""
    wa_enabled = '1' if data.get('wa_verification_enabled') else '0'
    wa_token = data.get('wa_api_token', '').strip()
    if wa_token.startswith('*'):
        wa_token = get_saas_setting('wa_api_token', '')
    wa_template = data.get('wa_otp_template', '').strip()
    default_exams = data.get('default_max_exams', '3')
    default_pdf_size_mb = data.get('default_max_pdf_size_mb', '1')
    default_active_days = data.get('default_active_days', '1')
    default_drafts = data.get('default_max_drafts', '2')
    default_draft_size_mb = data.get('default_max_draft_size_mb', '1')
    android_version = data.get('android_version', '2.2.0').strip()
    webapp_version = data.get('webapp_version', '2.2.0').strip()

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


@app.route('/admin/api/saas-settings', methods=['GET', 'POST'])
@super_admin_required
def admin_saas_settings():
    """Get or update SaaS settings."""
    if request.method == 'POST':
        data = request.json or {}
        return _handle_saas_settings_post(data)
    return _handle_saas_settings_get()
