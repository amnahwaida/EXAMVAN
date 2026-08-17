/* GENERATED from the merged settings page — see templates/admin/settings.html.
   Loaded lazily when the Pengaturan Umum tab is first opened.
   Owns two cards moved here in the 5-tab redesign:
     - SaaS & SMTP Email Settings (loadSaasSettings, defined in admin.js)
     - Pengaturan Paket (initPackages, defined in settings-packages.js)       */

window.__settingsReady['general'] = function() {
    if (document.getElementById('emailEnabledInput')) loadSaasSettings();
    if (typeof window.initPackages === 'function') window.initPackages();
};
