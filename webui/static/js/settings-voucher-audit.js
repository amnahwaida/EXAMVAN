/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

function renderAuditError(msg, page) {
    const tbody = document.getElementById('auditLogsBody');
    if (!tbody) return;
    tbody.setAttribute('aria-busy', 'false');
    tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;padding:40px;color:var(--color-danger-light);">' + escapeHtml(msg)
        + '<div style="margin-top:12px;"><button type="button" class="btn-sm btn-secondary" data-action="audit-retry" data-page="' + page + '">Coba Lagi</button></div></td></tr>';
}

let auditLoadSeq = 0;
// S78 (ronde 8): token permintaan monoton — respons permintaan lama yang
// lambat mendarat terakhir TIDAK boleh menimpa render yang lebih baru.
function loadAuditLogs(page = 1) {
    const seq = ++auditLoadSeq;
    const tbody = document.getElementById('auditLogsBody');
    const search = document.getElementById('auditSearchInput').value.trim();
    if (tbody) {
        tbody.setAttribute('aria-busy', 'true');
        tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;padding:40px;color:var(--color-text-secondary);"><svg class="icon-svg spin" style="width:16px;height:16px;vertical-align:-3px;margin-right:8px;" aria-hidden="true"><use href="#hi-refresh"/></svg>Memuat riwayat...</td></tr>';
    }
    const url = `/admin/api/vouchers/audit-logs?page=${page}&per_page=20&search=${encodeURIComponent(search)}`;

    apiFetch(url)
    .then(r => r.json())
    .then(res => {
        if (seq !== auditLoadSeq) return;
        if (!res.success) {
            renderAuditError(res.message || 'Gagal memuat riwayat audit', page);
            return;
        }
        if (tbody) tbody.setAttribute('aria-busy', 'false');
        renderAuditLogsTable(res.logs || []);
        renderAuditPagination(res.pagination);
    })
    .catch(err => {
        if (seq !== auditLoadSeq) return;
        console.error(err);
        renderAuditError('Gagal terhubung ke server', page);
    });
}

function renderAuditLogsTable(logs) {
    const tbody = document.getElementById('auditLogsBody');
    if (!logs.length) {
        const searchEl = document.getElementById('auditSearchInput');
        const q = searchEl ? searchEl.value.trim() : '';
        tbody.innerHTML = '<tr><td colspan="4" style="padding:40px;text-align:center;color:var(--color-text-secondary);">' + (q ? 'Tidak ditemukan riwayat yang cocok dengan pencarian "' + escapeHtml(q) + '".' : 'Belum ada riwayat klaim atau aktivasi voucher.') + '</td></tr>';
        return;
    }

    let html = '';
    for (const l of logs) {
        const isRedeemed = l.action === 'voucher_redeemed';
        const isActivated = l.action === 'voucher_activated';
        const isDeactivated = l.action === 'voucher_deactivated';
        const badge = isRedeemed
            ? '<span class="audit-action-badge redeemed">Diklaim</span>'
            : isActivated
                ? '<span class="audit-action-badge activated">Diaktifkan</span>'
                : isDeactivated
                    ? '<span class="audit-action-badge deactivated">Dinonaktifkan</span>'
                    : '<span class="audit-action-badge activated">' + escapeHtml(l.action || 'Aksi') + '</span>';
        html += `
        <tr style="border-bottom:1px solid rgba(255,255,255,0.04);">
            <td data-label="Waktu" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);white-space:nowrap;">${localizeUTC(l.created_at)}</td>
            <td data-label="Aksi" style="padding:14px 20px;">${badge}</td>
            <td data-label="Oleh" style="padding:14px 20px;"><strong style="color:#fff;font-size:12px;">${escapeHtml(l.username || '—')}</strong></td>
            <td data-label="Detail" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);">${escapeHtml(l.detail || '—')}</td>
        </tr>`;
    }
    tbody.innerHTML = html;
}

function renderAuditPagination(pg) {
    const container = document.getElementById('auditPaginationContainer');
    if (!pg || pg.total_pages <= 1) {
        container.innerHTML = `<span style="font-size:12px;color:var(--color-text-secondary);">Total: ${pg ? pg.total : 0} catatan</span>`;
        return;
    }

    // Cap halaman (sama seperti pagination Daftar Voucher): maksimal ~9 tombol.
    const range = paginationRange(pg.page, pg.total_pages);
    let btns = '';
    for (const item of range) {
        if (item === '…') {
            btns += `<span style="color:var(--color-text-secondary);padding:4px 6px;font-size:12px;">…</span>`;
            continue;
        }
        const activeStyle = item === pg.page ? 'background:var(--color-primary);color:#fff;' : 'background:rgba(255,255,255,0.06);color:var(--color-text-secondary);';
        btns += `<button type="button" data-action="audit-page" data-page="${item}" aria-label="Halaman ${item}"${item === pg.page ? ' aria-current="page"' : ''} style="min-height:40px;padding:8px 14px;border-radius:6px;border:none;font-size:12px;cursor:pointer;${activeStyle}">${item}</button>`;
    }

    container.innerHTML = `
        <span style="font-size:12px;color:var(--color-text-secondary);">Halaman ${pg.page} dari ${pg.total_pages} (Total ${pg.total} catatan)</span>
        <div style="display:flex;flex-wrap:wrap;gap:6px;">${btns}</div>`;
}


// Riwayat Klaim is now a sub-tab inside the Voucher tab (section-vouchers);
// this file is loaded together with settings-vouchers.js. The audit list is
// rendered lazily on first show of the Riwayat sub-tab.
window.__auditLoaded = false;
window.initVoucherAudit = function() {
    if (window.__auditLoaded) return;
    window.__auditLoaded = true;
    loadAuditLogs(1);
};

// Batch 7 (R28): aksi toolbar Riwayat Klaim via delegasi data-action.
if (window.Actions && typeof window.Actions.register === 'function') {
    window.Actions.register('audit-refresh', function () { loadAuditLogs(1); });
    window.Actions.register('audit-search', function () { loadAuditLogs(1); });
    window.Actions.register('audit-search-clear', function () {
        var input = document.getElementById('auditSearchInput');
        if (input) input.value = '';
        loadAuditLogs(1);
    });
    // Batch 11 (S62): render-JS error/paginasi kini bebas onclick —
    // nomor halaman lewat data-* + normalisasi parseInt (pola Batch 8).
    window.Actions.register('audit-retry', function (el) {
        loadAuditLogs(parseInt(el.getAttribute('data-page'), 10) || 1);
    });
    window.Actions.register('audit-page', function (el) {
        loadAuditLogs(parseInt(el.getAttribute('data-page'), 10) || 1);
    });
}
