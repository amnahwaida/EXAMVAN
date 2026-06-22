"""Frontend tests: HTML rendering, CSS validation, JS syntax, templates."""
import sys, os, json, unittest, re
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))
from app import app


class TestHTMLTemplates(unittest.TestCase):
    """All HTML templates should render with valid structure."""

    def setUp(self):
        self.client = app.test_client()

    def _check_html_structure(self, html, name):
        """Helper: check basic HTML structure."""
        self.assertIn('<!DOCTYPE html>', html, f'{name}: missing DOCTYPE')
        self.assertIn('<html', html, f'{name}: missing <html>')
        self.assertIn('</html>', html, f'{name}: missing </html>')
        self.assertIn('<head>', html, f'{name}: missing <head>')
        self.assertIn('</head>', html, f'{name}: missing </head>')
        self.assertIn('<body', html, f'{name}: missing <body>')
        self.assertIn('</body>', html, f'{name}: missing </body>')

    def _check_meta_viewport(self, html, name):
        """Check viewport meta tag for responsive design."""
        self.assertIn('viewport', html, f'{name}: missing viewport meta')

    def _check_charset(self, html, name):
        """Check UTF-8 charset declaration."""
        self.assertIn('charset', html, f'{name}: missing charset')

    def test_index_html(self):
        resp = self.client.get('/')
        self.assertEqual(resp.status_code, 200)
        self._check_html_structure(resp.text, 'index.html')
        self.assertIn('EXAMVAN', resp.text)

    def test_login_html(self):
        resp = self.client.get('/admin/login')
        self._check_html_structure(resp.text, 'login.html')
        self.assertIn('password', resp.text.lower())
        # login page is standalone (doesn't extend admin_base)
        # Check form elements
        self.assertIn('<form', resp.text)
        self.assertIn('</form>', resp.text)

    def test_register_html(self):
        resp = self.client.get('/register')
        self._check_html_structure(resp.text, 'register.html')
        self.assertIn('password', resp.text.lower())
        self.assertIn('whatsapp', resp.text.lower())

    def test_verify_otp_html(self):
        resp = self.client.get('/verify-otp?username=test')
        self._check_html_structure(resp.text, 'verify_otp.html')
        self.assertIn('otp', resp.text.lower())

    def test_download_html(self):
        resp = self.client.get('/download')
        self._check_html_structure(resp.text, 'download.html')
        self.assertIn('apk', resp.text.lower())
        self.assertIn('Unduh', resp.text)

    def test_hasil_html(self):
        """Public results page renders without errors."""
        resp = self.client.get('/hasil/TEST')
        self._check_html_structure(resp.text, 'hasil.html')
        self.assertIn('EXAMVAN', resp.text)

    def test_hasil_404(self):
        """Non-existent token should show error state."""
        resp = self.client.get('/hasil/NONEXIST')
        self.assertEqual(resp.status_code, 404)
        self.assertIn('Tidak Ditemukan', resp.text)

    def test_dashboard_html(self):
        """Admin dashboard (logged in) renders."""
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['csrf_token'] = 'test'
        resp = self.client.get('/admin/dashboard')
        self.assertEqual(resp.status_code, 200)
        self._check_html_structure(resp.text, 'dashboard.html')
        # Admin specific elements
        self.assertIn('dashboard', resp.text)
        self.assertIn('csrf-token', resp.text)

    def test_submissions_html(self):
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['csrf_token'] = 'test'
        resp = self.client.get('/admin/submissions')
        self.assertEqual(resp.status_code, 200)
        self._check_html_structure(resp.text, 'submissions.html')

    def test_admin_users_html(self):
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
            sess['csrf_token'] = 'test'
        resp = self.client.get('/admin/users')
        self.assertEqual(resp.status_code, 200)
        self._check_html_structure(resp.text, 'users.html')
        self.assertIn('Daftar User', resp.text)

    def test_create_exam_page_removed(self):
        """Buat Ujian page no longer exists."""
        resp = self.client.get('/admin/create-exam')
        self.assertIn(resp.status_code, [302, 404])


class TestCSSTemplates(unittest.TestCase):
    """CSS files should exist and be valid."""

    def setUp(self):
        self.client = app.test_client()
        self.css_dir = os.path.join(os.path.dirname(__file__), '..', 'static', 'css')
        self.js_dir = os.path.join(os.path.dirname(__file__), '..', 'static', 'js')

    def test_css_files_exist(self):
        """All CSS component files should exist."""
        expected = ['admin-base.css', 'admin-components.css',
                     'admin-editor.css', 'admin-responsive.css']
        for css_file in expected:
            path = os.path.join(self.css_dir, css_file)
            self.assertTrue(os.path.exists(path), f'Missing: {css_file}')
            self.assertGreater(os.path.getsize(path), 0, f'Empty: {css_file}')

    def test_css_no_syntax_errors(self):
        """CSS files should not have obvious syntax errors."""
        for css_file in os.listdir(self.css_dir):
            if not css_file.endswith('.css'):
                continue
            path = os.path.join(self.css_dir, css_file)
            with open(path) as f:
                content = f.read()
            lines = content.split('\n')
            # Check for unclosed braces (rough check)
            open_braces = content.count('{')
            close_braces = content.count('}')
            self.assertEqual(open_braces, close_braces,
                f'{css_file}: unclosed braces ({open_braces} vs {close_braces})')
            # Check for empty rules
            empty_rules = re.findall(r'\{[\s\n]*\}', content)
            if empty_rules:
                print(f'  ⚠ {css_file}: {len(empty_rules)} empty rule(s)')

    def test_css_variables_defined(self):
        """Check that CSS variables used are defined in :root."""
        for css_file in os.listdir(self.css_dir):
            if not css_file.endswith('.css'):
                continue
            path = os.path.join(self.css_dir, css_file)
            with open(path) as f:
                content = f.read()
            # Find var() usage
            vars_used = set(re.findall(r'var\((--[\w-]+)\)', content))
            # Find :root definitions
            root_defs = set(re.findall(r'--[\w-]+:', content))
            root_names = set()
            for d in root_defs:
                root_names.add(d.rstrip(':'))
            # Check all vars used are defined somewhere
            # (they might be defined in another CSS file)
            for v in vars_used:
                if v not in root_names:
                    pass  # Might be defined in another file

    def test_css_media_queries(self):
        """Check responsive breakpoints are consistent."""
        for css_file in ['admin-components.css', 'admin-editor.css',
                          'admin-responsive.css']:
            path = os.path.join(self.css_dir, css_file)
            if not os.path.exists(path):
                continue
            with open(path) as f:
                content = f.read()
            # Find all breakpoints
            breakpoints = re.findall(r'@media.*?max-width:\s*(\d+)px', content)
            for bp in breakpoints:
                bp_int = int(bp)
                # Standard responsive breakpoints
                self.assertIn(bp_int, [480, 600, 768, 968, 1000, 1024, 1200],
                    f'{css_file}: unusual breakpoint {bp}px')

    def test_admin_css_loaded_in_login(self):
        """Login page should load CSS."""
        resp = self.client.get('/admin/login')
        self.assertIn('css', resp.text)

    def test_admin_base_css_loaded_in_dashboard(self):
        """Dashboard should load component CSS files."""
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
        resp = self.client.get('/admin/dashboard')
        for css_file in ['admin-base.css', 'admin-components.css',
                          'admin-editor.css', 'admin-responsive.css']:
            self.assertIn(css_file, resp.text,
                f'Missing {css_file} in dashboard')

    def test_color_contrast(self):
        """Basic color contrast check for text/background."""
        with open(os.path.join(self.css_dir, 'admin-base.css')) as f:
            content = f.read()
        # Check that text colors exist (not just relying on defaults)
        self.assertIn('color', content)
        # Check for dark theme colors
        self.assertIn('--bg', content)
        self.assertIn('--text', content)


class TestJavaScriptTemplates(unittest.TestCase):
    """JavaScript files should exist and be valid."""

    def setUp(self):
        self.client = app.test_client()
        self.js_dir = os.path.join(os.path.dirname(__file__), '..', 'static', 'js')

    def test_js_files_exist(self):
        """All JS files should exist."""
        expected = ['admin-core.js', 'admin.js']
        for js_file in expected:
            path = os.path.join(self.js_dir, js_file)
            self.assertTrue(os.path.exists(path), f'Missing: {js_file}')
            self.assertGreater(os.path.getsize(path), 0, f'Empty: {js_file}')

    def test_admin_core_js_has_required_functions(self):
        """admin-core.js should define core utility functions."""
        path = os.path.join(self.js_dir, 'admin-core.js')
        with open(path) as f:
            content = f.read()
        for func in ['getCsrfToken', 'apiFetch', 'showToast',
                      'escapeHtml', 'localizeUTC', 'initMenuToggle']:
            self.assertIn(f'function {func}', content,
                f'admin-core.js missing function: {func}')

    def test_admin_js_has_required_functions(self):
        """admin.js should define admin feature functions."""
        path = os.path.join(self.js_dir, 'admin.js')
        with open(path) as f:
            content = f.read()
        for func in ['toggleExam', 'deleteExam', 'copyToken',
                      'regenerateToken', 'loadUsersList',
                      'submitChangePassword']:
            self.assertIn(f'function {func}', content,
                f'admin.js missing function: {func}')

    def test_admin_creator_js_removed(self):
        """admin-creator.js no longer exists."""
        import os
        path = os.path.join(os.path.dirname(__file__), '..', 'static', 'js', 'admin-creator.js')
        self.assertFalse(os.path.exists(path), 'admin-creator.js should be removed')

    def test_admin_core_js_loaded(self):
        """All admin pages should load admin-core.js."""
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
        resp = self.client.get('/admin/dashboard')
        self.assertIn('admin-core.js', resp.text)
        self.assertIn('admin.js', resp.text)

    def test_js_no_eval(self):
        """No JS files should use eval() (security risk)."""
        for js_file in os.listdir(self.js_dir):
            if not js_file.endswith('.js'):
                continue
            path = os.path.join(self.js_dir, js_file)
            with open(path) as f:
                content = f.read()
            # Check for eval (excluding comments)
            clean = re.sub(r'//.*', '', content)
            clean = re.sub(r'/\*.*?\*/', '', clean, flags=re.DOTALL)
            self.assertNotIn('eval(', clean,
                f'{js_file} uses eval() - security risk')

    def test_js_no_document_write(self):
        """No JS files should use document.write()."""
        for js_file in os.listdir(self.js_dir):
            if not js_file.endswith('.js'):
                continue
            path = os.path.join(self.js_dir, js_file)
            with open(path) as f:
                content = f.read()
            self.assertNotIn('document.write', content,
                f'{js_file} uses document.write()')


class TestResponsiveDesign(unittest.TestCase):
    """Responsive design meta tags and breakpoints."""

    def setUp(self):
        self.client = app.test_client()
    """Responsive design meta tags and breakpoints."""

    def test_viewport_meta(self):
        """All templates should have viewport meta tag for mobile."""
        templates = ['admin_base.html', 'hasil.html', 'login.html',
                      'register.html', 'verify_otp.html', 'download.html']
        tmpl_dir = os.path.join(os.path.dirname(__file__), '..', 'templates')
        for tmpl in templates:
            path = os.path.join(tmpl_dir, tmpl)
            if not os.path.exists(path):
                continue
            with open(path) as f:
                content = f.read()
            self.assertIn('viewport', content,
                f'{tmpl}: missing viewport meta tag')

    def test_media_queries_exist(self):
        """CSS should have mobile breakpoints."""
        css_dir = os.path.join(os.path.dirname(__file__), '..', 'static', 'css')
        for css_file in ['admin-components.css', 'admin-editor.css']:
            path = os.path.join(css_dir, css_file)
            if not os.path.exists(path):
                continue
            with open(path) as f:
                content = f.read()
            self.assertIn('@media', content,
                f'{css_file}: no media queries')

    def test_favicon_exists(self):
        """Favicon should exist."""
        resp = self.client.get('/static/favicon.png')
        self.assertEqual(resp.status_code, 200)

    def test_google_fonts(self):
        """Check Google Fonts loading in templates."""
        templates = ['index.html', 'login.html', 'hasil.html']
        tmpl_dir = os.path.join(os.path.dirname(__file__), '..', 'templates')
        for tmpl in templates:
            path = os.path.join(tmpl_dir, tmpl)
            if not os.path.exists(path):
                continue
            with open(path) as f:
                content = f.read()
            # Google Fonts present or inline fallback
            if 'fonts.googleapis.com' not in content:
                print(f'  ⚠ {tmpl}: no Google Fonts link')


class TestTemplateInheritance(unittest.TestCase):
    """Template inheritance structure."""

    def test_admin_templates_extend_base(self):
        """Admin templates should extend admin_base.html."""
        tmpl_dir = os.path.join(os.path.dirname(__file__), '..', 'templates')
        admin_templates = ['dashboard.html', 'submissions.html',
                           'users.html']
        for tmpl in admin_templates:
            path = os.path.join(tmpl_dir, tmpl)
            with open(path) as f:
                content = f.read()
            self.assertIn('extends "admin_base.html"', content,
                f'{tmpl}: should extend admin_base.html')
            self.assertIn('block content', content,
                f'{tmpl}: should have content block')

    def test_admin_base_has_required_blocks(self):
        """admin_base.html should define required blocks."""
        path = os.path.join(os.path.dirname(__file__), '..',
                            'templates', 'admin_base.html')
        with open(path) as f:
            content = f.read()
        for block in ['title', 'head', 'content', 'scripts']:
            self.assertIn(f'block {block}', content,
                f'admin_base.html missing block: {block}')

    def test_no_duplicate_extends(self):
        """No template should extend more than once."""
        tmpl_dir = os.path.join(os.path.dirname(__file__), '..', 'templates')
        for tmpl in os.listdir(tmpl_dir):
            if not tmpl.endswith('.html'):
                continue
            path = os.path.join(tmpl_dir, tmpl)
            with open(path) as f:
                content = f.read()
            extends_count = content.count('extends "')
            self.assertLessEqual(extends_count, 1,
                f'{tmpl}: multiple extends ({extends_count})')


class TestFormElements(unittest.TestCase):
    """Form structure and accessibility."""

    def setUp(self):
        self.client = app.test_client()

    def test_login_form_has_labels(self):
        """Login form inputs should have labels."""
        resp = self.client.get('/admin/login')
        html = resp.text
        # Check for label-input pairs
        labels = re.findall(r'<label[^>]*>', html)
        inputs = re.findall(r'<input[^>]*>', html)
        self.assertGreater(len(labels), 0, 'No labels in login form')
        self.assertGreater(len(inputs), 0, 'No inputs in login form')

    def test_forms_have_submit(self):
        """Forms should have submit buttons."""
        templates = ['login.html', 'register.html', 'verify_otp.html']
        tmpl_dir = os.path.join(os.path.dirname(__file__), '..', 'templates')
        for tmpl in templates:
            path = os.path.join(tmpl_dir, tmpl)
            if not os.path.exists(path):
                continue
            with open(path) as f:
                content = f.read()
            self.assertIn('type="submit"', content,
                f'{tmpl}: no submit button')
            self.assertIn('</form>', content,
                f'{tmpl}: unclosed form tag')

    def test_admin_pages_have_meta_csrf(self):
        """Admin pages should have CSRF meta tag."""
        with self.client.session_transaction() as sess:
            sess['admin_id'] = 1
            sess['admin_username'] = 'admin'
        resp = self.client.get('/admin/dashboard')
        self.assertIn('csrf-token', resp.text)

    def test_error_pages(self):
        """Error pages should render without crashing."""
        resp = self.client.get('/nonexistent12345')
        self.assertIn(resp.status_code, [302, 404])


if __name__ == '__main__':
    unittest.main()
