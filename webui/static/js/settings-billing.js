/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

// Keep function names identical to the previous billing page so any stale
// references (e.g. from browser caches) degrade gracefully.
let pendingRedeemCode = '';

function redeemVoucher() {
    if (window.__adminRole === 'superadmin') return;
    var input = document.getElementById('voucherCodeInput');
    var code = input.value.trim().toUpperCase();
    if (!code) {
        showToast('Silakan masukkan kode voucher', 'error');
        return;
    }
    pendingRedeemCode = code;
    document.getElementById('confirmRedeemCodeDisplay').textContent = code;
    document.getElementById('confirmRedeemModal').style.display = 'flex';
}

function closeConfirmRedeemModal(e) {
    if (!e || e.target.id === 'confirmRedeemModal' || e.target.classList.contains('modal-close')) {
        document.getElementById('confirmRedeemModal').style.display = 'none';
    }
}

function doRedeemVoucher() {
    if (!pendingRedeemCode) return;
    var btn = document.getElementById('btnConfirmDoRedeem');
    var origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Memproses...';

    var formData = new FormData();
    formData.append('code', pendingRedeemCode);

    apiFetch('/admin/api/vouchers/redeem', {
        method: 'POST',
        body: formData
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        btn.disabled = false;
        btn.textContent = origText;
        closeConfirmRedeemModal();
        if (res.success) {
            showToast(res.message, 'success');
            document.getElementById('voucherCodeInput').value = '';
            setTimeout(function() { location.reload(); }, 1500);
        } else {
            showToast(res.message || 'Gagal mengklaim voucher', 'error');
        }
    })
    .catch(function(err) {
        btn.disabled = false;
        btn.textContent = origText;
        closeConfirmRedeemModal();
        showToast('Gagal terhubung ke server', 'error');
    });
}

// ---------------------------------------------------------------------------
// Claimed packages: list them and let the user choose the active one
// ---------------------------------------------------------------------------
var PACKAGE_DISPLAY = {
    'free': 'Free / Trial',
    'guru': 'Paket Guru',
    'individu': 'Paket Individu',
    'sekolah_kecil': 'Paket Sekolah Kecil',
    'sekolah_menengah': 'Paket Sekolah Menengah',
    'sekolah_besar': 'Paket Sekolah Besar',
    'sekolah_unggulan': 'Paket Sekolah Unggulan'
};

function packageDisplayName(key) {
    return PACKAGE_DISPLAY[key] || key || 'Paket';
}

function fmtMB(v) {
    if (v >= 999999) return 'Tanpa Batas';
    if (v >= 1024) return (v / 1024).toFixed(2) + ' GB';
    return v + ' MB';
}

// Format a remaining lifetime (seconds) as a human-readable duration.
function fmtRemaining(sec) {
    sec = Math.max(0, Math.floor(sec || 0));
    var d = Math.floor(sec / 86400);
    var h = Math.floor((sec % 86400) / 3600);
    var m = Math.floor((sec % 3600) / 60);
    if (d > 0) return d + ' hari ' + h + ' jam';
    if (h > 0) return h + ' jam ' + m + ' menit';
    if (m > 0) return m + ' menit';
    if (sec > 0) return sec + ' detik';
    return 'Sisa waktu habis';
}

var myPackagesSeq = 0;
// S78 (ronde 8): token permintaan monoton — respons permintaan lama yang
// lambat mendarat terakhir TIDAK boleh menimpa render yang lebih baru.
function loadMyPackages() {
    var wrap = document.getElementById('myPackagesList');
    if (!wrap) return;
    var seq = ++myPackagesSeq; // S78: lihat catatan di atas

    apiFetch('/admin/api/vouchers/mine')
        .then(function(r) { return r.json(); })
        .then(function(res) {
            if (seq !== myPackagesSeq) return;
            if (!res.success) throw new Error(res.message);
            var list = res.redemptions || [];
            if (list.length === 0) {
                wrap.innerHTML = '<div style="text-align:center;padding:24px;color:var(--color-text-secondary);">Belum ada voucher yang diklaim. Gunakan kartu "Punya Kode Voucher?" di atas untuk klaim paket pertama Anda.</div>';
                return;
            }

            var html = '<table class="exam-table" style="width:100%;border-collapse:collapse;">' +
                '<thead><tr>' +
                '<th scope="col" style="text-align:left;">Paket</th>' +
                '<th scope="col" style="text-align:left;">Kode</th>' +
                '<th scope="col" style="text-align:left;">Sisa Masa Aktif</th>' +
                '<th scope="col" style="text-align:center;">Status</th>' +
                '<th scope="col" style="text-align:right;">Aksi</th>' +
                '</tr></thead><tbody>';

            list.forEach(function(p) {
                var badge = '';
                var action = '';
                // Expired wins over active: an exhausted package whose clock
                // has run out must read as "Berakhir", not "Paket Aktif".
                if (p.is_expired) {
                    badge = '<span class="status-badge" style="background:rgba(239,68,68,0.15);color:var(--color-danger);border:1px solid rgba(239,68,68,0.3);">Berakhir</span>';
                    action = '<span style="color:var(--color-text-secondary);font-size:12px;">Tidak tersedia</span>';
                } else if (p.is_active) {
                    badge = '<span class="status-badge" style="background:rgba(16,185,129,0.15);color:rgb(var(--rgb-success));border:1px solid rgba(16,185,129,0.3);">Paket Aktif</span>';
                    action = '<span style="color:var(--color-text-secondary);font-size:12px;">Berjalan</span>';
                } else {
                    badge = '<span class="status-badge" style="background:rgba(245,158,11,0.15);color:var(--color-warning);border:1px solid rgba(245,158,11,0.3);">Dijeda</span>';
                    action = '<button type="button" class="btn-sm" data-action="billing-package-activate" data-redemption-id="' + p.id + '" style="background:rgba(168,85,247,0.15);color:var(--color-accent-light);border:1px solid rgba(168,85,247,0.3);padding:4px 12px;cursor:pointer;">Aktifkan</button>';
                }

                var remaining;
                if (p.is_unlimited) {
                    remaining = 'Tanpa Batas';
                } else {
                    remaining = p.is_active ? fmtRemaining(p.remaining_seconds) + ' (berjalan)' : fmtRemaining(p.remaining_seconds);
                }
                html += '<tr>' +
                    '<td data-label="Paket"><strong style="color:#fff;">' + escapeHtml(packageDisplayName(p.package)) + '</strong><div style="font-size:11px;color:var(--color-text-secondary);">' + p.max_exams + ' ujian &middot; ' + (p.max_concurrent_exams || p.max_exams || 1) + ' serentak &middot; PDF ' + fmtMB(p.max_pdf_size_mb) + ' &middot; Storage ' + fmtMB(p.max_storage_mb) + '</div></td>' +
                    '<td data-label="Kode"><span style="font-family:var(--font-mono);font-size:12.5px;">' + escapeHtml(p.code || '—') + '</span></td>' +
                    '<td data-label="Sisa Masa Aktif"><span style="font-size:12.5px;">' + remaining + '</span></td>' +
                    '<td data-label="Status" style="text-align:center;">' + badge + '</td>' +
                    '<td data-label="Aksi" style="text-align:right;">' + action + '</td>' +
                    '</tr>';
            });

            html += '</tbody></table>';
            wrap.innerHTML = html;
        })
        .catch(function(err) {
            if (seq !== myPackagesSeq) return;
            console.error(err);
            wrap.innerHTML = '<div style="text-align:center;padding:24px;color:var(--color-danger-light);">Gagal memuat daftar paket.'
                + '<div style="margin-top:12px;"><button type="button" class="btn-sm btn-secondary" data-action="billing-packages-retry">Coba Lagi</button></div></div>';
        });
}

function activatePackage(redemptionId, el) {
    if (window.__adminRole === 'superadmin') return;
    var formData = new FormData();
    formData.append('redemption_id', redemptionId);

    // R141 (ronde 11): penahan klik-ganda — tombol pemanggil di-disable
    // handler delegasi; cabang gagal memulihkan, jalur sukses biarkan
    // disabled hingga reload (jeda 1,2 dtk pra-reload rentan dobel-POST).
    showToast('Mengaktifkan paket...', 'info');
    return apiFetch('/admin/api/vouchers/activate', {
        method: 'POST',
        body: formData
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        if (res.success) {
            showToast(res.message, 'success');
            setTimeout(function() { location.reload(); }, 1200);
        } else {
            if (el) el.disabled = false;
            showToast(res.message || 'Gagal mengaktifkan paket', 'error');
            loadMyPackages();
        }
    })
    .catch(function(err) {
        console.error(err);
        if (el) el.disabled = false;
        showToast('Gagal terhubung ke server', 'error');
    });
}


window.__settingsReady['billing'] = function() {

    var expEl = document.getElementById('expiresAtLabel');
    if (expEl) {
        var raw = expEl.textContent.trim();
        if (raw && raw !== '—') expEl.textContent = localizeUTC(raw);
    }
    if (window.__adminRole !== 'superadmin') {
        loadMyPackages();
    }

};

// Batch 7 (R28): aksi kartu klaim voucher & modal konfirmasi via delegasi
// data-action (tombol di settings.html membawa atribut, handler tetap di sini).
// Catatan: backdrop confirmRedeemModal ditangani aksi 'modal-dismiss' level
// halaman yang memanggil closeConfirmRedeemModal() tanpa argumen — perilaku
// lama hanya menutup untuk klik langsung di overlay / tombol tutup.
if (window.Actions && typeof window.Actions.register === 'function') {
    window.Actions.register('billing-redeem-open', function () { redeemVoucher(); });
    window.Actions.register('billing-redeem-cancel', function () { closeConfirmRedeemModal(); });
    window.Actions.register('billing-redeem-confirm', function () { doRedeemVoucher(); });
    // Batch 11 (S62): render-JS tabel paket klaiman kini bebas onclick —
    // id redemption lewat data-* + normalisasi parseInt (pola Batch 8).
    // R141 (ronde 11): disable tombol di awal + guard dobel-klik (tombol
    // disabled tidak boleh memicu POST kedua selama fetch + jeda reload).
    window.Actions.register('billing-package-activate', function (el) {
        if (el.disabled) return;
        el.disabled = true;
        return activatePackage(parseInt(el.getAttribute('data-redemption-id'), 10), el);
    });
    window.Actions.register('billing-packages-retry', function () { loadMyPackages(); });
}
