package main

import "testing"

// Budget rate-limit per-IP harus PAS untuk SATU RUANGAN di belakang satu
// NAT sekolah — bukan untuk satu perangkat.
//
// Aturan hitungannya (satu ruangan = 500 perangkat):
//
//   - /api/exams (list): satu panggilan per perangkat, 500 perangkat =>
//     500. Angka lama 60/menit membuat ±440 siswa langsung dapat 429.
//   - /hasil: SATU tampilan hasil = 2 permintaan (halaman HTML + panggilan
//     /api/hasil miliknya), jadi 500 × 2 = 1000/menit.
//   - submit: `submit_with_retry` mencoba maksimal 4 kali (percobaan
//   - backoff 1s/2s/4s). Seluruh ruangan bisa gagal bareng saat Wi-Fi
//     sekolah putus: 500 × 4 = 2000/menit.
//   - presence (access-log + complete): 500 login + 500 heartbeat pertama
//     datang lockstep di t=0, lalu 500 `complete` lockstep di deadline.
//     Jadi budget harus DI ATAS 500 × 2 = 1000, bukan tepat di atasnya —
//     hitungan t=0 saja sudah memakai 100% dari angka itu.
//   - /ws: 500 koneksi gelombang pertama + badai reconnect ruang
//     (500) = 1000/menit.
//
// Waves (join token, PDF, request-approval, result polling) sudah jauh di
// atas: polling hasil saja 500 × 24/menit = 12000. Dipakai sebagai
// pembanding supaya angka yang satu ini tidak ikut dipotong tanpa alasan.
//
// natRoomSizePinned sengaja DITULIS ULANG di sini sebagai literal, bukan
// memakai konstanta produksi: kalau tes ikut memakai `natRoomSize` dari
// main.go, setiap penggeseran angka di sana ikut menggeser ekspektasi tes
// dan bug sizing seperti ini bisa lolos diam-diam. Tes mengikat
// kebutuhan lab 500 perangkat; main.go yang harus menyesuaikan.
const natRoomSizePinned = 500

func TestRateLimitBudgetsCoverOneNATRoom(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
		why  string
	}{
		{
			name: "GET /api/exams",
			got:  rateLimitExamsPerMinute,
			want: natRoomSizePinned * 1,
			why:  "satu list per perangkat; 60/menit membuat 440 dari 500 siswa dapat 429",
		},
		{
			name: "GET /hasil + /api/hasil",
			got:  rateLimitHasilPerMinute,
			want: natRoomSizePinned * 2,
			why:  "satu tampilan hasil = halaman HTML + panggilan /api/hasil-nya",
		},
		{
			name: "POST /submit",
			got:  rateLimitBurstPerMinute,
			want: natRoomSizePinned * 4,
			why:  "submit_with_retry mencoba 4 kali; seluruh ruangan bisa gagal bareng saat jaringan Sesame",
		},
		{
			name: "GET /ws/:room_id",
			got:  rateLimitWSPerMinute,
			want: natRoomSizePinned * 2,
			why:  "500 koneksi gelombang pertama + badai reconnect 500 saat Wi-Fi sekolah putus",
		},
		{
			name: "gelombang join/PDF/poll",
			got:  rateLimitWavePerMinute,
			want: natRoomSizePinned * 24,
			why:  "poll hasil tiap ~2,5 dtk di semua perangkat = 12000/menit",
		},
	}
	for _, tc := range cases {
		if tc.got < tc.want {
			t.Errorf("%s: budget %d/menit < %d/menit (%d perangkat x faktor) -- %s",
				tc.name, tc.got, tc.want, natRoomSizePinned, tc.why)
		}
	}
}
