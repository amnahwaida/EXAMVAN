"""Database layer tests: init_db, migrations, CRUD."""
import sys, os, json, unittest, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))


class TestDatabaseInit(unittest.TestCase):
    """Test database initialization and schema."""

    def setUp(self):
        import tempfile
        self.db_fd, self.db_path = tempfile.mkstemp()
        # Patch the DATABASE path in app module
        import app as app_module
        self.orig_db = app_module.DATABASE
        app_module.DATABASE = self.db_path

    def tearDown(self):
        import os
        os.close(self.db_fd)
        os.unlink(self.db_path)
        import app as app_module
        app_module.DATABASE = self.orig_db

    def _get_db(self):
        import sqlite3
        db = sqlite3.connect(self.db_path)
        db.row_factory = sqlite3.Row
        db.execute('PRAGMA foreign_keys = ON')
        return db

    def test_init_db_creates_tables(self):
        """init_db should create all required tables."""
        from app import init_db
        init_db()
        db = self._get_db()
        tables = db.execute(
            "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
        ).fetchall()
        table_names = [t['name'] for t in tables]
        self.assertIn('exams', table_names)
        self.assertIn('admin_users', table_names)
        self.assertIn('submissions', table_names)
        self.assertIn('saas_settings', table_names)
        db.close()

    def test_init_db_creates_admin(self):
        """init_db should create default admin user."""
        from app import init_db, ADMIN_USERNAME
        init_db()
        db = self._get_db()
        admin = db.execute(
            'SELECT username FROM admin_users WHERE username = ?',
            (ADMIN_USERNAME,)
        ).fetchone()
        self.assertIsNotNone(admin)
        db.close()

    def test_init_db_seeds_settings(self):
        """init_db should seed default SaaS settings."""
        from app import init_db
        init_db()
        db = self._get_db()
        count = db.execute('SELECT COUNT(*) as c FROM saas_settings').fetchone()['c']
        self.assertGreater(count, 0)
        db.close()

    def test_init_db_tables_have_columns(self):
        """All tables should have expected columns."""
        from app import init_db
        init_db()
        db = self._get_db()

        # Check exams columns
        exams_cols = [c['name'] for c in db.execute('PRAGMA table_info(exams)').fetchall()]
        for col in ['id', 'name', 'file_path', 'token', 'status',
                     'questions_json', 'security_level', 'created_by',
                     'public_results', 'show_answers', 'identity_fields']:
            self.assertIn(col, exams_cols, f'exams table missing column: {col}')

        # Check admin_users columns
        users_cols = [c['name'] for c in db.execute('PRAGMA table_info(admin_users)').fetchall()]
        for col in ['id', 'username', 'password_hash', 'status',
                     'max_exams', 'max_pdf_size', 'expires_at',
                     'whatsapp_number', 'otp_code', 'otp_expiry',
                     'max_drafts', 'max_draft_size']:
            self.assertIn(col, users_cols, f'admin_users missing column: {col}')

        # Check submissions columns
        subs_cols = [c['name'] for c in db.execute('PRAGMA table_info(submissions)').fetchall()]
        for col in ['id', 'exam_id', 'student_name', 'exam_number',
                     'student_class', 'answers_json', 'score',
                     'start_time', 'mac_address', 'identity_data']:
            self.assertIn(col, subs_cols, f'submissions missing column: {col}')

        db.close()

    def test_init_db_idempotent(self):
        """init_db should be safe to call multiple times."""
        from app import init_db
        init_db()
        init_db()  # Second call should not raise
        init_db()  # Third call should not raise
        self.assertTrue(True)

    def test_exam_fk_cascade(self):
        """Deleting an exam should cascade delete submissions."""
        from app import init_db
        init_db()
        db = self._get_db()
        db.execute('INSERT INTO exams (id, name, file_path, size_bytes, token, status) '
                    'VALUES (1, "Test", "test.pdf", 100, "ABC123", "active")')
        db.execute('INSERT INTO submissions (exam_id, student_name, exam_number, '
                    'student_class, answers_json) '
                    'VALUES (1, "Student", "123", "X-A", "{}")')
        db.execute('DELETE FROM exams WHERE id = 1')
        remaining = db.execute(
            'SELECT COUNT(*) as c FROM submissions WHERE exam_id = 1'
        ).fetchone()['c']
        self.assertEqual(remaining, 0)
        db.close()

    def test_token_unique_constraint(self):
        """Exam tokens should be unique."""
        from app import init_db
        init_db()
        db = self._get_db()
        db.execute('INSERT INTO exams (name, file_path, size_bytes, token, status) '
                    'VALUES ("Exam 1", "f1.pdf", 100, "ABC123", "active")')
        with self.assertRaises(Exception):
            db.execute('INSERT INTO exams (name, file_path, size_bytes, token, status) '
                        'VALUES ("Exam 2", "f2.pdf", 100, "ABC123", "active")')
        db.close()


class TestDatabaseMigrations(unittest.TestCase):
    """Test migration logic for adding columns to existing tables."""
    # Self-contained: tests migration SQL without importing from app

    def _init_db_on_db(self, db):
        """Run init_db logic on an arbitrary connection (replicates app.init_db)."""
        import json, secrets, hashlib
        from werkzeug.security import generate_password_hash
        from datetime import datetime, timezone, timedelta
        from string import ascii_uppercase, digits
        import sqlite3

        db.executescript('''
            CREATE TABLE IF NOT EXISTS exams (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                file_path TEXT NOT NULL,
                size_bytes INTEGER NOT NULL,
                token TEXT UNIQUE NOT NULL,
                questions_json TEXT,
                status TEXT DEFAULT 'active' CHECK(status IN ('active', 'inactive')),
                security_level TEXT DEFAULT 'medium' CHECK(security_level IN ('medium', 'low')),
                public_results INTEGER DEFAULT 1,
                show_answers INTEGER DEFAULT 0,
                created_by INTEGER DEFAULT 1,
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
                start_time TIMESTAMP,
                mac_address TEXT,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY(exam_id) REFERENCES exams(id) ON DELETE CASCADE
            );
            CREATE TABLE IF NOT EXISTS saas_settings (
                key TEXT PRIMARY KEY,
                value TEXT
            );
        ''')

        # Run migrations (add columns if missing)
        migrations = [
            ('exams', 'token', 'TEXT'),
            ('exams', 'questions_json', 'TEXT'),
            ('exams', 'identity_fields', 'TEXT'),
            ('admin_users', 'status', "TEXT DEFAULT 'active'"),
            ('admin_users', 'max_exams', 'INTEGER DEFAULT 3'),
            ('admin_users', 'max_pdf_size', 'INTEGER DEFAULT 1048576'),
            ('admin_users', 'expires_at', 'TIMESTAMP'),
            ('admin_users', 'whatsapp_number', 'TEXT'),
            ('admin_users', 'otp_code', 'TEXT'),
            ('admin_users', 'otp_expiry', 'TIMESTAMP'),
            ('admin_users', 'max_drafts', 'INTEGER DEFAULT 2'),
            ('admin_users', 'max_draft_size', 'INTEGER DEFAULT 1048576'),
            ('submissions', 'start_time', 'TIMESTAMP'),
            ('submissions', 'mac_address', 'TEXT'),
            ('submissions', 'identity_data', 'TEXT'),
        ]
        for table, col, col_type in migrations:
            try:
                db.execute(f'SELECT {col} FROM {table} LIMIT 1')
            except sqlite3.OperationalError:
                db.execute(f'ALTER TABLE {table} ADD COLUMN {col} {col_type}')
        db.commit()

    def _create_old_schema(self, db):
        """Create a minimal old schema (before migrations)."""
        db.executescript('''
            CREATE TABLE exams (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                file_path TEXT NOT NULL,
                size_bytes INTEGER NOT NULL,
                status TEXT DEFAULT 'active'
            );
            CREATE TABLE admin_users (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                username TEXT UNIQUE NOT NULL,
                password_hash TEXT NOT NULL
            );
            CREATE TABLE submissions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                exam_id INTEGER NOT NULL,
                student_name TEXT NOT NULL,
                exam_number TEXT NOT NULL,
                student_class TEXT NOT NULL,
                answers_json TEXT NOT NULL,
                score REAL
            );
            CREATE TABLE saas_settings (
                key TEXT PRIMARY KEY,
                value TEXT
            );
        ''')
        db.commit()

    def _get_cols(self, path, table):
        import sqlite3
        db = sqlite3.connect(path)
        cols = [c[1] for c in db.execute(f'PRAGMA table_info({table})').fetchall()]
        db.close()
        return cols

    def test_migrate_adds_token_column(self):
        """Migration should add missing token column to exams."""
        import sqlite3, tempfile, os
        fd, path = tempfile.mkstemp()
        os.close(fd)
        db = sqlite3.connect(path)
        self._create_old_schema(db)
        db.close()
        # Run migrations
        db = sqlite3.connect(path)
        self._init_db_on_db(db)
        db.close()
        cols = self._get_cols(path, 'exams')
        self.assertIn('token', cols)
        self.assertIn('questions_json', cols)
        self.assertIn('identity_fields', cols)
        os.unlink(path)

    def test_migrate_adds_admin_columns(self):
        """Migration should add missing columns to admin_users."""
        import sqlite3, tempfile, os
        fd, path = tempfile.mkstemp()
        os.close(fd)
        db = sqlite3.connect(path)
        self._create_old_schema(db)
        db.close()
        db = sqlite3.connect(path)
        self._init_db_on_db(db)
        db.close()
        cols = self._get_cols(path, 'admin_users')
        self.assertIn('status', cols)
        self.assertIn('max_exams', cols)
        self.assertIn('expires_at', cols)
        self.assertIn('whatsapp_number', cols)
        os.unlink(path)


class TestExamCRUD(unittest.TestCase):
    """Test exam creation, retrieval, update, deletion."""

    def setUp(self):
        import tempfile
        self.db_fd, self.db_path = tempfile.mkstemp()
        import app as app_module
        self.orig_db = app_module.DATABASE
        app_module.DATABASE = self.db_path
        from app import init_db
        init_db()

    def tearDown(self):
        import os
        os.close(self.db_fd)
        os.unlink(self.db_path)
        import app as app_module
        app_module.DATABASE = self.orig_db

    def _get_db(self):
        import sqlite3
        db = sqlite3.connect(self.db_path)
        db.row_factory = sqlite3.Row
        db.execute('PRAGMA foreign_keys = ON')
        return db

    def test_create_exam(self):
        """Create exam with all fields."""
        db = self._get_db()
        db.execute(
            'INSERT INTO exams (name, file_path, size_bytes, token, status) '
            'VALUES (?, ?, ?, ?, ?)',
            ('Test Exam', 'test.pdf', 1024, 'TOKEN1', 'active')
        )
        db.commit()
        exam = db.execute('SELECT * FROM exams WHERE token = ?', ('TOKEN1',)).fetchone()
        self.assertEqual(exam['name'], 'Test Exam')
        self.assertEqual(exam['token'], 'TOKEN1')
        self.assertEqual(exam['status'], 'active')
        db.close()

    def test_exam_defaults(self):
        """New exam should have sensible defaults."""
        db = self._get_db()
        db.execute(
            'INSERT INTO exams (name, file_path, size_bytes, token, status) '
            'VALUES ("Default Test", "d.pdf", 100, "DEFLT1", "active")'
        )
        db.commit()
        exam = db.execute('SELECT * FROM exams WHERE token = ?', ('DEFLT1',)).fetchone()
        self.assertIsNotNone(exam)
        db.close()

    def test_create_user(self):
        """Create teacher user."""
        from werkzeug.security import generate_password_hash
        db = self._get_db()
        pw = generate_password_hash('testpass')
        db.execute(
            'INSERT INTO admin_users (username, password_hash, status, max_exams) '
            'VALUES (?, ?, ?, ?)',
            ('teacher1', pw, 'active', 5)
        )
        db.commit()
        user = db.execute(
            'SELECT username, max_exams FROM admin_users WHERE username = ?',
            ('teacher1',)
        ).fetchone()
        self.assertEqual(user['username'], 'teacher1')
        self.assertEqual(user['max_exams'], 5)
        db.close()

    def test_submission_with_identity_data(self):
        """Submission should store identity_data JSON."""
        db = self._get_db()
        db.execute(
            'INSERT INTO exams (id, name, file_path, size_bytes, token, status) '
            'VALUES (1, "Identity Test", "i.pdf", 100, "IDTST", "active")'
        )
        identity = json.dumps({
            'student_name': 'Budi',
            'exam_number': '1001',
            'student_class': 'XII-A',
            'extra_field': 'Custom Value'
        })
        db.execute(
            'INSERT INTO submissions (exam_id, student_name, exam_number, '
            'student_class, answers_json, score, identity_data) '
            'VALUES (?, ?, ?, ?, ?, ?, ?)',
            (1, 'Budi', '1001', 'XII-A', '{}', 85.0, identity)
        )
        db.commit()
        sub = db.execute(
            'SELECT identity_data FROM submissions WHERE exam_id = 1'
        ).fetchone()
        parsed = json.loads(sub['identity_data'])
        self.assertEqual(parsed['student_name'], 'Budi')
        self.assertEqual(parsed['extra_field'], 'Custom Value')
        db.close()

    def test_questions_json_storage(self):
        """Questions JSON should store and retrieve correctly."""
        db = self._get_db()
        questions = [
            {'number': 1, 'type': 'single_choice', 'weight': 2.0},
            {'number': 2, 'type': 'multiple_choice', 'weight': 3.0},
        ]
        db.execute(
            'INSERT INTO exams (name, file_path, size_bytes, token, status, questions_json) '
            'VALUES ("Q Test", "q.pdf", 100, "QQQQQ", "active", ?)',
            (json.dumps(questions),)
        )
        db.commit()
        row = db.execute(
            'SELECT questions_json FROM exams WHERE token = ?',
            ('QQQQQ',)
        ).fetchone()
        restored = json.loads(row['questions_json'])
        self.assertEqual(len(restored), 2)
        self.assertEqual(restored[0]['type'], 'single_choice')
        db.close()


if __name__ == '__main__':
    unittest.main()
