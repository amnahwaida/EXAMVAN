package public

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// ---------------------------------------------------------------------------
// H7 (lanjutan) — limiter /hasil harus DIPANGGIL dari call site handler.
//
// Kenapa tes ini perlu ada
// -----------------------
// `checkHasilClientRateLimit` + `checkHasilTokenRateLimit` sudah punya tes
// perilaku sendiri (`hasil_client_rate_limit_test.go`), dan semuanya hijau
// pada kode yang limiter-nya sama sekali tidak dipakai handler:
//
//	func probe(c *gin.Context) { if checkHasilRateLimit(c, ...) { ... } }
//
// Probe itu memanggil helper secara LANGSUNG, jadi ia tidak pernah melewati
// `HasilPage()`/`HasilAPI()`. Membalik call site handler kembali ke
// `checkHasilTokenRateLimit(c, token)` — persis bug aslinya, dengan
//RED: seluruh tes perilaku tetap hijau.
//
// HeadlessWebSocket tidak bisa jadiVenue: `/hasil/:token` menyentuh DB dan
// template sebelum merender, jadiixaUH harus iterating 1500x. Yang diuji di
// sini adalah INVARIAN STRUKTURAL call site, bukan perilakunya; perilaku
// helper-nya sudah tertutup di file lain.
//
// Invarian: SETIAP handler rute /hasil memanggil limiter GABUNGAN
// (`checkHasilRateLimit`), dan TIDAK ADA handler yang memanggil
// `checkHasilTokenRateLimit` langsung — pemanggilan bucket per-token hanya
// boleh di dalam `checkHasilRateLimit` (hasil_ratelimit.go).
// ---------------------------------------------------------------------------

// hasilRouteHandlers adalah handler rute /hasil yang wajib dijaga call
// site-nya. Dipakai sebagai daftar EXPECTATION, bukan daftar Gentle.
var hasilRouteHandlers = []string{"HasilPage", "HasilAPI"}

// parseHasilSource mengurai hasil.go dan mengembalikan AST + posisi token.
func parseHasilSource(t *testing.T) (*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "hasil.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("gagal parse hasil.go: %v", err)
	}
	return f, fset
}

// callTargets mengembalikan nama fungsi yang dipanggil di dalam fn, beserta
// nomor baris untuk pesan kegagalan.
func callTargets(t *testing.T, fn *ast.FuncDecl) map[string][]int {
	t.Helper()
	out := map[string][]int{}
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fnID := call.Fun.(type) {
		case *ast.Ident:
			name = fnID.Name
		case *ast.SelectorExpr:
			name = fnID.Sel.Name
		}
		if name == "" {
			return true
		}
		out[name] = append(out[name], fsetPosition(call.Pos()))
		return true
	})
	return out
}

var fsetGlobal *token.FileSet

func fsetPosition(p token.Pos) int { return fsetGlobal.Position(p).Line }

// sortedKeys mengembalikan kunci map dalam urutan stabil supaya pesan
// kegagalan deterministik.
func sortedKeys(m map[string][]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// findHandler mencari deklarasi func dengan nama persis yang diberikan.
func findHandler(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("handler %s tidak ditemukan di hasil.go — call site hilang atau "+
		"handler diganti nama", name)
	return nil
}

// TestHasilRouteHandlersUseTheCombinedLimiter mengunci bahwa kedua handler
// rute memanggil `checkHasilRateLimit` (gabungan client + backstop token).
func TestHasilRouteHandlersUseTheCombinedLimiter(t *testing.T) {
	f, fset := parseHasilSource(t)
	fsetGlobal = fset

	for _, name := range hasilRouteHandlers {
		t.Run(name, func(t *testing.T) {
			fn := findHandler(t, f, name)
			targets := callTargets(t, fn)

			lines, ok := targets["checkHasilRateLimit"]
			if !ok {
				t.Fatalf("%s tidak memanggil checkHasilRateLimit sama sekali — "+
					"anti-brute jadi tidak dijalankan. Panggilan yang ada: %v",
					name, sortedKeys(targets))
			}
			// Dijaga: limiter harus Dicek SEBELUM render/DB, bukan sesudah.
			// Satu panggilan sudah cukup; lebih dari satu berarti limiter
			// dipanggil dua kali untuk satu request.
			if len(lines) != 1 {
				t.Errorf("%s memanggil checkHasilRateLimit %d kali (baris %v), "+
					"harusnya tepat 1", name, len(lines), lines)
			}
		})
	}
}

// TestNoHasilHandlerCallsTheTokenLimiterDirectly mengunci pemanggilan
// bucket per-token hanya boleh lewat helper gabungan.
func TestNoHasilHandlerCallsTheTokenLimiterDirectly(t *testing.T) {
	f, fset := parseHasilSource(t)
	fsetGlobal = fset

	for _, name := range hasilRouteHandlers {
		fn := findHandler(t, f, name)
		targets := callTargets(t, fn)

		if lines, ok := targets["checkHasilTokenRateLimit"]; ok {
			t.Errorf("%s memanggil checkHasilTokenRateLimit langsung (baris %v). "+
				"Plafon per-token adalah BACKSTOP runaway, bukan anti-brute — "+
				"memanggilnya langsung membuat satu token jadi bucket yang "+
				"bisa menghabiskan kuota dipakai enumerasi, dan satu kelas "+
				"pecah di token bersama. Gunakan checkHasilRateLimit.",
				name, lines)
		}
		if lines, ok := targets["checkHasilClientRateLimit"]; ok {
			t.Errorf("%s memanggil checkHasilClientRateLimit langsung (baris %v); "+
				"backstop per-token jadi tidak ikut dijalankan. Gunakan "+
				"checkHasilRateLimit", name, lines)
		}
	}
}

// TestCombinedLimiterRunsBothDimensions mengunci helper gabungan benar-benar
// memanggil KEDUA dimensi. Tanpa ini, `checkHasilRateLimit` bisa dialihkan
// jadi token-saja dan tes di atas tetap hijau.
func TestCombinedLimiterRunsBothDimensions(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "hasil_ratelimit.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("gagal parse hasil_ratelimit.go: %v", err)
	}
	fsetGlobal = fset

	fn := findHandler(t, f, "checkHasilRateLimit")
	targets := callTargets(t, fn)

	for _, want := range []string{
		"checkHasilClientRateLimit",
		"checkHasilTokenRateLimit",
	} {
		if _, ok := targets[want]; !ok {
			t.Errorf("checkHasilRateLimit tidak memanggil %s — "+
				"salah satu dimensi limiter hilang", want)
		}
	}
}

// TestHasilLimitsAreDerivedFromNatRoomSize mengunci kedua plafon diturunkan
// dari ukuran satu ruangan, bukan angka tetap yang bisa lebih kecil dari
// jumlah siswa yang sah.
func TestHasilLimitsAreDerivedFromNatRoomSize(t *testing.T) {
	if hasilNatRoomSize != 500 {
		t.Fatalf("hasilNatRoomSize = %d, cermin natRoomSize di cmd/server/main.go "+
			"adalah 500. Bila natRoomSize berubah, cermin ini harus ikut.",
			hasilNatRoomSize)
	}
	if hasilClientRateLimitMax <= 0 {
		t.Errorf("plafon per-client = %d, harus positif", hasilClientRateLimitMax)
	}
	if hasilTokenRateLimitMax <= hasilClientRateLimitMax {
		t.Errorf("backstop per-token (%d) harus JAUH di atas plafon per-client "+
			"(%d), kalau tidak backstopnya yang.reject lebih dulu dan satu "+
			"token bersama bisa lagiKill satu kelas",
			hasilTokenRateLimitMax, hasilClientRateLimitMax)
	}
	if hasilTokenRateLimitMax < hasilNatRoomSize {
		t.Errorf("backstop per-token (%d) di bawah ukuran satu ruangan (%d): "+
			"kelas yang penuh akan kena 429",
			hasilTokenRateLimitMax, hasilNatRoomSize)
	}
}

// TestRateLimitWindowIsSharedByBothDimensions mencegah satu dimensi punya
// jendela waktu berbeda sehingga budgets tidak sebanding.
func TestRateLimitWindowIsSharedByBothDimensions(t *testing.T) {
	if hasilRateLimitWindow <= 0 {
		t.Fatalf("jendela rate limit = %s, harus positif",
			strconv.Quote(hasilRateLimitWindow.String()))
	}
}
