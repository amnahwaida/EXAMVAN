/* EXAMVAN Admin Panel - JavaScript */

// Toast notification
function showToast(message, type = 'success') {
    const container = document.getElementById('toastContainer');
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateX(40px)';
        toast.style.transition = 'all 0.3s';
        setTimeout(() => toast.remove(), 300);
    }, 3500);
}

// File input display
const pdfInput = document.getElementById('pdfFile');
if (pdfInput) {
    pdfInput.addEventListener('change', function() {
        const display = document.getElementById('fileDisplay');
        const textEl = display.querySelector('.file-text');
        if (this.files.length > 0) {
            const file = this.files[0];
            const sizeMB = (file.size / 1048576).toFixed(2);
            textEl.textContent = `${file.name} (${sizeMB} MB)`;
            display.style.borderColor = 'var(--success)';
        } else {
            textEl.textContent = 'Pilih file PDF...';
            display.style.borderColor = '';
        }
    });
}

// Upload form
const uploadForm = document.getElementById('uploadForm');
if (uploadForm) {
    uploadForm.addEventListener('submit', function(e) {
        e.preventDefault();

        const nameInput = document.getElementById('examName');
        const fileInput = document.getElementById('pdfFile');
        const btn = document.getElementById('btnUpload');
        const progressDiv = document.getElementById('uploadProgress');
        const progressFill = document.getElementById('progressFill');
        const progressText = document.getElementById('progressText');

        if (!nameInput.value.trim()) {
            showToast('Nama ujian wajib diisi', 'error');
            return;
        }
        if (!fileInput.files.length) {
            showToast('Pilih file PDF terlebih dahulu', 'error');
            return;
        }

        const formData = new FormData();
        formData.append('name', nameInput.value.trim());
        formData.append('pdf_file', fileInput.files[0]);

        btn.disabled = true;
        btn.textContent = 'Mengupload...';
        progressDiv.style.display = 'flex';

        const xhr = new XMLHttpRequest();
        xhr.open('POST', '/admin/api/upload');

        xhr.upload.addEventListener('progress', function(e) {
            if (e.lengthComputable) {
                const pct = Math.round((e.loaded / e.total) * 100);
                progressFill.style.width = pct + '%';
                progressText.textContent = pct + '%';
            }
        });

        xhr.addEventListener('load', function() {
            btn.disabled = false;
            btn.innerHTML = '<span>Upload Ujian</span>';
            try {
                const res = JSON.parse(xhr.responseText);
                if (res.success) {
                    showToast(res.message, 'success');
                    setTimeout(() => location.reload(), 1000);
                } else {
                    showToast(res.message || 'Upload gagal', 'error');
                    progressDiv.style.display = 'none';
                    progressFill.style.width = '0';
                }
            } catch {
                showToast('Terjadi kesalahan saat upload', 'error');
                progressDiv.style.display = 'none';
            }
        });

        xhr.addEventListener('error', function() {
            btn.disabled = false;
            btn.innerHTML = '<span>Upload Ujian</span>';
            progressDiv.style.display = 'none';
            showToast('Koneksi gagal. Periksa jaringan Anda.', 'error');
        });

        xhr.send(formData);
    });
}

// Toggle exam status
function toggleExam(examId) {
    fetch(`/admin/api/exams/${examId}/toggle`, { method: 'POST' })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                setTimeout(() => location.reload(), 800);
            } else {
                showToast(res.message || 'Gagal mengubah status', 'error');
            }
        })
        .catch(() => showToast('Koneksi gagal', 'error'));
}

// Delete exam
function deleteExam(examId, examName) {
    if (!confirm(`Hapus ujian "${examName}"?\nFile PDF juga akan dihapus permanen.`)) return;

    fetch(`/admin/api/exams/${examId}`, { method: 'DELETE' })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                const row = document.getElementById(`exam-row-${examId}`);
                if (row) {
                    row.style.opacity = '0';
                    row.style.transform = 'translateX(-20px)';
                    row.style.transition = 'all 0.3s';
                    setTimeout(() => {
                        row.remove();
                        location.reload();
                    }, 300);
                }
            } else {
                showToast(res.message || 'Gagal menghapus ujian', 'error');
            }
        })
        .catch(() => showToast('Koneksi gagal', 'error'));
}
