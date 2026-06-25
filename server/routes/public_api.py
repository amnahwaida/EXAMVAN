"""EXAMVAN routes — Public API endpoints (token-based, no login required)."""
import json
import html
import os
from datetime import datetime, timezone

from flask import (
    request, jsonify, send_file
)

from app import (
    app, get_db,
    get_saas_setting,
    check_rate_limit,
    safe_storage_path, STORAGE_DIR,
    DEFAULT_IDENTITY_FIELDS,
    VERSION,
)
from helpers import (
    format_iso_utc, _normalize_q_num,
    evaluate_answers_detailed, calculate_submission_score,
)
from routes._shared import (
    error_response, sanitize_student_input, _parse_identity_fields,
    REQUIRED_ANDROID_VERSION,
)

from app import logger as app_logger

logger = app_logger


# ===== Health & Time =====

@app.route('/api/health')
def api_health():
    """Health check endpoint."""
    now = datetime.now(timezone.utc)
    required_version = get_saas_setting('android_version', REQUIRED_ANDROID_VERSION)
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


# ===== Exam Listing (Public) =====

@app.route('/api/exams')
def api_exams():
    """Get list of active exams with pagination."""
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 50, type=int)
    per_page = min(max(per_page, 1), 200)

    db = get_db()

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


# ===== Exam by Token (Android App) =====

@app.route('/api/exams/token/<token>')
def api_exam_by_token(token):
    """Get exam info by token. Used by Android app."""
    required_version = get_saas_setting('android_version', REQUIRED_ANDROID_VERSION)
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
        'SELECT id, name, status, size_bytes, token, questions_json, security_level, strict_mode, identity_fields, panel_color, start_time, end_time, public_results, show_answers, created_at '
        'FROM exams WHERE token = ? AND status = ?',
        (token, 'active')
    ).fetchone()

    if not exam:
        return jsonify({
            'success': False,
            'error': 'invalid_token',
            'message': 'Token tidak valid atau ujian sudah berakhir'
        }), 404

    # Process questions (strip keys for security)
    questions_raw = exam['questions_json']
    questions = []
    if questions_raw:
        try:
            questions = json.loads(questions_raw)
            for q in questions:
                if 'key' in q:
                    del q['key']
        except Exception as e:
            logger.warning("Failed to parse questions_json for exam %s: %s", exam.get('id'), e)

    # Process identity fields
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
            'panel_color': exam['panel_color'] if exam['panel_color'] else '',
            'start_time': exam['start_time'] if exam['start_time'] else '',
            'end_time': exam['end_time'] if exam['end_time'] else '',
            'created_at': format_iso_utc(exam['created_at'])
        }
    })


# ===== Submit Exam (Android App) =====

@app.route('/api/exams/<int:exam_id>/submit', methods=['POST'])
def api_submit_exam(exam_id):
    """Receive student exam submissions and auto-grade if keys exist."""
    if not check_rate_limit(f'submit:{exam_id}', max_attempts=10, window_seconds=60):
        return error_response('Terlalu banyak percobaan submit. Silakan coba lagi nanti.', 429)

    required_version = get_saas_setting('android_version', REQUIRED_ANDROID_VERSION)
    client_version = request.headers.get('X-App-Version')
    if client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version or "v1.x"}). Silakan unduh EXAMVAN v{required_version} terbaru untuk dapat mengumpulkan jawaban.'
        }), 426

    data = request.json or {}

    # identity_data contains all dynamic identity fields
    identity_data = data.get('identity_data')
    if identity_data and isinstance(identity_data, dict):
        identity_data_sanitized = {}
        for k, v in identity_data.items():
            if isinstance(v, str):
                identity_data_sanitized[k] = html.escape(v.strip()[:200], quote=True)
            else:
                identity_data_sanitized[k] = v
        identity_data_json = json.dumps(identity_data_sanitized)
        student_name = identity_data_sanitized.get('student_name', '')
        exam_number = identity_data_sanitized.get('exam_number', '')
        student_class = identity_data_sanitized.get('student_class', '')
    else:
        identity_data_json = None
        student_name = sanitize_student_input(data.get('student_name', ''))
        exam_number = sanitize_student_input(data.get('exam_number', ''))
        student_class = sanitize_student_input(data.get('student_class', ''))

    answers = data.get('answers', {})
    start_time_raw = data.get('start_time')
    mac_address = ''.join(c for c in (data.get('mac_address', '') or '') if c.isprintable()).strip()[:100] or None

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


# ===== PDF Download (Android App) =====

@app.route('/api/exams/<int:exam_id>/pdf')
def api_exam_pdf(exam_id):
    """Stream PDF file for an exam.

    Token can be provided via:
      1. X-Exam-Token HTTP header (preferred)
      2. ?token= query param (legacy fallback)
    """
    token_param = (request.headers.get('X-Exam-Token') or '').strip().upper()
    if not token_param:
        token_param = request.args.get('token', '').strip().upper()
    if not token_param:
        return jsonify({
            'success': False,
            'error': 'token_required',
            'message': 'Token diperlukan untuk mengakses file ujian (header X-Exam-Token atau query param)'
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


# ===== Access Log (Android App) =====

@app.route('/api/exams/<int:exam_id>/access-log', methods=['POST'])
def api_access_log(exam_id):
    """Log student access event (login/heartbeat/logout) from Android."""
    required_version = get_saas_setting('android_version', REQUIRED_ANDROID_VERSION)
    client_version = request.headers.get('X-App-Version')
    if client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version or "v1.x"}). Silakan unduh EXAMVAN v{required_version} terbaru.'
        }), 426

    data = request.json or {}
    event = data.get('event', 'heartbeat')
    if event not in ('login', 'heartbeat', 'logout'):
        return error_response('Event tidak valid', 400)

    mac_address = ''.join(c for c in (data.get('mac_address', '') or '') if c.isprintable()).strip()[:100] or 'unknown'
    student_name = data.get('student_name', '')[:200] or None
    exam_number = data.get('exam_number', '')[:100] or None
    student_class = data.get('student_class', '')[:100] or None
    device_info = data.get('device_info', '')[:200] or None
    ip_address = request.remote_addr or ''

    db = get_db()
    exam = db.execute('SELECT id FROM exams WHERE id = ? AND status = ?', (exam_id, 'active')).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # Find matching submission if it exists
    submission = None
    if mac_address and mac_address != 'unknown':
        submission = db.execute(
            'SELECT id FROM submissions WHERE exam_id = ? AND mac_address = ? ORDER BY id DESC LIMIT 1',
            (exam_id, mac_address)
        ).fetchone()

    db.execute(
        'INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info) '
        'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
        (exam_id, submission['id'] if submission else None, mac_address, student_name, exam_number, student_class, event, ip_address, device_info)
    )
    db.commit()

    return jsonify({'success': True, 'message': 'Access logged'})
