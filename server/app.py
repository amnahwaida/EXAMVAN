"""
EXAMVAN Server - REST API & Admin Panel
Version: 1.2.0
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
from datetime import datetime
from functools import wraps

from flask import (
    Flask, request, jsonify, render_template,
    redirect, url_for, session, send_file, flash
)
from werkzeug.utils import secure_filename

# ===== Configuration =====
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
STORAGE_DIR = os.path.join(BASE_DIR, 'storage')
DATABASE = os.path.join(BASE_DIR, 'examvan.db')
MAX_FILE_SIZE = 5 * 1024 * 1024  # 5MB
DEFAULT_ADMIN = {'username': 'admin', 'password': 'examvan2026'}

os.makedirs(STORAGE_DIR, exist_ok=True)

# ===== App Init =====
app = Flask(__name__)
app.secret_key = os.environ.get('EXAMVAN_SECRET', secrets.token_hex(32))
app.config['MAX_CONTENT_LENGTH'] = MAX_FILE_SIZE + 4096


# ===== Database =====
def get_db():
    """Get database connection with Row factory."""
    db = sqlite3.connect(DATABASE)
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
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(exam_id) REFERENCES exams(id) ON DELETE CASCADE
        );
    ''')

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

    db.close()


# ===== Helpers =====
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
        return f(*args, **kwargs)
    return decorated


def get_local_ip():
    """Get the server's LAN IP address."""
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(1)
        s.connect(('8.8.8.8', 80))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except Exception:
        return '127.0.0.1'


def get_storage_stats():
    """Get total storage used by PDFs."""
    total = 0
    for f in os.listdir(STORAGE_DIR):
        fp = os.path.join(STORAGE_DIR, f)
        if os.path.isfile(fp):
            total += os.path.getsize(fp)
    return total


# ===== REST API Endpoints =====

@app.route('/api/health')
def api_health():
    """Health check endpoint."""
    return jsonify({
        'status': 'ok',
        'version': '1.1',
        'lan_mode': True,
        'timestamp': datetime.now().isoformat()
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
            'created_at': exam['created_at']
        })

    db.close()
    return jsonify({'success': True, 'data': data})


@app.route('/api/exams/token/<token>')
def api_exam_by_token(token):
    """Get exam info by token. Used by Android app."""
    token = token.strip().upper()
    db = get_db()
    exam = db.execute(
        'SELECT id, name, status, size_bytes, token, questions_json, created_at '
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

    # If no questions configured, generate 40 default multiple-choice questions
    if not questions:
        questions = [
            {
                "number": i,
                "type": "single_choice",
                "choices": ["A", "B", "C", "D", "E"]
            } for i in range(1, 41)
        ]

    return jsonify({
        'success': True,
        'data': {
            'id': exam['id'],
            'name': exam['name'],
            'status': exam['status'],
            'token': exam['token'],
            'size_mb': round(exam['size_bytes'] / (1024 * 1024), 2),
            'questions': questions,
            'created_at': exam['created_at']
        }
    })


@app.route('/api/exams/<int:exam_id>/submit', methods=['POST'])
def api_submit_exam(exam_id):
    """Receive student exam submissions and auto-grade if keys exist."""
    data = request.json or {}
    student_name = data.get('student_name', '').strip()
    exam_number = data.get('exam_number', '').strip()
    student_class = data.get('student_class', '').strip()
    answers = data.get('answers', {})  # Map of "number" -> answer value

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
            earned_weight = 0.0
            total_weight = 0.0
            for q in questions:
                q_num = str(q['number'])
                student_ans = answers.get(q_num)
                correct_ans = q.get('key')
                q_weight = float(q.get('weight', 1.0))
                total_weight += q_weight
                partial_scoring = q.get('partial_scoring', False)

                earned_q_weight = 0.0
                if student_ans is not None and correct_ans is not None:
                    if q['type'] in ['single_choice', 'true_false']:
                        if str(student_ans).strip().upper() == str(correct_ans).strip().upper():
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
                                        if str(student_ans.get(k)).strip().upper() == str(v).strip().upper():
                                            correct_matches += 1
                                    portion = correct_matches / len(correct_ans)
                                    earned_q_weight = portion * q_weight
                            else:
                                match = True
                                for k, v in correct_ans.items():
                                    if str(student_ans.get(k)).strip().upper() != str(v).strip().upper():
                                        match = False
                                        break
                                if match:
                                    earned_q_weight = q_weight
                
                earned_weight += earned_q_weight

            score = round(earned_weight, 2)
        except Exception as e:
            print("Auto-grading error:", e)

    # Save submission
    db.execute(
        'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, answers_json, score) '
        'VALUES (?, ?, ?, ?, ?, ?)',
        (exam_id, student_name, exam_number, student_class, json.dumps(answers), score)
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
    """Redirect to admin dashboard or login."""
    if 'admin_id' in session:
        return redirect(url_for('admin_dashboard'))
    return redirect(url_for('admin_login'))


@app.route('/admin/login', methods=['GET', 'POST'])
def admin_login():
    """Admin login page."""
    if 'admin_id' in session:
        return redirect(url_for('admin_dashboard'))

    if request.method == 'POST':
        username = request.form.get('username', '').strip()
        password = request.form.get('password', '')

        db = get_db()
        pw_hash = hashlib.sha256(password.encode()).hexdigest()
        user = db.execute(
            'SELECT id, username FROM admin_users WHERE username = ? AND password_hash = ?',
            (username, pw_hash)
        ).fetchone()
        db.close()

        if user:
            session['admin_id'] = user['id']
            session['admin_username'] = user['username']
            return redirect(url_for('admin_dashboard'))
        else:
            flash('Username atau password salah', 'error')

    return render_template('login.html')


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
    exams = db.execute('SELECT * FROM exams ORDER BY created_at DESC').fetchall()

    total = len(exams)
    active = sum(1 for e in exams if e['status'] == 'active')
    inactive = total - active
    storage_bytes = get_storage_stats()
    local_ip = get_local_ip()

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
        local_ip=local_ip,
        admin_user=session.get('admin_username', 'Admin'),
        max_size_mb=MAX_FILE_SIZE // (1024 * 1024)
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
            'message': f'Ukuran file melebihi batas {MAX_FILE_SIZE // (1024*1024)}MB'
        }), 400

    # Save file with secure name
    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    safe_name = secure_filename(file.filename)
    filename = f"{timestamp}_{safe_name}"
    file_path = os.path.join(STORAGE_DIR, filename)

    with open(file_path, 'wb') as f:
        f.write(file_data)

    # Generate unique token
    token = generate_token()

    # Save to database
    db = get_db()
    db.execute(
        'INSERT INTO exams (name, file_path, size_bytes, token, status) VALUES (?, ?, ?, ?, ?)',
        (name, filename, len(file_data), token, 'active')
    )
    db.commit()
    db.close()

    return jsonify({
        'success': True,
        'message': f'Ujian "{name}" berhasil diupload',
        'token': token
    })


@app.route('/admin/api/exams/<int:exam_id>/toggle', methods=['POST'])
@admin_required
def admin_toggle_exam(exam_id):
    """Toggle exam status between active and inactive."""
    db = get_db()
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


@app.route('/admin/api/exams/<int:exam_id>', methods=['DELETE'])
@admin_required
def admin_delete_exam(exam_id):
    """Delete an exam and its PDF file."""
    db = get_db()
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


@app.route('/admin/api/exams/<int:exam_id>/regenerate-token', methods=['POST'])
@admin_required
def admin_regenerate_token(exam_id):
    """Regenerate token for an exam."""
    db = get_db()
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


@app.route('/admin/api/stats')
@admin_required
def admin_stats():
    """Get dashboard statistics."""
    db = get_db()
    exams = db.execute('SELECT status, size_bytes FROM exams').fetchall()
    db.close()

    total = len(exams)
    active = sum(1 for e in exams if e['status'] == 'active')

    return jsonify({
        'success': True,
        'data': {
            'total': total,
            'active': active,
            'inactive': total - active,
            'storage_mb': round(get_storage_stats() / (1024 * 1024), 2),
            'local_ip': get_local_ip()
        }
    })


@app.route('/admin/api/exams/<int:exam_id>/questions', methods=['GET', 'POST'])
@admin_required
def admin_exam_questions(exam_id):
    """Get or save questions configuration for an exam."""
    db = get_db()
    exam = db.execute('SELECT * FROM exams WHERE id = ?', (exam_id,)).fetchone()
    if not exam:
        db.close()
        return jsonify({'success': False, 'message': 'Ujian tidak ditemukan'}), 404

    if request.method == 'GET':
        questions_raw = exam['questions_json']
        questions = []
        if questions_raw:
            try:
                questions = json.loads(questions_raw)
            except Exception:
                pass
        db.close()
        return jsonify({'success': True, 'questions': questions})

    else:
        # POST: Save questions configuration
        data = request.json or {}
        questions = data.get('questions', [])

        # Basic validation
        if not isinstance(questions, list):
            db.close()
            return jsonify({'success': False, 'message': 'Format data pertanyaan tidak valid'}), 400

        # Save to database
        db.execute(
            'UPDATE exams SET questions_json = ? WHERE id = ?',
            (json.dumps(questions), exam_id)
        )
        db.commit()
        db.close()
        return jsonify({'success': True, 'message': 'Konfigurasi soal berhasil disimpan'})


@app.route('/admin/submissions')
@admin_required
def admin_submissions():
    """Submissions overview page for admin."""
    db = get_db()
    # Join with exams to show exam name
    submissions = db.execute(
        'SELECT s.*, e.name as exam_name '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id '
        'ORDER BY s.created_at DESC'
    ).fetchall()
    
    exams = db.execute('SELECT id, name FROM exams ORDER BY created_at DESC').fetchall()
    local_ip = get_local_ip()
    db.close()
    
    return render_template(
        'submissions.html',
        submissions=submissions,
        exams=exams,
        local_ip=local_ip,
        admin_user=session.get('admin_username', 'Admin')
    )


@app.route('/admin/api/submissions/<int:submission_id>', methods=['DELETE'])
@admin_required
def admin_delete_submission(submission_id):
    """Delete a student submission."""
    db = get_db()
    db.execute('DELETE FROM submissions WHERE id = ?', (submission_id,))
    db.commit()
    db.close()
    return jsonify({'success': True, 'message': 'Hasil ujian berhasil dihapus'})


@app.route('/admin/api/submissions/export')
@admin_required
def admin_export_submissions():
    """Export student submissions to CSV file."""
    exam_id = request.args.get('exam_id')
    
    db = get_db()
    query = (
        'SELECT s.id, e.name as exam_name, s.student_name, s.exam_number, s.student_class, s.score, s.created_at '
        'FROM submissions s JOIN exams e ON s.exam_id = e.id'
    )
    params = []
    if exam_id:
        query += ' WHERE s.exam_id = ?'
        params.append(exam_id)
        
    query += ' ORDER BY s.created_at DESC'
    submissions = db.execute(query, params).fetchall()
    db.close()

    # Generate CSV in memory
    si = io.StringIO()
    cw = csv.writer(si)
    cw.writerow(['ID', 'Nama Ujian', 'Nama Siswa', 'Nomor Ujian', 'Kelas', 'Nilai', 'Tanggal Submit'])
    
    for row in submissions:
        cw.writerow([
            row['id'],
            row['exam_name'],
            row['student_name'],
            row['exam_number'],
            row['student_class'],
            row['score'] if row['score'] is not None else 'Belum Dinilai',
            row['created_at']
        ])

    output = si.getvalue()
    si.close()

    # Create Flask response
    filename = f"hasil_ujian_{datetime.now().strftime('%Y%m%d_%H%M%S')}.csv"
    response = send_file(
        io.BytesIO(output.encode('utf-8-sig')), # use utf-8-sig for Excel compatibility in Indonesian local settings
        mimetype='text/csv',
        as_attachment=True,
        download_name=filename
    )
    response.headers['Cache-Control'] = 'no-store, no-cache, must-revalidate'
    return response


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
    init_db()
    local_ip = get_local_ip()
    port = int(os.environ.get('PORT', 5000))

    print(f"""
╔══════════════════════════════════════════════╗
║           EXAMVAN Server v1.2.0              ║
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
