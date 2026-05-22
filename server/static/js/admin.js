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

// Copy token to clipboard
function copyToken(token) {
    if (!token || token === '—') {
        showToast('Token belum tersedia', 'error');
        return;
    }
    navigator.clipboard.writeText(token).then(() => {
        showToast(`Token "${token}" berhasil disalin`, 'success');
    }).catch(() => {
        // Fallback for older browsers
        const textarea = document.createElement('textarea');
        textarea.value = token;
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        textarea.remove();
        showToast(`Token "${token}" berhasil disalin`, 'success');
    });
}

// Regenerate token
function regenerateToken(examId) {
    if (!confirm('Generate token baru? Token lama tidak akan bisa digunakan lagi.')) return;

    fetch(`/admin/api/exams/${examId}/regenerate-token`, { method: 'POST' })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                const tokenEl = document.getElementById(`token-${examId}`);
                if (tokenEl) {
                    tokenEl.textContent = res.token;
                    tokenEl.style.animation = 'none';
                    tokenEl.offsetHeight; // force reflow
                    tokenEl.style.animation = 'toastIn 0.3s ease';
                }
                showToast(res.message, 'success');
            } else {
                showToast(res.message || 'Gagal regenerate token', 'error');
            }
        })
        .catch(() => showToast('Koneksi gagal', 'error'));
}

// Global modal state
let activeExamId = null;

function openQuestionsModal(examId, examName) {
    activeExamId = examId;
    document.getElementById('modalTitle').textContent = `Atur Soal Ujian: ${examName}`;
    const container = document.getElementById('questionsList');
    container.innerHTML = '<div style="color:var(--text-secondary); text-align:center; padding: 20px;">Memuat data soal...</div>';
    
    // Open modal first
    document.getElementById('questionsModal').style.display = 'flex';
    
    fetch(`/admin/api/exams/${examId}/questions`)
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                renderQuestions(res.questions);
            } else {
                showToast(res.message || 'Gagal memuat soal', 'error');
            }
        })
        .catch(() => showToast('Gagal memuat data soal', 'error'));
}

function closeQuestionsModal() {
    document.getElementById('questionsModal').style.display = 'none';
    activeExamId = null;
}

function renderQuestions(questions) {
    const container = document.getElementById('questionsList');
    container.innerHTML = '';
    
    if (!questions || questions.length === 0) {
        container.innerHTML = '<div style="color:var(--text-muted); text-align:center; padding: 20px;">Belum ada konfigurasi soal. Silakan gunakan generator otomatis di atas untuk membuat default soal.</div>';
        return;
    }
    
    questions.forEach((q, index) => {
        const num = q.number || (index + 1);
        const type = q.type || 'single_choice';
        const key = q.key || '';
        
        let optionsVal = '';
        if (type === 'single_choice' || type === 'multiple_choice') {
            optionsVal = q.choices ? q.choices.join(', ') : 'A, B, C, D, E';
        } else if (type === 'matching') {
            optionsVal = q.left_items && q.right_items ? `Kiri: ${q.left_items.join(', ')} | Kanan: ${q.right_items.join(', ')}` : 'Kiri: 1, 2, 3 | Kanan: A, B, C';
        }
        
        let keyVal = '';
        if (Array.isArray(key)) {
            keyVal = key.join(', ');
        } else if (typeof key === 'object' && key !== null) {
            keyVal = Object.keys(key).map(k => `${k}:${key[k]}`).join(', ');
        } else {
            keyVal = key;
        }

        const weight = q.weight !== undefined ? q.weight : 1.0;

        const card = document.createElement('div');
        card.className = 'question-editor-card';
        card.innerHTML = `
            <span class="q-num-badge">No. ${num}</span>
            <input type="hidden" class="q-number" value="${num}">
            <div class="q-field-group">
                <label>Tipe</label>
                <select class="q-type-select" onchange="onQuestionTypeChange(this)">
                    <option value="single_choice" ${type === 'single_choice' ? 'selected' : ''}>Pilihan Ganda (Single)</option>
                    <option value="multiple_choice" ${type === 'multiple_choice' ? 'selected' : ''}>Pilihan Ganda Kompleks</option>
                    <option value="true_false" ${type === 'true_false' ? 'selected' : ''}>Benar / Salah</option>
                    <option value="matching" ${type === 'matching' ? 'selected' : ''}>Menjodohkan (Matching)</option>
                </select>
            </div>
            <div class="q-field-group">
                <label>Bobot</label>
                <input type="number" class="q-weight-input" value="${weight}" step="0.5" min="0" placeholder="1.0">
            </div>
            <div class="q-field-group">
                <label>Kunci Jawaban</label>
                <input type="text" class="q-key-input" value="${keyVal}" placeholder="A / A,C / TRUE / 1:A, 2:B" title="Pilihan Kompleks (koma), Menjodohkan (K:V)">
            </div>
            <div class="q-field-group">
                <label>Pilihan / Konfigurasi Item</label>
                <input type="text" class="q-options-input" value="${optionsVal}" placeholder="Pilihan dipisahkan koma">
            </div>
            <button class="btn-remove-q" onclick="this.parentElement.remove()" title="Hapus Soal">✕</button>
        `;
        container.appendChild(card);
    });
}

function onQuestionTypeChange(selectEl) {
    const card = selectEl.closest('.question-editor-card');
    const optionsInput = card.querySelector('.q-options-input');
    const keyInput = card.querySelector('.q-key-input');
    const type = selectEl.value;
    
    if (type === 'single_choice') {
        optionsInput.value = 'A, B, C, D, E';
        keyInput.value = 'A';
    } else if (type === 'multiple_choice') {
        optionsInput.value = 'A, B, C, D, E';
        keyInput.value = 'A, C';
    } else if (type === 'true_false') {
        optionsInput.value = '';
        keyInput.value = 'TRUE';
    } else if (type === 'matching') {
        optionsInput.value = 'Kiri: 1, 2, 3 | Kanan: A, B, C';
        keyInput.value = '1:A, 2:B, 3:C';
    }
}

function quickGenerateQuestions() {
    const qty = parseInt(document.getElementById('generateQty').value) || 40;
    const type = document.getElementById('generateType').value;
    
    const questions = [];
    for (let i = 1; i <= qty; i++) {
        let q = { number: i, type: type, weight: 1.0 };
        if (type === 'single_choice') {
            q.choices = ['A', 'B', 'C', 'D', 'E'];
            q.key = 'A';
        } else if (type === 'multiple_choice') {
            q.choices = ['A', 'B', 'C', 'D', 'E'];
            q.key = ['A'];
        } else if (type === 'true_false') {
            q.key = 'TRUE';
        } else if (type === 'matching') {
            q.left_items = ['1', '2', '3'];
            q.right_items = ['A', 'B', 'C'];
            q.key = { '1': 'A', '2': 'B', '3': 'C' };
        }
        questions.push(q);
    }
    renderQuestions(questions);
}

function saveQuestionsConfig() {
    if (!activeExamId) return;
    
    const cards = document.querySelectorAll('.question-editor-card');
    const questions = [];
    
    for (let card of cards) {
        const number = parseInt(card.querySelector('.q-number').value);
        const type = card.querySelector('.q-type-select').value;
        const keyRaw = card.querySelector('.q-key-input').value.trim();
        const optionsRaw = card.querySelector('.q-options-input').value.trim();
        const weight = parseFloat(card.querySelector('.q-weight-input').value) || 1.0;
        
        let q = { number: number, type: type, weight: weight };
        
        if (type === 'single_choice' || type === 'multiple_choice') {
            q.choices = optionsRaw.split(',').map(x => x.trim()).filter(x => x);
            if (q.choices.length === 0) q.choices = ['A', 'B', 'C', 'D', 'E'];
        } else if (type === 'matching') {
            const parts = optionsRaw.split('|');
            let left = ['1', '2', '3'];
            let right = ['A', 'B', 'C'];
            
            parts.forEach(p => {
                const sub = p.split(':');
                if (sub.length === 2) {
                    const label = sub[0].trim().toLowerCase();
                    const val = sub[1].split(',').map(x => x.trim()).filter(x => x);
                    if (label.includes('kiri')) left = val;
                    else if (label.includes('kanan')) right = val;
                }
            });
            q.left_items = left;
            q.right_items = right;
        }
        
        if (type === 'single_choice') {
            q.key = keyRaw.toUpperCase();
        } else if (type === 'multiple_choice') {
            q.key = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
        } else if (type === 'true_false') {
            q.key = keyRaw.toUpperCase();
        } else if (type === 'matching') {
            const keyObj = {};
            const pairs = keyRaw.split(',');
            pairs.forEach(pair => {
                const item = pair.split(':');
                if (item.length === 2) {
                    keyObj[item[0].trim()] = item[1].trim().toUpperCase();
                }
            });
            q.key = keyObj;
        }
        
        questions.push(q);
    }
    
    fetch(`/admin/api/exams/${activeExamId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ questions: questions })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                closeQuestionsModal();
            } else {
                showToast(res.message || 'Gagal menyimpan konfigurasi', 'error');
            }
        })
        .catch(() => showToast('Gagal menyimpan konfigurasi', 'error'));
}

