# EXAMVAN — Sistem Ujian Digital

## Arsitektur
- **Backend**: Python Flask + SQLite (single-file DB)
- **Frontend**: Jinja2 templates + vanilla JS (no framework)
- **Android**: Kotlin, Material Components, Gson, EncryptedSharedPreferences
- **Deploy**: Gunicorn + systemd; optional Cloudflare Tunnel

## Struktur Direktori
```
server/
├── app.py              # Flask app, DB schema, migrations, decorators
├── routes.py           # Semua route handler (API + template rendering)
├── helpers.py          # Utility: scoring, sanitization
├── templates/          # Jinja2 HTML templates
│   ├── admin_base.html # Base layout admin panel
│   ├── dashboard.html  # Daftar ujian + modal Atur Soal
│   ├── users.html      # Kelola User (super admin)
│   ├── submissions.html# Hasil ujian admin
│   ├── hasil.html      # Halaman hasil ujian siswa (public)
│   ├── login.html
│   └── register.html
├── static/
│   ├── js/admin.js     # Semua JS admin (user CRUD, questions editor, dll)
│   ├── js/admin-core.js# Shared utilities (CSRF, toast, localizeUTC)
│   └── css/            # theme.css, tailwind, page-specific CSS
└── tests/

android/app/src/main/java/com/examvan/app/
├── ServerConfigActivity.kt  # Input server URL + token
├── ExamViewerActivity.kt    # PDF viewer + lembar jawaban
├── AppPrefs.kt              # EncryptedSharedPreferences keys
├── model/Exam.kt            # Data models (Exam, IdentityField, API responses)
├── helper/
│   ├── AnswerSheetBuilder.kt # Bangun UI lembar jawaban per tipe soal
│   ├── PdfRendererHelper.kt  # Download & render PDF
│   ├── SubmissionManager.kt  # Submit jawaban, auto-submit
│   └── SecurityEnforcer.kt   # Strict mode, lock task
└── view/ZoomableImageView.kt
```

## Database Schema (tabel utama)

### `admin_users`
- `id`, `username` (unique), `password_hash`
- `instansi` (TEXT, wajib diisi, dikelola super admin)
- `role` (TEXT: `'guru'` default, `'pengawas'`)
- `status` (`'active'`, `'suspended'`, `'pending_otp'`)
- `max_exams`, `max_pdf_size`, `max_drafts`, `max_draft_size`
- `whatsapp_number`, `expires_at`, `otp_code`, `otp_expiry`

### `exams`
- `id`, `name`, `file_path`, `size_bytes`, `token` (unique 8-char)
- `questions_json`, `identity_fields`, `security_level`, `strict_mode`
- `public_results`, `show_answers`, `panel_color`, `start_time`, `end_time`
- `created_by` (FK → admin_users.id), `status`

### `exam_pengawas` (junction table)
- `exam_id` (FK → exams.id, CASCADE DELETE)
- `user_id` (FK → admin_users.id, CASCADE DELETE)
- UNIQUE(exam_id, user_id)

### `submissions`
- `exam_id`, `student_name`, `exam_number`, `student_class`
- `answers_json`, `score`, `start_time`, `mac_address`, `identity_data`

## Pola Penting

### Auth & Authorization
- Super admin = username == `ADMIN_USERNAME` (env var, default `'superadmin'`) dan/atau role == `'superadmin'`
- **Tidak ada kolom `role` untuk admin** — super admin vs guru di-cek dari username
- `@admin_required` = login wajib + cek expiry
- `@super_admin_required` = hanya admin utama
- Ownership: `check_exam_ownership(db, exam_id)` — super admin bypass

### API Pattern
- Admin API: `/admin/api/...` (session-based auth + CSRF)
- Public API: `/api/...` (token-based, no login)
- Android API: `/api/exams/token/<token>` (X-App-Version header required)
- Semua response: `{'success': true/false, 'message': '...', ...}`

### Questions Editor Modal
- Single modal di `dashboard.html` (id: `questionsModal`)
- Config yang dihandle: questions list, security_level, identity_fields, panel_color, start_time, end_time, pengawas_ids
- `openQuestionsModal(examId)` fetch GET, `saveQuestionsConfig()` POST
- `activeExamId` global track exam yang sedang diedit

### Migration Pattern
- Migrations di `app.py` → list of tuples `(name, ALTER TABLE SQL)`
- Cek dari tabel `_migrations`, skip jika sudah ada
- `sqlite3.OperationalError` ditangkap (kolom sudah ada)

### Android Data Flow
- Token lookup → `ApiClient.getExamByToken()` → `TokenExamResponse`
- Config disimpan di `EncryptedSharedPreferences`
- `panel_color` di-apply via `applyPanelColor()` di `ExamViewerActivity`
- Questions di-render oleh `AnswerSheetBuilder.build(questions)`

## Perintah Berguna
```bash
# Jalankan server dev
cd server && python app.py

# Build Android
cd android && ./gradlew assembleRelease

# Run tests
cd server && python -m pytest tests/
```
