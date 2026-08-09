//go:build ignore

// One-off operational script: upload the freshly built 2.4.1 APK to Cloudflare
// R2 and point the system_apps android record (id=4) at the new file, so the
// R2 label matches the real BuildConfig.VERSION_NAME (bump 2.2.0 -> 2.4.1).
//
// Run from webui/:  go run cmd/release_apk/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func loadEnv(path string) map[string]string {
	env := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		env[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	return env
}

func main() {
	env := loadEnv(".env")

	// Prefer the real environment over .env so the script can run inside the
	// compose network (container). In that network DATABASE_URL must come from
	// the environment (host .env has no DATABASE_URL — the DB is internal).
	envVal := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return env[key]
	}
	accessKey := envVal("R2_ACCESS_KEY_ID")
	secretKey := envVal("R2_SECRET_ACCESS_KEY")
	endpoint := envVal("R2_ENDPOINT")
	bucket := envVal("R2_BUCKET")
	dbURL := envVal("DATABASE_URL")
	if accessKey == "" || secretKey == "" || endpoint == "" || bucket == "" || dbURL == "" {
		fmt.Println("FATAL: R2_* / DATABASE_URL belum lengkap (env atau .env)")
		os.Exit(1)
	}

	r2c := r2.NewClient(accessKey, secretKey, endpoint, bucket)
	if r2c == nil || !r2c.Enabled() {
		fmt.Println("FATAL: R2 client gagal init")
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("FATAL: DB connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// 1. Baca record android lama (id 4)
	app, err := models.GetSystemAppByID(ctx, pool, 4)
	if err != nil {
		fmt.Printf("FATAL: GetSystemAppByID(4): %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Record lama: id=%d name=%s platform=%s version=%s\n  file=%s\n  size=%d\n",
		app.ID, app.Name, app.Platform, app.Version, app.FilePath, app.SizeBytes)

	// 2. Upload APK student baru (versionName 2.4.1) ke R2
	apkPath := "../android/app/build/outputs/apk/student/debug/app-student-debug.apk"
	f, err := os.Open(apkPath)
	if err != nil {
		fmt.Printf("FATAL: buka APK: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	st, _ := f.Stat()
	fmt.Printf("APK baru: %s (%d bytes)\n", apkPath, st.Size())

	newKey := fmt.Sprintf("apps/android/2.4.1/%s-%d", filepath.Base(apkPath), time.Now().UnixNano())
	if err := r2c.UploadWithContentType(ctx, newKey, f, "application/vnd.android.package-archive"); err != nil {
		fmt.Printf("FATAL: upload R2: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Upload OK: %s\n", newKey)

	// 3. Update record id=4: versi tetap 2.4.1, file & ukuran baru
	oldKey := app.FilePath
	app.Version = "2.4.1"
	app.FilePath = newKey
	app.SizeBytes = st.Size()
	// Tidak ada model.UpdateSystemApp; pakai SQL langsung
	_, err = pool.Exec(ctx,
		`UPDATE system_apps SET version=$1, file_path=$2, size_bytes=$3, updated_at=NOW() WHERE id=$4`,
		app.Version, app.FilePath, app.SizeBytes, app.ID)
	if err != nil {
		fmt.Printf("FATAL: update DB: %v\n", err)
		_ = r2c.Delete(ctx, newKey)
		os.Exit(1)
	}
	fmt.Println("DB update OK — record id=4 kini menunjuk APK 2.4.1 baru.")

	// 4. Hapus file R2 lama (supaya tidak ada duplikasi label 2.4.1)
	if err := r2c.Delete(ctx, oldKey); err != nil {
		fmt.Printf("WARN: hapus file R2 lama gagal: %v\n", err)
	} else {
		fmt.Printf("File R2 lama dihapus: %s\n", oldKey)
	}
}
