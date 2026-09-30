package models

import (
	"context"
	"fmt"
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
func hasSubmittedAnswersFilter() string {
	return "COALESCE(NULLIF(BTRIM(answers_json), ''), '') <> ''"
}

// Pencocokan memakai exam_number dulu, karena itu yang paling stabil dan
// paling unik di kelas. Hanya kalau kolomnya kosong untuk semua baris
// submissions pada ujian ini, baru jatuh ke student_name -- supaya siswa
// dengan nomor kosong tidak tertukar dengan siswa bernama sama.
func HasSubmissionForStudentKey(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) (bool, error) {
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

// HasRepeatGrant melapor apakah izin mengulang sudah diberikan.
func HasRepeatGrant(
	ctx context.Context, pool *pgxpool.Pool, examID int, studentKey string,
) (bool, error) {
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
