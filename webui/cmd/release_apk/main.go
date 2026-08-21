//go:build ignore

// Operational release script: upload freshly built EXAMVAN Android APKs
// (student + kiosk flavors) to Cloudflare R2 and point the system_apps rows
// at the new files, then sync the android_version saas_setting so the
// version gate requires the newly released version.
//
// Usage (run from webui/, needs R2_*/DATABASE_URL in env or .env):
//   go run cmd/release_apk/main.go -list
//   go run cmd/release_apk/main.go -version 2.7.2 \
//     -student-apk ../android/app/build/outputs/apk/student/release/app-student-release.apk \
//     -kiosk-apk ../android/app/build/outputs/apk/kiosk/release/app-kiosk-release.apk
package main

import (
	"context"
	"flag"
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
	version := flag.String("version", "", "versi rilis APK (mis. 2.7.2)")
	studentApk := flag.String("student-apk", "", "path APK flavor student")
	kioskApk := flag.String("kiosk-apk", "", "path APK flavor kiosk (opsional)")
	listOnly := flag.Bool("list", false, "hanya cetak isi system_apps + android_version")
	flag.Parse()

	env := loadEnv(".env")
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

	apps, err := models.GetAllSystemApps(ctx, pool)
	if err != nil {
		fmt.Printf("FATAL: GetAllSystemApps: %v\n", err)
		os.Exit(1)
	}
	if *listOnly {
		fmt.Println("system_apps saat ini:")
		for _, a := range apps {
			fmt.Printf("  id=%d name=%q platform=%s version=%s size=%d file=%s\n",
				a.ID, a.Name, a.Platform, a.Version, a.SizeBytes, a.FilePath)
		}
		av := models.GetSaasSettingWithDefault(ctx, pool, models.SettingAndroidVersion, models.DefaultSettings[models.SettingAndroidVersion])
		fmt.Printf("saas_setting android_version = %q\n", av)
		return
	}

	if *version == "" || *studentApk == "" {
		fmt.Println("FATAL: -version dan -student-apk wajib (lihat header file ini)")
		os.Exit(1)
	}

	// Cari row system_apps: student = nama persis "EXAMVAN"; kiosk = nama
	// mengandung "kiosk". Row yang belum ada dibuat baru.
	findRow := func(nameMatch func(string) bool) *models.SystemApp {
		for i := range apps {
			if apps[i].Platform == "android" && nameMatch(strings.ToLower(apps[i].Name)) {
				return &apps[i]
			}
		}
		return nil
	}

	nameFor := func(flavor string) string {
		if flavor == "kiosk" {
			return "EXAMVAN Kiosk"
		}
		return "EXAMVAN"
	}

	publish := func(app *models.SystemApp, apkPath, flavor string) error {
		f, err := os.Open(apkPath)
		if err != nil {
			return fmt.Errorf("buka APK: %w", err)
		}
		defer f.Close()
		st, _ := f.Stat()
		fmt.Printf("APK %s baru: %s (%d bytes)\n", flavor, apkPath, st.Size())

		newKey := fmt.Sprintf("apps/android/%s/%s-%d", *version, filepath.Base(apkPath), time.Now().UnixNano())
		if err := r2c.UploadWithContentType(ctx, newKey, f, "application/vnd.android.package-archive"); err != nil {
			return fmt.Errorf("upload R2: %w", err)
		}
		fmt.Printf("Upload OK %s: %s\n", flavor, newKey)

		oldKey := ""
		if app == nil {
			app = &models.SystemApp{Name: nameFor(flavor), Platform: "android", Version: *version, FilePath: newKey, SizeBytes: st.Size()}
			if err := models.CreateSystemApp(ctx, pool, app); err != nil {
				_ = r2c.Delete(ctx, newKey)
				return fmt.Errorf("insert system_app: %w", err)
			}
			fmt.Printf("Row baru system_app id=%d name=%q\n", app.ID, app.Name)
		} else {
			oldKey = app.FilePath
			app.Version = *version
			app.FilePath = newKey
			app.SizeBytes = st.Size()
			if _, err := pool.Exec(ctx,
				`UPDATE system_apps SET version=$1, file_path=$2, size_bytes=$3, updated_at=NOW() WHERE id=$4`,
				app.Version, app.FilePath, app.SizeBytes, app.ID); err != nil {
				_ = r2c.Delete(ctx, newKey)
				return fmt.Errorf("update system_app: %w", err)
			}
			fmt.Printf("Row system_app id=%d (name=%q) kini menunjuk %s\n", app.ID, app.Name, newKey)

			if oldKey != "" && oldKey != newKey {
				if err := r2c.Delete(ctx, oldKey); err != nil {
					fmt.Printf("WARN: hapus file R2 lama (%s) gagal: %v\n", oldKey, err)
				} else {
					fmt.Printf("File R2 lama dihapus: %s\n", oldKey)
				}
			}
		}
		return nil
	}

	if err := publish(findRow(func(n string) bool { return n == "examvan" }), *studentApk, "student"); err != nil {
		fmt.Printf("FATAL (student): %v\n", err)
		os.Exit(1)
	}
	if *kioskApk != "" {
		if err := publish(findRow(func(n string) bool { return strings.Contains(n, "kiosk") }), *kioskApk, "kiosk"); err != nil {
			fmt.Printf("FATAL (kiosk): %v\n", err)
			os.Exit(1)
		}
	}

	// Sinkronkan syarat versi server dengan rilis yang baru saja dipublikasi:
	// klien usang langsung mendapat 426 force-update yang jelas.
	cur := models.GetSaasSettingWithDefault(ctx, pool, models.SettingAndroidVersion, models.DefaultSettings[models.SettingAndroidVersion])
	if cur != *version {
		if err := models.SetSaasSetting(ctx, pool, models.SettingAndroidVersion, *version); err != nil {
			fmt.Printf("WARN: SetSaasSetting android_version: %v\n", err)
		} else {
			fmt.Printf("saas_setting android_version: %q -> %q\n", cur, *version)
		}
	} else {
		fmt.Printf("saas_setting android_version sudah %q\n", *version)
	}

	fmt.Println("RELEASE SELESAI.")
}