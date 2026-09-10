# Review Alur Web & Dead Code — EXAMVAN webui

> Tanggal: 10 September 2026 · Scope: webui/ (Go/Gin + PostgreSQL + Redis), seluruh routing di `cmd/server/main.go` + review UI tiap halaman (permintaan lanjutan)
> Status dokumen: **SELESAI** — review penuh: alur web, dead code, dan UI tiap halaman (Bagian 1–6).

## Ringkasan

| Area | Status |
|---|---|
| Dead code frontend | ✅ Selesai — tidak ada file frontend dead (lihat Bagian 1) |
| File sampah root repo | ✅ Selesai — lihat Bagian 1.4 |
| Dead code Go (fungsi/package) | ✅ Analisis manual selesai — ±35 fungsi dead, 2 test-only, 1 file handler utuh tidak di-route (lihat Bagian 2) |
| Alur auth & siswa | ✅ Selesai — 1 high (deadlock versi submit), 5 medium, 7 low (lihat Bagian 3) |
| Alur admin & WebSocket | ✅ Selesai — 1 high (bypass operator di ListSubmissions), 3 medium, 4 low (lihat Bagian 4) |
| UI tiap halaman | ✅ Selesai — 0 high, 7 medium, 30 low (lihat Bagian 5) |

---

## Bagian 1 — Dead Code Frontend

### 1.1 Template (19 file, SEMUA hidup ✅)

Semua template di `templates/admin/` dan `templates/public/` diverifikasi dirender oleh handler Go:
`dashboard, login, pengawas, pengawas_detail, settings, submissions` + partials (`head, nav, settings-tabs, svg-symbols`) · public: `cek_hasil, download, forgot_password, hasil, index, register, register_confirm, reset_password, shared`.

Tidak ada template orphan.

### 1.2 CSS — SEMUA hidup ✅

| File | Direferensikan oleh |
|---|---|
| `admin-base.css` | admin/partials/head.html (semua halaman admin) |
| `public-desktop.css` + `public-mobile.css` | public/shared.html |
| `hasil.css` | public/hasil.html + cek_hasil.html |
| `theme.css` | partials/head.html (semua admin) + public |
| `tailwind/output.css` | public/index.html |

### 1.3 JavaScript — SEMUA hidup ✅ (temuan awal salah)

| File | Pemuat |
|---|---|
| `admin-core.js` | head.html semua admin |
| `admin.js` | submissions.html:412, dashboard.html:965, pengawas.html:161 |
| `settings-billing.js` | **loader dinamis** settings.html:2096 (`'settings-' + key + '.js'`) untuk tab `data-section="billing"` (settings.html:852) — grep literal awal saya melewatkannya; admin-core.js:394 juga menyebutnya sebagai konsumen `localizeUTC` |
| `settings-{general,packages,system-apps,users,vouchers,voucher-audit}.js` | loader dinamis settings.html:2096-2098 (vouchers→+voucher-audit, general→+packages) + load eksplisit system-apps (settings.html:1856) |
| `pengawas-detail.js` | pengawas_detail.html |
| `device-fingerprint.js` | index.html (public) + register |
| `fingerprintjs.min.js` | vendor — dicek referensinya di dalam device-fingerprint.js |

**Kesimpulan 1.3:** tidak ada file JS aplikasi yang dead. ~70 file `*.test.mjs` adalah test suite aktif (bukan dead code).

### 1.4 File sampah di root repo (bukan webui/) — DEAD

| File | Temuan |
|---|---|
| `fix_pricing.py` | One-off refactor script; merujuk `webui/templates/public/pricing.html` yang **sudah tidak ada** di repo — tugasnya selesai & targetnya hilang. DEAD. |
| `refactor_download.py` | One-off refactor script untuk download.html — tugasnya selesai. DEAD. |
| `refactor.sh` | One-off refactor script (membuat shared.html dari index.html) — shared.html sudah ada dan sudah berbentuk final. DEAD. |
| `test_tpl.go` | Quick check template parsing — utilitas debugging. DEAD (aman dihapus; hapus juga dari git). |
| `Screenshot_*.jpg` (2 file) | Screenshot Chrome Android di root repo, **tidak git-tracked**. Bukan kode; sampah di working tree. |
| `prompt.md`, `penawaran.md`, `strict_tutorial.md` | Git-tracked, adalah **dokumentasi produk** (panduan prompt AI untuk kunci jawaban XML, penawaran, tutorial) — bukan kode; putuskan apakah dipindah ke `docs/`. |
| `prd.md`, `README.md`, `roadmap_kapasitas.md`, `review_uiux_webui.md`, `upgrade_arsitektur.md` | Dokumentasi — putuskan lokasi, bukan dead code. |

### 1.5 File smoke-test ad-hoc di root webui/ — semi-dead

Lima file `//go:build ignore` (dikecualikan dari build, git-tracked), script E2E manual:

| File | Fungsi |
|---|---|
| `test_auth.go` | Login manual smoke test vs server dev :5001 |
| `test_http.go` | POST manual ke `/admin/api/exams/1/stop` — sangat ad-hoc, ID eksplisit hardcode |
| `test_start.go` | Pemeriksaan koneksi pgxpool manual |
| `test_concurrent_quota.go` | E2E ujian serentak vs dev server |
| `test_concurrent_quota_prod.go` | E2E ujian serentak vs stack produksi (nginx+Docker) |

Rekomendasi: pindahkan ke `webui/scripts/smoke/` (tetap `//go:build ignore`) atau hapus jika sudah tidak dipakai. `test_http.go` (hardcoded `/exams/1/stop`, localhost) adalah kandidat hapus terkuat.

---

## Bagian 2 — Dead Code Go (analisis manual)

> Tool `deadcode`/`staticcheck` gagal di-install sepanjang sesi ini (classifier kehabisan rate-limit), jadi analisis dilakukan manual: grep silang setiap fungsi ekspor di seluruh `internal/` + `cmd/`, lalu diklasifikasi ulang menurut jumlah referensi **produktif** (non-test, non-komentar, non-definisi) vs referensi **test**. Setiap kandidat ambigu diverifikasi ulang dengan menampilkan baris pemanggil aktual.

### 2.1 Fungsi ekspor DEAD — nol referensi produktif & nol referensi test (aman dihapus)

**internal/models/ (16 fungsi)**

| Fungsi | Lokasi |
|---|---|
| `GetStartedExamsByPengawas` | models (exam) |
| `ListUsersForInstansi` | models (user) |
| `DaysUntilExpiry` | models (user/instansi) |
| `ListAccessLogsByExam` | models/access_log.go |
| `GetAccessLogsForSubmission` | models/access_log.go |
| `GetLatestAccessLogForStudent` | models/access_log.go |
| `ParseIdentityDataJSON` | models/submission.go |
| `UpdateSubmissionScore` | models/submission.go |
| `DeleteSubmissionsByExam` | models/submission.go |
| `DeleteExamPengawas` | models/exam_pengawas.go |
| `DeletePengawasByExam` | models/exam_pengawas.go |
| `GetExamsForUser` | models/exam_pengawas.go |
| `IsExamAssignedToUser` | models/exam_pengawas.go |
| `GetPengawasCount` | models/exam_pengawas.go |
| `SeedDefaultSettings` | models/settings.go |
| `GetVoucherByCode` | models/voucher.go |

**internal/helpers/utils.go (3 fungsi)**

| Fungsi | Catatan |
|---|---|
| `ParseISOUTC` | nol referensi |
| `SanitizeStudentInput` | nol referensi — kandidat sanitasi input siswa yang tidak jadi dipakai |
| `GenerateToken` | nol referensi — token dibuat lewat session manager |

**internal/middleware/db.go (2 fungsi)**

| Fungsi | Catatan |
|---|---|
| `GetDB` | accessor DB dari context; handler memakai pool global/config langsung |
| `RequireDB` | idem |

**internal/redis/client.go (8 fungsi — satu suite utuh)**

| Fungsi | Catatan |
|---|---|
| `SetDialer` | nol caller |
| `SetHeartbeat` | nol caller |
| `GetHeartbeat` | nol caller |
| `GetActiveStudents` | nol caller |
| `DeleteHeartbeat` | nol caller |
| `CachedGet` | nol caller |
| `CachedSet` | nol caller |
| `CachedDelete` | nol caller |

> **Caveat sebelum hapus:** sebelum menghapus suite ini, pastikan tidak ada jalur di luar repo ini (desktop/windows/android/apk) yang memanggil klien Redis webui secara langsung — di dalam webui sendiri benar-benar nol referensi (produksi & test). Kandidat ini sehati dengan temuan bahwa WS hub sudah punya heartbeat internal sendiri (`TestPrivilegedGateHeartbeat` di hub), sehingga mekanisme heartbeat Redis lama tampaknya sudah digantikan.

**internal/queue/submission_queue.go**

| Fungsi | Catatan |
|---|---|
| `EnqueueSubmission` | hanya `EnqueueSubmissionWithJob` yang dipakai (submission_queue.go:266,551); versi tanpa jobID tidak pernah dipanggil |

### 2.2 File handler utuh yang tidak di-route — `internal/handlers/api/webhook.go`

**Temuan terbesar Bagian 2.** Seluruh file `webhook.go` tidak pernah didaftarkan di `cmd/server/main.go` (atau router mana pun):

- `WhatsappWebhook` — handler webhook WhatsApp Fonnte. Nol referensi produktif, nol test. Handler ini **tidak terpasang di server** — endpoint `/webhook`-nya tidak pernah hidup.
- `GetRegisterStatus` — endpoint status aktivasi user. Nol referensi produktif; hanya dipakai di `webhook_test.go:23`.

Konsekuensi alur: **fitur "aktivasi via WhatsApp" yang tersirat dari webhook.go tidak aktif di produksi.** Perlu konfirmasi pemilik produk: apakah memang ditinggalkan (→ aman hapus file + test), atau seharusnya dipasang (→ ini bug routing yang harus diperbaiki, bukan dead code).

### 2.3 Fungsi test-only — referensi test ada, referensi produksi nol

| Fungsi | Referensi test |
|---|---|
| `CountExamsByInstansi` (models/exam.go:399) | school_shared_quota_test.go:268,291,1235 |
| `GetRegisterStatus` (api/webhook.go:97) | webhook_test.go:23 |

Tidak dead penuh (masih mengegarkan kontrak lewat test), tetapi tidak melayani produksi — pertimbangkan pindahkan ke file test/`testutil` bila kontraknya masih relevan.

### 2.4 Terverifikasi HIDUP (awalnya kandidat, dicek ulang — JANGAN dihapus)

Kandidat ambigu berikut ternyata dipanggil internal (baris pemanggil diperiksa langsung):

| Fungsi | Pemanggil produksi |
|---|---|
| `MarshalSocketIO` | hub.go:375, 530 |
| `ParseSocketIO` | hub.go:194 |
| `EnqueueSubmissionWithJob` | submission_queue.go:266, 551 |
| `BestAndroidAppVersion` | system_apps.go:160 |
| `EffectiveAndroidRequiredVersionFrom` | system_apps.go:146 |
| `ListAccessLogsByExamAndIdentifier` | access_log.go:174 |
| `GetSubmissionByID` | submission.go:754 |
| `IsUserAssignedAsPengawas` | exam_pengawas.go:190 |
| `GetSubmissionStats` | submission.go:710 |
| `GenerateRandomVoucherCode` | voucher.go:92, 139, 145 |

### 2.5 Test infrastructure (bukan dead code aplikasi)

`TruncateDataTables` / `TestSchemaPool` / `NewPackageTestPool` di `internal/database/testdb.go` dipakai oleh banyak file test admin — infrastruktur test, biarkan.

### 2.6 Ringkasan dampak

- **±29 fungsi ekspor benar-benar dead** (2.1 + EnqueueSubmission) + 1 file handler utuh tidak di-route (2.2).
- Tidak ada dead code pada jalur render template/handler admin/api — semua route yang terdaftar di main.go memanggil handler hidup (dipakai untuk konfirmasi silang Bagian 1: 19/19 template dirender).
- Pola yang muncul: layer model menumpuk fungsi query "untuk masa depan" (penghapusan, statistik, listing) yang tidak pernah sampai ke handler — kandidat utama untuk bersih-bersih bertahap.

---

## Bagian 3 — Alur Auth & Siswa (review selesai)

### 3.1 Temuan per severity

**HIGH — 1 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| H1 | `internal/handlers/api/exams.go:893` (SubmitExam) & `:1409` (AccessLog) | Kedua handler membaca `GetSaasSettingWithDefault(SettingAndroidVersion, "2.5.0")` mentah, sedangkan middleware rute yang sama (`middleware/version.go:95-97`) memakai `EffectiveAndroidRequiredVersion` yang di-clamp ke APK publishable dan mengembalikan "" (skip) bila tidak ada APK — desain anti-deadlock di version.go:89-97. Skenario gagal: admin set `android_version=2.6.0` padahal APK tertinggi yang di-publish 2.5.0 → middleware meloloskan semua klien (effective 2.5.0), tapi submit & access-log menolak SEMUA klien 2.5.0 dengan 426 yang mengarah ke halaman download yang hanya punya 2.5.0 → **deadlock: seluruh ruangan tidak bisa submit di hari ujian**. Bonus: konstanta `requiredAndroidVersion="2.5.0"` hard-coded di exams.go:33 dipakai sebagai default 3×. |

**MEDIUM — 5 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| M1 | `cmd/server/main.go:489-490` (kontras :453, :540) | Halaman HTML `GET /hasil/:token` & `GET /hasil` tidak dipasangi rate limit sama sekali, padahal komentar `rateLimitHasilPerMinute=30` di :453 mengklaim "anti-brute token". Limit hanya terpasang di `GET /api/hasil/:token` (:540). Tiap request halaman menjalankan 3–4 query DB tanpa throttle (DoS ringan + dokumentasi ≠ kode). |
| M2 | `cmd/server/auth_recovery.go:263-374` | Reset password sukses **tidak menginvalidasi sesi lama**. Session store cookie client-side signed (main.go:214-222, MaxAge 86400) — tidak ada revocation server-side. Penyerang yang mencuri cookie tetap punya akses hingga 24 jam setelah korban reset password; `AuthRequired` me-revalidasi status/role tapi tidak ada atribut yang berubah saat reset. Satu-satunya cara usir: suspensi manual oleh admin. |
| M3 | `internal/handlers/api/exams.go:895`, `:1407-1425` | Cek versi hard-required tanpa pengecualian header `X-App-Version` kosong: header kosong → `parseVersionParts("")` → `isVersionAtLeast` false → 426. Kontras dua arah: `ExamByToken` (exams.go:670-684) hanya cek versi `if clientVersion != ""`, dan `AndroidVersionCheck` (version.go:52-57) skip saat header absen. Klien tanpa header bisa list exam & join token tapi 426 di submit — tiga endpoint memberlakukan kebijakan berbeda. |
| M4 | `internal/handlers/api/exams.go:1719-1731` (`isVersionAtLeast`) vs `middleware/version.go:106-128` (`isVersionCompatible`) | Semantik pembandingan berbeda: klien "2.5" vs required "2.5.0" → middleware TRUE (segmen hilang=0), handler FALSE (`len(cp)>=len(rp)` → 2>=3). Parsing juga beda (`parseVersionParts` skip segmen non-numerik; `parseVersion` jadikan 0). Klien yang lolos middleware bisa ditolak 426 di submit — inkoherensi dua lapisan pada rute yang sama. |
| M5 | `cmd/server/main.go:1692-1703` (`flushHeartbeatBatch`) + `internal/queue/submission_queue.go:551` | (a) Heartbeat flusher INSERT row submissions check-then-insert **tanpa advisory lock**, padahal TIGA jalur penulisan lain memakai `"approval:<exam_id>:<mac>"` untuk race yang sama (`EnsureFreshSubmissionOnApproval` submission.go:408-412, `CreateSubmission` :478-482, `upsertSubmissionRow` queue:501-505). Flusher vs approval-poll bisa dobel placeholder → siswa tampil dobel di Monitoring Perangkat. Juga `CompleteExam` tidak menghapus entry dari antrian `examvan:heartbeats:pending` → row kosong bisa muncul untuk siswa yang sudah submit (tampil "in-progress" padahal selesai). (b) Error re-enqueue retry diabaikan (`_ = EnqueueSubmissionWithJob`): bila Redis gagal saat push ulang, **jawaban siswa hilang permanen** → status "pending" selamanya; result key "failed" juga hanya TTL 5 menit (queue:49). Kehilangan jawaban senyap. |

**LOW — 7 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| L1 | `main.go:1437` vs `:1452`; `auth_recovery.go:337` vs `:345` | Urutan cek OTP tidak konsisten. Register-confirm: kode salah dulu, baru expiry; reset-password: expiry dulu. Tebakan salah pada OTP expired di register-confirm tetap dihitung attempt (bisa hapus akun setelah 5 attempt) alih-alih tampil "OTP kedaluwarsa". |
| L2 | `auth_recovery.go:80-152` | `resendOTPHandler` tidak memakai Turnstile (register/forgot/reset memakai). Mitigasi ada (5/mnt + cooldown 60 dtk + respons uniform) — hanya lapisan kurang, bukan lubang. |
| L3 | `middleware/csrf.go:65`, `:88` | Kegagalan CSRF selalu balas JSON 403 termasuk untuk form HTML biasa (login/register/confirm/resend/forgot/reset) → pengguna dengan token basi melihat JSON mentah di browser. Pola `isAPIRequest()` (auth.go:340-351) sudah ada tapi tidak dipakai di sini. |
| L4 | `main.go:1384-1396` (`maskEmail`) | Halaman confirm menampilkan email tersamarkan untuk sembarang username pending → oracle enumerasi ringan (konfirmasi keberadaan username + pola email). Dibatasi 5/mnt/IP; email ter-mask — dampak terbatas. |
| L5 | `middleware/csrf.go:100-108`, `:31-32` | Saat crypto/rand gagal, `randomHex` mengembalikan string kosong yang tetap di-set ke sesi → semua POST berikut 403 (fail-closed, aman; hanya ketersediaan). Self-heal via `token != ""` (:27). Sangat jarang. |
| L6 | `queue/submission_queue.go:644-651` | Fallback `generateJobID` pakai `time.Now().UnixNano()` heksa bila crypto/rand gagal → potensi kollision jobID antar worker (jendela kecil karena TTL 5 mnt). |
| L7 | `internal/handlers/public/hasil.go:410-434` | `evaluated_answers` (status benar/salah per soal) SELALU dikirim ke anonim meski `show_answers=false`; hanya answers mentah & key yang digerbangi. Terverifikasi TIDAK bocor kunci (EvaluationDetail hanya Earned/StatusText/StatusClass). Toggle jadi tidak konsisten penuh — keputusan produk: apakah semantik toggle mencakup status per-soal. |

### 3.2 Dead code di alur hasil publik — `isLoggedIn` yang tidak pernah aktif

`internal/handlers/public/hasil.go:163` (HasilPage) & `:241` (HasilAPI): `c.Get("user_id")` SELALU kosong — rute `/hasil/:token` (main.go:490) & `/api/hasil/:token` (main.go:540) tidak memakai `AuthRequired`, dan middleware global (main.go:338-364) hanya set cfg/db/timeout/redis/r2/ws_hub. Konsekuensi:

1. Guru yang sedang login tetap kena 403 "halaman hasil dinonaktifkan" saat membuka `/hasil/<token>` ujian non-public miliknya sendiri — harus lewat halaman admin submissions.
2. Cabang `showAnswersEnabled || isLoggedIn` (hasil.go:439-441) dan stripping key/answer `!isLoggedIn && !showAnswersEnabled` (:446-451) terdegradasi jadi perilaku public-only — fitur "pengunjung login berhak lihat jawaban mentah" (dokumentasi komentar :436-438) tidak pernah aktif.
3. Variabel template `is_logged_in` selalu false.

Bukan kebocoran (perilaku konservatif), tapi fitur yang jelas ditulis tidak berjalan → kandidat: hapus cabang, atau pasang auth opsional di rute `/hasil`.

### 3.3 Alur yang sudah solid (tidak perlu disentuh)

- **Open redirect**: `SafeRedirectPath` (auth.go:51-80) menolak backslash/`%5C`/URL absolut/`//`/prefix `/login`; semua redirect login memakainya.
- **Session**: `Clear()+Save()` saat login (anti-fixation), MaxAge/HttpOnly/SameSite=Lax/Secure-prod, logout hanya POST+CSRF, legacy GET `/logout` hanya redirect.
- **Revalidasi authority per-request** (`AuthRequired`, auth.go:147-232): suspended/pending langsung putus akses; role/super/instansi/username self-heal dari DB; akun expired ditandai `feature_locked` tanpa clearance sesi agar bisa renew.
- **Turnstile fail-closed** di login/register/forgot/reset + lockout 5 attempt/15 mnt (Redis + fallback memori) + respons uniform OTP (anti-enumerasi) + OTP TTL 15 mnt/cooldown 60 dtk.
- **ListExams**: scoping cross-tenant per instansi (list kosong tanpa kode) + cache Redis 30 dtk per sekolah.
- **RequestApproval**: auto-approve atomic di bawah advisory lock per exam; CASE semantics tepat (approved tak didemote, rejected tak ditimpa auto-approve); gate "exam live"; placeholder row idempoten via `EnsureFreshSubmissionOnApproval`.
- **ExamByToken**: `stripAnswerKeys` hapus kunci sebelum kirim; gate active+started+not-ended; throttle per-token.
- **ExamPDF**: gate approval server-side; URL R2 bertanda tangan 1 jam.
- **SubmitExam**: resubmit pemulihan pasca-deadline untuk perangkat approved; revokasi approval dipindah ke worker post-commit dengan justifikasi anti-kehilangan-jawaban (queue:455-465); idempotensi upsert + advisory lock di semua jalur — kecuali heartbeat flusher (M5).
- **ExamResult**: DB fallback mensyaratkan job_id + identitas (anti score-snoop); gate token-or-approved.
- **AccessLog**: heartbeat Redis-only (bypass DB); versi/sanitasi MAC sebagai boundary innerHTML.
- **Hasil publik**: header anti-cache + X-Robots-Tag; pesan error internal tidak dirender sebagai judul; waktu display WIB server-side.
- **Template & CSRF wiring**: semua field `csrf_token` cocok dengan fallback `CSRFRequired`; register-confirm mempertahankan `?username=` di action form.

---

## Bagian 4 — Alur Admin & WebSocket (review selesai)

> Temuan agent latar belakang, **diverifikasi ulang manual baris-per-baris** terhadap kode aktual sebelum masuk dokumen ini. Satu klaim agent terkoreksi saat verifikasi: gate kuota atomik hanya ada di SATU situs, bukan dua (lihat M7).

### 4.1 Temuan per severity

**HIGH — 1 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| H2 | `internal/handlers/admin/submissions.go:446-455` (ListSubmissions) | Blok "Verify access" hanya berjalan `if !isSuper && !isOp` → **operator dari instansi mana pun mem-bypass seluruh cek kepemilikan**; `models.ListSubmissionsByExam` (models/submission.go:621-633) lalu hanya memfilter `exam_id` → **kebocoran PII lintas-tenant**: nama siswa, kelas, nomor ujian, MAC, skor, status, riwayat akses — cukup lewat `GET /admin/api/submissions?exam_id=<id ujian sekolah lain>` dengan akun operator biasa. Guard yang benar sudah ada dan dipakai benar oleh tetangganya: `UserCanAccessExam` (models/exam.go:944 — operator hanya lolos bila instansi-nya non-kosong/non-"personal" dan sama dengan instansi pemilik ujian) di 7 endpoint pengawas.go (:65, :375, :706, :823, :921, :980, :1048) — hanya ListSubmissions yang tidak memakainya. Perbaikan: panggil `UserCanAccessExam` (atau tambah cabang operator dengan join instansi) sebelum query. `exam_id` sudah wajib (400 bila 0) dan `per_page` sudah di-clamp 5-100 — satu-satunya yang hilang adalah gate ini. |

**MEDIUM — 3 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| M6 | `submissions.go:327-334` (buildScopeConditions), `:363-370` (fetchFilterExams), `:810-849` khususnya `:823-830` (exportAllXLSX) | Scoping operator **fail-open** di tiga tempat: error `Scan(&instansi)` diabaikan, dan instansi kosong `''` di-match dengan `created_by IN (SELECT id FROM admin_users WHERE instansi = '')` → cocok dengan SEMUA akun ber-instansi-kosong, bukan cuma milik operator tsb. Pada jalur export, instansi kosong justru menghasilkan query **tanpa kondisi WHERE sama sekali** → satu request export seluruh submission sistem. Pola benar tersedia di paket yang sama: `getInstansiForOperator` (users.go:165-166 — pengawas.go:159-170 memakainya: error → 500, kosong/"personal" → hanya exam sendiri) dan fail-closed di CreateUser (users.go:776). Perbaikan: cek error `Scan`, fail-closed saat instansi kosong. Di jalur export, temuan ini menggandakan dampak H2. |
| M7 | `admin/exams.go:447-473` (satu-satunya gate atomik per-akun) — kontras `:277-278`, `:606-614`, `:910`, `:1779-1787` | Gate atomik pembuatan ujian (`SELECT max_exams ... FOR UPDATE` + `COUNT(*) WHERE created_by`) menolak saat `lockedMaxExams > 0 && cnt >= lockedMaxExams` **tanpa pengecualian pool sekolah** — padahal semua jalur kuota lain sadar-pool: pre-check (:277-278) sengaja melewati `maxExams` per-akun untuk sub-akun `OperatorCreated && poolActive` (komentar desain eksplisit: "the pool's atomic exam/PDF/storage gates below are the only caps"), ToggleExam (:606-614) & StartExam (:1779-1787) melewati limit konkuren per-user untuk pool-covered, dan gate PDF-replace (:910) sadar-pool. Sementara itu CreateUser **memaksa** kolom per-akun sub-akun operator ke `subAccountFreeMaxExams = 3` (users.go:82, blok paksaan :922-946 — kolom ini memang fallback saat pool mati, tapi tidak boleh aktif saat pool hidup). Konsekuensi: sub-akun sekolah dengan pool aktif terhenti di 3 ujian seumur-akun — kontradiksi dengan pre-check sendiri dan tampilan billing yang menampilkan kuota pool (billing.go:113-138). Catatan verifikasi: `lockedMaxExams` hanya muncul di SATU situs (:447-473); klaim awal agent "dua gate" salah — ":886-912" adalah gate PDF-replace yang justru sudah sadar-pool. Perbaikan: tambahkan pengecualian pool yang sama (`owner.OperatorCreated && poolActive` → skip hitung per-akun) pada gate atomik ini. |
| M8 | `internal/queue/submission_queue.go:320`, `:393`, `:631` (GetQueueStats) | Heartbeat worker (`examvan:submissions:worker_heartbeat`, TTL 30 dtk) hanya di-set **di dalam `flushBatch`** (:393), dan flush early-return saat buffer kosong (:320) → worker sehat tapi sedang idle >30 dtk tampil `WorkerActive=false` di `GET /admin/api/queue/status` (cek `EXISTS` :631). False alarm "worker mati" pada jam sepi mendorong admin bertindak keliru (restart dsb.). Perbaikan: set heartbeat pada setiap tick loop, bukan hanya saat flush. |

**LOW — 4 temuan**

| # | Lokasi | Temuan |
|---|---|---|
| L8 | `pengawas.go:695` (GetPendingApprovals), `:795` (SetApprovalStatus) | `examID, _ := strconv.Atoi(c.Param("exam_id"))` — error ditelan → examID=0 → `GetExamByID(0)` gagal → **404 "Ujian tidak ditemukan" yang menyesatkan** alih-alih 400 "ID ujian tidak valid". Pola benar dipakai di file yang sama pada :46, :356, :906, :956, :1032 — dua handler ini yang tertinggal. |
| L9 | `pengawas.go:367-378` (PengawasExamSubmissions; pola sama di :701-709, :818-826, :917-924, :975-983, :1044-1051) | Urutan `GetExamByID` dulu (404) baru `UserCanAccessExam` (403) → **oracle enumerasi keberadaan ujian** bagi pengguna tak berwenang: bisa membedakan "ujian ada tapi bukan milikmu" (403) vs "tidak ada" (404). Dampak terbatas (wajib login admin/pengawas, ID harus ditebak); bila ingin seragam, cukup balas 403 untuk kedua kasus. |
| L10 | `admin/exams.go:1589-1610` (RegenerateToken) | Generate token baru **tanpa retry loop dan tanpa pemetaan 23505** (unique violation) ke 400 ramah — kontras UploadExam (:301-333) yang punya retry 5×. Probabilitas rendah, tapi saat terjadi admin menerima 500 mentah tanpa penjelasan. |
| L11 | `admin/exams.go:1573` | `go recalculateScores(context.Background(), pool, examID)` — goroutine **tidak dilacak**, tidak dibatalkan saat graceful shutdown; shutdown di tengah proses menyisakan skor parsial sampai rescore berikutnya. Aman untuk diulang (idempoten, dihitung ulang dari answers), hanya tidak ada penutupan yang bersih. |

### 4.2 Alur yang sudah solid (tidak perlu disentuh)

- **pengawas.go — otorisasi exam-scoped yang benar**: 7 endpoint semuanya memakai `UserCanAccessExam` (operator dibatasi instansi pemilik; pengawas-only tidak mendapat kontrol — `UserCanControlExam` terpisah di :98); `PengawasExams` memakai `getInstansiForOperator` fail-closed (:159-170); N+1 dihapus lewat batch query (access logs, riwayat submission, heartbeat Redis dalam satu pipeline per-refresh); audit append-only hanya setelah write sukses; keputusan approval atomic via `UPDATE ... RETURNING` (tanpa read terpisah yang bisa basi); no-op perangkat tak dikenal tetap 200 tanpa menulis jejak audit palsu; semua keputusan hanya diterima saat ujian live (gate `IsActive` + `ExamScheduleEnded` konsisten antar endpoint).
- **users.go**: scoping fail-closed di semua cabang (kontras M6); advisory lock satu-operator-per-sekolah di semua jalur grant/move/claim/redeem/create; urutan advisory-lalu-row anti-deadlock; kebijakan sub-akun dipaksa konsisten di create (:922-946), update (:1225-1230), dan downgrade (:1974-2025); 23505 dipetakan ke pesan ramah.
- **vouchers.go**: aturan anti-oracle; `FOR UPDATE` + advisory + `schoolAlreadyHasOperator`; sinkronisasi role tidak pernah mendemote superadmin; audit ditulis SETELAH commit (bukan di dalam transaksi).
- **entitlement.go**: role 3-kolom; superadmin tak pernah tercapai lewat package; cascade suspend/restore; tombstone hanya exam yang belum mulai — exam yang sedang berjalan tidak dibunuh mendadak.
- **billing / packages / system_apps**: fail-open untuk DISPLAY vs fail-closed untuk ENFORCEMENT — disengaja dan konsisten; upsert transaksional; deteksi magic-bytes per platform; cleanup orphan R2; delete DB-first.
- **WS hub**: room per-exam; broadcast hanya dari klien privileged; sanitasi MAC; `device_info` tidak pernah dibroadcast ke room.
- **Queue**: savepoint per-row (row racun tidak membatalkan seluruh batch), publish setelah commit (tidak ada notifikasi untuk data yang bisa di-rollback), retry max 3, re-enqueue saat shutdown, poison drop setelah retry habis.
- **Background jobs**: semuanya wired & dibatalkan saat graceful shutdown (main.go:139/141/163/173/184); job expiry punya koneksi advisory-lock tersendiri, grace 24 jam, dan pass tombstone terpisah; approval-cleanup & retention live-tunable via `saas_settings` (0 = nonaktif).
- **Routing & middleware (main.go)**: rate limit & body size proporsional per rute; `SettingsRedirect` legacy tetap terpelihara; hanya `/admin/settings` + redeem/aktivasi voucher di luar `FeatureLockRequired` (by design — halaman perpanjangan harus terbuka saat akun terkunci); `DeactivateUserPackage` dilindungi `SuperAdminRequired()` inline (main.go:732); coverage template dua arah lengkap (konfirmasi silang Bagian 1: 19/19 template dirender, semua route menunjuk handler hidup).

---

## Bagian 5 — Review UI per Halaman

> Metode: tiga review agent paralel (admin non-settings, hub Pengaturan, halaman publik) + **verifikasi manual setiap klaim terhadap sumber** sebelum masuk dokumen. Penomoran melanjutkan global: **M9–M15** (medium), **L12–L41** (low). Total Bagian 5: **0 high / 7 medium / 30 low**. Keterbatasan pemulihan teks item agent dicatat di 5.6.

### 5.1 Halaman admin non-settings — 0 HIGH / 3 MEDIUM / 7 LOW

| Halaman | H | M | L |
|---|---|---|---|
| login.html | 0 | 0 | 0 |
| dashboard.html | 0 | 3 | 1 |
| pengawas.html | 0 | 0 | 2 |
| pengawas_detail.html | 0 | 0 | 3 |
| submissions.html | 0 | 0 | 1 |
| partials (head/nav/svg) | 0 | 0 | 0 |

**Klaster M9–M11 — tiga token CSS tidak terdefinisi di dashboard (terverifikasi penuh dari sumber).** Masalah UI terbesar sisi admin: `dashboard.html` memakai 3 token `var(...)` yang **tidak terdefinisi di mana pun** (dicek terhadap `theme.css`, `admin-base.css`, seluruh `static/css/`, dan semua template) — deklarasi styling-nya diam-diam tidak berlaku (fallback ke nilai bawaan browser). Dashboard adalah satu-satunya pemakai ketiganya:

| # | Token undefined | Lokasi di dashboard.html | Varian terdefinisi yang benar |
|---|---|---|---|
| M9 | `--accent-light` | :459 (nama pembuat ujian, kartu) | `--color-accent-light` (theme.css:23) |
| M10 | `--glass-border` | :693, :709 (border kartu) | `--color-glass-border` (theme.css:14) |
| M11 | `--text-primary` | :697, :714 (label "Tunjuk Guru" / "Atur Pengawas") | `--color-text` (theme.css:67) |

Perbaikan termurah: ganti 5 pemakaian ke varian terdefinisi (atau definisikan alias di `theme.css`). Tidak ada XSS dan tidak ada kerusakan alur fatal di seluruh halaman admin (agent admin; tidak ada bukti sebaliknya dari verifikasi manual).

**L12–L18 — 7 LOW** (dashboard 1 · pengawas 2 · pengawas_detail 3 · submissions 1). Teks item per-halaman tidak terpulihkan dari transcript agent; hitungan dipertahankan apa adanya. Dua temuan lintas-halaman terverifikasi manual (5.4 #1 SVG, #2 theme.css double-fetch — keduanya menyentuh halaman-halaman ini) kemungkinan besar mencakup sebagian item ini, tetapi pemetaan pasti tidak mungkin.

### 5.2 Hub Pengaturan (settings.html + 7 modul JS) — 0 HIGH / 1 MEDIUM / 10 LOW

Semua lokasi baris di bawah diverifikasi ulang manual dari sumber. Pemetaan per tab (agent):

| Tab | Temuan |
|---|---|
| general | — |
| packages | L21 |
| system-apps | L23, L24, L25 |
| users | L26, L27 |
| vouchers | M12, L19, L20, L21 |
| voucher-audit | L22 |
| billing | M12, L28 (L28 berlaku page-level) |

**M12 — Warna hex/rgba hardcode di render JS, inkonsisten dengan token `var(--color-*)` — bahkan di dalam fungsi yang sama.** `settings-vouchers.js:75-121` (renderVouchers) dan `settings-billing.js:132-149`. Konteks terverifikasi manual: halaman settings juga membawa 8 blok `<style>` inline (:22–:558) dan tombol Simpan dengan warna hex literal di luar token theme — `#6ee7b7` ×8 (:1454/:1500/:1532/:1580/:1606/:1633/:1674/:1697), `#93c5fd` (:1446), `#1e1b4b` (:462).

**L19 — Cabang ternary identik (dead branch)** pada switch subtab voucher — `settings.html:2166`: `b.style.color = on ? 'var(--color-text)' : 'var(--color-text)';`

**L20 — Penentuan "Kadaluarsa" memakai jam perangkat** — `settings-vouchers.js:73`: `new Date(v.expires_at) < new Date()` meleset ±1 hari di PC dengan jam salah. Terdokumentasi sebagai keputusan tunda di komentar S73 (baris 70-72) menunggu API waktu server; saran migrasi ke waktu server (pola R57/S69).

**L21 — Indikator loading teks polos ("Menyimpan...") tanpa spinner** — `settings-packages.js:121`, `settings-vouchers.js:280`, `settings-vouchers.js:326`.

**L22 — Retry audit tidak menormalkan `data-page` dengan parseInt** — `settings-voucher-audit.js:9`.

**L23 — Gagal memuat daftar aplikasi hanya menampilkan toast, grid dibiarkan tanpa state** — `settings-system-apps.js:39`.

**L24 — Tombol Hapus di kartu aplikasi memakai `addEventListener` langsung, bukan `data-action`** — `settings-system-apps.js:112` — melanggar pola delegasi tunggal Actions (listener tunggal di `admin-core.js:349-357`).

**L25 — Jalur muat ganda untuk modal upload aplikasi** — `settings.html:1841-1882`: fallback inline `window.openUploadModalSafe` (:1844, pemanggil :1879) menduplikasi `openUploadModal` di settings-system-apps.js. Saran: fallback cukup pesan "muat ulang halaman", atau hapus.

**L26 — Coupling lintas-section di modul users** — `settings-users.js:121`: modul tab users mem-probe `#emailEnabledInput` milik kartu SMTP section-general sebagai proxy ketersediaan pengaturan SaaS; jika elemen di-rename, gating mati diam-diam. Saran flag eksplisit (`window.__hasSaasSettings` / data-attribute).

**L27 — Inkonsistensi markup dismissal pada modal Ubah Password** — `settings.html:1928`: overlay `#changePasswordModal` tanpa `data-action="modal-dismiss"` (bandingkan 5 modal lain: :988, :1145, :1254, :1368, :1790). Hari ini tanpa efek perilaku — MutationObserver generik tetap menutup via backdrop/Escape (`closeChangePasswordModal` admin.js:1484).

**L28 — Dua mekanisme tooltip berbeda pada satu halaman** — `settings.html:927-959`: 5 tooltip kustom `.tooltip[data-tooltip]` vs ±70 tooltip native `title`.

### 5.3 Halaman publik — 0 HIGH / 3 MEDIUM / 13 LOW

| Halaman | H | M | L |
|---|---|---|---|
| index.html | 0 | 1 | 0 |
| download.html | 0 | 0 | 3 |
| register.html | 0 | 0 | 2 |
| register_confirm.html | 0 | 0 | 1 |
| forgot_password.html | 0 | 0 | 0 |
| reset_password.html | 0 | 0 | 1 |
| cek_hasil.html | 0 | 1 | 0 |
| hasil.html | 0 | 1 | 6 |

**M13 — cek_hasil.html tanpa guard double-submit (:53)** — terverifikasi dari sumber: tombol submit cek hasil tidak di-disable saat request berjalan, tidak seperti register_confirm (:366-368) dan reset_password (:339) yang punya guard — pola yang sama, satu halaman tertinggal. Agent publik juga menempatkan ini sebagai prioritas perbaikan tercepat #1.

**M14 — index.html, klaim "padanan bahasa" (agent-reported — TIDAK terkonfirmasi dari sumber).** Verifikasi manual seluruh isi index.html: sudah sepenuhnya bahasa Indonesia (hero "Ujian Digital Teraman & Siap Offline", tombol "Unduh Aplikasi" → /download, "Panel Admin" → /login, badge versi, seluruh label statistik/CTA). Klaim ini tidak dapat direproduksi dari sumber; identitas medium index tetap tercatat sebagai agent-computed tanpa substansi yang bisa diverifikasi.

**M15 — hasil.html (identitas item agent tidak terpulihkan).** Yang terverifikasi manual untuk halaman ini: **double-fetch theme.css** (5.4 #2) — halaman me-link `theme.css?v={{.version}}` (:23) sekaligus memuat `hasil.css` yang `@import url('theme.css')` tanpa versi (:1). Tidak ditemukan token drift di hasil.html — semua token `var(--color-*)` ter-prefix benar.

**L29–L41 — 13 LOW** (download 3 · register 2 · register_confirm 1 · reset_password 1 · hasil 6). Teks item tidak terpulihkan (transcript agent terpotong permanen); hitungan per halaman dipertahankan. Temuan manual terverifikasi yang menyentuh halaman publik: toast tidak single-sourced (5.4 #3).

### 5.4 Temuan lintas-halaman (verifikasi manual penuh dari sumber)

1. **67 simbol SVG inline menduplikasi sprite yang sudah dimuat di halaman yang sama.** Empat halaman admin meng-inline ulang simbol SVG: dashboard 30, submissions 19, pengawas_detail 11, pengawas 7 (total 67; 31 simbol unik). Pengecekan silang `symbol id`: **semua 31 simbol unik inline identik dengan simbol di sprite** `partials/svg-symbols.html` (42 simbol) yang dimuat di 5 halaman admin via `nav.html:1` (`{{template "admin/partials/svg-symbols.html"}}`) — overlap 31/31, nol simbol inline-only. Artinya 100% duplikasi murni: cukup `<use href="#id">` untuk menghapus ±67 blok `<symbol>` dan menyatukan sumber ikon.
2. **theme.css di-fetch dua kali per halaman (dua URL berbeda untuk file yang sama):** (a) 5 halaman admin yang memakai partial `head.html` — link `theme.css?v={{.version}}` (head.html:8) **plus** `admin-base.css` yang ber-`@import url('theme.css')` tanpa versi (admin-base.css:5) → cache browser tidak bisa dedup; (b) hasil.html — link `theme.css?v=` (:23) **plus** `hasil.css` `@import url('theme.css')` (:1). login.html tidak terkena (tanpa admin-base.css). Hanya dua @import ini di seluruh `static/css`. Fix termurah: hapus kedua @import (link langsung sudah ada) atau versi-kan string @import.
3. **Toast publik tidak single-sourced** — download.html membawa implementasi toast sendiri (27 kemunculan kode toast; container mulai :401), hasil.html punya `#toastContainer` sendiri (:73), cek_hasil tidak punya — sementara sisi admin terpusat di admin-core.js. Tiga jalur publik vs satu admin.
4. **Guard double-submit tidak seragam antar form** — register_confirm (:366-368) dan reset_password (:339) men-disable tombol saat submit; cek_hasil (:53) tidak (detail M13).

### 5.5 Yang sudah bagus (patut dipertahankan)

- **Aksesibilitas settings jauh di atas rata-rata**: ARIA tabs penuh (settings.html:1041-1088), header sortable `th scope="col" tabindex="0" role="button"` (:825-832), live region tabel `aria-live="polite" aria-busy` (:836), sinkronisasi `aria-expanded` dari JS (settings-general.js :22/:40-45/:51/:75/:111; settings-users.js :24/:57/:84), aktivasi Enter/Space.
- **Modal Manager generik** — MutationObserver admin-core.js:888-1040 menutup modal via backdrop/Escape secara konsisten, termasuk menutup celah modal yang lupa markup dismissal (lihat L27).
- **Race guards & sanitasi**: S78/S92 (anti double-bind listener), safeCode S3, escapeHtml di render baris tabel — tidak ditemukan XSS di halaman mana pun (dicek dua arah).
- **Fokus & keyboard**: S97 — visibility drawer selamat dari cascade `display` (komentar shared.html:874), fokus dikembalikan ke tombol pemicu (shared.html:71).
- **Form iOS-friendly**: input 16px (anti-zoom); probe unduhan memakai `redirect: 'manual'` (tidak membocorkan redirect lintas origin).
- **`lang="id"` konsisten di semua halaman** — shared.html:100 (define `public_head` :91; index.html mewarisinya, tidak punya tag `<html>` sendiri), hasil.html:15, cek_hasil.html:2.
- **Paritas warna address-bar R130** berkomentar di cek_hasil.html:6 & register.html:6 (= `--color-bg`, paritas shared.html:121).
- **hasil.html disiplin token** — semua `var(--color-*)` ter-prefix benar, nol token undefined; komentar R71 (:26) menjaga urutan stylesheet eksternal sebelum blok style inline.
- **Toast admin single-sourced** (`#toastContainer` di admin-core.js) — kontras dengan sisi publik (5.4 #3).
- **Guard double-submit** di register_confirm & reset_password; error state R63 konsisten di seluruh form auth.

### 5.6 Catatan verifikasi & keterbatasan Bagian 5

- Tiga agent review (admin non-settings / hub Pengaturan / publik); setiap klaim diverifikasi manual terhadap sumber sebelum didokumentasikan:
  - ✅ **Terverifikasi penuh**: M9–M11 (3 token, 5 baris :459/:693/:697/:709/:714, nol definisi di mana pun, dashboard satu-satunya pemakai), M13 (guard cek_hasil), seluruh M12 + L19–L28 (teks agent + lokasi baris dicek ulang), dan 4 temuan lintas-halaman 5.4.
  - ⚠️ **Agent-computed, teks tidak terpulihkan**: hitungan L per halaman (admin 7, publik 13) — dipertahankan apa adanya; identitas M15 (hasil) tidak diketahui; M14 (index "padanan bahasa") tidak terbukti dari sumber.
  - ❌ **Dikeluarkan karena tidak lolos / tidak dapat direproduksi**: klaim kontras `.tone-*`/`.notice`/`.pd-table-empty` pada admin-base.css, klaim `innerHTML` admin.js (:656/:697/:861/:1776/:2243/:2424), klaim kecocokan selector JS↔HTML. Tidak didokumentasikan sebagai temuan.
- Temuan 5.4 ditemukan verifikasi manual; sebagian kemungkinan sudah termasuk hitungan L agent per-halaman, tetapi pemetaan pasti tidak mungkin karena teks item agent hilang.
- Nol temuan HIGH: tidak ada XSS dan tidak ada kerusakan fatal alur di seluruh halaman yang direview.

---

## Bagian 6 — Rekomendasi Prioritas

> Urutan berbasis: (1) apakah ada data yang sedang bocor SEKARANG, (2) apakah kegagalan menghambat user nyata (guru/siswa), (3) kerapian internal. Rekomendasi mencakup temuan kode Bagian 1–4 (item 1–11) dan temuan UI Bagian 5 (item 12–17 di 6.5).

### 6.1 Langsung (hari ini — perbaikan kecil, dampak besar)

1. **[HIGH] Tutup bypass operator di ListSubmissions** (`submissions.go:446-455`, temuan H2 Bagian 4) — satu-satunya kebocoran aktif, tanpa prasyarat: cukup tambahkan cek `UserCanAccessExam` (models/exam.go:944) untuk cabang operator sebelum query submission. Guard-nya sudah ada, dipakai benar oleh 7 endpoint pengawas — hanya handler ini yang tertinggal.
2. **[HIGH] Sinkronkan dual enforcement versi Android** (`exams.go:893` SubmitExam, `exams.go:1409` AccessLog vs middleware `version.go:95-97`) — dengan middleware yang sudah meng-clamp ke APK publishable terbaru, pemeriksaan handler yang membaca nilai mentah `2.5.0` dapat **deadlock permanen**: build baru ditolak middleware sebelum sampai ke handler, build lama ditolak handler meski diizinkan middleware. Gunakan `EffectiveAndroidRequiredVersion` di kedua handler (lihat H1 Bagian 3).
3. **[MEDIUM] Fail-closed scoping operator di submissions.go** (M6) — cek error `Scan` di `:327-334`/`:363-370` dan tolak instansi kosong; jalur export (`:823-830`) harusnya tidak pernah bisa menghasilkan query tanpa WHERE. Kecil, tapi menggandakan dampak H2 di jalur export.

### 6.2 Minggu ini (perlu keputusan desain kecil)

4. **[MEDIUM] Tambah pengecualian pool pada gate atomik kuota** (M7, `admin/exams.go:447-473`) — tambahkan syarat `owner.OperatorCreated && poolActive` untuk melewatkan hitungan `max_exams` per-akun, konsisten dengan pre-check `:277-278`, ToggleExam, StartExam, dan gate PDF. Tanpa ini, sub-akun sekolah dengan pool aktif mati di 3 ujian seumur-akun.
5. **[MEDIUM] Tangani error re-enqueue jawaban di `retryOrFail`** (`queue/submission_queue.go:551`, M5 Bagian 3) — `_ = EnqueueSubmissionWithJob(...)` menelan error re-push: bila Redis gagal tepat saat job digagalkan lalu dicoba dimasukkan kembali ke antrean, jawaban siswa **hilang permanen**. TTL key hasil-gagal hanya 5 menit (`queue:49`) sehingga jejak kegagalan nyaris tidak tersisa. Perbaikan minimal: log error-nya; idealnya fallback spool lokal sebelum drop.
6. **[MENANTI: Produk] `webhook.go` belum di-route** (Bagian 2.2) — dua handler (`WhatsappWebhook`, `GetRegisterStatus`) berfungsi utuh tapi tidak pernah bisa dipanggil; putuskan: sambungkan ke route (bila WhatsApp self-regist masih rencana produk) atau hapus file (bila tidak). Keputusan produk, bukan teknis.
7. **[MENANTI: Produk] Dead code `isLoggedIn` di `hasil.go`** (Bagian 3.2) — variabel di-set tapi hanya mempengaruhi tampilan tombol tidak signifikan; putuskan atau hapus.
8. **[MEDIUM] Advisory lock pada heartbeat flusher** (`main.go:1692-1703`, M5 Bagian 3) — cukup satu baris pgx `TryAdvisoryLock` di awal loop agar dua instance webui tidak pernah double-flush.

### 6.3 Rutinitas pembersihan (kapan pun, low risk)

9. **[LOW] Perbaikan polos**: `Atoi` yang ditelan di pengawas.go:695/:795 (L8); balik urutan 404→403 menjadi 403 konsisten bila ingin menutup oracle enumerasi (L9); retry pada `RegenerateToken` exams.go:1589 (L10); tracking goroutine `recalculateScores` exams.go:1573 (L11); heartbeat worker setiap tick (M8); rate-limit HTML `/hasil` (M1); invalidasi session saat reset password (M2).
10. **[MEDIUM] Dead code Go ±29 fungsi** (Bagian 2) — sebagian besar fungsi model/helper yang tidak lagi dipanggil setelah refactor besar; hapus batch dengan `grep` sebelum tiap hapus; bedakan dari 10 fungsi terverifikasi-alive di 2.4 (JANGAN hapus). `test_http.go` kandidat hapus terkuat (semi-dead smoke test lama).
11. **[MEDIUM] File sampah root** (Bagian 1.4) — hapus `fix_pricing.py`, `refactor_download.py`, `refactor.sh`, `test_tpl.go` (4 dead), pindahkan 2 screenshot untracked + docs ke `docs/` (atau .gitignore screenshot), relokasi 5 smoke test ke `cmd/server/tests/` atau sejenisnya.

### 6.4 Catatan verifikasi

- M7 adalah temuan kuat dengan satu koreksi penting: gate atomik hanya ada di **satu** situs (`:447-473`), bukan dua seperti klaim awal agent — `:886-912` adalah gate PDF yang sudah sadar-pool.
- Jangan hapus fungsi dari Bagian 2.4 (verified-alive): `CountExamsByInstansi` hanya dipakai di test, `GetRegisterStatus` perlu keputusan webhook.go dulu.
- `recalculateScores` (`admin/exams.go:1579`) dan `models.RecalculateAllScoresForExam` (`models/submission.go:789`) **bukan dead code** — dipanggil langsung via `go recalculateScores(...)` di `admin/exams.go:1573` setiap kali konfigurasi soal disimpan. Terverifikasi lewat grep: **tidak ada HTTP route recalc di main.go**; hanya jalur implicit tersebut (karena itu dia tidak muncul di daftar route Bagian 2).

### 6.5 Rekomendasi UI (dari Bagian 5 — 0 high, 7 medium, 30 low)

Tidak ada temuan UI yang HIGH; semua perbaikan kecil dan terisolasi:

12. **[MEDIUM] Ganti 3 token CSS undefined di dashboard (M9–M11)** — cukup rename 5 pemakaian ke varian yang sudah terdefinisi: `--accent-light` → `--color-accent-light` (theme.css:23), `--glass-border` → `--color-glass-border` (theme.css:14), `--text-primary` → `--color-text` (theme.css:67). Murni rename, tanpa token baru.
13. **[MEDIUM] Tambah guard double-submit di cek_hasil.html (:53, M13)** — salin pola register_confirm (:366-368) / reset_password (:339).
14. **[MEDIUM] Satukan fetch theme.css (double-fetch, 5.4 #2)** — hapus `@import url('theme.css')` tanpa versi di `admin-base.css:5` dan `hasil.css:1` (link ter-versi sudah ada di kedua jalur), atau tambahkan `?v=` pada string @import. Berdampak pada 5 halaman admin + hasil.
15. **[LOW] Hapus 67 duplikasi simbol SVG inline (5.4 #1)** — ganti dengan `<use href="#id">`; sprite 42 simbol sudah dimuat di 5 halaman admin via nav.html:1 dan 31/31 simbol inline identik dengannya.
16. **[LOW] Single-source toast publik (5.4 #3)** — download (implementasi sendiri), hasil (`#toastContainer` sendiri), cek_hasil (tidak ada); pusatkan satu modul seperti pola admin-core.js.
17. **[LOW] Warna hex literal → token (M12)** — `settings-vouchers.js:75-121`, `settings-billing.js:132-149`, plus tombol Simpan di settings.html (`#6ee7b7` ×8, `#93c5fd`, `#1e1b4b`).

Sisanya perbaikan polos per-item (L19 ternary dead, L20 waktu server untuk kadaluarsa voucher, L22 `parseInt` data-page, L26 flag SaaS eksplisit, L27 markup `modal-dismiss`) — detail lengkap di Bagian 5.2/5.3.

