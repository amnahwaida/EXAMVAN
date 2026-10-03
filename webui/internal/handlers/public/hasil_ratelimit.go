package public

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Rate limit halaman hasil publik — dua dimensi, dua bucket per scope
// ---------------------------------------------------------------------------
//
// H7: plafon anti-brune TIDAK boleh di-key pada token yang ditebak.
//
// Sejarah (bug yang ditutup file ini): satu key
// `ratelimit:hasil-token:<scope>:<TOKEN>` pada 60/menit. Dua masalahnya saling
// merusaknya:
//
//  1. Plafon itu tidak bisa melakukan yang diklaimnya. Penyerang yang MENABAK
//     token mencoba token BERBEDA tiap permintaan, sehingga setiap tebakan
//     membuka bucket BARU — 60 permintaan PER TEBAKAN = tanpa batas lintas
//     tebakan sama sekali.
//  2. Pada mode static-token SATU token dipakai SELURUH ruangan
//     (natRoomSize = 500). Satu tampilan hasil memakai dua permintaan
//     (halaman + panggilan /api/hasil miliknya), jadi siswa ke-31 yang
//     membuka link sudah mendapat 429 — bukan karena dia, tapi karena teman
//     sekelasnya. Dan siapa pun yang tahu satu token bisa menghabiskan
//     60-request budget-nya dalam satu detik lalu meninggalkannya habis:
//     seluruh kelas ditolak dari halaman hasil, tanpa batas waktu, dan tidak
//     ada sisi yang bisafelsik.
//
// Jadi sekarang:
//
//	plafon anti-brute  -> per-CLIENT (IP + fingerprint), shared lintas token
//	backstop runaway  -> per-TOKEN, jauh lebih besar, hanya menahan lalu
//	                     lintas yang benar-benar liar
//
// Bucket halaman dan bucket API tetap dipisah di KEDUA dimensi: satu
// tampilan hasil = dua permintaan untuk token yang sama.

const (
	// hasilNatRoomSize cermin `natRoomSize` di cmd/server/main.go: satu NAT
	// sekolah = SATU bucket per-IP, jadi semua angka handler /hasil dihitung
	// untuk satu RUANGAN penuh, bukan untuk satu perangkat.
	//
	// Angka ini SENGAJA ditulis ulang sebagai literal, bukan diimpor dari
	// main.go: `natRoomSize` unexported di package main, dan tes harus
	// mengikat kebutuhan lab 500 perangkat supaya geseran di sini terlihat.
	// Pola yang sama dipakai natRoomSizePinned di
	// cmd/server/ratelimit_nat_capacity_test.go.
	hasilNatRoomSize = 500

	// hasilClientRateLimitMax adalah plafon anti-brute-force yang DI-KEY
	// pada CLIENT yang meminta (IP + fingerprint), BUKAN pada token yang
	// ditebak — 1500/menit per scope per client.
	//
	// Dengan key per-client, tebakan tidak pernah membelanjakan jatah
	// penonton yang sah, dan satu penyerang hanya mendapat SATU bucket
	// apa pun token yang ia coba.
	//
	// Disetel sama dengan plafon middleware per-rute di atas handler
	// (rateLimitHasilPerMinute = natRoomSize×3, lihat main.go): handler
	// berjalan SETELAH middleware, jadi angka yang lebih kecil di sini hanya
	// akan menolak satu sekolah lebih cepat tanpa menambah keamanan apa pun.
	// Tugas bucket ini anti-brute LINTAS TOKEN, bukan pengetatan tambahan —
	// ruang 500 perangkat di belakang NAT sekolah tetap muat (500 × 1
	// tampilan per menit per scope, jadi 3× kelonggaran).
	hasilClientRateLimitMax = hasilNatRoomSize * 3

	// hasilTokenRateLimitMax adalah BACKSTOP runaway per token (4000/menit
	// per scope) — bukan limit anti-brute utama. Tugasnya menahan satu token
	// yang dikejar banyak client sekaligus (botnet, atau satu ruangan yang
	// refresh bersama), yang tidak bisa disentuh plafon per-client karena
	// tiap client punya bucketnya sendiri.
	//
	// Angka lama 60/menit tidak bisa dipakai di sini: pada mode static-token
	// satu token dipakai 500 perangkat, jadi 60 bahkan tidak cukup untuk
	// SATU ruangan membuka halaman hasil. 8× ukuran ruangan memberi ruang
	// untuk gelombang refresh manual di atas satu tampilan per perangkat.
	hasilTokenRateLimitMax = hasilNatRoomSize * 8

	hasilRateLimitWindow     = 60 * time.Second
	hasilClientRateKeyPrefix = "ratelimit:hasil-client:" // + <scope>:<ip> | fp:<hash>
	hasilTokenRateKeyPrefix  = "ratelimit:hasil-token:"  // + <scope>:<token> (backstop)
)

// hasilScope membedakan bucket yang dipakai halaman HTML dari bucket yang
// dipakai API JSON-nya.
//
// Kenapa harus dua: satu tampilan hasil = DUA permintaan untuk token yang
// sama (halaman /hasil/<token>, lalu /api/hasil/<token> yang dipanggil
// halaman itu sendiri). Dengan satu key per scope saja, satu tampilan memakan
// dua unit dari kuota yang sama — dan pada mode static-token (satu token
// dipakai SELURUH ruangan) siswa-siswa pertama yang membuka link membuat
// siswa lain dapat 429 pada halaman yang sama.
type hasilScope string

const (
	hasilScopePage hasilScope = "page"
	hasilScopeAPI  hasilScope = "api"
)

// hasilScopeFor memilih scope dari rute yang sedang berjalan
// (`c.FullPath()` — pola rute yang terdaftar, bukan URL mentah).
// Default-nya `page`: request yang tidak berjalan di bawah rute terdaftar
// (mis. context test) tidak boleh mendapat bucket API hanya karena tidak
// ada pola `/api/` untuk dibaca.
func hasilScopeFor(c *gin.Context) hasilScope {
	if strings.HasPrefix(c.FullPath(), "/api/") {
		return hasilScopeAPI
	}
	return hasilScopePage
}

// hasilClientRateKeys menyusun dimensi CLIENT untuk bucket anti-brute: IP
// (atau user) sebagai identitas utama, ditambah fingerprint bila dikirim.
//
// Dua counter itu PARALEL, bukan digabung (pola yang sama dengan
// middleware.getRateLimitKeys): memalsukan fingerprint hanya me-reset counter
// fingerprint-nya sendiri, sedangkan counter IP tetap terakumulasi dan tetap
// menolak. Nilai fingerprint di-hash supaya input raksasa/biner tidak pernah
// masuk ke key space Redis.
func hasilClientRateKeys(c *gin.Context) []string {
	keys := make([]string, 0, 2)
	if uidVal, exists := c.Get("user_id"); exists {
		if uid, ok := uidVal.(int); ok {
			keys = append(keys, fmt.Sprintf("user_%d", uid))
		} else {
			keys = append(keys, c.ClientIP())
		}
	} else {
		keys = append(keys, c.ClientIP())
	}
	if fp := strings.TrimSpace(c.GetHeader("X-Device-Fingerprint")); fp != "" {
		sum := sha256.Sum256([]byte(fp))
		keys = append(keys, "fp:"+hex.EncodeToString(sum[:]))
	}
	return keys
}

// checkHasilClientRateLimit enforces the anti-brute ceiling keyed on the
// REQUESTING CLIENT, shared across every token that client guesses.
// Returns false when the client exhausted its budget (caller must 429).
//
// Fail-open tanpa Redis / saat Redis error, mengikuti pola checkRateLimit di
// internal/handlers/api.
func checkHasilClientRateLimit(c *gin.Context) bool {
	rdb := getRedis(c)
	if rdb == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	scope := string(hasilScopeFor(c))
	for _, k := range hasilClientRateKeys(c) {
		key := hasilClientRateKeyPrefix + scope + ":" + k
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			continue
		}
		if count == 1 {
			rdb.Expire(ctx, key, hasilRateLimitWindow)
		}
		if count > hasilClientRateLimitMax {
			return false
		}
	}
	return true
}

// checkHasilTokenRateLimit enforces the per-token BACKSTOP for /hasil routes.
// Returns false when the token was hammered hard enough to be runaway traffic
// (many clients chasing the same token at once), not for ordinary viewing —
// the anti-brute ceiling is per-CLIENT, see checkHasilClientRateLimit.
//
// Fail-open tanpa Redis / saat Redis error.
func checkHasilTokenRateLimit(c *gin.Context, token string) bool {
	rdb := getRedis(c)
	if rdb == nil {
		return true
	}
	token = strings.ToUpper(strings.TrimSpace(token))
	if token == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	key := hasilTokenRateKeyPrefix + string(hasilScopeFor(c)) + ":" + token
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if count == 1 {
		rdb.Expire(ctx, key, hasilRateLimitWindow)
	}
	return count <= hasilTokenRateLimitMax
}

// checkHasilRateLimit menjalankan KEDUA dimensi untuk /hasil: plafon
// anti-brute per client lebih dulu (itu yang menahan penyerang yang
// menebak-nebak token), lalu backstop per-token. False berarti caller harus
// menjawab 429.
func checkHasilRateLimit(c *gin.Context, token string) bool {
	return checkHasilClientRateLimit(c) && checkHasilTokenRateLimit(c, token)
}
