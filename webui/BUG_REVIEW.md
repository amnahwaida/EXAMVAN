# Laporan Review Bug — EXAMVAN webui

Tanggal: 2026-07-26 · Metode: review manual + workflow multi-agent (7 finder per-subsistem, tiap temuan diverifikasi secara adversarial). Status build: `go build ./...` ✅, `go vet ./...` ✅.

Ringkasan: **2 CRITICAL, 8 HIGH, 7 MEDIUM, 6 LOW** dikonfirmasi. Beberapa temuan "plausible" (bergantung konfigurasi/deploy) dan yang ditolak dicatat di bawah.

Prioritas perbaikan: **#1, #2 (XSS), #3, #4, #9 (kebocoran/akses lintas-tenant + submit gagal)** dulu — dampak keamanan & integritas ujian paling besar.

---

## ✅ STATUS PERBAIKAN (2026-07-26)

Semua temuan CRITICAL/HIGH/MEDIUM/LOW **sudah diperbaiki** + plausibles yang aman ditinjau. `go build`, `go vet`, `go test` semua lolos. Ringkasan perubahan:

- **CRITICAL XSS (MAC & username):** `sanitizeMAC` diperketat ke `[A-Za-z0-9:._-]` + diterapkan di `RequestApproval`; `pengawas_detail.html` escape mac di onclick; `admin.js` `escapeHtml(jsEscape(username))`; validasi username server-side `^[a-z0-9._-]{3,32}$` (`models.IsValidUsername`) di register + CreateUser.
- **HIGH IDOR:** export submission kini cek `checkExamOwnership`; WS `/ws/:room_id` cek `models.UserCanAccessExam`; bulk delete/toggle pakai `models.FilterAccessibleExamIDs` untuk semua non-super (operator tak lagi lintas-instansi; pengawas tak bisa hapus). **Seluruh endpoint monitoring `pengawas.go`** (detail page, submissions, approvals list, set-approval, dan list `PengawasExams`) kini scoped per-instansi via `UserCanAccessExam`/`UserCanControlExam` — sebelumnya operator diperlakukan "privileged" tanpa cek instansi (bocor PII + `active_token` lintas-tenant). Instansi `''` dan sentinel `'personal'` (bucket default) tak dianggap tenant nyata.
- **HIGH exam-flow:** rate-limit submit per `exam:mac` (+ route per-IP dinaikkan utk kelas NAT); `flushBatch` savepoint per-row & `storeResult` hanya setelah commit; WS `trySend` mutex-guarded; `GetPricingMap` pakai key `price_*` (billing tak lagi Rp 0); guard eskalasi role operator mencakup field `role` singular.
- **MEDIUM/LOW:** DOKU refund/void kini diproses saat approved + `hmac.Equal` + signature tak di-log + unmark on failure; entitlement merge role (SuperAdmin tak di-demote) + clawback role saat refund; `SaveQuestions` tak menghapus roster pengawas; `ListUsers` sertakan package; migrasi `ALTER amount` dikondisikan; expiry edit-user dikonversi UTC; map lockout dibatasi; stats dedup + COALESCE; `ToggleUserStatus` guard operator; `ListTransactions` clamp per_page + rows.Err; null-scan notes/proof di-COALESCE; CORS tak gabung `*`+credentials; `AccessLog` cek token; `jakartaLocation()` fallback UTC.
- **#14 `GET /api/exams` — SUDAH DIPERBAIKI (scoped per-sekolah, by CODE):** endpoint kini mewajibkan **kode sekolah unik** via query `?instansi=<kode>` (alias `?kode=` / `?code=`), dicocokkan HANYA dengan `instansi_code` (case-insensitive). Nama instansi TIDAK dipakai (tidak unik → bisa bocor antar sekolah bernama sama). Tanpa kode → daftar kosong (`data: []`), bukan daftar lintas-tenant. Cache Redis di-key per-sekolah. Fungsi lama `models.ListActiveExams` (list global tanpa scope) dihapus.
  - **PERUBAHAN KONTRAK API:** layar "Daftar Ujian" harus mengirim **kode instansi** (`instansi.code`, mis. `SCH-XXXXXXXX`). Response tetap `{"success":true,"data":[...]}`. Tanpa/salah kode → list kosong. Sarankan menambahkan input "Kode Sekolah" di layar konfigurasi server, atau pindah ke alur token-only.

---

## ✅ PERBAIKAN TAMBAHAN — ALUR UJIAN (9 Agustus 2026)

Review lanjutan tiga temuan di alur submission/penjadwalan. **Semua sudah diperbaiki + ditest** (`go build`, `go vet`, `go test` lolos; status 9 Agustus 2026). Catatan lengkap di [README.md → Perbaikan Ujian Serentak](../README.md#perbaikan-ujian-serentak--submit-async-jadwal--perangkat-bersama-9-agustus-2026).

### A. Submit async tidak punya jalur kembali ke skor siswa (TINGGI) — endpoint `GET /api/exams/:exam_id/result`
- **Masalah lama:** Saat Redis aktif, `SubmitExam` men-*enqueue* job → `status:"queued"`, `score:nil`, `job_id`; **tidak ada endpoint** untuk mem-poll hasilnya. Key Redis `ResultKeyPrefix` tak pernah dibaca siapa pun; `retryOrFail` (maks 3×) bisa gagal senyap sementara siswa mengira jawaban sudah terkumpul.
- **Fix:** endpoint publik **`GET /api/exams/:exam_id/result`** (`api.ExamResult`, `cmd/server/main.go:515`) membaca key hasil Redis worker → bila TTL (5 mnt) habis, **fallback ke DB** via `GetLatestSubmissionByIdentity` (cocokkan `mac_address` + `identity_data` dari submit) → jika belum ada, balas `pending`. Respons meniru bentuk submit sinkron (`done`/`pending`/`failed`).
- **Test:** `TestExamResultReturnsScoreFromDB`, `TestExamResultPending` di `internal/handlers/api/exams_test.go`.

### B. `end_time` tak pernah ditegakkan server-side (TINGGI)
- **Masalah lama:** `ExamByToken`/`SubmitExam` hanya cek `IsActive() && ExamStartedAt != nil`; ujian di luar jendela masih bisa dibuka/disubmit (hanya aplikasi yang menghitung `end_time`). `time_limit:null` juga ditampilkan.
- **Fix:** helper `examScheduleEnded(exam, now)` (`internal/handlers/api/exams.go:902`) — `now > end_time + 60s` (grace `scheduleGraceEnd`, :895) untuk submit yang datang tepat di tenggat. Diterapkan di **4 jalur**: `ExamByToken`, `ExamPDF`, `SubmitExam`, `AccessLog` (masing-masing `403 "Waktu ujian telah berakhir"`). `CompleteExam` **sengaja tidak di-gate** — klien memanggilnya tepat setelah submit di batas waktu untuk membersihkan heartbeat. `end_time` kini dikembalikan di payload `ExamByToken`.
- **Test:** `TestExamScheduleEnded` (unit, 5 kasus), `TestExamByTokenRejectsPastEndTime`, `TestExamByTokenAllowsInsideWindow`.

### C. Dedup submission per `mac_address` + placeholder per-MAC dihitung in-progress selamanya (MENENGAH)
- **Masalah lama:** dedup murni per MAC → perangkat yang dipakai bergantian 2 siswa disatukan jadi satu baris; baris kosong per MAC dihitung in-progress selamanya; device yang diinstal ulang/ganti → terhitung 2×.
- **Fix:** dedup kini di-**scope** dengan `exam_number` saat tidak kosong — di `upsertSubmissionRow` (`internal/queue/submission_queue.go`) dan `CreateSubmission` (`internal/models/submission.go`), sehingga satu perangkat mempertahankan **dua baris terpisah** untuk dua siswa. Identitas device tetap `mac_address` dari `resolveDeviceId()` (`DEVICE:<AndroidId>`, stabil walau di-uninstall/reinstall). Nomor ujian kosong → perilaku lama (satu baris per perangkat) dipertahankan.
- **Test:** `TestUpsertSubmissionRowTwoStudentsShareDevice` di `internal/queue/submission_queue_test.go`.

---

## ✅ AUDIT PERMUKAAN VOUCHER — KLIEN & WEB (9 Agustus 2026)

Audit lanjutan dari kebijakan "akun sub (dibuat operator) tidak boleh klaim voucher": memastikan tidak ada permukaan lain selain tab Paket & Voucher (`/admin/settings#billing`, menggantikan `/admin/billing`) yang menampilkan aksi klaim kepada akun sub. **Hasil: tidak ditemukan permukaan lain — tidak ada penyesuaian UI yang diperlukan.** Kontrak routing dikunci test `TestNoPublicVoucherRoutes` (tanpa database). Catatan lengkap di [README.md → Kebijakan Klaim Voucher Akun Sub](../README.md#kebijakan-klaim-voucher-akun-sub-dibuat-operator).

### A. Klien Android & desktop — tidak punya UI klaim voucher (0 kemunculan)
- Kedua klien (Android `api/ApiClient.kt`, desktop `examvan/api.py` + salinan `desktop/pkg-build/`) adalah aplikasi **ujian berbasis token**, bukan aplikasi manajemen akun. Di kode sumber klien (Java/Kotlin, layout & string `res/`, Python desktop, salinan `pkg-build/`) pencarian istilah `voucher`/`redeem`/`billing`/`claim`/`klaim` menghasilkan **0 kemunculan** (kata `paket` hanya muncul di skrip packaging `desktop/install.sh` dalam konteks manajer paket OS, bukan paket voucher).
- Endpoint yang dipanggil hanyalah rute ujian publik (`/api/health`, `/api/exams`, `/api/exams/request-approval`, `/api/exams/token/{token}`, `/api/exams/{exam_id}/pdf`, `/api/exams/{exam_id}/submit`). Endpoint claim/aktivasi (`/admin/api/vouchers/*`) bersifat **session-based admin** tanpa versi publik — klien token secara teknis pun tidak bisa memanggilnya.

### B. Permukaan web publik — bersih (0 referensi voucher)
- `templates/public/` (index, register, register_confirm, forgot_password, reset_password, hasil, download, shared), `templates/admin/login.html`, `internal/handlers/public/`, serta JS/CSS publik: **0 referensi** voucher/klaim/billing/paket. Satu-satunya kecocokan adalah teks lisensi `static/js/fingerprintjs.min.js` ("CLAIM, DAMAGES") — bukan kode aplikasi.

### C. Halaman admin lain yang terjangkau akun sub — sudah terkunci
- Dashboard/submissions/pengawas: 0 referensi voucher. Satu-satunya permukaan berisi form klaim adalah tab Paket & Voucher di hub Pengaturan (`/admin/settings#billing`) — sudah menyembunyikan form klaim + daftar "Paket yang Sudah Anda Klaim" untuk akun sub (kartu penjelasan "Akun Sub (Dibuat Operator)").
- Menu "Kelola Voucher"/"Pengaturan Paket" dibungkus `{{if $isSuper}}` di tab bar Pengaturan (`admin/partials/settings-tabs.html`); sejak 17 Agustus 2026 semua bagian settings pindah dari dropdown header `nav.html` ke **satu halaman `/admin/settings`** (`templates/admin/settings.html`) dengan tab client-side tanpa reload dan JS per-bagian dimuat lazy (`static/js/settings-*.js`); **sejak 17 Agustus 2026 (lanjutan) halaman settings lama dihapus dan keenam URL-nya 302-redirect ke tabnya** (`SettingsRedirect` di `internal/handlers/admin/settings.go`); toggle "Redeem Kode Promo / Voucher" di panel "Kontrol Monetisasi" (`settings.html`) hanya dirender di dalam panel SaaS `{{if eq .admin_role "superadmin"}}` — keduanya bukan aksi klaim per akun.

### D. Kontrak rute
- `TestNoPublicVoucherRoutes` (`cmd/server/routes_voucher_public_test.go`) menginspeksi tabel rute hasil `registerRoutes` asli: tidak ada rute ber-`voucher`/`redeem`/`activate` di luar prefix `/admin`, dan `POST /admin/api/vouchers/redeem`, `POST /admin/api/vouchers/activate`, `GET /admin/api/vouchers/mine` tetap terdaftar.

---

## ✅ PERBAIKAN — SETTING STORAGE DEFAULT PAKET PENDAFTARAN (9 Agustus 2026)

Gap ditemukan di halaman Kelola User: panel **"Default Paket Pendaftaran"** tidak punya pengaturan storage, padahal kunci **`default_max_storage_size`** sudah ada di `saas_settings` (default 50 MB = `52428800` byte) dan **sudah diterapkan** ke akun baru di `/register` (`cmd/server/main.go`, `MaxStorageSize`). Yang kurang hanyalah eksposnya: `GET/POST /admin/api/saas-settings` tidak memuat field-nya dan `users.html` tidak punya input — sehingga kuota storage akun baru terkunci 50 MB tanpa bisa diubah dari UI. Catatan lengkap di [README.md → Konfigurasi Default Paket Pendaftaran — Kuota Storage](../README.md#konfigurasi-default-paket-pendaftaran--kuota-storage-kapasitas-disk).

**Fix (end-to-end):**

- **`internal/handlers/admin/settings.go`** — GET kini mengembalikan `default_max_storage_size_mb` **dan** `storage_free_mb` (sisa disk partisi `STORAGE_PATH` via `getFreeDiskSpace`); POST menerima & menyimpan `default_max_storage_size_mb`.
- **Field disimpan bersyarat (pointer `*float64`):** bila `default_max_storage_size_mb` tidak dikirim (mis. UI lama ter-cache saat rollout), nilai tersimpan **tidak disentuh**. Ini penting karena `0` adalah nilai sah (tidak terbatas) — pola `<= 0 → reset ke default` yang dipakai field lain tidak bisa dipakai di sini, karena field absen tidak boleh mengubah quota yang sudah dikonfigurasi menjadi unlimited.
- **`templates/admin/settings.html`** (seksi **Pengaturan Umum**, `#general`; `users.html` lama dihapus & redirect, Default Paket Pendaftaran ikut pindah dari Kelola User dalam redesign 5 tab) — input **"Maks Storage (MB)"** di panel Default Paket Pendaftaran (grid 5 kolom desktop, 2 kolom mobile) + hint sisa disk.
- **`static/js/admin.js`** — `loadSaasSettings`/`saveSaasSettings` memuat/mengirim field; atribut `max` input di-set dari `storage_free_mb` (native form validation ikut aktif) + hint dinamis via `fmtStorageSize`.
- **Validasi kapasitas disk:** nilai **negatif ditolak**; nilai positif yang **melebihi sisa disk ditolak** (HTTP 400, pesan jelas dalam GB). Validasi dijalankan **sebelum penulisan setelan apa pun** — penolakan tidak meninggalkan partial-save (mengikuti pola validasi Turnstile yang sudah ada). Bila sisa disk **tidak dapat ditentukan** (`getFreeDiskSpace` = 0, mis. path belum tersedia), validasi dilewati (fail-open) agar penyimpanan setelan lain tidak terblokir.
- **Test:** `TestSaasSettingsDefaultMaxStorageSizeRoundtrip` (`internal/handlers/admin/saas_settings_test.go`) — router test memakai temp-dir agar free-disk deterministik; menguji roundtrip 50 (default saat key kosong) → 250 MB → 0 (unlimited), penolakan `1e12` MB (melebihi disk; DB tidak berubah) dan `-5` MB (negatif), serta verifikasi `storage_free_mb` di respons GET.

**Perluasan — validasi disk di Tambah User & Atur Limit per-user (`CreateUser`/`EditUser`):**

- Helper yang sama (`validateStorageQuota`, `internal/handlers/admin/users.go`) kini dipakai di `CreateUser` dan `EditUser`: nilai negatif ditolak, dan nilai positif > sisa disk ditolak (HTTP 400, pesan dalam GB) — **sebelum** akun dibuat / sebelum field lain diubah (tanpa user/field setengah jadi). `0` (tidak terbatas) selalu diterima; fail-open saat sisa disk tak dapat ditentukan. Sebelumnya kedua endpoint menyimpan `max_storage_size` apa adanya tanpa batasan.
- `UsersPage` kini mengirim `storage_free_mb` ke template (semua role): input **"Maks Storage (MB)"** di form Tambah User diberi `max` + `title` sisa disk (server-rendered), dan `window.__storageFreeMb` dipakai modal Atur Limit (set `max`+`title` saat dibuka) serta pre-check klien di `createUser`/`submitEditUser` — server tetap lapisan validasi final.
- **Test:** `TestUserStorageQuotaDiskCap` (`internal/handlers/admin/saas_settings_test.go`) — CreateUser tolak `1e12` & `-5` MB (tidak ada user dibuat), terima 250 MB (tersimpan `250*1024*1024` byte); EditUser tolak `1e12` MB (field lain tak berubah), terima `0` (unlimited).
- **Indikator dashboard:** halaman **Dashboard** kini menampilkan **"Sisa Disk Server"** (sisa kapasitas fisik partisi `STORAGE_PATH`) sebagai **kartu statistik atas** (`stat-card stat-disk`, amber, tepat setelah kartu Storage; `dashboard.go` + `dashboard.html`) — **khusus SuperAdmin**: role lain tidak melihat kartu/label ini, dan `GET /admin/api/stats` menyertakan `server_disk_free_mb` (MB) hanya untuk superadmin.
- **Test stats API & halaman Dashboard** (`internal/handlers/admin/saas_settings_test.go` + `dashboard_page_test.go`):
  - `TestStatsServerDiskFree` — GET `/admin/api/stats` sebagai superadmin: auth-gate **401** tanpa sesi (API-style `Accept: application/json`), `server_disk_free_mb > 0` (temp-dir di disk nyata), aggregate benar (total/active/inactive/storage_mb), dan konsisten dengan `getFreeDiskSpace` pada direktori yang sama (±1 MB tolerance untuk menyerap noise background write).
  - `TestStatsScopeByRole` — scoping per-role endpoint stats (wiring produksi `AuthRequired → FeatureLockRequired` saja, tanpa `SuperAdminRequired`): **superadmin** melihat semua exam; **operator** hanya exam yang dibuat akun dengan `instansi` sama; **guru** hanya `created_by = saya OR delegated_to = saya`; **pengawas-only** hanya exam yang ditugaskan lewat `exam_pengawas`; **guru+pengawas** melihat union ketiganya. `server_disk_free_mb` dilaporkan **hanya untuk superadmin** (role lain = 0/tidak disertakan).
  - `TestDashboardRendersServerDiskFree` — merender **halaman HTML nyata** `/admin/dashboard` (handler `Dashboard()` + template asli `dashboard.html`/partials, bukan stub): tanpa sesi → **302 redirect** ke `/login` (HTML page, bukan 401 API); sebagai superadmin → **200** dengan kartu `stat-card stat-disk` + label **"Sisa Disk Server"**; nilai `server_disk_free` ter-render bukan "—" (fallback), format `X.XX GB`/`X.X MB` yang > 0, dan jatuh dalam rentang **snapshot `getFreeDiskSpace` sebelum & sesudah request ± 8 MB** (margin menutupi granularity tampilan GB ≈ 10.24 MB/tick; bounding sebelum/sesudah lebih tahan flake daripada toleransi tetap karena sisa disk bisa bergeser antar panggilan statfs).
  - `TestDashboardHidesServerDiskForNonSuper` — halaman yang sama sebagai **guru (non-superadmin)** → **200** tanpa `stat-card stat-disk` dan tanpa label **"Sisa Disk Server"** (kartu Storage tetap tampil sebagai sanity bahwa halaman ter-render utuh) — menutup sisi negatif dari guard `{{if .is_super}}` di template.

**Perluasan — cap disk di alur voucher & paket (storage + PDF):**

> Ringkasan lengkap untuk pengguna/operator: [README.md → Cap Disk di Alur Voucher dan Paket](../README.md#cap-disk-di-alur-voucher-dan-paket).

- `parseCustomVoucherInto` (`vouchers.go`) memvalidasi **`custom_max_storage_size_mb` dan `custom_max_pdf_size_mb`** (voucher single & batch): negatif ditolak eksplisit, input tidak valid jatuh ke default (100 MB / 1 MB), lalu dicek terhadap sisa disk; `0` = tidak terbatas. Helper `voucherMBToBytes` (yang diam-diam mengubah negatif jadi default) dihapus.
- `SavePackageSettingsHandler` (`packages.go`) memvalidasi **`max_storage_mb` dan `max_pdf_size_mb`** per paket **sebelum transaksi menulis** — negatif ditolak sebelum clamp minimum 1 MB, dan seluruh paket divalidasi terhadap **satu snapshot `freeBytes`** (satu panggilan statfs per request; bila salah satu paket invalid, seluruh transaksi di-rollback tanpa partial-save).
- **SaaS & per-user:** "Maks Upload (MB)" di panel **Default Paket Pendaftaran** (`handleSaasSettingsPost`, `settings.go`) dan field `max_pdf_size_mb` di **CreateUser/EditUser** (`users.go`) kini divalidasi dengan `validatePDFQuota` yang sama — negatif ditolak, 0 = tidak terbatas, > sisa disk ditolak (400) **sebelum** penulisan apa pun. UI-nya: hint `defaultPdfHint` + `max = min(sisa disk, 100 MB)` di `loadSaasSettings`, `max` + pre-check di `createUser`/`submitEditUser`/`openEditUserModal` (`admin.js`), dan `max` server-rendered di `pdfSizeInput` (`users.html`). Test baru: kasus PDF di `TestSaasSettingsDefaultMaxStorageSizeRoundtrip` (tolak `1e12` & `-5` MB, terima 10 MB tersimpan persis) dan `TestUserStorageQuotaDiskCap` (CreateUser tolak `1e12`/`-5` & terima 10 MB; EditUser tolak `1e12` tanpa partial-apply, terima 20 MB).
- Helper di-generalisasi: `validateMBQuota(label, freeBytes, mb)` (`users.go`) sebagai inti bersama, dengan `validateStorageQuota`/`validateStorageQuotaFree` (label "Maks Storage") dan `validatePDFQuota`/`validatePDFQuotaFree` (label "Maks Ukuran PDF"). Pesan HTTP 400 konsisten, mis. *"Paket guru: Maks Ukuran PDF (999999999.00 MB) melebihi sisa kapasitas disk server (31.03 GB)."*
- UI: halaman **Vouchers** (input "Maks Upload (MB)" & "Maks Storage (MB)", single/batch) dan **Paket** (`renderTable` + pre-check `savePackages`) diberi `max` + `title` sisa disk via `storage_free_mb` / `window.__storageFreeMb` — server tetap lapisan final. Untuk input **PDF**, `max` dibatasi `min(sisa disk, 100 MB)` (100 MB = `maxFileSize`, batas upload global di `exams.go`) supaya kuota tidak lebih besar dari batas yang bisa dieksekusi; pre-check paket juga menolak > 100 MB dengan pesan khusus.
- **Test:** `TestVoucherPackageStorageDiskCap` diperluas — voucher single & **batch** tolak PDF `999999999` MB dan `-5` MB (tidak ada voucher dibuat), voucher PDF 10 MB tersimpan `10*1024*1024`; paket tolak storage/PDF `1e12` & `-5` MB, terima storage 250 MB & PDF 20 MB (tersimpan persis); payload multi-paket dengan satu nilai invalid → rollback total (paket pertama tidak berubah).

---

## ✅ PERBAIKAN — ATRIBUSI SUB-AKUN PER-OPERATOR `created_by` + INDEX (10 Agustus 2026)

Gap pada kuota bucket `"personal"` yang selama ini didokumentasikan sebagai *approximation*: flag `operator_created` tidak bisa membedakan **operator mana** yang membuat sebuah sub-akun, sehingga kuota bucket personal menghitung seluruh sub-akun operator sebagai satu kolam bersama (sub-akun operator lain ikut terhitung → kuota bisa meleset), dan migrasi saat operator klaim sekolah (`UpdateInstansi`) ikut menyapu sub-akun operator personal lain. Catatan lengkap di [README.md → Kebijakan Klaim Voucher Akun Sub](../README.md#kebijakan-klaim-voucher-akun-sub-dibuat-operator).

**Fix:**

- **Kolom `admin_users.created_by`** (`INTEGER REFERENCES admin_users(id) ON DELETE SET NULL`, migrasi idempoten di `webui/internal/database/schema.sql`) — mencatat id operator yang membuat akun; di-set oleh `CreateUser` saat caller operator. `ON DELETE SET NULL`: menghapus operator tidak pernah diblokir FK; sub-akun yang sudah dipindah SuperAdmin ke instansi lain jatuh ke fallback legacy.
- **`loadOperatorAccountQuota`** (bucket `"personal"`, `webui/internal/handlers/admin/users.go`) kini menghitung `created_by = <id operator>` — presisi per-operator. Baris legacy (`created_by` NULL, dibuat sebelum kolom ini ada dan tidak bisa diatribusikan) tetap dihitung konservatif ke semua operator personal via `created_by IS NULL AND operator_created`.
- **`UpdateInstansi`** — migrasi sub-akun saat klaim sekolah kini `WHERE created_by = <id operator>`: hanya sub-akun milik operator itu yang pindah ke instansi baru.
- **Route `/instansi/update` kini management-level** (`webui/cmd/server/main.go`): registrasi lama `csrfAPI.POST("/instansi/update")` (bisa dipanggil **semua user terautentikasi**, termasuk guru/pengawas — yang bisa mengganti nama instansi sekolah) diganti `adminUsers.POST("/instansi/update")` di bawah `AdminManagementRequired` (SuperAdmin & Operator saja, 403 untuk lainnya) dan `users.go` memperbarui header komentar endpoint-nya. UI mengikuti: kartu **"Instansi (Klik untuk Ubah)"** di dashboard hanya dirender untuk dua role itu (`dashboard.html`), dan modal onboarding instansi (`needs_instansi`, `helpers.go` + `nav.html`) hanya muncul untuk operator paket sekolah yang instansinya belum ditetapkan (kosong / `"personal"` / `"Belum Ditetapkan"`).
- **Paket akun sub terkunci** — operator tidak bisa memilih/mengubah paket langganan akun yang dibuatnya: `CreateUser` memaksa label `free`, `EditUser` mengabaikan field `package` untuk caller operator (guard `strings.EqualFold`+`TrimSpace` untuk role Operator juga diselaraskan di kedua handler) — UI menyembunyikan pemilih paket untuk operator (`users.html` + `admin.js`).
- **Index `idx_admin_users_created_by`** (`CREATE INDEX IF NOT EXISTS idx_admin_users_created_by ON admin_users(created_by)`) — mempercepat kedua pencarian per-operator (kuota di setiap cek `CreateUser` + halaman billing, dan migrasi klaim sekolah) dari full-table scan menjadi index scan saat data sub-akun membesar. Idempotent — aman dijalankan ulang setiap boot.
- **Test** (`webui/internal/handlers/admin/subaccount_voucher_policy_test.go`): `TestPersonalBucketQuotaAndMigrationScopedPerOperator` (dua operator personal berbagi bucket; kuota & migrasi scoped per-operator — **gagal sebelum fix**, ketika keduanya menghitung 3 dan migrasi menyapu sub-akun milik operator lain), `TestCreatedByDeleteSetsNull` (FK `ON DELETE SET NULL`: operator tetap bisa dihapus walau sub-akunnya sudah dipindah; sub-akun selamat dengan `created_by` NULL), `TestInstansiUpdateRouteRequiresManagementRole` (403 untuk guru/pengawas, 200 operator/superadmin), `TestUpdateInstansiMigratesPersonalBucketSubAccounts`, `TestOperatorCannotCreateOperatorAccount` (varian non-kanonik role), `TestOperatorCannotAssignPackageToSubAccount`, `TestOperatorQuotaEnforcedForPersonalInstansi`. UI instansi: `TestDashboardInstansiCardOnlyForManagementRoles`, `TestMandatoryInstansiModalOnlyForUnclaimedSchoolOperator`, `TestAllNavIncludesForwardNeedsInstansi` (`dashboard_page_test.go`).

Audit lengkap index untuk seluruh kolom FK (termasuk 4 index baru: `idx_admin_users_instansi_id`, `idx_vouchers_created_by`, `idx_voucher_redemptions_voucher_id`, `idx_voucher_redemptions_user_id`) ada di [README.md → Index Database](../README.md#index-database-kolom-fk-yang-sering-di-query).

---

### 1. Stored XSS lewat MAC address di antrean approval pengawas
- **Lokasi:** `templates/admin/pengawas_detail.html:1174,1177` (render) + `internal/handlers/api/exams.go:339` (`RequestApproval`, insert tanpa sanitasi) + route publik `cmd/server/main.go:473`.
- **Masalah:** Badge MAC di baris 1163 di-escape (`esc()`), tapi baris 1174/1177 menyisipkan `a.mac_address` **mentah** ke atribut `onclick="setApproval('...')"` lalu `tbody.innerHTML = html` (1187). `RequestApproval` menyimpan `mac_address` apa adanya, dan endpoint `POST /api/exams/request-approval` **tanpa autentikasi** (hanya rate-limit + version check).
- **Eksploitasi:** Penyerang anonim kirim `mac_address = '"><img src=x onerror=alert(document.cookie)>'` dengan `exam_id` tebakan. Saat pengawas membuka `/admin/pengawas/{id}`, payload dieksekusi di sesi pengawas (pencurian CSRF token, aksi admin) — **tanpa interaksi**.
- **Fix:** Escape `mac_address` untuk konteks atribut/JS (atau pakai `data-*` + `addEventListener`), dan validasi whitelist karakter MAC saat insert di `RequestApproval`.

### 2. Stored XSS lewat username di tombol aksi "Kelola User"
- **Lokasi:** `static/js/admin.js:1319,1325` + `static/js/admin-core.js:86` (`jsEscape`) + tanpa validasi username di `cmd/server/main.go:899` (register) & `internal/handlers/admin/users.go:213` (create).
- **Masalah:** `jsEscape(user.username)` disisipkan ke atribut `onclick="deleteUser(…, '…')"` yang di-set via `innerHTML`. `jsEscape` hanya escape `\ ' \n \r` — **tidak** escape `"` maupun `< >`. Username tidak divalidasi karakter di mana pun (hanya panjang ≥ 3 + keunikan).
- **Eksploitasi:** Penyerang anonim register username `aaa"><img src=x onerror=…>`. Saat SuperAdmin membuka halaman Kelola User, payload dieksekusi di sesi SuperAdmin → **unauthenticated → SuperAdmin XSS**.
- **Fix:** Pakai `escapeHtml` (bukan `jsEscape`) untuk konteks atribut, atau ganti inline `onclick` dengan `data-*` + listener. Tambah whitelist username server-side (mis. `[a-z0-9._-]`) di register & create.

---

## 🟠 HIGH

### 3. IDOR lintas-tenant: export submission tenant lain (PII siswa)
- **Lokasi:** `internal/handlers/admin/submissions.go` (`exportSingleExamCSV`/`fetchSubmissionsByExam`, sekitar :571–670); route `cmd/server/main.go:591`.
- **Masalah:** `ExportSubmissions` membaca `exam_id` dari query dan menjalankan `SELECT ... FROM submissions WHERE exam_id=$1` **tanpa** cek kepemilikan. Route hanya di belakang `AuthRequired()` (GET → tanpa CSRF, tanpa gate tenant). Endpoint saudaranya (`ListSubmissions`, `SubmissionDetail`, dll.) semua sudah men-scope — hanya path ini yang bolong.
- **Eksploitasi:** `GET /admin/api/submissions/export?exam_id=1234` dengan cookie sesi valid mengunduh CSV seluruh siswa (nama, nomor, kelas, skor, MAC/device) dari ujian tenant lain. Enumerasi `exam_id` = bocor semua tenant.
- **Fix:** Panggil `checkExamOwnership(c, pool, examFilter)` di awal `exportSingleExamCSV` (atau `ExportSubmissions` saat `examFilter>0`), balas 403/404 jika gagal.

### 4. IDOR lintas-tenant: WebSocket monitoring ujian tenant lain
- **Lokasi:** `cmd/server/main.go:340` (handler `/ws/:room_id`); broadcast di `internal/websocket/hub.go:343,371`.
- **Masalah:** Cabang sesi meng-authorize hanya dari `session.Get(SessionKeyAdminID) != nil` — tanpa cek bahwa user memiliki/delegasi/pengawas ujian `room_id`. (Cabang token sudah benar.) Setelah join, hub menyiarkan `student_update` berisi `student_name`, `exam_number`, `student_class`, `device_info`, `mac_address`.
- **Eksploitasi:** Admin tenant A login, enumerasi `exam_id` aktif tenant B via `GET /api/exams` (publik), buka `/ws/<examID-B>` → menerima PII siswa tenant B secara real-time.
- **Fix:** Di cabang sesi, parse exam ID dari `room_id` dan wajibkan cek kepemilikan (created_by/delegated_to/exam_pengawas/operator-instansi) sebelum `JoinRoom`.

### 5. Operator & pengawas bypass kepemilikan pada bulk delete/toggle
- **Lokasi:** `internal/handlers/admin/exams.go:1090` (`BulkDelete`), `:1463` (`BulkToggle`); filter `if !isSuper && !isOp`.
- **Masalah:** Untuk operator (`isOp`), blok filter kepemilikan **dilewati sepenuhnya**, sehingga `body.IDs` diteruskan mentah ke `BulkDeleteExams`/`BulkToggleExamStatus` (`... WHERE id = ANY($1)`, tanpa predikat instansi/owner). Ini bertentangan dengan `DelegateData`/`PostDelegateExam` yang menegakkan same-instansi untuk operator. Selain itu, untuk user non-privileged filter menyertakan `exam_pengawas`, sehingga **pengawas** bisa menghapus/menonaktifkan ujian yang hanya ia awasi — padahal path tunggal (`DeleteExam`→`checkExamOwnership`) menolak pengawas (403).
- **Eksploitasi:** Operator instansi A kirim `POST /admin/api/exams/bulk-delete {"ids":[<id ujian instansi B>]}` → ujian tenant lain terhapus (cascade submission/log/pengawas + file R2). `bulk-toggle` bisa menonaktifkan ujian tenant lain saat sedang berlangsung.
- **Fix:** Terapkan filter scoped-instansi juga untuk operator; hanya SuperAdmin yang boleh skip. Selaraskan klausa `exam_pengawas` dengan `checkExamOwnership` (hapus dari bulk, atau tambahkan penanganan pengawas yang konsisten).

### 6. Rate-limit submit di-key per-ujian → siswa sah kena 429
- **Lokasi:** `internal/handlers/api/exams.go:539` (`ratelimit:submit:<examID>`, `submitRateLimitMax=10`/60s, `:40-46`).
- **Masalah:** Satu bucket 10/menit **dibagi seluruh siswa** satu ujian (key hanya `exam_id`, bukan per-MAC/siswa).
- **Eksploitasi/dampak:** Kelas 30 siswa submit di akhir waktu → 10 pertama sukses, sisanya dapat 429 "Terlalu banyak percobaan submit" dan tidak bisa mengumpulkan. Bug ketersediaan pada alur inti ujian.
- **Fix:** Key rate-limit per `exam_id`+`mac_address` (atau naikkan drastis batas per-ujian dan/atau pakai token bucket per-perangkat).

### 7. Batch inserter: satu row gagal me-rollback seluruh batch (submission hilang diam-diam)
- **Lokasi:** `internal/queue/submission_queue.go:391` (UPSERT), `:426` (`storeResult(true)`), `:430` (`Commit`).
- **Masalah:** Semua job diproses dalam satu transaksi tanpa savepoint. `storeResult(...true...)` + increment counter dipanggil **sebelum** `Commit`. Bila satu row melanggar FK (mis. exam sudah dihapus — `schema.sql:89` `exam_id ... NOT NULL REFERENCES exams(id)`), transaksi abort, `defer Rollback` membuang job #1..#N yang sudah dilaporkan "sukses" dan tidak di-retry.
- **Eksploitasi/dampak:** Siswa menerima konfirmasi "Jawaban berhasil dikirim" padahal jawaban tidak tersimpan (data loss senyap).
- **Fix:** Insert per-row dengan savepoint (atau transaksi per-job), dan panggil `storeResult(true)` hanya setelah commit sukses; retry row yang gagal.

### 8. `GetPricingMap` salah key setting → harga paket diabaikan, billing Rp 0
- **Lokasi:** `internal/models/settings.go:199-200`; simpan di `:267` pakai key `price_guru_bulanan`, tapi lookup pakai key tanpa prefiks (`guru_bulanan`).
- **Masalah:** Loop `for k := range prices` membaca `settings[k]` dengan key tak-berprefiks, sedangkan handler simpan menyimpan key berprefiks `price_*`. Key tak pernah cocok → nilai default (0) dikembalikan.
- **Eksploitasi/dampak:** SuperAdmin ubah harga → halaman `/admin/billing` menampilkan Rp 0 untuk semua paket; `CalculatePackagePrice` juga salah → checkout dengan nominal salah.
- **Fix:** Samakan key: lookup pakai konstanta `SettingPrice*` (berprefiks) yang sama dengan path simpan.

### 9. Eskalasi hak: operator promosi user jadi Operator via field `role` (singular)
- **Lokasi:** `internal/handlers/admin/users.go:438` (guard hanya cek `body.Roles`) & `:532-547` (apply fallback ke `body.Role`).
- **Masalah:** Guard operator hanya me-loop `body.Roles`. Jika operator kirim `{"role":"operator"}` (singular, tanpa `roles`), guard terlewati; di apply, `roles = strings.Split(*body.Role, ",")` dan switch menerima `RoleOperator` → target di-set operator.
- **Eksploitasi:** Operator meng-edit guru di instansinya dengan `{"role":"operator"}` → guru itu jadi Operator (hak kelola user, dst.) tanpa restu SuperAdmin.
- **Fix:** Terapkan restriksi yang sama pada `body.Role` (bukan hanya `body.Roles`); tolak `RoleOperator` untuk caller operator di kedua jalur.

### 10. WebSocket: balasan ping/heartbeat bisa `send` ke channel tertutup → server panic
- **Lokasi:** `internal/websocket/hub.go:291` (`client.send <- payload` di `handleClientMessage`), `Close()` `:168-175`, `removeClient` `:248-260`.
- **Masalah:** `handleClientMessage` berjalan di goroutine `readPump` per-klien dan mengirim langsung ke `client.send` (bukan lewat hub). Bila buffer klien penuh, `broadcastToRoom` menjadwalkan unregister → `removeClient` → `Close()` menutup `send`. Kirim ke channel tertutup **panic**, dan goroutine pump tidak dilindungi `gin.Recovery` → proses crash.
- **Fix:** Jangan `close(send)` dari luar; pakai sinyal quit + `select` pada channel done, atau guard dengan mutex/`recover`; sinkronkan semua akses `send` lewat satu goroutine.

---

## 🟡 MEDIUM

### 11. Entitlement menimpa kolom `role` → SuperAdmin/multi-role turun jadi `["operator"]`
- **Lokasi:** `internal/handlers/admin/transactions.go:86-90` (`applyApprovedTransactionEntitlement`, `UPDATE ... role=$7`); voucher `internal/handlers/admin/vouchers.go:464-467` juga menimpa role sesi.
- **Masalah:** Role di-set tanpa mempertimbangkan role lama. Jika SuperAdmin (atau user multi-role) redeem voucher/beli paket, `role` ditimpa jadi `["operator"]` → kehilangan akses `SuperAdminRequired`.
- **Fix:** Merge role (jangan timpa); jangan pernah menurunkan SuperAdmin; pisahkan "package entitlement" dari "role".

### 12. Webhook DOKU REFUND/VOID/REVERSAL tak pernah tereksekusi → user refund tetap punya paket
- **Lokasi:** `internal/handlers/api/doku_webhook.go:178`.
- **Masalah:** `if tx.Status != TxStatusPending { return 200 "already processed" }` mendahului switch status. Untuk transaksi yang sudah `approved`, notifikasi REFUND/VOID/REVERSAL langsung di-short-circuit → `ProcessTransactionReversal` (yang menurunkan paket) tak pernah jalan.
- **Fix:** Untuk SUCCESS pertahankan guard pending (idempoten); untuk REFUND/VOID/REVERSAL izinkan proses saat status `approved`.

### 13. `SaveQuestions` menghapus seluruh daftar pengawas saat guru menyimpan
- **Lokasi:** `internal/handlers/admin/exams.go:816-818` (else branch) + `internal/models/exam_pengawas.go` (`SetPengawasForExam` = DELETE-all lalu insert).
- **Masalah:** Untuk caller non-operator/non-super, cabang else memanggil `SetPengawasForExam(examID, []int{userID})` yang menghapus semua row lalu hanya memasukkan caller.
- **Eksploitasi/dampak:** Operator set pengawas [B,C]; guru pemilik menyimpan soal (UI guru tak kirim `pengawas_ids`) → B & C terhapus dari pengawasan.
- **Fix:** Jangan reset daftar pengawas untuk caller non-privileged; sisakan yang ada, atau `INSERT ... ON CONFLICT DO NOTHING` untuk pemilik saja.

### 14. `GET /api/exams` publik membocorkan ujian aktif semua tenant
- **Lokasi:** `internal/handlers/api/exams.go:228` + query `internal/models/exam.go:281,293`; route `cmd/server/main.go:472` (tanpa auth).
- **Masalah:** Mengembalikan id/nama/status/waktu semua ujian aktif lintas sekolah tanpa autentikasi.
- **Fix:** Butuh token/scoping; minimal jangan kembalikan nama/metadata lintas tenant tanpa filter.

### 15. `ListUsers` tidak SELECT kolom `package` → package selalu ""
- **Lokasi:** `internal/models/user.go:479-511` (SELECT & Scan tanpa `package`); dipakai `internal/handlers/admin/users.go:163`.
- **Masalah/dampak:** API manajemen user selalu mengembalikan `"package":""`; admin tak bisa lihat paket user, badge/filter kosong.
- **Fix:** Tambahkan `u.package` ke SELECT & scan ke `&u.Package`.

### 16. `schema.sql` menulis-ulang tabel `transactions` tiap startup (ALTER COLUMN TYPE ... USING)
- **Lokasi:** `internal/database/schema.sql:220`; dieksekusi tiap boot di `internal/database/database.go:60,70`.
- **Masalah:** `USING amount::numeric::bigint` memicu table rewrite di bawah `ACCESS EXCLUSIVE` tiap restart walau kolom sudah `BIGINT`; pada volume besar mem-block webhook & billing, dan dua replika serial di lock.
- **Fix:** Jadikan migrasi idempoten/kondisional (skip bila tipe sudah benar), atau pindah ke tool migrasi terpisah.

### 17. Expiry edit-user disimpan tanpa konversi UTC → drift zona waktu
- **Lokasi:** `static/js/admin.js:1773` (kirim string lokal naif) → `internal/handlers/admin/users.go:553-561` (simpan verbatim ke `TIMESTAMPTZ`).
- **Masalah:** `submitEditUser` mengirim `"2026-06-01 23:59:00"` tanpa offset; disimpan/dibaca sebagai UTC → di GMT+7 masa aktif meleset ~7 jam, dan makin geser tiap kali modal disimpan ulang. (Bandingkan `createUser` di admin.js:2913 yang pakai `toISOString`.)
- **Fix:** Konsisten kirim/simpan UTC (ISO 8601) di jalur edit, seperti pada create.

---

## 🟢 LOW

### 18. Map lockout login in-memory tak terbatas (tanpa Redis) → DoS memori
- **Lokasi:** `cmd/server/main.go:727-728` (map global), hapus hanya saat login sukses `:831-832`.
- **Masalah:** Tanpa Redis, tiap username gagal unik menambah entri permanen (`failedLogins`/`lockoutTimes`) yang tak pernah dibersihkan.
- **Fix:** Batasi ukuran/LRU + sweep berkala, atau utamakan jalur Redis (TTL).

### 19. Submit ganda → baris skor duplikat (tanpa unique constraint)
- **Lokasi:** `internal/queue/submission_queue.go:391-412` & `internal/models/submission.go:407-433`; `schema.sql` tanpa `UNIQUE(exam_id, mac_address)`.
- **Masalah:** Pola "update row kosong, else insert" + tanpa unique + select-then-insert race → satu siswa bisa punya 2 baris ter-skor.
- **Fix:** Tambah unique index parsial/kondisional (exam_id, mac_address) dan UPSERT `ON CONFLICT`; atau dedup per-perangkat.

### 20. `GetSubmissionStats` error di ujian kosong (SUM→NULL ke int) + over-count duplikat
- **Lokasi:** `internal/models/submission.go:616-619`.
- **Masalah:** `SUM(...)` di-scan ke `int` → error scan NULL saat 0 baris (ditelan oleh err-swallow); `COUNT(*)` menghitung duplikat yang di-list justru di-`DISTINCT ON mac_address`, sehingga kartu statistik tak konsisten.
- **Fix:** `COALESCE(SUM(...),0)` + tipe nullable/`int64`; samakan dedup antara stats dan list.

### 21. `ToggleUserStatus`: operator bisa suspend operator lain + cascade-suspend seluruh instansi
- **Lokasi:** `internal/handlers/admin/users.go:624-630` (hanya cek instansi, tanpa guard `IsOperator()` seperti `EditUser:433`) + cascade `:658-673`.
- **Masalah:** Operator A suspend operator B seinstansi → cascade menonaktifkan semua user aktif instansi tsb.
- **Fix:** Tambahkan guard `targetUser.IsOperator()` (larang operator mengelola sesama operator), konsisten dengan `EditUser`.

### 22. HMAC webhook dibandingkan non-constant-time **dan** signature terhitung di-log
- **Lokasi:** `internal/handlers/api/doku_webhook.go:134` (`!=`) & `:135` (`log.Printf(... Computed: %s ...)`).
- **Masalah:** Perbandingan `!=` bocor timing (kecil), tapi yang lebih nyata: signature yang dihitung server dicetak ke log. Penyerang dengan akses log bisa membaca "Computed" lalu resubmit request identik untuk memalsukan callback.
- **Fix:** `hmac.Equal([]byte(a),[]byte(b))`; jangan pernah me-log signature terhitung.

### 23. `ListTransactions`: tak cek `rows.Err()` + `per_page` tak dibatasi
- **Lokasi:** `internal/models/transaction.go:159,168-188`; handler `internal/handlers/admin/transactions.go:117`.
- **Masalah:** `per_page` dari query tak di-clamp atas → `LIMIT 5000000` streaming semua baris (resource exhaustion); loop tak memanggil `rows.Err()` (error iterasi tersembunyi).
- **Fix:** Clamp `PerPage` (mis. ≤ 200) dan cek `rows.Err()`.

---

## ⚪ Plausible (bergantung konfigurasi/deploy — perlu ditinjau)

- **`access-log` tanpa cek token** — `internal/handlers/api/exams.go:921`: siapa pun bisa menyuntik login/logout/heartbeat untuk `exam_id` aktif (memalsukan kehadiran siswa). Tambahkan verifikasi token seperti `SubmitExam`/`CompleteExam`.
- **CORS credentialed-wildcard** — `internal/middleware/cors.go:38`: hanya berbahaya bila `EXAMVAN_CORS_ORIGINS=*`; dimitigasi cookie `SameSite=Lax` + default origin kosong. Jangan gabungkan `*` dengan `Allow-Credentials:true`.
- **`checkExamOwnership` instansi kosong** — `internal/handlers/admin/helpers.go:251`: `'' == ''` bocor, tapi register/create memaksa instansi `personal`. Fail-closed bila instansi kosong + bandingkan `instansi_id`.
- **`time.LoadLocation` nil-panic** — `internal/handlers/admin/exams.go:634`: hanya bila image tanpa tzdata (Dockerfile saat ini pasang tzdata). Fallback ke `time.UTC` bila error.
- **Idempotency DOKU: marker sebelum commit** — `doku_webhook.go:197`: retry dengan Request-Id sama dalam 10 menit bisa "menelan" approval yang gagal. Tandai setelah commit sukses.
- **Reversal tak claw-back waktu/role** — `transactions.go:399`: relevan bila gate #12 diperbaiki.
- **`transactions.notes` NULL-scan** — `internal/models/transaction.go:77`: `notes` nullable di-scan ke `string`; belum ada jalur Go yang menghasilkan NULL, tapi rapuh untuk data legacy. Pakai `*string`/`COALESCE`.

## ✅ PERBAIKAN TAMBAHAN — ALUR UJIAN (14 Agustus 2026)

Review kelima temuan di alur ujian siswa (submit → approval → PDF → hasil). **Semua sudah diperbaiki + ditest** TDD: 9 test baru ditulis sebagai bukti kegagalan (RED) lebih dulu, lalu fix, lalu seluruh suite hijau (`go build`, `go vet`, `go test ./...` lolos; status 14 Agustus 2026). Catatan lengkap di [README.md → Perbaikan Alur Ujian (14 Agustus 2026)](../README.md#perbaikan-alur-ujian-14-agustus-2026).

### A. Retry submit menghasilkan baris duplikat (Tinggi) — dedup ikut status submit
- **Masalah lama:** `upsertSubmissionRow` (`internal/queue/submission_queue.go`) dan `CreateSubmission` (`internal/models/submission.go`) hanya menargetkan baris "latest" bila baris itu **belum disubmit** (`answers_json IS NULL OR ''`). Saat siswa menekan submit ulang (mis. koneksi putus lalu retry async), baris yang sudah berisi jawaban dianggap "bukan milik perangkat" → dibuat **baris baru duplikat** dengan skor sama tapi identitas terbelah.
- **Fix:** filter "belum disubmit" dihapus dari lookup — retry kini memperbarui baris latest per `exam_id + mac_address (+ exam_number)`, apa pun statusnya. Dedup tetap mempertahankan pemisahan dua siswa berbagi perangkat.
- **Test:** `TestUpsertSubmissionRowRetryIsIdempotent` (queue), `TestCreateSubmissionRetryIsIdempotent` (models), `TestSubmitExamRetryDoesNotDuplicateRow` (HTTP).

### B. Pencabutan approval prematur di jalur async (Tinggi) — revoke pindah ke worker
- **Masalah lama:** `SubmitExam` menghapus baris `exam_approvals` **saat enqueue**. Bila job gagal permanen (worker mati/DB outage), siswa yang sudah dianggap "sukses" kehilangan baris approval → tidak bisa masuk ulang untuk memperbaiki, sementara jawaban lokalnya sudah dibersihkan klien → **kehilangan jawaban permanen**.
- **Fix:** `DELETE FROM exam_approvals` di hapus dari handler; `flushBatch` (`internal/queue/submission_queue.go`) kini mencabut approval **hanya setelah `tx.Commit` batch sukses** per baris yang berhasil. Baris gagal → re-enqueue + approval dipertahankan. Path sinkron tetap mencabut langsung setelah insert (data sudah durable). Response async dinaikkan ke HTTP **202** (`status:"queued"`) agar klien tahu jawaban belum durable.
- **Test:** `TestFlushBatchRevokesApprovalAfterCommit`, `TestFlushBatchKeepsApprovalWhenRowFails`, `TestSubmitExamAsyncKeepsApprovalUntilPersisted`.

### C. `GET /api/exams/:exam_id/result` publik (Tinggi) — kini kredensial-gated
- **Masalah lama:** endpoint hasil (dibuat 9 Agustus) bisa dipanggil tanpa kredensial apa pun — siapa pun yang menebak `mac_address` + `identity_data` membocorkan skor siswa (dan tautan identitas) sebelum guru mengumumkan hasil.
- **Fix:** gate akses diterima bila (1) `X-Exam-Token` valid untuk ujian tsb (header atau `token` query), atau (2) perangkat masih punya baris `exam_approvals` `approved` (menutup polling antara submit dan revoke worker di mode token dinamis). Tanpa kredensial → **401**; ujian tak ditemukan → 404. Row submission historis **tidak** lagi dianggap kredensial (itu celah akses pertama).
- **Test:** `TestExamResultRequiresTokenOrApproval`; test lama `TestExamResultReturnsScoreFromDB`/`TestExamResultPending` diperbarui mengirim token.

### D. PDF bisa diunduh tanpa persetujuan pengawas (Tinggi) — gate approval server-side
- **Masalah lama:** `GET /api/exams/:exam_id/pdf` hanya cek token aktif. Di mode statis token dibagi sekelas, dan layar menunggu (WaitingApprovalActivity/Dialog) hanya **klien-side** — klien buatan (curl + URL) mengambil PDF tanpa pernah disetujui.
- **Fix:** gate server-side di `ExamPDF` (`internal/handlers/api/exams.go`): perangkat harus punya baris `exam_approvals` `approved` untuk ujian tsb. Identitas device lewat header **`X-Device-Id`** (Android `<DEVICE:AndroidId>`, desktop MAC — nilai yang SAMA dipakai saat request-approval), fallback `mac_address` query untuk backend lama. Diberlakukan di mode manual **dan** auto-approve (klien resmi selalu lewat request-approval, jadi baris selalu ada). Android `ExamListActivity` tidak lagi membuka viewer langsung — melewati `WaitingApprovalActivity` (menutup bypass). Deskstop sudah selalu lewat dialog persetujuan.
- **Test:** `TestExamPDFRequiresApprovedDevice`, `TestExamPDFRequiresApprovedDeviceEvenWithAutoApprove`; test R2 lama (`exam_pdf_guard_test.go`) disesuaikan dengan device yang disetujui.

### E. Rate-limit submit key `unknown` global (Sedang) — bucket per-IP saat MAC tak dikenal
- **Masalah lama:** perangkat tanpa MAC ter-resolve (`sanitizeMAC` → `"unknown"`; install baru/emulator) semua memakai **satu bucket rate-limit global**, sehingga sekelas perangkat yang MAC-nya hilang saling memblokir setelah total cap tercapai.
- **Fix:** saat `mac_address` kosong/`"unknown"`, key rate-limit menjadi `exam:<id>:ip:<ClientIP>` — tiap perangkat mendapat bucket sendiri; perangkat dengan MAC tetap pakai bucket per-MAC.
- **Test:** `TestSubmitExamRateLimitKeyedByIPWhenMacUnknown` — IP A habiskan 10 izin → ke-11 = 429; IP B dengan MAC `unknown` sama tidak diblokir.

### F. Perubahan terkait di klien
- **Android** (`ApiClient.kt`, `PdfRendererHelper.kt`, `SubmissionManager.kt`, `ExamListActivity.kt`): `downloadPdf` kirim `X-Device-Id`; `SubmitResult.status`/`jobId` + endpoint `getExamResult`; setelah submit 202 `queued`, jawaban lokal **tidak dibersihkan** sampai `/result` melaporkan `done` (polling ~75 dtk), termasuk jalur auto-submit background; daftar ujian dibuka lewat `WaitingApprovalActivity`.
- **Desktop** (`examvan/api.py`, `ui/exam_viewer.py`): `download_pdf` kirim `X-Device-Id` = MAC (sama dengan saat persetujuan).

---

## ✅ KEBIJAKAN SATU OPERATOR PER SEKOLAH (15 Agustus 2026)

Audit alur redeem/aktivasi voucher operator: apakah satu sekolah bisa punya banyak operator dan apa dampaknya ke kuota. **Keputusan kebijakan: satu sekolah = satu operator**, kini di-enforce di seluruh jalur pemberian role Operator (`go build`, `go vet`, `go test ./...` seluruh repo lolos dengan `TEST_DATABASE_URL`). Catatan lengkap di [README.md → Kebijakan Satu Operator per Sekolah](../README.md#kebijakan-satu-operator-per-sekolah).

### A. Temuan audit (sebelum kebijakan)
- **Satu sekolah bisa punya banyak operator** lewat 3 jalur yang tidak dijaga: (1) guru kedua menukar kode voucher sekolah yang sama (cek `existingCount` hanya per `(voucher_id, user_id)`), (2) SuperAdmin membuat operator kedua via CreateUser/EditUser, (3) guru ber-paket sekolah tanpa role menukar voucher sekolah.
- **Dampak kuota:** (a) school pool memakai `MAX()` atas redemption aktif operator se-instansi → satu operator ber-paket besar menaikkan pool seluruh sekolah (by design, "never-surprising fallback"); (b) kuota `max_users` school-wide ikut terbagi — operator lain menghabiskan slot sub-akun; (c) 🔴 **BUG nyata: cascade suspend tidak sadar multi-operator** — `syncInstansiWithOperatorRole` (dipicu tiap redeem/aktivasi/fallback expiry saat role Operator hilang) menyuspend **seluruh** akun non-operator di instansi, sehingga salah satu dari dua operator yang turun paket membekukan seluruh sub-akun sekolah padahal operator lain masih menjalankan paket aktif. (Bug cascade ini sudah diperbaiki: cascade dilewati selama masih ada operator riil lain — `NOT operator_created` + role Operator + redemption aktif — yang menutupi sekolah.)

### B. Kebijakan dan guard
- Helper bersama `schoolAlreadyHasOperator` (`internal/handlers/admin/entitlement.go`): sebuah instansi sekolah nyata dianggap "sudah punya operator" bila ada akun **bukan** hasil buatan operator (`operator_created = false`) ber-role Operator di instansi tersebut (aktor yang bertindak dikecualikan agar renew operator sendiri tetap bisa; bucket `"personal"` bukan sekolah; akun sub legacy ber-role Operator sendiri **tidak** memblokir).
- Kelima jalur memanggil helper yang sama dan menolak **400** "Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.": `RedeemVoucherHandler` (di dalam transaksi, di bawah row lock yang sama dengan snapshot), `ActivateVoucherHandler`, `CreateUser` oleh SuperAdmin, `EditUser`, dan `UpdateInstansi` (klaim sekolah dari bucket personal).
- Test: `TestOneOperatorPerSchoolPolicy` (`operator_user_flow_test.go`, 9 sub-tes) mengunci kelima jalur ditolak + kasus yang tetap lolos (renew operator sendiri, sekolah tanpa operator, bucket personal, akun sub ber-role operator tidak memblokir). Test legacy yang membuat operator kedua lewat API diubah menjadi menanam state langsung ke DB (state pra-kebijakan).

---

## ✅ HARDENING WEBSOCKET HUB — ISOLASI TENANT, GATE PRIVILEGE, & SANITASI PAYLOAD (15 Agustus 2026)

Review `internal/websocket/hub.go` + route `/ws/:room_id` (`cmd/server/main.go`) untuk race check-then-act dan kebocoran data antar-tenant di jalur real-time. **Verdict race: bersih** — satu goroutine `Run` menyerialkan register/unregister/broadcast (tidak ada send-on-closed-channel), `trySend` mutex-guarded, buffer penuh → drop bukan blok, join di-gate `UserCanAccessExam`/token, room per-exam anti-spoof. Tiga celah di jalur join berbasis token ditutup + test hub dibuat dari nol. Catatan lengkap: [README.md → Hardening WebSocket Hub](../README.md#hardening-websocket-hub--isolasi-tenant-gate-privilege--sanitasi-payload-15-agustus-2026).

### A. Pemegang token kini receive-only (F1a)
- **Masalah:** token statis dibagi sekelas → semua siswa bisa join room dan mengirim `heartbeat` palsu (phantom student + injeksi siaran `student_update`) atau `exam_completed` dengan MAC perangkat lain (hapus presence → siswa tampak offline di dashboard monitoring, `is_online` dibaca dari Redis heartbeat).
- **Fix:** flag `privileged` di `Client`; route `/ws/:room_id` menandai sesi admin (lolos `UserCanAccessExam`) sebagai privileged, pemegang token sebagai non-privileged. `heartbeat`/`exam_completed` dari klien non-privileged di-ignore (log + return).
- **Test:** `TestPrivilegedGateHeartbeat`, `TestPrivilegedGateExamCompleted`, `TestJoinRoomPrivilegedPlumbing` (end-to-end socket asli + miniredis).

### B. Sanitasi payload (F1b)
- **Masalah:** payload heartbeat di-echo mentah ke Redis + room (semua pemegang token), termasuk `device_info` berisi markup untuk dashboard legacy.
- **Fix:** helper `sanitizeWSField`/`sanitizeWSMac`/`wsString`; snapshot tersanitasi dipersist & disiarkan; `device_info` sengaja tidak disiarkan.
- **Test:** `TestHeartbeatSanitization`.

### C. Rate limit route ws (F2)
- `/ws/:room_id` kini `middleware.RateLimitIP(20, time.Minute)` — pemegang token tidak bisa membuka koneksi tanpa batas.

### D. Test hub (F3)
- `hub_test.go` (baru): isolasi room (batas tenant), gate privilege heartbeat & exam_completed, sanitasi, ping/pong, plumbing `JoinRoom`. Verifikasi mutation: gate dilepas & sanitasi dilewati → test gagal; dipulihkan → 5/5 stabil; `go build ./...` + `go vet` bersih.

---

## ✅ AUDIT IDEMPOTENSI RESUBMIT RECOVERY — SERVER (16 Agustus 2026)

Audit menyusul fitur desktop/Android auto-submit-and-exit: submit auto berjalan di background setelah window ditutup segera, dan bila gagal, re-entry menawarkan "Kirim Lagi" (resubmit dari jawaban yang tetap di disk). Pertanyaan audit: **apakah server benar-benar idempoten untuk resubmit recovery** (tidak menduplikasi baris)? Verdict: **sudah idempoten untuk kasus sequential, satu celah race cross-path ditutup, dan satu gate jadwal dilonggarkan khusus recovery.** Catatan lengkap: [README.md → Idempotensi Resubmit Recovery (16 Agustus 2026)](../README.md#idempotensi-resubmit-recovery-server-16-agustus-2026).

### A. Sudah idempoten (diverifikasi, ada test)
- **Sync path** (`models.CreateSubmission`): ambil advisory lock `approval:<exam>:<mac>`, lalu UPDATE baris latest (match exam+device+exam_number, tanpa filter answers) — retry meng-update baris yang sama, bukan INSERT baru. `TestSubmitExamRetryDoesNotDuplicateRow` + `TestSubmitSyncConcurrentSameDeviceNoDuplicates`.
- **Async path** (`queue.upsertSubmissionRow`): pola sama — UPDATE latest row, INSERT hanya bila tidak ada. `TestUpsertSubmissionRowRetryIsIdempotent`, `TestUpsertSubmissionRowTwoStudentsShareDevice` (dua siswa berbagi device tetap dua baris).
- **Approval bookkeeping** (`EnsureFreshSubmissionOnApproval`): idempoten, dipanggil tiap poll approval.

### B. Celah race cross-path DITUTUP (baru, 16 Agustus 2026)
- **Masalah:** `upsertSubmissionRow` (jalur async queue) TIDAK mengambil advisory lock yang sama dengan `CreateSubmission` (sync) dan `EnsureFreshSubmissionOnApproval`. Job async (submit asli yang gagal terkonfirmasi) dan resubmit sync (recovery re-entry saat Redis enqueue gagal → fallback sync) bisa **berjalan bersamaan**: keduanya melihat "tidak ada row" → keduanya INSERT → siswa muncul 2× di monitoring table / hasil page dengan skor duplikat.
- **Fix:** `upsertSubmissionRow` kini mengambil `pg_advisory_xact_lock(hashtext('approval:<exam>:<mac>'))` yang sama sebelum UPDATE-then-INSERT — serialisasi lintas jalur sync/async/approval bookkeeping (dan lintas instance worker).
- **Test:** `TestUpsertSubmissionRowConcurrentSameDeviceNoDuplicates` (12 goroutine upsert bersamaan → tepat 1 baris).

### C. Gate jadwal dilonggarkan khusus RECOVERY (baru, 16 Agustus 2026)
- **Masalah:** `SubmitExam` menolak SEMUA submit setelah `end_time + 60s` (grace). Recovery "Kirim Lagi" untuk skenario yang justru dibuat fitur ini (jaringan mati di deadline, siswa re-entry menit/jam kemudian) ditolak 403 → jawaban yang sudah dikerjakan hilang permanen.
- **Fix:** lewat deadline, submit tetap diterima bila device **masih punya approval row `approved`** (approval hanya dicabut setelah submit DURABLE — sync path / worker pasca-commit, jadi approval tersisa = submit sebelumnya gagal). Device tanpa approval (belum pernah di-approve / sudah selesai) tetap 403 — cutoff keras untuk kerja lewat deadline dipertahankan, dan resubmit tetap idempoten (satu baris).
- **Test:** `TestSubmitExamRecoveryAfterDeadlineAllowed` (approved + lewat deadline → 200, 1 baris), `TestSubmitExamAfterDeadlineStillRejectsUnapproved` (tanpa approval → 403, 0 baris), `TestSubmitExamWithinGraceStillAllowed` (grace 60s tetap berlaku untuk semua).

---

## ✅ AUDIT PERILAKU FEATURE-LOCKED DI HUB PENGATURAN (17 Agustus 2026)

Verifikasi end-to-end akun feature-locked (`expires_at` di masa lalu) terhadap halaman settings 5-tab baru (desktop + API, session asli via browser). **Hasil: tidak ada celah — perilaku sudah benar; tidak ada perubahan kode yang diperlukan.**

### A. Halaman settings
- **Hanya tab Paket & Voucher yang dirender** — `section-general`, `section-users`, `section-vouchers`, `section-system-apps` **tidak ada di DOM** (0 blok accordion, 0 tombol Buka Semua/Lipat Semua, 0 form SaaS); tab bar & dropdown mobile hanya berisi opsi `billing`.
- **Semua hash lama fallback ke billing** — `#users`, `#vouchers`, `#general`, `#packages`, `#voucher-audit`, `#system-apps` → tab aktif selalu `billing` (5/5 diverifikasi).
- Banner kuning **"Masa aktif akun Anda telah berakhir…"** tampil (`{{if .user_expired}}` di `settings.html`); 0 error JS.

### B. API (header `Accept: application/json` + `X-Requested-With`)
- **Billing-exempt tetap terbuka** (by design): `GET /admin/api/vouchers/mine` → 200; `POST /admin/api/vouchers/redeem` → 400 "Kode voucher tidak valid" saat `voucher_redeem_enabled=1` (lolos gating — bukan 403 feature-lock).
- **Terkunci**: `stats`, `users`, `saas-settings` → 403 JSON (bukan redirect, karena header API).

### C. Catatan
- Saat `voucher_redeem_enabled=0` (fitur klaim dimatikan) redeem memberi 403 — itu kebijakan fitur, bukan celah gating (diverifikasi dengan toggle `0→1→0`).
- Referensi perilaku lengkap: [README.md → Konfigurasi Masa Aktif Default (Trial)](../README.md#konfigurasi-masa-aktif-default-trial).

---

## ✅ REVIEW MIDDLEWARE, WEBSOCKET HUB & KONEKSI REDIS (12 September 2026)

Audit `internal/middleware/` (ratelimit, template, timeout, version, db, csrf), `internal/websocket/hub.go`, dan `internal/redis/client.go`. Semua temuan LOW — setiap kutipan diverifikasi dengan membaca kode di lokasinya. Tidak ada perubahan kode (review-only).

### A. Rate limiter Redis gagal-buka (fail-open) saat Redis error — LOW
- **Lokasi:** `internal/middleware/ratelimit.go:228-232`
- **Masalah:** saat `pipe.Exec()` error (Redis down/lambat), middleware hanya log `"ratelimit: redis exec error"` lalu `c.Next()` — **semua** rate limit (login 10/menit, register, dll.) lenyap selama outage. Trade-off availability yang tampak disengaja, tapi layak didokumentasikan: pihak yang bisa membuat Redis error otomatis menonaktifkan seluruh pembatasan.
- **Fix:** tambahkan counter/alert saat jalur fail-open terpicu; pertimbangkan fail-closed khusus endpoint auth publik, atau fallback ke limiter memori in-process (`memStore` sudah ada) untuk beberapa menit pertama outage.

### B. `TemplateData()` menjalankan 6 query `saas_settings` berurutan per render halaman — LOW (perf)
- **Lokasi:** `internal/middleware/template.go:53-71` (helper yang dipanggil manual — BUKAN middleware terdaftar); 11 call site: `cmd/server/main.go:827,863,1061,1108,1167,1365,1420`, `cmd/server/auth_recovery.go:160,170,255,273`
- **Masalah:** setiap panggilan menjalankan 6 `SELECT value FROM saas_settings WHERE key=$1` **berurutan** (seo_title, seo_description, seo_keywords, seo_index, footer_text, footer_tagline) = 6 round-trip DB per halaman yang dirender, untuk nilai SEO/footer yang hampir tak pernah berubah.
- **Fix:** gabungkan menjadi 1 query `WHERE key IN (...)` + cache in-process dengan TTL pendek (1-5 menit), atau preload saat startup dan invalidasi saat admin menyimpan settings.

### C. Evictor limiter memori acak padahal komentar menulis "oldest" — LOW
- **Lokasi:** `internal/middleware/ratelimit.go:124,153`
- **Masalah:** dua komentar menulis "evict oldest entries" / "Evict oldest random entry", tapi implementasinya `for k := range memStore { delete(...); ... }` — iterasi map di Go urutannya acak, jadi yang ter-evict bisa counter milik IP yang sedang aktif (rate limit korban ter-reset); komentar juga menyesatkan maintainer berikutnya.
- **Fix:** simpan `lastSeen` timestamp per entry dan evict berdasarkan usia sungguhan; minimal koreksi komentar bila perilaku acak memang disengaja.

### D. Redis ops di hub WebSocket tanpa timeout eksplisit — LOW
- **Lokasi:** `internal/websocket/hub.go:423-424,430,475`
- **Masalah:** `Set`/`LPush` heartbeat dan `Del` exam_completed berjalan dengan `context.Background()` tanpa deadline. Dampak terbatas: `internal/redis/client.go` tidak mengatur timeout kustom sehingga default go-redis v9 (~3s/op) tetap membatasi, dan blok hanya menahan goroutine `readPump` client privileged itu sendiri — loop `Hub.Run()` tak pernah tersumbat.
- **Fix:** bungkus dengan `context.WithTimeout(context.Background(), 2*time.Second)` agar masalah Redis tak pernah menahan readPump lebih lama dari perlu.

---

## ✅ REVIEW UI: TEMPLATES, JS ADMIN/PUBLIK & COVERAGE CSRF END-TO-END (13 September 2026)

Sweep XSS client-side + verifikasi CSRF end-to-end di `templates/admin`, `templates/public`, `static/js`, `static/css`. **Tidak ada temuan baru** — semua sink `innerHTML` lolos verifikasi dan coverage CSRF 100%. Review-only, tanpa perubahan kode.

### A. Sweep XSS client-side — SEMUA sink terverifikasi aman (temuan: nihil)
- **Lokasi:** seluruh `static/js/` + template Go
- **Yang dicari:** data JSON dari server diinterpolasi ke `innerHTML` tanpa escaping → stored XSS via nama user/label paket/kode voucher/detil audit.
- **Hasil verifikasi (semua aman):**
  - `admin.js` — 9 sink dinamis, semua lewat `escapeHtml()`: student-access (598), createNewQuestionCard (641), createDivider (712), addIdentityFieldRow (985), user-row builder (1740-1830), modal Ubah Instansi (2279), createEditUserModal (2460, statis; input dirender via `textContent` 2174), detil jawaban ujian (4110-4135), popup identitas (4430-4455).
  - `settings-vouchers.js:130` — `safeCode = escapeHtml(v.code)` untuk semua kolom kode voucher.
  - `settings-voucher-audit.js` — `escapeHtml(msg)` di error (8), `escapeHtml(q)` di empty-state (52), `escapeHtml(l.username/l.detail/l.action)` di tabel (67,72-73), `encodeURIComponent(search)` di query (26).
  - `settings-billing.js` — `escapeHtml(packageDisplayName(p.package))` + `escapeHtml(p.code || '—')` (151-152); `PACKAGE_DISPLAY` dict dengan fallback yang ikut di-escape; angka (`max_exams`, `remaining_seconds`) lewat `fmtMB()`/`fmtRemaining()` yang selalu numerik.
  - `settings-packages.js:33-34` — `escapeHtml(p.label)` + `escapeHtml(p.key)`; field lain numerik murni.
  - `admin-core.js:389-434` — `escapeHtml`/`jsEscape`/`formatDateTimeID` terdefinisi & dipakai konsisten.
  - `settings-users.js`, `pengawas-detail.js`, `device-fingerprint.js` — NOL sink `innerHTML`.
  - Template Go: auto-escape `html/template` di semua halaman; tidak ada `template.JS`/`template.HTML`/`|safe`/inline event handler.
  - Tidak ada `document.write` / `eval` / `new Function` / jQuery `.html()` di seluruh `static/js/`.

### B. Coverage CSRF 100% — tidak ada route mutasi yang tercecer (temuan: nihil)
- **Lokasi:** `cmd/server/main.go`, `internal/middleware/csrf.go`, `static/js/admin-core.js`
- **Yang dicari:** POST/PUT/DELETE tanpa `CSRFRequired()`, atau form/JS yang tidak mengirim token.
- **Hasil verifikasi (semua tercakup):**
  - 8 POST publik semuanya per-route `CSRFRequired()` — login (469), logout (472), admin/login (478), register (481), register/confirm (483), register/resend (484), forgot-password (488), reset-password (490). Grep mutasi di luar grup terkenal tidak menemukan route lain.
  - `billingAPI := adminAPI.Group("", middleware.CSRFRequired())` (686) → klaim/aktivasi voucher terlindungi di level grup.
  - `csrfAPI := lockedAPI.Group("", middleware.CSRFRequired())` (701) → SEMUA POST admin lain terlindungi di level grup; `adminUsers` (724), `adminSettings` (742), `adminVouchers` (759) bersarang di dalamnya dan mewarisi CSRF.
  - 6 form POST di template membawa hidden field: 5× `csrf_token` (admin/login.html:70, public/register.html:235, register_confirm.html:221, forgot_password.html:58, reset_password.html:67) + 1× alias legacy `_csrf_token` (admin/partials/nav.html:83) — keduanya diterima middleware (csrf.go menerima alias `_csrf_token`).
  - JS: `apiFetch()` (admin-core.js:78) otomatis melampirkan header `X-CSRF-Token` untuk semua POST/PUT/DELETE/PATCH; `getCsrfToken()` (admin-core.js:4-7) membaca `meta[name="csrf-token"]`; tidak ada `fetch` POST mentah yang melewati `apiFetch`.
  - Logika middleware solid: token 32-byte `crypto/rand` hex, scoped per-session, hanya menegakkan non-GET/HEAD/OPTIONS/TRACE, dual-channel header+form.

---

## ✅ REVIEW ALUR PUBLIK: REGISTER, OTP VERIFIKASI, RESET PASSWORD & DOWNLOAD (13 September 2026)

Review alur register → verifikasi OTP → login, lupa/reset password, dan endpoint download publik. Fokus: enumerasi akun/email, diferensial pesan error, penyimpanan OTP at-rest, sanitasi kunci jawaban, rate limit download.

### A. Enumerasi akun & email lewat pesan diferensial + oracle status OTP di halaman konfirmasi (SEDANG)
- **Lokasi:** `cmd/server/main.go:1255-1257, 1264-1266, 1382-1398, 1438-1449, 1461-1463`
- **Masalah:**
  - Register POST membedakan penyebab kegagalan: `Username sudah digunakan.` (1255-1257) dan `Email sudah terdaftar. Gunakan email lain atau masuk dengan akun yang ada.` (1264-1266) — oracle keberadaan username/email langsung dari respons.
  - GET `/register/confirm?username=X` (1382-1398) merender halaman konfirmasi — termasuk `masked_email` dari `maskEmail()` (1401-1413: karakter pertama + terakhir bagian lokal, **domain penuh terlihat**) — untuk pengunjung mana pun tanpa gerbang sesi; cukup menebak username. User yang tidak pending-OTP hanya di-redirect ke `/login` (1388-1390) → perbedaan respons = oracle "akun X sedang menunggu verifikasi + domain emailnya".
  - POST confirm membedakan `User tidak ditemukan.` (1438-1440) vs `Akun Anda sudah aktif atau tidak membutuhkan verifikasi.` (1448-1449) → oracle keberadaan akun untuk semua status, bukan hanya pending.
  - Pesan `Sisa percobaan: N` (1463) membocorkan sisa budget percobaan OTP.
  - Kontras dengan desain seragam yang disengaja di alur recovery: resend OTP memakai `uniform()` di semua cabang (`auth_recovery.go:121-131`), forgot-password selalu redirect netral (`auth_recovery.go:203-241`) — register/confirm tidak mengikuti pola yang sama.
- **Mitigasi yang sudah ada:** Turnstile fail-closed bila diaktifkan (main.go:1231-1239), cap per-IP 3 akun/24 jam (1217-1225), whitelist domain email (1242-1247), 5 percobaan salah → akun pending dihapus (1458-1459), OTP TTL 15 menit (1290).
- **Fix:** seragamkan pesan POST confirm (satu pesan generik); throttle GET `/register/confirm` via RateLimit atau ikat ke sesi registrasi (token sekali-pakai saat redirect); hapus counter "Sisa percobaan". Pesan "username sudah dipakai" di register adalah tradeoff UX konvensional — minimal tutup oracle di confirm.

### B. Diferensial pesan "OTP kedaluwarsa" di reset password (RENDAH-SEDANG)
- **Lokasi:** `cmd/server/auth_recovery.go:333-340` vs `345-352`
- **Masalah:** cabang tanpa kode aktif memakai pesan seragam (333-336: "Kode OTP salah atau sudah tidak berlaku. Silakan minta kode baru.") dan cabang kode salah memang sengaja disamakan (345-352, lihat komentar 341-344) — tetapi cabang kedaluwarsa (337-340) memakai pesan berbeda ("Kode OTP telah kedaluwarsa. Silakan minta kode baru.") → penyerang yang mengetahui username bisa membedakan "akun ada & sempat punya kode reset" vs "tidak dikenal / tidak ada kode", mengalahkan tujuan komentar di baris 330-332.
- **Mitigasi yang sudah ada:** Turnstile fail-closed (310-318), cap 5 percobaan + kode di-NULL saat limit (346-349), TTL 15 menit.
- **Fix:** gunakan pesan yang sama dengan cabang 333-336 untuk cabang kedaluwarsa (perilaku lain tidak berubah).

### C. OTP 6-digit disimpan plaintext at-rest (RENDAH-SEDANG, hardening)
- **Lokasi:** `cmd/server/main.go:1276-1292, 1307` (register), `cmd/server/auth_recovery.go:135-136, 223-224` (resend & forgot-password), perbandingan polos `main.go:1454` & `auth_recovery.go:345`
- **Masalah:** kode OTP mentah disimpan ke kolom `otp_code` dan dibandingkan sebagai string biasa — siapa pun dengan akses baca DB (SQLi di modul lain, backup bocor, read replica, tooling admin DB) dapat membaca kode yang masih hidup dan mengambil-alih akun dalam jendela TTL 15 menit tanpa perlu akses email korban. Single-use sudah benar (kode di-NULL setelah sukses, auth_recovery.go:361-363) — masalahnya murni at-rest. Sisi timing perbandingan `!=` dapat diabaikan (ruang 6 digit + lockout 5 percobaan) dan bukan inti temuan.
- **Fix:** simpan hash OTP (bcrypt seperti `password_hash`, atau HMAC-SHA256 dengan secret server) dan bandingkan hash vs hash.

### D. Akun pending registrasi dihapus permanen setelah 5 tebakan OTP salah (RENDAH-SEDANG)
- **Lokasi:** `cmd/server/main.go:1458-1459` (DELETE saat limit), `main.go:482-483` (middleware confirm), `cmd/server/auth_recovery.go:31` (maxOTPAttempts=5)
- **Masalah:** saat tebakan OTP salah ke-5, confirm-register mengeksekusi `DELETE FROM admin_users WHERE id = $1` — menghapus seluruh akun pending, bukan sekadar kode. POST `/register/confirm` hanya dilindungi RateLimit 5/menit + CSRF (`main.go:483`), tanpa Turnstile; GET `/register/confirm` (`main.go:482`) bahkan tanpa middleware dan membocorkan status pending (temuan A). Penyerang yang mengetahui username korbannya cukup mengirim 5 tebakan sampah (muat tepat dalam budget 5/menit, atau disebar antar-menit/IP — counter percobaan disimpan per-akun, bukan per-IP) → registrasi korban terhapus dalam jendela TTL 15 menit → username langsung bebas didaftarkan ulang penyerang dengan emailnya sendiri (cap 3 akun/24 jam per IP tidak menghentikan satu target). Kontras: alur reset password hanya meng-NULL-kan `otp_code` saat limit (`auth_recovery.go:346-349`) — akun tetap utuh; perilaku destruktif ini unik di jalur register. Pencatatan di temuan A memandang delete-on-limit sebagai mitigasi brute-force — temuan ini menunjukkan mitigasinya sendiri dapat dipersenjatai.
- **Fix:** saat limit tercapai, NULL-kan `otp_code` saja (paritas dengan alur reset) sehingga korban bisa meminta kode baru; atau wajibkan Turnstile pada POST confirm sebelum tebakan dihitung.

### E. GET /download & /download/apk tanpa rate limit (RENDAH)
- **Lokasi:** `cmd/server/main.go:492-493` (tanpa RateLimit) vs `main.go:494` (`/download/app/:id` punya RateLimit 60/menit)
- **Masalah:** GET `/download` menjalankan query daftar app + pengaturan SaaS (`internal/handlers/public/download.go:33-38`) dan GET `/download/apk` menambah seleksi app Android terbaik (`download.go:72`) lalu menandatangani presigned URL R2 valid 5 menit di setiap request (`download.go:106, 151`) — keduanya tanpa throttle, sementara `/download/app/:id` yang lebih murah justru dibatasi 60/menit. Flood anonim → beban DB + pemanggilan signing R2 tak terbatas.
- **Fix:** tambahkan `middleware.RateLimit` (mis. 60/menit, paritas `/download/app/:id`) pada kedua rute.

---

## ✅ REVIEW QUEUE SUBMISSION, SCOPING TENANT & DATABASE (13 September 2026)

### A. Scoping tenant memakai nama instansi free-text, bukan `instansi_id`/`instansi_code` (TINGGI)
- **Lokasi:** `internal/models/voucher.go:380-395` (`RestoreCascadeSuspendedInstansi`), `voucher.go:404-416` (`SyncInstansiActiveRedemptionsToExpiry`), `internal/models/exam.go:241-246` (`ListExams`), `exam.go:402-411` (`CountExamsByInstansi`), `exam.go:415-424` (`SumStorageByInstansi`), `exam.go:431-441` (`CountRunningExamsByInstansi`), `exam.go:449-464` (`RunningExamCountsAfterActivationByInstansi`)
- **Masalah:** kolom `admin_users.instansi` adalah **nama bebas (free-text), bukan identifier unik** — dua sekolah berbeda boleh sama-sama bernama "SMA Negeri 1". Semua query di atas memfilter tenant dengan `WHERE u.instansi = $1` literal, sehingga pengguna dari sekolah A dicocokkan dengan pengguna/ujian sekolah B yang kebetulan satu nama:
  - `RestoreCascadeSuspendedInstansi` (`voucher.go:387-395`): `UPDATE admin_users u SET status='active', suspended_by_cascade=FALSE ... WHERE u.instansi = $1 AND u.suspended_by_cascade = TRUE AND u.id <> $2` — menghapus skorsing `suspended_by_cascade` lintas sekolah bernama sama saat paket sekolah A diperpanjang.
  - `SyncInstansiActiveRedemptionsToExpiry` (`voucher.go:404-416`): sinkron `expires_at` redemptions milik user sekolah lain ke kedaluwarsa pemilik paket.
  - `ListExams` (`exam.go:243`) + keempat fungsi agregat kuota: ujian dan pemakaian kuota sekolah B ikut terhitung/terlihat di dashboard Operator sekolah A (`created_by IN (SELECT id FROM admin_users WHERE instansi = $N)`), pembagian kuota jadi tidak akurat dan daftar ujian bocor lintas tenant.
  - Kontras dalam file yang sama membuktikan ini ketidaksinkronan, bukan desain: `ListActiveExamsByInstansi` (`exam.go:785-814`) sengaja match **by CODE** dengan komentar eksplisit `exam.go:781-784` ("*Matching is by CODE only (not the free-text instansi name): names are not unique...*"); `UserCanAccessExam`/`UserCanControlExam`/`FilterAccessibleExamIDs` (`exam.go:914-936, 944-965, 973-994`) setidaknya menjaga `me.instansi NOT IN ('', 'personal')`.
- **Fix:** migrasi `instansi_id` sudah tersedia dan terindeks (`internal/database/schema.sql:198-217`, index `idx_admin_users_instansi_id` di `schema.sql:207`) — alihkan semua filter tenant ini ke join `admin_users.instansi_id = instansi.id` (atau minimal `LOWER(u.instansi_code)` paritas `ListActiveExamsByInstansi`), lalu jadikan `instansi` nama hanya data tampilan.

### B. Hasil submission bisa terdampar di buffer `batchChan` saat shutdown — race worker vs batch-inserter (SEDANG)
- **Lokasi:** `internal/queue/submission_queue.go:184` (channel buffer `batchSize*2` = 100), `submission_queue.go:316-331` (send-vs-quit worker), `submission_queue.go:376-418` (`runBatchInserter`), `submission_queue.go:397-407` (loop drain quit-inserter)
- **Masalah:** saat shutdown, `runBatchInserter` memilih quit dan hanya men-drain `batchChan` sampai kosong sesaat itu (`case <-q.quit: ... default: flush(); return`). Worker yang **masih memproses job terakhirnya** bisa lolos seleksi: (1) `processSubmission` selesai, (2) select send-vs-quit menempatkan hasil ke buffer 100-slot `batchChan` **berhasil** (buffer belum penuh), (3) BARU kemudian worker melihat quit dan keluar. Jika `runBatchInserter` sudah keluar dari loop drain-nya di antara (2) dan (3), hasil itu **terdampar permanen di buffer tanpa pembaca** — tidak pernah di-flush, tidak di-spool, tidak di-enqueue ulang. Jalur quit worker (`submission_queue.go:316-331`) memang re-enqueue best-effort via `EnqueueSubmissionWithJob`, tapi itu hanya menyelamatkan job yang **belum selesai diproses**; job yang sudah dikirim ke buffer justru lolos dari jalur itu. Burst ≥ batchSize saat shutdown memperbesar kemungkinan race ini.
- **Fix:** pisahkan `WaitGroup` worker: `runBatchInserter` men-drain `batchChan` dengan `for { select { case r := <-batchChan: ... case <-quit: /* cek wgWorker.Wait() dulu; baru flush+return */ } }` — inserter hanya boleh keluar setelah semua worker `Done()`; atau pada select send-vs-quit worker, prioritaskan case quit (nested select/cek quit dua kali) sehingga job selesai-lalu-terkirim setelah quit tidak mungkin.

### C. Worker tidak memvalidasi ulang status/jadwal ujian saat konsumsi (SEDANG)
- **Lokasi:** `internal/queue/submission_queue.go:349-373` (`processSubmission` — satu-satunya query: `SELECT questions_json FROM exams WHERE id = $1`) vs validasi hanya di waktu enqueue `internal/handlers/api/exams.go:1009-1054`
- **Masalah:** gerbang `status='active'` + jadwal `start_time/end_time` + pengawas hanya diperiksa handler saat job masuk antrean. Di sisi konsumsi, worker membaca `questions_json` begitu saja: job yang menggendap di Redis ( backlog besar / retry / downtime ) tetap dijalankan meski ujian sudah `ended`/`draft`, kuota dibatalkan, atau ujian dihapus pengawasnya — menulis jawaban permanen ke `submissions` untuk ujian yang sudah tidak sah, dan event kuota `RunningExamCountsAfterActivationByInstansi` bisa tergelincir.
- **Fix:** di `processSubmission`, perlebar query yang sama menjadi `SELECT questions_json FROM exams WHERE id = $1 AND status = 'active'` (+ cek `end_time` bila berlaku) — jika kosong, tulis hasil gagal deterministik (bukan retry) tanpa menyentuh `submissions`.

### D. Job yang gagal permanen setelah retry-habis di-drop tanpa jejak (SEDANG)
- **Lokasi:** `internal/queue/submission_queue.go:607-625` (`retryOrFail`), cabang terminal `submission_queue.go:624`
- **Masalah:** setelah `maxRetriesPerJob` (3) kehabisan, cabang terminal hanya `w.storeResult(context.Background(), r.Job.JobID, false, nil, msg)` — payload jawaban siswa **tidak di-spool ke disk dan tidak dipersist**; satu-satunya jejak adalah key kegagalan Redis dengan **TTL 5 menit** (`storeResult`). Bandingkan jalur re-enqueue-FAILED yang sudah benar (fix M5 terverifikasi, `submission_queue.go:611-621` — fallback spool ke disk saat Redis mati): jalur terminal ini kehilangan jawatan permanen. Siswa melihat status "diproses" selamanya, admin tak punya data untuk recovery, dan bukti ujian (forensik kecurangan) hilang.
- **Fix:** pada cabang terminal, tulis payload ke spool terpisah `failed/` (atau row `failed_submissions` di DB) sebelum `storeResult`; sediakan halaman/endpoint admin untuk inspeksi & replay manual.

### E. Kolom `exams.active_token` di-query panas tanpa indeks (SEDANG)
- **Lokasi:** `internal/database/schema.sql:88` (`active_token TEXT NOT NULL DEFAULT ''` — tanpa indeks; kontras `token` di `schema.sql:68` yang UNIQUE) dan pemakaianya `internal/models/exam.go:576-579` (`GetExamByActiveToken`: `SELECT ... FROM exams e WHERE e.active_token = $1`) serta bentuk OR `exam.go:150` (`GetExamByToken` — `active_token = $1 OR token = $1`, seq scan pasti karena salah satu sisi OR tak terindeks)
- **Masalah:** `GetExamByActiveToken` adalah jalur panas autentikasi aplikasi Android (setiap heartbeat/start/pengumpulan ujian), dieksekusi dengan query full-table-scan pada tabel `exams` yang tumbuh per sekolah/ujian. Perangkat siswa ratusan → puluhan scan berat per detik saat ujian serentak; OR-form `GetExamByToken` memaksa scan bahkan saat lookup via `token` (sisi `active_token` tak terindeks → planner tidak bisa pakai index `token`).
- **Fix:** `CREATE UNIQUE INDEX IF NOT EXISTS idx_exams_active_token ON exams(active_token) WHERE active_token <> ''` (UNIQUE juga memberi idempotensi token aktif; `token` sudah jadi preseden di `schema.sql:68`), dan tambahkan ke daftar CREATE INDEX idempoten `schema.sql:182+` agar upgrade aman re-run.

### F. Versi aplikasi (`android_version`/`webapp_version`) disimpan tanpa validasi format (RENDAH-SEDANG)
- **Lokasi:** `internal/handlers/admin/settings.go:420-425` (`body.AndroidVersion` → hanya `TrimSpace` + cek non-kosong → langsung `SetSaasSetting`)
- **Masalah:** string apa pun (spasi, newline, `; rm -rf`, 10KB) diterima sebagai versi dan disimpan ke `saas_settings`; nilai ini dibandingkan/ditampilkan pada alur download APK & gate versi klien. Versi kotor membuat perbandingan versi di sisi klien/server salah dan tampilan UI rusak. (Catatan: ada `NormalizeAppVersion` di helpers untuk jalur lain — jalur settings ini melewatinya.)
- **Fix:** normalisasi + validasi pola `^v?\d+(\.\d+){0,3}$` (atau pakai helper yang sama dengan jalur lain), tolak dengan 400 bila tidak cocok; cap panjang (mis. ≤ 32).

### G. Temuan kecil (RENDAH)
- **G.1 Job racun di-drop diam-diam** — `internal/queue/submission_queue.go:304-308`: `json.Unmarshal` gagal → `log.Printf` + `continue`; payload tak tersimpan di mana pun (Redis key 5-menit pun tidak dibuat). Fix: spool ke `poison/` untuk inspeksi.
- **G.2 `SafeStoragePath` prefix tanpa pemisah direktori** — `internal/helpers/utils.go:185-192`: cek `full != cleanBase && !strings.HasPrefix(full, cleanBase)` menerima path `/data/uploads-evil` saat base `/data/uploads` (prefix string, bukan path). Fix: `strings.HasPrefix(full, cleanBase+string(os.PathSeparator))`.
- **G.3 `checkRateLimit` non-atomik** — `internal/handlers/api/exams.go:214-228`: pattern GET→SETEX memungkinkan burst kecil melewati batas pada permintaan konkuren sebelum TTL terpasang. Fix: `SET key n EX ttl NX` + `INCR` (atomic).
- **G.4 Instansi cascade tidak ikut transaksi Operator** — `users.go:1327-1334`: sinkron `instansi_id` pasca-update instansi memakai `pool` sementara sisanya dalam `opRoleTx`. (Diverifikasi pada review handler admin, lihat bagian berikut — dicatat di sini karena sejenis dengan A.)

---

## ✅ REVIEW HANDLER ADMIN: ESKALASI INSTANSI, IDOR SUBMISSION & DASHBOARD OPERATOR (13 September 2026)

Review handler admin: alur instansi (update/claim), manajemen user (edit/hapus/verifikasi), riwayat submission, dan dashboard Operator. Setiap tautan rantai dibaca langsung di kode dan diverifikasi sebelum dicatat di sini.

### A. Operator dapat mengambil-alih dan menghapus superadmin bootstrap lewat kolisi nama instansi `"owner"` (KRITIS)
- **Lokasi:** `internal/handlers/admin/users.go:2262-2266, 2290-2315, 2330-2348, 1137-1141, 1249-1251, 1327-1334, 1811-1859, 2063-2115`; `internal/handlers/admin/entitlement.go:106-112`; `internal/models/user.go:352-355` (bucket `"owner"`); `cmd/server/auth_recovery.go:355, 361-363`; rute `main.go:734` dalam grup `AdminManagementRequired` (`main.go:726`)
- **Masalah:** seluruh gerbang tenant memakai **kesetaraan nama instansi free-text**, dan `"owner"` — bucket tempat akun superadmin bootstrap dibuat `EnsureAdminUser` — tidak masuk daftar nama terlarang. Operator mana pun dapat masuk bucket tersebut dan mendapat kendali penuh atas akun superadmin. Rantai lengkap, semua tautan terverifikasi:
  1. **Ganti nama instansi sendiri menjadi `"owner"`** — validasi `UpdateInstansi` (`users.go:2262-2266`) hanya menolak `""`, `"personal"`, dan panjang < 3 → `"owner"` lolos. Rutenya (`main.go:734`) berada dalam grup `AdminManagementRequired` (`main.go:726`) yang dapat dijangkau Operator.
  2. **Klaim lolos guard satu-operator-per-sekolah** — `schoolAlreadyHasOperator` (`entitlement.go:106-112`) memfilter `role ILIKE '%"operator"%'`; akun bootstrap ber-role `superadmin` tidak terlihat oleh filter ini → tidak memblok klaim (`users.go:2303-2315`). Advisory lock (`users.go:2290-2291`) hanya men-serialisasi klaim, tidak memvalidasi nama.
  3. **Operator masuk bucket nama `"owner"`** — cabang else klaim (`users.go:2330-2348`) INSERT baris `instansi` baru dan UPDATE `instansi`/`instansi_id`/`instansi_code` operator → nama instansi operator kini identik dengan nama bucket superadmin.
  4. **Gerbang EditUser lolos** — cek Operator memakai kesetaraan NAMA instansi (`users.go:1137`) → `"owner" == "owner"` lolos; guard role di `:1141` tidak relevan karena target superadmin, bukan operator. Sinkronisasi `instansi_id` pasca-update juga berjalan di `pool`, bukan `opRoleTx` (`users.go:1327-1334` — substansi G.4 review queue: perubahan bisa ter-commit di luar transaksi bila `opRoleTx` rollback).
  5. **Overwrite email superadmin** — update `email` (`users.go:1249-1251`) TIDAK punya guard superadmin; empat guard `!isSuperAdminTarget` yang ada (`:1341` status, `:1354` role, `:1452` password, `:1591` beku-reaktivasi) tidak mencakup `email`/`name`/`whatsapp`/`expires_at`. Kontras: cascade kedaluwarsa paket (`:1482-1535`) justru membawa guard defense-in-depth — field identitas terlewat, ini inkonsistensi, bukan desain.
  6. **Pengambilalihan penuh lewat reset password publik** — email superadmin kini milik penyerang → alur publik lupa-password mengirim OTP ke email itu → handler reset menulis `password_hash` baru (bcrypt benar via `models.HashPassword`, `auth_recovery.go:355`) langsung ke akun superadmin (`auth_recovery.go:361-363`) → **akun superadmin bootstrap diambil alih sepenuhnya**.
  7. **Jalur destruktif independen: DELETE & VERIFY superadmin** — `DeleteUser` (`users.go:2063-2115`) hanya memblok self-delete (`:2077-2080`), target bernama sama (`:2097`), dan operator lain (`:2108`); tidak ada guard superadmin/target-role → Operator dapat **menghapus akun superadmin bootstrap**. Cascade penghapusan hanya menyasar target ber-role operator, jadi penghapusan superadmin bersih satu-akun; karena `EnsureAdminUser` hanya berjalan saat startup, akun tidak dibuat ulang → **DoS akses superadmin permanen**. `VerifyUser` (`users.go:1837-1848`) bolong dengan pola sama (hanya cek nama sama `:1837` dan blok operator lain `:1845-1848`).
- **Fix:** (1) tolak nama terlarang (`"owner"` minimum, idealnya semua nama bucket sistem) di `UpdateInstansi` dan pada perubahan `instansi` di `EditUser`; (2) alihkan pencocokan tenant ke `instansi_id` — kolom, index, dan backfill sudah tersedia (`internal/database/schema.sql:198-217`) — di gerbang EditUser/DeleteUser/VerifyUser; (3) tambah guard target-superadmin di `DeleteUser` & `VerifyUser`, dan perluas `!isSuperAdminTarget` ke `email`/`name`/`whatsapp`/`expires_at` di `EditUser`; (4) ganti `role ILIKE '%"operator"%'` dengan pencocokan role eksak (`entitlement.go:111`, `users.go:2303, 2345`, dan situs serupa lain — lihat temuan D di bawah).
- **Catatan:** rantai ini akar masalahnya berbeda dari temuan #9 (role-singular, sudah diperbaiki 2026-07-26) — di sini akarnya kolisi NAMA bucket sistem, bukan parsing role. Varian lintas-sekolah (bocor kuota/ujian antar tenant bernama sama) sudah dicatat sebagai temuan A review queue; temuan ini varian paling parah karena menyentuh bucket sistem tempat superadmin tinggal.

### B. Info ujian bocor lintas tenant lewat kartu filter exam_id di halaman submissions (TINGGI)
- **Lokasi:** `internal/handlers/admin/submissions.go:239-306` (blok `examInfo` di `SubmissionsPage`); gerbang halaman `:57` (Guru juga lolos ke halaman ini)
- **Masalah:** saat `?exam_id=N` diberikan, handler memanggil `GetExamByID` (`:240-241`) **tanpa cek ownership/tenant sama sekali**, lalu merakit kartu info: `sub_count` dari query global `SELECT COUNT(*) FROM submissions WHERE exam_id = $1` (`:244-245`), nama creator (`:247-249`), nama pendelegasi (`:251-255`), daftar username pengawas (`:258-272` — JOIN `exam_pengawas`×`admin_users`), dan `gin.H` `:287-304` yang menyertakan **`"token": exam.Token` (`:289` — token ujian utama)** plus jadwal, size, dan status tombstone. Enumerasi `?exam_id=1,2,3,...` oleh admin mana pun ber-role operator/guru membocorkan nama ujian, token ujian, username creator/delegated/pengawas, jumlah peserta, dan jadwal ujian tenant lain — meski baris submission di tabel tampak nol (list di `:84-127` tetap ter-scope), kartu info tetap terisi penuh.
- **Mitigasi yang sudah ada:** SEMUA sibling path di file yang sama sudah benar — list ter-scope via `buildScopeConditions` (`:84-127`), `ListSubmissions` memanggil `UserCanAccessExam` → 403 (`:480-483`, dengan komentar H2), `SubmissionDetail` & `DeleteSubmission` lewat `checkSubmissionOwnership` (`:528-531`, `:604-607`), dan `ExportSubmissions` lewat `checkExamOwnership` dengan komentar eksplisit "to prevent cross-tenant IDOR that would otherwise leak another tenant's student PII" (`:639-646`). Hanya blok kartu info halaman yang terlewat — persis pola "single-path aman, path lain bolong" yang dicatat di Catatan umum.
- **Fix:** bungkus blok `examInfo` dengan `checkExamOwnership`/`UserCanAccessExam` (pola `ExportSubmissions :643`) sebelum query apa pun; idealnya sentralisasikan pemeriksaan di SEMUA endpoint ber-`exam_id` (rujuk Catatan umum).

### C. Dashboard Operator fail-open: instansi kosong → daftar ujian & statistik global lintas tenant (TINGGI)
- **Lokasi:** `internal/handlers/admin/dashboard.go:61-75, 118-127, 264-292, 465-472`; `internal/models/exam.go:241-249, 327-329`
- **Masalah:** operator dengan kolom `instansi` kosong (akun lama pra-migrasi, atau nilai gagal scan) mendapat **daftar & statistik ujian SEMUA tenant**, bukan hanya miliknya:
  1. **Daftar global** — cabang operator melewati scope UserID (`dashboard.go:61-66`: `opts.UserID` hanya di-set untuk Guru/Pengawas; operator → `nil`), lalu `:69-75` mengisi `opts.Instansi` via `QueryRow` mentah `SELECT instansi FROM admin_users WHERE id = $1` — **bukan** `getInstansiForOperator`, tanpa guard `""`/`"personal"` → instansi kosong membuat `ListExams` tidak menambahkan kondisi apa pun (`exam.go:241-246` skip saat `opts.Instansi == ""`) DAN `opts.UserID == nil` melewati seluruh cabang visibility (`exam.go:249` → tanpa WHERE created_by/pengawas) → hasil `:77` adalah daftar ujian global seluruh tenant. Baris per ujian membocorkan **`token` dan `active_token`** (`examItem` `dashboard.go:268-269`), sehingga ini sekaligus kebocoran token ujian lintas tenant.
  2. **Statistik global** — jalur stats fail-open identik: `Dashboard` (`:118-127`) dan `Stats` JSON (`:465-472`) memakai pola `} else if isOp { ... if instansi != "" { tambah WHERE } }` — instansi kosong → **tanpa WHERE** → `COUNT/SUM` dihitung atas seluruh tabel `exams` lintas tenant (jumlah ujian, total size storage, dsb.).
  3. **Pengakuan kelas bocor oleh codebase sendiri** — komentar di `exam.go:327-329` menyatakan `ListActiveExams` dihapus karena "a global, unscoped list of every tenant's active exams is a cross-tenant leak" — persis kelas bocor yang masih hidup di jalur ini.
- **Mitigasi yang sudah ada:** jalur sibling sudah fail-closed dan menjadi kontras — `buildScopeConditions` submissions (`submissions.go:337-355`: `getInstansiForOperator` + fallback own-created saat `""`/`"personal"`), `helpers.go:312-316`, `exam.go:914/944/973`, `pengawas.go:167-170`, `main.go:1298`. Dashboard adalah satu-satunya yang masih memakai `QueryRow` mentah tanpa guard.
- **Fix:** gunakan `getInstansiForOperator` + fallback fail-closed (instansi `""`/`"personal"` → scope own-created, pola `buildScopeConditions`) untuk daftar ujian (`:69-75`) dan kedua jalur stats (`:118-127`, `:465-472`); nilai kosong tidak boleh berarti "tanpa kondisi".

### D. Pencocokan role via `ILIKE '%"operator"%'` — substring match, bukan pencocokan eksak (RENDAH — hardening)
- **Lokasi:** 28 baris produksi di 7 file, dikelompokkan per dampak:
  - **Gating otorisasi (paling sensitif):** `internal/models/exam.go:928` (`UserCanAccessExam`), `:957` (`UserCanControlExam`), `:985` (`FilterAccessibleExamIDs`) — cabang operator `me.role ILIKE '%"operator"%' AND me.instansi ... = owner.instansi`; `internal/handlers/admin/entitlement.go:111` (`schoolAlreadyHasOperator` — guard satu-operator-per-sekolah, komponen yang dilewati temuan A).
  - **Kuota/paket:** `internal/handlers/admin/users.go:230` (kuota akun operator per sekolah), `internal/handlers/admin/billing.go:130` (label paket sub-account), `internal/handlers/admin/entitlement.go:168` (kuota pool sekolah).
  - **Cascade UPDATE berskala instansi:** `internal/handlers/admin/users.go:1498` & `:1524` (cascade expired/unlimited), `:1752` (GREATEST perpanjang); `internal/handlers/admin/entitlement.go:346` (operator ter-cover), `:363` (cascade suspend), `:388` (restore perpanjang), `:413` (`creatorFilter = 'NOT (role ILIKE \'%"operator"%\')'` di `tombstoneUnstartedInstansiExams`).
  - **Exemption job:** `internal/handlers/admin/expiry_job.go:142` — bentuk GANDA `AND NOT (u.role = 'superadmin' OR u.role ILIKE '%"superadmin"%')` — equality legacy + ILIKE JSON hidup berdampingan di satu klausa (bukti format role pernah tidak konsisten).
  - **Filter/sort daftar:** `internal/models/user.go:499-502` & `:519-522` (CASE tier sort role), `:577-579` (`ExcludeOperator` `NOT ILIKE`), `:589-593` (`RoleFilter`, dengan komentar format JSON verbatim `// Role filter (ILIKE on JSON array string like `["guru"]`)`).
  - **Picker user:** `internal/handlers/admin/exams.go:1359` (`availablePengawas`), `:2163` (`availableGurus`), `:2193` (`availablePengawas`).
- **Masalah:** role disimpan sebagai string JSON (`["operator"]`) — dikonfirmasi komentar `internal/models/user.go:589` dan `parseRole` di `settings-packages.js:9-16` yang `JSON.parse` nilai role — tetapi seluruh 28 situs mencocokkannya dengan substring `ILIKE '%"operator"%'`, bukan pencocokan eksak. Konsekuensi: (1) nilai legacy non-JSON (`operator` tanpa kutip) **tidak cocok** dengan pola terkutip — `expiry_job.go:142` bahkan menghedging dua bentuk sekaligus (`u.role = 'superadmin' OR u.role ILIKE '%"superadmin"%'`), bukti drift format role nyata dan mismatch bisa terjadi diam-diam; (2) array multi-role (`["operator","guru"]`) dicocokkan sebagai operator oleh SEMUA cek — semantik array JSON tak pernah didefinisikan eksplisit. Arah salah-match langsung menyentuh keputusan otorisasi (`exam.go:928/957/985` menentukan siapa boleh mengakses/mengontrol ujian; `entitlement.go:111` — guard yang dilewati temuan A) dan blast radius UPDATE cascade (`users.go:1498/1524/1752`, `entitlement.go:346/363/388/413` menyentuh semua user satu instansi sekaligus).
- **Fix:** pencocokan role eksak di semua 28 situs — parse JSON role di lapis model (bentuk kanonik tunggal), atau migrasi kolom ke `jsonb` + operator `@>`, atau normalisasi ke bentuk kanonik pada setiap tulis. Prioritaskan situs gating otorisasi & entitlement (`exam.go`, `entitlement.go`, `users.go`, `billing.go`) sebelum situs tampilan/sort (`user.go` CASE, picker `exams.go`).

### E. SaveQuestions menelan error penyimpanan pengawas — handler menjawab "sukses" padahal roster tidak tersimpan (SEDANG)
- **Lokasi:** `internal/handlers/admin/exams.go:1522-1580` (dalam handler `SaveQuestions`, func mulai `:1417`); `internal/models/exam_pengawas.go:87-96` (`CreateExamPengawas`), `:100-125` (`SetPengawasForExam`)
- **Masalah:** setelah `UpdateExamQuestions` sukses (`:1514-1520` — jalur ini benar mengembalikan 500 saat gagal), handler menyimpan roster pengawas dalam dua cabang yang KEDUANYA menelan error:
  1. **Operator/superadmin** (`isOp || isSuperAdmin`, `:1525`; roster hanya di-replace saat `body.PengawasIDs != nil` `:1529`): `if err := models.SetPengawasForExam(...); err != nil { log.Printf(...) }` (`:1570-1572`) — error hanya masuk log server, lalu eksekusi tetap jatuh ke `successMessage(c, "Konfigurasi soal berhasil disimpan")` (`:1589`). Modelnya transaksional (`Begin`/`defer Rollback`/`DELETE` + `INSERT ... ON CONFLICT DO NOTHING`/`Commit`, `exam_pengawas.go:100-125`), jadi kegagalan = roster secara atomik tetap versi LAMA (tidak korup) — tetapi operator percaya roster barunya sudah tersimpan: divergensi senyap antara yang tampil di layar dan isi DB, tanpa pesan error apa pun ke pengguna.
  2. **Guru** (cabang `else` `:1575-1580`): `CreateExamPengawas` — yang memastikan creator bisa mengawasi ujiannya sendiri tanpa menghapus roster hasil penugasan operator (komentar `:1575-1577`) — error-nya juga hanya di-log (`:1578-1580`): guru-creator mungkin tidak bisa membuka approvals/submissions ujiannya sendiri, tetap menerima "sukses".
- **Fix:** perlakukan error pengawas sebagai kegagalan simpan — minimal `errorResponse` 500 dengan pesan "konfigurasi soal tersimpan, tetapi penugasan pengawas gagal — coba lagi" (dan di UI jangan tutup editor seolah semua bersih), atau jalankan penyimpanan roster dalam transaksi yang sama dengan pertanyaan; `successMessage` tidak boleh dikirim setelah error yang hanya di-log.

### F. `GetPengawasIDs`/`GetPengawasAssignments`: `rows.Err()` hanya di-log — list parsial dikembalikan seolah utuh (RENDAH — berantai dengan E)
- **Lokasi:** `internal/models/exam_pengawas.go:43-45` (`GetPengawasIDs`), `:75-77` (`GetPengawasAssignments`); pemanggil yang ikut mengabaikan error: `internal/handlers/admin/exams.go:1339` (`assignments, _ :=`), `:2210` (`assignedIDs, _ :=`), `internal/handlers/admin/pengawas.go:94` (`pengawasAssignments, _ :=`)
- **Masalah:** iterasi `rows.Next()` yang terputus di tengah jalan (konektivitas/context) menghasilkan list PARSIAL; `rows.Err()` hanya masuk `log.Printf` lalu list parsial dikembalikan seolah lengkap. Ketiga pemanggil memanggil model dengan `_, _` — pengabaian error berlapis dua. Dampak nyata: `exams.go:2210-2221` memuat `assigned_pengawas_ids` untuk picker penugasan pengawas (payload JSON `assigned_pengawas_ids`, `available_gurus`, `available_pengawas`) — list parsial/kosong membuat operator "tidak melihat" pengawas yang sebenarnya sudah ditugaskan; dikombinasikan dengan temuan E (SaveQuestions me-replace seluruh roster saat `PengawasIDs != nil`), operator bisa tidak sengaja MENGGANTI roster dengan hanya subset yang tampil — pengawas lama hilang tanpa pesan error apa pun. `exams.go:1339` dan `pengawas.go:94` menampilkan daftar pengawas ujian yang bisa tampil kosong/parsial seolah belum ada penugasan.
- **Fix:** kembalikan error dari kedua model tersebut (jangan hanya log); di handler, tolak render picker dengan pesan error bila load gagal — list kosong yang tak dibedakan dari "belum ada penugasan" berbahaya untuk picker yang dipakai me-replace roster (rujuk E).

### G. Impor XML menerima soal tanpa kunci — TERPERSIST senyap lalu dinilai degenerate saat ujian dikerjakan (SEDANG)
- **Lokasi:** `static/js/admin.js:2706-2806` (`importXMLQuestions` — parsing DOMParser mode `"text/xml"`), `:1258-1276` (`replaceEditorQuestions` — tanpa validasi isi), `:1342-1360` (`getQuestionsFromEditor` — keyRaw kosong diteruskan per tipe); `internal/handlers/admin/exams.go:1458-1462` (`SaveQuestions` marshal verbatim), `internal/models/exam.go:550-564` (`UpdateExamQuestions` UPDATE tanpa validasi isi); konsumsi saat penilaian: `internal/models/submission.go:53-59` (`GetKey`), `:122-137` (cabang nil + `single_choice`/`true_false`/`short_answer`), `:185-235` (`multiple_choice`), `:237-280` (`matching`), `:313-326` (`CalculateSubmissionScore`), `:328-335` (`ComputeMaxScore`)
- **Masalah:** rantai lengkap tanpa satu pun gerbang validasi kunci di sepanjang jalur impor→simpan→nilai:
  1. **Impor XML** (`admin.js:2706-2806`): dalam mode XML, `getElementsByTagName` case-sensitive. Tag `<question>` yang tidak ketemu memang error keras (toast merah — benar), tetapi tag `<key>` yang tidak ketemu (XML eksternal berkapsul `<Key>`/`<Answer>`, atau memang tanpa kunci) menghasilkan kunci `''`/`[]`/`{}` SENYAP — soal tetap dimuat ke editor dan toast hijau "Berhasil mengimpor N soal dari XML!" tetap tampil. Guard konfirmasi T15 (`:2793-2795`) hanya menutup jalur destruktif timpa isi editor, bukan validasi isi.
  2. **Editor** (`replaceEditorQuestions:1258-1276`) tidak memvalidasi isi; `getQuestionsFromEditor:1342-1360` meneruskan keyRaw kosong apa adanya per tipe — single_choice/true_false → `''` (via `toUpperCase()`), multiple_choice → `[]` (filter kosong), matching → `{}`, short_answer → `''`; `questions.push(q)` unconditional, kartu tanpa "Kunci" ikut dikumpulkan.
  3. **Server** (`exams.go:1458-1462`): `raw, _ := json.Marshal(body.Questions)` verbatim tanpa satu pun cek kunci (kontras: jadwal invalid ditolak keras 400 di `:1486-1492`); `UpdateExamQuestions` (`models/exam.go:550-564`) menjalankan `UPDATE exams SET questions_json = $1 ... WHERE id = $9` buta — soal tanpa kunci TERPERSIST ke `questions_json`.
  4. **Penilaian degenerate** (`models/submission.go`): `stripAnswerKeys` (`api/exams.go:1702-1721`, dipanggil `:748-749`) memang sudah menstrip kunci sebelum soal dikirim ke siswa — kunci kosong hanya dikonsumsi saat grading (sinkron `calculateScoreSync` `api/exams.go:1186-1210` / pekerja async Redis, keduanya bermuara ke `CalculateSubmissionScore`). `GetKey` (`:53-59`) mengembalikan `Key` selama non-nil — string/array/map kosong lolos sebagai "jawaban benar". Akibat per tipe: (a) kunci `''` (`:131-137`): jawaban siswa wajar TIDAK PERNAH cocok → `Earned 0`, padahal bobot soal tetap dihitung di penyebut `ComputeMaxScore` (`:328-335`) — poin siswa berkurang diam-diam; (b) jalur degenerate hadiah penuh: jawaban kosong siswa `""` cocok kunci `""` → bobot PENUH; multiple_choice kunci `[]` non-partial (`:218-234`): himpunan kosong vs kosong lolos perbandingan → bobot penuh; matching kunci `{}` non-partial (`:267-279`): `range` map kosong tak pernah gagal → bobot penuh; (c) kunci benar-benar absent (nil) selalu `Earned 0` (`:122-128`) — tidak bisa diraih lewat jalur mana pun. Nol error di semua lapisan: kegagalan integritas data baru ketahuan SETELAH siswa mengerjakan ujian.
- **Fix:** (1) di `importXMLQuestions`: kumpulkan soal ber-kunci kosong dan tampilkan error menyebut nomor soalnya (atau tolak seluruh impor); (2) di `getQuestionsFromEditor`: tandai/skip kartu ber-key kosong dengan konfirmasi eksplisit; (3) di server `SaveQuestions`: validasi keberadaan kunci per soal → 400 menyebut nomor soal bermasalah (paritas dengan validasi jadwal `:1486-1492`) — lapisan server wajib karena klien selalu bisa dilewati.
- **Mitigasi yang sudah ada:** AI prompt (`admin.js:2808-2884`) dan ekspor XML aplikasi sendiri (`exportXMLQuestions:1381-1415`) menghasilkan tag lowercase semua → hanya XML eksternal/hand-edited yang memicu; kartu ber-key kosong tetap tampil di editor (kolom Kunci kosong) sehingga operator teliti bisa menangkap sebelum simpan; `quickGenerateQuestions` memang sengaja menghasilkan `key:''` untuk short_answer (`admin.js:1278-1303`) sebagai draft by-design — berbeda dari impor yang menjanjikan soal lengkap.
- **Catatan:** paralel dengan E — kegagalan senyap ditutupi success toast; keduanya baru ketahuan setelah fakta di lapangan terbentuk. Jalur (a) merugikan siswa (bobot terhitung, tak terbakukan), jalur (b) menghadiahkan nilai gratis — dua-duanya merusak kepercayaan hasil ujian tanpa jejak error.

---

## ✅ REVIEW UI: SPRITE SVG, SETTINGS JS, IKON & HALAMAN HASIL PUBLIK (13 September 2026)

### A. Duplikat id simbol `hi-stop` di sprite — definisi Heroicons kedua tak terjangkau / markup mati (RENDAH)
- **Lokasi:** `templates/admin/partials/svg-symbols.html:37` dan `:39`; situs pemakaian: `templates/admin/submissions.html:211`, `templates/admin/pengawas_detail.html:113`, `:1110`, `:1116`, `static/js/settings-vouchers.js:180`
- **Masalah:** pasca c11135c ("satu sumber sprite SVG (5.4#1)"), sprite memuat **dua** definisi `<symbol id="hi-stop">` yang divergen: `:37` varian `<rect x="6" y="6" width="12" height="12" rx="2"/>` dengan `stroke-width="2"` (DITAMBAHKAN oleh c11135c — terlihat sebagai baris + di diff) dan `:39` varian Heroicons resmi (`<path d="M5.25 7.5A2.25 2.25 0 0 1 7.5 5.25h9a2.25 2.25 0 0 1 2.25 2.25v9a2.25 2.25 0 0 1-2.25 2.25h-9a2.25 2.25 0 0 1-2.25-2.25v-9Z"/>`, `stroke-width="1.5"`) yang sudah ada sebelumnya dan tidak ikut dihapus — duplikat id justru lahir dari commit dedupe itu sendiri. Duplikat id dalam satu dokumen → `<use href="#hi-stop">` selalu menyelesaikan ke definisi PERTAMA dalam urutan dokumen (rect `:37`); definisi `:39` tidak pernah terjangkau — dead weight. Semua 6 situs pemakaian (termasuk dinamis `<use href="#${v.is_active ? 'hi-stop' : 'hi-play'}"/>` di settings-vouchers.js:180) merender varian rect, dan test 5.4#1b pada commit itu sendiri menegaskan varian rect (`assert.match(SPRITE, /<symbol id="hi-stop"[^>]*><rect x="6" y="6" width="12" height="12" rx="2"\/><\/symbol>/`) — rect adalah definisi hidup yang disengaja, jadi TIDAK ADA bug visual hari ini. Yang salah: klaim commit "sprite 40 → 41 simbol, tetap nol mati" tidak akurat (sprite memuat 42 definisi, 1 di antaranya mati), dan duplikat ini bahaya laten — penghapusan/penataan-ulang `:37` kelak akan SENYAP mengganti semua ikon stop ke gaya Heroicons `:39` tanpa ada yang menyadari. Scan seluruh 42 id simbol sprite: selain duplikat ini nol simbol tak terpakai.
- **Fix:** hapus definisi Heroicons di `:39` (atau beri id berbeda bila varian itu memang diinginkan di suatu situs), lalu tambahkan asersi test bahwa setiap id simbol unik dalam file.

### B. Nama login admin (username) bocor ke halaman hasil publik — kolom display `name` yang sudah ada justru tak dipakai (RENDAH)
- **Lokasi:** `internal/handlers/public/hasil.go:182-188` (query `SELECT username FROM admin_users WHERE id = $1` untuk creator `:183`, dan untuk DelegatedTo bila non-nil `:187` — error scan diabaikan → string kosong), `:196-197` (lempar `creator_name`/`delegated_name` ke template); render `templates/public/hasil.html:138-149` (`Pembuat:` `:141`, `Guru:` `:147`); kolom display tersedia tapi tak dipakai: `internal/database/schema.sql:21` (`name TEXT DEFAULT ''`), model `internal/models/user.go:55`, dipersist oleh `CreateUser` (`:788-789` kolom, `:794` nilai `u.Name`).
- **Masalah:** username admin adalah kredensial login, bukan nama tampilan, namun dirender ke siapa pun pemegang token ujian pada path 200 (hasil publik aktif atau viewer login). Kolom `name` yang memang dibuat untuk keperluan tampilan justru tidak dipakai — setengah kredensial dibocorkan tanpa kebutuhan; username delegasi (`Guru:`) terekspos sama pada ujian yang didelegasikan.
- **Mitigasi yang sudah ada:** hanya path 200 — 403 bila hasil tidak publik dan viewer tidak login (`hasil.go:164-171`); halaman memang mensyaratkan token ujian; `X-Robots-Tag noindex` + `Cache-Control no-store` (`hasil.go:121-122`, dikunci `TestHasilPageNoIndexHeaders`); rate limit 30/menit per IP pada route halaman itu sendiri (`cmd/server/main.go:500-501`, komentar M1 `:495-499`) maupun API (`:551`) membatasi enumerasi; error scan → string kosong → span disembunyikan `{{if}}` (`hasil.html:138/:144`).
- **Fix:** ganti kedua query ke `SELECT name` dengan fallback string kosong (kolom `name` DEFAULT '' — bila kosong, biarkan span tersembunyi seperti perilaku error saat ini); jangan pernah mengirim `username` ke template publik.
- **Catatan:** tidak ada test yang mengunci eksposur ini — `TestHasilPageNoIndexHeaders` (`hasil_test.go:347-383`) hanya memeriksa headers/meta; `TestHasilAPIAnswersPrivacy` (`:250-276`) mengunci model entitlement jawaban, bukan nama creator. Fixture test sendiri memakai `Name: "Guru Hasil"` (`:37`) — data display memang sudah tersedia di jalur uji, memperkuat bahwa peralihan kolom tidak butuh perubahan skema.

---

## ✅ REVIEW SISA TEMPLATE PUBLIK (shared) & LOGIN ADMIN (13 September 2026)

### A. Skip-link halaman login admin dirender SETELAH nav dan masih inline — pelanggaran paritas M19 + duplikasi partial public_skip_link (RENDAH)
- **Lokasi:** `templates/admin/login.html:44-45` (`{{ template "public_auth_nav" . }}` di `:44`, skip-link inline baru di `:45`); paritas yang benar: `templates/public/shared.html:888/:895` (`public_head`: `public_skip_link` `:888` SEBELUM `public_nav` `:895`), `templates/public/register.html:179-180`, `templates/public/reset_password.html:35-36`, `templates/public/register_confirm.html:150-155` (komentar M19 eksplisit `:153-154`); partial satu-sumber: `templates/public/shared.html:81-89` (`public_skip_link`).
- **Masalah:** dua penyimpangan pada satu-satunya halaman yang lolos dari sapuan. (1) **Urutan dibalik** — keputusan M19 ("skip link dirender SEBELUM auth nav agar Tab pertama melompat ke konten, bukan ke nav") dijalankan di semua halaman publik, tetapi login.html menaruh nav dulu (`:44`) baru skip-link (`:45`); pengguna keyboard harus melewati logo + Unduh Aplikasi + Cek Hasil Ujian + Daftar + Masuk (±5 tab stop; skip-link baru terjangkau pada stop ke-6) — persis situasi yang mau dicegah M19; skip-link kehilangan fungsinya. (2) **Duplikasi inline** — anchor `:45` ditulis tangan meski partial `public_skip_link` ("single source of truth", `shared.html:81-89`) memancarkan markup identik; komentar Batch 6 di `register_confirm.html:151-152` ("satu-satunya sisa skip-link inline kini menyatu") menjadi tidak akurat — pola klaim-kelengkapan-refactor yang meleset, sama dengan temuan A ronde sprite (duplikat `hi-stop` lahir justru dari commit dedupe).
- **Mitigasi yang sudah ada:** markup inline identik dengan partial (href `#main-content`, class `skip-link`, label sama) — tidak ada perbedaan visual; styling `.skip-link` tetap dari theme.css, perilaku fokus tidak rusak, hanya urutan tab yang salah.
- **Fix:** ganti baris `:45` dengan `{{ template "public_skip_link" . }}` dan pindahkan ke atas `{{ template "public_auth_nav" . }}` — persis paritas `register.html:179-180`.
- **Catatan:** login.html adalah standalone template di folder admin (tidak lewat `public_head`) sehingga sapuan Batch 6 yang menyasar folder public melewatkannya — audit dedupe wajib mencakup folder admin yang memakai partial publik. Sisanya halaman ini bersih: seluruh interpolasi dinamis (`{{.next}}`, `{{.csrf_token}}`, `{{.seo_description}}`, `{{.turnstile_site_key}}`) berada di konteks attribute yang ter-escape (lihat butir Ditolak terakhir), JS toggle password/Turnstile/flash-fade aman.

---

## ✅ REVIEW ROUTE API ADMIN vs PENGGUNA FRONTEND (13 September 2026)

### A. Tiga route API admin terdaftar namun tidak punya pemanggil di kode manapun — endpoint mati (RENDAH)
- **Lokasi:** `cmd/server/main.go:774` (GET `/admin/api/submissions` → `admin.ListSubmissions()`), `cmd/server/main.go:777` (GET `/admin/api/submissions/:id/export_detail` → `admin.ExportSubmissionDetail()`, RateLimit 30/menit), `cmd/server/main.go:778` (GET `/admin/api/queue/status` → `admin.QueueStatus()`)
- **Masalah:** diff menyeluruh route-terdaftar vs pemakaian (templates + static produksi, plus klien eksternal parent-tree) menemukan tepat TIGA endpoint admin tanpa konsumen. Substring khas tiap endpoint — `queue/status`, `export_detail`, fetch daftar submissions polos — nol hit di seluruh `templates/` + `static/` (non-`*.test.mjs`), nol hit juga di `android/`, `desktop/`, `apk/`, `windows/`, `docs/`. Pembanding menegaskan ini bukan false-negative grep: halaman submissions dirender penuh server-side (`templates/admin/submissions.html` tidak memuat satu pun `apiFetch`/`fetch`), dan jalur export/detail yang benar-benar dipakai UI adalah route HIDUP yang berbeda — `/submissions/export` (`static/js/admin.js:3500`, route `main.go:776`) dan `/submissions/:id/detail` (`admin.js:4072`, route `main.go:775`). Konsekuensi: `ListSubmissions` dan `ExportSubmissionDetail` jadi duplikat fungsional dua route hidup itu; `QueueStatus` mengekspos metrik internal queue pada path yang tak pernah ditampilkan UI; ketiganya memperluas permukaan API yang wajib diaudit (tetap bisa dipanggil manual oleh admin yang hafal URL) tanpa konsumen yang menjelaskan keberadaannya.
- **Mitigasi yang sudah ada:** ketiganya berada di grup `lockedAPI` non-CSRF GET yang di belakang `AuthRequired` (sesi admin cookie — bukan API publik); `export_detail` ber-RateLimit 30/menit; semuanya read-only — dampak maksimal penyalahgunaan oleh admin yang sudah login hanyalah membaca data yang memang dapat ia akses lewat UI lain.
- **Fix:** hapus ketiga registrasi route beserta handler `ListSubmissions`/`ExportSubmissionDetail`/`QueueStatus` (serta test handler-nya bila ada); atau, bila memang dimaksudkan sebagai fitur mendatang, wiring ke UI terlebih dahulu baru route didaftarkan.
- **Catatan:** (metodologi) substring-grep dijadikan standar final di sini karena konkatenasi dinamis sekalipun tetap memuat fragmen path — berbeda dengan ekstraksi literal yang terpotong di tanda kutip, jebakan yang membuat 13 route lain sempat tampak mati padahal HIDUP via konkatenasi (`/start`, `/stop`, `regenerate-token`, pengawas `approvals`/`submissions`/`audit-logs`/`auto-approve`, `token-mode`, `delegate`, `users/:id`, `users/:id/edit`, `submissions/:id/delete`, `vouchers/audit-logs` — situs pemakaian antara lain `templates/admin/pengawas_detail.html:1073/:1102/:1183/:1394/:1539/:1821/:2001/:2036/:2124/:2170`, `static/js/admin.js:366/:398/:2156/:2329/:2431/:3131/:3562/:4072`, `static/js/settings-voucher-audit.js:26`); dua kandidat sisa lain (`/admin/api/pengawas/state`, `/admin/api/x`) terbukti hanya hidup di fixture test (`static/js/uiux-batch6-jscore.test.mjs:320/:371`, `static/js/admin-core.test.mjs:23`) — bukan pemakaian produksi. Batas metodologi: konkatenasi yang memecah fragmen itu sendiri (mis. `'/queue/st' + 'atus'`) tetap lolos dari grep apa pun — risiko sisa yang sama diterima pada diff Actions sebelumnya.

---

## ✅ Ditolak setelah verifikasi (bukan bug)
- CSRF `!=` non-constant-time — token adalah milik sesi caller sendiri, tak ada oracle. (`csrf.go:81`)
- "Race duplikat pending DOKU" — sudah ada unique index parsial `idx_transactions_pending_doku_unique`. (`schema.sql:261`)
- "Spoofing X-Forwarded-For mengebiri rate limit" — `SetTrustedProxies` hanya memercayai private range (loopback, 10/8, 172.16/12, 192.168/16), `ClientIP()` mengabaikan XFF dari sumber publik. (`main.go:206`)
- "CORS tidak terpasang" — terdaftar sebagai middleware global `r.Use(middleware.CORS(cfg.CORSOrigins))`. (`main.go:215`)
- "TimeoutMiddleware 30 detik memutus koneksi WebSocket" — middleware eksplisit melewati request ber-header `Upgrade: websocket` (juga `/api/health`, `/healthz`). (`timeout.go:21-23`)
- "RateLimit (fingerprint) dan RateLimitIP berbagi bucket yang sama" — dua dimensi PARALEL (user/IP + fingerprint, sengaja dipisah agar rotasi salah satu tidak me-reset keduanya), keduanya dihitung dan dicek dalam satu pipeline. (`ratelimit.go:20-45`)
- "GET /api/exams/:id/pdf me-stream file hingga 100MB dan bisa terpotong oleh request-context 30 detik" — handler TIDAK pernah me-stream byte PDF: ia redirect 302 ke signed URL Cloudflare R2 (server hanya menandatangani URL); plus gerbang approval server-side (`exam_approvals.status='approved'`) dan rate limit per exam+MAC/IP sudah terpasang. (`api/exams.go:799-869`)
- "POST /api/exams/* (main.go:525, 535, 549, 550) tanpa CSRFRequired" — autentikasi Android berbasis token perangkat via header (`X-Exam-Token` exams.go:779, 1000, 1319, 1517, 1633; `X-Device-Id`:824; `X-App-Version`:674/907/1430), bukan cookie session — tidak ada penggunaan `sessions` di handler produksi `internal/handlers/api/` (hanya file `_test.go`). CSRF N/A by construction.
- "SMTP header injection via email registrasi" — `toEmail` pengguna diinterpolasi ke header `To:` lewat `fmt.Sprintf` (`internal/helpers/email.go:16-19`), tetapi `client.Rcpt(toEmail)` (`email.go:72`) dieksekusi **sebelum** `client.Data()`/`w.Write(msg)` (`email.go:75-79`), dan `net/smtp` menolak CR/LF pada argumen `Mail`/`Rcpt` (`validateLine`) sebelum command dikirim — payload CRLF tidak pernah sampai ke wire; pengiriman gagal dan jalur gagal register menghapus user yang baru dibuat (`main.go:1337-1344`). Catatan hardening (bukan bug): validasi email register (`main.go:1200-1202`) hanya `strings.Contains("@")`/`Contains(".")` tanpa batas panjang — tambahkan `net/mail.ParseAddress` + cap panjang agar input aneh ditolak lebih awal.
- "EditUser menyimpan password mentah (`updates["password_hash"] = *body.Password`, `users.go:1452`)" — aman dua lapis: (1) guard `!isSuperAdminTarget` di titik yang sama memblok Operator menulis password target superadmin; (2) assignment mentah tersebut di-hash internal oleh lapis model — `updateUser` memanggil `HashPassword` untuk setiap nilai `password_hash` sebelum eksekusi SQL (`internal/models/user.go:897-908`, juga `UpdateUserField :840-848`), dan whitelist kolom `allowedUserColumns` (`:890-892`) sekaligus menutup SQLi di jalur yang sama.
- "`err.Error()` mentah di response voucher batch (`vouchers.go:375`)" — satu-satunya error yang sampai ke response adalah pesan buatan terkontrol dari `CreateBatchVouchers` (`internal/models/voucher.go:125-158`): pesan partial-batch berbahasa Indonesia ("hanya X dari Y voucher berhasil…"); error DB mentah dari `CreateVoucher` (di-wrap `:110`) ditelan ke `failed++` (`:146`) dan tidak pernah diteruskan; `count` dibatasi model maks 100 (`:129-131`). Route superadmin-only (`main.go:759-764`).
- "`err.Error()` mentah di endpoint test-SMTP (`settings.go:649`)" — `TestSMTPConnection` (`internal/helpers/email.go:123-178`) hanya mengembalikan pesan terkontrol (`:125`) atau wrap diagnostik koneksi (dial `:146`, banner `:155`, STARTTLS `:164`, auth `:173`) terhadap host:port yang admin itu sendiri ketik di form — informasi itu justru tujuan endpoint test-SMTP; route superadmin-only (`main.go:747`, route `:750`).
- "`err.Error()` mentah di ToggleUserStatus (`users.go:1701`)" — cabang `errors.Is(err, models.ErrPendingOTPToggleBlocked)` adalah sentinel yang memang sengaja di-surface dengan pesan terkontrol; error lain mendapat pesan generik. Bukan kebocoran.
- "Rollback ganda di cabang kuota-terlampaui upload ujian (`exams.go:478-479`)" — `_ = tx.Rollback(ctx)` kedua selalu mengembalikan `pgx.ErrTxClosed` (transaksi sudah ditutup oleh rollback pertama) dan hasilnya memang di-discard; tidak ada efek perilaku — transaksi tetap ter-rollback sekali, orphan R2 tetap dibersihkan. Baris mati untuk pembersihan trivial saja, bukan bug.
- "XSS tersimpan via `identity_data` (jalur async queue menyimpan data mentah tanpa `sanitizeMap`)" — `sanitizeMap` (`internal/handlers/api/exams.go:165-184`) memang bukan sanitasi HTML sejak awal: hanya `TrimSpace` + potong 200 karakter untuk nilai string (komentar `:170` eksplisit mengandalkan auto-escape html/template saat render) — kedua jalur persist (async: job mentah `enqueueSubmission :241-258` → pekerja queue → `upsertSubmissionRow :568` marshal verbatim; sinkron fallback: `sanitizeMap`) sama-sama menyimpan string ber-kemampuan-HTML, sehingga XSS ditentukan sepenuhnya di titik render. Ketiga titik render SUDAH ter-escape: `templates/public/hasil.html:782` `escapeHtml(String(val))` (label `:770` juga); `templates/admin/pengawas_detail.html:1775` `esc(key)`/`esc(String(...))` dengan `esc` = escaper DOM `createTextNode` (`:1671-1676`); `static/js/admin.js:4438` handler `identity-open` meng-escape label DAN nilai (`escapeHtml(label)` + `escapeHtml(v)`, fallback nama `:4445`). Transport ke popup juga aman: func template `json` (`cmd/server/main.go:267`) mengembalikan `string` biasa (bukan `template.HTML`/`template.JS`), sehingga html/template menerapkan escaping konteks-atribut pada `data-identity='{{json .IdentityData}}'` (`templates/admin/submissions.html:280`). Catatan minor (bukan vektor XSS): jalur async melewati normalisasi trim/200-karakter (panjang data inkonsisten antar jalur), dan nilai non-string (angka/array/objek) tidak dibatasi panjangnya di kedua jalur — pertimbangkan menormalkan di satu titik (persist) agar konsisten.
- "jobID hasil ujian bisa ditebak untuk membaca hasil siswa lain via Redis result key" — tidak terkonfirmasi: `generateJobID` (`api/exams.go:232-239`) membaca 8 byte dari `crypto/rand` (import `:4`) → 16 karakter hex = 64-bit entropy, tidak praktis untuk ditebak; fallback berbasis `UnixNano` hanya tercapai bila `rand.Read` error (crypto/rand praktis tidak pernah gagal di Linux). jobID memang dirancang sebagai rahasia per-submission (komentar `:1369-1376`) dan hanya dikembalikan ke perangkat pengirim; probing juga dibatasi rate limit per exam+MAC 60/menit + agregat per-exam 12000/menit (`:1295-1305`).
- "checkRateLimit gagal-terbuka saat Redis mati/error" — fail-open memang sengaja dan TERDOKUMENTASI di kode: komentar fungsi menyatakan "When Redis is unavailable the check is skipped (open access)" — baik `rdb == nil` (deployment tanpa Redis → jalur submit sinkron, `:215-217`) maupun error runtime Redis (`:221-223`) di-skip agar ujian tidak pernah terblokir oleh infra sekunder. Rate limit di sini adalah mitigasi abuse, bukan gerbang otorisasi — tiap endpoint peserta punya gerbang token/approval sendiri (X-Exam-Token `:779`, `:1000`, `:1319`, `:1517`, `:1633`; approval gate PDF; `examtoken.Matches` di CompleteExam `:1677`). (`internal/handlers/api/exams.go:212-227`)
- "Handler Actions mati (terdaftar di registry tapi tak pernah dipicu)" — NOL dari 117 nama terdaftar. Diff formal dua arah (nama `window.Actions.register` di static JS + inline template vs nama `data-action` di literal template + string HTML dinamis JS) menghasilkan tiga kandidat — `apps-retry-load`, `page-goto`, `toggle-score-detail` — dan ketiganya TIDAK MATI: pola pemakaian kelima, `el.setAttribute('data-action', 'name')` pada elemen yang dibuat via `document.createElement` (di luar jangkauan grep literal/string-HTML), justru menghidupkan semuanya: `apps-retry-load` (`static/js/settings-system-apps.js:44`, tombol retry di-append ke grid, registrasi `:549`), `app-delete` (`:148`, registrasi `:550`, juga hidup ganda via literal `templates/admin/settings.html:1833`), `toggle-score-detail` (`templates/public/hasil.html:605` per-`<tr>`, registrasi `:391`), `page-goto` (`:908` tombol paginasi, registrasi `:395`). Enumerasi lengkap `setAttribute('data-action'…)` produksi hanya 4 situs itu (sisanya fixture test `uiux-batch7-*.test.mjs`), `dataset.action` dipakai nol tempat — titik buta diff formal adalah pola setAttribute itu sendiri. Pelajaran audit: grep nama aksi wajib menyertakan `setAttribute('data-action'` (dua gaya kutip) + `dataset.action` di static JS DAN inline script template.
- "q.key/q.answer & jawaban mentah siswa bocor ke pengunjung publik pada /hasil/:token" — TIDAK: kunci+jawaban soal dihapus server-side untuk non-berhak (`internal/handlers/public/hasil.go:445-451`); jawaban mentah hanya disertakan bila show_answers aktif / viewer login (`:436-441` + `answers,omitempty` `:361`) — model entitlement ini DIKUNCI TEST `TestHasilAPIAnswersPrivacy` (`internal/handlers/public/hasil_test.go:250-276`: show_answers=0 → `answers` nil untuk anonim, `evaluated_answers` tetap ada); `evaluated_answers` memang selalu dikirim tapi hanya berisi earned/statusText/statusClass (`internal/models/submission.go:72-77`) — status benar/salah per soal memang publik by design (komentar `hasil.go:436-438`, `templates/public/hasil.html:725-727`); tab-hiding klien (`hasil.html:482-486`) didukung gating server nyata. Konsekuensi desain (bukan bug): skor + status per soal tiap siswa bernama tetap terlihat pemegang token — guru perlu menimbang granularity ini saat memilih public_results/show_answers.
- "Login, forgot-password, dan resend-OTP rentan enumerasi username (pesan diferensial / timing)" — TIDAK: ketiganya sengaja anti-enumerasi. Login memakai pesan seragam "Username atau password salah" untuk user-tidak-ditemukan dan password-salah (`internal/models/user.go:1378`, `:1384`) plus dummy bcrypt `_ = CheckPassword(password, dummyCompareHash)` di jalur no-rows (`:1377`) agar timing identik (var `:1358`, komentar desain `:1349-1355`) — pesan berbeda hanya muncul SETELAH password benar (suspen `:1387-1389`, pending-OTP `:1391-1393`, memang by design). Forgot-password lookup silent (`cmd/server/auth_recovery.go:213-215`), OTP hanya benar-benar dikirim bila akun aktif + ada email + SMTP terkonfigurasi + bebas cooldown (`:218-220`) dan pengirimannya async agar tak ada timing side-channel (`:226-230`), selalu redirect netral 302 ke halaman reset (`:240`, komentar desain `:203-206`). Resend-OTP memakai closure `uniform()` (definisi + komentar desain `auth_recovery.go:91-98`) yang mengembalikan HTTP 200 + pesan identik untuk SEMUA hasil — pool kosong (`:102`), user tidak ada / non-pending (`:117`, SELECT silent `:113-115`), cooldown (`:123`), SMTP/email kosong (`:129`), UPDATE gagal (`:138`), sukses (`:150`) — plus kirim async "so response latency is identical for all cases (no timing side-channel)" (`:142-148`). (Enumerasi yang benar-benar ada — oracle status register/confirm dan pesan kedaluwarsa reset-password yang dibedakan — sudah dicatat sebagai temuan A/B/D pada bagian REVIEW ALUR PUBLIK di atas.)
- "XSS injeksi via interpolasi template di halaman publik — `var USERNAME = "{{.username}}";` (register_confirm.html:260) menyuntik nilai query-string user mentah ke dalam blok `<script>`" — TIDAK: Gin merender lewat html/template yang menerapkan context-aware auto-escaping — aksi di dalam JS string literal melewati jsStrEscaper (`"` → `"`, `<` → `<`, `/` → `\/`, karakter kontrol → `\uXXXX`), sehingga payload tidak bisa menutup string JS maupun memecah keluar blok via `</script>`; aksi di dalam atribut URL dilewati dua lapis (percent-encode komponen query + HTML-escape attribute). Sink yang tampak seperti XSS pada batch halaman publik terakhir semuanya terverifikasi aman sesuai konteksnya: `action="/register/confirm?username={{.username}}"` (register_confirm.html:211) dan `action="/reset-password?username={{.username}}"` (reset_password.html:66) — URL-context; `var USERNAME = "{{.username}}"` (register_confirm.html:260) — JS-string context, dan nilai hasilnya hanya dikonsumsi aman (`encodeURIComponent(USERNAME)` :384, kunci sessionStorage :355 — tidak pernah menyentuh innerHTML); `value="{{.form_username}}"`/`value="{{.form_email}}"` (register.html:242/:253) dan `data-sitekey="{{.turnstile_site_key}}"` (register.html:294, reset_password.html:114) — attribute-context; download.html:469 `{{ .android_app.Name }}` — text-context, :476 `data-size-bytes="{{ .SizeBytes }}"` — attribute-context, :479/:503/:645/:724 `href="/download/app/{{ .ID }}"` — URL-path-context; `{{.version}}` (index.html:7) — text-context; shared.html `{{.seo_title}}` (:105, elemen title), `{{.seo_description}}`/`{{.seo_keywords}}` (:106-107), `{{.og_title}}`/`{{.og_description}}`/`{{.og_image}}` (:120-123), `{{.footer_text}}`/`{{.footer_tagline}}` (:927-928), serta admin/login.html `value="{{.next}}"` (:71), `{{.csrf_token}}` (:27/:70), `{{.seo_description}}` (:18), `{{.turnstile_site_key}}` (:90) — semuanya text/attribute-context ter-escape; `?v={{.version}}` pada semua link asset — URL-query-context. Halaman download.html keseluruhan (953 baris) juga bersih temuan baru: hash platform diresolve lewat allowlist `VALID_PLATFORMS` (:784-785, :831-834) — bukan vektor DOM-XSS, ukuran file diisi via textContent (S84 :849-855), tidak ada data admin/pribadi yang dirender ke halaman publik itu, dan guard double-click di `downloadApp` (:905-952) memang UX-only — rate limit server `/download/app/:id` 60/menit (temuan E) tetap yang berlaku.

## Catatan umum
- (Diperbarui 13 Sep 2026 — koreksi fakta) Klaim lama "cakupan test hampir nol (hanya `internal/services/examtoken`)" sudah usang: enumerasi langsung repo menemukan 73 file `*_test.go` — `internal/handlers` 48 (admin 37, api 8, public 2, r2 1), `internal/models` 10, `cmd/server` 6, `internal/queue` 3, `internal/middleware` 3, `internal/websocket` 1, `internal/services/examtoken` 1, `internal/database` 1. Isi mayoritas file belum dibaca dalam review ini (hanya keberadaan/distribusi yang diverifikasi) — saran lama tetap relevan sebagai bahan audit: pastikan di antara yang ada memang terdapat test otorisasi/tenancy & jalur pembayaran.
- Pola akar berulang: **otorisasi tenant hanya di sebagian jalur** (single-path aman, bulk/export/WS bolong) dan **escaping tak konsisten** (`esc`/`escapeHtml` vs `jsEscape`/mentah). Sentralisasikan helper `checkExamOwnership` di semua endpoint ber-`exam_id`, dan satu helper escaping untuk semua render dinamis.
- Kontrak konfirmasi hasil antrean tidak berfungsi di jalur auto-submit/recovery klien Android: `awaitQueuedResultDurable` (`android/app/src/main/java/com/examvan/app/helper/SubmissionManager.kt:513-522`, path relatif root EXAMVAN) meng-hardcode `jobId = null`, padahal server meng-gate KEDUA lookup hasil pada `jobID != ""` (Redis `internal/handlers/api/exams.go:1346`, fallback DB `:1377`), sehingga poll otomatis selalu menerima `"pending"` (`:1392-1397`) sampai deadline 75 detik habis (`QueuedResultPolling.kt:22-31`) dan dianggap gagal meskipun submission sebenarnya sudah durable di server — jalur manual tidak terkena karena meneruskan `result.jobId` (`SubmissionManager.kt:340`). Efek yang terlihat di klien: layar pemulihan "Submit sebelumnya TIDAK tuntas" (`ExamViewerActivity.kt:183-198`) dan jawaban lokal dipertahankan; re-entry menawarkan kirim ulang, sementara pembacaan hasil mengambil yang terbaru (`ORDER BY created_at DESC`, `internal/models/submission.go:522-542`). Kontrak job-id-sebagai-rahasia memang disengaja dan terdokumentasi (`api/exams.go:1369-1376`); klien perlu meneruskan jobId pada poll otomatis agar kontrak itu benar-benar berfungsi. Poller `/result` adalah aplikasi Android (`cmd/server/main.go:542`) — web tidak pernah memanggilnya.
- Kopling format `identity_data` rapuh lintas-platform: klien Android membangun JSON via org.json sesuai urutan field form (`ServerConfigActivity.kt:341-364`), meneruskannya mentah sebagai string extra (`WaitingApprovalActivity.kt:107` → `:453` → `ExamViewerActivity.kt:173`), dan mengirim string mentah itu saat poll hasil (`ApiClient.kt:627-628`) — sedangkan server menyimpan ulang melalui `json.Marshal` map Go di KEDUA jalur persist (async `api/exams.go:1115` → `internal/queue/submission_queue.go:568`; sinkron `:939-946` → `:1166`) yang mengurutkan kunci secara alfabetis. Klausa `identity_data = $3` di `GetLatestSubmissionByIdentity` (`internal/models/submission.go:522-542`) membandingkan string JSON secara eksak, sehingga tidak akan cocok kecuali urutan field kebetulan alfabetis — yang praktis "bekerja" hanyalah cabang OR `($3 = '' OR …)` yang cocok dengan submission mana pun pada exam+MAC tersebut. Tidak ada dampak keamanan (gerbang job_id+MAC `api/exams.go:1377` membatasi ke perangkat pengirim sendiri), tapi fallback DB ini praktis dekoratif lintas-format — normalisasikan ke satu bentuk kanonik (marshal ulang di titik persist) agar perbandingan menjadi deterministik.
- Indikator kehadiran peserta bisa dimanipulasi sesama peserta satu ujian: `CompleteExam` menghapus key heartbeat `heartbeat:%d:%s` (`api/exams.go:1685-1689`) memakai MAC dari body request (self-asserted) + token ujian yang dibagi per-exam, sehingga pemegang token mana pun bisa mem-flip indikator offline siswa lain dalam ujian yang sama hanya dengan menyertakan MAC target — satu panggilan cukup (heartbeat TTL 30 detik), dan rate limit per exam+MAC (`:1657-1661`) terhitung pada bucket MAC korban sehingga tidak mencegahnya. Dampak terbatas pada UI kehadiran: tidak menyentuh penilaian maupun akses lintas-exam (gerbang `examtoken.Matches` `:1677` tetap membatasi ke exam yang benar). Jika indikator offline dipakai pengawas untuk pengambilan keputusan, pertimbangkan mengikat operasi heartbeat ke MAC yang telah terverifikasi `exam_approvals`, bukan klaim body.
