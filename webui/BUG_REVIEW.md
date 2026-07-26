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
- **DITUNDA (butuh keputusan):** #14 `GET /api/exams` (endpoint mobile publik yang membocorkan ujian aktif lintas-tenant) — TIDAK diubah karena berisiko memutus aplikasi Android. Lihat catatan di bawah.

---

## 🔴 CRITICAL

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

## ✅ Ditolak setelah verifikasi (bukan bug)
- CSRF `!=` non-constant-time — token adalah milik sesi caller sendiri, tak ada oracle. (`csrf.go:81`)
- "Race duplikat pending DOKU" — sudah ada unique index parsial `idx_transactions_pending_doku_unique`. (`schema.sql:261`)

## Catatan umum
- Cakupan test hampir nol (hanya `internal/services/examtoken`). Tambahkan test untuk otorisasi/tenancy & jalur pembayaran.
- Pola akar berulang: **otorisasi tenant hanya di sebagian jalur** (single-path aman, bulk/export/WS bolong) dan **escaping tak konsisten** (`esc`/`escapeHtml` vs `jsEscape`/mentah). Sentralisasikan helper `checkExamOwnership` di semua endpoint ber-`exam_id`, dan satu helper escaping untuk semua render dinamis.
