/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

// Murni: true = aman menutup modal (tidak ada unggahan berjalan).
function canCloseUpload(uploadActive) {
    return !uploadActive;
}


// ===== Refresh kartu aplikasi IN-PLACE =====
// Fix review Aplikasi Sistem: satu unggah/hapus tidak lagi memicu reload
// penuh yang mendarat di tab default — grid dibangun ulang dari API JSON.

const PLATFORM_ICONS = {
    android: '<svg width="32" height="32" viewBox="0 0 24 24" fill="currentColor"><path d="M17.523 15.3414c-.5511 0-.9993-.4486-.9993-.9997s.4483-.9993.9993-.9993c.5511 0 .9993.4482.9993.9993s-.4482.9997-.9993.9997zm-11.046 0c-.5511 0-.9993-.4486-.9993-.9997s.4482-.9993.9993-.9993c.5515 0 .9997.4482.9997.9993s-.4482.9997-.9997.9997zm11.4045-6.02l1.9973-3.4592c.1158-.201.0462-.4576-.1551-.5737-.201-.1162-.4576-.0462-.5737.1551l-2.0224 3.502c-1.3965-.6328-2.9658-.9881-4.6277-.9881s-3.2312.3553-4.6276.9881l-2.0225-3.502c-.1161-.2013-.3726-.2713-.5737-.1551-.2013.1161-.2709.3726-.1551.5737l1.9973 3.4592C2.695 10.7495.2718 14.7766.2718 19.5h23.4563c0-4.7234-2.4232-8.7505-6.7996-10.1786z"/></svg>',
    windows: '<svg width="32" height="32" viewBox="0 0 24 24" fill="currentColor"><path d="M0 3.449L9.75 2.1v9.451H0m10.949-9.602L24 0v11.4H10.949M0 12.6h9.75v9.451L0 20.699M10.949 12.6H24V24l-12.951-1.801"/></svg>',
    linux: '<svg width="32" height="32" viewBox="0 0 24 24" fill="currentColor"><path d="M12.015 0c-2.41 0-4.623 1.156-5.918 3.125-.333.51-.531 1.094-.531 1.688 0 2.21 2.375 5.56 4.312 8.44-1.28.374-2.22 1.34-2.47 2.344-.656-.25-1.5-.188-2.124.281-.781.593-1.062 1.562-.688 2.406.344.75 1.125 1.125 1.938 1.125 1.125 0 2.125-.656 2.5-1.625.5 1.25 1.938 2.22 3.688 2.22 1.718 0 3.156-.97 3.656-2.188.375.938 1.344 1.594 2.469 1.594.781 0 1.53-.375 1.875-1.094.406-.844.156-1.844-.625-2.438-.594-.47-1.406-.56-2.062-.31-2.41-1.03-2.312-2.188-3.468-2.25 1.937-2.844 4.312-6.188 4.312-8.375 0-.594-.188-1.156-.531-1.656-1.313-1.97-3.5-3.125-5.938-3.125H12zM9 16c.563 0 1 .438 1 1s-.438 1-1 1-1-.438-1-1 .438-1 1-1zm6 0c.563 0 1 .438 1 1s-.438 1-1 1-1-.438-1-1 .438-1 1-1z"/></svg>'
};

function platformIcon(platform) {
    return PLATFORM_ICONS[platform] ||
        '<svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4a2 2 0 0 0 2 0z"/></svg>';
}

// S92 (ronde 10): token permintaan monoton — respons loadApps lama yang
// lambat mendarat tidak boleh menimpa render grid yang lebih baru.
var appLoadSeq = 0;

async function loadApps() {
    const seq = ++appLoadSeq;
    try {
        const res = await apiFetch('/admin/api/system-apps');
        const data = await res.json();
        if (seq !== appLoadSeq) return;
        if (!data.success) throw new Error(data.message || 'Gagal memuat');
        renderAppsGrid(data.apps || []);
    } catch (e) {
        if (seq !== appLoadSeq) return;
        showToast('Gagal memuat daftar aplikasi: ' + e.message, 'error');
    }
}

// Pembuat elemen ringkas; textContent aman XSS dari nama/versi custom.
function el(tag, styleText, textContentValue) {
    const node = document.createElement(tag);
    if (styleText) node.style.cssText = styleText;
    if (textContentValue !== undefined) node.textContent = textContentValue;
    return node;
}

function renderAppsGrid(apps) {
    const grid = document.querySelector('#section-system-apps .apps-grid');
    if (!grid) return;
    grid.innerHTML = '';

    if (!apps.length) {
        const empty = el('div',
            'grid-column:1/-1;text-align:center;padding:80px 20px;background:rgba(255,255,255,0.02);border-radius:24px;border:2px dashed rgba(255,255,255,0.1);');
        empty.appendChild(el('h3', 'color:white;font-size:1.5rem;font-weight:700;margin:0 0 12px 0;', 'Belum Ada Aplikasi'));
        empty.appendChild(el('p', 'color:var(--color-text-placeholder);font-size:1.05rem;max-width:480px;margin:0 auto;',
            'Anda belum mengunggah aplikasi. Klik tombol unggah di kanan atas untuk mulai mendistribusikan aplikasi ujian ke siswa.'));
        grid.appendChild(empty);
        return;
    }

    apps.forEach(function(app) {
        const card = el('div', '');
        card.className = 'premium-card';

        const iconWrap = el('div', '');
        iconWrap.className = 'platform-icon platform-' + app.Platform;
        iconWrap.innerHTML = platformIcon(app.Platform);
        card.appendChild(iconWrap);

        // Judul & versi — textContent agar aman XSS dari nama custom.
        const title = el('h3', '', '');
        title.className = 'app-title';
        title.textContent = app.Name;
        card.appendChild(title);

        const badge = el('div', '', '');
        const ver = el('span', '', '');
        ver.className = 'app-version';
        ver.textContent = 'v' + app.Version;
        badge.appendChild(ver);
        card.appendChild(badge);

        const meta = el('div', '', '');
        meta.className = 'app-meta';

        const sizeWrap = el('div', '', '');
        sizeWrap.appendChild(el('div',
            'font-size:0.75rem;text-transform:uppercase;letter-spacing:0.08em;margin-bottom:4px;font-weight:600;', 'Ukuran'));
        sizeWrap.appendChild(el('div',
            'font-weight:700;color:white;font-size:1.05rem;',
            (app.SizeBytes / (1024 * 1024)).toFixed(2) + ' MB'));
        meta.appendChild(sizeWrap);

        const dateWrap = el('div', 'text-align:right;', '');
        dateWrap.appendChild(el('div',
            'font-size:0.75rem;text-transform:uppercase;letter-spacing:0.08em;margin-bottom:4px;font-weight:600;', 'Diunggah Pada'));
        let dateText = '-';
        try { dateText = new Date(app.CreatedAt).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }); } catch (_) {}
        dateWrap.appendChild(el('div',
            'font-weight:700;color:white;font-size:1.05rem;', dateText));
        meta.appendChild(dateWrap);

        card.appendChild(meta);

        const actions = el('div', 'display:flex;gap:16px;', '');
        actions.className = 'action-buttons';

        const dl = el('a',
            'display:flex;align-items:center;gap:6px;color:var(--color-text-placeholder);text-decoration:none;font-size:13px;', 'Unduh');
        dl.href = '/download/app/' + app.ID;
        dl.target = '_blank';
        actions.appendChild(dl);

        const delBtn = el('button',
            'background:none;border:none;color:var(--color-danger-light);cursor:pointer;font-size:13px;', 'Hapus');
        delBtn.addEventListener('click', function() { deleteApp(app.ID, app.Name); });
        actions.appendChild(delBtn);

        card.appendChild(actions);
        grid.appendChild(card);
    });
}

// Batch 9 (S37): uploadModal kini memakai overlay standar .modal-overlay —
// buka/tutup via inline display (pola semua modal lain) sehingga perilaku
// Global Modal Manager (admin-core.js) konsisten; kelas arwah .modal-backdrop
// dan toggle .show tidak dipakai lagi.
// Batch 10 (R48): handle timeout penutupan tertunda — dibatalkan bila modal
// dibuka ulang sebelum 300 ms berlalu, supaya modal yang baru dibuka tidak
// ikut tertutup oleh timer lama.
var __uploadCloseTimer = null;

function openUploadModal() {
    const modal = document.getElementById('uploadModal');
    if (!modal) return;
    // R48: batalkan penutupan tertunda dari closeUploadModal sebelumnya.
    if (__uploadCloseTimer !== null) {
        clearTimeout(__uploadCloseTimer);
        __uploadCloseTimer = null;
    }
    // Reset defensif: jangan pernah menyambut user dengan error upaya sebelumnya.
    const errBox = document.getElementById('uploadError');
    if (errBox) errBox.style.display = 'none';
    wireUploadCloseGuard();
    // S118: lewat Modal Manager (focus trap + Escape + restore fokus).
    Modal.open(modal);
}

function closeUploadModal() {
    // Fix review Aplikasi Sistem #2: menutup modal saat unggah berjalan
    // tidak membatalkan XHR — reload sukses bisa terjadi mendadak.
    if (!canCloseUpload(!!window.__uploadInProgress)) {
        showToast('Unggahan masih berlangsung — tunggu hingga selesai.', 'error');
        return;
    }
    const modal = document.getElementById('uploadModal');
    __uploadCloseTimer = setTimeout(() => {
        __uploadCloseTimer = null;
        // S118: tutup lewat Modal Manager (setara display='none' + state SR).
        Modal.close(modal);
        document.getElementById('uploadAppForm').reset();
        document.getElementById('file-name-display').innerText = 'Pilih atau Seret File Ke Sini';
        document.getElementById('file-name-display').style.color = 'white';
        document.getElementById('uploadError').style.display = 'none';
        document.getElementById('uploadProgressContainer').style.display = 'none';
        hideUploadProgressPill();
    }, 300);
}

// ===== Batch 9 (S37): guard "unggahan masih berlangsung" ====================
// Dulu: tombol ✕/Batal menolak menutup (benar), TAPI Escape/klik-overlay dari
// Global Modal Manager (admin-core.js — MILIK AGEN LAIN, tidak boleh diedit)
// tetap menyembunyikan modal via forceClose. Fix dalam batas kepemilikan:
//   1) Listener CAPTURE pada modal yang berjalan SEBELUM handler manager di
//      dokumen — menahan Escape/klik-overlay + toast penjelasan.
//   2) lockUploadOverlay(): selama __uploadInProgress, setter style.display
//      dan classList.remove('show') milik ELEMEN INI dibungkus agar paksaan
//      tutup dari forceClose menjadi no-op (dilepas saat unggahan selesai).
//   3) Pill progres mengambang + aria-busy supaya jelas unggahan tetap jalan.
var __UPLOAD_BLOCKED_MSG = 'Unggahan masih berlangsung — tunggu hingga selesai.';

function notifyUploadBlocked() {
    if (typeof showToast === 'function') showToast(__UPLOAD_BLOCKED_MSG, 'error');
}

function wireUploadCloseGuard() {
    var modal = document.getElementById('uploadModal');
    if (!modal || modal.dataset.closeGuardWired) return;
    modal.dataset.closeGuardWired = '1';
    // Capture = fase paling awal pada modal, sebelum delegasi dokumen.
    modal.addEventListener('keydown', function (e) {
        if (!window.__uploadInProgress || e.key !== 'Escape') return;
        e.preventDefault();
        e.stopPropagation();
        notifyUploadBlocked();
    }, true);
    modal.addEventListener('click', function (e) {
        // Hanya klik LANGSUNG pada backdrop; klik konten modal lewat bebas.
        if (!window.__uploadInProgress || e.target !== modal) return;
        e.preventDefault();
        e.stopPropagation();
        notifyUploadBlocked();
    }, true);
}

function lockUploadOverlay(modal) {
    if (!modal || modal.dataset.uploadLocked) return;
    modal.dataset.uploadLocked = '1';
    modal.setAttribute('aria-busy', 'true');
    // Bungkus setter display: forceClose menulis style.display='none' langsung.
    var current = modal.style.display;
    try {
        Object.defineProperty(modal.style, 'display', {
            configurable: true,
            get: function () { return current; },
            set: function (v) {
                if (window.__uploadInProgress && String(v) === 'none') return;
                current = v;
            }
        });
    } catch (e) { /* lingkungan non-DOM: guard listener saja */ }
    // Bungkus classList.remove: forceClose menghapus class 'show'.
    var cls = modal.classList;
    if (cls && typeof cls.remove === 'function' && !cls.__uploadOrigRemove) {
        var origRemove = cls.remove;
        cls.__uploadOrigRemove = origRemove;
        cls.remove = function () {
            var args = Array.prototype.slice.call(arguments);
            if (window.__uploadInProgress && args.indexOf('show') !== -1) return;
            return origRemove.apply(cls, args);
        };
    }
}

function unlockUploadOverlay(modal) {
    if (!modal || !modal.dataset.uploadLocked) return;
    delete modal.dataset.uploadLocked;
    modal.removeAttribute('aria-busy');
    try { delete modal.style.display; } catch (e) {}
    var cls = modal.classList;
    if (cls && cls.__uploadOrigRemove) {
        cls.remove = cls.__uploadOrigRemove;
        delete cls.__uploadOrigRemove;
    }
}

// Pill progres mengambang: tetap terlihat walau modal tertutup paksa oleh
// perilaku core di luar kepemilikan file ini (S37 rekomendasi fallback).
function showUploadProgressPill() {
    var pill = document.getElementById('uploadProgressPill');
    if (!pill) {
        pill = document.createElement('div');
        pill.id = 'uploadProgressPill';
        pill.className = 'upload-progress-pill';
        pill.setAttribute('role', 'status');
        pill.innerHTML = '<svg class="icon-svg spin" aria-hidden="true"><use href="#hi-refresh"/></svg> Mengunggah...';
        (document.body || document.documentElement).appendChild(pill);
    }
    pill.style.display = 'flex';
    return pill;
}

function hideUploadProgressPill() {
    var pill = document.getElementById('uploadProgressPill');
    if (pill) pill.style.display = 'none';
}

function updateFileName(input) {
    const display = document.getElementById('file-name-display');
    if (input.files && input.files[0]) {
        display.innerText = input.files[0].name;
        display.style.color = 'var(--color-accent-light)';
    } else {
        display.innerText = 'Pilih atau Seret File Ke Sini';
        display.style.color = 'white';
    }
}

// Fix review Aplikasi Sistem #1: wire drag & drop pada area file —
// teks "Pilih atau Seret File Ke Sini" dulu hanya janji tanpa handler.
(function wireDragDrop() {
    const area = document.getElementById('fileDropArea');
    if (!area) return;
    ['dragover', 'dragenter'].forEach(function(ev) {
        area.addEventListener(ev, function(e) {
            e.preventDefault();
            area.classList.add('drag-over');
        });
    });
    ['dragleave', 'dragend'].forEach(function(ev) {
        area.addEventListener(ev, function() { area.classList.remove('drag-over'); });
    });
    area.addEventListener('drop', function(e) {
        e.preventDefault();
        area.classList.remove('drag-over');
        if (e.dataTransfer && e.dataTransfer.files.length) {
            const input = document.getElementById('appFile');
            input.files = e.dataTransfer.files;
            updateFileName(input);
        }
    });
})();

// S37: pasang guard Escape/backdrop sejak modul termuat — modal statis di
// settings.html sudah tersedia saat script lazy ini dieksekusi.
(function wireUploadGuardNow() {
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', wireUploadCloseGuard);
    } else {
        wireUploadCloseGuard();
    }
})();

// ===== Validasi field-level modal unggah aplikasi (T10b) =====
// Dulu validasi hanya native reportValidity() + toast saat submit — pesan
// lenyap 5 detik tanpa penanda field mana yang salah. Kini tiap field
// bermasalah ditandai inline via setFieldError() (admin-core.js): border
// merah + aria-invalid + <p role="alert"> tepat di bawah input.
function validateUploadForm() {
    const hasHelper = typeof setFieldError === 'function';
    if (!hasHelper) return true; // admin-core belum termuat — fallback native validity

    const form = document.getElementById('uploadAppForm');
    let valid = true;

    const nameInput = document.getElementById('appName');
    if (nameInput && !nameInput.value.trim()) {
        setFieldError(nameInput, 'Nama aplikasi wajib diisi.');
        valid = false;
    } else if (nameInput) clearFieldError(nameInput);

    const versionInput = document.getElementById('appVersion');
    if (versionInput && !versionInput.value.trim()) {
        setFieldError(versionInput, 'Versi aplikasi wajib diisi (contoh: 2.1.0).');
        valid = false;
    } else if (versionInput) clearFieldError(versionInput);

    const fileInput = document.getElementById('appFile');
    if (fileInput && !fileInput.files.length) {
        setFieldError(fileInput, 'Pilih file aplikasi terlebih dahulu (.apk / .exe / .AppImage).');
        valid = false;
    } else if (fileInput) clearFieldError(fileInput);

    return valid;
}

// Pembersihan live: error hilang begitu user memperbaiki field. Dipasang
// sekali per halaman (form ada di modal statis settings).
(function wireUploadFieldValidation() {
    function bind() {
        ['appName', 'appVersion'].forEach(function (id) {
            var input = document.getElementById(id);
            if (!input || input.dataset.fieldValidation) return;
            input.dataset.fieldValidation = '1';
            input.addEventListener('blur', function () { validateUploadForm(); });
            input.addEventListener('input', function () { clearFieldError(input); });
        });
        var fileInput = document.getElementById('appFile');
        if (fileInput && !fileInput.dataset.fieldValidation) {
            fileInput.dataset.fieldValidation = '1';
            fileInput.addEventListener('blur', function () { validateUploadForm(); });
            fileInput.addEventListener('change', function () { clearFieldError(fileInput); });
            // Event 'input' pada type=file tak konsisten antar browser;
            // dipasang defensif untuk kontrak listener, 'change' yang efektif.
            fileInput.addEventListener('input', function () { clearFieldError(fileInput); });
        }
    }
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', bind);
    } else {
        bind();
    }
})();

function submitUpload(event) {
    event.preventDefault();
    const form = document.getElementById('uploadAppForm');
    // Validasi field-level dulu (inline, persisten); native validity sebagai
    // lapis kedua untuk aturan yang tidak kami duplikasi.
    if (!validateUploadForm()) return;
    if (!form.reportValidity()) return;

    const btn = document.getElementById('uploadSubmitBtn');
    const originalContent = btn.innerHTML;
    const errorDiv = document.getElementById('uploadError');
    const errorText = document.getElementById('uploadErrorText');
    const progressContainer = document.getElementById('uploadProgressContainer');
    const progressBar = document.getElementById('uploadProgressBar');
    const percentText = document.getElementById('uploadPercentage');
    const statusText = document.getElementById('uploadStatusText');

    errorDiv.style.display = 'none';
    progressContainer.style.display = 'block';
    window.__uploadInProgress = true;
    // S37: kunci overlay + tampilkan indikator mengambang selama unggahan.
    const modalEl = document.getElementById('uploadModal');
    wireUploadCloseGuard();
    lockUploadOverlay(modalEl);
    showUploadProgressPill();
    btn.innerHTML = '<svg class="animate-spin" width="20" height="20" style="animation: spin 1s linear infinite;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"></path></svg> Memproses...';
    btn.disabled = true;

    const formData = new FormData(form);
    const xhr = new XMLHttpRequest();

    xhr.open('POST', '/admin/api/system-apps', true);
    // Fix: file ini STATIK — literal '{{ .csrf_token }}' tidak pernah
    // ter-render oleh template engine. Ambil token dari meta tag base.html
    // via helper getCsrfToken() (admin-core.js).
    xhr.setRequestHeader('X-CSRF-Token', getCsrfToken());

    xhr.upload.onprogress = function(e) {
        if (e.lengthComputable) {
            const percentComplete = Math.round((e.loaded / e.total) * 100);
            progressBar.style.width = percentComplete + '%';
            percentText.innerText = percentComplete + '%';
            if (percentComplete === 100) {
                statusText.innerHTML = '<svg class="animate-spin" width="18" height="18" style="animation: spin 1s linear infinite;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"></path></svg> Memvalidasi & Menyimpan...';
            }
        }
    };

    xhr.onload = function() {
        window.__uploadInProgress = false;
        unlockUploadOverlay(modalEl);
        hideUploadProgressPill();
        if (xhr.status === 200) {
            const data = JSON.parse(xhr.responseText);
            if (data.success) {
                // Fix review ronde 3: refresh KARTU aplikasi secara in-place —
                // tanpa reload/navigasi apapun, konteks tab tetap terjaga.
                showToast('Aplikasi berhasil diunggah', 'success');
                loadApps();
                closeUploadModal();
            } else {
                showError(apiErrorMessage(data, 'Gagal mengunggah aplikasi'));
            }
        } else {
            let msg = 'Gagal mengunggah file (Status ' + xhr.status + ')';
            try {
                const data = JSON.parse(xhr.responseText);
                msg = apiErrorMessage(data, msg);
            } catch(e) {}
            showError(msg);
        }
    };

    xhr.onerror = function() {
        window.__uploadInProgress = false;
        unlockUploadOverlay(modalEl);
        hideUploadProgressPill();
        showError('Terjadi kesalahan jaringan. Periksa koneksi internet Anda.');
    };

    function showError(msg) {
        errorText.innerText = msg;
        errorDiv.style.display = 'flex';
        progressContainer.style.display = 'none';
        btn.innerHTML = originalContent;
        btn.disabled = false;
        progressBar.style.width = '0%';
    }

        xhr.send(formData);
}

async function deleteApp(id, name) {
    const confirmed = await showConfirm(
        'Hapus aplikasi "' + name + '"?',
        'Aplikasi akan dihapus secara permanen dan tidak bisa dikembalikan.',
        'Ya, Hapus',
        'Batal'
    );
    if (!confirmed) return;

    try {
        // Error toast ditangani eksplisit di bawah (showApiErrorToast) —
        // opt-out dari listener global 'api:error' agar tidak dobel.
        const res = await apiFetch('/admin/api/system-apps/' + id + '/delete', { method: 'POST', suppressApiErrorToast: true });
        const data = await res.json();
        if (data.success) {
            showToast('Aplikasi berhasil dihapus', 'success');
            loadApps();
        } else {
            showApiErrorToast(data, 'Gagal menghapus aplikasi');
        }
    } catch (e) {
        showToast('Gagal menghapus: ' + e.message, 'error');
    }
}


// ===== Init pasca-load modul =====
window.__settingsReady = window.__settingsReady || {};
window.__settingsReady['system-apps'] = function() {
    // Toast sukses setelah redirect pasca-upload (?uploaded=1).
    var params = new URLSearchParams(window.location.search);
    if (params.get('uploaded') === '1' && typeof showToast === 'function') {
        showToast('Aplikasi berhasil diunggah', 'success');
        try { window.history.replaceState({}, '', '/admin/settings#system-apps'); } catch (e) {}
    }
};

// Batch 7 (R28): aksi modal unggah & hapus aplikasi via delegasi data-action.
// Argumen dibawa data-* di markup (data-app-id / data-app-name), sehingga
// nama aplikasi ber-kutip tidak bisa memutus atribut (pola S3).
if (window.Actions && typeof window.Actions.register === 'function') {
    window.Actions.register('app-upload-close', function () { closeUploadModal(); });
    window.Actions.register('app-upload-submit', function (el, ev) { submitUpload(ev); });
    window.Actions.register('app-delete', function (el) {
        var id = parseInt(el.getAttribute('data-app-id'), 10);
        if (Number.isNaN(id)) return;
        deleteApp(id, el.getAttribute('data-app-name') || '');
    });
}
