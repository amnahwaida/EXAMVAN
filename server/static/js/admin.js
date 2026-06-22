/* EXAMVAN Admin Panel - JavaScript */

// CSRF Token Helper
function getCsrfToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute('content') : '';
}

// Wrapper for fetch that auto-includes CSRF headers on state-changing methods
function apiFetch(url, options = {}) {
    const method = (options.method || 'GET').toUpperCase();
    if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
        options.headers = options.headers || {};
        options.headers['X-CSRF-Token'] = getCsrfToken();
    }
    return window.fetch.call(window, url, options);
}

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

        const customTokenInput = document.getElementById('customToken');

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
        if (customTokenInput && customTokenInput.value.trim()) {
            const tokenVal = customTokenInput.value.trim().toUpperCase();
            if (tokenVal.length !== 6 || !/^[A-Z0-9]+$/.test(tokenVal)) {
                showToast('Token kustom harus terdiri dari 6 karakter alfanumerik', 'error');
                return;
            }
            formData.append('custom_token', tokenVal);
        }

        btn.disabled = true;
        btn.textContent = 'Mengupload...';
        progressDiv.style.display = 'flex';

        const xhr = new XMLHttpRequest();
        xhr.open('POST', '/admin/api/upload');
        xhr.setRequestHeader('X-CSRF-Token', getCsrfToken());

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
    apiFetch(`/admin/api/exams/${examId}/toggle`, { method: 'POST' })
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

    apiFetch(`/admin/api/exams/${examId}`, { method: 'DELETE' })
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

// Copy results short link to clipboard
function copyResultsLink(token) {
    if (!token || token === '—') {
        showToast('Token belum tersedia', 'error');
        return;
    }
    const link = window.location.origin + '/hasil/' + token;
    navigator.clipboard.writeText(link).then(() => {
        showToast(`Link hasil ujian berhasil disalin: ${link}`, 'success');
    }).catch(() => {
        const textarea = document.createElement('textarea');
        textarea.value = link;
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        textarea.remove();
        showToast(`Link hasil ujian berhasil disalin: ${link}`, 'success');
    });
}

// Regenerate token
function regenerateToken(examId) {
    if (!confirm('Generate token baru? Token lama tidak akan bisa digunakan lagi.')) return;

    apiFetch(`/admin/api/exams/${examId}/regenerate-token`, { method: 'POST' })
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
let activeExamName = '';

function openQuestionsModal(examId, examName) {
    activeExamId = examId;
    activeExamName = examName;
    document.getElementById('modalTitle').textContent = `Atur Soal Ujian: ${examName}`;
    const container = document.getElementById('questionsList');
    container.innerHTML = '<div style="color:var(--text-secondary); text-align:center; padding: 20px;">Memuat data soal...</div>';
    
    // Open modal first
    document.getElementById('questionsModal').style.display = 'flex';
    
    apiFetch(`/admin/api/exams/${examId}/questions`)
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                const secSelect = document.getElementById('examSecurityLevel');
                if (secSelect) {
                    secSelect.value = res.security_level || 'medium';
                }
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
    activeExamName = '';
}

function createNewQuestionCard(q, num) {
    const type = q.type || 'single_choice';
    const key = q.key || '';
    const weight = q.weight !== undefined ? q.weight : 1.0;
    const partial = q.partial_scoring ? 'checked' : '';
    const partialVisibility = (type === 'multiple_choice' || type === 'matching') ? 'block' : 'none';
    const optionsVisibility = (type === 'true_false' || type === 'short_answer') ? 'none' : 'block';
    
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

    const card = document.createElement('div');
    card.className = 'question-editor-card';
    card.innerHTML = `
        <span class="q-num-badge">No. ${num}</span>
        <input type="hidden" class="q-number" value="${num}">
        <div class="q-card-body">
            <div class="q-field-group">
                <label>Tipe</label>
                <select class="q-type-select" onchange="onQuestionTypeChange(this)">
                    <option value="single_choice" ${type === 'single_choice' ? 'selected' : ''}>Pilihan Ganda</option>
                    <option value="multiple_choice" ${type === 'multiple_choice' ? 'selected' : ''}>PG Kompleks</option>
                    <option value="true_false" ${type === 'true_false' ? 'selected' : ''}>Benar / Salah</option>
                    <option value="matching" ${type === 'matching' ? 'selected' : ''}>Menjodohkan</option>
                    <option value="short_answer" ${type === 'short_answer' ? 'selected' : ''}>Isian Singkat</option>
                </select>
            </div>
            <div class="q-field-group">
                <label>Bobot</label>
                <input type="number" class="q-weight-input" value="${weight}" step="0.5" min="0" placeholder="1.0">
            </div>
            <div class="q-field-group q-partial-group" style="display: ${partialVisibility};">
                <label>
                    <input type="checkbox" class="q-partial-checkbox" ${partial}> Parsial
                </label>
            </div>
            <div class="q-field-group">
                <label>Kunci Jawaban</label>
                <input type="text" class="q-key-input" value="${keyVal}" placeholder="Jawaban..." title="PG: A,B,C | Menjodohkan: 1:A,2:B">
            </div>
            <div class="q-field-group q-options-group" style="display: ${optionsVisibility};">
                <label>Pilihan</label>
                <input type="text" class="q-options-input" value="${optionsVal}" placeholder="A, B, C, D, E">
            </div>
        </div>
        <button class="btn-sm btn-delete btn-remove-q" onclick="removeQuestionCard(this)" title="Hapus Soal">🗑️</button>
    `;
    return card;
}

function createDivider(index) {
    const div = document.createElement('div');
    div.className = 'q-editor-divider';
    div.dataset.index = index;
    div.innerHTML = `
        <div class="q-divider-line"></div>
        <button class="btn-add-inline" onclick="insertQuestionAt(${index})" title="Sisipkan Soal Baru Di Sini">➕ Sisipkan Soal</button>
        <div class="q-divider-line"></div>
    `;
    return div;
}

function insertQuestionAt(index) {
    const container = document.getElementById('questionsList');
    const newQ = { type: 'single_choice', weight: 1.0 };
    const newCard = createNewQuestionCard(newQ, 0);
    const newDivider = createDivider(0);
    
    const dividers = Array.from(container.querySelectorAll('.q-editor-divider'));
    const targetDivider = dividers.find(d => d.dataset.index == index);
    if (targetDivider) {
        const nextNode = targetDivider.nextSibling;
        if (nextNode) {
            container.insertBefore(newCard, nextNode);
            container.insertBefore(newDivider, newCard.nextSibling);
        } else {
            container.appendChild(newCard);
            container.appendChild(newDivider);
        }
    } else {
        container.appendChild(newCard);
        container.appendChild(newDivider);
    }
    reindexQuestions();
}

function removeQuestionCard(btn) {
    if (!confirm('Hapus soal ini?')) return;
    const card = btn.closest('.question-editor-card');
    const divider = card.nextSibling;
    if (divider && divider.classList && divider.classList.contains('q-editor-divider')) {
        divider.remove();
    }
    card.remove();
    reindexQuestions();
}

function reindexQuestions() {
    const container = document.getElementById('questionsList');
    const children = Array.from(container.children);
    
    let currentNum = 1;
    children.forEach(child => {
        if (child.classList.contains('question-editor-card')) {
            child.querySelector('.q-num-badge').textContent = `No. ${currentNum}`;
            child.querySelector('.q-number').value = currentNum;
            currentNum++;
        }
    });
    
    let dividerCount = 0;
    children.forEach(child => {
        if (child.classList.contains('q-editor-divider')) {
            child.dataset.index = dividerCount;
            const btn = child.querySelector('.btn-add-inline');
            if (btn) {
                btn.setAttribute('onclick', `insertQuestionAt(${dividerCount})`);
            }
            dividerCount++;
        }
    });
}

function setAllWeights() {
    const weightInputs = document.querySelectorAll('.q-weight-input');
    if (weightInputs.length === 0) {
        showToast('Tidak ada soal untuk diatur bobotnya', 'error');
        return;
    }

    const currentWeight = weightInputs[0].value || '1.0';

    // Build modal
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.style.display = 'flex';

    const card = document.createElement('div');
    card.className = 'modal-card';
    card.style.maxWidth = '420px';
    card.innerHTML = `
        <div class="modal-header">
            <h3>⚖️ Set Bobot Semua Soal</h3>
            <button class="modal-close" onclick="this.closest('.modal-overlay').remove()">✕</button>
        </div>
        <div class="modal-body bulk-weight-body">
            <p class="bulk-weight-desc">
                Masukkan bobot nilai yang akan diterapkan ke <strong>${weightInputs.length} soal</strong>:
            </p>
            <input type="number" id="bulkWeightInput" class="bulk-weight-input" value="${currentWeight}" step="0.5" min="0">
            <label class="bulk-weight-label">
                <input type="checkbox" id="bulkWeightIncludePartial" checked>
                Termasuk soal parsial
            </label>
        </div>
        <div class="modal-footer bulk-weight-footer">
            <button class="btn-sm" onclick="this.closest('.modal-overlay').remove()">Batal</button>
            <button class="btn-upload" onclick="applyBulkWeight(this)">Terapkan</button>
        </div>
    `;
    overlay.appendChild(card);
    document.body.appendChild(overlay);

    // Focus input and select all text
    const input = document.getElementById('bulkWeightInput');
    input.focus();
    input.select();
}

function applyBulkWeight(btn) {
    const overlay = btn.closest('.modal-overlay');
    const input = document.getElementById('bulkWeightInput');
    const includePartial = document.getElementById('bulkWeightIncludePartial').checked;

    const parsed = parseFloat(input.value);
    if (isNaN(parsed) || parsed < 0) {
        showToast('Bobot nilai harus berupa angka positif', 'error');
        input.focus();
        input.select();
        return;
    }

    const weightInputs = document.querySelectorAll('.q-weight-input');
    weightInputs.forEach((inputEl, idx) => {
        // If partial checkbox is unchecked, skip questions with partial scoring enabled
        if (!includePartial) {
            const card = inputEl.closest('.question-editor-card');
            const partialCheckbox = card ? card.querySelector('.q-partial-checkbox') : null;
            if (partialCheckbox && partialCheckbox.checked) return;
        }
        inputEl.value = parsed;
    });

    overlay.remove();
    showToast(`Bobot ${weightInputs.length} soal diubah menjadi ${parsed}`, 'success');
}

function renderQuestions(questions) {
    const container = document.getElementById('questionsList');
    container.innerHTML = '';
    
    if (!questions || questions.length === 0) {
        container.innerHTML = '<div style="color:var(--text-muted); text-align:center; padding: 16px; font-size: 13px;">Tidak ada soal dikonfigurasi. Ujian akan tampil sebagai PDF saja tanpa overlay jawaban.</div>';
        container.appendChild(createDivider(0));
        return;
    }
    
    container.appendChild(createDivider(0));
    
    questions.forEach((q, index) => {
        const num = index + 1;
        const card = createNewQuestionCard(q, num);
        container.appendChild(card);
        container.appendChild(createDivider(num));
    });
}

function onQuestionTypeChange(selectEl) {
    const card = selectEl.closest('.question-editor-card');
    const optionsInput = card.querySelector('.q-options-input');
    const optionsGroup = card.querySelector('.q-options-group');
    const keyInput = card.querySelector('.q-key-input');
    const partialGroup = card.querySelector('.q-partial-group');
    const type = selectEl.value;
    
    if (type === 'multiple_choice' || type === 'matching') {
        partialGroup.style.display = 'block';
    } else {
        partialGroup.style.display = 'none';
        card.querySelector('.q-partial-checkbox').checked = false;
    }
    
    if (type === 'true_false' || type === 'short_answer') {
        optionsGroup.style.display = 'none';
    } else {
        optionsGroup.style.display = 'block';
    }
    
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
    } else if (type === 'short_answer') {
        optionsInput.value = '';
        keyInput.value = '';
    }
}

function quickGenerateQuestions() {
    const rawQty = parseInt(document.getElementById('generateQty').value);
    const qty = isNaN(rawQty) ? 40 : rawQty;
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
        } else if (type === 'short_answer') {
            q.key = '';
        }
        questions.push(q);
    }
    renderQuestions(questions);
}

function getQuestionsFromEditor() {
    const cards = document.querySelectorAll('.question-editor-card');
    const questions = [];
    
    for (let card of cards) {
        const number = parseInt(card.querySelector('.q-number').value);
        const type = card.querySelector('.q-type-select').value;
        const keyRaw = card.querySelector('.q-key-input').value.trim();
        const optionsRaw = card.querySelector('.q-options-input').value.trim();
        const weight = parseFloat(card.querySelector('.q-weight-input').value) || 1.0;
        const partialCheckbox = card.querySelector('.q-partial-checkbox');
        const partialScoring = (type === 'multiple_choice' || type === 'matching') && partialCheckbox ? partialCheckbox.checked : false;
        
        let q = { number: number, type: type, weight: weight, partial_scoring: partialScoring };
        
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
        } else if (type === 'short_answer') {
            q.key = keyRaw;
        }
        
        questions.push(q);
    }
    
    questions.sort((a, b) => a.number - b.number);
    return questions;
}

function exportXMLQuestions() {
    const questions = getQuestionsFromEditor();
    if (questions.length === 0) {
        showToast("Tidak ada soal untuk diexport", "error");
        return;
    }
    
    let xml = '<?xml version="1.0" encoding="UTF-8"?>\n';
    xml += '<questions>\n';
    
    questions.forEach(q => {
        const partialAttr = (q.type === 'multiple_choice' || q.type === 'matching') ? ` partial_scoring="${q.partial_scoring}"` : '';
        xml += `    <question number="${q.number}" type="${q.type}" weight="${q.weight.toFixed(1)}"${partialAttr}>\n`;
        
        if (q.type === 'single_choice' || q.type === 'multiple_choice') {
            if (q.choices && q.choices.length > 0) {
                xml += `        <choices>${q.choices.join(', ')}</choices>\n`;
            }
        } else if (q.type === 'matching') {
            if (q.left_items && q.left_items.length > 0) {
                xml += `        <left_items>${q.left_items.join(', ')}</left_items>\n`;
            }
            if (q.right_items && q.right_items.length > 0) {
                xml += `        <right_items>${q.right_items.join(', ')}</right_items>\n`;
            }
        }
        
        let keyStr = '';
        if (q.type === 'multiple_choice' && Array.isArray(q.key)) {
            keyStr = q.key.join(', ');
        } else if (q.type === 'matching' && q.key && typeof q.key === 'object') {
            const pairs = [];
            for (const [k, v] of Object.entries(q.key)) {
                pairs.push(`${k}:${v}`);
            }
            keyStr = pairs.join(', ');
        } else {
            keyStr = q.key || '';
        }
        
        xml += `        <key>${keyStr}</key>\n`;
        xml += `    </question>\n`;
    });
    
    xml += '</questions>\n';
    
    const blob = new Blob([xml], { type: 'application/xml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    const safeName = (activeExamName || 'ujian').replace(/[^a-z0-9]/gi, '_').toLowerCase();
    a.href = url;
    a.download = `${safeName}_kunci_jawaban.xml`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    
    showToast(`Berhasil mengekspor ${questions.length} soal ke XML!`, "success");
}

function saveQuestionsConfig() {
    if (!activeExamId) return;
    
    const questions = getQuestionsFromEditor();
    const securityLevel = document.getElementById('examSecurityLevel') ? document.getElementById('examSecurityLevel').value : 'medium';
    
    apiFetch(`/admin/api/exams/${activeExamId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ questions: questions, security_level: securityLevel })
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


// ===== Change Password Modal =====

function openChangePasswordModal() {
    const modal = document.getElementById('changePasswordModal');
    if (modal) {
        modal.style.display = 'flex';
        const form = document.getElementById('changePasswordForm');
        if (form) form.reset();
    }
}

function closeChangePasswordModal() {
    const modal = document.getElementById('changePasswordModal');
    if (modal) modal.style.display = 'none';
}

function submitChangePassword(e) {
    e.preventDefault();
    const currentPassword = document.getElementById('currentPassword').value;
    const newPassword = document.getElementById('newPassword').value;
    const confirmPassword = document.getElementById('confirmNewPassword').value;

    if (newPassword !== confirmPassword) {
        showToast('Password baru dan konfirmasi tidak cocok', 'error');
        return;
    }

    if (newPassword.length < 4) {
        showToast('Password baru minimal 4 karakter', 'error');
        return;
    }

    apiFetch('/admin/api/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            current_password: currentPassword,
            new_password: newPassword
        })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                closeChangePasswordModal();
            } else {
                showToast(res.message || 'Gagal mengubah password', 'error');
            }
        })
        .catch(() => showToast('Gagal mengubah password', 'error'));
}


// ===== Manage Users Modal (Super Admin Only) =====

function openManageUsersModal() {
    const modal = document.getElementById('manageUsersModal');
    if (modal) {
        modal.style.display = 'flex';
        loadUsersList();
    }
}

function closeManageUsersModal() {
    const modal = document.getElementById('manageUsersModal');
    if (modal) modal.style.display = 'none';
}

function loadUsersList() {
    const tbody = document.getElementById('usersListBody');
    if (!tbody) return;

    tbody.innerHTML = '<tr><td colspan="3" style="text-align:center; padding: 20px; color: var(--text-secondary);">Memuat...</td></tr>';

    apiFetch('/admin/api/users')
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                if (res.users.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="3" style="text-align:center; padding: 20px; color: var(--text-secondary);">Belum ada user terdaftar</td></tr>';
                    return;
                }
                tbody.innerHTML = '';
                res.users.forEach(user => {
                    const tr = document.createElement('tr');
                    const isAdmin = user.username === 'admin';
                    tr.innerHTML = `
                        <td>
                            <strong style="color: ${isAdmin ? 'var(--accent-light)' : 'var(--text-color)'};">
                                ${isAdmin ? '👑 ' : ''}${user.username}
                            </strong>
                            ${isAdmin ? '<span style="font-size:11px; color: var(--text-secondary); display:block;">Super Admin</span>' : ''}
                        </td>
                        <td class="td-date" data-utc="${user.created_at || ''}" style="font-size: 12px; color: var(--text-secondary);">${user.created_at || '—'}</td>
                        <td>
                            ${isAdmin
                                ? '<span style="font-size:11px; color: var(--text-secondary);">—</span>'
                                : `<button class="btn-sm btn-delete" onclick="deleteUser(${user.id}, '${user.username}')" style="font-size: 11px; padding: 0 8px; height: 26px;">🗑️ Hapus</button>`
                            }
                        </td>
                    `;
                    tbody.appendChild(tr);
                });
                localizeDates();
            } else {
                tbody.innerHTML = '<tr><td colspan="3" style="text-align:center; padding: 20px; color: #fca5a5;">Gagal memuat daftar user</td></tr>';
            }
        })
        .catch(() => {
            tbody.innerHTML = '<tr><td colspan="3" style="text-align:center; padding: 20px; color: #fca5a5;">Gagal memuat daftar user</td></tr>';
        });
}

function submitCreateUser(e) {
    e.preventDefault();
    const username = document.getElementById('newUsername').value.trim();
    const password = document.getElementById('newUserPassword').value;

    if (!username || !password) {
        showToast('Username dan password wajib diisi', 'error');
        return;
    }

    apiFetch('/admin/api/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                document.getElementById('createUserForm').reset();
                loadUsersList();
            } else {
                showToast(res.message || 'Gagal membuat user', 'error');
            }
        })
        .catch(() => showToast('Gagal membuat user', 'error'));
}

function deleteUser(userId, username) {
    if (!confirm(`Hapus user "${username}"? Semua ujian dan data yang dibuat oleh user ini akan ikut terhapus.`)) return;

    apiFetch(`/admin/api/users/${userId}`, {
        method: 'DELETE'
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                loadUsersList();
            } else {
                showToast(res.message || 'Gagal menghapus user', 'error');
            }
        })
        .catch(() => showToast('Gagal menghapus user', 'error'));
}


// Edit Token Modal
function openEditTokenModal(examId, currentToken) {
    document.getElementById('editTokenExamId').value = examId;
    document.getElementById('editTokenInput').value = currentToken && currentToken !== '—' ? currentToken : '';
    document.getElementById('editTokenModal').style.display = 'flex';
    setTimeout(() => document.getElementById('editTokenInput').focus(), 100);
}

function closeEditTokenModal() {
    document.getElementById('editTokenModal').style.display = 'none';
    document.getElementById('editTokenForm').reset();
}

function submitEditToken(e) {
    e.preventDefault();
    const examId = document.getElementById('editTokenExamId').value;
    const token = document.getElementById('editTokenInput').value.trim().toUpperCase();

    if (token.length !== 6 || !/^[A-Z0-9]+$/.test(token)) {
        showToast('Token kustom harus terdiri dari 6 karakter alfanumerik', 'error');
        return;
    }

    apiFetch(`/admin/api/exams/${examId}/custom-token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                const tokenEl = document.getElementById(`token-${examId}`);
                if (tokenEl) {
                    tokenEl.textContent = res.token;
                    
                    // Update the edit button argument as well
                    const editBtn = tokenEl.parentElement.querySelector('.btn-edit');
                    if (editBtn) {
                        editBtn.setAttribute('onclick', `openEditTokenModal(${examId}, '${res.token}')`);
                    }
                    
                    tokenEl.style.animation = 'none';
                    tokenEl.offsetHeight; // force reflow
                    tokenEl.style.animation = 'toastIn 0.3s ease';
                }
                showToast(res.message, 'success');
                closeEditTokenModal();
            } else {
                showToast(res.message || 'Gagal mengubah token', 'error');
            }
        })
        .catch(() => showToast('Koneksi gagal', 'error'));
}


// ===== Close modals on overlay click =====
document.addEventListener('click', function(e) {
    if (e.target.classList.contains('modal-overlay')) {
        // Close any open modal when clicking on overlay background
        e.target.style.display = 'none';
    }
});

// Helper to localize a single UTC date string to the device's local timezone
function localizeUTC(rawDate) {
    if (!rawDate || rawDate === '—') return '—';
    // Parse as UTC (format from SQLite: YYYY-MM-DD HH:MM:SS or YYYY-MM-DDTHH:MM:SSZ)
    let isoString = rawDate;
    if (!isoString.includes('T')) {
        isoString = isoString.replace(' ', 'T');
    }
    if (!isoString.endsWith('Z')) {
        isoString = isoString + 'Z';
    }
    const date = new Date(isoString);
    if (isNaN(date.getTime())) return rawDate;
    
    return date.toLocaleString(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
}

// Localize dates from UTC to browser/device timezone
function localizeDates() {
    document.querySelectorAll('.td-date').forEach(el => {
        const rawDate = el.dataset.utc || el.textContent.trim();
        if (rawDate && rawDate !== '—' && !rawDate.includes('WIB') && !rawDate.includes('GMT') && !rawDate.includes('UTC')) {
            // Save original UTC raw string in dataset if not already present
            if (!el.dataset.utc) {
                el.dataset.utc = rawDate;
            }
            el.textContent = localizeUTC(rawDate);
        }
    });
}

document.addEventListener('DOMContentLoaded', localizeDates);

function importXMLQuestions(event) {
    const file = event.target.files[0];
    if (!file) return;
    
    const reader = new FileReader();
    reader.onload = function(e) {
        try {
            const parser = new DOMParser();
            const xmlDoc = parser.parseFromString(e.target.result, "text/xml");
            
            // Check for parse errors
            const parseError = xmlDoc.getElementsByTagName("parsererror");
            if (parseError.length > 0) {
                showToast("Format XML tidak valid atau rusak", "error");
                return;
            }
            
            const questionNodes = xmlDoc.getElementsByTagName("question");
            if (questionNodes.length === 0) {
                showToast("Tidak ditemukan elemen <question> dalam XML", "error");
                return;
            }
            
            const questions = [];
            for (let i = 0; i < questionNodes.length; i++) {
                const node = questionNodes[i];
                const number = parseInt(node.getAttribute("number")) || (i + 1);
                const type = node.getAttribute("type") || "single_choice";
                const weight = parseFloat(node.getAttribute("weight")) || 1.0;
                const partialScoring = node.getAttribute("partial_scoring") === "true";
                
                let choices = [];
                const choicesNode = node.getElementsByTagName("choices")[0];
                if (choicesNode) {
                    choices = choicesNode.textContent.split(',').map(x => x.trim()).filter(x => x);
                }
                
                let left_items = [];
                const leftNode = node.getElementsByTagName("left_items")[0];
                if (leftNode) {
                    left_items = leftNode.textContent.split(',').map(x => x.trim()).filter(x => x);
                }
                
                let right_items = [];
                const rightNode = node.getElementsByTagName("right_items")[0];
                if (rightNode) {
                    right_items = rightNode.textContent.split(',').map(x => x.trim()).filter(x => x);
                }
                
                let key = '';
                const keyNode = node.getElementsByTagName("key")[0];
                if (keyNode) {
                    const keyRaw = keyNode.textContent.trim();
                    if (type === 'multiple_choice') {
                        key = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
                    } else if (type === 'matching') {
                        const keyObj = {};
                        const pairs = keyRaw.split(',');
                        pairs.forEach(pair => {
                            const item = pair.split(':');
                            if (item.length === 2) {
                                keyObj[item[0].trim()] = item[1].trim().toUpperCase();
                            }
                        });
                        key = keyObj;
                    } else if (type === 'short_answer') {
                        key = keyRaw;
                    } else {
                        key = keyRaw.toUpperCase();
                    }
                }
                
                questions.push({
                    number: number,
                    type: type,
                    weight: weight,
                    partial_scoring: partialScoring,
                    choices: choices,
                    left_items: left_items,
                    right_items: right_items,
                    key: key
                });
            }
            
            // Sort by number to ensure sequential ordering
            questions.sort((a, b) => a.number - b.number);
            
            // Re-render questions in UI
            renderQuestions(questions);
            showToast(`Berhasil mengimpor ${questions.length} soal dari XML!`, "success");
        } catch (err) {
            console.error(err);
            showToast("Terjadi kesalahan saat membaca berkas XML", "error");
        }
    };
    reader.readAsText(file);
    // Reset file input value so same file can be re-imported if needed
    event.target.value = '';
}

const AI_PROMPT_CONTENT = `Anda adalah seorang ahli evaluasi pendidikan dan spesialis entri data akademis. Tugas Anda adalah menganalisis dokumen soal ujian (berupa teks atau file PDF soal yang dilampirkan) secara mendalam, memecahkan jawabannya dengan akurasi 100%, lalu mengekstrak serta menyusun kunci jawabannya ke dalam format XML terstruktur yang siap diimpor ke sistem aplikasi EXAMVAN.

Pahamilah aturan format XML EXAMVAN berikut secara detail:

### 1. Struktur Root XML
Semua daftar soal harus dibungkus dalam tag root <questions>...</questions>.

### 2. Atribut Tag <question>
Setiap butir soal ditulis sebagai elemen <question> dengan atribut wajib:
- number: Nomor urut soal (angka bulat positif, misalnya: 1, 2, 3, dst).
- type: Jenis tipe soal, harus bernilai salah satu dari:
  - single_choice (Pilihan Ganda Biasa)
  - multiple_choice (Pilihan Ganda Kompleks)
  - true_false (Benar / Salah)
  - matching (Menjodohkan / Mencocokkan)
  - short_answer (Isian Singkat)
- weight: Bobot nilai soal (default "1.0", bertipe desimal, misal: "1.0", "1.5", "2.0", dst).
- partial_scoring: Nilai parsial untuk tipe multiple_choice atau matching. Bernilai "true" jika siswa mendapat poin proporsional atas jawaban yang sebagian benar, atau "false" jika harus benar seluruhnya.

### 3. Skema Konten Per Tipe Soal

#### A. Tipe single_choice (Pilihan Ganda Tunggal)
- Wajib memiliki tag <choices> berisi daftar opsi pilihan dipisahkan dengan koma (misal: A, B, C, D, E).
- Tag <key> berisi satu huruf kapital opsi jawaban yang benar (misal: A).
Contoh:
<question number="1" type="single_choice" weight="1.0">
    <choices>A, B, C, D, E</choices>
    <key>C</key>
</question>

#### B. Tipe multiple_choice (Pilihan Ganda Kompleks - Jawaban Lebih dari Satu)
- Wajib memiliki tag <choices> berisi daftar opsi pilihan dipisahkan dengan koma (misal: A, B, C, D, E).
- Tag <key> berisi daftar opsi jawaban benar dipisahkan dengan koma (misal: A, C, D).
- Tambahkan atribut partial_scoring="true" jika ingin mengaktifkan penilaian sebagian.
Contoh:
<question number="2" type="multiple_choice" weight="2.0" partial_scoring="true">
    <choices>A, B, C, D, E</choices>
    <key>A, C, D</key>
</question>

#### C. Tipe true_false (Pernyataan Benar / Salah)
- Tidak membutuhkan tag <choices>.
- Tag <key> hanya boleh berisi salah satu dari nilai kapital: TRUE atau FALSE.
Contoh:
<question number="3" type="true_false" weight="1.0">
    <key>TRUE</key>
</question>

#### D. Tipe matching (Menjodohkan / Mencocokkan)
- Wajib memiliki tag <left_items> berisi daftar pertanyaan/item kiri yang dipisahkan koma.
- Wajib memiliki tag <right_items> berisi daftar opsi jawaban kanan yang dipisahkan koma.
- Tag <key> berisi relasi penjodohan dengan format itemKiri:itemKanan dipisahkan koma (misal: 1:B, 2:A, 3:C).
- Tambahkan atribut partial_scoring="true" agar siswa mendapat poin proporsional atas pasangan yang cocok.
Contoh:
<question number="4" type="matching" weight="3.0" partial_scoring="true">
    <left_items>1, 2, 3</left_items>
    <right_items>A, B, C</right_items>
    <key>1:B, 2:A, 3:C</key>
</question>

#### E. Tipe short_answer (Isian Singkat)
- Tidak membutuhkan tag <choices>.
- Tag <key> berisi kata kunci atau frasa jawaban benar yang diharapkan (misal: Fotosintesis atau Jakarta). Sistem akan mencocokkan jawaban siswa secara case-insensitive (mengabaikan huruf besar/kecil) dan membuang spasi di awal/akhir jawaban.
Contoh:
<question number="5" type="short_answer" weight="1.5">
    <key>Fotosintesis</key>
</question>

---

### TUGAS ANDA:
1. Bacalah seluruh soal dari dokumen PDF / teks soal yang saya berikan dengan teliti.
2. Identifikasi tipe masing-masing soal (apakah Pilihan Ganda Tunggal, Pilihan Ganda Kompleks, Benar/Salah, Menjodohkan, atau Isian Singkat).
3. Pecahkan/tentukan kunci jawaban yang paling tepat untuk masing-masing soal tersebut.
4. Tuliskan output kunci jawaban tersebut HANYA dalam format blok kode XML yang utuh dan valid berdasarkan aturan format di atas. Jangan sertakan teks penjelasan lainnya di luar blok kode XML agar mudah disalin langsung.

Mulai analisis dokumen soal ujian berikut:`;

function copyAIPrompt() {
    navigator.clipboard.writeText(AI_PROMPT_CONTENT)
        .then(() => showToast("Prompt AI berhasil disalin ke clipboard!", "success"))
        .catch(() => showToast("Gagal menyalin prompt", "error"));
}

// Toggle public student results access page
function togglePublicResults(examId) {
    const btn = document.getElementById(`btn-public-results-${examId}`);
    if (btn) {
        btn.disabled = true;
    }

    apiFetch(`/admin/api/exams/${examId}/toggle-public-results`, { method: 'POST' })
        .then(r => r.json())
        .then(res => {
            if (btn) {
                btn.disabled = false;
            }
            if (res.success) {
                showToast(res.message, 'success');
                // Dynamically update button appearance and text
                if (btn) {
                    if (res.public_results === 1) {
                        btn.style.background = 'rgba(16, 185, 129, 0.15)';
                        btn.style.borderColor = 'rgba(16, 185, 129, 0.3)';
                        btn.style.color = '#34d399';
                        btn.textContent = '🟢 Hal. Siswa Aktif';
                    } else {
                        btn.style.background = 'rgba(239, 68, 68, 0.15)';
                        btn.style.borderColor = 'rgba(239, 68, 68, 0.3)';
                        btn.style.color = '#f87171';
                        btn.textContent = '🔴 Hal. Siswa Nonaktif';
                    }
                }
            } else {
                showToast(res.message || 'Gagal mengubah akses halaman siswa', 'error');
            }
        })
        .catch(() => {
            if (btn) {
                btn.disabled = false;
            }
            showToast('Koneksi gagal', 'error');
        });
}

// Toggle show answers for students
function toggleShowAnswers(examId) {
    const btn = document.getElementById(`btn-show-answers-${examId}`);
    if (btn) {
        btn.disabled = true;
    }

    apiFetch(`/admin/api/exams/${examId}/toggle-show-answers`, { method: 'POST' })
        .then(r => r.json())
        .then(res => {
            if (btn) {
                btn.disabled = false;
            }
            if (res.success) {
                showToast(res.message, 'success');
                if (btn) {
                    if (res.show_answers === 1) {
                        btn.style.background = 'rgba(251, 191, 36, 0.15)';
                        btn.style.borderColor = 'rgba(251, 191, 36, 0.3)';
                        btn.style.color = '#fbbf24';
                        btn.textContent = '🔓 Kunci Terlihat';
                    } else {
                        btn.style.background = 'rgba(107, 114, 128, 0.15)';
                        btn.style.borderColor = 'rgba(107, 114, 128, 0.3)';
                        btn.style.color = '#9ca3af';
                        btn.textContent = '🔒 Kunci Tersembunyi';
                    }
                }
            } else {
                showToast(res.message || 'Gagal mengubah pengaturan kunci jawaban', 'error');
            }
        })
        .catch(() => {
            if (btn) {
                btn.disabled = false;
            }
            showToast('Koneksi gagal', 'error');
        });
}

// Dropdown Menu Toggle Handler
function initMenuToggle() {
    const menuToggle = document.getElementById('menuToggleBtn');
    const dropdownContent = document.getElementById('menuDropdownContent');
    
    if (menuToggle && dropdownContent) {
        menuToggle.onclick = (e) => {
            e.stopPropagation();
            dropdownContent.classList.toggle('show');
        };
        
        document.addEventListener('click', (e) => {
            if (!menuToggle.contains(e.target) && !dropdownContent.contains(e.target)) {
                dropdownContent.classList.remove('show');
            }
        });
    }
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMenuToggle);
} else {
    initMenuToggle();
}


// ===== Edit Exam Modal =====
function openEditExamModal(examId, examName) {
    const modal = document.getElementById('editExamModal');
    if (!modal) return;
    
    document.getElementById('editExamId').value = examId;
    document.getElementById('editExamName').value = examName;
    
    // Reset file input
    const fileInput = document.getElementById('editPdfFile');
    if (fileInput) fileInput.value = '';
    
    const displayText = document.getElementById('editFileDisplayText');
    if (displayText) displayText.textContent = 'Pilih file PDF baru jika ingin merubah...';
    
    const display = document.getElementById('editFileDisplay');
    if (display) display.style.borderColor = '';
    
    // Hide progress
    const progressDiv = document.getElementById('editUploadProgress');
    if (progressDiv) progressDiv.style.display = 'none';
    
    const progressFill = document.getElementById('editProgressFill');
    if (progressFill) progressFill.style.width = '0%';
    
    modal.style.display = 'flex';
}

function closeEditExamModal() {
    const modal = document.getElementById('editExamModal');
    if (modal) modal.style.display = 'none';
}

function handleEditFileChange(input) {
    const display = document.getElementById('editFileDisplay');
    const textEl = document.getElementById('editFileDisplayText');
    if (!display || !textEl) return;
    
    if (input.files.length > 0) {
        const file = input.files[0];
        const sizeMB = (file.size / 1048576).toFixed(2);
        textEl.textContent = `${file.name} (${sizeMB} MB)`;
        display.style.borderColor = 'var(--warning)';
    } else {
        textEl.textContent = 'Pilih file PDF baru jika ingin merubah...';
        display.style.borderColor = '';
    }
}

function submitEditExam(event) {
    event.preventDefault();
    
    const examId = document.getElementById('editExamId').value;
    const nameInput = document.getElementById('editExamName');
    const fileInput = document.getElementById('editPdfFile');
    const btn = document.getElementById('btnEditExamSave');
    const progressDiv = document.getElementById('editUploadProgress');
    const progressFill = document.getElementById('editProgressFill');
    const progressText = document.getElementById('editProgressText');
    
    if (!nameInput.value.trim()) {
        showToast('Nama ujian wajib diisi', 'error');
        return;
    }
    
    const formData = new FormData();
    formData.append('name', nameInput.value.trim());
    if (fileInput.files.length > 0) {
        formData.append('pdf_file', fileInput.files[0]);
    }
    
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';
    if (fileInput.files.length > 0) {
        progressDiv.style.display = 'flex';
    }
    
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/admin/api/exams/${examId}/edit`);
    
    xhr.upload.addEventListener('progress', function(e) {
        if (e.lengthComputable) {
            const pct = Math.round((e.loaded / e.total) * 100);
            progressFill.style.width = pct + '%';
            progressText.textContent = pct + '%';
        }
    });
    
    xhr.addEventListener('load', function() {
        btn.disabled = false;
        btn.textContent = '💾 Simpan Perubahan';
        try {
            const res = JSON.parse(xhr.responseText);
            if (res.success) {
                showToast(res.message, 'success');
                setTimeout(() => location.reload(), 1000);
            } else {
                showToast(res.message || 'Gagal menyimpan perubahan', 'error');
                progressDiv.style.display = 'none';
                progressFill.style.width = '0';
            }
        } catch {
            showToast('Respon server tidak valid', 'error');
            progressDiv.style.display = 'none';
            progressFill.style.width = '0';
        }
    });
    
    xhr.addEventListener('error', function() {
        btn.disabled = false;
        btn.textContent = '💾 Simpan Perubahan';
        showToast('Gagal terhubung ke server', 'error');
        progressDiv.style.display = 'none';
        progressFill.style.width = '0';
    });
    
    xhr.send(formData);
}

// Toggle Row Dropdown
function toggleRowDropdown(event, examId) {
    if (event) {
        event.stopPropagation();
    }
    const dropdown = document.getElementById(`dropdown-content-${examId}`);
    if (!dropdown) return;
    
    const isShown = dropdown.classList.contains('show');
    
    // Close all other dropdowns
    document.querySelectorAll('.exam-action-dropdown-content').forEach(d => {
        if (d !== dropdown) {
            d.classList.remove('show');
        }
    });
    
    dropdown.classList.toggle('show');
}

// Close dropdowns when clicking anywhere outside
document.addEventListener('click', function(event) {
    const clickedBtn = event.target.closest('.btn-more');
    const clickedDropdown = event.target.closest('.exam-action-dropdown-content');
    
    if (!clickedBtn && !clickedDropdown) {
        document.querySelectorAll('.exam-action-dropdown-content').forEach(d => {
            d.classList.remove('show');
        });
    }
});

// Bulk Selection Functions
function toggleSelectAllExams(masterCheckbox) {
    const checkboxes = document.querySelectorAll('.exam-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = masterCheckbox.checked;
    });
    updateBulkActions();
}

function updateBulkActions() {
    const checkboxes = document.querySelectorAll('.exam-checkbox:checked');
    const totalSelected = checkboxes.length;
    
    const bulkDeleteBtn = document.getElementById('bulkDeleteBtn');
    const bulkToggleBtn = document.getElementById('bulkToggleBtn');
    const bulkDeleteCount = document.getElementById('bulkDeleteCount');
    const bulkToggleCount = document.getElementById('bulkToggleCount');
    
    if (totalSelected > 0) {
        if (bulkDeleteBtn) {
            bulkDeleteBtn.style.display = 'inline-flex';
            bulkDeleteCount.textContent = totalSelected;
        }
        if (bulkToggleBtn) {
            bulkToggleBtn.style.display = 'inline-flex';
            bulkToggleCount.textContent = totalSelected;
            
            // Determine active/inactive mix
            let hasActive = false;
            checkboxes.forEach(cb => {
                if (cb.getAttribute('data-status') === 'active') {
                    hasActive = true;
                }
            });
            bulkToggleBtn.innerHTML = hasActive ? `⏸️ Nonaktifkan Terpilih (${totalSelected})` : `▶️ Aktifkan Terpilih (${totalSelected})`;
        }
    } else {
        if (bulkDeleteBtn) bulkDeleteBtn.style.display = 'none';
        if (bulkToggleBtn) bulkToggleBtn.style.display = 'none';
        
        const selectAll = document.getElementById('selectAllExams');
        if (selectAll) selectAll.checked = false;
    }
}

async function bulkDeleteExams() {
    const checkboxes = document.querySelectorAll('.exam-checkbox:checked');
    if (checkboxes.length === 0) return;
    
    const ids = Array.from(checkboxes).map(cb => parseInt(cb.value));
    const names = Array.from(checkboxes).map(cb => cb.getAttribute('data-name'));
    
    if (!confirm(`Apakah Anda yakin ingin menghapus ${ids.length} ujian berikut?\n- ${names.join('\n- ')}`)) {
        return;
    }
    
    try {
        const response = await apiFetch('/admin/exams/bulk-delete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ ids: ids })
        });
        const res = await response.json();
        if (res.success) {
            showToast(res.message || `${ids.length} ujian berhasil dihapus`, 'success');
            setTimeout(() => location.reload(), 1000);
        } else {
            showToast(res.message || 'Gagal menghapus ujian', 'error');
        }
    } catch (err) {
        showToast('Gagal menghubungi server', 'error');
    }
}

async function bulkToggleExams() {
    const checkboxes = document.querySelectorAll('.exam-checkbox:checked');
    if (checkboxes.length === 0) return;
    
    const ids = Array.from(checkboxes).map(cb => parseInt(cb.value));
    
    // Check if we should activate or deactivate. If any are active, we deactivate them all.
    let targetStatus = 'inactive';
    let hasActive = false;
    checkboxes.forEach(cb => {
        if (cb.getAttribute('data-status') === 'active') {
            hasActive = true;
        }
    });
    if (!hasActive) {
        targetStatus = 'active';
    }
    
    try {
        const response = await apiFetch('/admin/exams/bulk-toggle', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ ids: ids, status: targetStatus })
        });
        const res = await response.json();
        if (res.success) {
            showToast(res.message || `Status ${ids.length} ujian berhasil diperbarui`, 'success');
            setTimeout(() => location.reload(), 1000);
        } else {
            showToast(res.message || 'Gagal mengubah status ujian', 'error');
        }
    } catch (err) {
        showToast('Gagal menghubungi server', 'error');
    }
}



