"""EXAMVAN routes — Pengawas (exam supervisor) monitoring page."""
import json

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, flash, abort
)

from app import (
    app, get_db,
    admin_required,
)
from helpers import format_iso_utc
from routes._shared import error_response

from app import logger as app_logger
logger = app_logger


# ===== Pengawas Page =====

@app.route('/admin/pengawas')
@admin_required
def admin_pengawas():
    """Dedicated monitoring page for exam supervisors."""
    if not session.get('is_super_admin') and not session.get('is_operator') and 'pengawas' not in session.get('user_roles', []):
        flash('Akses ditolak. Halaman ini khusus untuk Pengawas.', 'error')
        return redirect(url_for('admin_dashboard'))
    return render_template(
        'pengawas.html',
        admin_user=session.get('admin_username', 'Admin'),
        admin_role=session.get('admin_role', 'guru'),
        active_page='pengawas'
    )


# ===== Pengawas: Detail Monitoring Page untuk Satu Ujian =====

@app.route('/admin/pengawas/<int:exam_id>')
@admin_required
def admin_pengawas_detail(exam_id):
    """Dedicated monitoring page for a single exam."""
    db = get_db()
    user_id = session['admin_id']
    is_privileged = session.get('is_super_admin') or session.get('is_operator')

    # Verify access
    if not is_privileged:
        assignment = db.execute(
            'SELECT id FROM exam_pengawas WHERE exam_id = ? AND user_id = ?',
            (exam_id, user_id)
        ).fetchone()
        if not assignment:
            flash('Akses ditolak: Anda tidak ditugaskan sebagai pengawas ujian ini.', 'error')
            return redirect(url_for('admin_pengawas'))

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        flash('Ujian tidak ditemukan.', 'error')
        return redirect(url_for('admin_pengawas'))

    return render_template(
        'pengawas_detail.html',
        exam=exam,
        admin_user=session.get('admin_username', 'Admin'),
        admin_role=session.get('admin_role', 'guru'),
        active_page='pengawas'
    )


# ===== Pengawas: List Assigned Exams =====

@app.route('/admin/api/pengawas/exams')
@admin_required
def admin_pengawas_exams():
    """List exams assigned to current user as pengawas, with pagination, search & submission stats."""
    db = get_db()
    user_id = session['admin_id']
    is_privileged = session.get('is_super_admin') or session.get('is_operator')

    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 10, type=int)
    search = request.args.get('search', '').strip()
    per_page = min(max(per_page, 5), 50)

    # Build conditions
    conditions = []
    params = []
    if not is_privileged:
        conditions.append('e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = ?)')
        params.append(user_id)
    if search:
        conditions.append('(e.name LIKE ? OR e.token LIKE ? OR u.username LIKE ?)')
        s = f'%{search}%'
        params.extend([s, s, s])

    where_clause = (' WHERE ' + ' AND '.join(conditions)) if conditions else ''

    # Count total
    count_sql = 'SELECT COUNT(*) as cnt FROM exams e JOIN admin_users u ON e.created_by = u.id' + where_clause
    total = db.execute(count_sql, params).fetchone()['cnt']

    total_pages = max(1, (total + per_page - 1) // per_page)

    # Fetch page
    exams = db.execute(
        'SELECT e.*, u.username as creator_name, '
        '(SELECT COUNT(*) FROM submissions s WHERE s.exam_id = e.id) as sub_count '
        'FROM exams e '
        'JOIN admin_users u ON e.created_by = u.id '
        + where_clause +
        ' ORDER BY e.created_at DESC LIMIT ? OFFSET ?',
        params + [per_page, (page - 1) * per_page]
    ).fetchall()

    exam_list = []
    for ex in exams:
        submitted = db.execute(
            "SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ? AND answers_json IS NOT NULL AND answers_json != ''",
            (ex['id'],)
        ).fetchone()['cnt']
        exam_list.append({
            'id': ex['id'],
            'name': ex['name'],
            'token': ex['token'],
            'status': ex['status'],
            'start_time': ex['start_time'] or '',
            'end_time': ex['end_time'] or '',
            'creator_name': ex['creator_name'],
            'total_students': ex['sub_count'],
            'submitted_count': submitted,
            'created_at': format_iso_utc(ex['created_at']),
        })

    # Overall stats (across all matching exams, ignoring pagination)
    all_match = db.execute(
        'SELECT e.id, e.status FROM exams e JOIN admin_users u ON e.created_by = u.id' + where_clause,
        params
    ).fetchall()
    total_exams_count = len(all_match)
    active_count = sum(1 for r in all_match if r['status'] == 'active')
    if all_match:
        ids = [r['id'] for r in all_match]
        ph = ','.join('?' * len(ids))
        row = db.execute(
            f'SELECT COUNT(*) as c FROM submissions WHERE exam_id IN ({ph})', ids
        ).fetchone()
        total_students = row['c'] if row else 0
        row2 = db.execute(
            f"SELECT COUNT(*) as c FROM submissions WHERE exam_id IN ({ph}) AND answers_json IS NOT NULL AND answers_json != ''",
            ids
        ).fetchone()
        total_submitted = row2['c'] if row2 else 0
    else:
        total_students = 0
        total_submitted = 0

    return jsonify({
        'success': True,
        'exams': exam_list,
        'is_privileged': is_privileged,
        'page': page,
        'per_page': per_page,
        'total': total,
        'total_pages': total_pages,
        'stats': {
            'total_exams': total_exams_count,
            'active_exams': active_count,
            'total_students': total_students,
            'total_submitted': total_submitted,
        },
    })


# ===== Pengawas: Exam Submissions =====

@app.route('/admin/api/pengawas/exams/<int:exam_id>/submissions')
@admin_required
def admin_pengawas_exam_submissions(exam_id):
    """List all submissions for an exam (pengawas view)."""
    db = get_db()
    user_id = session['admin_id']

    # Verify this user is assigned as pengawas for this exam
    assignment = db.execute(
        'SELECT id FROM exam_pengawas WHERE exam_id = ? AND user_id = ?',
        (exam_id, user_id)
    ).fetchone()
    if not assignment and not session.get('is_super_admin') and not session.get('is_operator'):
        return error_response('Akses ditolak: Anda tidak ditugaskan sebagai pengawas ujian ini', 403)

    exam = db.execute('SELECT id, name FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    search = request.args.get('search', '').strip()
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 20, type=int)
    per_page = min(max(per_page, 5), 100)

    base_q = 'FROM submissions WHERE exam_id = ?'
    params = [exam_id]
    if search:
        base_q += ' AND (student_name LIKE ? OR mac_address LIKE ? OR student_class LIKE ? OR exam_number LIKE ?)'
        s = f'%{search}%'
        params.extend([s, s, s, s])

    total = db.execute('SELECT COUNT(*) as cnt ' + base_q, params).fetchone()['cnt']
    total_pages = max(1, (total + per_page - 1) // per_page)

    submissions = db.execute(
        'SELECT id, student_name, exam_number, student_class, answers_json, score, start_time, created_at, mac_address, identity_data '
        + base_q + ' ORDER BY student_name ASC LIMIT ? OFFSET ?',
        params + [per_page, (page - 1) * per_page]
    ).fetchall()

    subs_data = []
    for sub in submissions:
        id_data = None
        if sub['identity_data']:
            try:
                id_data = json.loads(sub['identity_data'])
            except Exception:
                id_data = None

        student_identifier = sub['mac_address'] or ''
        access_logs = []
        first_access = sub['start_time'] or ''
        last_access = sub['created_at'] or ''

        if student_identifier:
            log_rows = db.execute(
                'SELECT event, ip_address, device_info, created_at, '
                'student_name, exam_number, student_class '
                'FROM student_access_logs '
                'WHERE exam_id = ? AND student_identifier = ? '
                'ORDER BY created_at ASC',
                (exam_id, student_identifier)
            ).fetchall()

            for log in log_rows:
                access_logs.append({
                    'event': log['event'],
                    'ip_address': log['ip_address'] or '',
                    'device_info': log['device_info'] or '',
                    'created_at': format_iso_utc(log['created_at']),
                    'student_name': log['student_name'] or '',
                    'exam_number': log['exam_number'] or '',
                    'student_class': log['student_class'] or '',
                })

            if access_logs:
                first_log = access_logs[0]
                last_log = access_logs[-1]
                first_access = first_log['created_at'] if first_log['event'] in ('login', 'heartbeat') else first_access
                last_access = last_log['created_at']

        subs_data.append({
            'id': sub['id'],
            'student_name': sub['student_name'],
            'exam_number': sub['exam_number'],
            'student_class': sub['student_class'],
            'identity_data': id_data,
            'submitted': sub['answers_json'] is not None and sub['answers_json'] != '',
            'start_time': sub['start_time'] or '',
            'created_at': sub['created_at'] or '',
            'first_access_at': first_access,
            'last_access_at': last_access,
            'mac_address': sub['mac_address'] or '',
            'score': sub['score'],
            'access_logs': access_logs,
        })

    # Total stats (across ALL submissions for this exam, ignoring search/pagination)
    all_total = db.execute(
        'SELECT COUNT(*) as c FROM submissions WHERE exam_id = ?', (exam_id,)
    ).fetchone()['c']
    all_submitted = db.execute(
        "SELECT COUNT(*) as c FROM submissions WHERE exam_id = ? AND answers_json IS NOT NULL AND answers_json != ''",
        (exam_id,)
    ).fetchone()['c']
    all_in_progress = db.execute(
        "SELECT COUNT(*) as c FROM submissions WHERE exam_id = ? AND (answers_json IS NULL OR answers_json = '') AND start_time IS NOT NULL",
        (exam_id,)
    ).fetchone()['c']
    all_not_started = all_total - all_submitted - all_in_progress

    return jsonify({
        'success': True,
        'exam_name': exam['name'],
        'submissions': subs_data,
        'page': page,
        'per_page': per_page,
        'total': total,
        'total_pages': total_pages,
        'stats': {
            'total': all_total,
            'submitted': all_submitted,
            'active': all_in_progress,
            'not_started': all_not_started,
        },
    })
