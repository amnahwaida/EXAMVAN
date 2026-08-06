package admin

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/examvan/webui/internal/models"
)

// PackagesPage renders the GET /admin/packages quota configuration page for
// SuperAdmin.
func PackagesPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := getCurrentUserRole(c)
		if role != models.RoleSuperAdmin {
			c.Redirect(http.StatusFound, "/admin/dashboard")
			return
		}

		data := gin.H{
			"title":       "Pengaturan Paket",
			"active_page": "packages",
			"admin_user":  getCurrentUsername(c),
			"admin_role":  role,
		}

		renderAdminPage(c, "admin/packages.html", data)
	}
}

// ListPackagesSettingsHandler handles GET /admin/api/packages (SuperAdmin
// only). Returns the editable quota rows for the fixed packages.
func ListPackagesSettingsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		settings, err := models.ListPackageSettings(c.Request.Context(), pool)
		if err != nil {
			log.Printf("list package settings error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat pengaturan paket")
			return
		}

		type item struct {
			Key        string  `json:"key"`
			Label      string  `json:"label"`
			MaxExams   int64   `json:"max_exams"`
			MaxPDFMB   float64 `json:"max_pdf_size_mb"`
			Concurrent int64   `json:"max_concurrent_exams"`
			MaxStoMB   float64 `json:"max_storage_mb"`
			Role       string  `json:"role"`
		}
		items := make([]item, 0, len(settings))
		for _, s := range settings {
			items = append(items, item{
				Key:        s.Key,
				Label:      s.Label,
				MaxExams:   s.MaxExams,
				MaxPDFMB:   roundTo(float64(s.MaxPDFSize)/(1024*1024), 1),
				Concurrent: s.MaxConcurrentExams,
				MaxStoMB:   roundTo(float64(s.MaxStorageSize)/(1024*1024), 2),
				Role:       s.Role,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"packages": items,
		})
	}
}

// SavePackageSettingsHandler handles POST /admin/api/packages (SuperAdmin
// only). Accepts a JSON body of package rows and upserts them all in one
// transaction.
func SavePackageSettingsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload struct {
			Packages []struct {
				Key          string  `json:"key"`
				Label        string  `json:"label"`
				MaxExams     int64   `json:"max_exams"`
				Concurrent   int64   `json:"max_concurrent_exams"`
				MaxPDFMB     float64 `json:"max_pdf_size_mb"`
				MaxStorageMB float64 `json:"max_storage_mb"`
				Role         string  `json:"role"`
			} `json:"packages"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data pengaturan paket tidak valid")
			return
		}
		if len(payload.Packages) == 0 {
			errorResponse(c, http.StatusBadRequest, "Tidak ada paket yang dikirim")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		dbTx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("save package settings begin tx error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan pengaturan paket")
			return
		}
		defer func() {
			_ = dbTx.Rollback(ctx)
		}()

		for _, p := range payload.Packages {
			key := strings.TrimSpace(p.Key)
			if !isPackageKey(key) {
				errorResponse(c, http.StatusBadRequest, "Paket tidak dikenal: "+key)
				return
			}
			label := strings.TrimSpace(p.Label)
			if label == "" {
				errorResponse(c, http.StatusBadRequest, "Nama paket tidak boleh kosong")
				return
			}
			if p.MaxExams < 1 {
				errorResponse(c, http.StatusBadRequest, "Jumlah ujian paket "+key+" minimal 1")
				return
			}
			concurrent := p.Concurrent
			if concurrent < 1 {
				concurrent = 1
			}
			if concurrent > p.MaxExams {
				concurrent = p.MaxExams
			}
			if p.MaxPDFMB < 1 {
				p.MaxPDFMB = 1
			}
			if p.MaxStorageMB < 1 {
				p.MaxStorageMB = 1
			}
			role, roleMsg := validPackageRole(p.Role)
			if roleMsg != "" {
				errorResponse(c, http.StatusBadRequest, roleMsg)
				return
			}

			if _, err := dbTx.Exec(ctx, `
				INSERT INTO package_settings
					(pkg_key, label, max_exams, max_pdf_size, max_concurrent_exams,
					 max_storage_size, role, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
				ON CONFLICT (pkg_key) DO UPDATE SET
					label = EXCLUDED.label,
					max_exams = EXCLUDED.max_exams,
					max_pdf_size = EXCLUDED.max_pdf_size,
					max_concurrent_exams = EXCLUDED.max_concurrent_exams,
					max_storage_size = EXCLUDED.max_storage_size,
					role = EXCLUDED.role,
					updated_at = CURRENT_TIMESTAMP`,
				key, label, p.MaxExams, int64(p.MaxPDFMB*1024*1024),
				concurrent, int64(p.MaxStorageMB*1024*1024), role); err != nil {
				log.Printf("save package setting error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan pengaturan paket")
				return
			}
		}

		if err := dbTx.Commit(ctx); err != nil {
			log.Printf("save package settings commit error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan pengaturan paket")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Pengaturan paket berhasil disimpan",
		})
	}
}

func isPackageKey(key string) bool {
	for _, k := range models.PackageSettingKeys {
		if k == key {
			return true
		}
	}
	return false
}

// validPackageRole validates a package role and returns its serialized form
// ("" when no role). Second return is a non-empty error message on failure.
func validPackageRole(role string) (string, string) {
	role = strings.TrimSpace(role)
	switch role {
	case "", models.RoleGuru, models.RolePengawas, models.RoleOperator:
		if role == "" {
			return "", ""
		}
		return models.SerializeRoles([]string{role}), ""
	default:
		return "", "Role paket tidak valid"
	}
}

// getPackageEntitlement returns (exams, pdfBytes, concurrent, storageBytes,
// role) for a fixed package, preferring the SuperAdmin-editable package_settings
// row when present and falling back to the built-in packageEntitlement defaults.
func getPackageEntitlement(ctx context.Context, q rowQuerier, pkg string) (int64, int64, int64, int64, string) {
	exams, pdf, concurrent, storage, role := packageEntitlement(pkg)
	err := q.QueryRow(ctx, `
		SELECT max_exams, max_pdf_size, max_concurrent_exams, max_storage_size,
		       COALESCE(role, '')
		FROM package_settings WHERE pkg_key = $1`, pkg).
		Scan(&exams, &pdf, &concurrent, &storage, &role)
	if err != nil {
		return packageEntitlement(pkg)
	}
	return exams, pdf, concurrent, storage, role
}

// rowQuerier abstracts a single-query source (pgx.Tx or pgxpool.Pool).
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
