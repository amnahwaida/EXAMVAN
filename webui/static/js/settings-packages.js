/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */

(function() {
 var PACKAGES = [];
 var ROLE_NAMES = { 'guru': 'Guru', 'pengawas': 'Pengawas', 'operator': 'Operator' };
 var ROLE_MAP = { 'guru': 'guru', 'pengawas': 'pengawas', 'operator': 'operator' };

 function parseRole(roleStr) {
  if (!roleStr) return '';
  try {
   var arr = JSON.parse(roleStr);
   if (Array.isArray(arr) && arr.length > 0) return ROLE_MAP[arr[0]] || '';
  } catch (e) {}
  return '';
 }

 function renderTable() {
  var tbody = document.getElementById('packagesTableBody');
  if (!PACKAGES.length) {
   tbody.innerHTML = '<tr><td colspan="7" style="padding:32px;text-align:center;color:var(--color-text-secondary);">Belum ada data paket.</td></tr>';
   return;
  }
  var html = '';
  PACKAGES.forEach(function(p) {
   var roleOpts = '';
    var pdfMaxMb = window.__storageFreeMb > 0 ? Math.min(Math.floor(window.__storageFreeMb), 100) : 0; /* 100 MB = batas upload global maxFileSize (exams.go) */
    ['', 'guru', 'pengawas', 'operator'].forEach(function(r) {
    var label = r === '' ? 'Tanpa Role' : (ROLE_NAMES[r] || r);
    roleOpts += '<option value="' + r + '"' + (p.role === r ? ' selected' : '') + '>' + label + '</option>';
   });
   html += '<tr style="border-bottom:1px solid var(--color-glass-border);">' +
    '<td data-label="Nama Paket" style="padding:14px 20px;"><input type="text" data-field="label" value="' + escapeHtml(p.label) + '" style="font-weight:600;" />' +
    '<span class="pkg-key-badge">' + escapeHtml(p.key) + '</span></td>' +
    '<td data-label="Total Ujian" style="padding:14px 20px;"><input type="number" min="1" step="1" data-field="max_exams" value="' + p.max_exams + '" /></td>' +
    '<td data-label="Ujian Serentak" style="padding:14px 20px;"><input type="number" min="1" step="1" data-field="concurrent" value="' + p.concurrent + '" /></td>';
   html += '<td data-label="Maks. PDF (MB)" style="padding:14px 20px;"><input type="number" min="1" step="0.5" data-field="pdf_mb" value="' + p.pdf_mb + '"' + (pdfMaxMb > 0 ? ' max="' + pdfMaxMb + '"' : '') + ' title="Maks ukuran PDF paket (MB). Tidak boleh melebihi sisa disk server (upload global dibatasi 100 MB)." /></td>' +
    '<td data-label="Storage (MB)" style="padding:14px 20px;"><input type="number" min="1" step="0.5" data-field="storage_mb" value="' + p.storage_mb + '"' + (window.__storageFreeMb > 0 ? ' max="' + Math.floor(window.__storageFreeMb) + '"' : '') + ' title="Batas storage paket (MB). Tidak boleh melebihi sisa disk server." /></td>' +
    '<td data-label="Maks. Akun Guru" style="padding:14px 20px;"><input type="number" min="0" step="1" data-field="max_users" value="' + p.max_users + '" title="Batas jumlah akun yang boleh dibuat operator di instansi ini (0 = tanpa batas)" /></td>' +
    '<td data-label="Role" style="padding:14px 20px;"><select data-field="role">' + roleOpts + '</select></td>' +
    '</tr>';
  });
  tbody.innerHTML = html;
 }

  // S92 (ronde 10): token permintaan monoton — reload pasca-save tidak
  // boleh ditimpa respons loadPackages lama yang mendarat terakhir.
  var packageLoadSeq = 0;
  function loadPackages() {
   var seq = ++packageLoadSeq;
   apiFetch('/admin/api/packages')
    .then(function(r) { return r.json(); })
    .then(function(res) {
     if (seq !== packageLoadSeq) return;
     if (!res.success) throw new Error(res.message);
    PACKAGES = (res.packages || []).map(function(p) {
     return {
      key: p.key,
      label: p.label,
      max_exams: p.max_exams,
      concurrent: p.max_concurrent_exams,
      pdf_mb: p.max_pdf_size_mb,
      storage_mb: p.max_storage_mb,
      max_users: p.max_users || 0,
      role: parseRole(p.role)
     };
    });
    renderTable();
   })
   .catch(function(err) {
    if (seq !== packageLoadSeq) return;
    console.error(err);
    document.getElementById('packagesTableBody').innerHTML =
     '<tr><td colspan="7" style="padding:32px;text-align:center;color:var(--color-danger-light);">Gagal memuat pengaturan paket.</td></tr>';
   });
 }

 function savePackages() {
  var rows = document.querySelectorAll('#packagesTableBody tr');
  var packages = [];
  var invalid = null;

  PACKAGES.forEach(function(p, i) {
   var tr = rows[i];
   var label = tr.querySelector('input[data-field="label"]').value.trim();
   if (!label) { invalid = 'Nama paket tidak boleh kosong'; return; }
   packages.push({
    key: p.key,
    label: label,
    max_exams: Math.max(1, parseInt(tr.querySelector('input[data-field="max_exams"]').value, 10) || 1),
    max_concurrent_exams: Math.max(1, parseInt(tr.querySelector('input[data-field="concurrent"]').value, 10) || 1),
    max_pdf_size_mb: Math.max(1, parseFloat(tr.querySelector('input[data-field="pdf_mb"]').value) || 1),
    max_storage_mb: Math.max(1, parseFloat(tr.querySelector('input[data-field="storage_mb"]').value) || 1),
    max_users: Math.max(0, parseInt(tr.querySelector('input[data-field="max_users"]').value, 10) || 0),
    role: tr.querySelector('select[data-field="role"]').value
   });
  });
  if (invalid) { showToast(invalid, 'error'); return; }

  // Pre-check kuota paket terhadap sisa kapasitas disk server (server juga memvalidasi).
  if (window.__storageFreeMb > 0) {
   for (var i = 0; i < packages.length; i++) {
    if (packages[i].max_storage_mb > window.__storageFreeMb) {
     showToast('Paket ' + packages[i].label + ': Maks Storage melebihi sisa kapasitas disk server (' + Math.floor(window.__storageFreeMb) + ' MB)', 'error');
     return;
    }
    if (packages[i].max_pdf_size_mb > window.__storageFreeMb) {
     showToast('Paket ' + packages[i].label + ': Maks Ukuran PDF melebihi sisa kapasitas disk server (' + Math.floor(window.__storageFreeMb) + ' MB)', 'error');
     return;
    }
    if (packages[i].max_pdf_size_mb > 100) { /* 100 MB = batas upload global maxFileSize (exams.go) */
     showToast('Paket ' + packages[i].label + ': Maks Ukuran PDF melebihi batas upload global (100 MB)', 'error');
     return;
    }
   }
  }

  var btn = document.getElementById('btnSavePackages');
  var orig = btn.textContent;
  btn.disabled = true;
  btn.textContent = 'Menyimpan...';

  apiFetch('/admin/api/packages', {
   method: 'POST',
   headers: { 'Content-Type': 'application/json' },
   body: JSON.stringify({ packages: packages })
  })
  .then(function(r) { return r.json(); })
  .then(function(res) {
   btn.disabled = false;
   btn.textContent = orig;
   if (res.success) {
    showToast(res.message, 'success');
    loadPackages();
   } else {
    showToast(res.message || 'Gagal menyimpan pengaturan paket', 'error');
   }
  })
  .catch(function(err) {
   console.error(err);
   btn.disabled = false;
   btn.textContent = orig;
   showToast('Gagal terhubung ke server', 'error');
  });
 }

 // R140 (ronde 11): registrasi yatim ke __settingsReady untuk key section
 // "packages" dihapus — key itu tak pernah eksis pasca redesign 5-tab;
 // jalur init hidup adalah initPackages (dipanggil settings-general.js).
 window.initPackages = loadPackages;
 window.savePackages = savePackages;

 // Batch 7 (R28): tombol "Simpan Perubahan" paket via delegasi data-action.
 if (window.Actions && typeof window.Actions.register === 'function') {
  window.Actions.register('packages-save', function () { savePackages(); });
 }
})();
