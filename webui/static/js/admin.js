/* EXAMVAN Admin Panel - JavaScript */

// CSRF Token Helper

// Toast notification

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
            display.style.borderColor = 'var(--color-success)';
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
            if (tokenVal.length !== 8 || !/^[A-Z0-9]+$/.test(tokenVal)) {
                showToast('Token kustom harus terdiri dari 8 karakter alfanumerik', 'error');
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
                const res = JSON.parse(xhr.responseText);				if (res.success) {
					// New exams are uploaded as INACTIVE; guide the admin to activate
					// and start the exam before sharing the token with students.
					showToast(res.message + ' Aktifkan & mulai ujian sebelum token dibagikan ke siswa.', 'success');
					setTimeout(() => location.reload(), 2000);
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

// Toggle exam status — update UI in-place tanpa reload
function toggleExam(examId) {
    const badge = document.getElementById('status-' + examId);
    if (!badge) return;
    // Disable sementara untuk cegah double-click
    badge.style.pointerEvents = 'none';
    badge.style.opacity = '0.5';

    apiFetch(`/admin/api/exams/${examId}/toggle`, { method: 'POST' })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                // Update badge in-place berdasarkan new_status dari server.
                // status-tombstoned dibuang: mengaktifkan ujian otomatis
                // membersihkan penanda tombstone di server, jadi badge harus
                // kembali normal (Aktif/Nonaktif), bukan lagi "Ditombstone".
                var isActive = res.new_status === 'active';
                badge.classList.remove('status-tombstoned');
                badge.classList.toggle('status-active', isActive);
                badge.classList.toggle('status-inactive', !isActive);
                badge.textContent = isActive ? 'Aktif' : 'Nonaktif';
                showToast(res.message, 'success');
            } else {
                showToast(res.message || 'Gagal mengubah status', 'error');
                badge.style.opacity = '1';
            }
        })
        .catch(function() {
            showToast('Koneksi gagal', 'error');
            badge.style.opacity = '';
        })
        .finally(function() {
            badge.style.pointerEvents = '';
            badge.style.opacity = '';
        });
}

// Delete exam
function deleteExam(examId, examName) {
    showConfirm(`Hapus ujian "${examName}"?`, 'File PDF juga akan dihapus permanen.').then(ok => {
        if (!ok) return;

        apiFetch(`/admin/api/exams/${examId}/delete`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        })
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
    });
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

// Copy all visible active tokens to clipboard (excludes permanent token)
function copyAllTokens() {
    const tokens = Array.from(document.querySelectorAll('.token-code:not(.token-permanent)')).filter(function(el) { return el.offsetParent !== null; }).map(el => el.textContent.trim()).filter(t => t && t !== '—');
    if (tokens.length === 0) {
        showToast('Tidak ada token tersedia', 'error');
        return;
    }
    const text = tokens.map((t, i) => `${i + 1}. ${t}`).join('\n');
    navigator.clipboard.writeText(text).then(() => {
        showToast(`${tokens.length} token berhasil disalin ke clipboard`, 'success');
    }).catch(() => {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        textarea.remove();
        showToast(`${tokens.length} token berhasil disalin`, 'success');
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

// Token mode handlers (static vs dynamic)
function onTokenModeChange(selectEl, examId) {
    var mode = selectEl.value;
    var staticEl = document.getElementById('token-static-' + examId);
    var dynamicEl = document.getElementById('token-dynamic-' + examId);

    if (mode === 'static') {
        if (staticEl) staticEl.style.display = 'flex';
        if (dynamicEl) dynamicEl.style.display = 'none';
    } else {
        if (staticEl) staticEl.style.display = 'none';
        if (dynamicEl) dynamicEl.style.display = 'flex';
    }

    // Save to server
    var data = { token_mode: mode };
    if (mode === 'dynamic') {
        var intervalInput = document.getElementById('interval-' + examId);
        data.reset_interval = parseInt(intervalInput ? intervalInput.value : 5);
    }

    apiFetch('/admin/api/exams/' + examId + '/token-mode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        if (res.success) {
            showToast('Mode token diubah ke ' + (mode === 'static' ? 'Statis' : 'Dinamis'), 'success');
        } else {
            showToast(res.message || 'Gagal mengubah mode token', 'error');
            // Revert UI
            selectEl.value = mode === 'static' ? 'dynamic' : 'static';
            if (staticEl) staticEl.style.display = mode === 'static' ? 'none' : 'flex';
            if (dynamicEl) dynamicEl.style.display = mode === 'static' ? 'flex' : 'none';
        }
    })
    .catch(function() {
        showToast('Koneksi gagal', 'error');
    });
}

function saveTokenInterval(examId) {
    var input = document.getElementById('interval-' + examId);
    if (!input) return;
    var interval = parseInt(input.value);
    if (isNaN(interval) || interval < 1) {
        showToast('Interval harus minimal 1 menit', 'error');
        input.focus();
        return;
    }

    apiFetch('/admin/api/exams/' + examId + '/token-mode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token_mode: 'dynamic', reset_interval: interval })
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        if (res.success) {
            showToast('Interval reset token berhasil disimpan (' + interval + ' menit)', 'success');
        } else {
            showToast(res.message || 'Gagal menyimpan interval', 'error');
        }
    })
    .catch(function() {
        showToast('Koneksi gagal', 'error');
    });
}

// Regenerate token
function regenerateToken(examId) {
    showConfirm('Generate token baru?', 'Token lama tidak akan bisa digunakan lagi.', 'Ya, Generate', 'Batal').then(ok => {
        if (!ok) return;

        apiFetch(`/admin/api/exams/${examId}/regenerate-token`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    var newToken = res.token;
                    // Update static token
                    const tokenEl = document.getElementById(`token-${examId}`);
                    if (tokenEl) {
                        tokenEl.textContent = newToken;
                        tokenEl.dataset.token = newToken;
                        tokenEl.style.animation = 'none';
                        tokenEl.offsetHeight; // force reflow
                        tokenEl.style.animation = 'toastIn 0.3s ease';
                    }
                    // Update dynamic token
                    const tokenDynEl = document.getElementById(`token-dyn-${examId}`);
                    if (tokenDynEl) {
                        tokenDynEl.textContent = newToken;
                        tokenDynEl.dataset.token = newToken;
                        tokenDynEl.style.animation = 'none';
                        tokenDynEl.offsetHeight;
                        tokenDynEl.style.animation = 'toastIn 0.3s ease';
                    }
                    // Update edit button
                    var editBtn = document.querySelector(`.btn-edit[data-exam-id="${examId}"]`);
                    if (editBtn) {
                        editBtn.setAttribute('data-token', newToken);
                        editBtn.setAttribute('onclick', `openEditTokenModal(${examId}, '${jsEscape(newToken)}')`);
                    }
                    // Update permanent token display
                    const permTokenEl = document.querySelector(`#exam-row-${examId} .token-permanent`);
                    if (permTokenEl) {
                        permTokenEl.textContent = newToken;
                        permTokenEl.dataset.token = newToken;
                    }
                    // Update all copy-link buttons for this exam
                    document.querySelectorAll(`.btn-copy-link[data-token]`).forEach(function(btn) {
                        if (btn.closest(`#token-static-${examId}`) || btn.closest(`#token-dynamic-${examId}`)) {
                            btn.setAttribute('data-token', newToken);
                        }
                    });
                    showToast(res.message, 'success');
                } else {
                    showToast(res.message || 'Gagal regenerate token', 'error');
                }
            })
            .catch(() => showToast('Koneksi gagal', 'error'));
    });
}

// Global modal state
let activeExamId = null;
let activeExamName = '';
let pendingFetchId = 0; // For race condition guard

// Warn before leaving if questions editor is open
window.addEventListener('beforeunload', function(e) {
    if (activeExamId !== null) {
        e.preventDefault();
        e.returnValue = '';
    }
});

function openQuestionsModal(examId, examName) {
    activeExamName = examName;
    document.getElementById('modalTitle').textContent = `Atur Soal Ujian: ${examName}`;
    const container = document.getElementById('questionsList');
    container.innerHTML = '<div style="color:var(--color-text-secondary); text-align:center; padding: 20px;">Memuat data soal...</div>';

    // Open modal first
    document.getElementById('questionsModal').style.display = 'flex';

    // Track fetch to prevent race condition
    const fetchId = ++pendingFetchId;

    apiFetch(`/admin/api/exams/${examId}/questions`)
        .then(r => r.json())
        .then(res => {
            if (fetchId !== pendingFetchId) return; // Stale response
            if (res.success) {
                activeExamId = examId; // Set AFTER data loaded
                const secSelect = document.getElementById('examSecurityLevel');
                if (secSelect) {
                    secSelect.value = res.security_level || 'medium';
                }
                // Panel color
                const colorVal = res.panel_color || '#6366F1';
                const colorInput = document.getElementById('examPanelColor');
                const hexInput = document.getElementById('panelColorHex');
                if (colorInput) colorInput.value = colorVal;
                if (hexInput) hexInput.value = colorVal;
                // Exam schedule times (support "YYYY-MM-DD HH:MM" and legacy "HH:MM")
                const startInput = document.getElementById('examStartTime');
                const endInput = document.getElementById('examEndTime');
                const startDateInput = document.getElementById('examStartDate');
                const endDateInput = document.getElementById('examEndDate');
                function parseSchedule(val, dateEl, timeEl) {
                    if (!val) { if (dateEl) dateEl.value = ''; if (timeEl) timeEl.value = ''; return; }
                    if (val.length === 5) {
                        if (timeEl) timeEl.value = val;
                        if (dateEl) dateEl.value = '';
                    } else {
                        var parts = val.split(' ');
                        if (parts.length === 2) {
                            if (dateEl) dateEl.value = parts[0];
                            if (timeEl) timeEl.value = parts[1];
                        }
                    }
                }
                parseSchedule(res.start_time, startDateInput, startInput);
                parseSchedule(res.end_time, endDateInput, endInput);
                renderStudentAccessControls(examId, res);
                renderQuestions(res.questions);
                renderIdentityFields(res.identity_fields || []);
                if (res.assigned_pengawas) {
                    renderPengawasSelection(res.assigned_pengawas, res.available_pengawas || []);
                }
            } else {
                showToast(res.message || 'Gagal memuat soal', 'error');
            }
        })
        .catch(err => {
            if (fetchId !== pendingFetchId) return; // Stale response
            showToast('Gagal memuat data soal', 'error');
        });
}

function closeQuestionsModal() {
    document.getElementById('questionsModal').style.display = 'none';
    activeExamId = null;
    activeExamName = '';
}

// Render the student access & answer-key controls inside the questions modal
// (moved here from the pengawasan page).
function renderStudentAccessControls(examId, res) {
    const wrap = document.getElementById('studentAccessControls');
    if (!wrap) return;
    const token = res.token || '';
    const prActive = res.public_results === 1;
    const saActive = res.show_answers === 1;
    wrap.innerHTML = `
        <a href="/hasil/${token}" target="_blank" class="pd-action-btn pd-action-link" title="Buka halaman hasil ujian untuk siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-link"/></svg> Halaman Siswa
        </a>
        <button id="btn-public-results-${examId}" data-exam-id="${examId}" onclick="togglePublicResults(this.dataset.examId)" class="pd-action-btn ${prActive ? 'pd-action-active' : 'pd-action-danger'}" title="Aktifkan/nonaktifkan halaman hasil ujian siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="${prActive ? '#hi-eye' : '#hi-eye-off'}"/></svg> ${prActive ? 'Hal. Siswa Aktif' : 'Hal. Siswa Nonaktif'}
        </button>
        <button id="btn-show-answers-${examId}" data-exam-id="${examId}" onclick="toggleShowAnswers(this.dataset.examId)" class="pd-action-btn ${saActive ? 'pd-action-warning' : 'pd-action-muted'}" title="Tampilkan/sembunyikan kunci jawaban untuk siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="${saActive ? '#hi-lock-open' : '#hi-lock'}"/></svg> ${saActive ? 'Kunci Terlihat' : 'Kunci Tersembunyi'}
        </button>`;
}

function setPanelColor(hex) {
    var colorInput = document.getElementById('examPanelColor');
    var hexInput = document.getElementById('panelColorHex');
    if (colorInput) colorInput.value = hex;
    if (hexInput) hexInput.value = hex;
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
    card.draggable = true;
    card.dataset.questionNum = num;
    // Drag events for reordering
    card.addEventListener('dragstart', handleDragStart);
    card.addEventListener('dragend', handleDragEnd);
    card.addEventListener('dragover', handleDragOver);
    card.addEventListener('drop', handleDrop);
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
                <input type="text" class="q-key-input" value="${escapeHtml(keyVal)}" placeholder="Jawaban..." title="PG: A,B,C | Menjodohkan: 1:A,2:B">
            </div>
            <div class="q-field-group q-options-group" style="display: ${optionsVisibility};">
                <label>Pilihan</label>
                <input type="text" class="q-options-input" value="${escapeHtml(optionsVal)}" placeholder="A, B, C, D, E">
            </div>
        </div>
        <button class="btn-sm btn-delete btn-remove-q" onclick="removeQuestionCard(this)" title="Hapus Soal"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-trash"/></svg></button>
    `;
    return card;
}

function createDivider(index) {
    const div = document.createElement('div');
    div.className = 'q-editor-divider';
    div.dataset.index = index;
    div.innerHTML = `
        <div class="q-divider-line"></div>
        <button class="btn-add-inline" onclick="insertQuestionAt(${index})" title="Sisipkan Soal Baru Di Sini"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-plus"/></svg> Sisipkan Soal</button>
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

// ===== Drag-and-Drop Question Reordering =====
let dragSrcCard = null;

function handleDragStart(e) {
    dragSrcCard = this;
    this.classList.add('q-dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', '');
}

function handleDragEnd(e) {
    this.classList.remove('q-dragging');
    document.querySelectorAll('.question-editor-card').forEach(c => c.classList.remove('q-drag-over'));
    document.querySelectorAll('.q-editor-divider').forEach(d => d.classList.remove('q-drag-hover'));
    dragSrcCard = null;
}

function handleDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';

    const container = document.getElementById('questionsList');
    const cards = Array.from(container.querySelectorAll('.question-editor-card'));
    const rect = this.getBoundingClientRect();
    const midY = rect.top + rect.height / 2;

    // Determine if drop is above or below this card
    let dropDivider;
    if (e.clientY < midY) {
        // Drop before this card — use the divider before it
        dropDivider = this.previousSibling;
    } else {
        // Drop after this card — use the divider after it
        dropDivider = this.nextSibling?.nextSibling || this.nextSibling;
    }

    document.querySelectorAll('.q-editor-divider').forEach(d => d.classList.remove('q-drag-hover'));
    if (dropDivider && dropDivider.classList.contains('q-editor-divider')) {
        dropDivider.classList.add('q-drag-hover');
    }
}

function handleDrop(e) {
    e.preventDefault();
    if (dragSrcCard === this) return;

    const container = document.getElementById('questionsList');
    const cards = Array.from(container.querySelectorAll('.question-editor-card'));
    const rect = this.getBoundingClientRect();
    const midY = rect.top + rect.height / 2;

    let dropDivider;
    if (e.clientY < midY) {
        dropDivider = this.previousSibling;
    } else {
        dropDivider = this.nextSibling?.nextSibling || this.nextSibling;
    }

    if (dropDivider && dropDivider.classList.contains('q-editor-divider')) {
        const srcDivider = dragSrcCard.nextSibling;
        if (srcDivider && srcDivider.classList.contains('q-editor-divider')) {
            container.insertBefore(dragSrcCard, dropDivider.nextSibling);
            container.insertBefore(srcDivider, dragSrcCard.nextSibling);
        }
    }

    document.querySelectorAll('.q-editor-divider').forEach(d => d.classList.remove('q-drag-hover'));
    reindexQuestions();
    showToast('Soal berhasil diurutkan ulang', 'success');
}
// ===== End Drag-and-Drop =====

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
    overlay.id = 'bulkWeightModal';
    overlay.style.display = 'flex';

    const card = document.createElement('div');
    card.className = 'modal-card';
    card.style.maxWidth = '420px';
    card.innerHTML = `
        <div class="modal-header">
            <h3>⚖️ Set Bobot Semua Soal</h3>
            <button class="modal-close" onclick="this.closest('.modal-overlay').remove()" aria-label="Tutup"><svg class="icon-svg" style="width:18px;height:18px;"><use href="#hi-x"/></svg></button>
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
        container.innerHTML = '<div style="color:var(--color-text-muted); text-align:center; padding: 16px; font-size: 13px;">Tidak ada soal dikonfigurasi. Ujian akan tampil sebagai PDF saja tanpa overlay jawaban.</div>';
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

// ===== Identity Fields Management =====
const DEFAULT_IDENTITY_FIELDS = [
    { key: 'student_name', label: 'Nama', required: true },
    { key: 'exam_number', label: 'Nomor Ujian', required: true },
    { key: 'student_class', label: 'Kelas', required: true }
];
/** Label dari field default yang wajib ada dan tidak bisa diubah/dihapus */
const LOCKED_IDENTITY_LABELS = ['Nama'];

function renderIdentityFields(fields) {
    const container = document.getElementById('identityFieldsList');
    container.innerHTML = '';

    if (!fields || fields.length === 0) {
        fields = JSON.parse(JSON.stringify(DEFAULT_IDENTITY_FIELDS));
    }

    fields.forEach(function(field, index) {
        addIdentityFieldRow(container, field, index);
    });
}

function addIdentityFieldRow(container, field, index) {
    const row = document.createElement('div');
    row.className = 'identity-field-row';
    const isLocked = LOCKED_IDENTITY_LABELS.includes(field.label);

    row.innerHTML = `
        <span class="identity-field-num">${index + 1}</span>
        <input type="text" class="ifield-label" value="${escapeHtml(field.label || '')}" placeholder="Label tampilan (cth: Nama Siswa)" title="Label yang dilihat siswa" ${isLocked ? 'readonly style="opacity:0.7;cursor:not-allowed;"' : ''}>
        <label class="ifield-required-wrap" ${isLocked ? 'style="opacity:0.5;"' : ''}>
            <input type="checkbox" class="ifield-required" ${field.required ? 'checked' : ''} ${isLocked ? 'disabled' : ''}> Wajib
        </label>
        ${isLocked ? '<span class="ifield-locked-badge" title="Field bawaan, tidak bisa dihapus"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-lock"/></svg></span>' : '<button class="ifield-remove-btn" onclick="this.closest(\'.identity-field-row\').remove()" title="Hapus field" aria-label="Hapus field"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-x"/></svg></button>'}
    `;

    container.appendChild(row);
}

function addIdentityField() {
    const container = document.getElementById('identityFieldsList');
    const count = container.children.length;
    const field = { key: 'field_' + (count + 1), label: '', required: false };
    addIdentityFieldRow(container, field, count);
}

function getIdentityFieldsFromEditor() {
    const rows = document.querySelectorAll('#identityFieldsList .identity-field-row');
    const fields = [];
    rows.forEach(function(row, idx) {
        const label = row.querySelector('.ifield-label').value.trim();
        const required = row.querySelector('.ifield-required').checked;
        // Auto-generate key dari label: lowercase, replace spasi/non-alpha dengan underscore
        var key = label.toLowerCase().replace(/[^a-z0-9]/g, '_').replace(/_+/g, '_').replace(/^_|_$/g, '');
        if (!key) key = 'field_' + (idx + 1);
        if (label) {
            fields.push({ key: key, label: label, required: required });
        }
    });
    return fields.length > 0 ? fields : JSON.parse(JSON.stringify(DEFAULT_IDENTITY_FIELDS));
}

// ===== Pengawas Assignment =====
function renderPengawasSelection(assigned, available) {
    var container = document.getElementById('pengawasList');
    if (!container) return;
    container.innerHTML = '';

    var assignedIds = (assigned || []).map(function(p) { return p.id; });

    if (!available || available.length === 0) {
        container.innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Tidak ada pengawas tersedia di instansi Anda. Tambah user dengan role "Pengawas" terlebih dahulu.</div>';
        return;
    }

    // Build dropdown wrapper
    var wrapper = document.createElement('div');
    wrapper.style.cssText = 'position:relative;';

    // Dropdown header — shows selected count / chips
    var header = document.createElement('div');
    header.id = 'pengawasDropdownHeader';
    header.style.cssText = 'display:flex;align-items:center;flex-wrap:wrap;gap:6px;min-height:38px;padding:6px 10px;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);border-radius:8px;cursor:pointer;transition:border-color 0.2s;';
    header.onclick = function(e) { e.stopPropagation(); togglePengawasDropdown(); };
    header.onmouseenter = function() { this.style.borderColor = 'rgba(99,102,241,0.4)'; };
    header.onmouseleave = function() { var dd = document.getElementById('pengawasDropdown'); if (!dd || dd.style.display==='none') this.style.borderColor = 'var(--color-glass-border)'; };

    var selectedPengawas = available.filter(function(p) { return assignedIds.indexOf(p.id) !== -1; });
    var unselectedPengawas = available.filter(function(p) { return assignedIds.indexOf(p.id) === -1; });

    function renderHeaderChips() {
        var currentSelected = document.querySelectorAll('#pengawasDropdown .pengawas-checkbox:checked');
        var ids = Array.from(currentSelected).map(function(cb) { return parseInt(cb.value); });
        var chips = available.filter(function(p) { return ids.indexOf(p.id) !== -1; });
        header.innerHTML = '';
        if (chips.length === 0) {
            header.innerHTML = '<span style="color:var(--color-text-muted);font-size:0.82rem;">Pilih pengawas...</span>';
        } else {
            chips.forEach(function(p) {
                var chip = document.createElement('span');
                chip.style.cssText = 'display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:12px;font-size:0.75rem;font-weight:500;background:rgba(168,85,247,0.15);color:#c084fc;border:1px solid rgba(168,85,247,0.25);';
                chip.textContent = p.username;
                var x = document.createElement('span');
                x.style.cssText = 'cursor:pointer;margin-left:2px;font-size:13px;line-height:1;opacity:0.7;';
                x.textContent = '×';
                x.onclick = function(ev) { ev.stopPropagation(); var cb = document.querySelector('#pengawasDropdown .pengawas-checkbox[value="' + p.id + '"]'); if (cb) { cb.checked = false; renderHeaderChips(); } };
                chip.appendChild(x);
                header.appendChild(chip);
            });
        }
        header.innerHTML += '<span style="margin-left:auto;font-size:11px;color:var(--color-text-muted);">▼</span>';
    }
    renderHeaderChips();

    // Dropdown body (hidden by default)
    var dropdown = document.createElement('div');
    dropdown.id = 'pengawasDropdown';
    dropdown.style.cssText = 'display:none;position:absolute;top:100%;left:0;right:0;z-index:100;margin-top:4px;background:rgba(30,30,50,0.98);border:1px solid rgba(255,255,255,0.1);border-radius:10px;max-height:220px;overflow-y:auto;box-shadow:0 8px 32px rgba(0,0,0,0.4);backdrop-filter:blur(12px);';

    // Search input inside dropdown
    var searchBox = document.createElement('input');
    searchBox.type = 'text';
    searchBox.placeholder = 'Cari pengawas...';
    searchBox.style.cssText = 'width:100%;padding:8px 12px;background:rgba(255,255,255,0.05);border:none;border-bottom:1px solid rgba(255,255,255,0.08);color:var(--color-text);font-size:0.82rem;outline:none;box-sizing:border-box;border-radius:10px 10px 0 0;';
    searchBox.oninput = function() {
        var q = this.value.toLowerCase();
        dropdown.querySelectorAll('.pengawas-option').forEach(function(opt) {
            opt.style.display = opt.textContent.toLowerCase().indexOf(q) !== -1 ? 'flex' : 'none';
        });
    };
    // Prevent dropdown from closing when clicking search
    searchBox.onclick = function(e) { e.stopPropagation(); };
    dropdown.appendChild(searchBox);

    // Option list
    var list = document.createElement('div');
    list.style.cssText = 'padding:4px 0;';

    available.forEach(function(p) {
        var isChecked = assignedIds.indexOf(p.id) !== -1;
        var opt = document.createElement('label');
        opt.className = 'pengawas-option';
        opt.style.cssText = 'display:flex;align-items:center;gap:8px;padding:8px 12px;cursor:pointer;font-size:0.85rem;color:var(--color-text);transition:background 0.1s;';
        opt.onmouseenter = function() { this.style.background = 'rgba(99,102,241,0.1)'; };
        opt.onmouseleave = function() { this.style.background = 'transparent'; };
        opt.innerHTML = '<input type="checkbox" class="pengawas-checkbox" value="' + p.id + '"' + (isChecked ? ' checked' : '') + ' style="accent-color:#818cf8;cursor:pointer;"> '
            + '<span style="font-weight:500;">' + escapeHtml(p.username) + '</span>'
            + ' <span style="font-size:0.7rem;color:var(--color-text-muted);margin-left:auto;">' + (p.role || 'Pengawas') + '</span>';
        opt.querySelector('input').addEventListener('change', function() {
            renderHeaderChips();
        });
        list.appendChild(opt);
    });
    dropdown.appendChild(list);
    wrapper.appendChild(header);
    wrapper.appendChild(dropdown);
    container.appendChild(wrapper);

    // Close dropdown on outside click
    if (!window._pengawasDropdownListener) {
        window._pengawasDropdownListener = true;
        document.addEventListener('click', function() {
            var dd = document.getElementById('pengawasDropdown');
            var hd = document.getElementById('pengawasDropdownHeader');
            if (dd && hd) {
                dd.style.display = 'none';
                hd.style.borderColor = 'var(--color-glass-border)';
            }
        });
    }
}

function togglePengawasDropdown() {
    var dd = document.getElementById('pengawasDropdown');
    var hd = document.getElementById('pengawasDropdownHeader');
    if (!dd || !hd) return;
    if (dd.style.display === 'none' || !dd.style.display) {
        dd.style.display = 'block';
        hd.style.borderColor = 'rgba(99,102,241,0.5)';
        // Focus search
        var sb = dd.querySelector('input[type="text"]');
        if (sb) setTimeout(function() { sb.focus(); }, 50);
    } else {
        dd.style.display = 'none';
        hd.style.borderColor = 'var(--color-glass-border)';
    }
}

function getPengawasIdsFromEditor() {
    var checkboxes = document.querySelectorAll('#pengawasDropdown .pengawas-checkbox:checked');
    return Array.from(checkboxes).map(function(cb) { return parseInt(cb.value); });
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
        const partialCb = card.querySelector('.q-partial-checkbox');
        if (partialCb) partialCb.checked = false;
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

function clearSchedule() {
    ['examStartDate','examStartTime','examEndDate','examEndTime'].forEach(function(id) {
        var el = document.getElementById(id);
        if (el) el.value = '';
    });
}

function saveQuestionsConfig() {
    if (!activeExamId) return;

    const questions = getQuestionsFromEditor();
    const securityLevel = document.getElementById('examSecurityLevel') ? document.getElementById('examSecurityLevel').value : 'medium';
    const strictMode = securityLevel === 'high';

    const identityFields = getIdentityFieldsFromEditor();

    const panelColor = document.getElementById('panelColorHex') ? document.getElementById('panelColorHex').value.trim() : '';
    function buildSchedule(dateId, timeId) {
        var d = document.getElementById(dateId);
        var t = document.getElementById(timeId);
        var dateVal = d ? d.value : '';
        var timeVal = t ? t.value : '';
        if (dateVal && timeVal) return dateVal + ' ' + timeVal;
        if (timeVal) return timeVal;
        return '';
    }
    const startTime = buildSchedule('examStartDate', 'examStartTime');
    const endTime = buildSchedule('examEndDate', 'examEndTime');
    const pengawasIds = getPengawasIdsFromEditor();

    apiFetch(`/admin/api/exams/${activeExamId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ questions: questions, security_level: securityLevel, strict_mode: strictMode, identity_fields: identityFields, panel_color: panelColor, start_time: startTime, end_time: endTime, pengawas_ids: pengawasIds })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                showToast(res.message, 'success');
                closeQuestionsModal();
                setTimeout(function() { location.reload(); }, 500);
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

    if (newPassword.length < 8) {
        showToast('Password baru minimal 8 karakter', 'error');
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


function renderRoleBadges(baseRoles, pkgRoles) {
    if (!Array.isArray(baseRoles)) baseRoles = [];
    if (!Array.isArray(pkgRoles)) pkgRoles = [];
    if (baseRoles.length + pkgRoles.length === 0) return '<span style="color:var(--color-text-muted);font-size:12px;">—</span>';
    var badgeStyles = {
        guru: 'background:rgba(99,102,241,0.15);color:#a5b4fc;border:1px solid rgba(99,102,241,0.3);',
        pengawas: 'background:rgba(168,85,247,0.15);color:#c084fc;border:1px solid rgba(168,85,247,0.3);'
    };
    // Roles granted by the ACTIVE package get an emerald tone + a small "paket"
    // tag so admins can tell them apart from base (permanent) roles — these
    // disappear when the package is switched or expires.
    var pkgStyle = 'background:rgba(16,185,129,0.15);color:#34d399;border:1px solid rgba(16,185,129,0.3);';
    var labels = { guru: 'Guru', pengawas: 'Pengawas' };
    var badges = [];
    baseRoles.forEach(function(r) {
        var style = badgeStyles[r] || badgeStyles.guru;
        var label = labels[r] || escapeHtml(r);
        badges.push('<span style="display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;' + style + '">' + label + '</span>');
    });
    pkgRoles.forEach(function(r) {
        var label = labels[r] || escapeHtml(r);
        badges.push('<span title="Dari paket aktif — hilang saat paket diganti/berakhir" style="display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;cursor:help;' + pkgStyle + '">' + label + ' <em style="font-style:normal;opacity:0.75;font-weight:500;font-size:9px;text-transform:uppercase;letter-spacing:0.03em;">paket</em></span>');
    });
    return badges.join(' ');
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

function loadUsersList(page) {
    const tbody = document.getElementById('usersTableBody');
    if (!tbody) return;
    if (!page) page = 1;

    var searchVal = document.getElementById('userSearchInput')?.value?.trim() || '';
    var roleFilter = document.getElementById('userRoleFilter')?.value || '';
    var url = '/admin/api/users?page=' + page + '&per_page=10';
    if (searchVal) url += '&search=' + encodeURIComponent(searchVal);
    if (roleFilter) url += '&role=' + encodeURIComponent(roleFilter);

    // Hapus popup yang tertinggal di body (dari fix backdrop-filter containing block)
    document.querySelectorAll('body > .user-info-popup').forEach(function(p) { p.remove(); });

    tbody.innerHTML = '<tr><td colspan="8" style="text-align:center; padding: 20px; color: var(--color-text-secondary);">⏳ Memuat...</td></tr>';

    apiFetch(url)
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                const pagination = res.pagination || { page: 1, total_pages: 1, total: 0 };
                tbody.innerHTML = '';

                if (!Array.isArray(res.users) || res.users.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="8" style="text-align:center; padding: 40px; color: var(--color-text-secondary);">'
                        + (searchVal ? 'Tidak ditemukan user yang cocok dengan "' + escapeHtml(searchVal) + '"' : 'Belum ada user terdaftar')
                        + '</td></tr>';
                    renderUsersPagination(pagination, page);
                    return;
                }

                res.users.forEach(user => {
                    const tr = document.createElement('tr');
                    const isAdmin = user.username === 'superadmin';
                    var statusClick = isAdmin ? '' : ' onclick="toggleUserStatus(' + user.id + ')"';
                    var statusBadge = user.status === 'active'
                         ? '<span class="status-badge status-active"' + statusClick + '>Aktif</span>'
                         : user.status === 'suspended'
                         ? '<span class="status-badge status-suspended"' + statusClick + '>Suspen</span>'
                         : '<span class="status-badge status-inactive"' + statusClick + '>Pending</span>';
                    // expires_at dikirim server dalam format UTC "YYYY-MM-DD HH:MM:SS"
                    // (dikonversi ke Date dengan + 'Z'). Akun dianggap "masa aktif habis"
                    // bila expires_at terisi dan sudah lewat dari waktu sekarang.
                    const isExpired = Boolean(user.expires_at && new Date(user.expires_at.replace(' ', 'T') + 'Z').getTime() <= Date.now());
                    const expiredBadge = isExpired ? '<span class="status-badge status-expired" title="Masa aktif akun telah habis. Perpanjang masa aktif agar user dapat login kembali.">Masa aktif habis</span>' : '';
                    const expiresAt = user.expires_at || '—';
                    const createdAt = user.created_at ? localizeUTC(user.created_at) : '—';
                    const limitPdfMb = user.max_pdf_size ? (user.max_pdf_size / (1024*1024)).toFixed(1) + ' MB' : '—';
                    const limitStorageMb = user.max_storage_size ? (user.max_storage_size / (1024*1024)).toFixed(1) + ' MB' : '—';

                    // Build action buttons for non-admin users
                    var actionsHtml = '<span style="font-size:11px; color: var(--color-text-secondary);">—</span>';
                    if (!isAdmin) {
                        var verifyBtn = user.status === 'pending_otp'
                            ? '<button class="btn-sm" onclick="verifyUser(' + user.id + ', \'' + escapeHtml(jsEscape(user.username)) + '\')" title="Verifikasi manual" style="font-size:11px;padding:2px 8px;background:rgba(99,102,241,0.15);border:1px solid rgba(99,102,241,0.3);color:#a5b4fc;"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-check"/></svg></button> '
                            : '';

                        actionsHtml = '<div style="display:flex;gap:4px;flex-wrap:wrap;justify-content:flex-end;">'
                            + verifyBtn
                            + '<button class="btn-sm" onclick="openEditUserModal(' + user.id + ')" title="Atur limit & reset password" style="font-size:11px;padding:2px 8px;background:rgba(99,102,241,0.1);border:1px solid rgba(99,102,241,0.2);color:#a5b4fc;"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-edit"/></svg></button> '
                            + '<button class="btn-sm btn-delete" onclick="deleteUser(' + user.id + ', \'' + escapeHtml(jsEscape(user.username)) + '\')" style="font-size:11px;padding:2px 8px;"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-trash"/></svg></button>'
                            + '</div>';
                    }

                    tr.innerHTML = `
                        <td data-label="Username">
                            <strong class="user-info-btn" style="cursor:pointer;color: ${isAdmin ? 'var(--color-accent-light)' : 'var(--color-text)'};">
                                ${isAdmin ? '<svg class="icon-svg" style="width:16px;height:16px;vertical-align:middle;color:#fbbf24;"><use href="#hi-star"/></svg> ' : ''}${escapeHtml(user.username)}
                            </strong>
                            ${isAdmin ? '<span style="font-size:11px; color: var(--color-text-secondary); display:block;">Super Admin</span>' : ''}
                            <div class="user-info-popup" style="display:none;">
                                <div class="user-info-item"><span>Ujian</span><strong>${user.exam_count ?? 0}</strong></div>
                                <div class="user-info-item"><span>Maks Total Ujian</span><strong>${user.max_exams ?? '—'}</strong></div>
                                <div class="user-info-item"><span title="Maksimal ujian yang berjalan bersamaan (sudah dimulai &amp; bisa dikerjakan siswa)">Ujian Serentak</span><strong>${user.max_concurrent_exams ?? '—'}</strong></div>
                                <div class="user-info-item"><span>Maks Upload (MB)</span><strong>${limitPdfMb}</strong></div>
                                <div class="user-info-item"><span>Maks Storage (MB)</span><strong>${limitStorageMb}</strong></div>
                                <div class="user-info-item"><span>Masa Aktif</span><strong${isExpired ? ' style="color:#f87171;"' : ''}>${expiresAt}</strong></div>
                                <div class="user-info-item"><span>Terdaftar</span><strong>${createdAt}</strong></div>
                            </div>
                        </td>
                        <td data-label="Nama">${escapeHtml(user.name || '—')}</td>
                        <td data-label="Instansi">${window.__adminRole === 'superadmin' ? '<span class="editable-instansi" data-user-id="' + user.id + '" style="color:#a5b4fc;cursor:pointer;border-bottom:1px dashed rgba(165,180,252,0.3);" title="Klik untuk ubah instansi">' + escapeHtml(user.instansi || '—') + '</span>' : escapeHtml(user.instansi || '—')}</td>
                        <td data-label="Paket"><span style="text-transform:uppercase;font-size:11px;font-weight:600;color:var(--color-accent-light);">${escapeHtml(user.package || 'free')}</span></td>
                        <td data-label="Role">${isAdmin ? '<span style="display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;background:rgba(251,191,36,0.15);color:#fbbf24;border:1px solid rgba(251,191,36,0.3);">Super Admin</span>' : renderRoleBadges(user.base_roles, user.package_roles)}</td>
                        <td data-label="Email">${escapeHtml(user.email || '—')}</td>
                        <td data-label="Status" style="text-align:center;">${statusBadge}${expiredBadge ? '<div style="margin-top:4px;">' + expiredBadge + '</div>' : ''}</td>
                        <td data-label="Aksi" style="text-align:right;">${actionsHtml}</td>
                    `;
                    tbody.appendChild(tr);
                });
                localizeDates();
                // Attach click handlers for editable instansi
                document.querySelectorAll('.editable-instansi').forEach(function(el) {
                    el.addEventListener('click', function() {
                        var uid = parseInt(this.getAttribute('data-user-id'));
                        var current = this.textContent.trim();
                        if (current === '—') current = '';
                        editUserInstansi(uid, current, this);
                    });
                });
                renderUsersPagination(pagination, page);
            } else {
                tbody.innerHTML = '<tr><td colspan="7" style="text-align:center; padding: 20px; color: #fca5a5;">Gagal memuat daftar user</td></tr>';
            }
        })
        .catch(() => {
            tbody.innerHTML = '<tr><td colspan="4" style="text-align:center; padding: 20px; color: #fca5a5;">Gagal memuat daftar user</td></tr>';
        });
}

function renderUsersPagination(pagination, currentPage) {
    // Remove existing pagination
    var existing = document.getElementById('usersPagination');
    if (existing) existing.remove();

    if (!pagination || pagination.total_pages <= 1) return;

    var container = document.createElement('div');
    container.id = 'usersPagination';
    container.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:16px 0 0 0;flex-wrap:wrap;gap:12px;';

    var info = document.createElement('span');
    info.style.cssText = 'font-size:13px;color:var(--color-text-muted);';
    info.textContent = 'Menampilkan ' + pagination.total + ' user';
    container.appendChild(info);

    var pagesDiv = document.createElement('div');
    pagesDiv.style.cssText = 'display:flex;gap:6px;align-items:center;';

    // Prev button
    var prev = document.createElement('a');
    prev.href = '#';
    prev.style.cssText = 'padding:8px 12px;border-radius:8px;font-size:13px;font-weight:600;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);color:var(--color-text-secondary);text-decoration:none;display:inline-flex;align-items:center;min-height:40px;' + (currentPage <= 1 ? 'opacity:0.4;pointer-events:none;' : '');
    prev.textContent = '◀ Sebelumnya';
    prev.onclick = function(e) { e.preventDefault(); loadUsersList(currentPage - 1); };
    pagesDiv.appendChild(prev);

    // Page numbers
    for (var p = 1; p <= pagination.total_pages; p++) {
        if (p >= currentPage - 2 && p <= currentPage + 2) {
            var pageLink = document.createElement('a');
            pageLink.href = '#';
            if (p === currentPage) pageLink.className = 'page-current';
            pageLink.style.cssText = 'padding:8px 12px;border-radius:8px;font-size:13px;font-weight:' + (p === currentPage ? '800' : '600') + ';text-decoration:none;display:inline-flex;align-items:center;min-height:40px;min-width:36px;justify-content:center;' + (p === currentPage ? 'background:rgba(99,102,241,0.2);color:#a5b4fc;border:1px solid rgba(99,102,241,0.4);' : 'background:rgba(255,255,255,0.03);color:var(--color-text-secondary);border:1px solid transparent;');
            pageLink.textContent = String(p);
            pageLink.onclick = (function(pg) { return function(e) { e.preventDefault(); loadUsersList(pg); }; })(p);
            pagesDiv.appendChild(pageLink);
        }
    }

    // Next button
    var next = document.createElement('a');
    next.href = '#';
    next.style.cssText = 'padding:8px 12px;border-radius:8px;font-size:13px;font-weight:600;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);color:var(--color-text-secondary);text-decoration:none;display:inline-flex;align-items:center;min-height:40px;' + (currentPage >= pagination.total_pages ? 'opacity:0.4;pointer-events:none;' : '');
    next.textContent = 'Berikutnya ▶';
    next.onclick = function(e) { e.preventDefault(); loadUsersList(currentPage + 1); };
    pagesDiv.appendChild(next);

    container.appendChild(pagesDiv);

    var tableSection = document.querySelector('#usersTableBody')?.closest('.glass-card');
    if (tableSection) tableSection.appendChild(container);
}

function localizeDates() {
    document.querySelectorAll('.td-date').forEach(el => {
        const rawDate = el.dataset.utc;
        if (rawDate) {
            el.dataset.utc = rawDate;
            el.textContent = localizeUTC(rawDate);
        }
    });
}

// ===== User Info Popup (Event Delegation) =====
(function() {
    var tableBody = document.getElementById('usersTableBody');
    if (!tableBody) return;
    tableBody.addEventListener('click', function(e) {
        var btn = e.target.closest('.user-info-btn');
        if (btn) {
            // Gunakan popup tersimpan (setelah dipindah ke body di klik sebelumnya),
            // atau cari di dalam td username (karena bukan lagi nextElementSibling —
            // ada <span> admin di antaranya).
            var popup = btn._infoPopup || btn.closest('td').querySelector('.user-info-popup');
            if (!popup) return;
            // Simpan referensi agar tetap bisa diakses setelah dipindah ke body
            btn._infoPopup = popup;
            // Close other popups first
            document.querySelectorAll('.user-info-popup.show').forEach(function(p) {
                if (p !== popup) p.classList.remove('show');
            });
            // Toggle this popup
            var isOpen = popup.classList.contains('show');
            if (isOpen) {
                popup.classList.remove('show');
            } else {
                // Pindahkan popup ke body agar position:fixed bekerja relatif ke viewport.
                // .glass-card punya backdrop-filter yang membuat containing block baru,
                // sehingga position:fixed di dalam table dihitung relatif ke glass-card
                // (bukan viewport) → popup muncul tapi tidak kelihatan (off-screen).
                if (popup.parentNode !== document.body) {
                    document.body.appendChild(popup);
                }
                popup.classList.add('show');
                var rect = btn.getBoundingClientRect();
                popup.style.position = 'fixed';
                popup.style.top = Math.min(rect.bottom + 4, window.innerHeight - 200) + 'px';
                popup.style.left = Math.max(10, Math.min(rect.left, window.innerWidth - 240)) + 'px';
            }
            e.stopPropagation();
            return;
        }
        // Click di dalam tabel tapi bukan popup
        if (!e.target.closest('.user-info-popup')) {
            document.querySelectorAll('.user-info-popup.show').forEach(function(p) { p.classList.remove('show'); });
        }
    });
})();

// Global click: close popup kalau klik di mana saja di luar tabel
// Table handler pakai stopPropagation, jadi ini hanya nangkep klik di luar tabel
document.addEventListener('click', function() {
    document.querySelectorAll('.user-info-popup.show').forEach(function(p) { p.classList.remove('show'); });
});

function deleteUser(userId, username) {
    showConfirm(`Hapus user "${username}"?`, 'Semua ujian dan data yang dibuat oleh user ini akan ikut terhapus.').then(ok => {
        if (!ok) return;

        apiFetch(`/admin/api/users/${userId}/delete`, {
            method: 'POST'
        })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadUsersList(getCurrentUsersPage());
                } else {
                    showToast(res.message || 'Gagal menghapus user', 'error');
                }
            })
            .catch(() => showToast('Gagal menghapus user', 'error'));
    });
}

function toggleUserStatus(userId) {
    showConfirm('Konfirmasi', 'Yakin ingin mengubah status user ini?', 'Ya, Ubah', 'Batal').then(ok => {
        if (!ok) return;
        apiFetch(`/admin/api/users/${userId}/toggle-status`, {
            method: 'POST'
        })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadUsersList(getCurrentUsersPage());
                } else {
                    showToast(res.message || 'Gagal mengubah status', 'error');
                }
            })
            .catch(() => showToast('Gagal mengubah status', 'error'));
    });
}

function verifyUser(userId, username) {
    showConfirm(`Verifikasi user "${username}"?`, 'User ini akan diaktifkan secara manual tanpa verifikasi Email.').then(ok => {
        if (!ok) return;
        apiFetch(`/admin/api/users/${userId}/verify`, {
            method: 'POST'
        })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadUsersList(getCurrentUsersPage());
                } else {
                    showToast(res.message || 'Gagal verifikasi user', 'error');
                }
            })
            .catch(() => showToast('Gagal verifikasi user', 'error'));
    });
}

function getCurrentUsersPage() {
    var pagEl = document.getElementById('usersPagination');
    if (pagEl) {
        // Try to extract current page from pagination info
        var activePage = pagEl.querySelector('a.page-current');
        if (activePage) return parseInt(activePage.textContent) || 1;
    }
    return 1;
}

// ===== Edit User Modal =====
function openEditUserModal(userId) {
    // Fetch user data first
    apiFetch('/admin/api/users?page=1&per_page=1000')
        .then(r => r.json())
        .then(res => {
            if (!res.success || !res.users) {
                showToast('Gagal memuat data user', 'error');
                return;
            }
            var user = res.users.find(function(u) { return u.id === userId; });
            if (!user) {
                showToast('User tidak ditemukan', 'error');
                return;
            }

            var editModal = document.getElementById('editUserModal');
            if (!editModal) {
                // Create modal if it doesn't exist
                editModal = createEditUserModal();
            }

            // Populate fields
            document.getElementById('editUserId').value = user.id;
            document.getElementById('editUserUsername').textContent = user.username;
            document.getElementById('editUserName').value = user.name || '';
            document.getElementById('editUserExams').value = user.max_exams ?? 3;
            document.getElementById('editUserConcurrent').value = user.max_concurrent_exams ?? 2;
            document.getElementById('editUserPdfSize').value = user.max_pdf_size ? (user.max_pdf_size / (1024*1024)).toFixed(1) : '1';
            document.getElementById('editUserStorageSize').value = user.max_storage_size ? (user.max_storage_size / (1024*1024)).toFixed(1) : '0';
            document.getElementById('editUserEmail').value = user.email || '';
            document.getElementById('editUserInstansi').value = user.instansi || '';
            document.getElementById('editUserPackage').value = user.package || 'free';
            var userRoles = user.roles || (user.role ? [user.role] : ['guru']);
            document.getElementById('editRoleGuru').checked = userRoles.indexOf('guru') !== -1;
            document.getElementById('editRolePengawas').checked = userRoles.indexOf('pengawas') !== -1;
            var editRoleOpEl = document.getElementById('editRoleOperator');
            if (editRoleOpEl) editRoleOpEl.checked = userRoles.indexOf('operator') !== -1;

            // Set expiry date — convert UTC back to local timezone
            var expiresInput = document.getElementById('editUserExpiry');
            var expiresTimeInput = document.getElementById('editUserExpiryTime');
            if (user.expires_at) {
                var expDate = new Date(user.expires_at.replace(' ', 'T') + 'Z');
                if (!isNaN(expDate.getTime())) {
                    var localDate = expDate.getFullYear() + '-' +
                        String(expDate.getMonth() + 1).padStart(2, '0') + '-' +
                        String(expDate.getDate()).padStart(2, '0');
                    var localTime = String(expDate.getHours()).padStart(2, '0') + ':' +
                        String(expDate.getMinutes()).padStart(2, '0');
                    expiresInput.value = localDate;
                    expiresTimeInput.value = localTime;
                } else {
                    expiresInput.value = '';
                    expiresTimeInput.value = '23:59';
                }
            } else {
                expiresInput.value = '';
                expiresTimeInput.value = '23:59';
            }

            // Sync limit fields based on role
            syncEditLimitFields();
            // Attach change listeners for edit modal
            var eguru = document.getElementById('editRoleGuru');
            var epengawas = document.getElementById('editRolePengawas');
            if (eguru && epengawas) {
                eguru.addEventListener('change', syncEditLimitFields);
                epengawas.addEventListener('change', syncEditLimitFields);
            }

            editModal.style.display = 'flex';
        });
}

function syncEditLimitFields() {
    var guruChecked = document.getElementById('editRoleGuru').checked;
    var pengawasOnly = document.getElementById('editRolePengawas').checked && !guruChecked;
    var limitInput = document.getElementById('editUserExams');
    var concurrentInput = document.getElementById('editUserConcurrent');
    var pdfInput = document.getElementById('editUserPdfSize');
    if (pengawasOnly) {
        limitInput.disabled = true;
        limitInput.value = '0';
        if (concurrentInput) { concurrentInput.disabled = true; concurrentInput.value = '0'; }
        pdfInput.disabled = true;
        pdfInput.value = '0';
    } else {
        limitInput.disabled = false;
        if (concurrentInput) concurrentInput.disabled = false;
        pdfInput.disabled = false;
    }
}

function closeEditUserModal() {
    var modal = document.getElementById('editUserModal');
    if (modal) modal.style.display = 'none';
}

// Inline edit instansi (superadmin only)
function editUserInstansi(userId, currentValue, targetEl) {
    // Create modal overlay
    var overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.style.display = 'flex';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', 'Ubah Instansi');

    var card = document.createElement('div');
    card.className = 'modal-card glass-card';
    card.style.maxWidth = '400px';
    card.innerHTML = `
        <div class="modal-header">
            <h3><svg class="icon-svg" style="vertical-align:middle;margin-top:-2px;"><use href="#hi-users"/></svg> Ubah Instansi</h3>
            <button class="modal-close" id="instansiModalClose" aria-label="Tutup"><svg class="icon-svg" style="width:18px;height:18px;"><use href="#hi-x"/></svg></button>
        </div>
        <div class="modal-body">
            <div style="margin-bottom:16px;">
                <label style="display:block;font-size:0.85rem;color:var(--color-text-secondary);margin-bottom:6px;">Nama Instansi</label>
                <input type="text" id="instansiEditInput" value="${escapeHtml(currentValue)}" placeholder="Contoh: SMA Negeri 1 Jakarta"
                    style="width:100%;padding:10px 12px;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);border-radius:8px;color:var(--color-text);font-size:14px;outline:none;transition:border-color 0.2s;box-sizing:border-box;"
                    onfocus="this.style.borderColor='rgba(99,102,241,0.5)'" onblur="this.style.borderColor=''">
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end;">
                <button class="btn-sm btn-delete" id="instansiModalCancel">Batal</button>
                <button class="btn-upload" id="instansiModalSave" style="padding:8px 20px;"><svg class="icon-svg"><use href="#hi-check"/></svg> Simpan</button>
            </div>
        </div>
    `;
    overlay.appendChild(card);
    document.body.appendChild(overlay);

    // Focus input
    var input = document.getElementById('instansiEditInput');
    setTimeout(function() { input.focus(); input.select(); }, 100);

    function closeModal() { overlay.remove(); }

    document.getElementById('instansiModalClose').onclick = closeModal;
    document.getElementById('instansiModalCancel').onclick = closeModal;
    overlay.addEventListener('click', function(e) { if (e.target === overlay) closeModal(); });

    // Enter key saves
    input.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') closeModal();
        if (e.key === 'Enter') document.getElementById('instansiModalSave').click();
    });

    document.getElementById('instansiModalSave').onclick = function() {
        var newInstansi = input.value.trim();
        if (!newInstansi) { showToast('Instansi tidak boleh kosong', 'error'); return; }
        if (newInstansi === currentValue) { closeModal(); return; }

        var btn = this;
        btn.disabled = true;
        btn.textContent = 'Menyimpan...';

        apiFetch('/admin/api/users/' + userId + '/edit', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ instansi: newInstansi })
        })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        if (res.success) {
            showToast('Instansi berhasil diubah', 'success');
            if (targetEl) targetEl.textContent = newInstansi;
            closeModal();
        } else {
            showToast(res.message || 'Gagal mengubah instansi', 'error');
            btn.disabled = false;
            btn.innerHTML = '<svg class="icon-svg"><use href="#hi-check"/></svg> Simpan';
        }
    })
    .catch(function() {
        showToast('Gagal mengubah instansi', 'error');
        btn.disabled = false;
        btn.innerHTML = '<svg class="icon-svg"><use href="#hi-check"/></svg> Simpan';
    });
}
}

function submitEditUser(e) {
    e.preventDefault();
    var userId = document.getElementById('editUserId').value;
    var data = {
        name: document.getElementById('editUserName').value.trim(),
        max_exams: (function(){ var v=document.getElementById('editUserExams').value; return v==='' ? 3 : parseInt(v); })(),
        max_concurrent_exams: (function(){ var v=document.getElementById('editUserConcurrent').value; return v==='' ? 2 : parseInt(v); })(),
        max_pdf_size_mb: (function(){ var v=document.getElementById('editUserPdfSize').value; return v==='' ? 1 : parseFloat(v); })(),
        max_storage_size_mb: (function(){ var v=document.getElementById('editUserStorageSize').value; return v==='' ? 0 : parseFloat(v); })(),
        email: document.getElementById('editUserEmail').value.trim(),
        package: document.getElementById('editUserPackage').value,
        roles: []
    };
    // Hanya kirim instansi jika fieldnya visible (tidak disembunyikan untuk operator)
    var instansiEl = document.getElementById('editUserInstansi');
    if (instansiEl && instansiEl.offsetParent !== null) {
        data.instansi = instansiEl.value.trim();
    }
    if (document.getElementById('editRoleGuru').checked) data.roles.push('guru');
    if (document.getElementById('editRolePengawas').checked) data.roles.push('pengawas');
    var editRoleOpEl = document.getElementById('editRoleOperator');
    if (editRoleOpEl && editRoleOpEl.checked) data.roles.push('operator');
    if (data.roles.length === 0) { showToast('Pilih minimal 1 role','error'); return; }

    var newPass = document.getElementById('editUserPassword').value.trim();
    if (newPass) {
        if (newPass.length < 8) {
            showToast('Password baru minimal 8 karakter', 'error');
            return;
        }
        data.password = newPass;
    }

    // Expiry — convert the admin's LOCAL date/time to a UTC timestamp before
    // sending, so it is stored consistently with createUser and the UTC-based
    // read path (which appends 'Z'). Sending a naive local string caused the
    // expiry to drift by the browser's timezone offset on every save.
    var expDate = document.getElementById('editUserExpiry').value;
    var expTime = document.getElementById('editUserExpiryTime').value || '23:59';
    if (expDate) {
        var localExpiry = new Date(expDate + 'T' + expTime + ':00');
        if (!isNaN(localExpiry.getTime())) {
            data.expires_at = localExpiry.toISOString().replace('T', ' ').substring(0, 19);
        } else {
            data.expires_at = expDate + ' ' + expTime + ':00';
        }
    } else {
        data.expires_at = '';
    }

    var btn = e.target.querySelector('button[type="submit"]');
    if (!btn) return;
    var originalHtml = btn.innerHTML;
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';

    apiFetch('/admin/api/users/' + userId + '/edit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    })
        .then(function(r) { return r.json(); })
        .then(function(res) {
            btn.disabled = false;
            btn.innerHTML = originalHtml;
            if (res.success) {
                showToast(res.message, 'success');
                closeEditUserModal();
                loadUsersList(getCurrentUsersPage());
            } else {
                showToast(res.message || 'Gagal menyimpan', 'error');
            }
        })
        .catch(function() {
            btn.disabled = false;
            btn.innerHTML = originalHtml;
            showToast('Gagal menyimpan pengaturan', 'error');
        });
}

function createEditUserModal() {
    var modal = document.createElement('div');
    modal.className = 'modal-overlay';
    modal.id = 'editUserModal';
    modal.style.display = 'none';
    modal.innerHTML = `
        <div class="modal-card glass-card" style="max-width:540px;">
            <div class="modal-header">
                <h3><svg class="icon-svg" style="vertical-align:middle;margin-top:-2px;"><use href="#hi-users"/></svg> Atur User: <span id="editUserUsername" style="color:#a5b4fc;"></span></h3>
                <button class="modal-close" onclick="closeEditUserModal()" aria-label="Tutup"><svg class="icon-svg" style="width:18px;height:18px;"><use href="#hi-x"/></svg></button>
            </div>
            <div class="modal-body">
                <form id="editUserForm" onsubmit="submitEditUser(event)">
                    <input type="hidden" id="editUserId">
                    <div class="form-group" style="margin-bottom:8px;">
                        <label for="editUserName">Nama Lengkap</label>
                        <input type="text" id="editUserName" placeholder="Contoh: Budi Sudarsono" style="width:100%;">
                    </div>
                    <div class="form-group" style="margin-bottom:8px;">
                        <label for="editUserPackage">Paket Langganan (Preset)</label>
                        <select id="editUserPackage" onchange="applyPackagePreset('edit')" style="width:100%;padding:8px 12px;background:rgba(255,255,255,0.05);border:1px solid var(--color-glass-border);border-radius:10px;color:var(--color-text);outline:none;font-size:13px;cursor:pointer;">
                            <option value="free">Free / Trial</option>
                            <option value="guru">Paket Guru</option>
                            <option value="individu">Paket Individu</option>
                            <option value="sekolah_kecil">Paket Sekolah Kecil</option>
                            <option value="sekolah_menengah">Paket Sekolah Menengah</option>
                            <option value="sekolah_besar">Paket Sekolah Besar</option>
                            <option value="sekolah_unggulan">Paket Sekolah Unggulan</option>
                        </select>
                    </div>
                    <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;">
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserExams">Maks Total Ujian</label>
                            <input type="number" id="editUserExams" required min="0" style="width:100%;">
                        </div>
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserConcurrent" title="Maksimal ujian yang berjalan bersamaan (sudah dimulai &amp; bisa dikerjakan siswa)">Ujian Serentak</label>
                            <input type="number" id="editUserConcurrent" required min="0" style="width:100%;">
                        </div>
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserPdfSize">Maks Upload (MB)</label>
                            <input type="number" id="editUserPdfSize" required min="0" step="0.1" style="width:100%;">
                        </div>
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserStorageSize">Maks Storage (MB)</label>
                            <input type="number" id="editUserStorageSize" required min="0" style="width:100%;">
                        </div>
                    </div>
                    <div class="form-group" style="margin-bottom:8px;">
                        <label for="editUserEmail">Email</label>
                        <input type="email" id="editUserEmail" placeholder="Contoh: guru@gmail.com" style="width:100%;">
                    </div>
                    <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;">
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserInstansi">Instansi <span style="font-size:11px;opacity:0.7;">(wajib)</span></label>
                            <input type="text" id="editUserInstansi" required placeholder="Contoh: SMA Negeri 1 Jakarta" style="width:100%;">
                        </div>
                        <div class="form-group" style="margin-bottom:8px;">
                            <label>Role <span class="multirole-hint">multirole</span></label>
                            <div class="role-chips">
                                <label class="role-chip">
                                    <input type="checkbox" id="editRoleGuru" value="guru"> <span title="Guru">G</span>
                                </label>
                                <label class="role-chip">
                                    <input type="checkbox" id="editRolePengawas" value="pengawas"> <span title="Pengawas">P</span>
                                </label>
                                <label class="role-chip" id="editRoleOperatorGroup">
                                    <input type="checkbox" id="editRoleOperator" value="operator"> <span title="Operator">O</span>
                                </label>
                            </div>
                        </div>
                    </div>
                    <div class="form-group" style="margin-bottom:8px;">
                        <label for="editUserPassword">Reset Password <span style="font-size:11px;opacity:0.7;">(Kosongkan jika tidak diubah)</span></label>
                        <input type="password" id="editUserPassword" placeholder="Min. 8 karakter" minlength="8" style="width:100%;">
                    </div>
                    <div class="form-group" style="margin-bottom:12px;">
                        <label>Masa Aktif <span style="font-size:11px;opacity:0.7;">(Kosongkan untuk tidak terbatas)</span></label>
                        <div style="display:flex;gap:8px;">
                            <input type="date" id="editUserExpiry" style="flex:1;">
                            <input type="time" id="editUserExpiryTime" value="23:59" style="width:120px;">
                        </div>
                    </div>
                    <button type="submit" class="btn-upload" style="width:100%;">
                        <svg class="icon-svg"><use href="#hi-check"/></svg> Simpan
                    </button>
                </form>
            </div>
        </div>
    `;
    document.body.appendChild(modal);

    // Hide operator checkbox in edit modal if current user is operator
    if (window.__adminRole === 'operator') {
        var opGroup = document.getElementById('editRoleOperatorGroup');
        if (opGroup) opGroup.style.display = 'none';
        // Sembunyikan field instansi untuk operator
        var instansiGroup = document.getElementById('editUserInstansi').closest('.form-group') || document.getElementById('editUserInstansi').parentNode;
        if (instansiGroup) instansiGroup.style.display = 'none';
    }

    // Close on overlay click
    modal.addEventListener('click', function(e) {
        if (e.target === modal) closeEditUserModal();
    });

    return modal;
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

            if (token.length !== 8 || !/^[A-Z0-9]+$/.test(token)) {
                showToast('Token kustom harus terdiri dari 8 karakter alfanumerik', 'error');
        return;
    }

    apiFetch(`/admin/api/exams/${examId}/edit-token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                var tokenUpdated = false;
                const tokenEl = document.getElementById(`token-${examId}`);
                if (tokenEl) {
                    tokenEl.textContent = res.token;
                    tokenUpdated = true;
                    tokenEl.style.animation = 'none';
                    tokenEl.offsetHeight; // force reflow
                    tokenEl.style.animation = 'toastIn 0.3s ease';
                }
                const tokenDynEl = document.getElementById(`token-dyn-${examId}`);
                if (tokenDynEl) {
                    tokenDynEl.textContent = res.token;
                    tokenUpdated = true;
                    tokenDynEl.style.animation = 'none';
                    tokenDynEl.offsetHeight;
                    tokenDynEl.style.animation = 'toastIn 0.3s ease';
                }
                // Update edit button data attribute
                if (tokenUpdated) {
                    var editBtn = document.querySelector(`.btn-edit[data-exam-id="${examId}"]`);
                    if (editBtn) {
                        editBtn.setAttribute('data-token', res.token);
                        editBtn.setAttribute('onclick', `openEditTokenModal(${examId}, '${jsEscape(res.token)}')`);
                    }
                    // Update permanent token display
                    const permTokenEl = document.querySelector(`#exam-row-${examId} .token-permanent`);
                    if (permTokenEl) {
                        permTokenEl.textContent = res.token;
                        permTokenEl.dataset.token = res.token;
                    }
                    // Also update copy link buttons
                    document.querySelectorAll(`.btn-copy-link[data-token]`).forEach(function(btn) {
                        if (btn.closest('[id^="token-dynamic-' + examId + '"]') || btn.closest('[id^="token-static-' + examId + '"]')) {
                            btn.setAttribute('data-token', res.token);
                        }
                    });
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
        // Close the appropriate modal based on which is open
        const overlay = e.target;
        const modalId = overlay.id;
        if (modalId === 'questionsModal') {
            closeQuestionsModal();
        } else if (modalId === 'changePasswordModal') {
            closeChangePasswordModal();
        } else if (modalId === 'editExamModal') {
            closeEditExamModal();
        } else if (modalId === 'editTokenModal') {
            closeEditTokenModal();
        } else if (modalId === 'detailModal') {
            closeDetailModal();
        } else if (modalId === 'manageUsersModal') {
            closeManageUsersModal();
        } else {
            overlay.style.display = 'none';
        }
    }
});

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
                        btn.innerHTML = '<svg class="icon-svg" style="width:16px;height:16px;vertical-align:middle;margin-top:-2px;" aria-hidden="true"><use href="#hi-eye"/></svg> Hal. Siswa Aktif';
                    } else {
                        btn.style.background = 'rgba(239, 68, 68, 0.15)';
                        btn.style.borderColor = 'rgba(239, 68, 68, 0.3)';
                        btn.style.color = '#f87171';
                        btn.innerHTML = '<svg class="icon-svg" style="width:16px;height:16px;vertical-align:middle;margin-top:-2px;" aria-hidden="true"><use href="#hi-eye-off"/></svg> Hal. Siswa Nonaktif';
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
                        btn.innerHTML = '<svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-lock-open"/></svg> Kunci Terlihat';
                    } else {
                        btn.style.background = 'rgba(107, 114, 128, 0.15)';
                        btn.style.borderColor = 'rgba(107, 114, 128, 0.3)';
                        btn.style.color = '#9ca3af';
                        btn.innerHTML = '<svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-lock"/></svg> Kunci Tersembunyi';
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

// ===== Delegate Exam (Operator) =====
function openDelegateExamModal(examId) {
    const modal = document.getElementById('delegateExamModal');
    if (!modal) return;
    document.getElementById('delegateExamId').value = examId;
    modal.style.display = 'flex';

    const guruSelect = document.getElementById('delegateOwnerSelect');
    guruSelect.innerHTML = '<option value="">-- Memuat data... --</option>';
    guruSelect.disabled = true;

    document.getElementById('delegateCurrentOwner').textContent = '';
    document.getElementById('delegatePengawasList').innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Memuat data pengawas...</div>';

    fetch('/admin/api/exams/' + examId + '/delegate-data')
        .then(function(r) { return r.json(); })
        .then(function(res) {
            guruSelect.innerHTML = '<option value="">-- Tidak ada Guru --</option>';

            if (res.success && res.data) {
                var d = res.data;

                // Current owner
                if (d.current_owner) {
                    document.getElementById('delegateCurrentOwner').textContent = 'Pembuat: ' + d.current_owner.username;
                }
                // Show current delegated teacher
                var delegateLabel = document.getElementById('delegateCurrentLabel');
                if (d.delegated_to) {
                    if (!delegateLabel) {
                        var infoDiv = document.querySelector('#delegateExamModal .form-group');
                        var lbl = document.createElement('div');
                        lbl.id = 'delegateCurrentLabel';
                        lbl.style.cssText = 'font-size:0.8rem;color:#94a3b8;margin-bottom:6px;';
                        lbl.textContent = 'Guru saat ini: ' + d.delegated_to.username;
                        infoDiv.parentNode.insertBefore(lbl, infoDiv);
                    }
                } else if (delegateLabel) {
                    delegateLabel.remove();
                }

                // Guru list
                if (d.available_gurus && d.available_gurus.length > 0) {
                    d.available_gurus.forEach(function(u) {
                        var opt = document.createElement('option');
                        opt.value = u.id;
                        opt.textContent = u.username + ' (' + (u.instansi || '') + ')';
                        if (d.delegated_to && d.delegated_to.id === u.id) {
                            opt.selected = true;
                        }
                        guruSelect.appendChild(opt);
                    });
                    guruSelect.disabled = false;
                } else {
                    guruSelect.innerHTML = '<option value="">-- Tidak ada guru lain di instansi ini --</option>';
                }

                // Pengawas list
                renderDelegatePengawas(d.available_pengawas || [], d.assigned_pengawas_ids || []);
            } else {
                guruSelect.innerHTML = '<option value="">-- Gagal memuat data --</option>';
                document.getElementById('delegatePengawasList').innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Gagal memuat data pengawas.</div>';
            }
        })
        .catch(function() {
            guruSelect.innerHTML = '<option value="">-- Gagal memuat data --</option>';
            document.getElementById('delegatePengawasList').innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Gagal memuat data pengawas.</div>';
        });
}

function renderDelegatePengawas(available, assignedIds) {
    var container = document.getElementById('delegatePengawasList');
    if (!container) return;
    container.innerHTML = '';

    if (!available || available.length === 0) {
        container.innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Tidak ada pengawas tersedia di instansi Anda. Tambah user dengan role "Pengawas" terlebih dahulu.</div>';
        return;
    }

    available.forEach(function(p) {
        var isChecked = assignedIds.indexOf(p.id) !== -1;
        var label = document.createElement('label');
        label.style.cssText = 'display:flex;align-items:center;gap:8px;padding:6px 0;cursor:pointer;font-size:0.85rem;color:var(--color-text);';
        label.innerHTML = '<input type="checkbox" class="delegate-pengawas-checkbox" value="' + p.id + '"' + (isChecked ? ' checked' : '') + '> '
            + escapeHtml(p.username)
            + ' <span style="font-size:0.75rem;color:var(--color-text-muted);">(' + escapeHtml(p.instansi || '') + ')</span>';
        container.appendChild(label);
    });
}

function closeDelegateExamModal() {
    const modal = document.getElementById('delegateExamModal');
    if (modal) modal.style.display = 'none';
}

function confirmDelegateExam() {
    const examId = document.getElementById('delegateExamId').value;
    const guruSelect = document.getElementById('delegateOwnerSelect');
    const newOwnerId = guruSelect.value;

    var pengawasCheckboxes = document.querySelectorAll('#delegatePengawasList .delegate-pengawas-checkbox:checked');
    var pengawasIds = Array.from(pengawasCheckboxes).map(function(cb) { return parseInt(cb.value); });

    var body = {};
    if (newOwnerId) {
        body.new_owner_id = parseInt(newOwnerId);
    }
    body.pengawas_ids = pengawasIds;

    var btn = document.querySelector('#delegateExamModal .btn-upload');
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';

    fetch('/admin/api/exams/' + examId + '/delegate', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': getCsrfToken()
        },
        body: JSON.stringify(body)
    })
    .then(function(r) { return r.json(); })
    .then(function(res) {
        btn.disabled = false;
        btn.textContent = 'Simpan';
        if (res.success) {
            showToast(res.message, 'success');
            closeDelegateExamModal();
            setTimeout(function() { location.reload(); }, 1000);
        } else {
            showToast(res.message || 'Gagal menyimpan', 'error');
        }
    })
    .catch(function() {
        btn.disabled = false;
        btn.textContent = 'Simpan';
        showToast('Koneksi gagal', 'error');
    });
}

function handleEditFileChange(input) {
    const display = document.getElementById('editFileDisplay');
    const textEl = document.getElementById('editFileDisplayText');
    if (!display || !textEl) return;
    
    if (input.files.length > 0) {
        const file = input.files[0];
        const sizeMB = (file.size / 1048576).toFixed(2);
        textEl.textContent = `${file.name} (${sizeMB} MB)`;
        display.style.borderColor = 'var(--color-warning)';
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
    
    // Close all other dropdowns
    document.querySelectorAll('.exam-action-dropdown-content.show').forEach(d => {
        if (d !== dropdown) d.classList.remove('show');
    });
    
    dropdown.classList.remove('drop-up', 'drop-down', 'align-right');
    dropdown.classList.toggle('show');
    
    if (dropdown.classList.contains('show')) {
        void dropdown.offsetHeight;
        const isMobile = window.matchMedia && window.matchMedia('(max-width: 768px)').matches;
        if (isMobile) {
            // Mobile: keep the absolute menu inside the card and flip up/down
            // within the viewport using the drop-up/drop-down classes.
            var r = dropdown.getBoundingClientRect();
            if (r.top < 0) {
                dropdown.classList.add('drop-down');
            } else if (r.bottom > window.innerHeight) {
                dropdown.classList.add('drop-up');
            }
            r = dropdown.getBoundingClientRect();
            if (r.right > window.innerWidth) {
                dropdown.classList.add('align-right');
            } else if (r.left < 0) {
                dropdown.classList.remove('align-right');
            }
            return;
        }
        // Desktop: the dropdown lives inside a <table> (and under a glass-card
        // with backdrop-filter). Chromium paints such a table as its own
        // stacking unit and treats backdrop-filter as a containing block for
        // fixed/absolute descendants — so a menu opened with position:absolute
        // gets its overhang covered by surrounding sections, and the viewport
        // math no longer holds. Fix: re-parent the opened menu to <body> and
        // position it from the button rect, so it always paints above everything.
        const wrapper = dropdown.closest('.exam-action-dropdown') || dropdown.__btnWrap;
        const btn = wrapper ? wrapper.querySelector('.btn-more') : null;
        if (btn && dropdown.parentElement !== document.body) {
            if (!dropdown.__btnWrap) dropdown.__btnWrap = wrapper;
            document.body.appendChild(dropdown);
            const br = btn.getBoundingClientRect();
            const h = dropdown.offsetHeight;
            const gap = 6;
            const spaceUp = br.top;
            const spaceDown = window.innerHeight - br.bottom;
            const openUp = spaceUp >= h + gap || spaceUp >= spaceDown;
            let top = openUp ? br.top - h - gap : br.bottom + gap;
            if (top < 8) top = 8;
            if (top + h > window.innerHeight - 8) top = Math.max(8, window.innerHeight - h - 8);
            let left = br.left;
            if (left + dropdown.offsetWidth > window.innerWidth - 8) {
                left = window.innerWidth - dropdown.offsetWidth - 8;
            }
            if (left < 8) left = 8;
            dropdown.style.position = 'fixed';
            dropdown.style.top = top + 'px';
            dropdown.style.bottom = 'auto';
            dropdown.style.left = left + 'px';
            dropdown.style.marginBottom = '0';
            dropdown.style.maxHeight = (window.innerHeight - 16) + 'px';
            dropdown.style.overflowY = 'auto';
        }
    } else {
        if (dropdown.__btnWrap && dropdown.parentElement === document.body) {
            dropdown.__btnWrap.appendChild(dropdown);
        }
        dropdown.style.position = '';
        dropdown.style.top = '';
        dropdown.style.bottom = '';
        dropdown.style.left = '';
        dropdown.style.marginBottom = '';
        dropdown.style.maxHeight = '';
        dropdown.style.overflowY = '';
    }
}

// Close dropdowns when clicking anywhere outside
document.addEventListener('click', function(event) {
    const clickedBtn = event.target.closest('.btn-more');
    const clickedDropdown = event.target.closest('.exam-action-dropdown-content');

    if (!clickedBtn && !clickedDropdown) {
        document.querySelectorAll('.exam-action-dropdown-content.show').forEach(el => {
            el.classList.remove('show');
        });
    }
});

// Close dropdowns with Escape key
document.addEventListener('keydown', function(event) {
    if (event.key === 'Escape') {
        document.querySelectorAll('.exam-action-dropdown-content.show').forEach(el => {
            el.classList.remove('show');
        });
    }
});

// Bulk Selection Functions
function getCheckboxOwnerId(cb) {
    const attr = cb.getAttribute('data-owner-id') || cb.getAttribute('data-owner');
    return parseInt(attr, 10);
}

function toggleSelectAllExams(masterCheckbox) {
    const checkboxes = document.querySelectorAll('.exam-checkbox');
    checkboxes.forEach(cb => {
        const ownerId = getCheckboxOwnerId(cb);
        if (masterCheckbox.checked && !IS_PRIVILEGED && !isNaN(ownerId) && ownerId !== ADMIN_ID) return;
        cb.checked = masterCheckbox.checked;
    });
    updateBulkActions();
}

function updateBulkActions() {
    const checkboxes = document.querySelectorAll('.exam-checkbox:checked');
    // Only count exams the user can manage (own or privileged)
    const manageable = IS_PRIVILEGED ? Array.from(checkboxes) : Array.from(checkboxes).filter(cb => {
        const ownerId = getCheckboxOwnerId(cb);
        return isNaN(ownerId) || ownerId === ADMIN_ID;
    });
    const totalSelected = manageable.length;
    
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
            manageable.forEach(cb => {
                if (cb.getAttribute('data-status') === 'active') {
                    hasActive = true;
                }
            });
            bulkToggleBtn.innerHTML = hasActive ? `<svg class="icon-svg" aria-hidden="true"><use href="#hi-lock"/></svg> Nonaktifkan Terpilih (${totalSelected})` : `<svg class="icon-svg" aria-hidden="true"><use href="#hi-lock-open"/></svg> Aktifkan Terpilih (${totalSelected})`;
        }
    } else {
        if (bulkDeleteBtn) bulkDeleteBtn.style.display = 'none';
        if (bulkToggleBtn) bulkToggleBtn.style.display = 'none';
        // Uncheck any that aren't manageable
        if (!IS_PRIVILEGED && checkboxes.length > 0 && totalSelected === 0) {
            checkboxes.forEach(cb => { cb.checked = false; });
        }
        const selectAll = document.getElementById('selectAllExams');
        if (selectAll) selectAll.checked = false;
    }
}

async function bulkDeleteExams() {
    const checkboxes = document.querySelectorAll('.exam-checkbox:checked');
    if (checkboxes.length === 0) return;

    const ids = Array.from(checkboxes).map(cb => parseInt(cb.value));
    // Confirmation already handled by confirmBulkDelete() in template

    try {
        const response = await apiFetch('/admin/api/exams/bulk-delete', {
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


// ===== Lost Functions (recovered from template inline scripts) =====


function filterSubmissions() {
    const filterVal = document.getElementById('filterExam').value;
    const params = new URLSearchParams(window.location.search);
    if (filterVal) {
        params.set('exam_id', filterVal);
    } else {
        params.delete('exam_id');
    }
    params.set('page', '1');
    const qs = params.toString();
    window.location.href = window.location.pathname + (qs ? '?' + qs : '');
}

function exportSubmissions() {
    const examId = document.getElementById('filterExam').value;
    const tzOffset = new Date().getTimezoneOffset();
    let url = '/admin/api/submissions/export?tz_offset=' + tzOffset;
    if (examId) {
        url += '&exam_id=' + examId;
    }
    window.location.href = url;
}

function deleteSubmission(id) {
    showConfirm('Hapus hasil ujian siswa ini?', 'Data akan dihapus secara permanen.').then(ok => {
        if (!ok) return;
        apiFetch(`/admin/api/submissions/${id}/delete`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    const row = document.getElementById(`submission-row-${id}`);
                    if (row) {
                        row.style.opacity = '0';
                        row.style.transform = 'translateX(-20px)';
                        row.style.transition = 'all 0.3s';
                        setTimeout(() => row.remove(), 300);
                    }
                } else {
                    showToast(res.message || 'Gagal menghapus', 'error');
                }
            })
            .catch(() => showToast('Koneksi gagal', 'error'));
    });
}

function saveSaasSettings(e) {
    e.preventDefault();
    const form = e.currentTarget;
    if (!form.checkValidity()) {
        form.reportValidity();
        showToast("Lengkapi atau perbaiki kolom setelan yang ditandai", "error");
        return;
    }

    const saveButton = document.getElementById("saveSaasSettingsBtn");
    const originalButtonHTML = saveButton ? saveButton.innerHTML : "";
    if (saveButton) {
        saveButton.disabled = true;
        saveButton.textContent = "Menyimpan...";
    }

    const email_verification_enabled = document.getElementById('emailEnabledInput').checked;
    const email_domain_whitelist = (document.getElementById('emailDomainWhitelistInput') || {}).value || '';
    const smtp_host = document.getElementById('smtpHostInput').value.trim();
    const smtp_port = document.getElementById('smtpPortInput').value.trim();
    const smtp_user = document.getElementById('smtpUserInput').value.trim();
    const smtp_password = document.getElementById('smtpPasswordInput').value.trim();
    const smtp_sender_name = document.getElementById('smtpSenderNameInput').value.trim();
    const default_max_exams = parseInt(document.getElementById('defaultExamsInput').value);
    const default_max_concurrent_exams = parseInt(document.getElementById('defaultConcurrentInput').value);
    const default_max_pdf_size_mb = parseFloat(document.getElementById('defaultPdfInput').value);
    const default_active_days = parseInt(document.getElementById('defaultActiveDaysInput').value);
    const android_version = document.getElementById('androidVersionInput').value.trim();
    const webapp_version = document.getElementById('webappVersionInput').value.trim();
    const seo_title = document.getElementById('seoTitleInput').value.trim();
    const seo_description = document.getElementById('seoDescriptionInput').value.trim();
    const seo_keywords = document.getElementById('seoKeywordsInput').value.trim();
    const seo_index = document.getElementById('seoIndexInput').checked;
    const footer_text = document.getElementById('footerTextInput').value.trim();
    const footer_tagline = document.getElementById('footerTaglineInput').value.trim();

    // Monetization toggles
    const voucher_redeem_enabled = !!(document.getElementById('voucherRedeemEnabledInput') || {}).checked;

    // Cloudflare Turnstile bot protection + per-IP registration cap
    const turnstile_enabled = !!(document.getElementById('turnstileEnabledInput') || {}).checked;
    const turnstile_site_key = (document.getElementById('turnstileSiteKeyInput') || {}).value || '';
    const turnstile_secret_key = (document.getElementById('turnstileSecretKeyInput') || {}).value || '';
    const max_accounts_per_ip = parseInt((document.getElementById('maxAccountsPerIpInput') || {}).value) || 0;

    apiFetch('/admin/api/saas-settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            email_verification_enabled, email_domain_whitelist, smtp_host, smtp_port, smtp_user, smtp_password, smtp_sender_name,
            default_max_exams, default_max_concurrent_exams, default_max_pdf_size_mb,
            default_active_days, android_version, webapp_version,
            seo_title, seo_description, seo_keywords, seo_index,
            footer_text, footer_tagline,
            voucher_redeem_enabled,
            turnstile_enabled, turnstile_site_key, turnstile_secret_key,
            max_accounts_per_ip
        })
    })
    .then(r => r.json())
    .then(res => {
        if (res.success) {
            showToast(res.message, 'success');
            loadSaasSettings();
        } else {
            showToast(res.message || 'Gagal menyimpan', 'error');
        }
    })
    .catch(() => showToast('Gagal menyimpan setelan SaaS', 'error'))
    .finally(() => {
        if (saveButton) {
            saveButton.disabled = false;
            saveButton.innerHTML = originalButtonHTML;
        }
    });
}

function testSmtpConnection() {
    const smtp_host = document.getElementById('smtpHostInput').value.trim();
    const smtp_port = document.getElementById('smtpPortInput').value.trim();
    const smtp_user = document.getElementById('smtpUserInput').value.trim();
    const smtp_password = document.getElementById('smtpPasswordInput').value.trim();

    if (!smtp_host || !smtp_port) {
        showToast('SMTP Host dan Port harus diisi', 'error');
        return;
    }

    const btn = document.getElementById('testSmtpBtn');
    const btnText = document.getElementById('testSmtpText');
    const btnIcon = document.getElementById('testSmtpIcon');

    // Disable button and show loading state
    btn.disabled = true;
    btn.style.opacity = '0.6';
    btnText.innerText = 'Menghubungkan...';
    if (btnIcon) btnIcon.style.animation = 'loginSpinner 0.8s linear infinite';

    apiFetch('/admin/api/saas-settings/test-smtp', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ smtp_host, smtp_port, smtp_user, smtp_password })
    })
    .then(r => r.json().then(data => ({ status: r.status, body: data })))
    .then(({ status, body }) => {
        if (status === 200 && body.success) {
            showToast(body.message || 'Koneksi SMTP berhasil terhubung!', 'success');
        } else {
            showToast(body.message || 'Koneksi SMTP gagal', 'error');
        }
    })
    .catch(() => {
        showToast('Terjadi kesalahan saat menghubungi server', 'error');
    })
    .finally(() => {
        btn.disabled = false;
        btn.style.opacity = '1';
        btnText.innerText = 'Test Koneksi SMTP';
        if (btnIcon) btnIcon.style.animation = 'none';
    });
}

function syncLimitFields() {
    var guruChecked = document.getElementById('roleGuru').checked;
    var pengawasOnly = document.getElementById('rolePengawas').checked && !guruChecked;
    var limitInput = document.getElementById('limitInput');
    var concurrentInput = document.getElementById('concurrentInput');
    var pdfInput = document.getElementById('pdfSizeInput');
    if (pengawasOnly) {
        limitInput.disabled = true;
        limitInput.value = '0';
        if (concurrentInput) { concurrentInput.disabled = true; concurrentInput.value = '0'; }
        pdfInput.disabled = true;
        pdfInput.value = '0';
    } else {
        limitInput.disabled = false;
        if (concurrentInput) concurrentInput.disabled = false;
        pdfInput.disabled = false;
    }
}

document.addEventListener('DOMContentLoaded', function() {
    var guruCb = document.getElementById('roleGuru');
    var pengawasCb = document.getElementById('rolePengawas');
    if (guruCb && pengawasCb) {
        guruCb.addEventListener('change', syncLimitFields);
        pengawasCb.addEventListener('change', syncLimitFields);
        syncLimitFields();
    }
});

function createUser(e) {
    e.preventDefault();
    const username = document.getElementById('usernameInput').value.trim();
    const name = document.getElementById('nameInput')?.value?.trim() || '';
    const password = document.getElementById('passwordInput').value;
    const emailEl = document.getElementById('emailInput');
    const email = emailEl ? emailEl.value.trim() : '';
    const instansi = document.getElementById('instansiInput').value.trim() || 'personal';
    var roles = [];
    if (document.getElementById('roleGuru').checked) roles.push('guru');
    if (document.getElementById('rolePengawas').checked) roles.push('pengawas');
    var roleOpEl = document.getElementById('roleOperator');
    if (roleOpEl && roleOpEl.checked) roles.push('operator');
    if (roles.length === 0) { showToast('Pilih minimal 1 role','error'); return; }
    const max_exams = parseInt(document.getElementById('limitInput').value);
    const max_concurrent_exams = parseInt(document.getElementById('concurrentInput').value);
    const max_pdf_size_mb = parseFloat(document.getElementById('pdfSizeInput').value);
    const max_storage_size_mb = parseFloat(document.getElementById('storageSizeInput').value);
    const opExpiryEl = document.getElementById('operatorExpiresAt');
    let expires_at = '';
    if (opExpiryEl && opExpiryEl.value) {
        expires_at = opExpiryEl.value;
    } else {
        const expDate = document.getElementById('newUserExpiry').value;
        const expTime = document.getElementById('newUserExpiryTime').value || '23:59';
        if (expDate) {
            const localDateTime = new Date(`${expDate}T${expTime}`);
            if (!isNaN(localDateTime.getTime())) {
                expires_at = localDateTime.toISOString().replace('T', ' ').substring(0, 19);
            }
        }
    }
    const package = document.getElementById('packageSelect').value;
    if (!username || !password) { showToast('Username dan password wajib diisi','error'); return; }
    apiFetch('/admin/api/users', {
        method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ username, name, password, email, instansi, roles, max_exams, max_concurrent_exams, max_pdf_size_mb, max_storage_size_mb, expires_at, package })
    }).then(r=>r.json()).then(res => {
        if (res.success) {
            showToast(res.message,'success');
            document.getElementById('newUserForm').reset();
            loadUsersList(1);
        }
        else showToast(res.message||'Gagal','error');
    }).catch(()=>showToast('Gagal menghubungi server','error'));
}

function resetNewUserFormDefaults() {
    document.getElementById('limitInput').value = 0;
    document.getElementById('concurrentInput').value = 0;
    document.getElementById('pdfSizeInput').value = 1.0;
    syncLimitFields();
}

function loadSaasSettings() {
    apiFetch('/admin/api/saas-settings')
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                const s = res.settings;
                document.getElementById('emailEnabledInput').checked = s.email_verification_enabled || false;
                var _edw = document.getElementById('emailDomainWhitelistInput');
                if (_edw) _edw.value = s.email_domain_whitelist || '';
                document.getElementById('smtpHostInput').value = s.smtp_host || 'smtp.gmail.com';
                document.getElementById('smtpPortInput').value = s.smtp_port || '587';
                document.getElementById('smtpUserInput').value = s.smtp_user || '';
                document.getElementById('smtpPasswordInput').value = s.smtp_password || '';
                document.getElementById('smtpSenderNameInput').value = s.smtp_sender_name || 'EXAMVAN';
                document.getElementById('defaultExamsInput').value = s.default_max_exams || 3;
                document.getElementById('defaultConcurrentInput').value = s.default_max_concurrent_exams || 2;
                document.getElementById('defaultPdfInput').value = s.default_max_pdf_size_mb || 1;
                document.getElementById('defaultActiveDaysInput').value = s.default_active_days || 1;
                document.getElementById('androidVersionInput').value = s.android_version || '2.1.9';
                document.getElementById('webappVersionInput').value = s.webapp_version || '2.1.9';
                document.getElementById('seoTitleInput').value = s.seo_title || '';
                document.getElementById('seoDescriptionInput').value = s.seo_description || '';
                document.getElementById('seoKeywordsInput').value = s.seo_keywords || '';
                document.getElementById('seoIndexInput').checked = s.seo_index || false;
                document.getElementById('footerTextInput').value = s.footer_text || '© 2026 EXAMVAN Team. All rights reserved.';
                document.getElementById('footerTaglineInput').value = s.footer_tagline || '';

                // Monetization toggles (default enabled when unset)
                var _vr = document.getElementById('voucherRedeemEnabledInput');
                if (_vr) _vr.checked = s.voucher_redeem_enabled !== false;

                // Cloudflare Turnstile bot protection (secret is masked on read)
                var _ts = document.getElementById('turnstileEnabledInput');
                if (_ts) _ts.checked = s.turnstile_enabled || false;
                var _tssk = document.getElementById('turnstileSiteKeyInput');
                if (_tssk) _tssk.value = s.turnstile_site_key || '';
                var _tskr = document.getElementById('turnstileSecretKeyInput');
                if (_tskr) _tskr.value = s.turnstile_secret_key || '';

                // Per-IP registration cap
                var _map = document.getElementById('maxAccountsPerIpInput');
                if (_map) _map.value = (typeof s.max_accounts_per_ip === 'number') ? s.max_accounts_per_ip : 3;

                toggleEmailFields();
                toggleTurnstileFields();
            }
        });
}

function toggleEmailFields() {
    const enabled = document.getElementById('emailEnabledInput').checked;
    document.getElementById('emailSettingsFields').style.display = enabled ? 'flex' : 'none';
}

function toggleTurnstileFields() {
    const enabled = document.getElementById('turnstileEnabledInput').checked;
    const fields = document.getElementById('turnstileFields');
    if (fields) fields.style.display = enabled ? 'flex' : 'none';
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

    const actionLabel = targetStatus === 'inactive' ? 'Nonaktifkan' : 'Aktifkan';
    const confirmed = await showConfirm(`${actionLabel} ${ids.length} ujian terpilih?`, '', `Ya, ${actionLabel}`, 'Batal');
    if (!confirmed) return;

    try {
        const response = await apiFetch('/admin/api/exams/bulk-toggle', {
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




// ===== Submission Detail Modal =====
var activeSubmissionId = null;

function showSubmissionDetail(id) {
    activeSubmissionId = id;
    const container = document.getElementById('detailAnswersContainer');
    container.innerHTML = '<div style="color:var(--color-text-secondary); text-align:center; padding: 20px;">Memuat detail jawaban...</div>';
    document.getElementById('detailStudentName').textContent = '...';
    document.getElementById('detailStudentClass').textContent = '...';

    document.getElementById('detailModal').style.display = 'flex';

    apiFetch(`/admin/api/submissions/${id}/detail`)
        .then(r => r.json())
        .then(res => {
            if (!res.success) {
                showToast(res.message || 'Gagal memuat detail', 'error');
                closeDetailModal();
                return;
            }

            document.getElementById('detailStudentName').textContent = res.student_name;
            document.getElementById('detailStudentClass').textContent = res.student_class;
            document.getElementById('detailExamName').textContent = res.exam_name || '—';
            document.getElementById('detailStartTime').textContent = res.start_time ? localizeUTC(res.start_time) : '—';
            document.getElementById('detailSubmitTime').textContent = res.created_at ? localizeUTC(res.created_at) : '—';
            document.getElementById('detailMacAddress').textContent = res.mac_address || '—';

            container.innerHTML = '';
            const answers = res.answers || {};
            const questions = res.questions || [];
            const evaluated = res.evaluated_answers || {};

            if (questions.length === 0) {
                container.innerHTML = '<div style="color:var(--color-text-muted); text-align:center; padding: 20px;">Tidak ada konfigurasi soal untuk ujian ini.</div>';
                return;
            }

            questions.forEach(q => {
                const qNum = String(q.number);
                const weight = parseFloat(q.weight || 1.0);
                const studentAns = answers[qNum];
                const correctAns = q.key;
                const evalData = evaluated[qNum] || {};

                const earnedPoints = evalData.earned || 0;
                const statusText = evalData.statusText || 'Belum Dijawab';
                const statusClass = evalData.statusClass || 'unanswered';

                const fmtAns = (val) => {
                    if (val === undefined || val === null) return '—';
                    if (Array.isArray(val)) return val.join(', ');
                    if (typeof val === 'object') return Object.keys(val).map(k => `${k}→${val[k]}`).join(', ');
                    return String(val);
                };

                const item = document.createElement('div');
                item.className = `detail-answer-item ${escapeHtml(statusClass)}`;
                const statusColor = statusClass === 'correct' ? '#34d399' : statusClass === 'partial' ? '#f59e0b' : '#f87171';
                item.innerHTML = `
                    <span class="detail-q-num">No. ${escapeHtml(qNum)}</span>
                    <span class="detail-q-ans">${escapeHtml(fmtAns(studentAns))}</span>
                    <span class="detail-q-key">${escapeHtml(fmtAns(correctAns))}</span>
                    <div style="text-align:right; width:110px;">
                        <span class="detail-q-status" style="color:${statusColor}">${escapeHtml(statusText)}</span>
                        <span class="detail-q-points">${earnedPoints.toFixed(1)} / ${weight.toFixed(1)} Poin</span>
                    </div>
                `;
                container.appendChild(item);
            });
        })
        .catch(() => {
            showToast('Gagal memuat detail jawaban', 'error');
            closeDetailModal();
        });
}

function closeDetailModal() {
    document.getElementById('detailModal').style.display = 'none';
}

// ===== Identity Popup (klik nama siswa di tabel hasil) =====
(function() {
    var table = document.getElementById('submissionsTable');
    if (!table) return;
    var LABEL_MAP = { student_name: 'Nama', exam_number: 'Nomor Ujian', student_class: 'Kelas', nama: 'Nama', nomor_ujian: 'Nomor Ujian', kelas: 'Kelas' };
    table.addEventListener('click', function(e) {
        var btn = e.target.closest('.submission-identity-btn');
        if (btn) {
            var popup = document.getElementById('identityPopup');
            if (!popup) return;
            // Baca identity_data dari data-identity
            var raw = btn.getAttribute('data-identity');
            var data = {};
            try { data = JSON.parse(raw); } catch (x) {}
            // Jika identity_data kosong, fallback dari kolom tabel
            var html = '<div class="identity-popup-header">Identitas Siswa</div><div class="identity-popup-body">';
            var hasData = false;
            var seenVals = {};
            for (var k in data) {
                if (data.hasOwnProperty(k) && data[k]) {
                    var v = String(data[k]);
                    // Deduplicate: skip if same value already shown (standard key mirrors custom key)
                    if (seenVals[v]) continue;
                    seenVals[v] = true;
                    var label = LABEL_MAP[k] || k.replace(/_/g, ' ').replace(/\b\w/g, function(c) { return c.toUpperCase(); });
                    html += '<div class="identity-popup-item"><span class="idp-label">' + escapeHtml(label) + '</span><strong class="idp-value">' + escapeHtml(v) + '</strong></div>';
                    hasData = true;
                }
            }
            // Fallback: tampilkan nama dari kolom tabel
            if (!hasData) {
                var name = btn.textContent.trim();
                if (name) {
                    html += '<div class="identity-popup-item"><span class="idp-label">Nama</span><strong class="idp-value">' + escapeHtml(name) + '</strong></div>';
                }
            }
            html += '</div>';
            popup.innerHTML = html;
            // Tutup popup lain
            document.querySelectorAll('.identity-popup.show').forEach(function(p) { if (p !== popup) p.classList.remove('show'); });
            // Toggle
            var isOpen = popup.classList.contains('show');
            if (isOpen) {
                popup.classList.remove('show');
            } else {
                popup.classList.add('show');
                var rect = btn.getBoundingClientRect();
                popup.style.position = 'fixed';
                popup.style.top = Math.min(rect.bottom + 4, window.innerHeight - 200) + 'px';
                popup.style.left = Math.max(10, Math.min(rect.left, window.innerWidth - 240)) + 'px';
            }
            e.stopPropagation();
            return;
        }
        // Click di luar popup
        if (!e.target.closest('.identity-popup')) {
            document.querySelectorAll('.identity-popup.show').forEach(function(p) { p.classList.remove('show'); });
        }
    });
})();

// Global click: close identity popup
document.addEventListener('click', function() {
    document.querySelectorAll('.identity-popup.show').forEach(function(p) { p.classList.remove('show'); });
});

// ===== Search Exams (client-side) =====
function filterExamRows() {
    const query = document.getElementById('searchExam').value.trim().toLowerCase();
    const status = document.getElementById('statusFilter')?.value || '';
    const rows = document.querySelectorAll('#examTable tbody tr');
    let visibleCount = 0;
    rows.forEach(row => {
        const nameEl = row.querySelector('.td-name');
        const tokenEl = row.querySelector('.token-code');
        const creatorEl = row.querySelector('td[data-label="Pembuat"]');
        const statusEl = row.querySelector('.status-badge');
        const text = [
            nameEl?.textContent || '',
            tokenEl?.textContent || '',
            creatorEl?.textContent || ''
        ].join(' ').toLowerCase();
        const matchQuery = !query || text.includes(query);
        // Select values are machine keys (active/inactive/tombstoned) while the
        // badges read localized labels (Aktif/Nonaktif/Ditombstone) — map the
        // value to the label so the client-side filter matches the rows.
        const statusText = { active: 'Aktif', inactive: 'Nonaktif', tombstoned: 'Ditombstone' };
        const matchStatus = !status || (statusEl?.textContent.trim() === (statusText[status] || status));
        if (matchQuery && matchStatus) {
            row.style.display = '';
            visibleCount++;
        } else {
            row.style.display = 'none';
        }
    });
    // Show/hide empty state
    let emptyEl = document.querySelector('.empty-search-state');
    if (visibleCount === 0 && rows.length > 0) {
        if (!emptyEl) {
            emptyEl = document.createElement('div');
            emptyEl.className = 'empty-search-state';
            emptyEl.style.cssText = 'text-align:center;padding:40px 20px;color:var(--color-text-muted);';
            emptyEl.innerHTML = '<svg class="icon-svg-xl" style="margin:0 auto 16px;opacity:0.4;"><use href="#hi-search"/></svg><p>Tidak ditemukan ujian yang cocok dengan pencarian Anda.</p>';
            const table = document.getElementById('examTable');
            table.parentNode.insertBefore(emptyEl, table.nextSibling);
        }
        emptyEl.style.display = '';
        document.getElementById('examTable').style.display = 'none';
    } else {
        if (emptyEl) emptyEl.style.display = 'none';
        document.getElementById('examTable').style.display = '';
    }
    // Update clear button
    const btn = document.getElementById('searchClearBtn');
    if (btn) btn.style.display = query || status ? 'flex' : 'none';
}

const debounceSearch = debounce(filterExamRows, 300);
// Keep old name as alias for inline onkeyup="searchExams()"
function searchExams() { filterExamRows(); }

function clearSearch() {
    const input = document.getElementById('searchExam');
    if (input) input.value = '';
    const statusEl = document.getElementById('statusFilter');
    if (statusEl) statusEl.value = '';
    filterExamRows();
}

// Override inline onchange handler: filter client-side instead of page reload
function searchExamsWithStatus() {
    filterExamRows();
}

// Enter key also triggers client-side filter
document.addEventListener('DOMContentLoaded', function() {
    const searchInput = document.getElementById('searchExam');
    if (searchInput) {
        searchInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                filterExamRows();
            }
        });
    }
});

// ===== Calculate Duration =====
document.addEventListener('DOMContentLoaded', function() {
    // Keyboard shortcuts (admin pages with dashboard)
    if (document.querySelector('.dashboard')) {
        initKeyboardShortcuts();
        if (!window.location.pathname.includes('submissions')) {
            startAutoRefresh(30);
        }
    }

    document.querySelectorAll('.duration-cell').forEach(function(el) {
        var start = el.getAttribute('data-start');
        var end = el.getAttribute('data-end');
        if (start && end) {
            try {
                var toIso = function(v) {
                    var s = String(v).trim();
                    if (s.includes(' ') && !s.includes('T')) s = s.replace(' ', 'T');
                    if (!s.endsWith('Z') && !s.includes('+') && !(/-\d{2}:\d{2}$/.test(s))) s += 'Z';
                    return s;
                };
                var s = new Date(toIso(start));
                var e = new Date(toIso(end));
                var diff = Math.floor((e - s) / 1000);
                if (diff > 0) {
                    var h = Math.floor(diff / 3600);
                    var m = Math.floor((diff % 3600) / 60);
                    var parts = [];
                    if (h > 0) parts.push(h + 'j');
                    parts.push(m + 'm');
                    el.textContent = parts.join(' ');
                }
            } catch(_) {}
        }
    });
});

// Subscription Package Presets Config
const EXAMVAN_PACKAGES = {
    free: { name: 'Free / Trial', exams: 1, concurrent: 1, pdf: 1, storage: 50 },
    guru: { name: 'Paket Guru', exams: 1, concurrent: 1, pdf: 10, storage: 100 },
    individu: { name: 'Paket Individu', exams: 2, concurrent: 2, pdf: 30, storage: 300 },
    sekolah_kecil: { name: 'Paket Sekolah Kecil', exams: 3, concurrent: 3, pdf: 50, storage: 500 },
    sekolah_menengah: { name: 'Paket Sekolah Menengah', exams: 5, concurrent: 5, pdf: 200, storage: 2000 },
    sekolah_besar: { name: 'Paket Sekolah Besar', exams: 10, concurrent: 10, pdf: 500, storage: 5000 },
    sekolah_unggulan: { name: 'Paket Sekolah Unggulan', exams: 99999, concurrent: 99999, pdf: 99999, storage: 999999 }
};

function applyPackagePreset(type) {
    if (type === 'new') {
        const pkgKey = document.getElementById('packageSelect').value;
        const limits = EXAMVAN_PACKAGES[pkgKey];
        if (limits) {
            document.getElementById('limitInput').value = limits.exams;
            document.getElementById('concurrentInput').value = limits.concurrent;
            document.getElementById('pdfSizeInput').value = limits.pdf;
            document.getElementById('storageSizeInput').value = limits.storage;
        }
    } else if (type === 'edit') {
        const pkgKey = document.getElementById('editUserPackage').value;
        const limits = EXAMVAN_PACKAGES[pkgKey];
        if (limits) {
            document.getElementById('editUserExams').value = limits.exams;
            document.getElementById('editUserConcurrent').value = limits.concurrent;
            document.getElementById('editUserPdfSize').value = limits.pdf;
            document.getElementById('editUserStorageSize').value = limits.storage;
        }
    }
}

