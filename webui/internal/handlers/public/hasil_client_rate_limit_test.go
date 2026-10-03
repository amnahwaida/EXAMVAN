package public

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// H7 — plafon anti-brute /hasil harus di-key pada CLIENT yang meminta,
// bukan pada TOKEN yang ditebak.
//
// Bug
// ---
// Key limiter di handler adalah
// `ratelimit:hasil-token:<scope>:<TOKEN>` pada 60/menit. Dua masalahnya
// saling merusaknya:
//
//  1. Plafon itu tidak bisa melakukan yang diklaimnya. Penyerang yang
//     MENABAK token mencoba token BERBEDA tiap permintaan, jadi setiap
//     tebakan membuka bucket BARU: 60 permintaan PER TEBAKAN = tanpa
//     batas sama sekali lintas tebakan.
//  2. Pada mode static-token SATU token dipakai SELURUH ruangan
//     (natRoomSize = 500). Satu tampilan hasil memakai dua permintaan
//     (halaman + panggilan /api/hasil miliknya), jadi siswa ke-31 yang
//     membuka link sudah mendapat 429 — bukan karena dia, tapi karena
//     teman sekelasnya. Dan siapa pun yang tahu satu token bisa menghabiskan
//     60-request budget-nya dalam satu detik lalu meninggalkannya habis,
//     menolak seluruh kelas dari halaman hasil tanpa batas waktu.
//
// Fix: plafon anti-brute dipindah ke dimensi CLIENT (IP + fingerprint),
// sehingga token yang ditebak tidak pernah bisa membelanjakan jatah
// penonton yang sah. Plafon per-token tetap ada sebagai runaway backstop,
// tapi jauh lebih besar dan dihitung dari ukuran satu ruangan.
// ---------------------------------------------------------------------------

// newHasilLimitProbe membangun engine yang memanggil limiter SESUNGGUHNYA
// (bucket client lalu bucket token), persis seperti handler `/hasil/:token`
// dan `/api/hasil/:token` memanggilnya. Engine sungguhan dipakai supaya
// `c.FullPath()` benar-benar berisi rute yang dipanggil — pemilihan scope
// bergantung padanya.
func newHasilLimitProbe(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	probe := func(c *gin.Context) {
		if checkHasilRateLimit(c, c.Param("token")) {
			c.Status(204)
			return
		}
		c.Status(429)
	}
	r.GET("/hasil/:token", probe)
	r.GET("/api/hasil/:token", probe)
	return r
}

// callHits menjalankan satu permintaan throttle lewat limiter sungguhan dan
// mengembalikan statusnya (204 = lolos, 429 = salah satu bucket habis).
func callHits(r *gin.Engine, path, remoteAddr string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = remoteAddr
	r.ServeHTTP(rec, req)
	return rec.Code
}

// (a) Satu client yang meminta BANYAK TOKEN BERBEDA harus memakai satu
// budget yang sama.
//
// Ini inti bug-nya: dengan key per-token tiap tebakan membuka bucket baru,
// jadi penyerang tidak pernah menyentuh plafonnya. Dengan key per-client,
// permintaan (client, token manapun) semuanya menghitung ke angka yang sama.
func TestClientBudgetIsSharedAcrossGuessedTokens(t *testing.T) {
	r := newHasilLimitProbe(t)
	const client = "198.51.100.7:4444"

	// Setiap request memakai token BARU — persis pola penyerang yang
	// menebak-nebak. Kalau plafonnya masih per-token, semua ini 204.
	for n := 1; n <= hasilClientRateLimitMax; n++ {
		path := fmt.Sprintf("/api/hasil/BRUTEFORCE%06d", n)
		if code := callHits(r, path, client); code != 204 {
			t.Fatalf("request client %d/%d = %d, want 204 (jatah client harus "+
				"cukup untuk satu ruangan %d perangkat)",
				n, hasilClientRateLimitMax, code, hasilNatRoomSize)
		}
	}
	// Jatah client habis -> token BERBEDA berikutnya juga ditolak. Inilah
	// yang tidak mungkin dilakukan limiter per-token.
	if code := callHits(r, "/api/hasil/BRUTEFORCE999999", client); code != 429 {
		t.Fatalf("tebakan token %d = %d, want 429 — satu client masih mendapat "+
			"bucket BARU untuk setiap token yang ditebak, jadi 60/menit per token "+
			"justru berarti TIDAK ADA batas lintas tebakan",
			hasilClientRateLimitMax+1, code)
	}
}

// (b) Satu token yang diminta BANYAK CLIENT BERBEDA tidak boleh habis, dan
// client yang sudah menghabiskan jatahnya tidak boleh mematikan classmates
// yang memakai token yang sama di belakang NAT yang sama.
func TestOneTokenIsNotExhaustedByAnotherClient(t *testing.T) {
	r := newHasilLimitProbe(t)
	const token = "TOK12345"

	// Client A menghabiskan SELURUH jatahnya pada satu token.
	for n := 1; n <= hasilClientRateLimitMax; n++ {
		if code := callHits(r, "/api/hasil/"+token, "198.51.100.11:1111"); code != 204 {
			t.Fatalf("client A request %d/%d = %d, want 204",
				n, hasilClientRateLimitMax, code)
		}
	}
	// Client B (IP berbeda = perangkat/NAT berbeda) harus tetap dilayani.
	// Inilah denial-of-service yang ditutup fix ini: sebelumnya token yang
	// sama sudah habis oleh A sehingga B langsung mendapat 429.
	for n := 1; n <= 5; n++ {
		if code := callHits(r, "/api/hasil/"+token, "198.51.100.12:2222"); code != 204 {
			t.Fatalf("client B request %d = %d, want 204 — client lain yang "+
				"membelanjakan budget token yang sama mematikan seluruh kelas "+
				"pada halaman hasil", n, code)
		}
	}
	// Banyak client berbeda atas token yang sama: semuanya dilayani selama
	// satu ruangan belum menyentuh backstop per-token.
	for c := 20; c < 20+hasilNatRoomSize; c++ {
		// Subnet sendiri dari client A/B di atas: `ClientIP()` membuang
		// port, jadi IP yang sama dengan port berbeda TETAP satu bucket
		// dan akan tersangkut di sisa jatah client A.
		n := c - 20
		ip := fmt.Sprintf("198.51.%d.%d:3333", 20+n/250, 1+n%250)
		if code := callHits(r, "/api/hasil/"+token, ip); code != 204 {
			t.Fatalf("client ke-%d = %d, want 204 (satu ruangan %d perangkat "+
				"berbagi token static harus bisa membuka halaman hasil)",
				c, code, hasilNatRoomSize)
		}
	}
}

// (c) Backstop per-token harus MASIH menahan klien liar, dan ambangnya
// sekurang-kurangnya sebesar satu ruangan — bukan 60/menit seperti dulu
// yang mengunci siswa ke-31 di kelasnya sendiri.
func TestHasilTokenBackstopStopsRunawayAndIsAtLeastNatRoomSize(t *testing.T) {
	if hasilTokenRateLimitMax < hasilNatRoomSize {
		t.Fatalf("hasilTokenRateLimitMax = %d/menit, di bawah ukuran satu "+
			"ruangan (%d/menit) — backstop ini menahan kelas yang sah sebelum "+
			"penyerang pun kewalahan",
			hasilTokenRateLimitMax, hasilNatRoomSize)
	}
	if hasilTokenRateLimitMax <= hasilClientRateLimitMax {
		t.Fatalf("hasilTokenRateLimitMax (%d) harus JAUH lebih besar dari "+
			"plafon client (%d) — kalau tidak, backstop per-token yang "+
			"menjadi limit sesungguhnya dan tokenizer masih bisa membelanjakan "+
			"jatah viewer",
			hasilTokenRateLimitMax, hasilClientRateLimitMax)
	}

	r := newHasilLimitProbe(t)
	// Botnet: banyak client BERBEDA mengejar satu token. Plafon per-client
	// tidak menyentuhnya, jadi backstop per-token yang harus bekerja.
	const token = "TOK12345"
	rejectedAt := 0
	for n := 1; n <= hasilTokenRateLimitMax+1; n++ {
		ip := fmt.Sprintf("203.0.113.%d:5555", n%250)
		if callHits(r, "/api/hasil/"+token, ip) == 429 {
			rejectedAt = n
			break
		}
	}
	if rejectedAt != hasilTokenRateLimitMax+1 {
		t.Fatalf("backstop per-token menolak di request %d, want %d — atau "+
			"tidak menolak sama sekali (backstop runaway hilang)",
			rejectedAt, hasilTokenRateLimitMax+1)
	}
}

// (d) Bucket client halaman dan API tetap dipisah: satu tampilan hasil =
// halaman + panggilan /api/hasil-nya, jadi keduanya harus punya jatah
// sendiri. Token dirotasi tiap request supaya yang diuji murni dimensi
// client, bukan sisa budget per-token.
func TestHasilPageAndAPIClientBudgetsAreSeparate(t *testing.T) {
	r := newHasilLimitProbe(t)
	const client = "203.0.113.99:6666"

	for n := 1; n <= hasilClientRateLimitMax; n++ {
		path := fmt.Sprintf("/hasil/PAGE%06d", n)
		if code := callHits(r, path, client); code != 204 {
			t.Fatalf("halaman request %d/%d = %d, want 204",
				n, hasilClientRateLimitMax, code)
		}
	}
	if code := callHits(r, "/hasil/PAGE999999", client); code != 429 {
		t.Fatalf("halaman request %d = %d, want 429",
			hasilClientRateLimitMax+1, code)
	}
	// Bucket API milik client yang sama harus MASIH fresh — inilah yang rusak
	// kalau kedua rute memakai satu bucket.
	for n := 1; n <= 5; n++ {
		path := fmt.Sprintf("/api/hasil/API%06d", n)
		if code := callHits(r, path, client); code != 204 {
			t.Fatalf("API request %d = %d, want 204 — bucket halaman dan "+
				"bucket API masih berbagi satu angka, sehingga satu tampilan "+
				"hasil memakan dua unit dari jatah yang sama", n, code)
		}
	}
}

// Fingerprint adalah dimensi CLIENT kedua dan sifatnya PARALEL, bukan
// gabungan: memalsukannya hanya me-reset counter fingerprint-nya sendiri,
// counter IP tetap terakumulasi dan tetap menolak (pola yang sama dengan
// middleware.getRateLimitKeys).
func TestSpoofedFingerprintDoesNotResetTheClientCeiling(t *testing.T) {
	r := newHasilLimitProbe(t)
	const client = "192.0.2.44:7777"

	do := func(fp string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/hasil/TOK12345", nil)
		req.RemoteAddr = client
		req.Header.Set("X-Device-Fingerprint", fp)
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Satu client, fingerprint BERUBAH tiap request — client budget tetap
	// harus terakumulasi di counter IP.
	for n := 1; n <= hasilClientRateLimitMax; n++ {
		if code := do(fmt.Sprintf("fp-%d", n)); code != 204 {
			t.Fatalf("request %d/%d = %d, want 204",
				n, hasilClientRateLimitMax, code)
		}
	}
	if code := do("fp-terakhir"); code != 429 {
		t.Fatalf("request %d dengan fingerprint baru = %d, want 429 — client "+
			"dapat bucket baru cukup dengan memalsukan fingerprint",
			hasilClientRateLimitMax+1, code)
	}
}

// (e) Angka handler harus dihitung dari ukuran SATU RUANGAN — sama seperti
// semua angka lain di cmd/server/main.go.
//
// `natRoomSizePinned` sengaja ditulis ulang sebagai literal di sini, bukan
// memakai konstanta produksi main.go: kalau tes ikut memakai `natRoomSize`,
// setiap penggeseran angka di sana ikut menggeser ekspektasi tes dan bug
// sizing seperti "siswa ke-31 kelas dapat 429" bisa lolos diam-diam. Pola
// yang sama dipakai natRoomSizePinned di cmd/server.
//
// Tiga hal yang dikunci:
//   - handler mengukur satu ruangan, bukan satu perangkat;
//   - plafon client tidak boleh lebih ketat daripada plafon middleware
//     per-rute di atasnya (handler jalan SETELAH middleware, jadi angka yang
//     lebih kecil hanya menolak kelas lebih cepat tanpa menambah keamanan);
//   - backstop per-token jauh lebih besar daripada plafon client — kalau
//     tidak, backstop itu yang jadi limit sesungguhnya dan tokenizer tetap
//     bisa membelanjakan jatah viewer.
const natRoomSizePinned = 500

func TestHasilHandlerBudgetsAreSizedFromNatRoomSize(t *testing.T) {
	if hasilNatRoomSize != natRoomSizePinned {
		t.Errorf("hasilNatRoomSize = %d, harus sama dengan ukuran ruangan yang "+
			"dijaga tes (%d) — keduanya harus jadi acuan yang sama dengan "+
			"natRoomSize di cmd/server/main.go",
			hasilNatRoomSize, natRoomSizePinned)
	}

	if hasilClientRateLimitMax < natRoomSizePinned {
		t.Errorf("hasilClientRateLimitMax = %d/menit, di bawah satu ruangan "+
			"(%d/menit) — siswa ke-%d di kelasnya sendiri akan mendapat 429",
			hasilClientRateLimitMax, natRoomSizePinned, hasilClientRateLimitMax+1)
	}

	if hasilTokenRateLimitMax <= hasilClientRateLimitMax {
		t.Errorf("hasilTokenRateLimitMax = %d/menit harus jauh di atas plafon "+
			"client (%d/menit) — kalau tidak, backstop per-token yang jadi limit "+
			"sesungguhnya dan tokenizer tetap bisa membelanjakan jatah viewer",
			hasilTokenRateLimitMax, hasilClientRateLimitMax)
	}
	if hasilTokenRateLimitMax < natRoomSizePinned {
		t.Errorf("hasilTokenRateLimitMax = %d/menit, di bawah satu ruangan "+
			"(%d/menit) — pada mode static-token satu token dipakai seluruh ruang",
			hasilTokenRateLimitMax, natRoomSizePinned)
	}
}
