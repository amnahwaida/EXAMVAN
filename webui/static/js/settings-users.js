/* GENERATED from the merged settings page — see templates/admin/settings.html.
   Loaded lazily when the Kelola User tab is first opened.
   Wires the collapsible cards in #section-users:
     - Kartu "Tambah User Manual" (users-add) berisi form yang dipecah jadi
       3 sub-bagian collapsible: Informasi Akun (users-identity), Role & Paket
       (users-role), Kuota & Masa Aktif (users-quota)
     - Kartu "Daftar User & Guru" (users-list)
   Plus the Buka Semua / Lipat Semua toolbar (toggleAllUsersBtn).
   Unlike Pengaturan Umum (default collapsed — halaman konfigurasi), Kelola
   User adalah halaman kerja: semua blok default TERBUKA agar form & daftar
   siap pakai; lipatan manual user persist di localStorage.               */

function usersCollapseKeyFor(block) {
    return 'saas-collapse:' + (block.getAttribute('data-collapse-id') || block.id || 'x');
}

function toggleUsersCollapse(head) {
    var block = head.closest('.saas-collapse');
    if (!block) return;
    var body = block.querySelector('.saas-collapse-body');
    if (!body) return;
    var isOpen = body.style.display !== 'none';
    body.style.display = isOpen ? 'none' : '';
    head.setAttribute('aria-expanded', isOpen ? 'false' : 'true');
    head.classList.toggle('collapsed', isOpen);
    try { localStorage.setItem(usersCollapseKeyFor(block), isOpen ? '0' : '1'); } catch (e) {}
    updateToggleAllUsersLabel();
}

// Wire one existing .saas-collapse: head click/keyboard toggles the body,
// then restore the persisted state. Default: OPEN; a stored '0' means the
// user explicitly folded this block before.
function wireUsersCollapseBlock(block) {
    if (block.dataset.collapseReady) return;
    block.dataset.collapseReady = '1';
    var head = block.querySelector('.saas-collapse-head');
    var body = block.querySelector('.saas-collapse-body');
    if (!head || !body) return;
    if (!head.getAttribute('role')) head.setAttribute('role', 'button');
    if (!head.getAttribute('tabindex')) head.setAttribute('tabindex', '0');
    var toggle = function (e) {
        if (e && e.type === 'keydown' && e.key !== 'Enter' && e.key !== ' ') return;
        if (e && e.type === 'keydown') e.preventDefault();
        // S108 (ronde 10): klik tombol aksi di dalam head (mis. "Muat Ulang")
        // tidak boleh ikut melipat kartu — stopPropagation delegasi Actions
        // berjalan terlambat (fase bubble), jadi dicegah dari sini.
        if (e && e.target && e.target.closest && e.target.closest('[data-action]')) return;
        toggleUsersCollapse(head);
    };
    head.addEventListener('click', toggle);
    head.addEventListener('keydown', toggle);

    var saved = '';
    try { saved = localStorage.getItem(usersCollapseKeyFor(block)); } catch (e) {}
    if (saved === '0') {
        body.style.display = 'none';
        head.setAttribute('aria-expanded', 'false');
        head.classList.add('collapsed');
    }
}

function setupUsersCollapse() {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-users .saas-collapse'));
    blocks.forEach(wireUsersCollapseBlock);
    updateToggleAllUsersLabel();
}

// ---- Buka Semua / Lipat Semua (toolbar above the cards) ----
function countUsersCollapsed() {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-users .saas-collapse'));
    return blocks.filter(function (b) {
        var body = b.querySelector('.saas-collapse-body');
        return body && body.style.display === 'none';
    }).length;
}

function setAllUsersCollapse(expand) {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-users .saas-collapse'));
    blocks.forEach(function (b) {
        var body = b.querySelector('.saas-collapse-body');
        var head = b.querySelector('.saas-collapse-head');
        if (!body || !head) return;
        body.style.display = expand ? '' : 'none';
        head.setAttribute('aria-expanded', expand ? 'true' : 'false');
        head.classList.toggle('collapsed', !expand);
        try { localStorage.setItem(usersCollapseKeyFor(b), expand ? '1' : '0'); } catch (e) {}
    });
    updateToggleAllUsersLabel();
}

function updateToggleAllUsersLabel() {
    var btn = document.getElementById('toggleAllUsersBtn');
    var label = document.getElementById('toggleAllUsersLabel');
    var icon = document.getElementById('toggleAllUsersIcon');
    if (!btn || !label || !icon) return;
    var total = document.querySelectorAll('#section-users .saas-collapse').length;
    var collapsed = countUsersCollapsed();
    var expand = collapsed > 0;
    // Label cukup kata kerja (Buka Semua / Lipat Semua) — jumlah terlipat
    // dipindah ke title agar teks tombol tidak membingungkan saat 0 terlipat.
    label.textContent = expand ? 'Buka Semua' : 'Lipat Semua';
    icon.style.transform = expand ? 'rotate(0deg)' : 'rotate(180deg)';
    btn.title = (expand ? 'Buka semua bagian' : 'Lipat semua bagian') + ' (' + collapsed + '/' + total + ' terlipat)';
}

window.toggleAllUsersCollapse = function () {
    setAllUsersCollapse(countUsersCollapsed() > 0);
};

// Batch 7 (R28): aksi toolbar "Lipat/Buka Semua" via delegasi data-action.
if (window.Actions && typeof window.Actions.register === 'function') {
    window.Actions.register('users-toggle-all', function () {
        window.toggleAllUsersCollapse();
    });
}

window.__settingsReady['users'] = function() {

 setupUsersCollapse();
 loadUsersList();
 if (document.getElementById('emailEnabledInput')) loadSaasSettings();  // Cap Maks Storage di form Tambah User pada sisa kapasitas disk server.
  if (window.__storageFreeMb > 0) {
   var si = document.getElementById('storageSizeInput');
   if (si) {
    si.max = Math.floor(window.__storageFreeMb);
    si.title = 'Batas total kapasitas storage (MB). 0 = tidak terbatas. Sisa disk server: ' + fmtStorageSize(window.__storageFreeMb) + '.';
   }
  }
  // Isi badge "Sisa disk server" di header Default Paket Pendaftaran segera
  // (tanpa menunggu fetch API) dari nilai yang dirender server-side.
  var _dbt = document.getElementById('diskFreeBadgeText');
  if (_dbt) {
   _dbt.textContent = window.__storageFreeMb > 0
    ? 'Sisa disk server: ' + fmtStorageSize(window.__storageFreeMb)
    : 'Sisa disk server tidak dapat ditentukan';
  }
 if (__adminHasRole('operator')) {
  var inp = document.getElementById('instansiInput');
  if (inp) {
   inp.value = window.__adminInstansi;
   inp.readOnly = true;
   inp.style.opacity = '0.7';
   inp.title = 'Instansi otomatis mengikuti akun Anda';
  }
 }

};
