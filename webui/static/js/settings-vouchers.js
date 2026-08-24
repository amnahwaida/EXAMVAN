/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

// S29-followup: definisi copyCode versi lokal DIHAPUS — versi tanpa guard
// navigator.clipboard menimpa versi guarded dari admin-core.js sehingga klik
// salin kode voucher gagal senyap di HTTP LAN. Semua pemanggil kini memakai
// copyCode guarded (guard API + fallback textarea/execCommand) dari core.

let currentVoucherPage = 1;

function renderVouchersError(msg, page) {
    const tbody = document.getElementById('vouchersTableBody');
    if (!tbody) return;
    tbody.setAttribute('aria-busy', 'false');
    // R29: retry via data-action (handler diregister di bawah) — halaman
    // dibawa data-page hasil parseInt agar interpolasi tetap numerik.
    tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;padding:40px;color:#fca5a5;">${msg}
        <div style="margin-top:12px;"><button type="button" class="btn-sm btn-secondary" data-action="voucher-retry-load" data-page="${parseInt(page, 10) || 1}">Coba Lagi</button></div></td></tr>`;
}

function loadVouchers(page = 1) {
    currentVoucherPage = Math.max(1, parseInt(page, 10) || 1);
    const tbody = document.getElementById('vouchersTableBody');
    const search = document.getElementById('searchVoucher').value.trim();
    if (tbody) {
        tbody.setAttribute('aria-busy', 'true');
        tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;padding:40px;color:var(--color-text-secondary);"><svg class="icon-svg spin" style="width:16px;height:16px;vertical-align:-3px;margin-right:8px;" aria-hidden="true"><use href="#hi-refresh"/></svg>Memuat data voucher...</td></tr>`;
    }
    const url = `/admin/api/vouchers?page=${page}&search=${encodeURIComponent(search)}`;
    
    apiFetch(url)
    .then(r => r.json())
    .then(res => {
        if (!res.success) {
            renderVouchersError(res.message || 'Gagal memuat voucher', page);
            return;
        }
        if (tbody) tbody.setAttribute('aria-busy', 'false');
        renderVouchersTable(res.vouchers);
        renderPagination(res.pagination);
    })
    .catch(err => {
        console.error(err);
        renderVouchersError('Gagal terhubung ke server', page);
    });
}

function renderVouchersTable(vouchers) {
    const tbody = document.getElementById('vouchersTableBody');
    if (!vouchers || vouchers.length === 0) {
        const searchEl = document.getElementById('searchVoucher');
        const q = searchEl ? searchEl.value.trim() : '';
        tbody.innerHTML = `<tr><td colspan="7" style="padding:40px;text-align:center;color:var(--color-text-secondary);">${q ? 'Tidak ditemukan voucher yang cocok dengan pencarian "' + escapeHtml(q) + '".' : 'Belum ada voucher yang dibuat.'}</td></tr>`;
        return;
    }

    let html = '';
    vouchers.forEach(v => {
        // Kode voucher di-escape sekali dan dipakai untuk teks tampil MAUPUN
        // nilai data-* sehingga kode berisi kutip/backslash/tag tidak pernah
        // dimasukkan mentah ke markup (S3).
        const safeCode = escapeHtml(v.code);
        const isExpired = v.expires_at && new Date(v.expires_at) < new Date();
        const isFull = v.used_count >= v.max_usage;
        let statusBadge = `<span style="padding:3px 8px;border-radius:12px;font-size:11px;font-weight:700;background:rgba(16,185,129,0.15);color:#10b981;">Aktif</span>`;
        
        if (!v.is_active) {
            statusBadge = `<span style="padding:3px 8px;border-radius:12px;font-size:11px;font-weight:700;background:rgba(239,68,68,0.15);color:#ef4444;">Nonaktif</span>`;
        } else if (isExpired) {
            statusBadge = `<span style="padding:3px 8px;border-radius:12px;font-size:11px;font-weight:700;background:rgba(245,158,11,0.15);color:#f59e0b;">Kadaluarsa</span>`;
        } else if (isFull) {
            statusBadge = `<span style="padding:3px 8px;border-radius:12px;font-size:11px;font-weight:700;background:rgba(148,163,184,0.15);color:#94a3b8;">Habis</span>`;
        }

        const expiryStr = v.expires_at ? new Date(v.expires_at).toLocaleDateString('id-ID', { year: 'numeric', month: 'short', day: 'numeric' }) : 'Selamanya';

        let durationText = v.duration_type;
        if (v.duration_type === 'bulanan') durationText = 'Bulanan (30 Hari)';
        else if (v.duration_type === 'semester') durationText = 'Semester (180 Hari)';
        else if (v.duration_type === 'tahunan') durationText = 'Tahunan (365 Hari)';
        else if (!isNaN(parseInt(v.duration_type))) durationText = `${v.duration_type} Hari (Kustom)`;

        html += `
        <tr style="border-bottom:1px solid rgba(255,255,255,0.04);">
            <td data-label="Kode" style="padding:14px 20px;">
                <div class="voucher-code-badge" data-action="copy" data-voucher-code="${safeCode}" title="Klik untuk menyalin kode">
                    <span>${safeCode}</span>
                    <svg class="icon-svg voucher-copy-btn" style="width:14px;height:14px;"><use href="#hi-clipboard"/></svg>
                </div>
            </td>
            <td data-label="Paket & Durasi" style="padding:14px 20px;">
                <strong style="color:#fff;text-transform:uppercase;font-size:12px;">${v.package}</strong>
                <div style="font-size:11px;color:var(--color-text-secondary);">${durationText}</div>
            </td>
            <td data-label="Penggunaan" style="padding:14px 20px;">
                <span style="font-weight:700;color:${v.used_count > 0 ? '#c084fc' : '#94a3b8'};">${v.used_count}</span> / ${v.max_usage}
                ${v.used_count > 0 ? `<button type="button" data-action="redemptions" data-id="${v.id}" data-voucher-code="${safeCode}" style="background:none;border:none;color:#a855f7;font-size:12px;cursor:pointer;margin-left:4px;text-decoration:underline;padding:8px 6px;">(Lihat User)</button>` : ''}
            </td>
            <td data-label="Kadaluarsa" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);">${expiryStr}</td>
            <td data-label="Status" style="padding:14px 20px;">${statusBadge}</td>
            <td data-label="Catatan" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);">${v.notes || '—'}</td>
            <td data-label="Aksi" style="padding:14px 20px;text-align:right;">
                <button type="button" data-action="toggle" data-id="${v.id}" data-voucher-code="${safeCode}" data-active="${v.is_active ? '1' : '0'}" style="display:inline-flex;align-items:center;gap:5px;background:rgba(255,255,255,0.06);border:1px solid var(--color-glass-border);color:#fff;padding:8px 12px;border-radius:8px;font-size:12px;cursor:pointer;margin-right:8px;min-height:36px;">
                    <svg class="icon-svg" style="width:13px;height:13px;" aria-hidden="true"><use href="#${v.is_active ? 'hi-stop' : 'hi-play'}"/></svg>
                    ${v.is_active ? 'Matikan' : 'Aktifkan'}
                </button>
                <button type="button" data-action="delete" data-id="${v.id}" data-voucher-code="${safeCode}" style="display:inline-flex;align-items:center;gap:5px;background:rgba(239,68,68,0.15);border:1px solid rgba(239,68,68,0.3);color:#ef4444;padding:8px 12px;border-radius:8px;font-size:12px;cursor:pointer;min-height:36px;">
                    <svg class="icon-svg" style="width:13px;height:13px;" aria-hidden="true"><use href="#hi-trash"/></svg>
                    Hapus
                </button>
            </td>
        </tr>`;
    });

    tbody.innerHTML = html;
}

// Delegasi klik untuk semua aksi baris voucher (salin/toggle/lihat user/
// hapus): id & kode voucher dibawa lewat data-* attribute sehingga kode yang
// mengandung kutip/backslash tidak bisa memutus atribut handler (S3).
function wireVoucherRowActions() {
    const tbody = document.getElementById('vouchersTableBody');
    if (!tbody || tbody.dataset.rowActionsWired) return;
    tbody.dataset.rowActionsWired = '1';
    tbody.addEventListener('click', (e) => {
        const target = e.target.closest('[data-action]');
        if (!target || !tbody.contains(target)) return;
        const action = target.getAttribute('data-action');
        if (action === 'copy') {
            copyCode(target.closest('.voucher-code-badge') || target, target.getAttribute('data-voucher-code') || '');
            return;
        }
        const id = parseInt(target.getAttribute('data-id'), 10);
        if (Number.isNaN(id)) return;
        const code = target.getAttribute('data-voucher-code') || '';
        if (action === 'redemptions') viewRedemptions(id, code);
        else if (action === 'toggle') toggleVoucher(id, code, target.getAttribute('data-active') === '1');
        else if (action === 'delete') deleteVoucher(id, code);
    });
}

function renderPagination(pg) {
    const container = document.getElementById('paginationContainer');
    if (!pg || pg.total_pages <= 1) {
        container.innerHTML = `<span style="font-size:12px;color:var(--color-text-secondary);">Total: ${pg ? pg.total : 0} voucher</span>`;
        return;
    }

    // Cap halaman yang dirender: maksimal ~9 tombol (1, …, sekitar halaman
    // aktif, …, terakhir) supaya daftar dengan banyak halaman tidak
    // memunculkan puluhan tombol.
    const range = paginationRange(pg.page, pg.total_pages);
    let btns = '';
    for (const item of range) {
        if (item === '…') {
            btns += `<span style="color:var(--color-text-secondary);padding:4px 6px;font-size:12px;">…</span>`;
            continue;
        }
        // R29: paginasi via data-action + data-page (tanpa onclick inline).
        const activeStyle = item === pg.page ? 'background:var(--color-primary);color:#fff;' : 'background:rgba(255,255,255,0.06);color:var(--color-text-secondary);';
        btns += `<button type="button" data-action="voucher-page" data-page="${parseInt(item, 10) || 1}" style="padding:4px 10px;border-radius:6px;border:none;font-size:12px;cursor:pointer;${activeStyle}">${item}</button>`;
    }

    container.innerHTML = `
        <span style="font-size:12px;color:var(--color-text-secondary);">Halaman ${pg.page} dari ${pg.total_pages} (Total ${pg.total} voucher)</span>
        <div style="display:flex;flex-wrap:wrap;gap:6px;">${btns}</div>`;
}

// Halaman yang dirender untuk pagination ter-cap (dipakai juga oleh Riwayat).
function paginationRange(page, total) {
    if (total <= 9) {
        const r = [];
        for (let i = 1; i <= total; i++) r.push(i);
        return r;
    }
    const candidates = new Set([1, page - 2, page - 1, page, page + 1, page + 2, total]);
    const nums = Array.from(candidates).filter(n => n >= 1 && n <= total).sort((a, b) => a - b);
    const out = [];
    let prev = 0;
    for (const n of nums) {
        if (prev && n - prev > 1) out.push('…');
        out.push(n);
        prev = n;
    }
    return out;
}

function openSingleModal() { document.getElementById('singleModal').style.display = 'flex'; }
function closeSingleModal() { document.getElementById('singleModal').style.display = 'none'; }
function openBatchModal() { document.getElementById('batchModal').style.display = 'flex'; }
function closeBatchModal() { document.getElementById('batchModal').style.display = 'none'; }
function closeRedemptionsModal() { document.getElementById('redemptionsModal').style.display = 'none'; }

function toggleCustomDuration(type) {
    const sel = document.getElementById(type + 'Duration');
    const grp = document.getElementById(type + 'CustomDaysGroup');
    if (sel && grp) {
        grp.style.display = sel.value === 'custom' ? 'block' : 'none';
    }
}

function toggleCustomPackage(type) {
    const sel = document.getElementById(type + 'Package');
    const grp = document.getElementById(type + 'CustomGroup');
    if (sel && grp) {
        grp.style.display = sel.value === 'custom' ? 'block' : 'none';
    }
}

// Appends the custom entitlement fields (used when package === 'custom').
function appendCustomVoucherFields(formData, type) {
    formData.append('custom_label', (document.getElementById(type + 'CustomLabel').value || '').trim());
    formData.append('custom_max_exams', document.getElementById(type + 'CustomMaxExams').value);
    formData.append('custom_max_concurrent_exams', document.getElementById(type + 'CustomConcurrent').value);
    formData.append('custom_max_pdf_size_mb', document.getElementById(type + 'CustomPdfMb').value);
    formData.append('custom_max_storage_size_mb', document.getElementById(type + 'CustomStorageMb').value);
    formData.append('custom_max_users', document.getElementById(type + 'CustomMaxUsers').value);
    formData.append('custom_role', document.getElementById(type + 'CustomRole').value);
}

function submitSingleVoucher(e) {
    e.preventDefault();
    const btn = document.getElementById('btnSubmitSingle');
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';

    const durationVal = document.getElementById('singleDuration').value;
    const pkgVal = document.getElementById('singlePackage').value;
    const formData = new FormData();
    formData.append('code', document.getElementById('singleCode').value.trim());
    formData.append('package', pkgVal);
    formData.append('duration_type', durationVal);
    if (durationVal === 'custom') {
        formData.append('custom_days', document.getElementById('singleCustomDays').value);
    }
    if (pkgVal === 'custom') {
        appendCustomVoucherFields(formData, 'single');
    }
    formData.append('max_usage', document.getElementById('singleMaxUsage').value);
    formData.append('expires_at', document.getElementById('singleExpiresAt').value);
    formData.append('notes', document.getElementById('singleNotes').value.trim());

    apiFetch('/admin/api/vouchers', {
        method: 'POST',
        body: formData
    })
    .then(r => r.json())
    .then(res => {
        btn.disabled = false;
        btn.textContent = 'Simpan Voucher';
        if (res.success) {
            showToast(res.message, 'success');
            closeSingleModal();
            document.getElementById('formSingleVoucher').reset();
            loadVouchers(1);
        } else {
            showToast(res.message || 'Gagal membuat voucher', 'error');
        }
    })
    .catch(err => {
        btn.disabled = false;
        btn.textContent = 'Simpan Voucher';
        showToast('Gagal terhubung ke server', 'error');
    });
}

function submitBatchVoucher(e) {
    e.preventDefault();
    const btn = document.getElementById('btnSubmitBatch');
    btn.disabled = true;
    btn.textContent = 'Generating...';

    const durationVal = document.getElementById('batchDuration').value;
    const pkgVal = document.getElementById('batchPackage').value;
    const formData = new FormData();
    formData.append('prefix', document.getElementById('batchPrefix').value.trim());
    formData.append('count', document.getElementById('batchCount').value);
    formData.append('package', pkgVal);
    formData.append('duration_type', durationVal);
    if (durationVal === 'custom') {
        formData.append('custom_days', document.getElementById('batchCustomDays').value);
    }
    if (pkgVal === 'custom') {
        appendCustomVoucherFields(formData, 'batch');
    }
    formData.append('max_usage', document.getElementById('batchMaxUsage').value);
    formData.append('expires_at', document.getElementById('batchExpiresAt').value);
    formData.append('notes', document.getElementById('batchNotes').value.trim());

    apiFetch('/admin/api/vouchers/batch', {
        method: 'POST',
        body: formData
    })
    .then(r => r.json())
    .then(res => {
        btn.disabled = false;
        btn.textContent = 'Generate Batch';
        if (res.success) {
            showToast(res.message, 'success');
            closeBatchModal();
            loadVouchers(1);
        } else {
            showToast(res.message || 'Gagal generate batch', 'error');
        }
    })
    .catch(err => {
        btn.disabled = false;
        btn.textContent = 'Generate Batch';
        showToast('Gagal terhubung ke server', 'error');
    });
}

let pendingConfirmCallback = null;

function showConfirmModal(title, message, actionBtnText, isDanger, onConfirm) {
    document.getElementById('confirmActionTitle').textContent = title;
    document.getElementById('confirmActionMessage').innerHTML = message;
    
    const btn = document.getElementById('btnConfirmActionSubmit');
    btn.textContent = actionBtnText || 'Ya, Lanjutkan';
    if (isDanger) {
        btn.style.background = '#ef4444';
        btn.style.border = 'none';
        btn.style.color = '#fff';
    } else {
        btn.style.background = 'linear-gradient(135deg, #a855f7, #6366f1)';
        btn.style.border = 'none';
        btn.style.color = '#fff';
    }
    
    pendingConfirmCallback = onConfirm;
    document.getElementById('confirmActionModal').style.display = 'flex';
}

function closeConfirmActionModal(e) {
    if (!e || e.target.id === 'confirmActionModal' || e.target.classList.contains('modal-close') || e.target.tagName === 'BUTTON') {
        // R25: tutup via API Modal terpusat (admin-core.js).
        Modal.close('confirmActionModal');
        pendingConfirmCallback = null;
    }
}

var __btnConfirmAction = document.getElementById('btnConfirmActionSubmit');
if (__btnConfirmAction) __btnConfirmAction.addEventListener('click', () => {
    if (pendingConfirmCallback) {
        const cb = pendingConfirmCallback;
        pendingConfirmCallback = null;
        document.getElementById('confirmActionModal').style.display = 'none';
        cb();
    }
});

function toggleVoucher(id, code, isActive) {
    const actionText = isActive ? 'menonaktifkan' : 'mengaktifkan kembali';
    const btnText = isActive ? 'Matikan Voucher' : 'Aktifkan Voucher';
    showConfirmModal(
        'Konfirmasi Status Voucher',
        `Apakah Anda yakin ingin ${actionText} kode voucher <strong style="color:#c084fc;">${escapeHtml(code)}</strong>?`,
        btnText,
        isActive,
        () => {
            apiFetch(`/admin/api/vouchers/${id}/toggle`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadVouchers(currentVoucherPage);
                } else {
                    showToast(res.message, 'error');
                }
            })
            .catch(() => showToast('Gagal terhubung ke server', 'error'));
        }
    );
}

function toggleVoucherSearchClear() {
    const btn = document.getElementById('voucherSearchClearBtn');
    const input = document.getElementById('searchVoucher');
    if (btn && input) btn.style.display = input.value ? 'flex' : 'none';
}

function clearVoucherSearch() {
    const input = document.getElementById('searchVoucher');
    if (input) input.value = '';
    toggleVoucherSearchClear();
    loadVouchers(1);
}

function deleteVoucher(id, code) {
    showConfirmModal(
        'Konfirmasi Hapus Voucher',
        `Apakah Anda yakin ingin menghapus kode voucher <strong style="color:#c084fc;">${escapeHtml(code)}</strong>? Tindakan ini tidak dapat dibatalkan.`,
        'Hapus Voucher',
        true,
        () => {
            apiFetch(`/admin/api/vouchers/${id}/delete`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadVouchers(currentVoucherPage);
                } else {
                    showToast(res.message, 'error');
                }
            })
            .catch(() => showToast('Gagal terhubung ke server', 'error'));
        }
    );
}

function viewRedemptions(id, code) {
    wireRedemptionsRetry();
    document.getElementById('redemptionsTitle').textContent = `Pengguna Voucher (${code})`;
    document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:var(--color-text-secondary);">Memuat...</p>`;
    document.getElementById('redemptionsModal').style.display = 'flex';

    apiFetch(`/admin/api/vouchers/${id}/redemptions`)
    .then(r => r.json())
    .then(res => {
        if (!res.success) {
            document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:#fca5a5;">${escapeHtml(res.message || 'Gagal memuat data pengguna')} <button type="button" class="btn-sm btn-secondary" data-retry-redemptions data-id="${parseInt(id, 10) || 0}" data-code="${escapeHtml(code)}" style="margin-left:8px;">Coba Lagi</button></p>`;
            return;
        }
        if (!res.redemptions || res.redemptions.length === 0) {
            document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:var(--color-text-secondary);">Belum ada user yang mengklaim voucher ini.</p>`;
            return;
        }
        let html = '<ul style="list-style:none;padding:0;margin:0;">';
        res.redemptions.forEach(r => {
            const dateStr = new Date(r.redeemed_at).toLocaleString('id-ID');
            html += `<li style="padding:10px 14px;border-bottom:1px solid var(--color-glass-border);display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;">
                <strong style="color:#fff;">${escapeHtml(r.username)}</strong>
                <span style="font-size:12px;color:var(--color-text-secondary);">${dateStr}</span>
            </li>`;
        });
        html += '</ul>';
        document.getElementById('redemptionsBody').innerHTML = html;
    })
    .catch(() => {
        document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:#fca5a5;">Gagal terhubung ke server. <button type="button" class="btn-sm btn-secondary" data-retry-redemptions data-id="${parseInt(id, 10) || 0}" data-code="${escapeHtml(code)}" style="margin-left:8px;">Coba Lagi</button></p>`;
    });
}

// Delegasi klik tombol "Coba Lagi" di modal redemptions — pengganti inline
// onclick yang sebelumnya menyisipkan kode voucher mentah ke atribut (S3).
function wireRedemptionsRetry() {
    const body = document.getElementById('redemptionsBody');
    if (!body || body.dataset.retryWired) return;
    body.dataset.retryWired = '1';
    body.addEventListener('click', (e) => {
        const btn = e.target.closest('[data-retry-redemptions]');
        if (!btn) return;
        viewRedemptions(parseInt(btn.getAttribute('data-id'), 10) || 0, btn.getAttribute('data-code') || '');
    });
}


// ===== Batch 8 (R28-FU): registrasi aksi delegasi milik modul ini ============
// Wrapper-tipis yang sebelumnya ada di inline script settings.html dipindah
// ke SINI — satu tempat mendefinisikan + mendaftarkan; fungsi dipanggil
// langsung (hoisting function declaration), tanpa guard typeof.
if (window.Actions && typeof window.Actions.register === 'function') {
    // Buka/tutup modal generate & kelola voucher.
    window.Actions.register('voucher-open-batch', function () { openBatchModal(); });
    window.Actions.register('voucher-open-single', function () { openSingleModal(); });
    window.Actions.register('voucher-close-batch', function () { closeBatchModal(); });
    window.Actions.register('voucher-close-single', function () { closeSingleModal(); });
    window.Actions.register('voucher-close-redemptions', function () { closeRedemptionsModal(); });
    // Toolbar pencarian daftar voucher.
    window.Actions.register('voucher-search', function () { loadVouchers(1); });
    window.Actions.register('voucher-search-clear', function () { clearVoucherSearch(); });
    // Batch 9 (R29): pengganti onclick inline render-JS (retry daftar &
    // paginasi) — halaman tujuan dibawa data-page, dinormalisasi parseInt(x,10).
    window.Actions.register('voucher-retry-load', function (el) {
        loadVouchers(parseInt(el.getAttribute('data-page'), 10) || 1);
    });
    window.Actions.register('voucher-page', function (el) {
        loadVouchers(parseInt(el.getAttribute('data-page'), 10) || 1);
    });
    // Modal konfirmasi generik dipakai modul ini untuk hapus voucher dsb.;
    // closeConfirmActionModal didefinisikan di file ini juga.
    window.Actions.register('confirm-action-close', function () { closeConfirmActionModal(); });
}

// The Voucher tab owns two sub-panels: Daftar (this file) and Riwayat
// (settings-voucher-audit.js, loaded alongside). Wire the sub-tab switch to
// lazy-load the audit list the first time Riwayat is shown, and pre-warm it
// after the list so the switch is instant.
function wireVoucherSubtabs() {
    if (window.__voucherSubtabsWired) return;
    window.__voucherSubtabsWired = true;
    var orig = window.switchVoucherSubtab;
    if (typeof orig === 'function') {
        window.switchVoucherSubtab = function(name) {
            orig(name);
            if (name === 'history' && typeof window.initVoucherAudit === 'function') {
                window.initVoucherAudit();
            }
        };
    }
}

window.__settingsReady['vouchers'] = function() {
    wireVoucherRowActions();
    loadVouchers(1);
    wireVoucherSubtabs();
    // Pre-warm the audit list so the Riwayat sub-tab is ready instantly.
    if (typeof window.initVoucherAudit === 'function') window.initVoucherAudit();
};
