# EXAMVAN API Documentation

Base URL: `http://<server>:5000`

## Authentication

### Public API (No Auth Required)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/health` | GET | Server health check & version info |
| `/api/time` | GET | Server UTC time |
| `/api/exams` | GET | List of active exams |
| `/api/exams/token/<token>` | GET | Get exam config by 6-8 char token |
| `/api/exams/<id>/pdf` | GET | Download exam PDF |
| `/api/exams/<id>/submit` | POST | Submit student answers |
| `/api/hasil/<token>` | GET | Public exam results (JSON) |

### Admin API (Requires Login)

All admin endpoints require an active session (login via `/admin/login`).

> **Important:** Since v2.1.9, all `POST`, `PUT`, `DELETE` requests require a **CSRF token**.
> Include header: `X-CSRF-Token: <token>` (read from `<meta name="csrf-token">` in page).

---

## Public Endpoints

### `GET /api/health`

Server health check. Also returns the required Android client version.

**Response `200 OK`:**
```json
{
  "status": "ok",
  "version": "2.0",
  "required_app_version": "2.1.9",
  "lan_mode": true,
  "certificate_fingerprint": "sha256/AbCdEfGhIjKlMnOpQrStUvWxYz1234567890abcdefgh",
  "timestamp": "2026-06-22T17:08:40.379308+00:00",
  "server_time_utc": "2026-06-22T17:08:40Z"
}
```

### `GET /api/time`

Server UTC time for client sync.

**Response `200 OK`:**
```json
{
  "utc": "2026-06-22T17:08:40Z",
  "unix": 1750626520,
  "timezone": "UTC"
}
```

### `GET /api/exams`

List all active exams (for Android client).

**Response `200 OK`:**
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "UAS Matematika",
      "status": "active",
      "size_mb": 2.45,
      "created_at": "2026-06-22T10:00:00Z"
    }
  ]
}
```

### `GET /api/exams/token/<token>`

Get exam configuration by 6-character token.

**Headers:**
- `X-App-Version: 2.1.9` (required, returns 426 if mismatch)

**Response `200 OK`:**
```json
{
  "success": true,
  "data": {
    "id": 1,
    "name": "UAS Matematika",
    "status": "active",
    "token": "ABC123",
    "size_mb": 2.45,
    "questions": [
      {
        "number": 1,
        "type": "single_choice",
        "choices": ["A", "B", "C", "D", "E"],
        "weight": 1
      }
    ],
    "security_level": "medium",
    "identity_fields": [
      {"key": "student_name", "label": "Nama Siswa", "required": true},
      {"key": "exam_number", "label": "Nomor Ujian", "required": true},
      {"key": "student_class", "label": "Kelas", "required": true}
    ],
    "created_at": "2026-06-22T10:00:00Z"
  }
}
```

**Note:** Answer keys (`key` field) are **stripped** from questions in this response for security.

**Error Responses:**
- `426 Upgrade Required` — outdated app version
- `404 Not Found` — invalid token or exam inactive

### `GET /api/exams/<id>/pdf`

Download the exam PDF file for streaming.

**Headers:**
- `Accept-Ranges: bytes`

**Query Parameters:**
- `token` (string, optional) — Exam access token for validation

**Response `200 OK`:**
- Content-Type: `application/pdf`
- Content-Disposition: `inline; filename="exam_<id>.pdf"`
- Cache-Control: `no-store, no-cache, must-revalidate`

**Error:**
- `404 Not Found` — exam not found or file missing

### `POST /api/exams/<id>/submit`

Submit student answers and receive auto-graded score.

**Headers:**
- `X-App-Version: 2.1.9` (required)

**Request Body:**
```json
{
  "student_name": "Budi Santoso",
  "exam_number": "2026001",
  "student_class": "XII-A",
  "identity_data": {
    "student_name": "Budi Santoso",
    "exam_number": "2026001",
    "student_class": "XII-A",
    "extra_field": "Custom Value"
  },
  "answers": {
    "1": "A",
    "2": "B",
    "3": ["A", "C"],
    "4": {"1": "A", "2": "B"},
    "5": "Jakarta"
  },
  "start_time": "2026-06-22T10:00:00Z",
  "mac_address": "AA:BB:CC:DD:EE:FF"
}
```

**Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `student_name` | string | Yes | Legacy field (backward compat) |
| `exam_number` | string | Yes | Legacy field (backward compat) |
| `student_class` | string | Yes | Legacy field (backward compat) |
| `identity_data` | object | No | All identity fields as JSON (new) |
| `answers` | object | Yes | Map of question number → answer |
| `start_time` | string | No | ISO 8601 UTC |
| `mac_address` | string | No | Device identifier |

**Answer Format by Question Type:**

| Type | Format | Example |
|------|--------|---------|
| `single_choice` | String | `"A"` |
| `true_false` | String | `"TRUE"` |
| `short_answer` | String | `"Jakarta"` |
| `multiple_choice` | Array | `["A", "C"]` |
| `matching` | Object | `{"1": "A", "2": "B"}` |

**Response `200 OK`:**
```json
{
  "success": true,
  "message": "Jawaban berhasil dikirim",
  "score": 85.0
}
```

**Error:**
- `400 Bad Request` — incomplete identity
- `404 Not Found` — exam not found
- `426 Upgrade Required` — outdated app version

### `GET /api/hasil/<token>`

Public exam results API. Returns all submissions with evaluated answers.

**Response `200 OK`:**
```json
{
  "success": true,
  "exam_name": "UAS Matematika",
  "show_answers": false,
  "token": "ABC123",
  "questions": [
    {
      "number": 1,
      "type": "single_choice",
      "choices": ["A", "B", "C", "D", "E"],
      "weight": 1
    }
  ],
  "max_score": 100,
  "submissions": [
    {
      "id": 1,
      "student_name": "Budi Santoso",
      "exam_number": "2026001",
      "student_class": "XII-A",
      "answers": {"1": "A"},
      "score": 85.0,
      "max_score": 100,
      "start_time": "2026-06-22T10:00:00Z",
      "created_at": "2026-06-22T11:00:00Z",
      "evaluated_answers": {
        "1": {
          "statusClass": "correct",
          "statusText": "Benar ✔️",
          "earned": 1.0
        }
      }
    }
  ]
}
```

**Note:** If `show_answers` is disabled and not logged in as admin, the `key` field in questions is stripped.

---

## Admin Web Pages

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/login` | GET/POST | Admin login page |
| `/admin/logout` | GET | Logout & clear session |
| `/admin/dashboard` | GET | Main dashboard |
| `/admin/create-exam` | GET | Create exam page |
| `/admin/submissions` | GET | Submissions overview |
| `/admin/users` | GET | User management (super admin only) |

---

## Admin API

All admin endpoints require:
- Active session (`admin_id` in session)
- `@admin_required` decorator (auto-checks expiry + CSRF)
- For non-super-admin: ownership checks on exams/submissions

### Exam Management

#### `POST /admin/api/upload`
Upload a new exam PDF.

**Request:** `multipart/form-data`
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Exam display name |
| `pdf_file` | file | Yes | PDF file (max 5MB, must start with `%PDF`) |
| `custom_token` | string | No | Custom 6-char alphanumeric token |

**Response `200 OK`:**
```json
{
  "success": true,
  "message": "Ujian \"UAS\" berhasil diupload dengan token: ABC123",
  "token": "ABC123"
}
```

**Errors:** `400` (validation), `403` (limit reached)

#### `POST /admin/api/exams/create-from-editor`
Create exam from the question editor.

**Request:** `multipart/form-data`
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Exam name |
| `questions_json` | string | Yes | JSON array of questions |
| `pdf_file` | file | Yes | Generated PDF file |
| `custom_token` | string | No | Custom 6-char token |

#### `POST /admin/api/exams/<id>/toggle`
Toggle exam status between `active` and `inactive`.

#### `POST /admin/api/exams/<id>/toggle-public-results`
Toggle public student results page ON/OFF.

#### `POST /admin/api/exams/<id>/toggle-show-answers`
Toggle showing answer keys on results page.

#### `POST /admin/api/exams/<id>/edit`
Edit exam name and/or replace PDF.

**Request:** `multipart/form-data`
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | New exam name |
| `pdf_file` | file | No | New PDF (optional) |

#### `DELETE /admin/api/exams/<id>`
Delete exam and its PDF file. Cascades to delete all submissions.

#### `POST /admin/exams/bulk-delete`
Bulk delete exams.

**Request:**
```json
{"ids": [1, 2, 3]}
```

#### `POST /admin/exams/bulk-toggle`
Bulk toggle exam status.

**Request:**
```json
{"ids": [1, 2, 3], "status": "inactive"}
```

#### `POST /admin/api/exams/<id>/questions`
Save questions configuration for an exam.

**Request:**
```json
{
  "questions": [
    {
      "number": 1,
      "type": "single_choice",
      "key": "A",
      "weight": 1.0,
      "choices": ["A", "B", "C", "D", "E"]
    }
  ],
  "security_level": "medium",
  "identity_fields": [
    {"key": "student_name", "label": "Nama Siswa", "required": true},
    {"key": "class", "label": "Kelas", "required": true}
  ]
}
```

**Question Types:**
| Type | Description | Key Format |
|------|-------------|------------|
| `single_choice` | Pilihan Ganda | String e.g. `"A"` |
| `multiple_choice` | PG Kompleks | Array e.g. `["A","C"]` |
| `true_false` | Benar / Salah | `"TRUE"` or `"FALSE"` |
| `matching` | Menjodohkan | Object e.g. `{"1":"A","2":"B"}` |
| `short_answer` | Isian Singkat | String e.g. `"Jakarta"` |

#### `GET /admin/api/exams/<id>/questions`
Get questions configuration.

**Response:**
```json
{
  "success": true,
  "questions": [...],
  "security_level": "medium",
  "identity_fields": [...]
}
```

#### `GET /admin/exams/<id>/pdf`
View or download exam PDF. Add `?download=1` for attachment.

#### `POST /admin/api/exams/<id>/regenerate-token`
Generate a new 6-char token.

#### `POST /admin/api/exams/<id>/custom-token`
Set a custom 6-char token.

**Request:**
```json
{"token": "NEWTKN"}
```

### Submission Management

#### `GET /admin/api/submissions/<id>/detail`
Get detailed submission with answer comparison.

**Response:**
```json
{
  "success": true,
  "submission_id": 1,
  "student_name": "Budi Santoso",
  "exam_number": "2026001",
  "student_class": "XII-A",
  "exam_name": "UAS Matematika",
  "score": 85.0,
  "start_time": "2026-06-22T10:00:00Z",
  "mac_address": "AA:BB:CC:DD:EE:FF",
  "created_at": "2026-06-22T11:00:00Z",
  "answers": {"1": "A"},
  "questions": [...],
  "evaluated_answers": {
    "1": {"earned": 1.0, "statusText": "Benar ✔️", "statusClass": "correct"}
  }
}
```

#### `GET /admin/api/submissions/<id>/export_detail`
Export single submission as CSV. Add `?tz_offset=<minutes>` for timezone.

#### `DELETE /admin/api/submissions/<id>`
Delete a submission.

#### `GET /admin/api/submissions/export`
Export all submissions.
- `?exam_id=<id>`: export specific exam as XLSX
- Without `exam_id`: export all as CSV

### User Management (Super Admin Only)

#### `GET /admin/api/users`
List all registered users.

#### `POST /admin/api/users`
Create a new teacher user.

**Request:**
```json
{
  "username": "pak_budi",
  "password": "initial123",
  "whatsapp_number": "081234567890",
  "max_exams": 3,
  "max_pdf_size_mb": 1.0,
  "max_drafts": 2,
  "max_draft_size_mb": 1.0
}
```

#### `POST /admin/api/users/<id>/edit`
Edit user limits, status, and optionally password.

#### `DELETE /admin/api/users/<id>`
Delete user and all their exams/files.

#### `POST /admin/api/users/<id>/verify`
Manually activate a pending OTP user.

#### `POST /admin/api/users/<id>/toggle-status`
Suspend or activate a user account.

### SaaS Settings

#### `GET /admin/api/saas-settings`
Get global SaaS settings.

#### `POST /admin/api/saas-settings`
Update global settings.

**Request:**
```json
{
  "wa_verification_enabled": true,
  "wa_api_token": "your_fonnte_token",
  "wa_otp_template": "Kode OTP: {otp}",
  "default_max_exams": 3,
  "default_max_pdf_size_mb": 1.0,
  "default_active_days": 1,
  "default_max_drafts": 2,
  "default_max_draft_size_mb": 1.0,
  "android_version": "2.1.9",
  "webapp_version": "2.1.9"
}
```

### Stats & Password

#### `GET /admin/api/stats`
Get dashboard statistics.

**Response:**
```json
{
  "success": true,
  "data": {
    "total": 10,
    "active": 5,
    "inactive": 5,
    "storage_mb": 12.34,
    "local_ip": "192.168.1.100:5000"
  }
}
```

#### `POST /admin/api/change-password`
Change current user's password.

**Request:**
```json
{
  "current_password": "oldpass",
  "new_password": "newpass"
}
```

---

## Auth Endpoints (No CSRF Required)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/login` | POST | Login with username/password |
| `/register` | POST | Register new teacher account |
| `/verify-otp` | POST | Verify WhatsApp OTP (rate limited: 5/5min) |
| `/resend-otp` | POST | Resend OTP (rate limited: 3/10min) |

---

## Static Files

| Path | Description |
|------|-------------|
| `/static/js/admin-core.js` | Core JS utilities (CSRF, toast, etc.) |
| `/static/js/admin.js` | Admin panel feature functions |
| `/static/js/admin-creator.js` | Question creator UI |
| `/static/css/admin-base.css` | CSS variables & reset |
| `/static/css/admin-components.css` | Buttons, cards, tables, modals |
| `/static/css/admin-editor.css` | Question editor styles |
| `/static/css/admin-responsive.css` | Responsive breakpoints |
| `/download/apk` | Download student Android APK |

---

## Error Codes

| Status | Code | Description |
|--------|------|-------------|
| 400 | `validation_error` | Invalid input data |
| 403 | `invalid_csrf` | Missing/wrong CSRF token |
| 403 | `expired` | Account expired |
| 403 | `forbidden` | Insufficient permissions |
| 404 | `not_found` | Resource not found |
| 413 | `file_too_large` | File exceeds max size (5MB) |
| 426 | `upgrade_required` | Outdated client version |
| 429 | `rate_limited` | Too many requests (OTP) |
| 500 | `internal_error` | Server error (logged) |

All error responses follow this format:
```json
{
  "success": false,
  "error": "error_code",
  "message": "Human-readable error message"
}
```

---

## CSRF Protection (v2.1.9+)

All state-changing admin requests require a CSRF token.

**How it works:**
1. Admin page renders with `<meta name="csrf-token" content="...">`
2. JavaScript reads the token via `getCsrfToken()`
3. `apiFetch()` automatically adds `X-CSRF-Token` header to POST/PUT/DELETE
4. File uploads (XHR) manually set header: `xhr.setRequestHeader('X-CSRF-Token', token)`

**Rate Limiting:**
- `/verify-otp`: max 5 attempts per 5 minutes per IP
- `/resend-otp`: max 3 attempts per 10 minutes per IP

---

## Question Types Reference

### Single Choice
```json
{
  "number": 1,
  "type": "single_choice",
  "key": "A",
  "weight": 1.0,
  "choices": ["A", "B", "C", "D", "E"]
}
```

### Multiple Choice (PG Kompleks)
```json
{
  "number": 2,
  "type": "multiple_choice",
  "key": ["A", "C"],
  "weight": 2.0,
  "partial_scoring": true,
  "choices": ["A", "B", "C", "D", "E"]
}
```

### True / False
```json
{
  "number": 3,
  "type": "true_false",
  "key": "TRUE",
  "weight": 1.0
}
```

### Matching (Menjodohkan)
```json
{
  "number": 4,
  "type": "matching",
  "key": {"1": "A", "2": "B", "3": "C"},
  "weight": 3.0,
  "partial_scoring": true,
  "left_items": ["1", "2", "3"],
  "right_items": ["A", "B", "C"]
}
```

### Short Answer (Isian Singkat)
```json
{
  "number": 5,
  "type": "short_answer",
  "key": "Jakarta",
  "weight": 2.0
}
```
