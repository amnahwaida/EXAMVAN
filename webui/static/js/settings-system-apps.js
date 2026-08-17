/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

function openUploadModal() {
    const modal = document.getElementById('uploadModal');
    modal.style.display = 'flex';
    void modal.offsetWidth;
    modal.classList.add('show');
}

function closeUploadModal() {
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
    btn.innerHTML = '<svg class="animate-spin" width="20" height="20" style="animation: spin 1s linear infinite;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"></path></svg> Memproses...';
    btn.disabled = true;

    const formData = new FormData(form);
    const xhr = new XMLHttpRequest();

    xhr.open('POST', '/admin/api/system-apps', true);
    xhr.setRequestHeader('X-CSRF-Token', '{{ .csrf_token }}');

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
        if (xhr.status === 200) {
            const data = JSON.parse(xhr.responseText);
            if (data.success) {
                window.location.reload();
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
