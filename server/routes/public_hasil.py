"""EXAMVAN routes — Public exam results (/hasil/<token>, /api/hasil/<token>, /<token>)."""
import json

from flask import (
    request, jsonify, render_template,
    redirect, url_for, session, abort
)

from app import (
    app, get_db,
    DEFAULT_IDENTITY_FIELDS,
)
from helpers import format_iso_utc
from routes._shared import (
    error_response, _parse_identity_fields, _get_identity_data,
)

from app import logger as app_logger
logger = app_logger


# ===== Short URL Redirect (/<token>) =====

@app.route('/<token>')
def short_token_hasil(token):
    """Short URL redirect to exam results page, e.g. /BSGRIJ."""
    token_upper = token.strip().upper()
    if len(token_upper) == 8 and token_upper.isalnum():
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

    abort(404)


# ===== Hasil Page (HTML) =====

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


# ===== Hasil API (JSON) =====

@app.route('/api/hasil/<token>')
def api_public_hasil(token):
    """Public API: get exam results by token (no login required)."""
    token = token.strip().upper()
    db = get_db()
    exam = db.execute(
        'SELECT id, name, token, questions_json, public_results, show_answers, identity_fields FROM exams WHERE token = ?',
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

    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 100, type=int)
    per_page = min(max(per_page, 1), 500)

    total = db.execute(
        'SELECT COUNT(*) as cnt FROM submissions WHERE exam_id = ?',
        (exam['id'],)
    ).fetchone()['cnt']

    submissions = db.execute(
        'SELECT id, student_name, exam_number, student_class, answers_json, score, start_time, created_at, identity_data '
        'FROM submissions WHERE exam_id = ? ORDER BY score DESC LIMIT ? OFFSET ?',
        (exam['id'], per_page, (page - 1) * per_page)
    ).fetchall()

    questions = []
    if exam['questions_json']:
        try:
            questions = json.loads(exam['questions_json'])
        except Exception as e:
            logger.warning("Failed to parse questions_json for exam %s: %s", exam.get('id'), e)

    max_score = 0
    for q in questions:
        max_score += float(q.get('weight', 1.0))

    subs_data = []
    for sub in submissions:
        id_data = {}
        try:
            raw_id = sub['identity_data']
            id_data = json.loads(raw_id) if raw_id else {}
        except Exception:
            pass

        subs_data.append({
            'id': sub['id'],
            'student_name': sub['student_name'],
            'exam_number': sub['exam_number'],
            'student_class': sub['student_class'],
            'identity_data': id_data,
            'score': sub['score'],
            'max_score': max_score if max_score > 0 else None,
            'start_time': format_iso_utc(sub['start_time']) if sub['start_time'] else None,
            'created_at': format_iso_utc(sub['created_at'])
        })

    show_answers_enabled = (exam['show_answers'] if exam['show_answers'] is not None else 0) == 1

    if not is_logged_in and not show_answers_enabled:
        for q in questions:
            if 'key' in q:
                del q['key']

    total_pages = max(1, (total + per_page - 1) // per_page)

    identity_fields = _parse_identity_fields(exam)

    return jsonify({
        'success': True,
        'exam_name': exam['name'],
        'show_answers': show_answers_enabled,
        'token': exam['token'],
        'questions': questions,
        'identity_fields': identity_fields,
        'max_score': max_score if max_score > 0 else None,
        'submissions': subs_data,
        'pagination': {
            'page': page,
            'per_page': per_page,
            'total': total,
            'total_pages': total_pages
        }
    })
