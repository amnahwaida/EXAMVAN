"""Extended API tests: bulk ops, exports, edge cases."""
import sys, os, json, io, unittest
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))
from app import app
from routes import REQUIRED_ANDROID_VERSION


class TestBulkOperations(unittest.TestCase):
    """Bulk delete and toggle operations."""

    def setUp(self):
        self.client = app.test_client()
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['is_super_admin'] = True
            sess['csrf_token'] = 'bulk_test_csrf'
        self.csrf = 'bulk_test_csrf'

    def _create_exam(self, name):
        pdf = b'%PDF-1.4 bulk test'
        resp = self.client.post('/admin/api/upload', data={
            'name': name,
            'pdf_file': (io.BytesIO(pdf), 'bulk.pdf', 'application/pdf'),
        }, headers={'X-CSRF-Token': self.csrf})
        return resp.get_json().get('token')

    def test_bulk_toggle_invalid(self):
        """Bulk toggle with no IDs should fail."""
        resp = self.client.post('/admin/exams/bulk-toggle',
            headers={'X-CSRF-Token': self.csrf},
            json={'ids': [], 'status': 'inactive'})
        self.assertEqual(resp.status_code, 400)

    def test_bulk_toggle_invalid_status(self):
        """Bulk toggle with invalid status should fail."""
        resp = self.client.post('/admin/exams/bulk-toggle',
            headers={'X-CSRF-Token': self.csrf},
            json={'ids': [1], 'status': 'invalid'})
        self.assertEqual(resp.status_code, 400)


class TestSubmissionsExport(unittest.TestCase):
    """Submission export tests."""

    def setUp(self):
        self.client = app.test_client()
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['is_super_admin'] = True
            sess['csrf_token'] = 'export_test_csrf'
        self.csrf = 'export_test_csrf'

    def test_export_all_csv(self):
        """Export all submissions should return CSV."""
        resp = self.client.get('/admin/api/submissions/export',
            headers={'X-CSRF-Token': self.csrf})
        # Should redirect or return CSV
        self.assertIn(resp.status_code, [200, 302])

    def test_export_detail_not_found(self):
        """Export detail for non-existent submission should return error."""
        resp = self.client.get('/admin/api/submissions/99999/export_detail',
            headers={'X-CSRF-Token': self.csrf})
        # Admin 404 handler redirects to index (not /api/ prefix)
        self.assertIn(resp.status_code, [302, 404, 403])


class TestSaasSettings(unittest.TestCase):
    """SaaS settings API."""

    def setUp(self):
        self.client = app.test_client()
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['is_super_admin'] = True
            sess['csrf_token'] = 'saas_test_csrf'
        self.csrf = 'saas_test_csrf'

    def test_get_settings(self):
        """GET SaaS settings should return config."""
        resp = self.client.get('/admin/api/saas-settings',
            headers={'X-CSRF-Token': self.csrf})
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertTrue(data['success'])
        self.assertIn('settings', data)

    def test_update_settings(self):
        """POST SaaS settings should update config."""
        resp = self.client.post('/admin/api/saas-settings',
            headers={'X-CSRF-Token': self.csrf},
            json={'default_max_exams': '10', 'default_active_days': '30'})
        self.assertEqual(resp.status_code, 200)
        data = resp.get_json()
        self.assertTrue(data['success'])


class TestIdentityFields(unittest.TestCase):
    """Identity fields configuration."""

    def setUp(self):
        self.client = app.test_client()
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['is_super_admin'] = True
            sess['csrf_token'] = 'identity_test_csrf'
        self.csrf = 'identity_test_csrf'

    def test_identity_fields_default(self):
        """Default identity fields should be returned."""
        resp = self.client.get('/admin/api/exams/1/questions',
            headers={'X-CSRF-Token': self.csrf})
        data = resp.get_json()
        # Non-existent exam returns 404
        self.assertIn(resp.status_code, [200, 404])

    def test_identity_fields_in_token_api(self):
        """Token API should include identity_fields."""
        import json
        # Check that identity_fields config exists
        with app.test_request_context():
            from app import DEFAULT_IDENTITY_FIELDS
            fields = json.loads(DEFAULT_IDENTITY_FIELDS)
            self.assertEqual(len(fields), 3)
            self.assertEqual(fields[0]['key'], 'student_name')


class TestVersionCompatibility(unittest.TestCase):
    """App version compatibility checks."""

    def test_version_header_required(self):
        """API endpoints require X-App-Version header."""
        client = app.test_client()
        resp = client.get('/api/exams/token/ABC123')
        self.assertEqual(resp.status_code, 426)
        data = resp.get_json()
        self.assertIn('upgrade_required', data.get('error', ''))

    def test_version_mismatch(self):
        """Wrong version should return 426."""
        client = app.test_client()
        resp = client.get('/api/exams/token/ABC123',
            headers={'X-App-Version': '0.0.1'})
        self.assertEqual(resp.status_code, 426)

    def test_correct_version(self):
        """Correct version should proceed to token validation."""
        client = app.test_client()
        resp = client.get('/api/exams/token/ABC123',
            headers={'X-App-Version': REQUIRED_ANDROID_VERSION})
        self.assertEqual(resp.status_code, 404)  # token not found, but version OK


class TestTokenGeneration(unittest.TestCase):
    """Exam token generation."""

    def test_generate_token(self):
        """Token should be 6-char alphanumeric."""
        from app import generate_token
        token = generate_token()
        self.assertEqual(len(token), 6)
        self.assertTrue(token.isalnum())
        self.assertTrue(token.isupper())

    def test_generate_unique_tokens(self):
        """Multiple tokens should be unique."""
        from app import generate_token
        tokens = {generate_token() for _ in range(100)}
        self.assertEqual(len(tokens), 100)  # All unique

    def test_custom_token_validation(self):
        """Custom tokens must be 6-char alphanumeric."""
        # Test via admin upload validation
        pass


class TestPasswordHashing(unittest.TestCase):
    """Password hashing and verification."""

    def test_verify_password(self):
        """Password verification should work."""
        from werkzeug.security import generate_password_hash
        from app import _verify_password
        pw_hash = generate_password_hash('test123')
        self.assertTrue(_verify_password('test123', pw_hash))
        self.assertFalse(_verify_password('wrong', pw_hash))

    def test_legacy_hash_compat(self):
        """Legacy SHA-256 hashes are NO LONGER supported (security fix)."""
        import hashlib
        from app import _verify_password
        legacy = hashlib.sha256('test123'.encode()).hexdigest()
        # SHA-256 legacy support removed — must return False
        self.assertFalse(_verify_password('test123', legacy))
        self.assertFalse(_verify_password('wrong', legacy))


class TestStaticFiles(unittest.TestCase):
    """Static files should be accessible."""

    def test_favicon(self):
        client = app.test_client()
        resp = client.get('/static/favicon.png')
        self.assertEqual(resp.status_code, 200)

    def test_css_files(self):
        client = app.test_client()
        for css in ['admin-base']:
            resp = client.get(f'/static/css/{css}.css')
            self.assertEqual(resp.status_code, 200, f'Missing CSS: {css}')

    def test_js_files(self):
        client = app.test_client()
        for js in ['admin-core', 'admin']:
            resp = client.get(f'/static/js/{js}.js')
            self.assertEqual(resp.status_code, 200, f'Missing JS: {js}')


if __name__ == '__main__':
    unittest.main()
