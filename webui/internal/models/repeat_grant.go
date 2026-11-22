package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RepeatGrant adalah izin mengulang yang diberikan pengawas untuk satu
// siswa pada satu ujian.
type RepeatGrant struct {
	ExamID     int       `json:"exam_id"`
	StudentKey string    `json:"student_key"`
	GrantedBy  string    `json:"granted_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// HasSubmissionForStudentKey melaporkan apakah siswa dengan kunci tersebut
// SUDAH MENGIRIM jawaban untuk ujian ini.
//
// Yang dihitung hanya baris dengan answers_json terisi. Baris kosong TIDAK
// dihitung, dan itu bukan hal kecil: EnsureFreshSubmissionOnApproval
// membuat baris kosong begitu perangkat disetujui, supaya halaman
// monitoring bisa menampilkan siswa yang sedang mengerjakan. Kalau baris
// kosong ikut dihitung, polling kedua dari ujian yang SEDANG BERLANGSUNG
// akan terbaca sebagai "sudah mengulang" dan siswa terkunci di tengah ujian.
// SubmittedAnswersFilterSQL adalah predikat SQL "submission ini punya
// jawaban nyata". Diekspor karena request-approval perlu mengecek definisi
// yang SAMA di dalam ON CONFLICT DO UPDATE (di bawah row lock), dan
// duplikasi string itu gampang melenceng.
//
// answers_json = '{}' / '[]' adalah payload kosong yang tetap ditulis
// client saat auto-submit tidak punya jawaban. Itu BUKAN attempt: kalau
// dihitung, siswa yang ter-auto-submit tanpa menjawab apa pun terkunci
// "sudah mengerjakan" padahal tidak ada jawaban yang perlu dilindungi.
const SubmittedAnswersFilterSQL = `COALESCE(NULLIF(BTRIM(answers_json), ''), '') NOT IN ('', '{}', '[]', 'null')`

func hasSubmittedAnswersFilter() string { return SubmittedAnswersFilterSQL }

// Pencocokan memakai exam_number dulu, karena itu yang paling stabil dan
// paling unik di kelas. Hanya kalau kolomnya kosong untuk semua baris
// submissions pada ujian ini, baru jatuh ke student_name -- supaya siswa
// dengan nomor kosong tidak tertukar dengan siswa bernama sama.
func HasSubmissionForStudentKey(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) (bool, error) {
	// Normalisasi DI DALAM fungsi, bukan di pemanggil. T kolomnya sudah
	// LOWER(), jadi kalau argumennya "N01" dan kolomnya "n01", keduanya
	// tidak akan cocok -- dan pemanggil yang lupa menormalkan akan mendapat
	// "belum submit" lalu memberi izin yang sebenarnya tidak perlu.
	// Client sudah selalu mengirim lowercase, jadi bug ini tidak muncul dari
	// jalur produksi; tapi ia muncul begitu endpoint admin dipanggil langsung
	// (test TestHasSubmissionMatchesCaseInsensitively menangkapnya).
	studentKey = strings.ToLower(strings.TrimSpace(studentKey))
	if studentKey == "" {
		return false, nil
	}
	var byNumber int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions
		 WHERE exam_id = $1 AND LOWER(BTRIM(exam_number)) = $2 AND `+hasSubmittedAnswersFilter(),
		examID, studentKey,
	).Scan(&byNumber); err != nil {
		return false, fmt.Errorf("count submissions by number: %w", err)
	}
	if byNumber > 0 {
		return true, nil
	}

	// Nomor kosong untuk semua submission di ujian ini? Ujian yang memang
	// tidak mengumpulkan nomor, jadi kunci berarti nama.
	var withNumber int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND BTRIM(exam_number) <> ''`,
		examID,
	).Scan(&withNumber); err != nil {
		return false, fmt.Errorf("count submissions with number: %w", err)
	}
	if withNumber > 0 {
		// Ada nomor di ujian ini, tapi tidak milik siswa ini -> belum submit.
		return false, nil
	}
	var byName int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM submissions
		 WHERE exam_id = $1 AND LOWER(BTRIM(student_name)) = $2 AND `+hasSubmittedAnswersFilter(),
		examID, studentKey,
	).Scan(&byName); err != nil {
		return false, fmt.Errorf("count submissions by name: %w", err)
	}
	return byName > 0, nil
}

// openRepeatAttemptSQL detects an open repeat attempt for a student:
// $1 = exam_id, $2 = student_key. The approval bookkeeping transaction reuses
// it to consume a one-shot repeat grant atomically with the new open row.
const openRepeatAttemptSQL = `EXISTS (
	WITH keyed AS (
		SELECT
			LOWER(BTRIM(COALESCE(exam_number, ''))) AS number_key,
			LOWER(BTRIM(COALESCE(student_name, ''))) AS name_key,
			(answers_json IS NULL OR BTRIM(answers_json) = '') AS is_open,
			(BTRIM(COALESCE(start_time, '')) <> '') AS has_start,
			created_at,
			` + SubmittedAnswersFilterSQL + ` AS is_completed
		FROM submissions
		WHERE exam_id = $1
	),
	has_numbers AS (
		SELECT EXISTS(SELECT 1 FROM keyed WHERE number_key <> '') AS any_number
	),
	matched AS (
		SELECT * FROM keyed
		WHERE (number_key = $2 AND (SELECT any_number FROM has_numbers))
		   OR (name_key = $2 AND NOT (SELECT any_number FROM has_numbers))
	),
	latest_completed AS (
		SELECT MAX(created_at) AS ts FROM matched WHERE is_completed
	)
	SELECT 1 FROM matched m, latest_completed lc
	WHERE m.is_open AND m.has_start AND lc.ts IS NOT NULL
	  AND m.created_at > lc.ts
)`

// HasOpenRepeatAttempt melaporkan apakah siswa punya baris attempt yang masih
// terbuka (belum dijawab) SETELAH attempt selesai terakhirnya. Baris itu
// adalah penanda bahwa pengawas sudah menyetujui repeat yang sedang berjalan.
// Setelah siswa submit, tidak ada lagi baris terbuka setelah attempt terakhir,
// sehingga permintaan repeat berikutnya kembali masuk antrean pending — satu
// approval untuk satu attempt, bukan izin permanen.
//
// Pencocokan memakai aturan yang sama dengan HasSubmissionForStudentKey:
// nomor ujian dulu, fallback nama hanya bila tidak ada satu pun submission di
// ujian itu yang punya nomor.
func HasOpenRepeatAttempt(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) (bool, error) {
	studentKey = strings.ToLower(strings.TrimSpace(studentKey))
	if studentKey == "" {
		return false, nil
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT `+openRepeatAttemptSQL,
		examID, studentKey).Scan(&exists); err != nil {
		return false, fmt.Errorf("has open repeat attempt: %w", err)
	}
	return exists, nil
}

// SubmittedStudentKeys mengembalikan himpunan kunci siswa yang punya minimal
// satu submission BERJAWABAN untuk ujian ini, memakai aturan pencocokan yang
// sama dengan HasSubmissionForStudentKey (nomor dulu; fallback nama hanya bila
// tidak ada satu pun submission di ujian itu yang punya nomor).
//
// Dipakai halaman pengawas untuk menandai baris perangkat yang status
// terbarunya masih "mengerjakan" (placeholder NULL) tetapi siswa tersebut
// sudah punya attempt selesai di baris/perangkat lain. Tanpa flag ini tombol
// "Izinkan Mengulang" hanya muncul saat baris terbaru submitted, sehingga
// supervisor tidak punya cara melepas siswa yang terblokir.
func SubmittedStudentKeys(
	ctx context.Context, pool *pgxpool.Pool, examID int,
) (map[string]bool, error) {
	var anyWithNumber bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM submissions
			WHERE exam_id = $1 AND BTRIM(COALESCE(exam_number, '')) <> ''
		)`, examID,
	).Scan(&anyWithNumber); err != nil {
		return nil, fmt.Errorf("check submissions with number: %w", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT DISTINCT BTRIM(COALESCE(exam_number, '')), BTRIM(COALESCE(student_name, ''))
		 FROM submissions
		 WHERE exam_id = $1 AND `+hasSubmittedAnswersFilter(),
		examID,
	)
	if err != nil {
		return nil, fmt.Errorf("list submitted student keys: %w", err)
	}
	defer rows.Close()

	keys := make(map[string]bool)
	for rows.Next() {
		var number, name string
		if err := rows.Scan(&number, &name); err != nil {
			return nil, fmt.Errorf("scan submitted student key: %w", err)
		}
		key := number
		if key == "" {
			if anyWithNumber {
				continue
			}
			key = name
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			keys[key] = true
		}
	}
	return keys, rows.Err()
}

// HasRepeatGrant melapor apakah izin mengulang sudah diberikan.
func HasRepeatGrant(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) (bool, error) {
	// Sama seperti HasSubmissionForStudentKey: normalisasi di dalam, supaya
	// GRANT dan CHECK tidak bisa strolling ke kunci yang berbeda hanya
	// karena beda huruf besar-kecil di pemanggil.
	studentKey = strings.ToLower(strings.TrimSpace(studentKey))
	if studentKey == "" {
		return false, nil
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM exam_repeat_grants WHERE exam_id = $1 AND student_key = $2)`,
		examID, studentKey,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("has repeat grant: %w", err)
	}
	return exists, nil
}

// GrantRepeat memberi izin mengulang. Idempoten: memberi izin dua kali
// tidak membuat baris ganda dan tidak menggeser created_at.
func GrantRepeat(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey, grantedBy string,
) error {
	studentKey = strings.ToLower(strings.TrimSpace(studentKey))
	if studentKey == "" {
		return fmt.Errorf("student key kosong")
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO exam_repeat_grants (exam_id, student_key, granted_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (exam_id, student_key)
		 DO UPDATE SET granted_by = EXCLUDED.granted_by`,
		examID, studentKey, grantedBy,
	)
	if err != nil {
		return fmt.Errorf("grant repeat: %w", err)
	}
	return nil
}

// RevokeRepeat mencabut izin mengulang. Aman kalau izinnya memang tidak ada.
func RevokeRepeat(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) error {
	studentKey = strings.ToLower(strings.TrimSpace(studentKey))
	if studentKey == "" {
		return fmt.Errorf("student key kosong")
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM exam_repeat_grants WHERE exam_id = $1 AND student_key = $2`,
		examID, studentKey,
	); err != nil {
		return fmt.Errorf("revoke repeat: %w", err)
	}
	return nil
}

// ListRepeatGrants mengembalikan seluruh izin yang masih berlaku untuk satu
// ujian, supaya halaman pengawas bisa menandai tombol mana yang sudah aktif.
func ListRepeatGrants(
	ctx context.Context, pool *pgxpool.Pool, examID int,
) ([]RepeatGrant, error) {
	rows, err := pool.Query(ctx,
		`SELECT exam_id, student_key, granted_by, created_at
		 FROM exam_repeat_grants WHERE exam_id = $1 ORDER BY student_key`,
		examID,
	)
	if err != nil {
		return nil, fmt.Errorf("list repeat grants: %w", err)
	}
	defer rows.Close()

	out := []RepeatGrant{}
	for rows.Next() {
		var g RepeatGrant
		if err := rows.Scan(&g.ExamID, &g.StudentKey, &g.GrantedBy, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repeat grant: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
