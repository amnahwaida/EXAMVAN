/* EXAMVAN Pengawas Monitoring Page */

function loadPengawasExams() {
    var container = document.getElementById('pengawasExamList');
    if (!container) return;
    container.innerHTML = '<div style="text-align:center;padding:40px;color:var(--color-text-secondary);">⏳ Memuat data ujian...</div>';

    apiFetch('/admin/api/pengawas/exams')
        .then(function(r) { return r.json(); })
        .then(function(res) {
            if (!res.success) {
                container.innerHTML = '<div style="text-align:center;padding:40px;color:#fca5a5;">Gagal memuat data</div>';
                return;
            }
            var exams = res.exams || [];
            updatePengawasStats(exams);

            if (exams.length === 0) {
                var msg = res.is_privileged
                    ? 'Belum ada ujian yang tersedia.'
                    : 'Belum ada ujian yang ditugaskan kepada Anda sebagai pengawas.';
                container.innerHTML = '<div style="text-align:center;padding:40px;color:var(--color-text-secondary);">' + msg + '</div>';
                return;
            }

            container.innerHTML = '';
            exams.forEach(function(ex) {
                var card = document.createElement('div');
                card.className = 'exam-monitor-card';
                card.id = 'exam-card-' + ex.id;

                var total = ex.total_students || 0;
                var submitted = ex.submitted_count || 0;
                var pct = total > 0 ? Math.round(submitted / total * 100) : 0;
                var barColor = pct >= 100 ? '#34d399' : (pct > 0 ? '#60a5fa' : '#6b7280');
                var statusText = ex.status === 'active' ? '<span class="status-badge status-active">Aktif</span>' : '<span class="status-badge status-inactive">Nonaktif</span>';
                var scheduleText = (ex.start_time && ex.end_time) ? (ex.start_time + ' - ' + ex.end_time) : '—';

                card.innerHTML =
                    '<div class="exam-monitor-header">' +
                        '<div>' +
                            '<div class="exam-monitor-title">' + escapeHtml(ex.name) + '</div>' +
                            '<div class="exam-monitor-meta">' +
                                '<span>Token: <strong>' + escapeHtml(ex.token) + '</strong></span>' +
                                '<span>Jadwal: ' + scheduleText + '</span>' +
                                '<span>Pembuat: ' + escapeHtml(ex.creator_name) + '</span>' +
                            '</div>' +
                        '</div>' +
                        '<div>' + statusText + '</div>' +
                    '</div>' +
                    '<div class="exam-monitor-progress">' +
                        '<div class="progress-bar-container">' +
                            '<div class="progress-bar-fill" style="width:' + pct + '%;background:' + barColor + ';"></div>' +
                        '</div>' +
                        '<span class="progress-text" style="color:' + barColor + ';">' + submitted + '/' + total + ' (' + pct + '%)</span>' +
                        '<button class="student-list-toggle" onclick="toggleStudentList(' + ex.id + ')" id="toggle-btn-' + ex.id + '"><svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-chevron-down"/></svg> Lihat Siswa</button>' +
                    '</div>' +
                    '<div id="student-list-' + ex.id + '" style="display:none;"></div>';
                container.appendChild(card);
            });
        })
        .catch(function() {
            container.innerHTML = '<div style="text-align:center;padding:40px;color:#fca5a5;">Gagal menghubungi server</div>';
        });
}

function updatePengawasStats(exams) {
    var total = exams.length;
    var active = exams.filter(function(e) { return e.status === 'active'; }).length;
    var totalStudents = exams.reduce(function(sum, e) { return sum + (e.total_students || 0); }, 0);
    var totalSubmitted = exams.reduce(function(sum, e) { return sum + (e.submitted_count || 0); }, 0);

    document.getElementById('statTotalExam').textContent = total;
    document.getElementById('statActiveExam').textContent = active;
    document.getElementById('statTotalStudents').textContent = totalStudents;
    document.getElementById('statSubmitted').textContent = totalSubmitted;
}

function toggleStudentList(examId) {
    var listEl = document.getElementById('student-list-' + examId);
    var btn = document.getElementById('toggle-btn-' + examId);
    if (!listEl) return;

    if (listEl.style.display === 'none') {
        btn.innerHTML = '<svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-chevron-up"/></svg> Sembunyikan';
        listEl.innerHTML = '<div style="text-align:center;padding:20px;color:var(--color-text-secondary);">⏳ Memuat...</div>';
        listEl.style.display = 'block';

        apiFetch('/admin/api/pengawas/exams/' + examId + '/submissions')
            .then(function(r) { return r.json(); })
            .then(function(res) {
                if (!res.success) {
                    listEl.innerHTML = '<div style="text-align:center;padding:12px;color:#fca5a5;">Gagal memuat data siswa</div>';
                    return;
                }
                var subs = res.submissions || [];
                if (subs.length === 0) {
                    listEl.innerHTML = '<div style="text-align:center;padding:12px;color:var(--color-text-muted);">Belum ada siswa yang terdaftar.</div>';
                    return;
                }
                var html = '<table class="student-table"><thead><tr>' +
                    '<th>No</th><th>Nama Siswa</th><th>Kelas</th><th>Status</th>' +
                    '<th>Pertama Akses</th><th>Score</th>' +
                    '</tr></thead><tbody>';
                subs.forEach(function(s, i) {
                    var statusClass = s.submitted ? 'submitted' : (s.start_time ? 'in-progress' : 'not-started');
                    var statusLabel = s.submitted ? 'Terkumpul' : (s.start_time ? 'Mengerjakan' : 'Belum Mulai');
                    var firstAccess = s.first_access_at ? localizeUTC(s.first_access_at.replace(' ', 'T') + 'Z') : (s.start_time ? localizeUTC(s.start_time.replace(' ', 'T') + 'Z') : '—');
                    var scoreText = s.score !== null && s.score !== undefined ? (s.submitted ? s.score : '—') : '—';
                    var studentName = s.student_name || '—';
                    var studentClass = s.student_class || '—';
                    html += '<tr class="clickable-row" onclick="showAccessLog(' + s.id + ')">' +
                        '<td>' + (i + 1) + '</td>' +
                        '<td><strong style="color:var(--color-text);">' + escapeHtml(studentName) + '</strong><br><span style="font-size:0.7rem;color:var(--color-text-muted);font-family:monospace;">' + escapeHtml(s.mac_address || '—') + '</span></td>' +
                        '<td>' + escapeHtml(studentClass) + '</td>' +
                        '<td><span class="student-status ' + statusClass + '">' + statusLabel + '</span></td>' +
                        '<td style="font-size:0.75rem;color:var(--color-text-muted);">' + firstAccess + '</td>' +
                        '<td style="text-align:center;font-weight:600;color:' + (s.submitted ? '#34d399' : '#6b7280') + ';">' + scoreText + '</td>' +
                        '</tr>';
                });
                html += '</tbody></table>';

                // Store full submissions data on the exam container for access log lookup
                var examCard = document.getElementById('exam-card-' + examId);
                if (examCard) {
                    examCard.setAttribute('data-subs', JSON.stringify(subs));
                }

                listEl.innerHTML = html;
            })
            .catch(function() {
                listEl.innerHTML = '<div style="text-align:center;padding:12px;color:#fca5a5;">Gagal menghubungi server</div>';
            });
    } else {
        btn.innerHTML = '<svg class="icon-svg" style="width:14px;height:14px;"><use href="#hi-chevron-down"/></svg> Lihat Siswa';
        listEl.style.display = 'none';
    }
}

function findSubmissionById(id) {
    var cards = document.querySelectorAll('[data-subs]');
    for (var i = 0; i < cards.length; i++) {
        var arr = JSON.parse(cards[i].getAttribute('data-subs') || '[]');
        for (var j = 0; j < arr.length; j++) {
            if (arr[j].id === id) return arr[j];
        }
    }
    return null;
}

function showAccessLog(submissionId) {
    var modal = document.getElementById('accessLogModal');
    var body = document.getElementById('accessLogBody');
    if (!modal || !body) return;

    var subData = findSubmissionById(submissionId);
    if (!subData) {
        body.innerHTML = '<div style="text-align:center;padding:32px;color:#fca5a5;">Data tidak ditemukan</div>';
        modal.style.display = 'flex';
        return;
    }

    var logs = subData.access_logs || [];

    // Header — only MAC and status (unique device identifier, identity not fixed)
    var headerHtml =
        '<div class="log-modal-header-info">' +
            '<div class="log-identity-card">' +
                '<div class="log-id-col">' +
                    '<span class="log-id-label">Siswa</span>' +
                    '<span class="log-id-value">' + escapeHtml(subData.student_name || '—') + '</span>' +
                '</div>' +
                '<div class="log-id-col">' +
                    '<span class="log-id-label">Kelas</span>' +
                    '<span class="log-id-value">' + escapeHtml(subData.student_class || '—') + '</span>' +
                '</div>' +
                '<div class="log-id-col">' +
                    '<span class="log-id-label">No. Ujian</span>' +
                    '<span class="log-id-value">' + escapeHtml(subData.exam_number || '—') + '</span>' +
                '</div>' +
            '</div>' +
            '<div class="log-device-row">' +
                '<svg class="icon-svg" style="width:14px;height:14px;color:#64748b;"><use href="#hi-device"/></svg> ' +
                '<span style="font-family:monospace;color:var(--color-text-muted);font-size:0.8rem;">' + escapeHtml(subData.mac_address || '—') + '</span>' +
                '<span class="student-status ' + (subData.submitted ? 'submitted' : (subData.start_time ? 'in-progress' : 'not-started')) + '" style="margin-left:auto;">' + (subData.submitted ? 'Terkumpul' : (subData.start_time ? 'Mengerjakan' : 'Belum Mulai')) + '</span>' +
            '</div>' +
        '</div>';

    // Timeline — each login event shows identity data used at that time
    var timelineHtml = '';
    if (logs.length === 0) {
        timelineHtml = '<div class="no-logs">Belum ada riwayat akses tercatat.</div>';
    } else {
        timelineHtml = '<div class="access-log-timeline">';
        logs.forEach(function(log) {
            var eventLabel = log.event === 'login' ? 'Login / Mulai Mengerjakan' : (log.event === 'logout' ? 'Logout / Keluar' : 'Heartbeat / Aktif');
            var timeFormatted = log.created_at ? localizeUTC(log.created_at) : '—';
            var deviceInfo = log.device_info ? ' &middot; ' + escapeHtml(log.device_info) : '';
            var ipInfo = log.ip_address ? ' &middot; IP: ' + escapeHtml(log.ip_address) : '';

            // Identity used during this event (most relevant for login)
            var identityInfo = '';
            if (log.event === 'login' && (log.student_name || log.exam_number || log.student_class)) {
                identityInfo = '<div class="log-identity">' +
                    (log.student_name ? '<span>Nama: <strong>' + escapeHtml(log.student_name) + '</strong></span>' : '') +
                    (log.exam_number ? '<span>No. Ujian: <strong>' + escapeHtml(log.exam_number) + '</strong></span>' : '') +
                    (log.student_class ? '<span>Kelas: <strong>' + escapeHtml(log.student_class) + '</strong></span>' : '') +
                    '</div>';
            }

            timelineHtml +=
                '<div class="log-entry ' + log.event + '">' +
                    '<div class="log-event">' + eventLabel + '</div>' +
                    '<div class="log-time">' + timeFormatted + '</div>' +
                    '<div class="log-detail">' + ipInfo + deviceInfo + '</div>' +
                    identityInfo +
                '</div>';
        });
        timelineHtml += '</div>';
    }

    body.innerHTML = headerHtml + timelineHtml;
    modal.style.display = 'flex';
}

function closeAccessLogModal(event) {
    if (event && event.target !== document.getElementById('accessLogModal')) return;
    document.getElementById('accessLogModal').style.display = 'none';
}
