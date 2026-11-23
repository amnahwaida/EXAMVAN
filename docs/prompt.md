# Panduan Prompt AI untuk Pembuatan Kunci Jawaban XML (EXAMVAN)

Dokumen ini berisi template prompt yang siap disalin dan dimasukkan ke dalam AI (seperti **ChatGPT-4.1**, **Claude 4.5 Sonnet/Opus 4.5**, atau **Gemini 2.5 Pro**) bersama dengan berkas PDF soal ujian Anda. Prompt ini dirancang agar AI dapat menganalisis soal ujian dari PDF secara akurat dan menyusun kunci jawaban langsung dalam format XML yang kompatibel dengan fitur import EXAMVAN.

---

## Salin Prompt Di Bawah Ini:

```markdown
Anda adalah seorang ahli evaluasi pendidikan dan spesialis entri data akademis. Tugas Anda adalah menganalisis dokumen soal ujian (berupa teks atau file PDF soal yang dilampirkan) secara mendalam, memecahkan jawabannya dengan akurasi 100%, lalu mengekstrak serta menyusun kunci jawabannya ke dalam format XML terstruktur yang siap diimpor ke sistem aplikasi EXAMVAN.

Pahamilah aturan format XML EXAMVAN berikut secara detail:

### 1. Struktur Root XML
Semua daftar soal harus dibungkus dalam tag root `<questions>...</questions>`.

### 2. Atribut Tag `<question>`
Setiap butir soal ditulis sebagai elemen `<question>` dengan atribut berikut:
- `number` (wajib): Nomor urut soal (angka bulat positif, misalnya: 1, 2, 3, dst).
- `type` (wajib): Jenis tipe soal, harus bernilai salah satu dari:
  - `single_choice` (Pilihan Ganda Biasa)
  - `multiple_choice` (Pilihan Ganda Kompleks)
  - `true_false` (Benar / Salah)
  - `matching` (Menjodohkan / Mencocokkan)
  - `short_answer` (Isian Singkat)
- `weight` (wajib): Bobot nilai soal (default "1.0", bertipe desimal, misal: "1.0", "1.5", "2.0", dst).
- `partial_scoring` (opsional): Nilai parsial untuk tipe `multiple_choice`, `matching`, `true_false`, atau `short_answer`. Default-nya `"true"` agar jawaban yang sebagian benar tetap mendapat poin proporsional. Set `"false"` hanya jika soal mewajibkan seluruh jawaban benar.

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
- Penilaian parsial aktif secara default. Tambahkan `partial_scoring="false"` hanya jika semua pilihan harus benar.
*Contoh:*
```xml
<question number="2" type="multiple_choice" weight="2.0" partial_scoring="true">
    <choices>A, B, C, D, E</choices>
    <key>A, C, D</key>
</question>
```

#### C. Tipe `true_false` (Pernyataan Benar / Salah)
- Tidak membutuhkan tag `<choices>`.
- SATU nomor boleh memuat BEBERAPA pernyataan. Tulis tag `<statements>` berisi daftar pernyataan, SATU PERNYATAAN PER BARIS (newline), agar teks yang mengandung koma tetap utuh.
- Tag `<key>` berisi daftar nilai kapital `TRUE` atau `FALSE` dipisahkan koma, dengan URUTAN dan JUMLAH yang sama dengan pernyataan.
- Penilaian parsial aktif secara default. Tambahkan `partial_scoring="false"` hanya jika semua pernyataan harus benar.
- Label jawaban default adalah Benar/Salah. Jika soal memakai istilah lain, tambahkan tag `<true_label>` dan `<false_label>`, misalnya `Sesuai` dan `Tidak Sesuai`. Kunci tetap ditulis `TRUE`/`FALSE`.
*Contoh satu pernyataan (format lama tetap didukung):*
```xml
<question number="3" type="true_false" weight="1.0">
    <key>TRUE</key>
</question>
```
*Contoh beberapa pernyataan dengan label kustom:*
```xml
<question number="3" type="true_false" weight="2.0" partial_scoring="true">
    <statements>Ibu kota Indonesia adalah Jakarta.
2 + 2 = 5
Air membeku pada suhu 0 derajat Celsius.</statements>
    <true_label>Sesuai</true_label>
    <false_label>Tidak Sesuai</false_label>
    <key>TRUE, FALSE, TRUE</key>
</question>
```

#### D. Tipe `matching` (Menjodohkan / Mencocokkan)
- Wajib memiliki tag `<left_items>` berisi daftar pertanyaan/item kiri yang dipisahkan koma.
- Wajib memiliki tag `<right_items>` berisi daftar opsi jawaban kanan yang dipisahkan koma.
- Tag `<key>` berisi relasi penjodohan dengan format `itemKiri:itemKanan` dipisahkan koma (misal: `1:B, 2:A, 3:C`).
- Penilaian parsial aktif secara default agar siswa mendapat poin proporsional atas pasangan yang cocok. Tambahkan `partial_scoring="false"` hanya jika semua pasangan harus benar.
- Default seluruh kartu memakai SATU nomor. Tambahkan atribut `numbering="range"` HANYA bila setiap item kiri di soal asli memang bernomor sendiri; saat itu nomor soal berikutnya harus melompati jumlah item kiri (contoh: card nomor 4 dengan 3 item kiri memakai 4–6, soal berikutnya nomor 7).
*Contoh:*
```xml
<question number="4" type="matching" weight="3.0" partial_scoring="true">
    <left_items>1, 2, 3</left_items>
    <right_items>A, B, C</right_items>
    <key>1:B, 2:A, 3:C</key>
</question>
```

#### E. Tipe `short_answer` (Isian Singkat)
- Tidak membutuhkan tag `<choices>`.
- Tag `<key>` berisi kata kunci atau frasa jawaban benar yang diharapkan (misal: `Fotosintesis` atau `Jakarta`).
- Pencocokan mengabaikan huruf besar/kecil dan SEMUA spasi, sehingga `Ibu Kota` dianggap sama dengan `ibukota`. Koma desimal dinormalisasi, misalnya `1,5` sama dengan `1.5`.
- Penilaian parsial aktif secara default. Tambahkan `partial_scoring="false"` hanya jika semua isian harus benar.
- SATU nomor boleh memuat BEBERAPA isian, MAKSIMAL 10 isian per nomor. Tulis tag `<statements>` berisi label/prompt tiap isian, SATU PER BARIS. Tag `<key>` berisi jawaban dengan urutan dan jumlah yang sama, juga SATU JAWABAN PER BARIS (newline) agar jawaban yang mengandung koma seperti `1,5` tetap utuh.
- Default jawaban harus urut sesuai kunci. Tambahkan atribut `answer_order="any"` bila urutan jawaban siswa boleh acak; penilaian akan mencocokkan setiap kunci ke jawaban siswa yang belum terpakai.
- Default seluruh kartu memakai SATU nomor. Tambahkan atribut `numbering="range"` HANYA bila setiap isian di soal asli memang bernomor sendiri; bobot sebaiknya sama dengan jumlah isian (contoh: 2 isian bernomor 5–6 memakai `weight="2.0"`).
*Contoh satu isian:*
```xml
<question number="5" type="short_answer" weight="1.5">
    <key>Fotosintesis</key>
</question>
```
*Contoh beberapa isian:*
```xml
<question number="5" type="short_answer" weight="2.0" numbering="range" partial_scoring="true">
    <statements>Ibu kota Indonesia adalah ...
Hasil dari 3 + 4 adalah ...</statements>
    <key>Jakarta
7</key>
</question>
```

---

### TUGAS ANDA:
1. Bacalah seluruh soal dari dokumen PDF / teks soal yang saya berikan dengan teliti.
2. Identifikasi tipe masing-masing soal (apakah Pilihan Ganda Tunggal, Pilihan Ganda Kompleks, Benar/Salah, Menjodohkan, atau Isian Singkat).
3. Pecahkan/tentukan kunci jawaban yang paling tepat untuk masing-masing soal tersebut.
4. Tuliskan output kunci jawaban tersebut **HANYA** dalam format blok kode XML yang utuh dan valid berdasarkan aturan format di atas. Jangan sertakan teks penjelasan lainnya di luar blok kode XML agar mudah disalin langsung.

Mulai analisis dokumen soal ujian berikut:
[LAMPIRKAN TEKS ATAU UNGGAH PDF SOAL DI SINI]
```
