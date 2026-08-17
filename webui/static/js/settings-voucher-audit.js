/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

function loadAuditLogs(page = 1) {
    const search = document.getElementById('auditSearchInput').value.trim();
    const url = `/admin/api/vouchers/audit-logs?page=${page}&per_page=20&search=${encodeURIComponent(search)}`;

    apiFetch(url)
    .then(r => r.json())
    .then(res => {
        if (!res.success) {
            showToast(res.message || 'Gagal memuat riwayat audit', 'error');
            return;
        }
        renderAuditLogsTable(res.logs || []);
        renderAuditPagination(res.pagination);
    })
    .catch(err => {
        console.error(err);
        showToast('Gagal terhubung ke server', 'error');
    });
}

function renderAuditLogsTable(logs) {
    const tbody = document.getElementById('auditLogsBody');
    if (!logs.length) {
        tbody.innerHTML = '<tr><td colspan="4" style="padding:40px;text-align:center;color:var(--color-text-secondary);">Belum ada riwayat klaim atau aktivasi voucher.</td></tr>';
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
            <td data-label="Detail" style="padding:14px 20px;font-size:12px;color:#cbd5e1;">${escapeHtml(l.detail || '—')}</td>
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

    let btns = '';
    for (let i = 1; i <= pg.total_pages; i++) {
        const activeStyle = i === pg.page ? 'background:var(--color-primary);color:#fff;' : 'background:rgba(255,255,255,0.06);color:var(--color-text-secondary);';
        btns += `<button onclick="loadAuditLogs(${i})" style="padding:4px 10px;border-radius:6px;border:none;font-size:12px;cursor:pointer;margin-left:4px;${activeStyle}">${i}</button>`;
    }

    container.innerHTML = `
        <span style="font-size:12px;color:var(--color-text-secondary);">Halaman ${pg.page} dari ${pg.total_pages} (Total ${pg.total} catatan)</span>
        <div>${btns}</div>`;
}


window.__settingsReady['voucher-audit'] = function() { loadAuditLogs(1); };
