"""EXAMVAN routes — User management (CRUD, pagination, instansi filtering)."""
import json
from datetime import datetime, timezone, timedelta

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, flash, abort
)
from werkzeug.security import generate_password_hash

from app import (
    app, get_db,
    get_saas_setting,
    admin_management_required,
    ADMIN_USERNAME,
)
from helpers import (
    format_iso_utc, parse_roles, serialize_roles, display_roles,
)
from routes._shared import error_response

from app import logger as app_logger
logger = app_logger


# ===== Users Page =====

@app.route('/admin/users')
@admin_management_required
def admin_manage_users_page():
    """HTML page for super admin/operator to manage other users and set limits."""
    db = get_db()
    admin_instansi = ''
    operator_expires_at = ''
    if session.get('is_operator'):
        row = db.execute('SELECT instansi, expires_at FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        if row:
            admin_instansi = row['instansi'] or ''
            operator_expires_at = row['expires_at'] or ''
    return render_template(
        'users.html',
        admin_user=session.get('admin_username', 'Admin'),
        admin_role=session.get('admin_role', 'guru'),
        active_page='users',
        admin_instansi=admin_instansi,
        operator_expires_at=operator_expires_at
    )


# ===== List Users =====

@app.route('/admin/api/users', methods=['GET'])
@admin_management_required
def admin_list_users():
    """List all registered users with exam count and limit. Supports pagination & search."""
    db = get_db()
    search = (request.args.get('search', '') or '').strip().lower()

    try:
        page = max(1, int(request.args.get('page', 1)))
    except (ValueError, TypeError):
        page = 1
    try:
        per_page = max(5, min(200, int(request.args.get('per_page', 10))))
    except (ValueError, TypeError):
        per_page = 10

    where_extra = ''
    params_extra = []
    if search:
        where_extra = 'AND (username LIKE ? OR whatsapp_number LIKE ?)'
        params_extra = [f'%{search}%', f'%{search}%']

    exclusion_params = []
    is_operator_viewing = session.get('is_operator', False)
    if is_operator_viewing:
        op_instansi = db.execute(
            'SELECT instansi FROM admin_users WHERE id = ?',
            (session['admin_id'],)
        ).fetchone()
        op_instansi_val = op_instansi['instansi'] if (op_instansi and op_instansi['instansi']) else ''
        exclusion_extra = " AND username != ? AND role NOT LIKE ? AND instansi = ?"
        exclusion_extra_alias = " AND u.username != ? AND u.role NOT LIKE ? AND u.instansi = ?"
        exclusion_params = [ADMIN_USERNAME, '%"operator"%', op_instansi_val]
    else:
        exclusion_extra = ' AND username != ?'
        exclusion_extra_alias = ' AND u.username != ?'
        exclusion_params = [ADMIN_USERNAME]

    total_row = db.execute(
        'SELECT COUNT(*) as cnt FROM admin_users WHERE 1=1 ' + exclusion_extra + ' ' + where_extra,
        exclusion_params + params_extra
    ).fetchone()
    total = total_row['cnt'] if total_row else 0
    total_pages = max(1, (total + per_page - 1) // per_page)
    page = min(page, total_pages) if total > 0 else 1
    offset = (page - 1) * per_page

    BASE_SELECT = (
        'SELECT u.id, u.username, u.whatsapp_number, u.status, '
        'u.max_exams, u.max_pdf_size, u.max_drafts, u.max_draft_size, '
        'u.instansi, u.role, '
        'u.expires_at, u.created_at, COUNT(e.id) as exam_count '
        'FROM admin_users u LEFT JOIN exams e ON e.created_by = u.id '
    )

    admin_user = None
    if not search or ADMIN_USERNAME.startswith(search):
        admin_user = db.execute(
            BASE_SELECT + 'WHERE u.username = ? GROUP BY u.id',
            (ADMIN_USERNAME,)
        ).fetchone()

    users = db.execute(
        BASE_SELECT
        + 'WHERE 1=1 ' + exclusion_extra_alias + ' ' + where_extra
        + 'GROUP BY u.id ORDER BY u.username ASC LIMIT ? OFFSET ?',
        exclusion_params + params_extra + [per_page, offset]
    ).fetchall()

    def _build_user(u):
        raw_role = u['role'] if 'role' in u.keys() else 'guru'
        return {
            'id': u['id'],
            'username': u['username'],
            'whatsapp_number': u['whatsapp_number'] or '',
            'status': u['status'] or 'active',
            'max_exams': u['max_exams'] if u['max_exams'] is not None else 3,
            'max_pdf_size': u['max_pdf_size'] if u['max_pdf_size'] is not None else 1048576,
            'instansi': u['instansi'] if 'instansi' in u.keys() else '',
            'roles': parse_roles(raw_role),
            'role': serialize_roles(parse_roles(raw_role)),
            'expires_at': u['expires_at'] or '',
            'exam_count': u['exam_count'],
            'created_at': format_iso_utc(u['created_at'])
        }

    user_list = []
    if admin_user and admin_user['username'] == ADMIN_USERNAME and not is_operator_viewing:
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


# ===== Create User =====

@app.route('/admin/api/users', methods=['POST'])
@admin_management_required
def admin_create_user():
    """Create a new user (teacher/pengawas/operator) with exam limit."""
    data = request.json or {}
    username = data.get('username', '').strip().lower()
    password = data.get('password', '')
    whatsapp = data.get('whatsapp_number', '').strip()
    roles_raw = data.get('roles', data.get('role', ['guru']))
    if isinstance(roles_raw, str):
        roles_raw = [roles_raw]
    if session.get('is_operator'):
        if 'operator' in roles_raw:
            return error_response('Operator tidak dapat membuat akun dengan role Operator', 400)
        op_data = get_db().execute('SELECT instansi, whatsapp_number FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
        instansi = op_data['instansi'] if (op_data and op_data['instansi']) else 'personal'
        if op_data and op_data['whatsapp_number']:
            whatsapp = op_data['whatsapp_number']
    else:
        instansi = data.get('instansi', '').strip() or 'personal'
    roles = [r for r in roles_raw if r in ('guru', 'pengawas', 'operator')] or ['guru']
    role = serialize_roles(roles)
    max_exams = data.get('max_exams', 3)
    max_pdf_size_mb = data.get('max_pdf_size_mb', 1)
    max_drafts = data.get('max_drafts', 2)
    max_draft_size_mb = data.get('max_draft_size_mb', 1)

    try:
        max_exams = int(max_exams)
    except (ValueError, TypeError):
        return error_response('Nilai max_exams tidak valid', 400)

    try:
        max_pdf_size_mb = float(max_pdf_size_mb)
    except (ValueError, TypeError):
        return error_response('Nilai max_pdf_size_mb tidak valid', 400)
    max_pdf_size = int(max_pdf_size_mb * 1024 * 1024)

    try:
        max_drafts = int(max_drafts)
    except (ValueError, TypeError):
        return error_response('Nilai max_drafts tidak valid', 400)

    try:
        max_draft_size_mb = float(max_draft_size_mb)
    except (ValueError, TypeError):
        return error_response('Nilai max_draft_size_mb tidak valid', 400)
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
        'INSERT INTO admin_users (username, password_hash, whatsapp_number, instansi, role, status, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
        (username, pw_hash, whatsapp, instansi, role, 'active', max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at)
    )
    db.commit()

    return jsonify({'success': True, 'message': f'User "{username}" berhasil dibuat'})


# ===== Edit User =====

@app.route('/admin/api/users/<int:user_id>/edit', methods=['POST'])
@admin_management_required
def admin_edit_user(user_id):
    """Edit user's max_exams limit, whatsapp, status, instansi, role, and optionally their password."""
    data = request.json or {}
    max_exams = data.get('max_exams')
    max_pdf_size_mb = data.get('max_pdf_size_mb')
    whatsapp = data.get('whatsapp_number')
    status = data.get('status')
    password = data.get('password', '').strip()
    instansi = data.get('instansi')
    role = data.get('role')

    db = get_db()
    user = db.execute('SELECT username, role, instansi FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)

    if user['username'] == ADMIN_USERNAME:
        return error_response(f'Super Admin "{ADMIN_USERNAME}" tidak dapat diubah limitnya', 400)

    if session.get('is_operator'):
        op_instansi = db.execute(
            'SELECT instansi FROM admin_users WHERE id = ?',
            (session['admin_id'],)
        ).fetchone()
        op_instansi_val = op_instansi['instansi'] if (op_instansi and op_instansi['instansi']) else ''
        if user['instansi'] != op_instansi_val:
            return error_response('Anda hanya dapat mengelola user dalam satu instansi yang sama', 400)
        target_roles = parse_roles(user['role'])
        if 'operator' in target_roles:
            return error_response('Operator tidak dapat mengelola akun dengan role Operator', 400)
        if role is not None:
            roles_raw = data.get('roles', data.get('role'))
            if isinstance(roles_raw, str):
                roles_raw = [roles_raw]
            if isinstance(roles_raw, list) and 'operator' in roles_raw:
                return error_response('Operator tidak dapat memberikan role Operator', 400)

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

    if instansi is not None:
        instansi = instansi.strip()
        if not instansi:
            return error_response('Instansi tidak boleh kosong', 400)
        db.execute('UPDATE admin_users SET instansi = ? WHERE id = ?', (instansi, user_id))

    if role is not None:
        roles_raw = data.get('roles', data.get('role'))
        if isinstance(roles_raw, str):
            roles_raw = [roles_raw]
        if isinstance(roles_raw, list):
            filtered = [r for r in roles_raw if r in ('guru', 'pengawas', 'operator')]
            if filtered:
                db.execute('UPDATE admin_users SET role = ? WHERE id = ?', (serialize_roles(filtered), user_id))

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
            try:
                datetime.strptime(exp_val, '%Y-%m-%d %H:%M:%S')
            except (ValueError, TypeError):
                return error_response('Format tanggal expiry tidak valid. Gunakan format YYYY-MM-DD HH:MM:SS', 400)
            db.execute('UPDATE admin_users SET expires_at = ? WHERE id = ?', (exp_val, user_id))
        else:
            db.execute('UPDATE admin_users SET expires_at = NULL WHERE id = ?', (user_id,))

    db.commit()

    return jsonify({'success': True, 'message': f'Pengaturan user "{user["username"]}" berhasil diperbarui'})


# ===== Verify User =====

@app.route('/admin/api/users/<int:user_id>/verify', methods=['POST'])
@admin_management_required
def admin_verify_user_manual(user_id):
    """Manually activate/verify a pending user."""
    db = get_db()
    user = db.execute('SELECT username, status, instansi FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)

    if session.get('is_operator'):
        op_instansi = db.execute(
            'SELECT instansi FROM admin_users WHERE id = ?',
            (session['admin_id'],)
        ).fetchone()
        op_instansi_val = op_instansi['instansi'] if (op_instansi and op_instansi['instansi']) else ''
        if user['instansi'] != op_instansi_val:
            return error_response('Anda hanya dapat mengelola user dalam satu instansi yang sama', 400)

    db.execute('UPDATE admin_users SET status = ?, otp_code = NULL, otp_expiry = NULL WHERE id = ?', ('active', user_id))
    db.commit()
    return jsonify({'success': True, 'message': f'User "{user["username"]}" berhasil diaktifkan secara manual'})


# ===== Toggle User Status =====

@app.route('/admin/api/users/<int:user_id>/toggle-status', methods=['POST'])
@admin_management_required
def admin_toggle_user_status(user_id):
    """Suspend or activate a user account."""
    db = get_db()
    user = db.execute('SELECT username, status, expires_at, instansi FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)

    if session.get('is_operator'):
        op_instansi = db.execute(
            'SELECT instansi FROM admin_users WHERE id = ?',
            (session['admin_id'],)
        ).fetchone()
        op_instansi_val = op_instansi['instansi'] if (op_instansi and op_instansi['instansi']) else ''
        if user['instansi'] != op_instansi_val:
            return error_response('Anda hanya dapat mengelola user dalam satu instansi yang sama', 400)

    if user['username'] == ADMIN_USERNAME:
        return error_response(f'Status Super Admin "{ADMIN_USERNAME}" tidak dapat diubah', 400)

    now = datetime.utcnow()
    new_status = 'suspended' if user['status'] == 'active' else 'active'

    if new_status == 'active':
        if user['expires_at']:
            try:
                exp = datetime.fromisoformat(user['expires_at'])
                if exp.tzinfo:
                    exp = exp.replace(tzinfo=None)
            except (ValueError, TypeError):
                exp = None
        else:
            exp = None

        if exp is None or exp <= now:
            new_exp = (now + timedelta(days=1)).strftime('%Y-%m-%d %H:%M:%S')
            db.execute('UPDATE admin_users SET status = ?, expires_at = ? WHERE id = ?',
                       (new_status, new_exp, user_id))
            msg = f'Status user "{user["username"]}" diaktifkan. Masa aktif: +1 hari (expired)'
        else:
            db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (new_status, user_id))
            msg = f'Status user "{user["username"]}" diaktifkan (masa aktif dipertahankan)'
    else:
        db.execute('UPDATE admin_users SET status = ? WHERE id = ?', (new_status, user_id))
        msg = f'Status user "{user["username"]}" dinonaktifkan'

    db.commit()
    return jsonify({'success': True, 'message': msg})


# ===== Delete User =====

@app.route('/admin/api/users/<int:user_id>', methods=['DELETE'])
@admin_management_required
def admin_delete_user(user_id):
    """Delete a user and all their exams/files."""
    db = get_db()
    user = db.execute('SELECT username, role, instansi FROM admin_users WHERE id = ?', (user_id,)).fetchone()
    if not user:
        return error_response('User tidak ditemukan', 404)

    if user['username'] == ADMIN_USERNAME:
        return error_response(f'Super Admin "{ADMIN_USERNAME}" tidak dapat dihapus', 400)

    if session.get('is_operator') and 'operator' in parse_roles(user['role']):
        return error_response('Operator tidak dapat menghapus akun dengan role Operator', 400)

    if session.get('is_operator'):
        op_instansi = db.execute(
            'SELECT instansi FROM admin_users WHERE id = ?',
            (session['admin_id'],)
        ).fetchone()
        op_instansi_val = op_instansi['instansi'] if (op_instansi and op_instansi['instansi']) else ''
        if user['instansi'] != op_instansi_val:
            return error_response('Anda hanya dapat mengelola user dalam satu instansi yang sama', 400)

    db.execute('BEGIN')
    try:
        exams = db.execute('SELECT file_path FROM exams WHERE created_by = ?', (user_id,)).fetchall()
        db.execute('DELETE FROM exams WHERE created_by = ?', (user_id,))
        db.execute('DELETE FROM admin_users WHERE id = ?', (user_id,))
        import os
        from app import safe_storage_path as _safe_path
        for e in exams:
            try:
                file_path = _safe_path(e['file_path'])
                if os.path.exists(file_path):
                    os.remove(file_path)
            except (ValueError, KeyError) as e_path:
                logger.error("Invalid file path for user %s exam: %s", user_id, e_path)
            except Exception as e_fs:
                logger.warning("Failed to remove file for user %s: %s", user_id, e_fs)
        db.commit()
    except Exception as e:
        db.rollback()
        logger.error("Error deleting user %s: %s", user_id, e)
        return error_response('Terjadi kesalahan saat menghapus user', 500)

    return jsonify({'success': True, 'message': f'User "{user["username"]}" beserta seluruh soalnya berhasil dihapus'})
