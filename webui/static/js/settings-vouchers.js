/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

function copyCode(el, code) {
    let textToCopy = '';
    let targetBadge = null;

    if (typeof el === 'string') {
        textToCopy = el;
    } else if (el) {
        textToCopy = code || el.textContent.trim();
        targetBadge = el.closest('.voucher-code-badge') || el;
    }

    if (!textToCopy) return;

    navigator.clipboard.writeText(textToCopy).then(() => {
        showToast(`Kode ${textToCopy} tersalin ke clipboard!`, 'success');
        
        if (targetBadge) {
            targetBadge.classList.add('copied');
            
            const useEl = targetBadge.querySelector('use');
            let origHref = '';
            if (useEl) {
                origHref = useEl.getAttribute('href');
                useEl.setAttribute('href', '#hi-check');
            }
            
            setTimeout(() => {
                targetBadge.classList.remove('copied');
                if (useEl && origHref) {
                    useEl.setAttribute('href', origHref);
                }
            }, 1400);
        }
    }).catch(() => {
        showToast('Gagal menyalin kode', 'error');
    });
}

function loadVouchers(page = 1) {
    const search = document.getElementById('searchVoucher').value.trim();
    const url = `/admin/api/vouchers?page=${page}&search=${encodeURIComponent(search)}`;
    
    apiFetch(url)
    .then(r => r.json())
    .then(res => {
        if (!res.success) {
            showToast(res.message || 'Gagal memuat voucher', 'error');
            return;
        }
        renderVouchersTable(res.vouchers);
        renderPagination(res.pagination);
    })
    .catch(err => {
        console.error(err);
        showToast('Gagal terhubung ke server', 'error');
    });
}

function renderVouchersTable(vouchers) {
    const tbody = document.getElementById('vouchersTableBody');
    if (!vouchers || vouchers.length === 0) {
        tbody.innerHTML = `<tr><td colspan="7" style="padding:40px;text-align:center;color:var(--color-text-secondary);">Belum ada voucher yang dibuat.</td></tr>`;
        return;
    }

    let html = '';
    vouchers.forEach(v => {
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
                <div class="voucher-code-badge" onclick="copyCode(this, '${v.code}')" title="Klik untuk menyalin kode">
                    <span>${v.code}</span>
                    <svg class="icon-svg voucher-copy-btn" style="width:14px;height:14px;"><use href="#hi-clipboard"/></svg>
                </div>
            </td>
            <td data-label="Paket & Durasi" style="padding:14px 20px;">
                <strong style="color:#fff;text-transform:uppercase;font-size:12px;">${v.package}</strong>
                <div style="font-size:11px;color:var(--color-text-secondary);">${durationText}</div>
            </td>
            <td data-label="Penggunaan" style="padding:14px 20px;">
                <span style="font-weight:700;color:${v.used_count > 0 ? '#c084fc' : '#94a3b8'};">${v.used_count}</span> / ${v.max_usage}
                ${v.used_count > 0 ? `<button onclick="viewRedemptions(${v.id}, '${v.code}')" style="background:none;border:none;color:#a855f7;font-size:12px;cursor:pointer;margin-left:4px;text-decoration:underline;padding:8px 6px;">(Lihat User)</button>` : ''}
            </td>
            <td data-label="Kadaluarsa" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);">${expiryStr}</td>
            <td data-label="Status" style="padding:14px 20px;">${statusBadge}</td>
            <td data-label="Catatan" style="padding:14px 20px;font-size:12px;color:var(--color-text-secondary);">${v.notes || '—'}</td>
            <td data-label="Aksi" style="padding:14px 20px;text-align:right;">
                <button onclick="toggleVoucher(${v.id}, '${v.code}', ${v.is_active})" style="background:rgba(255,255,255,0.06);border:1px solid var(--color-glass-border);color:#fff;padding:8px 12px;border-radius:8px;font-size:12px;cursor:pointer;margin-right:8px;min-height:36px;">
                    ${v.is_active ? 'Matikan' : 'Aktifkan'}
                </button>
                <button onclick="deleteVoucher(${v.id}, '${v.code}')" style="background:rgba(239,68,68,0.15);border:1px solid rgba(239,68,68,0.3);color:#ef4444;padding:8px 12px;border-radius:8px;font-size:12px;cursor:pointer;min-height:36px;">
                    Hapus
                </button>
            </td>
        </tr>`;
    });

    tbody.innerHTML = html;
}

function renderPagination(pg) {
    const container = document.getElementById('paginationContainer');
    if (!pg || pg.total_pages <= 1) {
        container.innerHTML = `<span style="font-size:12px;color:var(--color-text-secondary);">Total: ${pg ? pg.total : 0} voucher</span>`;
        return;
    }

    let btns = '';
    for (let i = 1; i <= pg.total_pages; i++) {
        const activeStyle = i === pg.page ? 'background:var(--color-primary);color:#fff;' : 'background:rgba(255,255,255,0.06);color:var(--color-text-secondary);';
        btns += `<button onclick="loadVouchers(${i})" style="padding:4px 10px;border-radius:6px;border:none;font-size:12px;cursor:pointer;margin-left:4px;${activeStyle}">${i}</button>`;
    }

    container.innerHTML = `
        <span style="font-size:12px;color:var(--color-text-secondary);">Halaman ${pg.page} dari ${pg.total_pages} (Total ${pg.total} voucher)</span>
        <div>${btns}</div>`;
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
        document.getElementById('confirmActionModal').style.display = 'none';
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
        `Apakah Anda yakin ingin ${actionText} kode voucher <strong style="color:#c084fc;">${code}</strong>?`,
        btnText,
        isActive,
        () => {
            apiFetch(`/admin/api/vouchers/${id}/toggle`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadVouchers();
                } else {
                    showToast(res.message, 'error');
                }
            })
            .catch(() => showToast('Gagal terhubung ke server', 'error'));
        }
    );
}

function deleteVoucher(id, code) {
    showConfirmModal(
        'Konfirmasi Hapus Voucher',
        `Apakah Anda yakin ingin menghapus kode voucher <strong style="color:#c084fc;">${code}</strong>? Tindakan ini tidak dapat dibatalkan.`,
        'Hapus Voucher',
        true,
        () => {
            apiFetch(`/admin/api/vouchers/${id}/delete`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadVouchers();
                } else {
                    showToast(res.message, 'error');
                }
            })
            .catch(() => showToast('Gagal terhubung ke server', 'error'));
        }
    );
}

function viewRedemptions(id, code) {
    document.getElementById('redemptionsTitle').textContent = `Pengguna Voucher (${code})`;
    document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:var(--color-text-secondary);">Memuat...</p>`;
    document.getElementById('redemptionsModal').style.display = 'flex';

    apiFetch(`/admin/api/vouchers/${id}/redemptions`)
    .then(r => r.json())
    .then(res => {
        if (!res.success || !res.redemptions || res.redemptions.length === 0) {
            document.getElementById('redemptionsBody').innerHTML = `<p style="text-align:center;color:var(--color-text-secondary);">Belum ada user yang mengklaim voucher ini.</p>`;
            return;
        }
        let html = '<ul style="list-style:none;padding:0;margin:0;">';
        res.redemptions.forEach(r => {
            const dateStr = new Date(r.redeemed_at).toLocaleString('id-ID');
            html += `<li style="padding:10px 14px;border-bottom:1px solid var(--color-glass-border);display:flex;justify-content:space-between;align-items:center;">
                <strong style="color:#fff;">${r.username}</strong>
                <span style="font-size:12px;color:var(--color-text-secondary);">${dateStr}</span>
            </li>`;
        });
        html += '</ul>';
        document.getElementById('redemptionsBody').innerHTML = html;
    });
}


window.__settingsReady['vouchers'] = function() { loadVouchers(1); };
