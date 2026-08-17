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

function loadMyPackages() {
    var wrap = document.getElementById('myPackagesList');
    if (!wrap) return;

    apiFetch('/admin/api/vouchers/mine')
        .then(function(r) { return r.json(); })
        .then(function(res) {
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
                    badge = '<span class="status-badge" style="background:rgba(239,68,68,0.15);color:#ef4444;border:1px solid rgba(239,68,68,0.3);">Berakhir</span>';
                    action = '<span style="color:var(--color-text-secondary);font-size:12px;">Tidak tersedia</span>';
                } else if (p.is_active) {
                    badge = '<span class="status-badge" style="background:rgba(16,185,129,0.15);color:#10b981;border:1px solid rgba(16,185,129,0.3);">Paket Aktif</span>';
                    action = '<span style="color:var(--color-text-secondary);font-size:12px;">Berjalan</span>';
                } else {
                    badge = '<span class="status-badge" style="background:rgba(245,158,11,0.15);color:#f59e0b;border:1px solid rgba(245,158,11,0.3);">Dijeda</span>';
                    action = '<button class="btn-sm" onclick="activatePackage(' + p.id + ')" style="background:rgba(168,85,247,0.15);color:#c084fc;border:1px solid rgba(168,85,247,0.3);padding:4px 12px;cursor:pointer;">Aktifkan</button>';
                }

                var remaining;
                if (p.is_unlimited) {
                    remaining = 'Tanpa Batas';
                } else {
                    remaining = p.is_active ? fmtRemaining(p.remaining_seconds) + ' (berjalan)' : fmtRemaining(p.remaining_seconds);
                }
                html += '<tr>' +
                    '<td data-label="Paket"><strong style="color:#fff;">' + escapeHtml(packageDisplayName(p.package)) + '</strong><div style="font-size:11px;color:var(--color-text-secondary);">' + p.max_exams + ' ujian serentak &middot; PDF ' + fmtMB(p.max_pdf_size_mb) + ' &middot; Storage ' + fmtMB(p.max_storage_mb) + '</div></td>' +
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
            console.error(err);
            wrap.innerHTML = '<div style="text-align:center;padding:24px;color:#f87171;">Gagal memuat daftar paket.</div>';
        });
}

function activatePackage(redemptionId) {
    if (window.__adminRole === 'superadmin') return;
    var formData = new FormData();
    formData.append('redemption_id', redemptionId);

    showToast('Mengaktifkan paket...', 'info');
    apiFetch('/admin/api/vouchers/activate', {
        method: 'POST',
        body: formData
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        if (res.success) {
            showToast(res.message, 'success');
            setTimeout(function() { location.reload(); }, 1200);
        } else {
            showToast(res.message || 'Gagal mengaktifkan paket', 'error');
            loadMyPackages();
        }
    })
    .catch(function(err) {
        console.error(err);
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
