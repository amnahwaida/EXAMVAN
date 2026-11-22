/* EXAMVAN Admin Panel - JavaScript */

// __adminHasRole returns true when the current admin's session role string
// contains the given role. The session role is usually normalized at login
// ("operator", "superadmin") but can also hold the raw role JSON (e.g.
// '["guru","operator"]') when it was refreshed by a voucher redeem/activate
// without re-login — so compare by role membership, not by exact string.
function __adminHasRole(target) {
    var roleStr = window.__adminRole || '';
    if (roleStr === target) return true;
    try {
        var arr = JSON.parse(roleStr);
        if (Array.isArray(arr)) return arr.indexOf(target) !== -1;
    } catch (e) { /* not JSON — fall through to substring check */ }
    return (',' + roleStr.replace(/["\[\]]/g, '') + ',').indexOf(',' + target + ',') !== -1;
}

// CSRF Token Helper

// Toast notification

// File input display
const pdfInput = document.getElementById('pdfFile');
if (pdfInput) {
    const pdfMaxMB = parseFloat(pdfInput.getAttribute('data-max-mb')) || 0;
    pdfInput.addEventListener('change', function() {
        const display = document.getElementById('fileDisplay');
        const textEl = display.querySelector('.file-text');
        if (this.files.length > 0) {
            const file = this.files[0];
            const sizeMB = (file.size / 1048576).toFixed(2);
            textEl.textContent = `${file.name} (${sizeMB} MB)`;
            if (pdfMaxMB > 0 && file.size > pdfMaxMB * 1048576) {
                textEl.textContent += ` — melebihi batas ${pdfMaxMB} MB`;
                display.style.borderColor = 'var(--color-warning)';
            } else {
                display.style.borderColor = 'var(--color-success)';
            }
        } else {
            textEl.textContent = 'Pilih file PDF...';
            display.style.borderColor = '';
        }
    });
}

// ===== T10b/S19: validasi field-level form upload ujian =====
// Pesan error tampil inline per field via helper admin-core.js (setFieldError
// dikerjakan agen lain — panggil defensif), bukan hanya toast yang lenyap.
// Toast tetap ada sebagai pelengkap ringkasan saat submit.
function showUploadFieldError(inputEl, msg) {
    if (typeof setFieldError === 'function') {
        setFieldError(inputEl, msg);
    } else {
        // Fallback minimal bila helper belum tersedia di halaman ini
        inputEl.setAttribute('aria-invalid', 'true');
    }
}

function clearUploadFieldError(inputEl) {
    if (typeof clearFieldError === 'function') {
        clearFieldError(inputEl);
    } else {
        inputEl.removeAttribute('aria-invalid');
    }
}

function validateUploadExamName() {
    const el = document.getElementById('examName');
    if (!el) return true;
    if (!el.value.trim()) {
        showUploadFieldError(el, 'Nama ujian wajib diisi');
        return false;
    }
    clearUploadFieldError(el);
    return true;
}

function validateUploadPdfFile() {
    const el = document.getElementById('pdfFile');
    if (!el) return true;
    if (!el.files.length) {
        showUploadFieldError(el, 'Pilih file PDF terlebih dahulu');
        return false;
    }
    const maxUploadMB = parseFloat(el.getAttribute('data-max-mb')) || 0;
    if (maxUploadMB > 0 && el.files[0].size > maxUploadMB * 1048576) {
        showUploadFieldError(el, 'Ukuran file melebihi batas ' + maxUploadMB + ' MB');
        return false;
    }
    clearUploadFieldError(el);
    return true;
}

function validateUploadCustomToken() {
    const el = document.getElementById('customToken');
    if (!el) return true;
    // Kosong = generate otomatis (field opsional). Huruf kecil dinormalisasi
    // karena input hanya di-style text-transform:uppercase.
    const val = el.value.trim().toUpperCase();
    if (!val) {
        clearUploadFieldError(el);
        return true;
    }
    if (val.length !== 8 || !/^[A-Z0-9]+$/.test(val)) {
        showUploadFieldError(el, 'Token kustom harus tepat 8 karakter huruf/angka (A-Z, 0-9)');
        return false;
    }
    clearUploadFieldError(el);
    return true;
}

// Jalankan semua validator; fokuskan field invalid pertama agar guru langsung
// diarahkan ke masalahnya. Return true bila seluruh field valid.
function validateUploadFormFields() {
    const results = [
        validateUploadExamName(),
        validateUploadPdfFile(),
        validateUploadCustomToken()
    ];
    if (results.indexOf(false) !== -1) {
        const order = ['examName', 'pdfFile', 'customToken'];
        for (var i = 0; i < order.length; i++) {
            var el = document.getElementById(order[i]);
            if (el && el.getAttribute && el.getAttribute('aria-invalid') === 'true') { el.focus(); break; }
        }
        return false;
    }
    return true;
}

// Validasi live: tampilkan saat blur dengan isian salah, clear segera saat
// isian diperbaiki (input/change).
(function () {
    const nameInput = document.getElementById('examName');
    if (nameInput) {
        nameInput.addEventListener('blur', validateUploadExamName);
        nameInput.addEventListener('input', function() {
            if (nameInput.getAttribute('aria-invalid') === 'true') validateUploadExamName();
        });
    }
    const pdfFileEl = document.getElementById('pdfFile');
    if (pdfFileEl) {
        pdfFileEl.addEventListener('change', validateUploadPdfFile);
    }
    const tokenInput = document.getElementById('customToken');
    if (tokenInput) {
        tokenInput.addEventListener('blur', validateUploadCustomToken);
        tokenInput.addEventListener('input', function() {
            if (tokenInput.getAttribute('aria-invalid') === 'true') validateUploadCustomToken();
        });
    }
})();

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

        // T10b: validasi inline per-field — hasilnya ditandai di masing-masing
        // field, toast hanya pelengkap ringkasan.
        if (!validateUploadFormFields()) {
            showToast('Periksa kembali isian yang ditandai merah', 'error');
            return;
        }

        const formData = new FormData();
        formData.append('name', nameInput.value.trim());
        formData.append('pdf_file', fileInput.files[0]);
        if (customTokenInput && customTokenInput.value.trim()) {
            formData.append('custom_token', customTokenInput.value.trim().toUpperCase());
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
                    showApiErrorToast(res, 'Upload gagal');
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

// Toggle exam status — update UI in-place tanpa reload.
// S1: perubahan status kini lewat dialog konfirmasi dulu (label tombol
// menyesuaikan arah toggle) supaya badge tidak ter-trigger salah ketuk saat
// scroll/zoom daftar ujian di HP.
function toggleExam(examId) {
    const badge = document.getElementById('status-' + examId);
    if (!badge) return;
    if (badge.dataset.toggling === '1') return; // request sedang berjalan

    const wasActive = badge.classList.contains('status-active');
    // Nama ujian untuk pesan konfirmasi: ambil dari checkbox baris (data-name),
    // fallback ke link nama ujian di kolom yang sama.
    const row = document.getElementById('exam-row-' + examId);
    const nameSrc = row ? (row.querySelector('.exam-checkbox') || row.querySelector('.exam-link')) : null;
    let examName = nameSrc ? ((nameSrc.getAttribute('data-name') || nameSrc.textContent || '') + '').trim() : '';
    if (!examName) examName = 'ini';

    const confirmMsg = wasActive
        ? `Nonaktifkan ujian "${examName}"?`
        : `Aktifkan ujian "${examName}"?`;
    const confirmDetail = wasActive
        ? 'Siswa tidak bisa login ujian ini selama statusnya nonaktif.'
        : 'Siswa bisa kembali login dan mengerjakan ujian ini.';
    const confirmLabel = wasActive ? 'Ya, Nonaktifkan' : 'Ya, Aktifkan';

    showConfirm(confirmMsg, confirmDetail, confirmLabel, 'Batal').then(ok => {
        if (!ok) return; // batal: badge tidak pernah dikunci, tidak ada yang perlu dipulihkan
        // Disable sementara untuk cegah double-click selama request berjalan
        badge.dataset.toggling = '1';
        badge.style.pointerEvents = 'none';
        badge.style.opacity = '0.5';

        apiFetch(`/admin/api/exams/${examId}/toggle`, { method: 'POST' })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    // Update badge in-place berdasarkan new_status dari server.
                    // status-tombstoned dibuang: mengaktifkan ujian otomatis
                    // membersihkan penanda tombstone di server, jadi badge harus
                    // kembali normal (Aktif/Nonaktif), bukan lagi "Nonaktif Otomatis".
                    var isActive = res.new_status === 'active';
                    badge.classList.remove('status-tombstoned');
                    badge.classList.toggle('status-active', isActive);
                    badge.classList.toggle('status-inactive', !isActive);
                    badge.textContent = isActive ? 'Aktif' : 'Nonaktif';
                    // Status juga diekspos ke assistive technology via aria-pressed,
                    // dan title disesuaikan dengan arah toggle berikutnya.
                    badge.setAttribute('aria-pressed', isActive ? 'true' : 'false');
                    badge.title = isActive ? 'Klik untuk menonaktifkan ujian' : 'Klik untuk mengaktifkan ujian';
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
                delete badge.dataset.toggling;
                badge.style.pointerEvents = '';
                badge.style.opacity = '';
            });
    });
}

// Delete exam
function deleteExam(examId, examName) {
    showConfirm(`Hapus ujian "${examName}"?`, 'File PDF juga akan dihapus permanen.').then(ok => {
        if (!ok) return;

        // DeleteExam kini mengembalikan error_code R2 (R2_NOT_CONFIGURED) saat
        // backend R2 hilang/nonaktif — pakai showApiErrorToast untuk pesan ramah
        // + style warning; suppressApiErrorToast mencegah toast dobel dengan
        // listener global 'api:error'.
        apiFetch(`/admin/api/exams/${examId}/delete`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            suppressApiErrorToast: true
        })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    // R6: hapus row langsung dari DOM tanpa reload halaman
                    // penuh, agar posisi scroll & pagination tidak hilang.
                    // Animasi keluar yang lama sia-sia karena tetap diakhiri
                    // reload.
                    const row = document.getElementById(`exam-row-${examId}`);
                    if (row) row.remove();
                    // Perbarui kartu statistik dashboard bila tersedia
                    // (refreshDashboardStats guard sendiri terhadap #statsGrid).
                    if (typeof refreshDashboardStats === 'function') {
                        refreshDashboardStats();
                    }
                } else {
                    showApiErrorToast(res, 'Gagal menghapus ujian');
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
    // S29: delegasi ke copyCode (admin-core.js) yang berguard clipboard API
    // + fallback execCommand + toast — aman di origin HTTP LAN.
    copyCode(String(token));
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

// ===== S2: guard unsaved-changes modal konfigurasi soal =====
// Semua input di modal (bobot soal, level keamanan, jumlah soal, jadwal,
// pesan ucapan, warna panel, identitas siswa, pengawas) menandai state kotor
// lewat listener terdelegasi di bawah; perubahan yang dilakukan programatik
// (setAllWeights, tambah/hapus/sisip soal, reorder drag-drop, preset warna,
// hapus jadwal) menandai manual karena tidak memicu event input/change.
let questionsConfigDirty = false;
let questionsDiscardConfirmOpen = false;

function markQuestionsConfigDirty() {
    // Tanpa guard activeExamId: field hanya bisa berubah lewat UI saat modal
    // terbuka, dan openQuestionsModal me-reset flag ini pasca data server
    // ter-render — sehingga pemanggilan liar di luar konteks tak berbahaya.
    questionsConfigDirty = true;
}

function resetQuestionsConfigDirty() {
    questionsConfigDirty = false;
}

function openQuestionsModal(examId, examName) {
    activeExamName = examName;
    // Modal dibuka dengan data segar dari server — mulai dari state bersih.
    questionsConfigDirty = false;
    // (reset penuh lewat helper setelah data server ter-render; assignment
    // di sini menutup celah klik-kotak antara open dan fetch selesai.)
    resetQuestionsConfigDirty();
    document.getElementById('modalTitle').textContent = `Atur Soal Ujian: ${examName}`;
    const container = document.getElementById('questionsList');
    container.innerHTML = '<div style="color:var(--color-text-secondary); text-align:center; padding: 20px;">Memuat data soal...</div>';

    // Open modal first
    // R25: toggle display lewat API Modal terpusat (admin-core.js).
    Modal.open('questionsModal');

    // Track fetch to prevent race condition
    const fetchId = ++pendingFetchId;

    apiFetch(`/admin/api/exams/${examId}/questions`)
        .then(r => r.json())
        .then(res => {
            if (fetchId !== pendingFetchId) return; // Stale response
            if (res.success) {
                activeExamId = examId; // Set AFTER data loaded
                // Data server sudah dirender ulang (edit yang dilakukan saat
                // loading ikut terganti) — mulai hitung dirty dari titik ini.
                resetQuestionsConfigDirty();
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
                const toleranceInput = document.getElementById('examScheduleTolerance');
                if (toleranceInput) {
                    toleranceInput.value = (res.schedule_tolerance_minutes !== undefined && res.schedule_tolerance_minutes !== null)
                        ? res.schedule_tolerance_minutes
                        : 5;
                }
                // Gate mid-exam: banner peringatan saat ujian berjalan —
                // kunci/bobot akan dibekukan server meski tombol Simpan hijau.
                const runningBanner = document.getElementById('examRunningBanner');
                if (runningBanner) runningBanner.style.display = res.exam_running ? 'block' : 'none';
                // Congratulations message (nullable — empty when unset)
                const congratsEl = document.getElementById('examCongratsMessage');
                if (congratsEl) congratsEl.value = res.congrats_message || '';
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

function closeQuestionsModal(force) {
    // S2: Batal/✕/Escape/backdrop saat ada perubahan belum disimpan →
    // konfirmasi dulu sebelum membuang. force=true dipakai jalur internal
    // (pasca-setuju buang, atau pasca simpan sukses).
    if (!force && questionsConfigDirty && !questionsDiscardConfirmOpen) {
        questionsDiscardConfirmOpen = true;
        showConfirm('Buang perubahan?', 'Perubahan konfigurasi soal belum disimpan dan akan hilang bila modal ditutup.', 'Ya, Buang', 'Lanjut Edit')
            .then(function(ok) {
                questionsDiscardConfirmOpen = false;
                if (ok) closeQuestionsModal(true);
            });
        return;
    }
    resetQuestionsConfigDirty();
    // R25: delegasi ke API Modal terpusat; fallback manual hanya bila
    // admin-core.js tidak ikut dimuat (halaman/embedding terisolasi).
    if (typeof Modal === 'undefined') {
        var _qModal = document.getElementById('questionsModal');
        if (_qModal) _qModal.style.display = 'none';
    } else {
        Modal.close('questionsModal');
    }
    activeExamId = null;
    activeExamName = '';
}

// S2: tandai kotor pada input/change apa pun di dalam modal (bubble dari
// field statis maupun yang dirender JS dinamis). Dipasang eksplisit agar
// mudah diuji dan tidak terlewat saat modal dirender ulang.
(function () {
    const qModal = document.getElementById('questionsModal');
    if (!qModal) return;
    qModal.addEventListener('input', markQuestionsConfigDirty);
    qModal.addEventListener('change', markQuestionsConfigDirty);
    // R29: pengganti onchange inline pada string HTML createNewQuestionCard —
    // perubahan tipe soal disesuaikan field kartunya via delegasi change.
    qModal.addEventListener('change', function (e) {
        var t = e.target;
        if (t && t.classList && t.classList.contains('q-type-select')) {
            onQuestionTypeChange(t);
        }
    });
    // Koreksi guru menghapus penanda kartu invalid (outline merah) saat isi
    // kartu diedit — penanda hanya hidup sampai nilainya disentuh lagi.
    qModal.addEventListener('input', function (e) {
        var t = e.target;
        if (t && t.classList && (t.classList.contains('q-key-input') || t.classList.contains('q-options-input') || t.classList.contains('q-statements-input') || t.classList.contains('q-left-items-input') || t.classList.contains('q-right-items-input'))) {
            var card = t.closest('.question-editor-card');
            if (card) {
                card.style.outline = '';
                if (t.classList.contains('q-options-input') || t.classList.contains('q-statements-input') || t.classList.contains('q-left-items-input') || t.classList.contains('q-right-items-input')) {
                    renderQuestionAssist(card);
                }
            }
        }
    });

    function isQuestionsModalOpen() {
        return qModal.style.display !== 'none' && qModal.style.display !== '';
    }

    // Escape & backdrop click secara normal ditangani Global Modal Manager
    // (admin-core.js) yang memaksa-menutup overlay yang masih terbuka — itu
    // akan membuang perubahan melewati guard di atas. Keduanya di-intercept
    // di fase capture SAAT state kotor saja; selain itu alur lama berjalan
    // normal. Manager sendiri melewatkan keydown bila defaultPrevented, jadi
    // tidak perlu menyentuh admin-core.js (refactor penuh = S16 batch 4).
    window.guardQuestionsModalEscape = function guardQuestionsModalEscape(e) {
        if (e.key !== 'Escape') return;
        if (!isQuestionsModalOpen() || !questionsConfigDirty || questionsDiscardConfirmOpen) return;
        e.preventDefault();
        e.stopPropagation();
        closeQuestionsModal(); // tampilkan konfirmasi buang via guard closeQuestionsModal
    };
    window.guardQuestionsModalBackdropClick = function guardQuestionsModalBackdropClick(e) {
        if (e.target !== qModal) return; // hanya klik backdrop sungguhan
        if (!isQuestionsModalOpen() || !questionsConfigDirty || questionsDiscardConfirmOpen) return;
        e.stopPropagation();
        closeQuestionsModal();
    };
    document.addEventListener('keydown', guardQuestionsModalEscape, true);
    document.addEventListener('click', guardQuestionsModalBackdropClick, true);
})();

// Render the student access & answer-key controls inside the questions modal
// (moved here from the pengawasan page).
function renderStudentAccessControls(examId, res) {
    const wrap = document.getElementById('studentAccessControls');
    if (!wrap) return;
    const token = res.token || '';
    const prActive = res.public_results === 1;
    const saActive = res.show_answers === 1;
    wrap.innerHTML = `
        <a href="/hasil/${escapeHtml(token)}" target="_blank" rel="noopener noreferrer" class="pd-action-btn pd-action-link" title="Buka halaman hasil ujian untuk siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-link"/></svg> Halaman Siswa
        </a>
        <button id="btn-public-results-${escapeHtml(examId)}" data-exam-id="${escapeHtml(examId)}" data-action="toggle-public-results" class="pd-action-btn ${prActive ? 'pd-action-active' : 'pd-action-danger'}" title="Aktifkan/nonaktifkan halaman hasil ujian siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="${prActive ? '#hi-eye' : '#hi-eye-off'}"/></svg> ${prActive ? 'Hal. Siswa Aktif' : 'Hal. Siswa Nonaktif'}
        </button>
        <button id="btn-show-answers-${escapeHtml(examId)}" data-exam-id="${escapeHtml(examId)}" data-action="toggle-show-answers" class="pd-action-btn ${saActive ? 'pd-action-warning' : 'pd-action-muted'}" title="Tampilkan/sembunyikan kunci jawaban untuk siswa">
            <svg class="icon-svg" style="width:14px;height:14px;"><use href="${saActive ? '#hi-lock-open' : '#hi-lock'}"/></svg> ${saActive ? 'Kunci Terlihat' : 'Kunci Tersembunyi'}
        </button>`;
}

// L47: preset warna panel kini mengirim token triplet ("var(--rgb-rose-500)"
// dst.) atau hex langsung — hex diturunkan HANYA di satu titik (resolvePanelHex)
// dari nilai computed var(--rgb-*), sehingga perubahan palet ikut ke preset.
function resolvePanelHex(value) {
    if (!value) return '';
    var v = String(value).trim();
    if (/^#[0-9a-fA-F]{3,8}$/.test(v)) return v.toUpperCase();
    var m = v.match(/var\(\s*(--[\w-]+)\s*\)/);
    if (!m) return '';
    var triplet = getComputedStyle(document.documentElement).getPropertyValue(m[1]).trim();
    var parts = triplet.split(',').map(function (s) { return parseInt(s, 10); });
    if (parts.length !== 3 || parts.some(function (n) { return isNaN(n); })) return '';
    return '#' + parts.map(function (n) {
        return Math.max(0, Math.min(255, n)).toString(16).padStart(2, '0');
    }).join('').toUpperCase();
}

function setPanelColor(value) {
    var hex = resolvePanelHex(value);
    if (!hex) return; // nilai tak dikenal: jangan menulis hex kosong ke input
    var colorInput = document.getElementById('examPanelColor');
    var hexInput = document.getElementById('panelColorHex');
    markQuestionsConfigDirty(); // S2: preset warna programatik tidak memicu event input/change
    if (colorInput) colorInput.value = hex;
    if (hexInput) hexInput.value = hex;
}

// Sinkronkan input kunci tersembunyi dengan pilihan Benar/Salah per
// pernyataan. Input kunci tetap menjadi satu-satunya sumber yang dibaca
// getQuestionsFromEditor / findQuestionCardProblems, sehingga format JSON
// yang dikirim ke server tidak berubah.
function syncTrueFalseKey(card) {
    if (!card) return;
    const keyInput = card.querySelector('.q-key-input');
    const selects = card.querySelectorAll('.q-tf-answer-select');
    if (!keyInput || !selects || selects.length === 0) return;
    keyInput.value = Array.from(selects).map(s => s.value).join(', ');
}

// Tampilkan satu kontrol Benar/Salah untuk SETIAP pernyataan. Jumlah kontrol
// mengikuti jumlah baris pernyataan secara otomatis — guru tidak perlu
// mengetik daftar kunci secara manual. Format satu pernyataan (tanpa daftar)
// tetap memakai input Kunci Jawaban lama.
function renderTrueFalseAnswerRows(card) {
    if (!card) return;
    const typeSel = card.querySelector('.q-type-select');
    const type = typeSel ? typeSel.value : '';
    const tfGroup = card.querySelector('.q-tf-answers-group');
    const keyGroup = card.querySelector('.q-key-group');
    const list = card.querySelector('.q-tf-answer-list');
    const statementsInput = card.querySelector('.q-statements-input');
    const keyInput = card.querySelector('.q-key-input');
    if (!tfGroup || !list) return;

    if (type !== 'true_false') {
        tfGroup.style.display = 'none';
        if (keyGroup) keyGroup.style.display = 'flex';
        return;
    }

    const statements = statementsInput
        ? statementsInput.value.split('\n').map(x => x.trim()).filter(x => x)
        : [];

    if (statements.length === 0) {
        tfGroup.style.display = 'none';
        if (keyGroup) keyGroup.style.display = 'flex';
        return;
    }

    tfGroup.style.display = 'flex';
    if (keyGroup) keyGroup.style.display = 'none';

    const existing = Array.from(list.querySelectorAll('.q-tf-answer-select')).map(s => s.value);
    const keyRaw = keyInput ? keyInput.value.trim() : '';
    const keyValues = keyRaw
        ? keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x)
        : [];

    list.innerHTML = '';
    statements.forEach((statement, idx) => {
        let value = existing[idx];
        if (value !== 'TRUE' && value !== 'FALSE') value = keyValues[idx];
        if (value !== 'TRUE' && value !== 'FALSE') value = 'TRUE';

        const row = document.createElement('div');
        row.className = 'q-tf-answer-row';

        const label = document.createElement('span');
        label.className = 'q-tf-answer-label';
        label.textContent = `${idx + 1}. ${statement}`;

        const select = document.createElement('select');
        select.className = 'q-tf-answer-select';
        [['TRUE', 'Benar'], ['FALSE', 'Salah']].forEach(([val, text]) => {
            const opt = document.createElement('option');
            opt.value = val;
            opt.textContent = text;
            if (val === value) opt.selected = true;
            select.appendChild(opt);
        });
        select.addEventListener('change', function () {
            syncTrueFalseKey(card);
            markQuestionsConfigDirty();
        });

        row.appendChild(label);
        row.appendChild(select);
        list.appendChild(row);
    });

    syncTrueFalseKey(card);
}

function splitEditorLines(value) {
    return String(value || '').split('\n').map(x => x.trim()).filter(x => x);
}

function splitEditorCsv(value) {
    return String(value || '').split(',').map(x => x.trim()).filter(x => x);
}

function syncChoiceKey(card) {
    if (!card) return;
    const keyInput = card.querySelector('.q-key-input');
    const typeSel = card.querySelector('.q-type-select');
    if (!keyInput || !typeSel) return;
    const checked = Array.from(card.querySelectorAll('.q-choice-key-input:checked')).map(el => el.value);
    keyInput.value = typeSel.value === 'single_choice'
        ? (checked.length ? checked[0] : '')
        : checked.join(', ');
}

function renderChoiceKeyOptions(card) {
    if (!card) return;
    const list = card.querySelector('.q-choice-key-list');
    const optionsInput = card.querySelector('.q-options-input');
    const keyInput = card.querySelector('.q-key-input');
    const typeSel = card.querySelector('.q-type-select');
    if (!list || !optionsInput || !keyInput || !typeSel) return;

    const type = typeSel.value;
    const hint = card.querySelector('.q-choice-hint');
    if (hint) {
        hint.textContent = type === 'single_choice'
            ? 'Pilih satu jawaban yang benar.'
            : 'Centang semua jawaban yang benar.';
    }
    const options = splitEditorCsv(optionsInput.value);
    const previous = Array.from(list.querySelectorAll('.q-choice-key-input:checked')).map(el => el.value);
    const fromKey = splitEditorCsv(keyInput.value);
    const selected = (previous.length ? previous : fromKey).map(x => x.toUpperCase());
    const valid = selected.filter(v => options.some(o => o.toUpperCase() === v));

    list.innerHTML = '';
    if (options.length === 0) {
        const hint = document.createElement('div');
        hint.className = 'q-assist-hint';
        hint.textContent = 'Isi pilihan jawaban terlebih dahulu.';
        list.appendChild(hint);
        return;
    }

    const groupName = 'q-choice-' + Math.random().toString(36).slice(2);
    options.forEach((option, idx) => {
        const row = document.createElement('label');
        row.className = 'q-choice-row';

        const input = document.createElement('input');
        input.type = type === 'single_choice' ? 'radio' : 'checkbox';
        input.className = 'q-choice-key-input';
        input.value = option;
        input.name = groupName;
        if (type === 'single_choice') {
            input.checked = valid.length ? valid[0] === option.toUpperCase() : idx === 0;
        } else {
            input.checked = valid.includes(option.toUpperCase());
        }
        input.addEventListener('change', function () {
            syncChoiceKey(card);
            markQuestionsConfigDirty();
        });

        const text = document.createElement('span');
        text.textContent = option;
        row.appendChild(input);
        row.appendChild(text);
        list.appendChild(row);
    });

    syncChoiceKey(card);
}

function syncMatchingKey(card) {
    if (!card) return;
    const keyInput = card.querySelector('.q-key-input');
    const selects = card.querySelectorAll('.q-matching-select');
    if (!keyInput || selects.length === 0) return;
    keyInput.value = Array.from(selects)
        .map(select => `${select.dataset.left}:${select.value}`)
        .join(', ');
}

function renderMatchingPairs(card) {
    if (!card) return;
    const list = card.querySelector('.q-matching-pairs-list');
    const leftInput = card.querySelector('.q-left-items-input');
    const rightInput = card.querySelector('.q-right-items-input');
    const keyInput = card.querySelector('.q-key-input');
    if (!list || !leftInput || !rightInput || !keyInput) return;

    const left = splitEditorLines(leftInput.value);
    const right = splitEditorLines(rightInput.value);
    const previous = {};
    list.querySelectorAll('.q-matching-select').forEach(select => {
        previous[select.dataset.left] = select.value;
    });
    const fromKey = {};
    splitEditorCsv(keyInput.value).forEach(pair => {
        const idx = pair.indexOf(':');
        if (idx > 0) {
            fromKey[pair.slice(0, idx).trim()] = pair.slice(idx + 1).trim().toUpperCase();
        }
    });

    list.innerHTML = '';
    if (left.length === 0 || right.length === 0) {
        const hint = document.createElement('div');
        hint.className = 'q-assist-hint';
        hint.textContent = 'Isi minimal satu item kiri dan satu item kanan.';
        list.appendChild(hint);
        return;
    }

    left.forEach((item, idx) => {
        let value = previous[item];
        if (!value || !right.some(r => r.toUpperCase() === String(value).toUpperCase())) {
            value = fromKey[item];
        }
        if (!value || !right.some(r => r.toUpperCase() === String(value).toUpperCase())) {
            value = right[idx] || right[0];
        }

        const row = document.createElement('div');
        row.className = 'q-matching-row';

        const label = document.createElement('span');
        label.className = 'q-matching-label';
        label.textContent = item;

        const select = document.createElement('select');
        select.className = 'q-matching-select';
        select.dataset.left = item;
        right.forEach(optionValue => {
            const option = document.createElement('option');
            option.value = optionValue;
            option.textContent = optionValue;
            if (optionValue.toUpperCase() === String(value).toUpperCase()) option.selected = true;
            select.appendChild(option);
        });
        select.addEventListener('change', function () {
            syncMatchingKey(card);
            markQuestionsConfigDirty();
        });

        row.appendChild(label);
        row.appendChild(select);
        list.appendChild(row);
    });

    syncMatchingKey(card);
}

function renderQuestionAssist(card) {
    if (!card) return;
    const typeSel = card.querySelector('.q-type-select');
    const type = typeSel ? typeSel.value : '';
    const keyGroup = card.querySelector('.q-key-group');
    const choiceGroup = card.querySelector('.q-choice-key-group');
    const matchingGroup = card.querySelector('.q-matching-group');
    const tfGroup = card.querySelector('.q-tf-answers-group');
    const optionsInput = card.querySelector('.q-options-input');
    const leftInput = card.querySelector('.q-left-items-input');
    const rightInput = card.querySelector('.q-right-items-input');

    if (choiceGroup) choiceGroup.style.display = 'none';
    if (matchingGroup) matchingGroup.style.display = 'none';
    if (tfGroup) tfGroup.style.display = 'none';
    if (keyGroup) keyGroup.style.display = 'flex';

    if (type === 'true_false') {
        renderTrueFalseAnswerRows(card);
        return;
    }

    if (type === 'single_choice' || type === 'multiple_choice') {
        const options = optionsInput ? splitEditorCsv(optionsInput.value) : [];
        if (options.length > 0) {
            if (choiceGroup) choiceGroup.style.display = 'flex';
            if (keyGroup) keyGroup.style.display = 'none';
            renderChoiceKeyOptions(card);
        }
        return;
    }

    if (type === 'matching') {
        if (matchingGroup) matchingGroup.style.display = 'flex';
        const left = leftInput ? splitEditorLines(leftInput.value) : [];
        const right = rightInput ? splitEditorLines(rightInput.value) : [];
        if (left.length > 0 && right.length > 0) {
            if (keyGroup) keyGroup.style.display = 'none';
            renderMatchingPairs(card);
        }
    }
}

function createNewQuestionCard(q, num) {
    const type = q.type || 'single_choice';
    const key = q.key || '';
    const weight = q.weight !== undefined ? q.weight : 1.0;
    const partial = q.partial_scoring ? 'checked' : '';
    const partialVisibility = (type === 'multiple_choice' || type === 'matching' || type === 'true_false') ? 'block' : 'none';
    const optionsVisibility = (type === 'true_false' || type === 'short_answer' || type === 'matching') ? 'none' : 'block';
    const statementsVisibility = type === 'true_false' ? 'block' : 'none';
    const statements = Array.isArray(q.statements) ? q.statements : [];
    const statementsVal = statements.join('\n');
    
    let optionsVal = '';
    let matchingLeftVal = '1\n2\n3';
    let matchingRightVal = 'A\nB\nC';
    if (type === 'single_choice' || type === 'multiple_choice') {
        // Data lama disimpan sebagai `choices`, format kanonik baru `options`.
        const opts = q.options || q.choices;
        optionsVal = opts && opts.length ? opts.join(', ') : 'A, B, C, D, E';
    } else if (type === 'matching') {
        const left = Array.isArray(q.left_items) && q.left_items.length ? q.left_items : ['1', '2', '3'];
        const right = Array.isArray(q.right_items) && q.right_items.length ? q.right_items : ['A', 'B', 'C'];
        matchingLeftVal = left.join('\n');
        matchingRightVal = right.join('\n');
        optionsVal = `Kiri: ${left.join(', ')} | Kanan: ${right.join(', ')}`;
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
                <select class="q-type-select">
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
            <div class="q-field-group q-statements-group" style="display: ${statementsVisibility};">
                <label>Pernyataan Benar/Salah</label>
                <span class="q-field-hint">Tulis satu pernyataan per baris.</span>
                <textarea class="q-statements-input" rows="3" placeholder="Pernyataan 1&#10;Pernyataan 2">${escapeHtml(statementsVal)}</textarea>
            </div>
            <div class="q-field-group q-key-group">
                <label>Kunci Jawaban</label>
                <input type="text" class="q-key-input" value="${escapeHtml(keyVal)}" placeholder="Jawaban..." title="PG: A,B,C | Menjodohkan: 1:A,2:B | Benar/Salah: TRUE,FALSE">
            </div>
            <div class="q-field-group q-assist-panel q-tf-answers-group" style="display: none;">
                <label>Kunci Benar/Salah</label>
                <span class="q-field-hint">Pilih Benar atau Salah untuk setiap pernyataan.</span>
                <div class="q-tf-answer-list"></div>
            </div>
            <div class="q-field-group q-assist-panel q-choice-key-group" style="display: none;">
                <label>Kunci Jawaban</label>
                <span class="q-field-hint q-choice-hint">Pilih jawaban yang benar.</span>
                <div class="q-choice-key-list"></div>
            </div>
            <div class="q-field-group q-assist-panel q-matching-group" style="display: none;">
                <div class="q-matching-grid">
                    <div class="q-matching-field">
                        <label>Item Kiri</label>
                        <span class="q-field-hint">Satu item per baris.</span>
                        <textarea class="q-left-items-input" rows="3" placeholder="1&#10;2&#10;3">${escapeHtml(matchingLeftVal)}</textarea>
                    </div>
                    <div class="q-matching-field">
                        <label>Item Kanan</label>
                        <span class="q-field-hint">Satu item per baris.</span>
                        <textarea class="q-right-items-input" rows="3" placeholder="A&#10;B&#10;C">${escapeHtml(matchingRightVal)}</textarea>
                    </div>
                </div>
                <label>Pasangan Benar</label>
                <span class="q-field-hint">Pilih pasangan untuk setiap item kiri.</span>
                <div class="q-matching-pairs-list"></div>
            </div>
            <div class="q-field-group q-options-group" style="display: ${optionsVisibility};">
                <label>Pilihan</label>
                <input type="text" class="q-options-input" value="${escapeHtml(optionsVal)}" placeholder="A, B, C, D, E">
            </div>
        </div>
        <button class="btn-sm btn-delete btn-remove-q" data-action="question-remove" title="Hapus Soal"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-trash"/></svg></button>
    `;
    renderQuestionAssist(card);
    return card;
}

function createDivider(index) {
    const div = document.createElement('div');
    div.className = 'q-editor-divider';
    div.dataset.index = index;
    div.innerHTML = `
        <div class="q-divider-line"></div>
        <button class="btn-add-inline" data-action="question-insert-at" data-index="${index}" title="Sisipkan Soal Baru Di Sini"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-plus"/></svg> Sisipkan Soal</button>
        <div class="q-divider-line"></div>
    `;
    return div;
}

function insertQuestionAt(index) {
    const container = document.getElementById('questionsList');
    const newQ = { type: 'single_choice', weight: 1.0 };
    const newCard = createNewQuestionCard(newQ, 0);
    const newDivider = createDivider(0);
    markQuestionsConfigDirty(); // S2: soal baru = perubahan belum tersimpan
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
    markQuestionsConfigDirty(); // S2: hapus soal = perubahan belum tersimpan
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
            // T28: tombol divider TIDAK boleh membawa atribut onclick — satu
            // klik menjadi dua panggilan insertQuestionAt karena jalur
            // delegasi question-insert-at sudah aktif. Yang wajib diperbarui
            // saat reindex hanyalah data-index yang dibaca delegasi.
            const btn = child.querySelector('.btn-add-inline');
            if (btn) {
                btn.setAttribute('data-index', String(dividerCount));
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
    markQuestionsConfigDirty(); // S2: reorder mengubah nomor/nomor urut soal
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
            <button class="modal-close" data-action="modal-remove" aria-label="Tutup"><svg class="icon-svg" style="width:18px;height:18px;"><use href="#hi-x"/></svg></button>
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
            <button class="btn-sm" data-action="modal-remove">Batal</button>
            <button class="btn-upload" data-action="bulk-weight-apply">Terapkan</button>
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
    markQuestionsConfigDirty(); // S2: set nilai programatik tidak memicu event input/change

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
        ${isLocked ? '<span class="ifield-locked-badge" title="Field bawaan, tidak bisa dihapus"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-lock"/></svg></span>' : '<button class="ifield-remove-btn" data-action="identity-field-remove" title="Hapus field" aria-label="Hapus field"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-x"/></svg></button>'}
    `;

    container.appendChild(row);
}

function addIdentityField() {
    const container = document.getElementById('identityFieldsList');
    const count = container.children.length;
    const field = { key: 'field_' + (count + 1), label: '', required: false };
    markQuestionsConfigDirty(); // S2: baris identitas baru = perubahan belum tersimpan
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
    // S90: pemicu dropdown wajib hidup untuk keyboard — fokusable, ber-role
    // button, statusnya diumumkan via aria-expanded (di-update di
    // togglePengawasDropdown), dan Enter/Space memicu toggle yang sama
    // dengan klik.
    header.setAttribute('tabindex', '0');
    header.setAttribute('role', 'button');
    header.setAttribute('aria-haspopup', 'listbox');
    header.setAttribute('aria-expanded', 'false');
    header.setAttribute('aria-label', 'Pilih pengawas');
    header.addEventListener('click', function(e) { e.stopPropagation(); togglePengawasDropdown(); });
    header.addEventListener('keydown', function(e) {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            e.stopPropagation();
            togglePengawasDropdown();
        } else if (e.key === 'Escape') {
            // R132: konvensi menu topbar — Escape menutup dropdown dan fokus
            // kembali ke pemicunya agar navigasi keyboard tidak tercerai.
            var ddEsc = document.getElementById('pengawasDropdown');
            if (ddEsc && ddEsc.style.display !== 'none' && ddEsc.style.display) {
                ddEsc.style.display = 'none';
                header.setAttribute('aria-expanded', 'false');
                header.style.borderColor = 'var(--color-glass-border)';
                header.focus();
                e.stopPropagation();
            }
        }
    });
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
                // S90: warna chip via token (paritas S80–S83), bukan literal ungu.
                chip.style.cssText = 'display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:12px;font-size:0.75rem;font-weight:500;background:rgba(var(--rgb-accent),0.15);color:var(--color-accent-light);border:1px solid rgba(var(--rgb-accent),0.25);';
                chip.textContent = p.username;
                // S90: tombol hapus chip berupa <button> native-fokusable
                // dengan nama aksesibel yang memuat nama pengawasnya.
                var x = document.createElement('button');
                x.type = 'button';
                x.style.cssText = 'cursor:pointer;margin-left:2px;font-size:13px;line-height:1;opacity:0.7;background:transparent;border:none;color:var(--color-text-secondary);padding:0;font-family:inherit;';
                x.setAttribute('aria-label', 'Hapus pengawas ' + p.username);
                x.addEventListener('click', function(ev) { ev.stopPropagation(); var cb = Array.prototype.find.call(document.querySelectorAll('#pengawasDropdown .pengawas-checkbox'), function(c) { return c.value === String(p.id); }); if (cb) { cb.checked = false; renderHeaderChips(); } });
                chip.appendChild(x);
                header.appendChild(chip);
            });
        }
        // S90: panah di-append sebagai elemen — konkatenasi innerHTML += akan
        // menserialisasi chip dan MEMBUNUH listener tombol hapusnya.
        var arrow = document.createElement('span');
        arrow.style.cssText = 'margin-left:auto;font-size:11px;color:var(--color-text-muted);';
        arrow.setAttribute('aria-hidden', 'true');
        arrow.textContent = '▼';
        header.appendChild(arrow);
    }
    renderHeaderChips();

    // Dropdown body (hidden by default)
    var dropdown = document.createElement('div');
    dropdown.id = 'pengawasDropdown';
    dropdown.style.cssText = 'display:none;position:absolute;top:100%;left:0;right:0;z-index:var(--z-dropdown);margin-top:4px;background:rgba(30,30,50,0.98);border:1px solid rgba(255,255,255,0.1);border-radius:10px;max-height:220px;overflow-y:auto;box-shadow:0 8px 32px rgba(0,0,0,0.4);backdrop-filter:blur(12px);';

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
        opt.innerHTML = '<input type="checkbox" class="pengawas-checkbox" value="' + escapeHtml(p.id) + '"' + (isChecked ? ' checked' : '') + ' style="accent-color:var(--color-primary-bright);cursor:pointer;"> '
            + '<span style="font-weight:500;">' + escapeHtml(p.username) + '</span>'
            + ' <span style="font-size:0.7rem;color:var(--color-text-muted);margin-left:auto;">' + escapeHtml(p.role || 'Pengawas') + '</span>';
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
                // R132: tutup via klik-luar wajib me-reset state SR — tanpa
                // ini screen reader tetap membaca "expanded=true".
                hd.setAttribute('aria-expanded', 'false');
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
        hd.setAttribute('aria-expanded', 'true'); // S90: status diumumkan ke SR
        // Focus search
        var sb = dd.querySelector('input[type="text"]');
        if (sb) setTimeout(function() { sb.focus(); }, 50);

        // S113: fokus default saat terbuka ada di search box - Escape dari sana
        // tidak pernah menyentuh handler header dan jatuh ke Modal Manager
        // (menutup SELURUH modal delegasi; form terisi hilang).
        if (!dd.__escWired) {
            dd.__escWired = true;
            dd.addEventListener('keydown', function(ev) {
                if (ev.key !== 'Escape') return;
                ev.stopPropagation();
                var hd2 = document.getElementById('pengawasDropdownHeader');
                dd.style.display = 'none';
                if (hd2) {
                    hd2.setAttribute('aria-expanded', 'false');
                    hd2.style.borderColor = 'var(--color-glass-border)';
                    hd2.focus();
                }
            });
        }
    } else {
        dd.style.display = 'none';
        hd.style.borderColor = 'var(--color-glass-border)';
        hd.setAttribute('aria-expanded', 'false'); // S90
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
    const statementsInput = card.querySelector('.q-statements-input');
    const statementsGroup = card.querySelector('.q-statements-group');
    const leftItemsInput = card.querySelector('.q-left-items-input');
    const rightItemsInput = card.querySelector('.q-right-items-input');
    const keyInput = card.querySelector('.q-key-input');
    const partialGroup = card.querySelector('.q-partial-group');
    const type = selectEl.value;
    
    if (type === 'multiple_choice' || type === 'matching' || type === 'true_false') {
        partialGroup.style.display = 'block';
    } else {
        partialGroup.style.display = 'none';
        const partialCb = card.querySelector('.q-partial-checkbox');
        if (partialCb) partialCb.checked = false;
    }
    
    if (statementsGroup) statementsGroup.style.display = type === 'true_false' ? 'block' : 'none';
    if (type === 'true_false' || type === 'short_answer' || type === 'matching') {
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
        if (statementsInput) statementsInput.value = '';
        keyInput.value = 'TRUE';
    } else if (type === 'matching') {
        if (leftItemsInput) leftItemsInput.value = '1\n2\n3';
        if (rightItemsInput) rightItemsInput.value = 'A\nB\nC';
        optionsInput.value = 'Kiri: 1, 2, 3 | Kanan: A, B, C';
        keyInput.value = '1:A, 2:B, 3:C';
    } else if (type === 'short_answer') {
        optionsInput.value = '';
        keyInput.value = '';
    }
    renderQuestionAssist(card);
}

// ===== T15: guard penggantian isi editor soal ==============================
// Generate & Import XML sama-sama menimpa SELURUH isi editor via
// renderQuestions() (container.innerHTML = ''). Tanpa guard, guru dengan
// 30 soal yang sedang diedit kehilangan semuanya seketika dan flag dirty
// tetap false sehingga Batal menutup tanpa peringatan (bocoran guard S2
// tepat pada aksi paling destruktif di modal).
function replaceEditorQuestions(questions, onReplaced) {
    var container = document.getElementById('questionsList');
    var hasCards = Boolean(container && container.querySelectorAll &&
        container.querySelectorAll('.question-editor-card').length > 0);
    var proceed = function () {
        renderQuestions(questions);
        markQuestionsConfigDirty(); // S2: hasil generate/import = belum tersimpan
        if (typeof onReplaced === 'function') onReplaced();
    };
    if ((hasCards || questionsConfigDirty) && typeof showConfirm === 'function') {
        showConfirm(
            'Ganti semua soal di editor?',
            'Soal yang sedang diedit akan hilang.',
            'Ya, Ganti', 'Batal'
        ).then(function (ok) { if (ok) proceed(); });
        return;
    }
    proceed();
}

function quickGenerateQuestions() {
    const rawQty = parseInt(document.getElementById('generateQty').value);
    const qty = isNaN(rawQty) ? 40 : rawQty;
    const type = document.getElementById('generateType').value;

    const questions = [];
    for (let i = 1; i <= qty; i++) {
        let q = { number: i, type: type, weight: 1.0 };
        if (type === 'single_choice') {
            q.options = ['A', 'B', 'C', 'D', 'E'];
            q.key = 'A';
        } else if (type === 'multiple_choice') {
            q.options = ['A', 'B', 'C', 'D', 'E'];
            q.key = ['A'];
        } else if (type === 'true_false') {
            q.key = 'TRUE';
        } else if (type === 'matching') {
            q.left_items = ['1', '2', '3'];
            q.right_items = ['A', 'B', 'C'];
            q.key = { '1': 'A', '2': 'B', '3': 'C' };
        } else if (type === 'short_answer') {
            // Kunci dibiarkan kosong agar guru mengisinya; saveQuestionsConfig
            // menandai kartu dengan kunci kosong sebelum mengirim ke server.
            q.key = '';
        }
        questions.push(q);
    }
    replaceEditorQuestions(questions);
}

function getQuestionsFromEditor() {
    const cards = document.querySelectorAll('.question-editor-card');
    const questions = [];
    
    for (let card of cards) {
        const number = parseInt(card.querySelector('.q-number').value);
        const type = card.querySelector('.q-type-select').value;
        const keyRaw = card.querySelector('.q-key-input').value.trim();
        const optionsRaw = card.querySelector('.q-options-input').value.trim();
        const statementsInput = card.querySelector('.q-statements-input');
        const statementsRaw = statementsInput ? statementsInput.value : '';
        const weight = parseFloat(card.querySelector('.q-weight-input').value) || 1.0;
        const partialCheckbox = card.querySelector('.q-partial-checkbox');
        const partialScoring = (type === 'multiple_choice' || type === 'matching' || type === 'true_false') && partialCheckbox ? partialCheckbox.checked : false;
        
        let q = { number: number, type: type, weight: weight, partial_scoring: partialScoring };
        
        if (type === 'single_choice' || type === 'multiple_choice') {
            q.options = optionsRaw.split(',').map(x => x.trim()).filter(x => x);
            if (q.options.length === 0) q.options = ['A', 'B', 'C', 'D', 'E'];
        } else if (type === 'matching') {
            const leftInput = card.querySelector('.q-left-items-input');
            const rightInput = card.querySelector('.q-right-items-input');
            if (leftInput && rightInput) {
                q.left_items = splitEditorLines(leftInput.value);
                q.right_items = splitEditorLines(rightInput.value);
                if (q.left_items.length === 0) q.left_items = ['1', '2', '3'];
                if (q.right_items.length === 0) q.right_items = ['A', 'B', 'C'];
            } else {
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
        }
        
        if (type === 'single_choice') {
            q.key = keyRaw.toUpperCase();
        } else if (type === 'multiple_choice') {
            q.key = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
        } else if (type === 'true_false') {
            const statements = statementsRaw.split('\n').map(x => x.trim()).filter(x => x);
            if (statements.length > 0) {
                q.statements = statements;
                q.key = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
            } else {
                q.key = keyRaw.toUpperCase();
            }
        } else if (type === 'matching') {
            const keyObj = {};
            const selects = card.querySelectorAll('.q-matching-select');
            if (selects.length > 0) {
                selects.forEach(select => {
                    keyObj[select.dataset.left] = select.value.toUpperCase();
                });
            } else {
                const pairs = keyRaw.split(',');
                pairs.forEach(pair => {
                    const item = pair.split(':');
                    if (item.length === 2) {
                        keyObj[item[0].trim()] = item[1].trim().toUpperCase();
                    }
                });
            }
            q.key = keyObj;
        } else if (type === 'short_answer') {
            q.key = keyRaw;
        }
        
        questions.push(q);
    }
    
    questions.sort((a, b) => a.number - b.number);
    return questions;
}

// M25: escape karakter khusus XML agar data soal yang diekspor selamat
// round-trip lewat parser import — nilai seperti "Benar & Salah" dulu
// menghasilkan XML invalid (parsererror di import, data tak bisa kembali).
function xmlEscape(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&apos;');
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
        const partialAttr = (q.type === 'multiple_choice' || q.type === 'matching' || q.type === 'true_false') ? ` partial_scoring="${xmlEscape(q.partial_scoring)}"` : '';
        xml += `    <question number="${q.number}" type="${q.type}" weight="${q.weight.toFixed(1)}"${partialAttr}>\n`;
        
        if (q.type === 'single_choice' || q.type === 'multiple_choice') {
            const choices = q.options || q.choices;
            if (choices && choices.length > 0) {
                xml += `        <choices>${xmlEscape(choices.join(', '))}</choices>\n`;
            }
        } else if (q.type === 'matching') {
            if (q.left_items && q.left_items.length > 0) {
                xml += `        <left_items>${xmlEscape(q.left_items.join(', '))}</left_items>\n`;
            }
            if (q.right_items && q.right_items.length > 0) {
                xml += `        <right_items>${xmlEscape(q.right_items.join(', '))}</right_items>\n`;
            }
        } else if (q.type === 'true_false' && q.statements && q.statements.length > 0) {
            xml += `        <statements>${xmlEscape(q.statements.join('\n'))}</statements>\n`;
        }
        
        let keyStr = '';
        if ((q.type === 'multiple_choice' || q.type === 'true_false') && Array.isArray(q.key)) {
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
        
        xml += `        <key>${xmlEscape(keyStr)}</key>\n`;
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

// Validasi pre-save: kunci kosong dan kunci di luar daftar pilihan ditangkap
// di sisi klien agar guru langsung diarahkan ke kartu yang bermasalah —
// server hanya membalas toast generik yang tidak menunjuk lokasi soalnya.
function findQuestionCardProblems() {
    const problems = [];
    document.querySelectorAll('.question-editor-card').forEach(card => {
        card.style.outline = '';
        const numInput = card.querySelector('.q-number');
        const num = numInput ? numInput.value : '?';
        const typeSel = card.querySelector('.q-type-select');
        const type = typeSel ? typeSel.value : '';
        const keyInput = card.querySelector('.q-key-input');
        const keyRaw = keyInput ? keyInput.value.trim() : '';
        const optionsInput = card.querySelector('.q-options-input');
        const optionsRaw = optionsInput ? optionsInput.value.trim() : '';
        const statementsInput = card.querySelector('.q-statements-input');
        const statementsRaw = statementsInput ? statementsInput.value : '';
        const statements = statementsRaw.split('\n').map(x => x.trim()).filter(x => x);
        let msg = '';
        if (!keyRaw) {
            msg = 'belum memiliki kunci jawaban';
        } else if (type === 'true_false') {
            const keys = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
            const invalid = keys.filter(k => k !== 'TRUE' && k !== 'FALSE');
            if (invalid.length) {
                msg = `kunci jawaban '${invalid.join(', ')}' harus TRUE atau FALSE`;
            } else if (statements.length > 0 && keys.length !== statements.length) {
                msg = `jumlah kunci jawaban (${keys.length}) harus sama dengan jumlah pernyataan (${statements.length})`;
            } else if (statements.length === 0 && keys.length !== 1) {
                msg = 'satu soal Benar/Salah tanpa daftar pernyataan hanya boleh memiliki satu kunci';
            }
        } else if ((type === 'single_choice' || type === 'multiple_choice') && optionsRaw) {
            const opts = optionsRaw.split(',').map(x => x.trim().toLowerCase()).filter(x => x);
            const keys = keyRaw.split(',').map(x => x.trim().toLowerCase()).filter(x => x);
            const missing = keys.filter(k => !opts.includes(k));
            if (missing.length) {
                msg = `kunci jawaban '${missing.join(', ')}' tidak ada di pilihan jawaban`;
            }
        }
        if (msg) problems.push({ num: num, msg: msg, card: card });
    });
    return problems;
}

function saveQuestionsConfig() {
    if (!activeExamId) return;

    // S27 double-submit guard: disable tombol selama request (pola createUser).
    var btn = document.getElementById('btnSaveQuestionsConfig');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;
    var restoreBtn = function() { btn.disabled = false; btn.innerHTML = originalHtml; };

    const questions = getQuestionsFromEditor();

    const problems = findQuestionCardProblems();
    if (problems.length > 0) {
        problems.forEach(p => { p.card.style.outline = '2px solid var(--color-danger-light)'; });
        if (problems[0].card.scrollIntoView) {
            problems[0].card.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }
        showToast(`Soal #${problems[0].num} ${problems[0].msg}${problems.length > 1 ? ` (+${problems.length - 1} soal lain)` : ''} — perbaiki sebelum menyimpan`, 'error');
        restoreBtn();
        return;
    }

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
    const startDateEl = document.getElementById('examStartDate');
    const startTimeEl = document.getElementById('examStartTime');
    const endDateEl = document.getElementById('examEndDate');
    const endTimeEl = document.getElementById('examEndTime');
    if (!startDateEl || !startDateEl.value || !startTimeEl || !startTimeEl.value) {
        const focusEl = (!startDateEl || !startDateEl.value) ? startDateEl : startTimeEl;
        if (focusEl && focusEl.focus) focusEl.focus();
        showToast('Tanggal dan jam mulai wajib diisi', 'error');
        restoreBtn();
        return;
    }
    if (!endDateEl || !endDateEl.value || !endTimeEl || !endTimeEl.value) {
        const focusEl = (!endDateEl || !endDateEl.value) ? endDateEl : endTimeEl;
        if (focusEl && focusEl.focus) focusEl.focus();
        showToast('Tanggal dan jam selesai wajib diisi', 'error');
        restoreBtn();
        return;
    }
    if (endTime <= startTime) {
        if (endDateEl.focus) endDateEl.focus();
        showToast('Jadwal selesai harus setelah jadwal mulai', 'error');
        restoreBtn();
        return;
    }
    const toleranceEl = document.getElementById('examScheduleTolerance');
    const toleranceMinutes = toleranceEl && toleranceEl.value !== '' ? parseInt(toleranceEl.value, 10) : 5;
    if (isNaN(toleranceMinutes) || toleranceMinutes < 0 || toleranceMinutes > 1440) {
        if (toleranceEl && toleranceEl.focus) toleranceEl.focus();
        showToast('Toleransi waktu harus antara 0 dan 1440 menit', 'error');
        restoreBtn();
        return;
    }
    const pengawasIds = getPengawasIdsFromEditor();
    const congratsMessage = document.getElementById('examCongratsMessage') ? document.getElementById('examCongratsMessage').value.trim() : '';

    apiFetch(`/admin/api/exams/${activeExamId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ questions: questions, security_level: securityLevel, strict_mode: strictMode, identity_fields: identityFields, panel_color: panelColor, start_time: startTime, end_time: endTime, schedule_tolerance_minutes: toleranceMinutes, pengawas_ids: pengawasIds, congrats_message: congratsMessage })
    })
        .then(r => r.json())
        .then(res => {
            if (res.success) {
                // Gate mid-exam: kunci/bobot dibekukan server — jangan tampil
                // hijau "berhasil" yang menyamarkan fakta kunci TIDAK berubah.
                if (res.questions_frozen) {
                    showToast(res.message, 'warning', 8000);
                } else {
                    showToast(res.message, 'success');
                }
                resetQuestionsConfigDirty(); // S2: sudah tersimpan — tutup tanpa konfirmasi buang
                // R31: tanpa reload penuh (pola R6 hapus-tanpa-reload) — posisi
                // scroll & pagination daftar ujian dipertahankan. Modal ditutup
                // dan kartu statistik disegarkan in-place; status baris ujian
                // tidak berubah oleh simpan konfigurasi soal.
                closeQuestionsModal();
                if (typeof refreshDashboardStats === 'function') {
                    refreshDashboardStats();
                }
            } else {
                showToast(res.message || 'Gagal menyimpan konfigurasi', 'error');
            }
        })
        .catch(() => showToast('Gagal menyimpan konfigurasi', 'error'))
        .finally(restoreBtn);
}

// ===== Change Password Modal =====

function openChangePasswordModal() {
    // R25: buka via API Modal terpusat; reset form tetap side-effect di sini.
    if (!Modal.open('changePasswordModal')) return;
    const form = document.getElementById('changePasswordForm');
    if (form) form.reset();
}

function closeChangePasswordModal() {
    Modal.close('changePasswordModal');
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

    // S27 double-submit guard (pola createUser): disable tombol submit selama
    // request; klik ganda/Enter berulang tidak mengirim POST kedua.
    var btn = e.target.querySelector('button[type="submit"]');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;
    var restoreBtn = function() { btn.disabled = false; btn.innerHTML = originalHtml; };

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
        .catch(() => showToast('Gagal mengubah password', 'error'))
        .finally(restoreBtn);
}


function renderRoleBadges(baseRoles, pkgRoles) {
    if (!Array.isArray(baseRoles)) baseRoles = [];
    if (!Array.isArray(pkgRoles)) pkgRoles = [];
    if (baseRoles.length + pkgRoles.length === 0) return '<span style="color:var(--color-text-muted);font-size:12px;">—</span>';
    var badgeStyles = {
        guru: 'background:rgba(99,102,241,0.15);color:var(--color-primary-light);border:1px solid rgba(99,102,241,0.3);',
        pengawas: 'background:rgba(168,85,247,0.15);color:#c084fc;border:1px solid rgba(168,85,247,0.3);'
    };
    // Roles granted by the ACTIVE package get an emerald tone + a small "paket"
    // tag so admins can tell them apart from base (permanent) roles — these
    // disappear when the package is switched or expires.
    var pkgStyle = 'background:rgba(var(--rgb-success),0.15);color:var(--color-success-light);border:1px solid rgba(var(--rgb-success),0.3);';
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

// ===== Kelola User: kolom sorting + pencarian live =====

// Sorting state for the user table. Keys must match the whitelist on the
// server (models/user.go userSortExprs) — unknowns there fall back to the
// default role-priority order, so the UI only ever sends known keys.
var usersSortState = { key: '', dir: 'asc' };

function toggleUsersSort(key) {
    if (!key) return;
    if (usersSortState.key === key) {
        if (usersSortState.dir === 'asc') {
            usersSortState.dir = 'desc';
        } else {
            // Klik ketiga pada kolom yang sama: kembali ke urutan default
            // (role-priority) — cara mudah membatalkan sorting.
            usersSortState.key = '';
            usersSortState.dir = 'asc';
        }
    } else {
        usersSortState.key = key;
        usersSortState.dir = 'asc';
    }
    refreshUsersSortHeaders();
    loadUsersList(1);
}

// Sinkronkan dropdown sortir mobile dengan state sorting desktop (dipanggil
// dari refreshUsersSortHeaders, jadi kedua arah selalu konsisten).
function syncUsersSortMobileSelect() {
    var sel = document.getElementById('userSortMobile');
    if (!sel) return;
    sel.value = usersSortState.key ? usersSortState.key + '_' + usersSortState.dir : '';
}

function onUsersSortMobileChange() {
    var sel = document.getElementById('userSortMobile');
    if (!sel) return;
    var v = sel.value; // '' atau 'key_dir' — semua kunci kolom satu kata
    if (!v) {
        usersSortState.key = '';
        usersSortState.dir = 'asc';
    } else {
        var parts = v.split('_');
        usersSortState.key = parts[0];
        usersSortState.dir = parts[1] === 'desc' ? 'desc' : 'asc';
    }
    refreshUsersSortHeaders();
    loadUsersList(1);
}

function refreshUsersSortHeaders() {
    var tableEl = document.getElementById('usersTableBody');
    if (!tableEl) return;
    var table = tableEl.closest('table');
    if (!table) return;
    table.querySelectorAll('th[data-sort]').forEach(function(th) {
        var k = th.getAttribute('data-sort');
        if (!th.dataset.titleBase) th.dataset.titleBase = th.title;
        th.classList.remove('sort-active', 'sort-asc', 'sort-desc');
        th.removeAttribute('aria-sort');
        if (k === usersSortState.key) {
            th.classList.add('sort-active', 'sort-' + usersSortState.dir);
            th.setAttribute('aria-sort', usersSortState.dir === 'asc' ? 'ascending' : 'descending');
            th.title = th.dataset.titleBase + ' (klik lagi: balik arah, klik ke-3: kembali ke urutan default)';
        } else {
            th.title = th.dataset.titleBase;
        }
    });
    syncUsersSortMobileSelect();
}

// Keyboard parity untuk header sortable (th memakai tabindex="0" di markup
// template): Enter/Space pada sebuah <th> memicu sorting seperti klik.
document.addEventListener('keydown', function(e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var th = e.target && e.target.closest ? e.target.closest('th.sortable') : null;
    if (!th || !th.getAttribute('data-sort')) return;
    // L51 (review_ui_halaman_web_2026-09-12.md): th.sortable milik modul lain
    // (mis. voucher-toggle-sort di settings-vouchers.js) punya handler keydown
    // sendiri — generic ini hanya melayani header users agar satu Enter tidak
    // memicu dua sort sekaligus.
    if (th.getAttribute('data-action') !== 'users-toggle-sort') return;
    e.preventDefault();
    toggleUsersSort(th.getAttribute('data-sort'));
});

// Pencarian live: debounce 300ms; panah angkut, Enter langsung jalankan.
// Tombol ✕ hanya tampil saat ada teks (dan ikut memicu pemuatan ulang).
function onUsersSearchInput(input) {
    var clearBtn = document.getElementById('userSearchClearBtn');
    if (clearBtn) clearBtn.style.display = input.value ? '' : 'none';
    clearTimeout(window.__usersSearchTimer);
    window.__usersSearchTimer = setTimeout(function() { loadUsersList(1); }, 300);
}

function clearUsersSearch() {
    var input = document.getElementById('userSearchInput');
    if (input) input.value = '';
    var clearBtn = document.getElementById('userSearchClearBtn');
    if (clearBtn) clearBtn.style.display = 'none';
    clearTimeout(window.__usersSearchTimer);
    loadUsersList(1);
}

var usersListSeq = 0;
// S78 (ronde 8): token permintaan monoton — respons permintaan lama yang
// lambat mendarat terakhir TIDAK boleh menimpa render yang lebih baru.
function loadUsersList(page) {
    const tbody = document.getElementById('usersTableBody');
    if (!tbody) return;
    if (!page) page = 1;
    var seq = ++usersListSeq;

    tbody.setAttribute('aria-busy', 'true');
    var searchVal = document.getElementById('userSearchInput')?.value?.trim() || '';
    var roleFilter = document.getElementById('userRoleFilter')?.value || '';
    // #users-per-page: pemilih 10/25/50 — default 10, diteruskan ke server
    // (server clamp mempertahankan jendela masuk akal 5..200).
    var perPage = document.getElementById('userPerPage')?.value || '10';
    var url = '/admin/api/users?page=' + page + '&per_page=' + encodeURIComponent(perPage);
    if (searchVal) url += '&search=' + encodeURIComponent(searchVal);
    if (roleFilter) url += '&role=' + encodeURIComponent(roleFilter);
    if (usersSortState.key) url += '&sort_by=' + encodeURIComponent(usersSortState.key) + '&sort_dir=' + usersSortState.dir;

    // Hapus popup yang tertinggal di body (dari fix backdrop-filter containing block)
    document.querySelectorAll('body > .user-info-popup').forEach(function(p) { p.remove(); });

    tbody.innerHTML = '<tr><td colspan="9" style="text-align:center; padding: 20px; color: var(--color-text-secondary);"><svg class="icon-svg spin" style="width:16px;height:16px;vertical-align:-3px;margin-right:8px;" aria-hidden="true"><use href="#hi-refresh"/></svg>Memuat...</td></tr>';

    apiFetch(url)
        .then(r => r.json())
        .then(res => {
            if (seq !== usersListSeq) return;
            if (res.success) {
                const pagination = res.pagination || { page: 1, total_pages: 1, total: 0 };
                tbody.innerHTML = '';

                if (!Array.isArray(res.users) || res.users.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="9" style="text-align:center; padding: 40px; color: var(--color-text-secondary);">'
                        + (searchVal ? 'Tidak ditemukan user yang cocok dengan "' + escapeHtml(searchVal) + '"' : 'Belum ada user terdaftar')
                        + '</td></tr>';
                    tbody.setAttribute('aria-busy', 'false');
                    renderUsersPagination(pagination, page);
                    return;
                }

                res.users.forEach(user => {
                    const tr = document.createElement('tr');
                    const isAdmin = user.username === 'superadmin';
                    // pending_otp accounts are activated only via the explicit
                    // "Verifikasi" action — the generic status toggle would
                    // silently bypass the email/OTP gate, so its badge is not
                    // clickable.
                    var statusToggleable = !isAdmin && user.status !== 'pending_otp';
                    var statusLabel = user.status === 'active'
                        ? 'Aktif'
                        : user.status === 'suspended'
                        ? 'Nonaktif'
                        : 'Pending';
                    var statusCls = user.status === 'active'
                        ? 'status-active'
                        : user.status === 'suspended'
                        ? 'status-suspended'
                        : 'status-inactive';
                    // Affordance: clickable badges get role="button" (pointer
                    // cursor) + a real title describing the NEXT action.
                    var statusTitle;
                    if (isAdmin) {
                        statusTitle = 'Akun Super Admin';
                    } else if (user.status === 'pending_otp') {
                        statusTitle = 'Menunggu verifikasi email/OTP — gunakan tombol Verifikasi';
                    } else if (user.status === 'active') {
                        statusTitle = 'Klik untuk menonaktifkan akun';
                    } else if (user.status === 'suspended') {
                        statusTitle = 'Klik untuk mengaktifkan kembali akun';
                    } else {
                        statusTitle = 'Klik untuk mengaktifkan akun';
                    }
                    var statusBadge = '<span class="status-badge ' + statusCls
                        + (statusToggleable ? ' status-toggle' : '')
                        + '" title="' + statusTitle + '"'
                        + (statusToggleable ? ' role="button" tabindex="0" data-user-id="' + user.id + '" data-username="' + escapeHtml(jsEscape(user.username)) + '" data-status="' + user.status + '"' : '')
                        + '>' + statusLabel + '</span>';
                    // expires_at dikirim server dalam format UTC "YYYY-MM-DD HH:MM:SS"
                    // (dikonversi ke Date dengan + 'Z'). Akun dianggap "masa aktif habis"
                    // bila expires_at terisi dan sudah lewat dari waktu sekarang.
                    const isExpired = Boolean(user.expires_at && new Date(user.expires_at.replace(' ', 'T') + 'Z').getTime() <= Date.now());
                    const expiredBadge = isExpired ? '<span class="status-badge status-expired" title="Masa aktif akun telah habis. Perpanjang masa aktif agar user dapat login kembali.">Masa aktif habis</span>' : '';
                    const expiresAt = user.expires_at ? formatDateTimeID(user.expires_at) : '—';
                    const createdAt = user.created_at ? formatDateTimeID(user.created_at) : '—';
                    const limitPdfMb = user.max_pdf_size ? (user.max_pdf_size / (1024*1024)).toFixed(1) + ' MB' : '—';
                    const limitStorageMb = user.max_storage_size ? (user.max_storage_size / (1024*1024)).toFixed(1) + ' MB' : '—';

                    // Build action buttons for non-admin users
                    var actionsHtml = '<span style="font-size:11px; color: var(--color-text-secondary);">—</span>';
                    if (!isAdmin) {
                        // Icon + label: the row actions were icon-only, which
                        // left desktop users guessing and touch users with no
                        // tooltip at all (title does not appear on tap).
                        var verifyBtn = user.status === 'pending_otp'
                            ? '<button class="btn-sm btn-row-action btn-row-action-verify" data-action="user-verify" data-user-id="' + user.id + '" data-name="' + escapeHtml(user.username) + '" title="Verifikasi manual"><svg class="icon-svg" aria-hidden="true"><use href="#hi-check"/></svg> Verifikasi</button> '
                            : '';

                        // "Nonaktifkan Paket" — SuperAdmin only, and only when the
                        // account actually runs an active package (server-flagged
                        // has_active_package, so the button never renders when
                        // there is nothing to deactivate). Deactivation burns the
                        // active package: the account falls back to the best
                        // remaining claimed voucher or reverts to the free trial.
                        var deactivateBtn = (window.__adminRole === 'superadmin' && user.has_active_package)
                            ? '<button class="btn-sm btn-row-action btn-row-action-deactivate" data-action="user-deactivate-package" data-user-id="' + user.id + '" data-name="' + escapeHtml(user.username) + '" title="Nonaktifkan paket aktif akun ini — akun kembali ke paket free atau voucher lain yang masih tersisa"><svg class="icon-svg" aria-hidden="true"><use href="#hi-exclamation"/></svg> Nonaktifkan Paket</button> '
                            : '';

                        actionsHtml = '<div class="user-row-actions">'
                            + verifyBtn
                            + deactivateBtn
                            + '<button class="btn-sm btn-row-action btn-row-action-edit" data-action="user-edit-open" data-user-id="' + user.id + '" title="Atur limit & reset password"><svg class="icon-svg" aria-hidden="true"><use href="#hi-edit"/></svg> Edit</button> '
                            + '<button class="btn-sm btn-row-action btn-row-action-danger" data-action="user-delete" data-user-id="' + user.id + '" data-name="' + escapeHtml(user.username) + '" title="Hapus user beserta semua ujiannya"><svg class="icon-svg" aria-hidden="true"><use href="#hi-trash"/></svg> Hapus</button>'
                            + '</div>';
                    }

                    tr.innerHTML = `
                        <td data-label="Username">
                            <strong class="user-info-btn" style="cursor:pointer;color: ${isAdmin ? 'var(--color-accent-light)' : 'var(--color-text)'};">
                                ${isAdmin ? '<svg class="icon-svg" style="width:16px;height:16px;vertical-align:middle;color:var(--color-warning-light);"><use href="#hi-star"/></svg> ' : ''}${escapeHtml(user.username)}
                                <svg class="icon-svg user-info-icon" aria-hidden="true" title="Klik untuk lihat detail kuota &amp; masa aktif"><use href="#hi-information"/></svg>
                            </strong>
                            ${isAdmin ? '<span style="font-size:11px; color: var(--color-text-secondary); display:block;">Super Admin</span>' : ''}
                            ${user.operator_created ? '<span title="Akun ini dibuat oleh Operator (akun sub sekolah). Paket, kuota, dan masa aktifnya dikelola melalui paket sekolah Operator — akun ini tidak dapat menukar kode voucher sendiri." style="display:inline-block;margin-top:3px;padding:1px 8px;border-radius:10px;font-size:10px;font-weight:600;background:rgba(251,146,60,0.12);color:#fb923c;border:1px solid rgba(251,146,60,0.25);cursor:help;">Dibuat oleh Operator</span>' : ''}
                            <div class="user-info-popup" style="display:none;">
                                <div class="user-info-item"><span>Ujian</span><strong>${user.exam_count ?? 0}</strong></div>
                                <div class="user-info-item"><span>Maks Total Ujian</span><strong>${user.max_exams ?? '—'}</strong></div>
                                <div class="user-info-item"><span title="Maksimal ujian yang berjalan bersamaan (sudah dimulai &amp; bisa dikerjakan siswa)">Ujian Serentak</span><strong>${user.max_concurrent_exams ?? '—'}</strong></div>
                                <div class="user-info-item"><span>Maks Upload (MB)</span><strong>${limitPdfMb}</strong></div>
                                <div class="user-info-item"><span>Maks Storage (MB)</span><strong>${limitStorageMb}</strong></div>
                                <div class="user-info-item"><span>Masa Aktif</span><strong${isExpired ? ' style="color:var(--color-danger-light);"' : ''}>${expiresAt}</strong></div>
                                <div class="user-info-item"><span>Terdaftar</span><strong>${createdAt}</strong></div>
                            </div>
                        </td>
                        <td data-label="Nama">${escapeHtml(user.name || '—')}</td>
                        <td data-label="Instansi">${window.__adminRole === 'superadmin' ? '<span class="editable-instansi" data-user-id="' + user.id + '" role="button" tabindex="0" aria-label="Ubah instansi" style="color:var(--color-primary-light);cursor:pointer;border-bottom:1px dashed rgba(165,180,252,0.3);" title="Klik untuk ubah instansi">' + escapeHtml(user.instansi || '—') + '</span>' : escapeHtml(user.instansi || '—')}</td>
                        <td data-label="Paket"><span style="text-transform:uppercase;font-size:11px;font-weight:600;color:var(--color-accent-light);">${escapeHtml(user.package || 'free')}</span></td>
                        <td data-label="Role">${isAdmin ? '<span style="display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;background:rgba(var(--rgb-warning-dim),0.15);color:var(--color-warning-light);border:1px solid rgba(var(--rgb-warning-dim),0.3);">Super Admin</span>' : renderRoleBadges(user.base_roles, user.package_roles)}</td>
                        <td data-label="Email">${escapeHtml(user.email || '—')}</td>
                        <td data-label="Status" style="text-align:center;"><span class="user-status-cell">${statusBadge}${expiredBadge}</span></td>
                        <td data-label="Terdaftar">${createdAt}</td>
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
                    // Keyboard parity: role="button" spans must react to
                    // Enter/Space exactly like a click.
                    el.addEventListener('keydown', function(e) {
                        if (e.key !== 'Enter' && e.key !== ' ') return;
                        e.preventDefault();
                        this.click();
                    });
                });
                renderUsersPagination(pagination, page);
                refreshUsersSortHeaders();
                tbody.setAttribute('aria-busy', 'false');
            } else {
                tbody.innerHTML = '<tr><td colspan="9" style="text-align:center; padding: 20px; color: var(--color-danger-light);">Gagal memuat daftar user'
                    + '<div style="margin-top:12px;"><button type="button" class="btn-sm btn-secondary" data-action="users-retry-load" data-page="' + page + '">Coba Lagi</button></div></td></tr>';
                tbody.setAttribute('aria-busy', 'false');
            }
        })
        .catch(() => {
            if (seq !== usersListSeq) return;
            var t = document.getElementById('usersTableBody');
            if (t) {
                t.innerHTML = '<tr><td colspan="9" style="text-align:center; padding: 20px; color: var(--color-danger-light);">Gagal memuat daftar user'
                    + '<div style="margin-top:12px;"><button type="button" class="btn-sm btn-secondary" data-action="users-retry-load" data-page="' + page + '">Coba Lagi</button></div></td></tr>';
                t.setAttribute('aria-busy', 'false');
            }
        });
}

function renderUsersPagination(pagination, currentPage) {
    // Remove existing pagination
    var existing = document.getElementById('usersPagination');
    if (existing) existing.remove();

    if (!pagination || pagination.total <= 0) return;

    var total = pagination.total;
    var perPage = pagination.per_page || 10;
    var totalPages = pagination.total_pages || 1;
    var start = (currentPage - 1) * perPage + 1;
    var end = Math.min(currentPage * perPage, total);

    var container = document.createElement('div');
    container.id = 'usersPagination';
    container.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:16px 0 0 0;flex-wrap:wrap;gap:12px;';

    // Rentang numerik ("1–10 dari 57 user") — tidak hanya jumlah total.
    var info = document.createElement('span');
    info.style.cssText = 'font-size:13px;color:var(--color-text-muted);';
    info.textContent = 'Menampilkan ' + start + '–' + end + ' dari ' + total + ' user';
    container.appendChild(info);

    // Kontrol satu halaman muncul saat total <= 0? Tidak — info baris di atas
    // tetap ditampilkan untuk daftar 1 halaman. Tombol page hanya bila > 1.
    if (totalPages > 1) {
        var pagesDiv = document.createElement('div');
        pagesDiv.style.cssText = 'display:flex;gap:6px;align-items:center;flex-wrap:wrap;';

        function mkBtn(label, pg, disabled, isCurrent) {
            var b = document.createElement('button');
            b.type = 'button';
            b.style.cssText = 'padding:8px 12px;border-radius:8px;font-size:13px;font-weight:600;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);color:var(--color-text-secondary);cursor:pointer;display:inline-flex;align-items:center;min-height:40px;min-width:36px;justify-content:center;font-family:inherit;transition:background 0.2s,border-color 0.2s;';
            if (disabled) {
                b.disabled = true;
                b.style.cssText += 'opacity:0.4;cursor:not-allowed;';
            }
            if (isCurrent) {
                b.className = 'pagination-current';
                // R139: halaman aktif TIDAK di-disabled - SR harus bisa membaca
                // posisi halaman via aria-current (disabled = tak fokusable).
                b.setAttribute('aria-current', 'page');
                b.setAttribute('aria-label', 'Halaman ' + pg + ', halaman saat ini');
                b.style.cssText += 'background:rgba(99,102,241,0.2);color:var(--color-primary-light);border:1px solid rgba(99,102,241,0.4);font-weight:800;';
            }
            b.textContent = label;
            b.onclick = function() { loadUsersList(pg); };
            return b;
        }

        // Navigasi: jendela halaman dengan ellipsis + lompat ke halaman
        // pertama/terakhir — sebelumnya halaman 1 hilang dari jendela saat
        // berada jauh di daftar, tanpa cara lompat cepat.
        pagesDiv.appendChild(mkBtn('◀ Sebelumnya', currentPage - 1, currentPage <= 1, false));

        var pages = [];
        if (totalPages <= 7) {
            for (var p = 1; p <= totalPages; p++) pages.push(p);
        } else {
            pages.push(1);
            var lo = Math.max(2, currentPage - 1);
            var hi = Math.min(totalPages - 1, currentPage + 1);
            if (lo > 2) pages.push('…');
            for (var q = lo; q <= hi; q++) pages.push(q);
            if (hi < totalPages - 1) pages.push('…');
            pages.push(totalPages);
        }
        pages.forEach(function(pg) {
            if (pg === '…') {
                var e = document.createElement('span');
                e.textContent = '…';
                e.setAttribute('aria-hidden', 'true');
                e.style.cssText = 'color:var(--color-text-muted);padding:0 4px;';
                pagesDiv.appendChild(e);
            } else {
                pagesDiv.appendChild(mkBtn(String(pg), pg, false, pg === currentPage));
            }
        });

        pagesDiv.appendChild(mkBtn('Berikutnya ▶', currentPage + 1, currentPage >= totalPages, false));
        container.appendChild(pagesDiv);
    }

    var tableSection = document.querySelector('#usersTableBody')?.closest('.glass-card');
    if (tableSection) tableSection.appendChild(container);
}

function localizeDates() {
    document.querySelectorAll('.td-date').forEach(el => {
        const rawDate = el.dataset.utc;
        if (rawDate) {
            el.dataset.utc = rawDate;
            el.textContent = formatDateTimeID(rawDate);
        }
    });
}

// ===== User Info Popup (Event Delegation) =====
(function() {
    var tableBody = document.getElementById('usersTableBody');
    if (!tableBody) return;
    tableBody.addEventListener('click', function(e) {
        // Status badge toggle — ikon info & username pakai popup (di bawah).
        var st = e.target.closest('.status-toggle');
        if (st) {
            toggleUserStatus(parseInt(st.getAttribute('data-user-id')), st.getAttribute('data-username'), st.getAttribute('data-status'));
            return;
        }
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
    // Keyboard: Enter/Space pada badge status yang punya role="button".
    tableBody.addEventListener('keydown', function(e) {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        var st = e.target.closest('.status-toggle');
        if (!st) return;
        e.preventDefault();
        toggleUserStatus(parseInt(st.getAttribute('data-user-id')), st.getAttribute('data-username'), st.getAttribute('data-status'));
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

function deactivatePackage(userId, username) {
    showConfirm(
        `Nonaktifkan paket akun "${username}"?`,
        'Paket aktif akan dinonaktifkan permanen. Akun kembali ke paket free (masa aktif gratis baru) atau ke voucher lain yang masih tersisa di akun. Tindakan ini tidak dapat dibatalkan dari halaman billing akun.',
        'Ya, Nonaktifkan', 'Batal'
    ).then(ok => {
        if (!ok) return;
        apiFetch(`/admin/api/users/${userId}/deactivate-package`, {
            method: 'POST'
        })
            .then(r => r.json())
            .then(res => {
                if (res.success) {
                    showToast(res.message, 'success');
                    loadUsersList(getCurrentUsersPage());
                } else {
                    showToast(res.message || 'Gagal menonaktifkan paket', 'error');
                }
            })
            .catch(() => showToast('Gagal menonaktifkan paket', 'error'));
    });
}

function toggleUserStatus(userId, username, currentStatus) {
    // Konfirmasi menyebutkan status AKTIF-SAAT-INI dan tindakan berikutnya,
    // bukan teks generik "Yakin ingin mengubah status?".
    var isActive = currentStatus === 'active';
    var title;
    var msg;
    var confirmLabel;
    if (isActive) {
        title = 'Nonaktifkan akun "' + username + '"?';
        msg = 'Akun tidak dapat login sampai kamu mengaktifkannya kembali.';
        confirmLabel = 'Ya, Nonaktifkan';
    } else {
        title = 'Aktifkan akun "' + username + '"?';
        msg = 'Akun dapat login dan menggunakan fitur kembali.';
        confirmLabel = 'Ya, Aktifkan';
    }
    showConfirm(title, msg, confirmLabel, 'Batal').then(ok => {
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
        var active = pagEl.querySelector('button.pagination-current');
        if (active) return parseInt(active.textContent) || 1;
    }
    return 1;
}

// ===== Edit User Modal =====
// S109: token generasi respons detail user — mencegah modal "Atur User"
// menampilkan data akun lain saat klik cepat bergantian (kelas race S78).
var editUserModalSeq = 0;

function openEditUserModal(userId) {
    // Fetch user data from the single-account detail endpoint — the old
    // per_page=1000 list fetch slowed down in lockstep with the account
    // count, so the modal now loads exactly one row.
    var seq = ++editUserModalSeq;
    apiFetch('/admin/api/users/' + userId)
        .then(r => r.json())
        .then(res => {
            if (seq !== editUserModalSeq) return; // S109: respons basi diabaikan
            if (!res.success || !res.user) {
                showToast('Gagal memuat data user', 'error');
                return;
            }
            var user = res.user;

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
            // Cap Maks Storage di modal Atur Limit pada sisa kapasitas disk server.
            var _est = document.getElementById('editUserStorageSize');
            if (_est && window.__storageFreeMb > 0) {
                _est.max = Math.floor(window.__storageFreeMb);
                _est.title = 'Batas total kapasitas storage (MB). 0 = tidak terbatas. Sisa disk server: ' + fmtStorageSize(window.__storageFreeMb) + '.';
            }
            // Cap Maks Upload PDF pada min(sisa disk, 100 MB) — 100 MB adalah
            // batas upload global (maxFileSize, exams.go).
            var _ept = document.getElementById('editUserPdfSize');
            if (_ept && window.__storageFreeMb > 0) {
                _ept.max = Math.min(Math.floor(window.__storageFreeMb), 100);
                _ept.title = 'Limit ukuran file PDF (MB). Tidak boleh melebihi sisa disk server (maks 100 MB global). Sisa disk server: ' + fmtStorageSize(window.__storageFreeMb) + '.';
            }
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
            // R119: pemasangan listener change TIDAK lagi di sini — dipindah
            // ke createEditUserModal() agar terpasang sekali saat modal
            // dibuat (modal di-cache; .then berjalan tiap buka = menumpuk).

            // R25: buka via API Modal terpusat.
            Modal.open(editModal);
        })
        .catch(function () {
            // S109+R133: gagal jaringan tak boleh unhandled rejection — dan
            // toast basi dari permintaan lama tak boleh muncul setelah modal
            // baru sudah terisi (guard seq simetris dengan cabang then).
            if (seq !== editUserModalSeq) return;
            showToast('Gagal memuat data user', 'error');
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
    // R25: delegasi ke API Modal terpusat.
    Modal.close('editUserModal');
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
                    style="width:100%;padding:10px 12px;background:rgba(255,255,255,0.04);border:1px solid var(--color-glass-border);border-radius:8px;color:var(--color-text);font-size:14px;outline:none;transition:border-color 0.2s;box-sizing:border-box;">
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

    // R100: highlight fokus via listener terprogram (pengganti atribut
    // onfocus/onblur inline) — warna memakai token, bukan literal rgba.
    input.addEventListener('focus', function() { input.style.borderColor = 'var(--color-primary-light)'; });
    input.addEventListener('blur', function() { input.style.borderColor = ''; });

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
        roles: []
    };
    // Sub-account package policy: operator tidak boleh memilih/mengubah paket
    // langganan akun di bawahnya — field package tidak dikirim (server juga
    // mengabaikannya). Hanya Super Admin yang dapat menetapkan paket.
    if (!__adminHasRole('operator')) {
        data.package = document.getElementById('editUserPackage').value;
    }
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

    // Pre-check Maks Storage terhadap sisa kapasitas disk server (server juga memvalidasi).
    if (window.__storageFreeMb > 0 && data.max_storage_size_mb > window.__storageFreeMb) {
        showToast('Maks Storage melebihi sisa kapasitas disk server (' + fmtStorageSize(window.__storageFreeMb) + ')', 'error');
        return;
    }
    // Pre-check Maks Upload PDF (server juga memvalidasi). 100 MB = batas upload global (maxFileSize, exams.go).
    if (window.__storageFreeMb > 0 && data.max_pdf_size_mb > window.__storageFreeMb) {
        showToast('Maks Upload PDF melebihi sisa kapasitas disk server (' + fmtStorageSize(window.__storageFreeMb) + ')', 'error');
        return;
    }
    if (data.max_pdf_size_mb > 100) {
        showToast('Maks Upload PDF melebihi batas upload global (100 MB)', 'error');
        return;
    }

    var btn = e.target.querySelector('button[type="submit"]');
    // R134: guard persis kontrak S27 (pola submitEditToken/saveQuestionsConfig).
    if (!btn || btn.disabled) return;
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
                <h3><svg class="icon-svg" style="vertical-align:middle;margin-top:-2px;"><use href="#hi-users"/></svg> Atur User: <span id="editUserUsername" style="color:var(--color-primary-light);"></span></h3>
                <button class="modal-close" data-action="modal-dismiss" data-modal-close="closeEditUserModal" aria-label="Tutup"><svg class="icon-svg" style="width:18px;height:18px;"><use href="#hi-x"/></svg></button>
            </div>
            <div class="modal-body">
                <form id="editUserForm">
                    <input type="hidden" id="editUserId">
                    <div class="form-group" style="margin-bottom:8px;">
                        <label for="editUserName">Nama Lengkap</label>
                        <input type="text" id="editUserName" placeholder="Contoh: Budi Sudarsono" style="width:100%;">
                    </div>
                    <div class="form-group" style="margin-bottom:8px;" id="editUserPackageGroup">
                        <label for="editUserPackage">Paket Langganan (Preset)</label>
                        <select id="editUserPackage" style="width:100%;padding:8px 12px;background:rgba(255,255,255,0.05);border:1px solid var(--color-glass-border);border-radius:10px;color:var(--color-text);outline:none;font-size:13px;cursor:pointer;">
                            <option value="free">Free / Trial</option>
                            <option value="guru">Paket Guru</option>
                            <option value="individu">Paket Individu</option>
                            <option value="sekolah_kecil">Paket Sekolah Kecil</option>
                            <option value="sekolah_menengah">Paket Sekolah Menengah</option>
                            <option value="sekolah_besar">Paket Sekolah Besar</option>
                            <option value="sekolah_unggulan">Paket Sekolah Unggulan</option>
                        </select>
                    </div>
                    <div id="editUserQuotaGrid" class="edit-user-quota-grid" style="display:grid;grid-template-columns:1fr 1fr;gap:12px;">
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
                        <label for="editUserEmail">Email <span style="font-size:11px;opacity:0.7;">(opsional)</span></label>
                        <input type="email" id="editUserEmail" placeholder="Contoh: guru@gmail.com" style="width:100%;">
                    </div>
                    <div class="edit-user-row-2" style="display:grid;grid-template-columns:1fr 1fr;gap:12px;">
                        <div class="form-group" style="margin-bottom:8px;">
                            <label for="editUserInstansi">Instansi <span style="font-size:11px;opacity:0.7;">(wajib)</span></label>
                            <input type="text" id="editUserInstansi" required placeholder="Contoh: SMA Negeri 1 Jakarta" style="width:100%;">
                        </div>
                        <div class="form-group" style="margin-bottom:8px;">
                            <label>Role <span class="multirole-hint">multirole</span></label>
                            <div class="role-chips">
                                <label class="role-chip">
                                    <input type="checkbox" id="editRoleGuru" value="guru"> <span title="Guru">Guru</span>
                                </label>
                                <label class="role-chip">
                                    <input type="checkbox" id="editRolePengawas" value="pengawas"> <span title="Pengawas">Pengawas</span>
                                </label>
                                <label class="role-chip" id="editRoleOperatorGroup">
                                    <input type="checkbox" id="editRoleOperator" value="operator"> <span title="Operator">Operator</span>
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
                        <div class="edit-user-expiry-row" style="display:flex;gap:8px;">
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

    // S51: preset paket di-wire programatik (pengganti atribut onchange inline).
    var pkgSelect = document.getElementById('editUserPackage');
    if (pkgSelect) pkgSelect.addEventListener('change', function () { applyPackagePreset('edit'); });

    // R100: submit form Atur User via listener terprogram (pengganti atribut
    // onsubmit inline) — satu jalur wiring, selaras kontrak delegasi.
    var editUserForm = document.getElementById('editUserForm');
    if (editUserForm) editUserForm.addEventListener('submit', submitEditUser);

    // R119: listener change role dipasang SEKALI di sini (saat modal dibuat),
    // bukan di .then openEditUserModal — modal di-cache sehingga pemasangan
    // berulang menumpuk N listener pada elemen yang sama.
    var eguru = document.getElementById('editRoleGuru');
    var epengawas = document.getElementById('editRolePengawas');
    if (eguru && epengawas) {
        eguru.addEventListener('change', syncEditLimitFields);
        epengawas.addEventListener('change', syncEditLimitFields);
    }

    // Hide operator checkbox in edit modal if current user is operator
    if (__adminHasRole('operator')) {
        var opGroup = document.getElementById('editRoleOperatorGroup');
        if (opGroup) opGroup.style.display = 'none';
        // Sembunyikan field instansi untuk operator
        var instansiGroup = document.getElementById('editUserInstansi').closest('.form-group') || document.getElementById('editUserInstansi').parentNode;
        if (instansiGroup) instansiGroup.style.display = 'none';
        // Sub-account package policy: sembunyikan pemilih paket langganan —
        // akun sub tidak memiliki paket sendiri (kuota & masa aktif mengikuti
        // paket sekolah Operator/Super Admin).
        var pkgGroup = document.getElementById('editUserPackageGroup');
        if (pkgGroup) pkgGroup.style.display = 'none';
        // Kuota per-akun akun sub juga tidak bisa diubah operator (server
        // mengabaikan field ini untuk caller operator) — sembunyikan agar tidak
        // ada kontrol yang hasilnya senyap diabaikan.
        var quotaGrid = document.getElementById('editUserQuotaGrid');
        if (quotaGrid) quotaGrid.style.display = 'none';
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
    // R25: buka via API Modal terpusat.
    Modal.open('editTokenModal');
    setTimeout(() => document.getElementById('editTokenInput').focus(), 100);
}

function closeEditTokenModal() {
    // R25: delegasi ke API Modal terpusat; reset form tetap side-effect di sini.
    Modal.close('editTokenModal');
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

    // S27 double-submit guard (pola createUser).
    var btn = e.target.querySelector('button[type="submit"]');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;
    var restoreBtn = function() { btn.disabled = false; btn.innerHTML = originalHtml; };

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
                        // R28-lanjutan: tombol kini ber-aksi via delegasi
                        // data-action="token-edit-open" — cukup perbarui
                        // data-token, TANPA memasang ulang onclick lama.
                        editBtn.setAttribute('data-token', res.token);
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
        .catch(() => showToast('Koneksi gagal', 'error'))
        .finally(restoreBtn);
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
                
                // Terima <options> maupun <choices> (nama field legacy).
                const parseCSVNode = (tag) => {
                    const n = node.getElementsByTagName(tag)[0];
                    if (!n) return null;
                    const list = n.textContent.split(',').map(x => x.trim()).filter(x => x);
                    return list.length ? list : null;
                };
                // Pernyataan true/false dipisah baris agar teks yang
                // mengandung koma tetap utuh.
                const parseLinesNode = (tag) => {
                    const n = node.getElementsByTagName(tag)[0];
                    if (!n) return null;
                    const list = n.textContent.split(/\r?\n/).map(x => x.trim()).filter(x => x);
                    return list.length ? list : null;
                };
                let left_items = null;
                let right_items = null;
                let qOpts = null;
                let statements = null;
                if (type === 'single_choice' || type === 'multiple_choice') {
                    qOpts = parseCSVNode('options') || parseCSVNode('choices');
                } else if (type === 'matching') {
                    left_items = parseCSVNode('left_items');
                    right_items = parseCSVNode('right_items');
                } else if (type === 'true_false') {
                    statements = parseLinesNode('statements');
                }
                
                let key = '';
                const keyNode = node.getElementsByTagName("key")[0];
                if (keyNode) {
                    const keyRaw = keyNode.textContent.trim();
                    if (type === 'multiple_choice') {
                        key = keyRaw.split(',').map(x => x.trim().toUpperCase()).filter(x => x);
                    } else if (type === 'true_false' && statements && statements.length > 0) {
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
                
                const qData = {
                    number: number,
                    type: type,
                    weight: weight,
                    partial_scoring: partialScoring,
                    key: key
                };
                if (qOpts) qData.options = qOpts;
                if (left_items) qData.left_items = left_items;
                if (right_items) qData.right_items = right_items;
                if (statements) qData.statements = statements;
                questions.push(qData);
            }
            
            // Sort by number to ensure sequential ordering
            questions.sort((a, b) => a.number - b.number);

            // T15: re-render lewat guard konfirmasi; toast sukses baru setelah
            // soal benar-benar dirender (batal = tidak ada toast menyesatkan).
            replaceEditorQuestions(questions, function () {
                showToast(`Berhasil mengimpor ${questions.length} soal dari XML!`, "success");
            });
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
- partial_scoring: Nilai parsial untuk tipe multiple_choice, matching, atau true_false multi-pernyataan. Bernilai "true" jika siswa mendapat poin proporsional atas jawaban yang sebagian benar, atau "false" jika harus benar seluruhnya.

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
- SATU nomor boleh memuat BEBERAPA pernyataan. Tulis tag <statements> berisi daftar pernyataan, SATU PERNYATAAN PER BARIS (newline), agar teks yang mengandung koma tetap utuh.
- Tag <key> berisi daftar nilai kapital TRUE atau FALSE dipisahkan koma, dengan URUTAN dan JUMLAH yang sama dengan pernyataan.
- Tambahkan atribut partial_scoring="true" bila setiap pernyataan benar bernilai proporsional. Tanpa itu, semua pernyataan harus benar untuk mendapat nilai penuh.
Contoh satu pernyataan (format lama tetap didukung):
<question number="3" type="true_false" weight="1.0">
    <key>TRUE</key>
</question>
Contoh beberapa pernyataan:
<question number="3" type="true_false" weight="2.0" partial_scoring="true">
    <statements>Ibu kota Indonesia adalah Jakarta.
2 + 2 = 5
Air membeku pada suhu 0 derajat Celsius.</statements>
    <key>TRUE, FALSE, TRUE</key>
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
    // S29: guard+fallback lewat copyCode.
    copyCode(AI_PROMPT_CONTENT);
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
                        btn.style.background = 'rgba(var(--rgb-success), 0.15)';
                        btn.style.borderColor = 'rgba(var(--rgb-success), 0.3)';
                        btn.style.color = 'var(--color-success-light)';
                        btn.innerHTML = '<svg class="icon-svg" style="width:16px;height:16px;vertical-align:middle;margin-top:-2px;" aria-hidden="true"><use href="#hi-eye"/></svg> Hal. Siswa Aktif';
                    } else {
                        btn.style.background = 'rgba(var(--rgb-danger), 0.15)';
                        btn.style.borderColor = 'rgba(var(--rgb-danger), 0.3)';
                        btn.style.color = 'var(--color-danger-light)';
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
                        btn.style.background = 'rgba(var(--rgb-warning-dim), 0.15)';
                        btn.style.borderColor = 'rgba(var(--rgb-warning-dim), 0.3)';
                        btn.style.color = 'var(--color-warning-light)';
                        btn.innerHTML = '<svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-lock-open"/></svg> Kunci Terlihat';
                    } else {
                        btn.style.background = 'rgba(var(--rgb-gray), 0.15)';
                        btn.style.borderColor = 'rgba(var(--rgb-gray), 0.3)';
                        btn.style.color = 'var(--color-text-muted)';
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
    
    // R25: buka via API Modal terpusat.
    Modal.open(modal);
}

function closeEditExamModal() {
    // R25: delegasi ke API Modal terpusat.
    Modal.close('editExamModal');
}

// ===== Delegate Exam (Operator) =====
// S102 (kelas race S78): modal aksi tulis — konteks data wajib milik ujian
// TERAKHIR yang dibuka, bukan respons basi permintaan sebelumnya.
var delegateModalSeq = 0;

function openDelegateExamModal(examId) {
    const seq = ++delegateModalSeq;
    const modal = document.getElementById('delegateExamModal');
    if (!modal) return;
    document.getElementById('delegateExamId').value = examId;
    // R25: buka via API Modal terpusat.
    Modal.open(modal);

    const guruSelect = document.getElementById('delegateOwnerSelect');
    guruSelect.innerHTML = '<option value="">-- Memuat data... --</option>';
    guruSelect.disabled = true;

    document.getElementById('delegateCurrentOwner').textContent = '';
    document.getElementById('delegatePengawasList').innerHTML = '<div style="color:var(--color-text-muted);font-size:0.82rem;padding:8px 0;">Memuat data pengawas...</div>';

    apiFetch('/admin/api/exams/' + examId + '/delegate-data')
        .then(function(r) { return r.json(); })
        .then(function(res) {
            if (seq !== delegateModalSeq) return; // S102: respons basi diabaikan
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
                        lbl.style.cssText = 'font-size:0.8rem;color:var(--color-text-placeholder);margin-bottom:6px;';
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
            if (seq !== delegateModalSeq) return; // S102: kegagalan basi tak menimpa modal aktif
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
        label.innerHTML = '<input type="checkbox" class="delegate-pengawas-checkbox" value="' + escapeHtml(p.id) + '"' + (isChecked ? ' checked' : '') + '> '
            + escapeHtml(p.username)
            + ' <span style="font-size:0.75rem;color:var(--color-text-muted);">(' + escapeHtml(p.instansi || '') + ')</span>';
        container.appendChild(label);
    });
}

function closeDelegateExamModal() {
    // R25: delegasi ke API Modal terpusat.
    Modal.close('delegateExamModal');
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

    // apiFetch menyuntik X-CSRF-Token otomatis untuk method POST.
    apiFetch('/admin/api/exams/' + examId + '/delegate', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
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
        const maxMB = parseFloat(input.getAttribute('data-max-mb')) || 0;
        textEl.textContent = `${file.name} (${sizeMB} MB)`;
        if (maxMB > 0 && file.size > maxMB * 1048576) {
            textEl.textContent += ` — melebihi batas ${maxMB} MB`;
            display.style.borderColor = 'var(--color-warning)';
        } else {
            display.style.borderColor = 'var(--color-success)';
        }
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
        const maxEditMB = parseFloat(fileInput.getAttribute('data-max-mb')) || 0;
        if (maxEditMB > 0 && fileInput.files[0].size > maxEditMB * 1048576) {
            showToast('Ukuran file melebihi batas ' + maxEditMB + ' MB', 'error');
            return;
        }
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
                showApiErrorToast(res, 'Gagal menyimpan perubahan');
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
        if (d !== dropdown) {
            d.classList.remove('show');
            // R144: reset state SR tombol pemilik menu yang ditutup paksa.
            const w = d.__btnWrap;
            const b = w ? w.querySelector('.btn-more') : null;
            if (b) b.setAttribute('aria-expanded', 'false');
        }
    });
    
    dropdown.classList.remove('drop-up', 'drop-down', 'align-right');
    // R138: sinkron state popup ke tombol pemicu (SR tahu menu terbuka/tutup).
    const triggerBtn = document.querySelector('.btn-more[data-exam-id="' + examId + '"]');
    dropdown.classList.toggle('show');
    if (triggerBtn) triggerBtn.setAttribute('aria-expanded', dropdown.classList.contains('show') ? 'true' : 'false');
    
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
            // R144: klik-luar juga wajib me-reset aria-expanded pemilik menu.
            const ownerBtn = el.__btnWrap ? el.__btnWrap.querySelector('.btn-more') : null;
            if (ownerBtn) ownerBtn.setAttribute('aria-expanded', 'false');
        });
    }
});

// Close dropdowns with Escape key
document.addEventListener('keydown', function(event) {
    if (event.key === 'Escape') {
        document.querySelectorAll('.exam-action-dropdown-content.show').forEach(el => {
            el.classList.remove('show');
            // R138: pulihkan fokus ke tombol pemicu agar navigasi keyboard
            // tidak tercerai setelah menu (yang re-parent ke body) ditutup.
            const ownerBtn = el.__btnWrap ? el.__btnWrap.querySelector('.btn-more') : null;
            if (ownerBtn) {
                ownerBtn.setAttribute('aria-expanded', 'false');
                ownerBtn.focus();
            }
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

    // S27 double-submit guard: tombol toolbar bulk-delete dinonaktifkan selama
    // request; restore di finally agar error jaringan pun memulihkannya.
    const btn = document.getElementById('bulkDeleteBtn');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;

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
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalHtml;
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
    // S40: fetch via apiFetch + unduhan blob — bukan window.location.href
    // langsung (dataset besar + jaringan lambat membuat tombol terasa mati,
    // dan error server merender JSON mentah menggantikan halaman).
    const examIdEl = document.getElementById('filterExam');
    const examId = examIdEl ? examIdEl.value : '';
    const tzOffset = new Date().getTimezoneOffset();
    let url = '/admin/api/submissions/export?tz_offset=' + tzOffset;
    if (examId) {
        url += '&exam_id=' + examId;
    }

    // Tombol milik submissions.html (agen lain) — dicari defensif by id,
    // fallback ke data-action, agar halaman tetap berfungsi tanpa tombol.
    var btn = document.getElementById('exportBtn')
        || document.querySelector('[data-action="export-submissions"]');
    if (btn && btn.getAttribute('data-exporting') === '1') return; // S40: guard dobel-klik
    var originalHtml = btn ? btn.innerHTML : '';
    if (btn) {
        btn.setAttribute('data-exporting', '1');
        btn.disabled = true;
        btn.innerHTML = 'Mengekspor...';
    }

    apiFetch(url)
        .then(function (resp) {
            if (!resp.ok) {
                // R47: body error bisa non-JSON (halaman HTML proxy 502 dst.)
                // — resp.json() reject SyntaxError yang tadinya tampil mentah
                // di toast. Fallback {} → pesan generik di bawah.
                return resp.json().catch(function () { return {}; }).then(function (err) {
                    throw new Error((err && err.message) || 'Gagal mengekspor data');
                });
            }
            return resp.blob().then(function (blob) {
                // Nama file dari Content-Disposition server, fallback generik.
                var filename = 'hasil-ujian.xlsx';
                var cd = (resp.headers && typeof resp.headers.get === 'function')
                    ? resp.headers.get('Content-Disposition') : null;
                var m = cd && cd.match(/filename\*?=(?:UTF-8'')?"?([^";]+)"?/i);
                if (m) {
                    try { filename = decodeURIComponent(m[1]); } catch (e) { /* pakai fallback */ }
                }
                var objUrl = URL.createObjectURL(blob);
                var a = document.createElement('a');
                a.href = objUrl;
                a.download = filename;
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                URL.revokeObjectURL(objUrl);
            });
        })
        .catch(function (err) {
            showToast((err && err.message) || 'Gagal mengekspor data', 'error');
        })
        .then(function () {
            // Pulihkan tombol di jalur sukses maupun gagal.
            if (btn) {
                btn.removeAttribute('data-exporting');
                btn.disabled = false;
                btn.innerHTML = originalHtml;
            }
        });
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

// ---- Per-section SaaS saves (Pengaturan Umum: 8 kartu terpisah) ----
// Each card saves ONLY its own fields — the server handler is a partial
// update (only fields present in the JSON body are written), so saving one
// section never touches the others.

// saveSaasSection posts a partial payload and refreshes the form values from
// the server. btnId is the card's save button (loading state + toast).
// Batch 10 (S47): cardId adalah identitas kartu Pengaturan Umum milik section
// ini — di cabang SUKSES saja clearSaasCardDirtyByCardId(cardId) membersihkan
// indikator dirty. Kontrak lama (observer toast SAAS_PENDING_SAVE di
// settings-general.js) dihapus karena bisa membersihkan kartu yang salah.
function saveSaasSection(payload, btnId, successMsg, cardId) {
    const btn = document.getElementById(btnId);
    const originalHTML = btn ? btn.innerHTML : '';
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Menyimpan...';
    }
    apiFetch('/admin/api/saas-settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    })
    .then(r => r.json())
    .then(res => {
        if (res.success) {
            if (cardId && typeof clearSaasCardDirtyByCardId === 'function') {
                // Fungsi milik settings-general.js (lazy-loaded) — guard typeof
                // agar halaman tanpa modul itu tetap aman.
                clearSaasCardDirtyByCardId(cardId);
            }
            showToast(res.message || successMsg, 'success');
            loadSaasSettings();
        } else {
            showToast(res.message || 'Gagal menyimpan', 'error');
        }
    })
    .catch(() => showToast('Gagal menyimpan setelan', 'error'))
    .finally(() => {
        if (btn) {
            btn.disabled = false;
            btn.innerHTML = originalHTML;
        }
    });
}

function saveSmtpSettings() {
    const form = document.getElementById('saas-card-smtp');
    const inputs = form.querySelectorAll('input[required], textarea[required]');
    for (const el of inputs) {
        if (!el.value.trim()) {
            el.reportValidity();
            showToast('Lengkapi atau perbaiki kolom setelan yang ditandai', 'error');
            return;
        }
    }
    saveSaasSection({
        email_verification_enabled: document.getElementById('emailEnabledInput').checked,
        email_domain_whitelist: (document.getElementById('emailDomainWhitelistInput') || {}).value || '',
        smtp_host: document.getElementById('smtpHostInput').value.trim(),
        smtp_port: document.getElementById('smtpPortInput').value.trim(),
        smtp_user: document.getElementById('smtpUserInput').value.trim(),
        smtp_password: document.getElementById('smtpPasswordInput').value.trim(),
        smtp_sender_name: document.getElementById('smtpSenderNameInput').value.trim()
    }, 'saveSmtpSettingsBtn', 'Setelan SMTP disimpan', 'saas-card-smtp');
}

function saveTurnstileSettings() {
    saveSaasSection({
        turnstile_enabled: !!(document.getElementById('turnstileEnabledInput') || {}).checked,
        turnstile_site_key: (document.getElementById('turnstileSiteKeyInput') || {}).value || '',
        turnstile_secret_key: (document.getElementById('turnstileSecretKeyInput') || {}).value || '',
        max_accounts_per_ip: parseInt((document.getElementById('maxAccountsPerIpInput') || {}).value) || 0,
        max_approvals_per_exam: parseInt((document.getElementById('maxApprovalsPerExamInput') || {}).value) || 0
    }, 'saveTurnstileSettingsBtn', 'Setelan Turnstile disimpan', 'saas-card-turnstile');
}

function saveCleanupSettings() {
    // Numeric tuning fields: 0 is a MEANINGFUL value ("purge immediately") for
    // grace/TTL, so a bare || fallback must not coerce it away — empty/NaN
    // falls back to the default, a typed 0 is preserved as 0.
    const cleanupNum = function(id, fallback) {
        const el = document.getElementById(id);
        if (!el || el.value === '') return fallback;
        const n = parseInt(el.value, 10);
        return isNaN(n) ? fallback : n;
    };
    saveSaasSection({
        approval_cleanup_interval_minutes: cleanupNum('approvalCleanupIntervalMinutesInput', 15),
        approval_cleanup_ended_grace_hours: cleanupNum('approvalCleanupEndedGraceHoursInput', 1),
        approval_cleanup_inactive_ttl_hours: cleanupNum('approvalCleanupInactiveTTLHoursInput', 24)
    }, 'saveCleanupSettingsBtn', 'Setelan pembersihan disimpan', 'saas-card-cleanup');
}

function saveDefaultPkgSettings() {
    const form = document.getElementById('saas-card-default-pkg');
    const inputs = form.querySelectorAll('input[required]');
    for (const el of inputs) {
        if (!el.value.trim()) {
            el.reportValidity();
            showToast('Lengkapi atau perbaiki kolom setelan yang ditandai', 'error');
            return;
        }
    }
    saveSaasSection({
        default_max_exams: parseInt(document.getElementById('defaultExamsInput').value),
        default_max_concurrent_exams: parseInt(document.getElementById('defaultConcurrentInput').value),
        default_max_pdf_size_mb: parseFloat(document.getElementById('defaultPdfInput').value),
        default_max_storage_size_mb: parseFloat(document.getElementById('defaultStorageInput').value),
        default_active_days: parseInt(document.getElementById('defaultActiveDaysInput').value)
    }, 'saveDefaultPkgSettingsBtn', 'Default paket disimpan', 'saas-card-default-pkg');
}

function saveVersionsSettings() {
    const form = document.getElementById('saas-card-versions');
    const inputs = form.querySelectorAll('input[required]');
    for (const el of inputs) {
        if (!el.value.trim()) {
            el.reportValidity();
            showToast('Lengkapi atau perbaiki kolom setelan yang ditandai', 'error');
            return;
        }
    }
    saveSaasSection({
        android_version: document.getElementById('androidVersionInput').value.trim(),
        webapp_version: document.getElementById('webappVersionInput').value.trim()
    }, 'saveVersionsSettingsBtn', 'Versi aplikasi disimpan', 'saas-card-versions');
}

function saveFooterSettings() {
    saveSaasSection({
        footer_text: document.getElementById('footerTextInput').value.trim(),
        footer_tagline: document.getElementById('footerTaglineInput').value.trim()
    }, 'saveFooterSettingsBtn', 'Footer disimpan', 'saas-card-footer');
}

function saveSeoSettings() {
    saveSaasSection({
        seo_title: document.getElementById('seoTitleInput').value.trim(),
        seo_description: document.getElementById('seoDescriptionInput').value.trim(),
        seo_keywords: document.getElementById('seoKeywordsInput').value.trim(),
        seo_index: document.getElementById('seoIndexInput').checked
    }, 'saveSeoSettingsBtn', 'Setelan SEO disimpan', 'saas-card-seo');
}

function saveMonetizationSettings() {
    saveSaasSection({
        voucher_redeem_enabled: !!(document.getElementById('voucherRedeemEnabledInput') || {}).checked
    }, 'saveMonetizationSettingsBtn', 'Kontrol monetisasi disimpan', 'saas-card-monetization');
}

// Format ukuran storage untuk hint sisa disk (MB → MB/GB).
function fmtStorageSize(mb) {
    if (mb === undefined || mb === null || isNaN(mb)) return '—';
    if (mb >= 1024) return (mb / 1024).toFixed(2) + ' GB';
    return mb.toFixed(0) + ' MB';
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
    // Double-submit guard: disable tombol selama request berjalan agar klik
    // ganda (atau Enter berulang) tidak membuat akun duplikat.
    var btn = e.target.querySelector('button[type="submit"]');
    if (!btn) return;
    if (btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;
    var restoreBtn = function() { if (!btn) return; btn.disabled = false; btn.innerHTML = originalHtml; };

    const username = document.getElementById('usernameInput').value.trim();
    const name = document.getElementById('nameInput')?.value?.trim() || '';
    const password = document.getElementById('passwordInput').value;
    const emailEl = document.getElementById('emailInput');
    const email = emailEl ? emailEl.value.trim() : '';
    // Instansi input hanya dirender untuk Super Admin (operator selalu masuk
    // instansi sekolahnya — server mengabaikan nilai ini), jadi guard null.
    const instansiEl = document.getElementById('instansiInput');
    const instansi = instansiEl ? (instansiEl.value.trim() || 'personal') : 'personal';
    var roles = [];
    if (document.getElementById('roleGuru').checked) roles.push('guru');
    if (document.getElementById('rolePengawas').checked) roles.push('pengawas');
    var roleOpEl = document.getElementById('roleOperator');
    if (roleOpEl && roleOpEl.checked) roles.push('operator');
    const max_exams = parseInt(document.getElementById('limitInput').value);
    const max_concurrent_exams = parseInt(document.getElementById('concurrentInput').value);
    const max_pdf_size_mb = parseFloat(document.getElementById('pdfSizeInput').value);
    const max_storage_size_mb = parseFloat(document.getElementById('storageSizeInput').value);
    if (!username || !password) { restoreBtn(); showToast('Username dan password wajib diisi','error'); return; }
    if (roles.length === 0) { restoreBtn(); showToast('Pilih minimal 1 role','error'); return; }
    // Pre-check Maks Storage terhadap sisa kapasitas disk server (server juga memvalidasi).
    if (window.__storageFreeMb > 0 && max_storage_size_mb > window.__storageFreeMb) {
        restoreBtn(); showToast('Maks Storage melebihi sisa kapasitas disk server (' + fmtStorageSize(window.__storageFreeMb) + ')', 'error');
        return;
    }
    // Pre-check Maks Upload PDF (server juga memvalidasi). 100 MB = batas upload global (maxFileSize, exams.go).
    if (window.__storageFreeMb > 0 && max_pdf_size_mb > window.__storageFreeMb) {
        restoreBtn(); showToast('Maks Upload PDF melebihi sisa kapasitas disk server (' + fmtStorageSize(window.__storageFreeMb) + ')', 'error');
        return;
    }
    if (max_pdf_size_mb > 100) {
        restoreBtn(); showToast('Maks Upload PDF melebihi batas upload global (100 MB)', 'error');
        return;
    }
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
    // Sub-account package policy: untuk Operator pemilih paket disembunyikan
    // (akun sub tidak punya paket sendiri) — kirim 'free', server memaksa nilai
    // yang sama untuk akun yang dibuat operator.
    var pkgEl = document.getElementById('packageSelect');
    const package = pkgEl ? pkgEl.value : 'free';
    apiFetch('/admin/api/users', {
        method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ username, name, password, email, instansi, roles, max_exams, max_concurrent_exams, max_pdf_size_mb, max_storage_size_mb, expires_at, package })
    }).then(r=>r.json()).then(res => {
        restoreBtn();
        if (res.success) {
            showToast(res.message,'success');
            document.getElementById('newUserForm').reset();
            loadUsersList(1);
        }
        else showToast(res.message||'Gagal','error');
    }).catch(()=>{ restoreBtn(); showToast('Gagal menghubungi server','error'); });
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
                // 0 = tidak terbatas, jadi hanya pakai fallback saat field belum ada
                var _dsmb = s.default_max_storage_size_mb;
                var _dsInp = document.getElementById('defaultStorageInput');
                _dsInp.value = (_dsmb === undefined || _dsmb === null) ? 50 : _dsmb;
                // Cap input pada sisa kapasitas disk server (dikirim GET sebagai
                // storage_free_mb; 0 berarti tidak dapat ditentukan). Sisa disk
                // ditampilkan SEKALI di badge header section (diskFreeBadge) —
                // hint per-field hanya memuat aturan inputnya masing-masing.
                var _freeMb = typeof s.storage_free_mb === 'number' ? s.storage_free_mb : 0;
                if (_freeMb > 0) {
                    _dsInp.max = Math.floor(_freeMb);
                } else {
                    _dsInp.removeAttribute('max');
                }
                // Cap PDF upload default pada min(sisa disk, 100 MB) — 100 MB
                // adalah batas upload global (maxFileSize, exams.go).
                var _dpInp = document.getElementById('defaultPdfInput');
                if (_freeMb > 0) {
                    _dpInp.max = Math.min(Math.floor(_freeMb), 100);
                } else {
                    _dpInp.removeAttribute('max');
                }
                // Badge sisa disk (idempotent: nilai API sama dengan
                // window.__storageFreeMb yang sudah dirender server-side).
                var _dbText = document.getElementById('diskFreeBadgeText');
                if (_dbText) {
                    _dbText.textContent = _freeMb > 0
                        ? 'Sisa disk server: ' + fmtStorageSize(_freeMb)
                        : 'Sisa disk server tidak dapat ditentukan';
                }
                document.getElementById('defaultActiveDaysInput').value = s.default_active_days || 14;
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

                // Per-exam approved-device cap for auto-approve
                var _mape = document.getElementById('maxApprovalsPerExamInput');
                if (_mape) _mape.value = (typeof s.max_approvals_per_exam === 'number') ? s.max_approvals_per_exam : 500;

                // Approval-cleanup job tuning (interval/grace/TTL)
                var _aci = document.getElementById('approvalCleanupIntervalMinutesInput');
                if (_aci) _aci.value = (typeof s.approval_cleanup_interval_minutes === 'number') ? s.approval_cleanup_interval_minutes : 15;
                var _acg = document.getElementById('approvalCleanupEndedGraceHoursInput');
                if (_acg) _acg.value = (typeof s.approval_cleanup_ended_grace_hours === 'number') ? s.approval_cleanup_ended_grace_hours : 1;
                var _act = document.getElementById('approvalCleanupInactiveTTLHoursInput');
                if (_act) _act.value = (typeof s.approval_cleanup_inactive_ttl_hours === 'number') ? s.approval_cleanup_inactive_ttl_hours : 24;

                toggleEmailFields();
                toggleTurnstileFields();
            } else {
                // R122: non-success tanpa pesan membuat form kosong diam-diam.
                showToast(res.message || 'Gagal memuat pengaturan', 'error');
            }
        })
        .catch(function () {
            // R122: gagal jaringan = unhandled rejection bila tak ditangkap.
            showToast('Gagal memuat pengaturan', 'error');
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

    // S27 double-submit guard (setelah konfirmasi disetujui).
    const btn = document.getElementById('bulkToggleBtn');
    if (!btn || btn.disabled) return;
    btn.disabled = true;
    var originalHtml = btn.innerHTML;

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
    } finally {
        btn.disabled = false;
        btn.innerHTML = originalHtml;
    }
}




// ===== Submission Detail Modal =====
var activeSubmissionId = null;
// S91 (kelas race S78): klik cepat dua submission tidak boleh membiarkan
// respons lambat permintaan pertama menimpa modal permintaan terakhir.
var submissionDetailSeq = 0;

function showSubmissionDetail(id) {
    activeSubmissionId = id;
    var seq = ++submissionDetailSeq;
    const container = document.getElementById('detailAnswersContainer');
    container.innerHTML = '<div style="color:var(--color-text-secondary); text-align:center; padding: 20px;">Memuat detail jawaban...</div>';
    document.getElementById('detailStudentName').textContent = '...';
    document.getElementById('detailStudentClass').textContent = '...';

    // R25: buka via API Modal terpusat.
    Modal.open('detailModal');

    apiFetch(`/admin/api/submissions/${id}/detail`)
        .then(r => r.json())
        .then(res => {
            if (seq !== submissionDetailSeq) return; // S91: respons basi diabaikan
            if (!res.success) {
                showToast(res.message || 'Gagal memuat detail', 'error');
                closeDetailModal();
                return;
            }

            document.getElementById('detailStudentName').textContent = res.student_name;
            document.getElementById('detailStudentClass').textContent = res.student_class;
            document.getElementById('detailExamName').textContent = res.exam_name || '—';
            document.getElementById('detailStartTime').textContent = res.start_time ? formatDateTimeID(res.start_time) : '—';
            document.getElementById('detailSubmitTime').textContent = res.created_at ? formatDateTimeID(res.created_at) : '—';
            document.getElementById('detailMacAddress').textContent = res.mac_address || '—';

            container.innerHTML = '';
            const answers = res.answers || {};
            const questions = res.questions || [];
            const evaluated = res.evaluated_answers || {};

            if (questions.length === 0) {
                container.innerHTML = '<div style="color:var(--color-text-muted); text-align:center; padding: 20px;">Tidak ada konfigurasi soal untuk ujian ini.</div>';
                return;
            }

            const fmtAns = (val) => {
                if (val === undefined || val === null) return '—';
                if (Array.isArray(val)) return val.join(', ');
                if (typeof val === 'object') return Object.keys(val).map(k => `${k}→${val[k]}`).join(', ');
                return String(val);
            };

            const listHeader = document.createElement('div');
            listHeader.className = 'sub-detail-list-header';
            listHeader.innerHTML = '<span>No</span><span>Jawaban Siswa</span><span>Kunci Jawaban</span><span>Status</span>';
            container.appendChild(listHeader);

            const list = document.createElement('div');
            list.className = 'sub-detail-list';

            let correctCount = 0;
            let partialCount = 0;
            let incorrectCount = 0;
            let unansweredCount = 0;
            let earnedTotal = 0;
            let maxTotal = 0;

            questions.forEach(q => {
                const qNum = String(q.number);
                const weight = parseFloat(q.weight || 1.0);
                const studentAns = answers[qNum];
                const correctAns = q.key;
                const evalData = evaluated[qNum] || {};

                const earnedPoints = evalData.earned || 0;
                const statusText = evalData.statusText || 'Belum Dijawab';
                const statusClass = evalData.statusClass || 'unanswered';

                maxTotal += weight;
                earnedTotal += earnedPoints;
                if (statusClass === 'correct') correctCount++;
                else if (statusClass === 'partial') partialCount++;
                else if (statusClass === 'unanswered') unansweredCount++;
                else incorrectCount++;

                const item = document.createElement('div');
                item.className = `sub-detail-row ${escapeHtml(statusClass)}`;
                item.innerHTML = `
                    <span class="sub-detail-num">No. ${escapeHtml(qNum)}</span>
                    <div class="sub-detail-cell">
                        <span class="sub-detail-cell-label">Jawaban Siswa</span>
                        <span class="sub-detail-cell-value">${escapeHtml(fmtAns(studentAns))}</span>
                    </div>
                    <div class="sub-detail-cell sub-detail-key-cell">
                        <span class="sub-detail-cell-label">Kunci Jawaban</span>
                        <span class="sub-detail-cell-value">${escapeHtml(fmtAns(correctAns))}</span>
                    </div>
                    <div class="sub-detail-result">
                        <span class="sub-detail-status ${escapeHtml(statusClass)}">${escapeHtml(statusText)}</span>
                        <span class="sub-detail-points">${earnedPoints.toFixed(1)} / ${weight.toFixed(1)} Poin</span>
                    </div>
                `;
                list.appendChild(item);
            });

            container.appendChild(list);

            const pills = [
                `<span class="sub-detail-pill correct">Benar ${correctCount}</span>`,
                `<span class="sub-detail-pill incorrect">Salah ${incorrectCount}</span>`,
            ];
            if (partialCount > 0) pills.splice(1, 0, `<span class="sub-detail-pill partial">Parsial ${partialCount}</span>`);
            if (unansweredCount > 0) pills.push(`<span class="sub-detail-pill unanswered">Kosong ${unansweredCount}</span>`);

            const summary = document.createElement('div');
            summary.className = 'sub-detail-summary';
            summary.innerHTML = `
                <div class="sub-detail-pills">${pills.join('')}</div>
                <div class="sub-detail-total">
                    <span class="sub-detail-total-label">Total Poin</span>
                    <span class="sub-detail-total-value">${earnedTotal.toFixed(1)} / ${maxTotal.toFixed(1)}</span>
                </div>
            `;
            container.prepend(summary);
        })
        .catch(() => {
            if (seq !== submissionDetailSeq) return; // S91: kegagalan basi tak boleh menutup modal milik lain
            showToast('Gagal memuat detail jawaban', 'error');
            closeDetailModal();
        });
}

function closeDetailModal() {
    // R25: delegasi ke API Modal terpusat.
    Modal.close('detailModal');
}

// ===== Identity Popup (klik nama siswa di tabel hasil) =====
// M24: popup kini dibuka via registry Actions (data-action="identity-open")
// sehingga filter keyboard global [data-action][role="button"] memberi Enter/
// Space paritas dengan klik — tabindex=0 tanpa jalur keyboard = interaksi
// ghost. Handler klik-lama (closest tombol) dihapus dari listener tabel;
// guard penutupan di luar popup kini melewatkan klik pada tombol/popup.
var LABEL_MAP = { student_name: 'Nama', exam_number: 'Nomor Ujian', student_class: 'Kelas', nama: 'Nama', nomor_ujian: 'Nomor Ujian', kelas: 'Kelas' };
(function() {
    var table = document.getElementById('submissionsTable');
    if (!table) return;
    table.addEventListener('click', function(e) {
        // M24: klik pada tombol identitas ditangani registry Actions
        // (data-action="identity-open"); early-return agar tombol tidak
        // dianggap "klik di luar popup" oleh guard di bawah. Body render
        // lama yang men-dereference btn DIHAPUS: btn kini selalu null di
        // path ini (render tinggal di handler registry 'identity-open'),
        // sisanya hanya melempar TypeError pada setiap klik biasa di tabel.
        if (e.target.closest('.submission-identity-btn')) {
            return;
        }
        // Click di luar popup → tutup semua popup yang terbuka
        if (!e.target.closest('.identity-popup') && !e.target.closest('.submission-identity-btn')) {
            document.querySelectorAll('.identity-popup.show').forEach(function(p) { p.classList.remove('show'); });
        }
    });
})();

// R136: listener dokumen tanpa syarat DIHAPUS - ia menutup popup walau klik
// terjadi DI DALAM popup (seleksi teks identitas), melumpuhkan guard
// closest('.identity-popup') milik listener ber-scope tabel di atas.

// ===== S28: pencarian client-side DIHAPUS ==================================
// Fungsi pencarian/filter baris versi client-side beserta timer debounce-nya
// dan listener Enter-nya dihapus — dashboard.html mendefinisikan ulang versi
// URL-navigasi sendiri SETELAH file ini dimuat, sehingga versi di sini MATI
// (tertimpa) tapi listener Enter-nya tetap hidup dan menyebabkan flicker
// dobel-alur saat menekan Enter di kolom cari. Pulihkan dari git history
// bila strategi client-side kelak dibutuhkan.

// Peta value→label status ujian tetap dipertahankan sebagai konstanta bersama:
// menjadi acuan label badge status (label baru "Nonaktif Otomatis") dan
// kompatibel dengan skrip halaman yang membacanya lintas file.
var EXAM_STATUS_LABELS = { active: 'Aktif', inactive: 'Nonaktif', tombstoned: 'Nonaktif Otomatis' };

// ===== Calculate Duration =====
document.addEventListener('DOMContentLoaded', function() {
    // Keyboard shortcuts (admin pages with dashboard)
    if (document.querySelector('.dashboard')) {
        initKeyboardShortcuts();
        // Auto-refresh hanya untuk halaman yang benar-benar punya stats grid
        // (#statsGrid hanya ada di dashboard.html). Halaman admin lain
        // (pengawas, users, vouchers, ...) tidak perlu fetch /admin/api/stats
        // tiap 30 detik — dan di pengawas.html malah bisa menimpa angka kartu
        // "Ujian Diawasi" (scope Stats() ≠ scope ListPengawasExams).
        if (document.getElementById('statsGrid') && !window.location.pathname.includes('submissions')) {
            startAutoRefresh(30);
        }
    }

    // R55: kalkulator durasi versi singkat ("Xj Ym") DIHAPUS —
    // submissions.html punya formatter verbose sendiri dan keduanya berlomba
    // menimpa isi sel beberapa detik setelah render. Satu sumber kebenaran:
    // formatter di submissions.html.
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

// ===== R28-lanjutan (Batch 7): registrasi aksi delegasi dashboard ============
// Semua fungsi milik admin.js yang sebelumnya dipanggil onclick inline di
// dashboard.html kini didaftarkan ke registry Actions (admin-core.js) lewat
// SATU blok terpusat ini — argumen dibawa lewat atribut data-* pada elemen.
// Aksi milik skrip inline dashboard.html (instansi-open, exams-search, dst.)
// didaftarkan di inline script halaman itu sendiri.
//
// Guard typeof: admin.js juga dieksekusi harness uji tanpa admin-core.
if (typeof Actions !== 'undefined' && typeof Actions.register === 'function') {
    // Resolver nama fungsi penutup modal: utama via window, fallback ke
    // globalThis (identik di browser; berguna di lingkungan sandbox).
    var __resolveModalCloseFn = function (name) {
        if (!name) return null;
        if (typeof window[name] === 'function') return window[name];
        if (typeof globalThis !== 'undefined' && typeof globalThis[name] === 'function') return globalThis[name];
        return null;
    };

    // Toolbar & utilitas
    Actions.register('page-reload', function () { location.reload(); });
    // Nonaktifkan/Aktifkan terpilih (toolbar bulk) — milik admin.js.
    // (bulk-delete-confirm milik skrip inline dashboard.html.)
    Actions.register('bulk-toggle-exams', function () { bulkToggleExams(); });
    // Tutup modal via nama fungsi penutup (data-modal-close). Dipakai tombol
    // ✕/Batal modal; perilaku identik memanggil close*() langsung.
    Actions.register('modal-close', function (el) {
        var fn = __resolveModalCloseFn(el.getAttribute('data-modal-close'));
        if (fn) fn();
    });
    // Backdrop modal (modal-dismiss) kini diregistrasi KANONIK di
    // admin-core.js — tersedia otomatis di semua halaman (Batch 8).

    // Token kolom daftar ujian
    Actions.register('token-copy', function (el) { copyToken(el.getAttribute('data-token')); });
    Actions.register('token-edit-open', function (el) {
        openEditTokenModal(parseInt(el.getAttribute('data-exam-id'), 10), el.getAttribute('data-token'));
    });
    Actions.register('token-interval-save', function (el) {
        saveTokenInterval(parseInt(el.getAttribute('data-exam-id'), 10));
    });

    // Baris daftar ujian
    Actions.register('exam-toggle-status', function (el) {
        toggleExam(parseInt(el.getAttribute('data-exam-id'), 10));
    });
    // Popup pengawas: hentikan propagasi SEKARANG (listener dokumen lain tidak
    // boleh melihat klik ini — paritas dengan onclick attribute lama yang
    // berhenti di elemen sebelum mencapai document).
    Actions.register('pengawas-popup-toggle', function (el, ev) {
        if (ev && typeof ev.stopImmediatePropagation === 'function') ev.stopImmediatePropagation();
        togglePengawasPopup(ev, parseInt(el.getAttribute('data-exam-id'), 10));
    });
    Actions.register('questions-open', function (el) {
        openQuestionsModal(parseInt(el.getAttribute('data-exam-id'), 10), el.getAttribute('data-exam-name'));
    });
    // Dropdown baris: sama seperti popup pengawas — closer "tutup semua
    // dropdown" yang terdaftar lebih lambat di document TIDAK boleh langsung
    // menutup menu yang baru dibuka handler ini.
    Actions.register('row-dropdown-toggle', function (el, ev) {
        if (ev && typeof ev.stopImmediatePropagation === 'function') ev.stopImmediatePropagation();
        toggleRowDropdown(ev, parseInt(el.getAttribute('data-exam-id'), 10));
    });
    Actions.register('edit-exam-open', function (el) {
        openEditExamModal(parseInt(el.getAttribute('data-exam-id'), 10), el.getAttribute('data-exam-name'));
    });
    Actions.register('delegate-exam-open', function (el) {
        openDelegateExamModal(parseInt(el.getAttribute('data-exam-id'), 10));
    });
    Actions.register('delegate-exam-confirm', function () { confirmDelegateExam(); });
    Actions.register('exam-delete', function (el) {
        deleteExam(parseInt(el.getAttribute('data-exam-id'), 10), el.getAttribute('data-exam-name'));
    });

    // Modal konfigurasi soal & kontrolnya
    Actions.register('panel-color-set', function (el) { setPanelColor(el.getAttribute('data-color')); });
    Actions.register('identity-field-add', function () { addIdentityField(); });
    // T15/R29: hapus baris field identitas via data-action (pengganti onclick
    // inline pada string HTML addIdentityFieldRow) + tandai konfigurasi kotor.
    Actions.register('identity-field-remove', function (el) {
        var row = el.closest ? el.closest('.identity-field-row') : null;
        if (row && typeof row.remove === 'function') row.remove();
        markQuestionsConfigDirty(); // S2: hapus baris identitas = belum tersimpan
    });
    // R29: pengganti onclick inline pada string HTML render-JS admin.js
    // (kontrol halaman siswa, tombol hapus soal, divider sisip soal).
    Actions.register('toggle-public-results', function (el) {
        togglePublicResults(parseInt(el.getAttribute('data-exam-id'), 10));
    });
    Actions.register('toggle-show-answers', function (el) {
        toggleShowAnswers(parseInt(el.getAttribute('data-exam-id'), 10));
    });
    Actions.register('question-remove', function (el) { removeQuestionCard(el); });
    Actions.register('question-insert-at', function (el) {
        insertQuestionAt(parseInt(el.getAttribute('data-index'), 10));
    });
    Actions.register('questions-generate', function () { quickGenerateQuestions(); });
    Actions.register('weights-set-all', function () { setAllWeights(); });
    Actions.register('ai-prompt-copy', function () { copyAIPrompt(); });
    Actions.register('xml-import-browse', function () {
        var input = document.getElementById('xmlFileInput');
        if (input && typeof input.click === 'function') input.click();
    });
    Actions.register('xml-export', function () { exportXMLQuestions(); });
    Actions.register('questions-save', function () { saveQuestionsConfig(); });

    // ===== Batch 8: wrapper pindahan dari inline script halaman ==============
    // Fungsi-fungsi di bawah HIDUP di admin.js ini juga, jadi registrasinya
    // dipindah ke sini (satu tempat mendefinisikan + mendaftarkan) dan fungsi
    // dipanggil LANGSUNG tanpa guard typeof window.x.
    // Halaman Pengaturan — kartu SMTP/Turnstile/Cleanup/dll. & modal password
    Actions.register('smtp-test', function () { testSmtpConnection(); });
    Actions.register('smtp-save', function () { saveSmtpSettings(); });
    Actions.register('turnstile-save', function () { saveTurnstileSettings(); });
    Actions.register('cleanup-save', function () { saveCleanupSettings(); });
    Actions.register('default-pkg-save', function () { saveDefaultPkgSettings(); });
    Actions.register('versions-save', function () { saveVersionsSettings(); });
    Actions.register('footer-save', function () { saveFooterSettings(); });
    Actions.register('seo-save', function () { saveSeoSettings(); });
    Actions.register('monetization-save', function () { saveMonetizationSettings(); });

    // Kelola User — toolbar pencarian/urut & refresh daftar
    Actions.register('users-refresh-list', function (el, ev) {
        // stopPropagation dipertahankan dari onclick lama: tombol refresh ada
        // di dalam header kartu yang punya listener klik sendiri.
        if (ev && ev.stopPropagation) ev.stopPropagation();
        loadUsersList(getCurrentUsersPage());
    });
    Actions.register('users-clear-search', function () { clearUsersSearch(); });
    Actions.register('users-search', function () { loadUsersList(1); });
    Actions.register('users-toggle-sort', function (el) {
        toggleUsersSort(el.getAttribute('data-sort'));
    });

    // Batch 10 (S51): migrasi sisa onclick render-path users/modal dinamis ke
    // data-action (pola Batch 8). Id/halaman dinormalisasi parseInt(..., 10);
    // nama user dibawa via data-name TANPA jsEscape — konteksnya atribut HTML
    // biasa, escapeHtml pada saat render sudah cukup.
    Actions.register('user-verify', function (el) {
        verifyUser(parseInt(el.getAttribute('data-user-id'), 10), el.getAttribute('data-name'));
    });
    Actions.register('user-deactivate-package', function (el) {
        deactivatePackage(parseInt(el.getAttribute('data-user-id'), 10), el.getAttribute('data-name'));
    });
    Actions.register('user-edit-open', function (el) {
        openEditUserModal(parseInt(el.getAttribute('data-user-id'), 10));
    });
    Actions.register('user-delete', function (el) {
        deleteUser(parseInt(el.getAttribute('data-user-id'), 10), el.getAttribute('data-name'));
    });
    Actions.register('users-retry-load', function (el) {
        var page = parseInt(el.getAttribute('data-page'), 10);
        loadUsersList(page || 1);
    });
    // Modal bobot massal dibangun dinamis & dibuang dengan .remove() —
    // penutup generik untuk overlay tanpa fungsi close bernama.
    Actions.register('modal-remove', function (el) {
        var overlay = el.closest ? el.closest('.modal-overlay') : null;
        if (overlay && typeof overlay.remove === 'function') overlay.remove();
    });
    Actions.register('bulk-weight-apply', function (el) { applyBulkWeight(el); });

    // Halaman Pengajuan (submissions.html)
    // Id numerik dinormalisasi parseInt(..., 10) — konsisten dengan handler
    // exam-toggle-status/token-interval-save; argumen string (token/nama/
    // warna) sengaja TIDAK di-parse.
    Actions.register('show-submission-detail', function (el) {
        var id = parseInt(el.getAttribute('data-submission-id'), 10);
        if (id) showSubmissionDetail(id);
    });
    Actions.register('delete-submission', function (el) {
        var id = parseInt(el.getAttribute('data-submission-id'), 10);
        if (id) deleteSubmission(id);
    });
    Actions.register('close-detail-modal', function () { closeDetailModal(); });
    Actions.register('export-submissions', function () { exportSubmissions(); });
    // M24: buka popup identitas — terdaftar di registry agar jalur delegasi
    // klik global DAN filter keyboard global (Enter/Space pada
    // [data-action][role=button]) sama-sama berlaku untuk tombol identitas.
    Actions.register('identity-open', function (el) {
        var btn = el.closest('.submission-identity-btn');
        if (!btn) return;
        var popup = document.getElementById('identityPopup');
        if (!popup) return;
        var raw = btn.getAttribute('data-identity');
        var data = {};
        try { data = JSON.parse(raw); } catch (x) {}
        var html = '<div class="identity-popup-header">Identitas Siswa</div><div class="identity-popup-body">';
        var hasData = false;
        var seenVals = {};
        for (var k in data) {
            if (data.hasOwnProperty(k) && data[k]) {
                var v = String(data[k]);
                if (seenVals[v]) continue;
                seenVals[v] = true;
                var label = LABEL_MAP[k] || k.replace(/_/g, ' ').replace(/\b\w/g, function(c) { return c.toUpperCase(); });
                html += '<div class="identity-popup-item"><span class="idp-label">' + escapeHtml(label) + '</span><strong class="idp-value">' + escapeHtml(v) + '</strong></div>';
                hasData = true;
            }
        }
        if (!hasData) {
            var name = btn.textContent.trim();
            if (name) {
                html += '<div class="identity-popup-item"><span class="idp-label">Nama</span><strong class="idp-value">' + escapeHtml(name) + '</strong></div>';
            }
        }
        html += '</div>';
        popup.innerHTML = html;
        document.querySelectorAll('.identity-popup.show').forEach(function(p) { if (p !== popup) p.classList.remove('show'); });
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
    });
}

// Keyboard parity untuk elemen NON-button ber-data-action (badge status,
// kartu instansi, kode token, badge pengawas — semuanya role="button"
// tabindex="0"): Enter/Space memicu click yang sama dengan klik mouse.
// BUTTON/A sengaja dikecualikan agar tidak dobel (browser sudah memicu click
// native untuk mereka).
document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var el = e.target && e.target.closest ? e.target.closest('[data-action][role="button"]') : null;
    if (!el || !Actions.has(el.getAttribute('data-action'))) return;
    if (typeof e.preventDefault === 'function') e.preventDefault();
    if (typeof el.click === 'function') el.click();
});

