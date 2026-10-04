// Izin mengulang per siswa, diberikan dari halaman pengawas.
//
// Default: siswa yang sudah mengirim jawaban tidak bisa mengulang. Endpoint
// inilah yang melepasnya — dan mencabutnya kembali.
//
// Mengikuti pola SetApprovalStatus: authorization per-ujian lewat
// UserCanAccessExam, dan keputusan hanya sah saat ujian masih aktif. Tanpa
// dua hal itu, operator bisa melepas izin untuk ujian yang sudah selesai
// dan jejaknya tidak bisa ditindaklanjuti siapa pun.
package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
)

type repeatGrantRequest struct {
	StudentKey   string                 `json:"student_key"`
	StudentName  string                 `json:"student_name"`
	ExamNumber   string                 `json:"exam_number"`
	StudentClass string                 `json:"student_class"`
	IdentityData map[string]interface{} `json:"identity_data"`
}

// resolveRepeatStudentKey menentukan student_key dari payload. Diprioritaskan
// field yang dikirim UI (sudah berupa kunci), lalu identity_data, lalu kolom
// standar -- urutan yang sama dengan client.
func resolveRepeatStudentKey(req repeatGrantRequest) string {
	if k := helpers.StudentKey(req.StudentKey, "", ""); k != "" {
		return k
	}
	if k := helpers.StudentKeyFromIdentityData(
		req.IdentityData, req.ExamNumber, req.StudentName, req.StudentClass,
	); k != "" {
		return k
	}
	return helpers.StudentKey(req.ExamNumber, req.StudentName, req.StudentClass)
}

// authorizeRepeatExam mengembalikan true kalau pemanggil berhak mengambil
// keputusan untuk ujian ini, dan sudah menulis respons 4xx kalau tidak.
func authorizeRepeatExam(c *gin.Context, examID int) bool {
	pool := getPool(c)
	ctx := c.Request.Context()
	exam, err := models.GetExamByID(ctx, pool, examID)
	if err != nil {
		errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
		return false
	}
	if !models.UserCanAccessExam(
		ctx, pool, getCurrentUserID(c), isSuperAdmin(c), examID) {
		errorResponse(c, http.StatusForbidden,
			"Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
		return false
	}
	if !exam.IsActive() {
		errorResponse(c, http.StatusBadRequest,
			"Ujian tidak aktif — izin mengulang tidak dapat diubah")
		return false
	}
	if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
		errorResponse(c, http.StatusBadRequest,
			"Waktu ujian telah berakhir — izin mengulang tidak dapat diubah")
		return false
	}
	return true
}

// GrantStudentRepeat memberi izin mengulang untuk satu siswa.
func GrantStudentRepeat() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}
		var req repeatGrantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			errorResponse(c, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		studentKey := resolveRepeatStudentKey(req)
		if studentKey == "" {
			errorResponse(c, http.StatusBadRequest,
				"Identitas siswa tidak terbaca — izin mengulang tidak bisa diberikan")
			return
		}
		if !authorizeRepeatExam(c, examID) {
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()
		submitted, err := models.HasSubmissionForStudentKey(
			ctx, pool, examID, studentKey)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError,
				"Gagal memeriksa jawaban siswa")
			return
		}
		if !submitted {
			// Izin untuk siswa yang belum mengirim jawaban tidak ada
			// artinya, dan hanya akan mengotori tabel izin.
			errorResponse(c, http.StatusBadRequest,
				"Siswa ini belum mengirim jawaban, jadi tidak perlu izin mengulang")
			return
		}
		if err := models.GrantRepeat(
			ctx, pool, examID, studentKey, strconv.Itoa(getCurrentUserID(c))); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan izin")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true, "exam_id": examID,
			"student_key": studentKey, "granted": true,
		})
	}
}

// RevokeStudentRepeat mencabut izin mengulang. Aman kalau memang tidak ada.
func RevokeStudentRepeat() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}
		studentKey := c.Param("student_key")
		if studentKey == "" {
			errorResponse(c, http.StatusBadRequest, "student_key kosong")
			return
		}
		if !authorizeRepeatExam(c, examID) {
			return
		}
		if err := models.RevokeRepeat(
			c.Request.Context(), getPool(c), examID, studentKey); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal mencabut izin")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true, "exam_id": examID,
			"student_key": studentKey, "granted": false,
		})
	}
}

// ListStudentRepeats mengembalikan izin yang berlaku untuk satu ujian, supaya
// tombol di halaman pengawas bisa menandai mana yang sudah aktif.
//
// Authorization: the same exam-access predicate as grant/revoke
// (UserCanAccessExam) — without it any authenticated account of any tenant
// could enumerate another exam's student keys. Deliberately NOT the full
// authorizeRepeatExam: the active-exam requirement there is for MUTABLE
// decisions, while this read must also render on ended exams (history view).
func ListStudentRepeats() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}
		pool := getPool(c)
		ctx := c.Request.Context()
		if _, err := models.GetExamByID(ctx, pool, examID); err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, getCurrentUserID(c), isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden,
				"Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}
		grants, err := models.ListRepeatGrants(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar izin")
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "grants": grants})
	}
}
