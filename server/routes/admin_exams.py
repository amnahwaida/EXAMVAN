"""EXAMVAN routes — Exam CRUD, questions, delegation, bulk operations."""
import json
import re
import os
from datetime import datetime, timezone

from flask import (
    request, jsonify, send_file, abort
)
from werkzeug.utils import secure_filename

from app import (
    app, get_db,
    generate_token,
    admin_required, check_rate_limit,
    check_exam_ownership,
    safe_storage_path, STORAGE_DIR,
    DEFAULT_IDENTITY_FIELDS,
)
from helpers import (
    evaluate_answers_detailed, calculate_submission_score,
    parse_roles,
)
from routes._shared import (
    error_response, success_response, validate_pdf_upload,
)

from app import logger as app_logger
logger = app_logger


# ===== View Exam PDF =====

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


# ===== Toggle Exam Status =====

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


# ===== Toggle Public Results =====

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


# ===== Toggle Show Answers =====

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


# ===== Edit Exam =====

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

    if file and file.filename != '':
        file_data = file.read()
        is_valid, error_msg = validate_pdf_upload(file_data, file.filename, file.content_type)
        if not is_valid:
            return error_response(error_msg, 400)

        try:
            old_file_path = safe_storage_path(exam['file_path'])
            if os.path.exists(old_file_path):
                try:
                    os.remove(old_file_path)
                except Exception as e:
                    logger.error("Error removing old PDF: %s", e)
        except (ValueError, KeyError):
            logger.error("Invalid file path for exam %s: %s", exam_id, exam.get('file_path'))

        timestamp = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M%S')
        safe_name = secure_filename(file.filename) or 'exam.pdf'
        filename = f"{timestamp}_{safe_name}"
        file_path = os.path.join(STORAGE_DIR, filename)

        with open(file_path, 'wb') as f:
            f.write(file_data)

        db.execute(
            'UPDATE exams SET name = ?, file_path = ?, size_bytes = ? WHERE id = ?',
            (name, filename, len(file_data), exam_id)
        )
    else:
        db.execute(
            'UPDATE exams SET name = ? WHERE id = ?',
            (name, exam_id)
        )

    db.commit()

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil diperbarui'
    })


# ===== Delete Exam =====

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

    try:
        file_path = safe_storage_path(exam['file_path'])
        if os.path.exists(file_path):
            os.remove(file_path)
    except (ValueError, KeyError):
        logger.error("Invalid file path for exam %s: %s", exam_id, exam.get('file_path'))

    db.execute('DELETE FROM exams WHERE id = ?', (exam_id,))
    db.commit()

    return jsonify({'success': True, 'message': 'Ujian berhasil dihapus'})


# ===== Bulk Delete =====

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
        if not session.get('is_super_admin') and not session.get('is_operator'):
            owned = db.execute(
                f'SELECT id FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)}) AND created_by = ?',
                exam_ids + [session['admin_id']]
            ).fetchall()
            exam_ids = [row['id'] for row in owned]
            if not exam_ids:
                return error_response('Tidak ada ujian yang dapat dihapus', 400)

        rows = db.execute(
            f'SELECT file_path FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)})',
            exam_ids
        ).fetchall()

        for row in rows:
            try:
                fp = safe_storage_path(row['file_path'])
                if os.path.exists(fp):
                    os.remove(fp)
            except (ValueError, KeyError) as e:
                logger.warning("Invalid file path in bulk delete: %s", e)

        db.execute('BEGIN')
        try:
            db.execute(
                f'DELETE FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)})',
                exam_ids
            )
            db.commit()
        except Exception as e:
            db.rollback()
            raise e
        return jsonify({'success': True, 'message': f'{len(exam_ids)} ujian berhasil dihapus'})
    except Exception as e:
        logger.error("Bulk delete error: %s", e)
        return error_response('Terjadi kesalahan saat menghapus ujian', 500)


# ===== Bulk Toggle =====

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
        if session.get('admin_username') != app.ADMIN_USERNAME:
            owned = db.execute(
                f'SELECT id FROM exams WHERE id IN ({",".join("?" for _ in exam_ids)}) AND created_by = ?',
                exam_ids + [session['admin_id']]
            ).fetchall()
            exam_ids = [row['id'] for row in owned]
            if not exam_ids:
                return error_response('Tidak ada ujian yang dapat diperbarui', 400)

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


# ===== Regenerate Token =====

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


# ===== Custom Token =====

@app.route('/admin/api/exams/<int:exam_id>/custom-token', methods=['POST'])
@admin_required
def admin_custom_token(exam_id):
    """Set custom token for an exam."""
    data = request.get_json() or {}
    custom_token = data.get('token', '').strip().upper()

    if not custom_token:
        return error_response('Token kustom tidak boleh kosong', 400)

    if not re.match(r'^[A-Z0-9]{8}$', custom_token):
        return error_response('Token kustom harus terdiri dari 8 karakter alfanumerik', 400)

    db = get_db()
    if not check_exam_ownership(db, exam_id):
        return error_response('Akses ditolak: Anda tidak memiliki akses ke ujian ini', 403)

    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

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


# ===== Questions Configuration =====

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
        if not identity_fields:
            try:
                identity_fields = json.loads(DEFAULT_IDENTITY_FIELDS)
            except Exception:
                identity_fields = []

        # Get assigned pengawas
        assigned_pengawas = []
        pengawas_rows = db.execute(
            'SELECT ep.user_id, u.username, u.instansi '
            'FROM exam_pengawas ep JOIN admin_users u ON ep.user_id = u.id '
            'WHERE ep.exam_id = ? ORDER BY u.username',
            (exam_id,)
        ).fetchall()
        for p in pengawas_rows:
            assigned_pengawas.append({'id': p['user_id'], 'username': p['username'], 'instansi': p['instansi'] or ''})

        # Get available pengawas (same instansi as exam creator)
        available_pengawas = []
        creator = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (exam['created_by'],)).fetchone()
        if creator and creator['instansi']:
            avail_rows = db.execute(
                "SELECT id, username, instansi FROM admin_users WHERE role LIKE ? AND instansi = ? AND status = ? ORDER BY username",
                ('%"pengawas"%', creator['instansi'], 'active')
            ).fetchall()
            for a in avail_rows:
                available_pengawas.append({'id': a['id'], 'username': a['username'], 'instansi': a['instansi'] or ''})

        return jsonify({
            'success': True,
            'questions': questions,
            'security_level': security_level,
            'strict_mode': strict_mode,
            'identity_fields': identity_fields,
            'panel_color': exam['panel_color'] if exam['panel_color'] else '',
            'start_time': exam['start_time'] if exam['start_time'] else '',
            'end_time': exam['end_time'] if exam['end_time'] else '',
            'assigned_pengawas': assigned_pengawas,
            'available_pengawas': available_pengawas
        })

    else:
        # POST: Save questions configuration
        data = request.json or {}
        questions = data.get('questions', [])
        security_level = data.get('security_level', 'medium')

        if security_level not in ['medium', 'low', 'high']:
            security_level = 'medium'

        strict_mode = 1 if security_level == 'high' else 0

        if not isinstance(questions, list):
            return error_response('Format data pertanyaan tidak valid', 400)

        identity_fields = data.get('identity_fields')
        if identity_fields is not None and isinstance(identity_fields, list):
            identity_fields_json = json.dumps(identity_fields)
        else:
            identity_fields_json = exam['identity_fields'] or DEFAULT_IDENTITY_FIELDS

        panel_color = data.get('panel_color', '')
        if panel_color and not panel_color.startswith('#'):
            panel_color = ''
        panel_color = panel_color[:7]

        start_time = data.get('start_time', '')
        end_time = data.get('end_time', '')
        if start_time and not re.match(r'^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$', start_time) and not re.match(r'^\d{2}:\d{2}$', start_time):
            start_time = ''
        if end_time and not re.match(r'^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$', end_time) and not re.match(r'^\d{2}:\d{2}$', end_time):
            end_time = ''

        db.execute(
            'UPDATE exams SET questions_json = ?, security_level = ?, strict_mode = ?, identity_fields = ?, panel_color = ?, start_time = ?, end_time = ? WHERE id = ?',
            (json.dumps(questions), security_level, strict_mode, identity_fields_json, panel_color or None, start_time or None, end_time or None, exam_id)
        )
        db.commit()

        # Save pengawas assignment
        pengawas_ids = data.get('pengawas_ids')
        if pengawas_ids is not None and isinstance(pengawas_ids, list):
            creator = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (exam['created_by'],)).fetchone()
            creator_instansi = creator['instansi'] if creator else ''
            db.execute('DELETE FROM exam_pengawas WHERE exam_id = ?', (exam_id,))
            for uid in pengawas_ids:
                try:
                    uid = int(uid)
                except (ValueError, TypeError):
                    continue
                pengawas_user = db.execute(
                    "SELECT id, instansi, role FROM admin_users WHERE id = ? AND role LIKE ? AND instansi = ?",
                    (uid, '%"pengawas"%', creator_instansi)
                ).fetchone()
                if pengawas_user:
                    try:
                        db.execute(
                            'INSERT OR IGNORE INTO exam_pengawas (exam_id, user_id) VALUES (?, ?)',
                            (exam_id, uid)
                        )
                    except Exception:
                        pass
            db.commit()

        # Recalculate scores for all existing submissions
        total_subs = db.execute('SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?', (exam_id,)).fetchone()['cnt']
        page_size = 100
        try:
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
        except:
            db.rollback()
            raise
        return jsonify({'success': True, 'message': 'Konfigurasi soal berhasil disimpan dan nilai siswa berhasil diperbarui'})


# ===== Delegation Data (Operator) =====

@app.route('/admin/api/exams/<int:exam_id>/delegate-data', methods=['GET'])
@admin_required
def admin_exam_delegate_data(exam_id):
    """Get available gurus + pengawas data for delegation modal (operator only)."""
    if not session.get('is_operator'):
        return error_response('Akses ditolak', 403)
    db = get_db()
    operator = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
    if not operator or not operator['instansi']:
        return error_response('Operator tidak memiliki instansi', 400)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    guru_rows = db.execute(
        "SELECT id, username, instansi FROM admin_users WHERE role LIKE ? AND instansi = ? AND status = ? AND id != ? ORDER BY username",
        ('%"guru"%', operator['instansi'], 'active', exam['created_by'])
    ).fetchall()
    available_gurus = [{'id': r['id'], 'username': r['username'], 'instansi': r['instansi'] or ''} for r in guru_rows]

    pengawas_rows = db.execute(
        "SELECT id, username, instansi FROM admin_users WHERE role LIKE ? AND instansi = ? AND status = ? ORDER BY username",
        ('%"pengawas"%', operator['instansi'], 'active')
    ).fetchall()
    available_pengawas = [{'id': r['id'], 'username': r['username'], 'instansi': r['instansi'] or ''} for r in pengawas_rows]

    assigned_rows = db.execute(
        'SELECT user_id FROM exam_pengawas WHERE exam_id = ?',
        (exam_id,)
    ).fetchall()
    assigned_pengawas_ids = [r['user_id'] for r in assigned_rows]

    owner = db.execute('SELECT id, username FROM admin_users WHERE id = ?', (exam['created_by'],)).fetchone()
    current_owner = {'id': owner['id'], 'username': owner['username']} if owner else None

    delegated_info = None
    if exam['delegated_to']:
        d_user = db.execute('SELECT id, username FROM admin_users WHERE id = ?', (exam['delegated_to'],)).fetchone()
        if d_user:
            delegated_info = {'id': d_user['id'], 'username': d_user['username']}

    return success_response({
        'available_gurus': available_gurus,
        'available_pengawas': available_pengawas,
        'assigned_pengawas_ids': assigned_pengawas_ids,
        'current_owner': current_owner,
        'delegated_to': delegated_info,
    })


# ===== Delegation Execute =====

@app.route('/admin/api/exams/<int:exam_id>/delegate', methods=['POST'])
@admin_required
def admin_delegate_exam(exam_id):
    """Transfer exam ownership and/or manage pengawas (operator only)."""
    if not session.get('is_operator'):
        return error_response('Akses ditolak', 403)
    data = request.get_json() or {}
    new_owner_id = data.get('new_owner_id')
    pengawas_ids = data.get('pengawas_ids')
    db = get_db()
    operator = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (session['admin_id'],)).fetchone()
    if not operator or not operator['instansi']:
        return error_response('Operator tidak memiliki instansi', 400)
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        return error_response('Ujian tidak ditemukan', 404)

    messages = []

    if new_owner_id:
        new_owner = db.execute(
            "SELECT id, username, instansi, role FROM admin_users WHERE id = ? AND instansi = ? AND status = ?",
            (new_owner_id, operator['instansi'], 'active')
        ).fetchone()
        if not new_owner:
            return error_response('Guru tidak ditemukan di instansi yang sama', 404)
        user_roles = parse_roles(new_owner['role'])
        if 'guru' not in user_roles:
            return error_response('User bukan guru', 400)
        db.execute('UPDATE exams SET delegated_to = ? WHERE id = ?', (new_owner_id, exam_id))
        messages.append(f'Penanggung jawab: {new_owner["username"]}')

    if pengawas_ids is not None and isinstance(pengawas_ids, list):
        db.execute('DELETE FROM exam_pengawas WHERE exam_id = ?', (exam_id,))
        for uid in pengawas_ids:
            try:
                uid = int(uid)
            except (ValueError, TypeError):
                continue
            pengawas_user = db.execute(
                "SELECT id FROM admin_users WHERE id = ? AND role LIKE ? AND instansi = ?",
                (uid, '%"pengawas"%', operator['instansi'])
            ).fetchone()
            if pengawas_user:
                try:
                    db.execute(
                        'INSERT OR IGNORE INTO exam_pengawas (exam_id, user_id) VALUES (?, ?)',
                        (exam_id, uid)
                    )
                except Exception:
                    pass
        messages.append('Pengawas diperbarui')

    db.commit()

    if not messages:
        return error_response('Tidak ada perubahan yang dilakukan', 400)
    return success_response(message='; '.join(messages))
