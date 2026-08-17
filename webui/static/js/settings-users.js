/* GENERATED from the standalone settings pages — see templates/admin/settings.html.
   Loaded lazily when its tab is first opened. */
window.__settingsReady['users'] = function() {

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
