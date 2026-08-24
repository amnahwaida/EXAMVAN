/* Logika murni antrean izin halaman detail pengawasan (pengawas_detail.html).
 *
 * T8: polling 5 detik tidak boleh mengganti seluruh tbody antrean tanpa
 * pamrih — tap Izinkan/Tolak bisa mendarat di DOM yang baru diganti. Fungsi
 * di sini MURNI (tanpa DOM): menghitung apakah payload baru perlu dirender,
 * dan bila iya, operasi per-baris apa yang dibutuhkan. Penerapannya ke tbody
 * tetap di template karena butuh esc/localizeUTC dan akses DOM.
 *
 * Dimuat SEBELUM script inline dengan cache-buster ?v={{.version}} dan dipakai
 * lewat global PengawasDetailQueue. Diuji perilakunya oleh
 * static/js/uiux-batch3-t8-polling.test.mjs (Node vm).
 */
(function (global) {
    'use strict';

    // Sidik jari SATU item approval atas field yang benar-benar dirender ke
    // baris tabel (lihat buildApprovalRowHTML di pengawas_detail.html):
    // mac_address (ID perangkat + tombol aksi), student_name, created_at.
    // Perubahan field lain (identity_data, exam_number, student_class, status)
    // tidak mengubah tampilan sehingga dianggap identik.
    function approvalFingerprint(a) {
        return [
            a && a.mac_address ? String(a.mac_address) : '',
            a && a.student_name ? String(a.student_name) : '',
            a && a.created_at ? String(a.created_at) : ''
        ].join('|');
    }

    function fingerprintById(list, id) {
        for (var i = 0; i < list.length; i++) {
            if (list[i].mac_address === id) return approvalFingerprint(list[i]);
        }
        return null;
    }

    // Sidik jari stabil seluruh list untuk guard skip-if-same: bila hasilnya
    // sama dengan render sukses terakhir, tbody sama sekali tidak disentuh.
    // Urutan list disertakan karena menentukan kolom "No".
    function serializeApprovals(list) {
        var rows = list || [];
        var out = new Array(rows.length);
        for (var i = 0; i < rows.length; i++) out[i] = approvalFingerprint(rows[i]);
        return out.join('');
    }

    /* Bandingkan daftar approval lama vs baru (server mengurutkan by
     * created_at ASC; identitas baris = mac_address, unik per ujian).
     *
     * Mengembalikan salah satu:
     *   { type: 'none', ops: [] }   — payload identik, jangan sentuh DOM.
     *   { type: 'replace' }         — tak terpetakan per baris (id hilang atau
     *                                 duplikat) → fallback replace penuh.
     *   { type: 'ops', ops: [...] } — operasi berurutan dengan kontrak:
     *       semua remove dulu, lalu update, lalu add terurut index menaik;
     *       index add/update mengacu posisi FINAL di nextList.
     *       { op: 'remove', id }
     *       { op: 'update', id, item, index }
     *       { op: 'add', id, item, index }
     */
    function computeApprovalRowOps(prevList, nextList) {
        prevList = prevList || [];
        nextList = nextList || [];

        if (serializeApprovals(prevList) === serializeApprovals(nextList)) {
            return { type: 'none', ops: [] };
        }

        // Data harus punya id unik agar bisa dipetakan per baris.
        function hasUniqueIds(list) {
            var seen = {};
            for (var i = 0; i < list.length; i++) {
                var id = list[i] && list[i].mac_address;
                if (!id || seen[id]) return false;
                seen[id] = true;
            }
            return true;
        }
        if (!hasUniqueIds(prevList) || !hasUniqueIds(nextList)) {
            return { type: 'replace' };
        }

        var prevIds = {};
        for (var p = 0; p < prevList.length; p++) prevIds[prevList[p].mac_address] = true;

        var nextIds = {};
        for (var n = 0; n < nextList.length; n++) nextIds[nextList[n].mac_address] = true;

        var ops = [];

        // 1) Baris hilang lebih dulu (disetujui/ditolak di tempat lain,
        //    auto-approve di server, atau dibersihkan) — setelah tahap ini
        //    baris yang tersisa tinggal diperbarui/disisipi.
        for (var r = 0; r < prevList.length; r++) {
            if (!nextIds[prevList[r].mac_address]) {
                ops.push({ op: 'remove', id: prevList[r].mac_address });
            }
        }

        // 2) Isi baris berubah → update in-place hanya baris itu.
        for (var u = 0; u < nextList.length; u++) {
            var it = nextList[u];
            if (prevIds[it.mac_address] &&
                approvalFingerprint(it) !== fingerprintById(prevList, it.mac_address)) {
                ops.push({ op: 'update', id: it.mac_address, item: it, index: u });
            }
        }

        // 3) Baris baru masuk, terurut index menaik agar penyisipan DOM cukup
        //    berjalan sekali dari kiri ke kanan.
        for (var a2 = 0; a2 < nextList.length; a2++) {
            if (!prevIds[nextList[a2].mac_address]) {
                ops.push({ op: 'add', id: nextList[a2].mac_address, item: nextList[a2], index: a2 });
            }
        }

        // 4) Reorder: set ID sama & isi sama tapi urutan berubah — serialisasi
        //    (yang menyertakan urutan) membedakannya, namun langkah 1-3 tidak
        //    menghasilkan apa pun. Posisi "No" bagian dari tampilan, jadi
        //    baris yang pindah di-update in-place pada posisi barunya.
        if (!ops.length) {
            var reordered = false;
            for (var o = 0; o < nextList.length; o++) {
                if (prevList[o] && prevList[o].mac_address !== nextList[o].mac_address) {
                    reordered = true;
                    break;
                }
            }
            if (reordered) {
                for (var w = 0; w < nextList.length; w++) {
                    ops.push({
                        op: 'update',
                        id: nextList[w].mac_address,
                        item: nextList[w],
                        index: w
                    });
                }
            }
        }

        return { type: 'ops', ops: ops };
    }

    global.PengawasDetailQueue = {
        serializeApprovals: serializeApprovals,
        computeApprovalRowOps: computeApprovalRowOps
    };
})(typeof window !== 'undefined' ? window : this);
