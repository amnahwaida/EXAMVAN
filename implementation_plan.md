# 📋 EXAMVAN Implementation Plan

## Architecture
| Component | Technology | Purpose |
|-----------|-----------|---------|
| **Backend API** | Python Flask + SQLite | REST API + file serving |
| **Admin Panel** | HTML/CSS/JS (served by Flask) | Exam management |
| **Android App** | Kotlin + PdfRenderer + OkHttp | Student exam viewer |

## Build Phases

### Phase 1: Backend Server ✅
- `server/app.py` — Flask app with REST API + Admin routes
- `server/requirements.txt` — Dependencies
- SQLite DB auto-init with admin user

### Phase 2: Admin Panel UI
- `server/templates/login.html` — Admin login
- `server/templates/dashboard.html` — Dashboard with upload/manage
- `server/static/css/admin.css` — Dark glassmorphism theme
- `server/static/js/admin.js` — AJAX interactions

### Phase 3: Android Project Structure
- Gradle build files, manifest, network security config
- Resource files (layouts, values, drawables)

### Phase 4: Android Source Code
- `ServerConfigActivity.kt` — Server URL configuration
- `ExamListActivity.kt` — Exam list with RecyclerView
- `ExamViewerActivity.kt` — PDF viewer with PdfRenderer
- Supporting classes (ApiClient, ExamAdapter, Exam model)

### Phase 5: Testing & Polish
- End-to-end testing on LAN
- Slow network simulation
- Security verification (FLAG_SECURE)

## API Endpoints
| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/health` | GET | Server health check |
| `/api/exams` | GET | List active exams |
| `/api/exams/{id}/pdf` | GET | Stream PDF file |

## Default Credentials
- **Admin**: `admin` / `examvan2026`
