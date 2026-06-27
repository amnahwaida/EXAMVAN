"""EXAMVAN routes — Public API endpoints (token-based, no login required)."""
import json
import html
import os
from datetime import datetime, timezone

from flask import request, jsonify, send_file

from app import (
    app, get_db,
    get_saas_setting,
    check_rate_limit,
    safe_storage_path, STORAGE_DIR,
    DEFAULT_IDENTITY_FIELDS,
    VERSION, cached, redis_client,
)
from helpers import format_iso_utc
from routes._shared import (
    error_response, sanitize_student_input, REQUIRED_ANDROID_VERSION,
)
import logging
logger = logging.getLogger('examvan')


# ===== Health =====

@app.route('/api/health')
def api_health():
    """Health check endpoint."""
    return jsonify({
        'success': True,
        'status': 'healthy',
        'version': VERSION,
        'certificate_fingerprint': get_saas_setting('certificate_fingerprint', ''),
    })


@app.route('/api/time')
def api_time():
    """Return server time for time synchronization."""
    return jsonify({
        'success': True,
        'server_time': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
        'timezone': 'UTC',
    })


# ===== Exam List (Android) =====

@app.route('/api/exams')
@cached(ttl=30)
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


# ===== Exam by Token (Android) =====

@app.route('/api/exams/token/<token>')
def api_exam_by_token(token):
    """Get exam details by token."""
    required_version = get_saas_setting('android_version', REQUIRED_ANDROID_VERSION)
    client_version = request.headers.get('X-App-Version')
    if client_version and client_version != required_version:
        return jsonify({
            'success': False,
            'error': 'upgrade_required',
            'message': f'Versi aplikasi Anda usang ({client_version}). Silakan unduh EXAMVAN v{required_version} terbaru.'
        }), 426

    db = get_db()
    exam = db.execute(
        'SELECT * FROM exams WHERE token = ?',
        (token,)
    ).fetchone()

    if not exam:
        return jsonify({
            'success': False,
            'message': 'Token tidak valid'
        }), 404

    identity_fields = exam['identity_fields'] or DEFAULT_IDENTITY_FIELDS
    try:
        identity_fields = json.loads(identity_fields) if isinstance(identity_fields, str) else identity_fields
    except (json.JSONDecodeError, TypeError):
        identity_fields = json.loads(DEFAULT_IDENTITY_FIELDS)

    return jsonify({
        'success': True,
        'exam': {
            'id': exam['id'],
            'name': exam['name'],
            'status': exam['status'],
            'security_level': exam.get('security_level', 'medium'),
            'strict_mode': bool(exam.get('strict_mode', False)),
            'public_results': exam.get('public_results', 1),
            'show_answers': exam.get('show_answers', 0),
            'identity_fields': identity_fields,
            'panel_color': exam.get('panel_color', '#6366f1'),
            'size_mb': round(exam['size_bytes'] / (1024 * 1024), 2),
            'time_limit': None,
            'start_time': exam['start_time'] if exam.get('start_time') else None,
            'end_time': exam['end_time'] if exam.get('end_time') else None,
        }
    })


# ===== Submit Exam (Android) =====
# Supports two modes:
#  1. ASYNC (with Redis)  → enqueue → return 202 immediately → worker processes
#  2. SYNC (no Redis)     → process inline → return 200 (fallback)

@app.route('/api/exams/<int:exam_id>/submit', methods=['POST'])
def api_submit_exam(exam_id):
    """Receive student exam submission, enqueue for processing."""
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

    # Sanitize identity data
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

    # Check exam exists and is active (fast validation)
    db = get_db()
    exam = db.execute('SELECT id FROM exams WHERE id = ? AND status = ?', (exam_id, 'active')).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    # === ASYNC PATH (Redis available) ===
    if redis_client is not None:
        try:
            import submission_queue as _queue
            job_data = {
                'exam_id': exam_id,
                'student_name': student_name,
                'exam_number': exam_number,
                'student_class': student_class,
                'identity_data': identity_data_json,
                'answers_json': json.dumps(answers),
                'start_time': start_time,
                'mac_address': mac_address,
            }
            job_id = _queue.enqueue_submission(redis_client, job_data)

            # Log the access event (login/heartbeat) asynchronously
            try:
                from app import set_student_heartbeat
                set_student_heartbeat(exam_id, mac_address, {
                    'student_name': student_name,
                    'exam_number': exam_number,
                    'student_class': student_class,
                    'event': 'login',
                    'last_seen': datetime.now(timezone.utc).isoformat(),
                })
            except Exception:
                pass

            return jsonify({
                'success': True,
                'message': 'Jawaban berhasil dikirim',
                'status': 'queued',
                'job_id': job_id,
                'score': None,
            })

        except Exception as e:
            logger.warning(f"Async submit queue failed, falling back to sync: {e}")
            # Fall through to sync path

    # === SYNC PATH (fallback) ===
    try:
        from helpers import calculate_submission_score as score_fn

        questions_raw = db.execute(
            'SELECT questions_json FROM exams WHERE id = ?',
            (exam_id,)
        ).fetchone()['questions_json']

        score = None
        if questions_raw:
            try:
                questions = json.loads(questions_raw)
                score = score_fn(answers, questions)
            except Exception as e:
                logger.error(f"Auto-grading error: {e}")

        db.execute(
            'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, identity_data, answers_json, score, start_time, mac_address) '
            'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
            (exam_id, student_name, exam_number, student_class, identity_data_json, json.dumps(answers), score, start_time, mac_address)
        )
        db.commit()

        return jsonify({
            'success': True,
            'message': 'Jawaban berhasil dikirim',
            'score': score,
        })
    except Exception as e:
        logger.error(f"Sync submit failed: {e}")
        return error_response('Gagal menyimpan jawaban', 500)


# ===== PDF Download (Android App) =====

@app.route('/api/exams/<int:exam_id>/pdf')
def api_exam_pdf(exam_id):
    """Download exam PDF (for Android app)."""
    token = request.headers.get('X-Exam-Token') or request.args.get('token', '')
    if not token:
        return error_response('Token tidak disertakan', 401)

    db = get_db()
    exam = db.execute(
        'SELECT file_path, name FROM exams WHERE id = ? AND token = ?',
        (exam_id, token)
    ).fetchone()

    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    try:
        pdf_path = safe_storage_path(exam['file_path'])
        if not os.path.exists(pdf_path):
            return error_response('File PDF tidak ditemukan', 404)
        # Path validation passed, now send file
        return send_file(pdf_path, mimetype='application/pdf',
                        as_attachment=False,
                        download_name=f"{exam['name']}.pdf")
    except (ValueError, KeyError):
        return error_response('Path tidak valid', 400)


# ===== Access Log (Android) =====

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

    # Write heartbeat to Redis for real-time status
    if event in ('heartbeat', 'login'):
        try:
            from app import set_student_heartbeat
            set_student_heartbeat(exam_id, mac_address, {
                'student_name': student_name,
                'exam_number': exam_number,
                'student_class': student_class,
                'device_info': device_info,
                'ip_address': ip_address,
                'event': event,
                'last_seen': datetime.now(timezone.utc).isoformat(),
            })
        except Exception:
            pass

    return jsonify({'success': True, 'message': 'Access logged'})


# ===== Queue Status (Admin) =====

@app.route('/admin/api/queue/status')
def api_queue_status():
    """Admin endpoint to monitor submission queue."""
    import submission_queue as _queue
    if redis_client is None:
        return jsonify({'success': True, 'enabled': False, 'message': 'Redis not available, using sync mode'})

    stats = _queue.get_queue_stats(redis_client)
    stats['enabled'] = True
    stats['success'] = True
    return jsonify(stats)
