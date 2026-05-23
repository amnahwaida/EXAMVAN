# Panduan Prompt AI untuk Pembuatan Kunci Jawaban XML (EXAMVAN)

Dokumen ini berisi template prompt yang siap disalin dan dimasukkan ke dalam AI (seperti **ChatGPT-4**, **Claude 3.5 Sonnet**, atau **Gemini 1.5 Pro**) bersama dengan berkas PDF soal ujian Anda. Prompt ini dirancang agar AI dapat menganalisis soal ujian dari PDF secara akurat dan menyusun kunci jawaban langsung dalam format XML yang kompatibel dengan fitur import EXAMVAN.

---

## Salin Prompt Di Bawah Ini:

```markdown
Anda adalah seorang ahli evaluasi pendidikan dan spesialis entri data akademis. Tugas Anda adalah menganalisis dokumen soal ujian (berupa teks atau file PDF soal yang dilampirkan) secara mendalam, memecahkan jawabannya dengan akurasi 100%, lalu mengekstrak serta menyusun kunci jawabannya ke dalam format XML terstruktur yang siap diimpor ke sistem aplikasi EXAMVAN.

Pahamilah aturan format XML EXAMVAN berikut secara detail:

### 1. Struktur Root XML
Semua daftar soal harus dibungkus dalam tag root `<questions>...</questions>`.

### 2. Atribut Tag `<question>`
Setiap butir soal ditulis sebagai elemen `<question>` dengan atribut wajib:
- `number`: Nomor urut soal (angka bulat positif, misalnya: 1, 2, 3, dst).
- `type`: Jenis tipe soal, harus bernilai salah satu dari:
  - `single_choice` (Pilihan Ganda Biasa)
  - `multiple_choice` (Pilihan Ganda Kompleks)
  - `true_false` (Benar / Salah)
  - `matching` (Menjodohkan / Mencocokkan)
- `weight`: Bobot nilai soal (default "1.0", bertipe desimal, misal: "1.0", "1.5", "2.0", dst).
- `partial_scoring`: Nilai parsial untuk tipe `multiple_choice` atau `matching`. Bernilai `"true"` jika siswa mendapat poin proporsional atas jawaban yang sebagian benar, atau `"false"` jika harus benar seluruhnya.

### 3. Skema Konten Per Tipe Soal

#### A. Tipe `single_choice` (Pilihan Ganda Tunggal)
- Wajib memiliki tag `<choices>` berisi daftar opsi pilihan dipisahkan dengan koma (misal: `A, B, C, D, E`).
- Tag `<key>` berisi satu huruf kapital opsi jawaban yang benar (misal: `A`).
*Contoh:*
```xml
<question number="1" type="single_choice" weight="1.0">
    <choices>A, B, C, D, E</choices>
    <key>C</key>
</question>
```

#### B. Tipe `multiple_choice` (Pilihan Ganda Kompleks - Jawaban Lebih dari Satu)
- Wajib memiliki tag `<choices>` berisi daftar opsi pilihan dipisahkan dengan koma (misal: `A, B, C, D, E`).
- Tag `<key>` berisi daftar opsi jawaban benar dipisahkan dengan koma (misal: `A, C, D`).
- Tambahkan atribut `partial_scoring="true"` jika ingin mengaktifkan penilaian sebagian.
*Contoh:*
```xml
<question number="2" type="multiple_choice" weight="2.0" partial_scoring="true">
    <choices>A, B, C, D, E</choices>
    <key>A, C, D</key>
</question>
```

#### C. Tipe `true_false` (Pernyataan Benar / Salah)
- Tidak membutuhkan tag `<choices>`.
- Tag `<key>` hanya boleh berisi salah satu dari nilai kapital: `TRUE` atau `FALSE`.
*Contoh:*
```xml
<question number="3" type="true_false" weight="1.0">
    <key>TRUE</key>
</question>
```

#### D. Tipe `matching` (Menjodohkan / Mencocokkan)
- Wajib memiliki tag `<left_items>` berisi daftar pertanyaan/item kiri yang dipisahkan koma.
- Wajib memiliki tag `<right_items>` berisi daftar opsi jawaban kanan yang dipisahkan koma.
- Tag `<key>` berisi relasi penjodohan dengan format `itemKiri:itemKanan` dipisahkan koma (misal: `1:B, 2:A, 3:C`).
- Tambahkan atribut `partial_scoring="true"` agar siswa mendapat poin proporsional atas pasangan yang cocok.
*Contoh:*
```xml
<question number="4" type="matching" weight="3.0" partial_scoring="true">
    <left_items>1, 2, 3</left_items>
    <right_items>A, B, C</right_items>
    <key>1:B, 2:A, 3:C</key>
</question>
```

---

### TUGAS ANDA:
1. Bacalah seluruh soal dari dokumen PDF / teks soal yang saya berikan dengan teliti.
2. Identifikasi tipe masing-masing soal (apakah Pilihan Ganda Tunggal, Pilihan Ganda Kompleks, Benar/Salah, atau Menjodohkan).
3. Pecahkan/tentukan kunci jawaban yang paling tepat untuk masing-masing soal tersebut.
4. Tuliskan output kunci jawaban tersebut **HANYA** dalam format blok kode XML yang utuh dan valid berdasarkan aturan format di atas. Jangan sertakan teks penjelasan lainnya di luar blok kode XML agar mudah disalin langsung.

Mulai analisis dokumen soal ujian berikut:
[LAMPIRKAN TEKS ATAU UNGGAH PDF SOAL DI SINI]
```
