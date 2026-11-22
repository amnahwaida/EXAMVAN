package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// startExamCore enforces the schedule requirements and the concurrent-exam
// quota, then marks the exam started. It is shared by the manual StartExam
// handler and the background schedule job, so an automatic start can never
// bypass a rule the manual path enforces.
//
// Returns an HTTP status + user-facing message; 200 with an empty message
// means the exam was started.
func startExamCore(ctx context.Context, pool *pgxpool.Pool, exam *models.Exam, isSuper bool) (int, string) {
	if exam == nil {
		return http.StatusNotFound, "Ujian tidak ditemukan"
	}
	if exam.ExamStartedAt != nil {
		return http.StatusBadRequest, "Ujian sudah dimulai"
	}
	if exam.StartTime == nil {
		return http.StatusBadRequest, "Tanggal dan jam mulai wajib diisi sebelum memulai pengawasan"
	}
	if exam.EndTime == nil {
		return http.StatusBadRequest, "Tanggal dan jam selesai wajib diisi sebelum memulai pengawasan"
	}

	examID := exam.ID
	started := false
	if !isSuper {
		owner, err := models.GetUserByID(ctx, pool, exam.CreatedBy)
		if err == nil {
			gate := examQuotaGate(ctx, pool, exam.CreatedBy)
			poolMaxConcurrent, poolActive := gate.maxConcurrent, gate.active
			// School-pool sub-accounts: when the pool is active it is the
			// account's ONLY concurrent quota (schoolPoolCovers), so the
			// per-account max_concurrent_exams must not gate below it.
			// Operators skip their own column only while the pool is
			// ACTIVE; with no pool (personal bucket, legacy school) the
			// operator's own column binds too. The quota follows the exam
			// OWNER, not the acting user, so a delegated start and an
			// automatic start spend the same budget.
			ownerIsOperator := models.HasRole(owner.Role, models.RoleOperator)
			perUserLimit := (!ownerIsOperator || !poolActive) && !(owner.OperatorCreated && poolActive) && owner.MaxConcurrentExams > 0
			poolConcActive := poolActive && poolMaxConcurrent > 0

			if perUserLimit || poolConcActive {
				tx, err := pool.Begin(ctx)
				if err != nil {
					log.Printf("start begin tx error: %v", err)
					return http.StatusInternalServerError, "Gagal memulai ujian"
				}
				defer func() { _ = tx.Rollback(ctx) }()

				if poolConcActive {
					// Lock every account row the gate counts: concurrent
					// starts by ANY account sharing the budget serialize on
					// the shared budget.
					if err := gate.lockRows(ctx, tx); err != nil {
						log.Printf("start lock school pool error: %v", err)
						return http.StatusInternalServerError, "Gagal memulai ujian"
					}
					running, err := gate.countRunning(ctx, tx, examID)
					if err == nil && running >= int(poolMaxConcurrent) {
						_ = tx.Rollback(ctx)
						return http.StatusForbidden,
							fmt.Sprintf("Batas ujian serentak sekolah tercapai. Maksimal %d ujian sekolah dapat berjalan bersamaan.", poolMaxConcurrent)
					}
				}
				if perUserLimit {
					var locked int
					var lockedRole string
					if err := tx.QueryRow(ctx, `SELECT max_concurrent_exams, COALESCE(role, '') FROM admin_users WHERE id = $1 FOR UPDATE`, exam.CreatedBy).Scan(&locked, &lockedRole); err != nil {
						log.Printf("start lock owner error: %v", err)
						return http.StatusInternalServerError, "Gagal memulai ujian"
					}
					// An operator owner's own starts spend the family budget
					// together with its sub-accounts.
					var running int
					var rerr error
					if models.HasRole(lockedRole, models.RoleOperator) {
						running, rerr = models.CountRunningExamsByFamily(ctx, tx, exam.CreatedBy, examID)
					} else {
						running, rerr = models.CountRunningExams(ctx, tx, exam.CreatedBy, examID)
					}
					if rerr == nil && running >= locked {
						_ = tx.Rollback(ctx)
						return http.StatusForbidden,
							fmt.Sprintf("Batas ujian serentak tercapai. Maksimal %d ujian dapat berjalan bersamaan.", locked)
					}
				}

				if err := models.StartExamTx(ctx, tx, examID); err != nil {
					log.Printf("start exam tx error: %v", err)
					return http.StatusInternalServerError, "Gagal memulai ujian"
				}
				// Saat mulai ujian, gunakan token permanen sebagai active_token awal.
				// Untuk mode dynamic, token ini baru akan di-rotate/regenerasi setelah interval waktu terlewati.
				if err := models.UpdateExamActiveTokenTx(ctx, tx, examID, exam.Token); err != nil {
					log.Printf("start exam: set active token to permanent error: %v", err)
				}
				if err := tx.Commit(ctx); err != nil {
					log.Printf("start commit error: %v", err)
					return http.StatusInternalServerError, "Gagal memulai ujian"
				}
				started = true
			}
		}
	}

	if !started {
		if err := models.StartExam(ctx, pool, examID); err != nil {
			log.Printf("start exam error: %v", err)
			return http.StatusInternalServerError, "Gagal memulai ujian"
		}

		// Saat mulai ujian, gunakan token permanen sebagai active_token awal.
		// Untuk mode dynamic, token ini baru akan di-rotate/regenerasi setelah interval waktu terlewati.
		if err := models.UpdateExamActiveToken(ctx, pool, examID, exam.Token); err != nil {
			log.Printf("start exam: set active token to permanent error: %v", err)
		}
	}

	return http.StatusOK, ""
}
