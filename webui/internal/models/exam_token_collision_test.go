package models

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// ---------------------------------------------------------------------------
// GetExamByToken sengaja melebar sampai exam_token_history supaya tautan hasil
// dengan token yang sudah dirotasi tetap resolve. Konsekuensinya: fungsi yang
// SAMA dipakai admin sebagai cek-tabrakan token kustom, sehingga token yang
// HANYA ada di history — token lama yang tidak akan pernah bisa di-join lagi
// — dilaporkan "sudah digunakan" dan guru tidak bisa memakainya.
//
// GetExamByLiveToken adalah saudara yang sempit: hanya token / active_token /
// previous_active_token, tanpa history. Inilah yang harus dipakai admin.
// GetExamByActiveToken (gate join) dan dua jalur hasil di public/hasil.go
// TIDAK boleh tersentuh.
// ---------------------------------------------------------------------------

// setupTokenLookupTestDB memakai schema terisolasi paket ini (lihat
// database.NewPackageTestPool), skip bila TEST_DATABASE_URL tidak di-set.
func setupTokenLookupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "models")
}

// insertTokenExam membuat satu baris exams minimal untuk uji lookup token,
// dimiliki guru yang dibuat fresh supaya FK exams_created_by terpenuhi.
func insertTokenExam(t *testing.T, pool *pgxpool.Pool, name string, token, activeToken, previousActiveToken *string) int {
	t.Helper()
	ctx := context.Background()
	owner := createAuthTestUser(t, pool, "guru-"+name, time.Now().UTC().Add(24*time.Hour))
	var id int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, previous_active_token, status, security_level, created_by)
		VALUES ($1, 'x.pdf', 1024, $2, $3, $4, 'active', 'medium', $5)
		RETURNING id`, name, token, activeToken, previousActiveToken, owner.ID).Scan(&id); err != nil {
		t.Fatalf("insert exam %s: %v", name, err)
	}
	return id
}

func tokenPtr(s string) *string { return &s }

// archiveToken menaruh token ke exam_token_history, meniru rotasi token dinamis.
func archiveToken(t *testing.T, pool *pgxpool.Pool, examID int, token string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO exam_token_history (exam_id, token) VALUES ($1, $2)`, examID, token); err != nil {
		t.Fatalf("insert token history %s: %v", token, err)
	}
}

// TestGetExamByLiveTokenIgnoresHistoryOnlyTokens: token yang HANYA tersimpan di
// exam_token_history harus dianggap BEBAS oleh cek-tabrakan admin — token itu
// sudah tidak bisa di-join siapa pun, jadi menolak token kustom hanya demi
// nilai yang sudah mati tanpa alasan.
func TestGetExamByLiveTokenIgnoresHistoryOnlyTokens(t *testing.T) {
	pool := setupTokenLookupTestDB(t)
	ctx := context.Background()

	const retired = "RETIRED1"
	id := insertTokenExam(t, pool, "Ujian Dinamis", tokenPtr("PERSIST1"), tokenPtr("ACTIVE01"), nil)
	archiveToken(t, pool, id, retired)

	// Token retired tetap resolve lewat GetExamByToken (tautan hasil).
	if _, err := GetExamByToken(ctx, pool, retired); err != nil {
		t.Fatalf("GetExamByToken(%s) = %v, want resolve lewat history", retired, err)
	}
	// Tapi cek-tabrakan admin harus menganggapnya bebas.
	got, err := GetExamByLiveToken(ctx, pool, retired)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetExamByLiveToken(%s) err = %v, want pgx.ErrNoRows — token yang hanya ada di "+
			"history tidak pernah bisa di-join lagi, jadi tidak boleh memblokir token kustom", retired, err)
	}
	if got.ID != 0 {
		t.Fatalf("GetExamByLiveToken(%s) = exam %d, want 0", retired, got.ID)
	}
}

// TestGetExamByLiveTokenStillRejectsLiveTokens adalah kontrol: token yang
// benar-benar hidup (token, active_token, previous_active_token) TETAP harus
// ditolak, kalau tidak uniqueness token jadi tidak berarti apa-apa.
func TestGetExamByLiveTokenStillRejectsLiveTokens(t *testing.T) {
	pool := setupTokenLookupTestDB(t)
	ctx := context.Background()

	id := insertTokenExam(t, pool, "Ujian Statis", tokenPtr("PERSIST1"), tokenPtr("ACTIVE01"), tokenPtr("PREV0001"))

	for _, token := range []string{"PERSIST1", "ACTIVE01", "PREV0001"} {
		got, err := GetExamByLiveToken(ctx, pool, token)
		if err != nil {
			t.Fatalf("GetExamByLiveToken(%s) err = %v, want baris yang cocok", token, err)
		}
		if got.ID != id {
			t.Fatalf("GetExamByLiveToken(%s) = exam %d, want %d", token, got.ID, id)
		}
	}
}

// TestGetExamByTokenIsCaseInsensitive (item 6) memakai helper yang sama.
func TestGetExamByTokenIsCaseInsensitive(t *testing.T) {
	pool := setupTokenLookupTestDB(t)
	ctx := context.Background()

	id := insertTokenExam(t, pool, "Ujian Huruf Campur", tokenPtr("mixedcase"), tokenPtr("MixedActive"), tokenPtr("MixedPrev"))

	// examtoken.Matches sudah memakai strings.EqualFold, jadi token yang
	// di-uppercase client sebelum dikirim harus tetap resolve.
	for _, probe := range []string{"MIXEDCASE", "mixedcase", "MiXeDcAsE"} {
		got, err := GetExamByToken(ctx, pool, probe)
		if err != nil {
			t.Fatalf("GetExamByToken(%s) err = %v, want baris yang cocok (EqualFold)", probe, err)
		}
		if got.ID != id {
			t.Fatalf("GetExamByToken(%s) = exam %d, want %d", probe, got.ID, id)
		}
	}
	for _, probe := range []string{"MIXEDACTIVE", "mixedactive", "MIXEDPREV", "mixedprev"} {
		if _, err := GetExamByToken(ctx, pool, probe); err != nil {
			t.Fatalf("GetExamByToken(%s) err = %v, want baris yang cocok (EqualFold)", probe, err)
		}
	}

	// History juga harus case-insensitive supaya tautan hasil lama tidak
	// bergantung pada kapitalisasi token.
	archiveToken(t, pool, id, "histtoken")
	for _, probe := range []string{"HISTTOKEN", "histtoken"} {
		if _, err := GetExamByToken(ctx, pool, probe); err != nil {
			t.Fatalf("GetExamByToken(%s via history) err = %v, want baris yang cocok", probe, err)
		}
	}
}

// TestGetExamByLiveTokenIsCaseInsensitive: cek-tabrakan admin memakai
// perbandingan yang sama seperti GetExamByToken, kalau tidak admin menulis
// token huruf kecil lalu melaporkan bentrok padahal client meng-uppercase
// token itu sebelum mengirimkannya.
func TestGetExamByLiveTokenIsCaseInsensitive(t *testing.T) {
	pool := setupTokenLookupTestDB(t)
	ctx := context.Background()

	id := insertTokenExam(t, pool, "Ujian Campur Dua", tokenPtr("mixedcase"), tokenPtr("MixedActive"), nil)

	for _, probe := range []string{"MIXEDCASE", "mixedcase"} {
		got, err := GetExamByLiveToken(ctx, pool, probe)
		if err != nil {
			t.Fatalf("GetExamByLiveToken(%s) err = %v, want baris yang cocok (EqualFold)", probe, err)
		}
		if got.ID != id {
			t.Fatalf("GetExamByLiveToken(%s) = exam %d, want %d", probe, got.ID, id)
		}
	}
}

// TestGetExamByActiveTokenStaysStrict (item 6): gate join TIDAK boleh ikut
// melebar — hanya active_token, dan tetap strict (history tidak diikutkan).
func TestGetExamByActiveTokenStaysStrict(t *testing.T) {
	pool := setupTokenLookupTestDB(t)
	ctx := context.Background()

	id := insertTokenExam(t, pool, "Ujian Gate", tokenPtr("PERSIST1"), tokenPtr("ACTIVE01"), tokenPtr("PREV0001"))
	archiveToken(t, pool, id, "RETIRED1")

	if got, err := GetExamByActiveToken(ctx, pool, "ACTIVE01"); err != nil || got.ID != id {
		t.Fatalf("GetExamByActiveToken(ACTIVE01) = (%d, %v), want (%d, nil)", got.ID, err, id)
	}
	for _, token := range []string{"PERSIST1", "PREV0001", "RETIRED1"} {
		if _, err := GetExamByActiveToken(ctx, pool, token); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("GetExamByActiveToken(%s) err = %v, want pgx.ErrNoRows — gate join harus strict", token, err)
		}
	}
}
