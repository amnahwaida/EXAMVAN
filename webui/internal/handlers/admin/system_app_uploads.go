package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/models"
)

// Chunked upload contract for large system-app artifacts. A single ~100 MB
// request through Cloudflare can exceed the fixed ~100s origin timeout before
// the server ever gets a chance to send response heartbeats. Splitting the file
// into small requests keeps every body transfer well below that limit; the
// finalize request (which uploads the assembled temp file to R2) has no body,
// so the existing streamed heartbeat helper can keep Cloudflare alive.
const (
	systemAppUploadChunkSize         = 2 * 1024 * 1024
	systemAppUploadMaxBytes          = 500 * 1024 * 1024
	systemAppUploadTTL               = 24 * time.Hour
	systemAppUploadFinalizeThreshold = 20 * time.Second
)

var (
	systemAppUploadIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	errSystemAppUploadGone   = errors.New("system app upload not found")
)

type systemAppUploadMeta struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Platform    string    `json:"platform"`
	Version     string    `json:"version"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Key         string    `json:"key"`
	Size        int64     `json:"size"`
	ChunkSize   int64     `json:"chunk_size"`
	TotalChunks int       `json:"total_chunks"`
	CreatedAt   time.Time `json:"created_at"`
}

type initSystemAppUploadRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type systemAppUploadIDRequest struct {
	UploadID string `json:"upload_id"`
}

func systemAppUploadRoot(c *gin.Context) string {
	storageRoot := filepath.Join(getStoragePath(c), "tmp", "system-app-uploads")
	if err := os.MkdirAll(storageRoot, 0755); err == nil {
		return storageRoot
	}
	// Existing Docker volumes are often root-owned while the server runs as
	// appuser. Chunk temp files are disposable, so fall back to the container's
	// writable /tmp instead of failing the upload on storage permissions.
	fallback := filepath.Join(os.TempDir(), "examvan-system-app-uploads")
	if err := os.MkdirAll(fallback, 0755); err == nil {
		return fallback
	}
	return storageRoot
}

func systemAppUploadDir(c *gin.Context, id string) (string, error) {
	if !systemAppUploadIDPattern.MatchString(id) {
		return "", errSystemAppUploadGone
	}
	return filepath.Join(systemAppUploadRoot(c), id), nil
}

func systemAppUploadPartPath(dir string, index int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%06d", index))
}

func newSystemAppUploadID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func writeSystemAppUploadMeta(dir string, meta systemAppUploadMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}

func loadSystemAppUploadMeta(c *gin.Context, id string) (systemAppUploadMeta, string, error) {
	dir, err := systemAppUploadDir(c, id)
	if err != nil {
		return systemAppUploadMeta{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if os.IsNotExist(err) {
		return systemAppUploadMeta{}, dir, errSystemAppUploadGone
	}
	if err != nil {
		return systemAppUploadMeta{}, dir, err
	}
	var meta systemAppUploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return systemAppUploadMeta{}, dir, fmt.Errorf("meta rusak: %w", err)
	}
	if meta.ID != id {
		return systemAppUploadMeta{}, dir, errSystemAppUploadGone
	}
	return meta, dir, nil
}

func cleanupStaleSystemAppUploads(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > systemAppUploadTTL {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func contentTypeForSystemAppPlatform(platform string) string {
	switch platform {
	case "android":
		return "application/vnd.android.package-archive"
	case "windows":
		return "application/x-msdownload"
	case "linux":
		return "application/x-executable"
	default:
		return "application/octet-stream"
	}
}

func systemAppUploadFilename(name string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "app"
	}
	if len(name) > 160 {
		name = name[len(name)-160:]
	}
	return name
}

func isSystemAppPlatform(platform string) bool {
	switch platform {
	case "android", "windows", "linux":
		return true
	default:
		return false
	}
}

func systemAppMagicValid(platform string, header []byte) bool {
	switch platform {
	case "android":
		return len(header) >= 4 && header[0] == 0x50 && header[1] == 0x4B && header[2] == 0x03 && header[3] == 0x04
	case "windows":
		return len(header) >= 2 && header[0] == 0x4D && header[1] == 0x5A
	case "linux":
		return len(header) >= 4 && header[0] == 0x7F && header[1] == 0x45 && header[2] == 0x4C && header[3] == 0x46
	default:
		return false
	}
}

func systemAppChunkExpectedSize(meta systemAppUploadMeta, index int) (int64, error) {
	if index < 0 || index >= meta.TotalChunks {
		return 0, fmt.Errorf("indeks potongan di luar rentang")
	}
	if index == meta.TotalChunks-1 {
		size := meta.Size - int64(index)*meta.ChunkSize
		if size <= 0 || size > meta.ChunkSize {
			return 0, fmt.Errorf("ukuran potongan terakhir tidak valid")
		}
		return size, nil
	}
	return meta.ChunkSize, nil
}

func validateSystemAppUploadParts(dir string, meta systemAppUploadMeta) (int64, error) {
	var total int64
	var firstHeader []byte
	for i := 0; i < meta.TotalChunks; i++ {
		partPath := systemAppUploadPartPath(dir, i)
		info, err := os.Stat(partPath)
		if err != nil {
			return 0, fmt.Errorf("potongan %d belum lengkap", i)
		}
		expected, err := systemAppChunkExpectedSize(meta, i)
		if err != nil {
			return 0, err
		}
		if info.Size() != expected {
			return 0, fmt.Errorf("ukuran potongan %d tidak sesuai", i)
		}
		total += info.Size()
		if i == 0 {
			f, err := os.Open(partPath)
			if err != nil {
				return 0, err
			}
			firstHeader = make([]byte, 4)
			n, _ := io.ReadFull(f, firstHeader)
			_ = f.Close()
			firstHeader = firstHeader[:n]
		}
	}
	if total != meta.Size {
		return 0, fmt.Errorf("total ukuran file tidak sesuai")
	}
	if !systemAppMagicValid(meta.Platform, firstHeader) {
		return 0, fmt.Errorf("format file tidak valid untuk platform yang dipilih")
	}
	return total, nil
}

func openSystemAppUploadParts(dir string, meta systemAppUploadMeta) ([]io.Reader, []*os.File, int64, error) {
	readers := make([]io.Reader, 0, meta.TotalChunks)
	files := make([]*os.File, 0, meta.TotalChunks)
	var total int64
	for i := 0; i < meta.TotalChunks; i++ {
		partPath := systemAppUploadPartPath(dir, i)
		f, err := os.Open(partPath)
		if err != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			return nil, nil, 0, err
		}
		files = append(files, f)
		readers = append(readers, f)
		info, err := f.Stat()
		if err != nil {
			for _, opened := range files {
				_ = opened.Close()
			}
			return nil, nil, 0, err
		}
		total += info.Size()
	}
	return readers, files, total, nil
}

func closeSystemAppUploadParts(files []*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// InitSystemAppUpload creates a resumable upload session and reserves the R2
// key. The browser then posts small raw chunks to UploadSystemAppChunk.
func InitSystemAppUpload() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		pool := getPool(c)

		r2Val, exists := c.Get("r2")
		r2 := r2client.FromContext(r2Val)
		if !exists || r2 == nil || !r2.Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error_code": r2client.ErrCodeNotConfigured, "message": r2client.ErrMsgNotConfigured})
			return
		}

		var req initSystemAppUploadRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Data unggahan tidak valid."})
			return
		}

		name := strings.TrimSpace(req.Name)
		platform := strings.ToLower(strings.TrimSpace(req.Platform))
		version := models.NormalizeAppVersion(req.Version)
		if name == "" || version == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Semua field harus diisi."})
			return
		}
		if !isSystemAppPlatform(platform) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Platform aplikasi tidak valid."})
			return
		}
		if req.Size <= 0 || req.Size > systemAppUploadMaxBytes {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Ukuran file tidak valid atau melebihi 500 MB."})
			return
		}

		existsApp, err := models.CheckSystemAppExists(ctx, pool, name, platform, version)
		if err != nil {
			log.Printf("admin: check system app duplicate failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal memeriksa aplikasi."})
			return
		}
		if existsApp {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Aplikasi dengan nama, platform, dan versi ini sudah ada."})
			return
		}

		id, err := newSystemAppUploadID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membuat sesi unggahan."})
			return
		}

		root := systemAppUploadRoot(c)
		cleanupStaleSystemAppUploads(root)
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("admin: create system app upload dir failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyiapkan penyimpanan sementara."})
			return
		}

		filename := systemAppUploadFilename(req.Filename)
		totalChunks := int((req.Size + systemAppUploadChunkSize - 1) / systemAppUploadChunkSize)
		meta := systemAppUploadMeta{
			ID:          id,
			Name:        name,
			Platform:    platform,
			Version:     version,
			Filename:    filename,
			ContentType: contentTypeForSystemAppPlatform(platform),
			Key:         fmt.Sprintf("apps/%s/%s/%s-%d", platform, version, filename, time.Now().UnixNano()),
			Size:        req.Size,
			ChunkSize:   systemAppUploadChunkSize,
			TotalChunks: totalChunks,
			CreatedAt:   time.Now().UTC(),
		}
		if err := writeSystemAppUploadMeta(dir, meta); err != nil {
			_ = os.RemoveAll(dir)
			log.Printf("admin: write system app upload meta failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyiapkan sesi unggahan."})
			return
		}

		log.Printf("admin: system app upload init id=%s name=%q platform=%s version=%s size=%d chunks=%d", id, name, platform, version, req.Size, totalChunks)
		c.JSON(http.StatusOK, gin.H{
			"success":      true,
			"upload_id":    id,
			"chunk_size":   systemAppUploadChunkSize,
			"total_chunks": totalChunks,
		})
	}
}

// UploadSystemAppChunk stores one raw chunk. The body is exactly the slice's
// size, so Cloudflare sees a short request instead of the whole installer.
func UploadSystemAppChunk() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Query("id")
		index, err := strconv.Atoi(c.Query("index"))
		if err != nil || index < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Indeks potongan tidak valid."})
			return
		}

		meta, dir, err := loadSystemAppUploadMeta(c, id)
		if errors.Is(err, errSystemAppUploadGone) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Sesi unggahan tidak ditemukan atau sudah kedaluwarsa."})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membaca sesi unggahan."})
			return
		}

		expected, err := systemAppChunkExpectedSize(meta, index)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		if c.Request.ContentLength >= 0 && c.Request.ContentLength != expected {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Ukuran potongan file tidak sesuai."})
			return
		}

		partPath := systemAppUploadPartPath(dir, index)
		tmpPath := partPath + ".tmp"
		f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			log.Printf("admin: open system app chunk failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyimpan potongan file."})
			return
		}
		written, copyErr := io.Copy(f, io.LimitReader(c.Request.Body, expected+1))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmpPath)
			log.Printf("admin: write system app chunk failed: %v", copyErr)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyimpan potongan file."})
			return
		}
		if written != expected {
			_ = os.Remove(tmpPath)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Ukuran potongan file tidak sesuai."})
			return
		}
		if err := os.Rename(tmpPath, partPath); err != nil {
			_ = os.Remove(tmpPath)
			log.Printf("admin: commit system app chunk failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyimpan potongan file."})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// CompleteSystemAppUpload verifies every chunk, uploads the assembled object to
// R2, and creates the DB row. The R2 upload runs behind streamed heartbeats.
func CompleteSystemAppUpload() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		pool := getPool(c)

		r2Val, exists := c.Get("r2")
		r2 := r2client.FromContext(r2Val)
		if !exists || r2 == nil || !r2.Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error_code": r2client.ErrCodeNotConfigured, "message": r2client.ErrMsgNotConfigured})
			return
		}

		var req systemAppUploadIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Data unggahan tidak valid."})
			return
		}

		meta, dir, err := loadSystemAppUploadMeta(c, req.UploadID)
		if errors.Is(err, errSystemAppUploadGone) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Sesi unggahan tidak ditemukan atau sudah kedaluwarsa."})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membaca sesi unggahan."})
			return
		}

		total, err := validateSystemAppUploadParts(dir, meta)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		if total != meta.Size {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Total ukuran file tidak sesuai."})
			return
		}

		existsApp, err := models.CheckSystemAppExists(ctx, pool, meta.Name, meta.Platform, meta.Version)
		if err != nil {
			log.Printf("admin: check system app duplicate before finalize failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal memeriksa aplikasi."})
			return
		}
		if existsApp {
			_ = os.RemoveAll(dir)
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Aplikasi dengan nama, platform, dan versi ini sudah ada."})
			return
		}

		log.Printf("admin: system app upload finalize id=%s key=%q size=%d", meta.ID, meta.Key, meta.Size)
		respondSlowOperation(c, systemAppUploadFinalizeThreshold, func(opCtx context.Context) (int, gin.H) {
			readers, files, total, err := openSystemAppUploadParts(dir, meta)
			if err != nil {
				log.Printf("admin: open system app parts id=%s failed: %v", meta.ID, err)
				return http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membaca file sementara."}
			}
			defer closeSystemAppUploadParts(files)
			if total != meta.Size {
				return http.StatusBadRequest, gin.H{"success": false, "message": "Total ukuran file tidak sesuai."}
			}

			if err := r2.UploadWithContentType(opCtx, meta.Key, io.MultiReader(readers...), meta.ContentType); err != nil {
				log.Printf("admin: upload system app key=%q size=%d failed: %v", meta.Key, meta.Size, err)
				return http.StatusInternalServerError, gin.H{"success": false, "error_code": r2client.ErrCodeUploadFailed, "message": r2client.ErrMsgUploadFailed}
			}

			app := &models.SystemApp{
				Name:      meta.Name,
				Platform:  meta.Platform,
				Version:   meta.Version,
				FilePath:  meta.Key,
				SizeBytes: meta.Size,
			}
			if err := models.CreateSystemApp(opCtx, pool, app); err != nil {
				if err := r2.Delete(opCtx, meta.Key); err != nil {
					log.Printf("admin: cleanup orphan R2 object %s failed: %v", meta.Key, err)
				}
				return http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyimpan metadata aplikasi."}
			}

			if err := os.RemoveAll(dir); err != nil {
				log.Printf("admin: cleanup system app upload dir %s failed: %v", dir, err)
			}
			log.Printf("admin: system app upload complete id=%s key=%q", meta.ID, meta.Key)
			return http.StatusOK, gin.H{"success": true, "message": "Aplikasi berhasil diunggah."}
		})
	}
}

// CancelSystemAppUpload discards a partially uploaded session.
func CancelSystemAppUpload() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req systemAppUploadIDRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Data unggahan tidak valid."})
			return
		}
		dir, err := systemAppUploadDir(c, req.UploadID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Sesi unggahan tidak valid."})
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("admin: cancel system app upload %s failed: %v", req.UploadID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membatalkan unggahan."})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}
