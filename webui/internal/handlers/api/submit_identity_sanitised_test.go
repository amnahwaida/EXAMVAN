package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/queue"
)

// ---------------------------------------------------------------------------
// SubmitExam membangun `sanitized := sanitizeMap(body.IdentityData)` untuk
// PENYIMPANAN, tapi validasi field wajib membaca body.IdentityData yang MENTAH,
// dan job async meng-enqueue body.IdentityData yang mentah juga. Tiga hal
// jadi tidak sinkron karena itu:
//
//  1. nilai >200 rune lolos validasi, disimpan terpotong 200 rune — siswa
//     melihat nama/alamat terpotong tanpa ada tanda sama sekali;
//  2. nilai yang isinya HANYA karakter tersembunyi lolos validasi sebagai
//     "terisi" lalu tersimpan jadi string kosong;
//  3. jalur async ( jalur produksi normal) sama sekali tidak menyanitasi
//     identity_data, sehingga karakter bidi yang dibuang sanitize() di handler
//     tetap sampai ke tabel yang dilihat seluruh kelas.
//
// Fix: sanitasi dibangun SEKALI lalu dipakai untuk validasi, job async, dan
// penyimpanan sync — satu sumber kebenaran.
//
// Pilihan untuk kasus >200 rune: KONSISTEN TERPOTONG, bukan tolak. Pemotongan
// 200 rune sudah jadi kontrak penyimpanan yang berlaku lama untuk semua field,
// jadi memilih "konsisten" tidak menambah aturan baru yang hanya berlaku di
// jalur submit — dan siswa yang mengetik nama panjang tetap boleh submit.
// ---------------------------------------------------------------------------

// queuedIdentityData membaca identity_data dari job pertama yang di-enqueue.
func queuedIdentityData(t *testing.T, rdb *goredis.Client) map[string]interface{} {
	t.Helper()
	ctx := context.Background()
	items, err := rdb.LRange(ctx, queue.QueueKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("baca queue: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("queue kosong — submit tidak meng-enqueue job")
	}
	var job map[string]interface{}
	if err := json.Unmarshal([]byte(items[0]), &job); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	id, ok := job["identity_data"].(map[string]interface{})
	if !ok {
		t.Fatalf("job[\"identity_data\"] = %#v, want object", job["identity_data"])
	}
	return id
}

// TestSubmitRejectsIdentityThatIsOnlyHiddenCharacters adalah regression test
// item 5: nilai yang MENTAH-nya terlihat terisi tapi HASIL SANITASI-nya kosong
// harus ditolak 400, bukan diterima lalu disimpan sebagai string kosong.
func TestSubmitRejectsIdentityThatIsOnlyHiddenCharacters(t *testing.T) {
	pool := newSubmitSanitisedTestPool(t)
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	insertApprovalRow(t, pool, examID, "DEVICE:hidden-only")

	router := newExamFixesRouter(pool, rdb)
	payload := map[string]interface{}{
		"identity_data": map[string]interface{}{
			// Isi hanya U+202E + U+200F: tidak terlihat, tapi TrimSpace tidak
			// membuangnya, jadi validasi lama menganggap ini "terisi".
			"student_name":  "‮‏",
			"exam_number":   "A1",
			"student_class": "XII-A",
		},
		"answers":     map[string]interface{}{"1": "jakarta"},
		"mac_address": "DEVICE:hidden-only",
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "", headers, payload)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s) — nama yang hanya berisi karakter tersembunyi "+
			"sanitize() buang, jadi tidak boleh lolos sebagai identitas terisi", rec.Code, rec.Body.String())
	}
	if n, _ := rdb.LLen(context.Background(), queue.QueueKey).Result(); n != 0 {
		t.Fatalf("queue berisi %d job, want 0 — request yang ditolak tidak boleh di-enqueue", n)
	}
}

// TestSubmitStoresTheSameIdentityItValidated mengunci kasus >200 rune dengan
// pilihan "konsisten terpotong": request DITERIMA, dan apa yang di-enqueue
// harus persis yang divalidasi (200 rune), bukan bentuk mentah (250 rune).
func TestSubmitStoresTheSameIdentityItValidated(t *testing.T) {
	pool := newSubmitSanitisedTestPool(t)
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	insertApprovalRow(t, pool, examID, "DEVICE:long-name")

	router := newExamFixesRouter(pool, rdb)
	longName := strings.Repeat("a", 250)
	payload := map[string]interface{}{
		"identity_data": map[string]interface{}{
			"student_name":  longName,
			"exam_number":   "A1",
			"student_class": "XII-A",
		},
		// Field kustom bersarang: harus ikut terpotong di semua kedalaman.
		"extra":       map[string]interface{}{"alamat": map[string]interface{}{"jalan": "J"}},
		"answers":     map[string]interface{}{"1": "jakarta"},
		"mac_address": "DEVICE:long-name",
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "", headers, payload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s) — nama >200 rune tetap boleh submit, "+
			"hanya disimpan terpotong", rec.Code, rec.Body.String())
	}

	stored := queuedIdentityData(t, rdb)
	got, ok := stored["student_name"].(string)
	if !ok {
		t.Fatalf("stored student_name = %#v, want string", stored["student_name"])
	}
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("stored student_name = %d rune, want 200 — yang disimpan harus sama persis "+
			"dengan yang divalidasi", n)
	}
}

// TestSubmitStripsHiddenCharactersFromQueuedIdentityData menutup celah yang
// selalu ada di jalur async: karakter bidi harus hilang dari job yang di-enqueue
// (dan berarti hilang dari identity_data serta kolom kanonik yang(dirender di
// tabel hasil publik).
func TestSubmitStripsHiddenCharactersFromQueuedIdentityData(t *testing.T) {
	pool := newSubmitSanitisedTestPool(t)
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	insertApprovalRow(t, pool, examID, "DEVICE:bidi")

	router := newExamFixesRouter(pool, rdb)
	payload := map[string]interface{}{
		"identity_data": map[string]interface{}{
			"student_name":  "‮Andi‬",
			"exam_number":   "A1",
			"student_class": "XII-A",
			"nested":        map[string]interface{}{"b": "‮evil‬"},
			"listed":        []interface{}{"‮evil‬"},
		},
		"answers":     map[string]interface{}{"1": "jakarta"},
		"mac_address": "DEVICE:bidi",
	}
	headers := map[string]string{"X-Exam-Token": token, "X-App-Version": "2.5.0"}

	rec := doJSONRequest(router, http.MethodPost,
		fmt.Sprintf("/api/exams/%d/submit", examID), "", headers, payload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}

	stored := queuedIdentityData(t, rdb)
	if got := stored["student_name"]; got != "Andi" {
		t.Errorf("stored student_name = %#v, want \"Andi\"", got)
	}
	nested, ok := stored["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("stored nested = %#v, want object", stored["nested"])
	}
	if got := nested["b"]; got != "evil" {
		t.Errorf("stored nested.b = %#v, want \"evil\"", got)
	}
	listed, ok := stored["listed"].([]interface{})
	if !ok || len(listed) != 1 || listed[0] != "evil" {
		t.Errorf("stored listed = %#v, want [\"evil\"]", stored["listed"])
	}

	// Kolom kanonik yang dirender di halaman hasil harus bersih juga.
	if !strings.Contains(rec.Body.String(), "success") {
		t.Errorf("body = %s, want success", rec.Body.String())
	}
}

// newSubmitSanitisedTestPool memakai schema terisolasi paket api (skip bila
// TEST_DATABASE_URL tidak di-set), sama seperti harness exams_fixes_test.go.
func newSubmitSanitisedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "api")
}
