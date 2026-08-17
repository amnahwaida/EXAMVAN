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

Audit lanjutan dari kebijakan "akun sub (dibuat operator) tidak boleh klaim voucher": memastikan tidak ada permukaan lain selain `/admin/billing` (yang sudah disesuaikan) yang menampilkan aksi klaim kepada akun sub. **Hasil: tidak ditemukan permukaan lain — tidak ada penyesuaian UI yang diperlukan.** Kontrak routing dikunci test `TestNoPublicVoucherRoutes` (tanpa database). Catatan lengkap di [README.md → Kebijakan Klaim Voucher Akun Sub](../README.md#kebijakan-klaim-voucher-akun-sub-dibuat-operator).

### A. Klien Android & desktop — tidak punya UI klaim voucher (0 kemunculan)
- Kedua klien (Android `api/ApiClient.kt`, desktop `examvan/api.py` + salinan `desktop/pkg-build/`) adalah aplikasi **ujian berbasis token**, bukan aplikasi manajemen akun. Di kode sumber klien (Java/Kotlin, layout & string `res/`, Python desktop, salinan `pkg-build/`) pencarian istilah `voucher`/`redeem`/`billing`/`claim`/`klaim` menghasilkan **0 kemunculan** (kata `paket` hanya muncul di skrip packaging `desktop/install.sh` dalam konteks manajer paket OS, bukan paket voucher).
- Endpoint yang dipanggil hanyalah rute ujian publik (`/api/health`, `/api/exams`, `/api/exams/request-approval`, `/api/exams/token/{token}`, `/api/exams/{exam_id}/pdf`, `/api/exams/{exam_id}/submit`). Endpoint claim/aktivasi (`/admin/api/vouchers/*`) bersifat **session-based admin** tanpa versi publik — klien token secara teknis pun tidak bisa memanggilnya.

### B. Permukaan web publik — bersih (0 referensi voucher)
- `templates/public/` (index, register, register_confirm, forgot_password, reset_password, hasil, download, shared), `templates/admin/login.html`, `internal/handlers/public/`, serta JS/CSS publik: **0 referensi** voucher/klaim/billing/paket. Satu-satunya kecocokan adalah teks lisensi `static/js/fingerprintjs.min.js` ("CLAIM, DAMAGES") — bukan kode aplikasi.

### C. Halaman admin lain yang terjangkau akun sub — sudah terkunci
- Dashboard/submissions/pengawas/system-apps: 0 referensi voucher. Satu-satunya halaman berisi form klaim adalah `/admin/billing` — sudah menyembunyikan form klaim + daftar "Paket yang Sudah Anda Klaim" untuk akun sub (kartu penjelasan "Akun Sub (Dibuat Operator)").
- Menu "Kelola Voucher"/"Pengaturan Paket" dibungkus `{{if $isSuper}}` di tab bar Pengaturan (`admin/partials/settings-tabs.html`); sejak 17 Agustus 2026 semua bagian settings pindah dari dropdown header `nav.html` ke **satu halaman `/admin/settings`** (`templates/admin/settings.html`) dengan tab client-side tanpa reload dan JS per-bagian dimuat lazy (`static/js/settings-*.js`); toggle "Redeem Kode Promo / Voucher" di panel "Kontrol Monetisasi" (`users.html`) hanya dirender di dalam panel SaaS `{{if eq .admin_role "superadmin"}}` — keduanya bukan aksi klaim per akun.

### D. Kontrak rute
- `TestNoPublicVoucherRoutes` (`cmd/server/routes_voucher_public_test.go`) menginspeksi tabel rute hasil `registerRoutes` asli: tidak ada rute ber-`voucher`/`redeem`/`activate` di luar prefix `/admin`, dan `POST /admin/api/vouchers/redeem`, `POST /admin/api/vouchers/activate`, `GET /admin/api/vouchers/mine` tetap terdaftar.

---

## ✅ PERBAIKAN — SETTING STORAGE DEFAULT PAKET PENDAFTARAN (9 Agustus 2026)

Gap ditemukan di halaman Kelola User: panel **"Default Paket Pendaftaran"** tidak punya pengaturan storage, padahal kunci **`default_max_storage_size`** sudah ada di `saas_settings` (default 50 MB = `52428800` byte) dan **sudah diterapkan** ke akun baru di `/register` (`cmd/server/main.go`, `MaxStorageSize`). Yang kurang hanyalah eksposnya: `GET/POST /admin/api/saas-settings` tidak memuat field-nya dan `users.html` tidak punya input — sehingga kuota storage akun baru terkunci 50 MB tanpa bisa diubah dari UI. Catatan lengkap di [README.md → Konfigurasi Default Paket Pendaftaran — Kuota Storage](../README.md#konfigurasi-default-paket-pendaftaran--kuota-storage-kapasitas-disk).

**Fix (end-to-end):**

- **`internal/handlers/admin/settings.go`** — GET kini mengembalikan `default_max_storage_size_mb` **dan** `storage_free_mb` (sisa disk partisi `STORAGE_PATH` via `getFreeDiskSpace`); POST menerima & menyimpan `default_max_storage_size_mb`.
- **Field disimpan bersyarat (pointer `*float64`):** bila `default_max_storage_size_mb` tidak dikirim (mis. UI lama ter-cache saat rollout), nilai tersimpan **tidak disentuh**. Ini penting karena `0` adalah nilai sah (tidak terbatas) — pola `<= 0 → reset ke default` yang dipakai field lain tidak bisa dipakai di sini, karena field absen tidak boleh mengubah quota yang sudah dikonfigurasi menjadi unlimited.
- **`templates/admin/users.html`** — input **"Maks Storage (MB)"** di panel Default Paket Pendaftaran (grid 5 kolom desktop, 2 kolom mobile) + hint sisa disk.
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

## ✅ Ditolak setelah verifikasi (bukan bug)
- CSRF `!=` non-constant-time — token adalah milik sesi caller sendiri, tak ada oracle. (`csrf.go:81`)
- "Race duplikat pending DOKU" — sudah ada unique index parsial `idx_transactions_pending_doku_unique`. (`schema.sql:261`)

## Catatan umum
- Cakupan test hampir nol (hanya `internal/services/examtoken`). Tambahkan test untuk otorisasi/tenancy & jalur pembayaran.
- Pola akar berulang: **otorisasi tenant hanya di sebagian jalur** (single-path aman, bulk/export/WS bolong) dan **escaping tak konsisten** (`esc`/`escapeHtml` vs `jsEscape`/mentah). Sentralisasikan helper `checkExamOwnership` di semua endpoint ber-`exam_id`, dan satu helper escaping untuk semua render dinamis.
