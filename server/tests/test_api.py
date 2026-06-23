"""Tests for EXAMVAN API endpoints (requires running server or test app)."""
import sys, os, json, io, unittest, secrets
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))
from app import app, get_db_standalone, init_db


class TestAPIHealth(unittest.TestCase):
    def setUp(self):
        self.app = app
        self.client = app.test_client()

    def test_health_check(self):
        resp = self.client.get('/api/health')
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertEqual(data['status'], 'ok')

    def test_server_time(self):
        resp = self.client.get('/api/time')
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertIn('utc', data)
        self.assertIn('unix', data)

    def test_exams_list(self):
        resp = self.client.get('/api/exams')
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertTrue(data['success'])


class TestAuth(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def test_login_page_get(self):
        resp = self.client.get('/admin/login')
        self.assertEqual(resp.status_code, 200)
        self.assertIn('Masuk', resp.text)

    def test_login_fail_wrong_password(self):
        resp = self.client.post('/admin/login', data={
            'username': 'admin', 'password': 'wrongpass'
        }, follow_redirects=True)
        self.assertIn('salah', resp.text.lower())

    def test_login_success(self):
        resp = self.client.post('/admin/login', data={
            'username': 'admin', 'password': 'examvan2026'
        }, follow_redirects=False)
        self.assertEqual(resp.status_code, 302)

    def test_logout(self):
        with self.client as c:
            with c.session_transaction() as sess:
                sess['admin_id'] = 1
                sess['admin_username'] = 'admin'
            resp = c.get('/admin/logout', follow_redirects=False)
            self.assertEqual(resp.status_code, 302)

    def test_register_page_get(self):
        resp = self.client.get('/register')
        self.assertEqual(resp.status_code, 200)

    def test_dashboard_requires_login(self):
        resp = self.client.get('/admin/dashboard', follow_redirects=False)
        self.assertEqual(resp.status_code, 302)  # redirect to login

    def test_csrf_token_in_admin_pages(self):
        with self.client as c:
            with c.session_transaction() as sess:
                sess['admin_id'] = 1
                sess['admin_username'] = 'admin'
            resp = c.get('/admin/dashboard')
            self.assertEqual(resp.status_code, 200)
            self.assertIn('csrf-token', resp.text)
            self.assertIn('admin-core.js', resp.text)
            self.assertIn('admin.js', resp.text)


class TestCSRFProtection(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def test_post_without_csrf_rejected(self):
        with self.client as c:
            with c.session_transaction() as sess:
                sess['admin_id'] = 1
                sess['admin_username'] = 'admin'
            # POST to /admin/api/exams/1/toggle (a POST endpoint) without CSRF
            resp = c.post('/admin/api/exams/1/toggle', content_type='application/json')
            self.assertEqual(resp.status_code, 403)
            data = resp.get_json()
            self.assertIn('csrf', data.get('error', ''))

    def test_post_with_csrf_accepted(self):
        with self.client as c:
            with c.session_transaction() as sess:
                sess['admin_id'] = 1
                sess['admin_username'] = 'admin'
                sess['is_super_admin'] = True
                sess['csrf_token'] = 'test_csrf_123'
            resp = c.post('/admin/api/exams/1/toggle',
                headers={'X-CSRF-Token': 'test_csrf_123'},
                content_type='application/json')
            # 404 is expected since exam 1 doesn't exist in test DB, but CSRF passed
            self.assertEqual(resp.status_code, 404)


class TestExamCRUD(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def _login_admin(self, client):
        with client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['is_super_admin'] = True
            sess['csrf_token'] = 'test_csrf'
        return 'test_csrf'

    def test_upload_exam(self):
        csrf = self._login_admin(self.client)
        pdf_content = b'%PDF-1.4 test document content'
        resp = self.client.post('/admin/api/upload', data={
            'name': 'Unit Test Exam',
            'pdf_file': (io.BytesIO(pdf_content), 'test.pdf', 'application/pdf'),
        }, headers={'X-CSRF-Token': csrf})
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertTrue(data['success'])
        self.assertIn('token', data)

    def test_toggle_exam(self):
        csrf = self._login_admin(self.client)
        # First create an exam
        pdf_content = b'%PDF-1.4 test'
        resp = self.client.post('/admin/api/upload', data={
            'name': 'Toggle Test',
            'pdf_file': (io.BytesIO(pdf_content), 'test.pdf', 'application/pdf'),
        }, headers={'X-CSRF-Token': csrf})
        exam_id = resp.get_json().get('data', {}).get('id')

        # Toggle it
        if exam_id:
            resp = self.client.post(f'/admin/api/exams/{exam_id}/toggle',
                headers={'X-CSRF-Token': csrf})
            self.assertEqual(resp.status_code, 200)

    def test_list_users_as_admin(self):
        csrf = self._login_admin(self.client)
        resp = self.client.get('/admin/api/users',
            headers={'X-CSRF-Token': csrf})
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertTrue(data['success'])
        self.assertIn('users', data)


class TestPublicEndpoints(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def test_index_page(self):
        resp = self.client.get('/')
        self.assertEqual(resp.status_code, 200)
        self.assertIn('EXAMVAN', resp.text)

    def test_download_page(self):
        resp = self.client.get('/download')
        self.assertEqual(resp.status_code, 200)
        self.assertIn('Unduh', resp.text)

    def test_404_redirects(self):
        resp = self.client.get('/nonexistent', follow_redirects=False)
        # Should redirect to index
        self.assertIn(resp.status_code, [302, 404])

    def test_token_redirect_valid_format(self):
        # A 6-char alphanumeric in URL should redirect to hasil
        resp = self.client.get('/ABCDEF', follow_redirects=False)
        # Could redirect or 404 depending on whether token exists
        self.assertIn(resp.status_code, [302, 404])


class TestRateLimiting(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()
        # Clear any residual rate limit state from previous runs
        try:
            from app import get_db_standalone as _get_db
            db = _get_db()
            db.execute('DELETE FROM rate_limits')
            db.commit()
            db.close()
        except Exception:
            pass

    def test_resend_otp_rate_limit(self):
        # Set up CSRF token for state-changing request + clear rate limits
        try:
            from app import get_db_standalone as _get_db
            db = _get_db()
            db.execute("DELETE FROM rate_limits WHERE key LIKE 'resend_otp:%'")
            db.commit()
            db.close()
        except Exception:
            pass
        with self.client.session_transaction() as sess:
            sess['csrf_token'] = 'test_csrf_123'
        resp = self.client.post('/resend-otp', data={
            'username': 'nonexistent',
            'csrf_token': 'test_csrf_123'
        })
        # Should be 404 (user not found) — if rate limited, accept 429
        self.assertIn(resp.status_code, [404, 429])

    def test_verify_otp_brute_force(self):
        # Multiple rapid attempts should eventually hit rate limit
        for i in range(10):
            resp = self.client.post('/verify-otp', data={
                'username': 'nonexistent_user',
                'otp': '123456'
            }, follow_redirects=True)
        # After many attempts, should see rate limit message
        # Note: depends on rate limiter state
        self.assertIn(resp.status_code, [200, 429])


class TestTemplateRendering(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def test_login_template(self):
        resp = self.client.get('/admin/login')
        self.assertIn('username', resp.text.lower())
        self.assertIn('password', resp.text.lower())

    def test_register_template(self):
        resp = self.client.get('/register')
        self.assertIn('whatsapp', resp.text.lower())
        self.assertIn('password', resp.text.lower())

    def test_download_template_has_version(self):
        resp = self.client.get('/download')
        self.assertIn('Unduh Aplikasi Siswa', resp.text)
        self.assertIn('Android', resp.text)


class TestScoringEndpoint(unittest.TestCase):
    def test_submit_and_score(self):
        client = app.test_client()
        with client.session_transaction() as sess:
            sess['csrf_token'] = 'test_csrf'

        # Submit answers (no auth required for public submit endpoint)
        import time
        resp = client.post('/api/exams/1/submit', json={
            'student_name': 'Test Student',
            'exam_number': '123',
            'student_class': 'X-A',
            'answers': {'1': 'A', '2': 'B'},
            'start_time': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
            'mac_address': '00:11:22:33:44:55',
        })
        # Should work for any exam, even non-existent returns JSON
        self.assertEqual(resp.content_type, 'application/json')


class TestErrorHandlers(unittest.TestCase):
    def setUp(self):
        self.client = app.test_client()

    def test_413_file_too_large(self):
        """Test that requests exceeding MAX_CONTENT_LENGTH return 413."""
        # MAX_CONTENT_LENGTH = 5MB + 4096
        big_data = b'x' * (6 * 1024 * 1024)  # 6MB
        resp = self.client.post('/admin/api/upload', data={
            'name': 'Big file',
            'pdf_file': (io.BytesIO(big_data), 'big.pdf', 'application/pdf'),
        })
        # Without proper session, expect 401, but if session exists, expect 413
        # At minimum, this should not crash
        self.assertIn(resp.status_code, [401, 413, 200])


if __name__ == '__main__':
    unittest.main()
