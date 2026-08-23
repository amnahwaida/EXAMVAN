/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

// Murni: true = aman menutup modal (tidak ada unggahan berjalan).
function canCloseUpload(uploadActive) {
    return !uploadActive;
}

function openUploadModal() {
    const modal = document.getElementById('uploadModal');
    modal.style.display = 'flex';
    void modal.offsetWidth;
    modal.classList.add('show');
}

function closeUploadModal() {
    // Fix review Aplikasi Sistem #2: menutup modal saat unggah berjalan
    // tidak membatalkan XHR — reload sukses bisa terjadi mendadak.
    if (!canCloseUpload(!!window.__uploadInProgress)) {
        showToast('Unggahan masih berlangsung — tunggu hingga selesai.', 'error');
        return;
    }
    const modal = document.getElementById('uploadModal');
    modal.classList.remove('show');
    setTimeout(() => {
        modal.style.display = 'none';
        document.getElementById('uploadAppForm').reset();
        document.getElementById('file-name-display').innerText = 'Pilih atau Seret File Kesini';
        document.getElementById('file-name-display').style.color = 'white';
        document.getElementById('uploadError').style.display = 'none';
        document.getElementById('uploadProgressContainer').style.display = 'none';
    }, 300);
}

function updateFileName(input) {
    const display = document.getElementById('file-name-display');
    if (input.files && input.files[0]) {
        display.innerText = input.files[0].name;
        display.style.color = '#c084fc';
    } else {
        display.innerText = 'Pilih atau Seret File Kesini';
        display.style.color = 'white';
    }
}

// Fix review Aplikasi Sistem #1: wire drag & drop pada area file —
// teks "Pilih atau Seret File Kesini" dulu hanya janji tanpa handler.
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

function submitUpload(event) {
    event.preventDefault();
    const form = document.getElementById('uploadAppForm');
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
        if (xhr.status === 200) {
            const data = JSON.parse(xhr.responseText);
            if (data.success) {
                // Fix UX: kembali KE TAB APLIKASI SISTEM (query ?uploaded=1
                // memaksa reload penuh; hash membuka tab yang benar) — dulu
                // reload polos mendarat di tab default (Kelola User) sehingga
                // kartu aplikasi seolah hilang.
                window.location.href = '/admin/settings?uploaded=1#system-apps';
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
            window.location.reload();
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
