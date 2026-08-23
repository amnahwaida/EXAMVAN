/* GENERATED from the merged settings page — see templates/admin/settings.html.
   Loaded lazily when the Pengaturan Umum tab is first opened.
   Owns the cards moved here in the 5-tab redesign:
     - 8 SaaS cards (SMTP, Turnstile, Pembersihan, Default Paket, Versi,
       Footer, SEO, Monetisasi) — each with its own save button (per-section
       partial update, see saveSaasSection in admin.js)
     - Pengaturan Paket (initPackages, defined in settings-packages.js)
   Wires the accordion: every .saas-collapse in the section gets a clickable
   head that collapses/expands its body. State persists per-user in
   localStorage, default collapsed so the page stays compact.               */

function collapseKeyFor(block) {
    return 'saas-collapse:' + (block.getAttribute('data-collapse-id') || block.id || 'x');
}

function toggleGeneralCollapse(head) {
    var block = head.closest('.saas-collapse');
    if (!block) return;
    var body = block.querySelector('.saas-collapse-body');
    var isOpen = body.style.display !== 'none';
    body.style.display = isOpen ? 'none' : '';
    head.setAttribute('aria-expanded', isOpen ? 'false' : 'true');
    head.classList.toggle('collapsed', isOpen);
    try { localStorage.setItem(collapseKeyFor(block), isOpen ? '0' : '1'); } catch (e) {}
    if (typeof updateToggleAllLabel === 'function') updateToggleAllLabel();
}

// Wire one existing .saas-collapse: head click/keyboard toggles the body,
// then restore the persisted state (default: collapsed; a stored '1' means
// the user explicitly opened this block before).
function wireCollapseBlock(block) {
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
        toggleGeneralCollapse(head);
    };
    head.addEventListener('click', toggle);
    head.addEventListener('keydown', toggle);

    var saved = '0';
    try { saved = localStorage.getItem(collapseKeyFor(block)); } catch (e) {}
    if (saved !== '1') {
        body.style.display = 'none';
        head.setAttribute('aria-expanded', 'false');
        head.classList.add('collapsed');
    }
}

function setupGeneralCollapse() {
    // The 8 SaaS cards ship with explicit .saas-collapse markup — just wire
    // each one.
    var cards = Array.prototype.slice.call(document.querySelectorAll('#section-general .saas-collapse'));
    cards.forEach(wireCollapseBlock);

    // The Pengaturan Paket card (packages-card) has no collapsible markup in
    // the template; wrap it in a .saas-collapse whose body holds its former
    // children, so it collapses like the SaaS cards.
    var pkgCard = document.getElementById('packages-card');
    if (pkgCard && !pkgCard.dataset.collapseReady) {
        pkgCard.dataset.collapseReady = '1';
        var pkgWrap = document.createElement('section');
        pkgWrap.className = 'saas-collapse';
        pkgWrap.setAttribute('data-collapse-id', 'packages');
        pkgCard.parentNode.insertBefore(pkgWrap, pkgCard);

        var pkgHead = document.createElement('div');
        pkgHead.className = 'saas-collapse-head packages-head';
        pkgHead.setAttribute('aria-expanded', 'true');
        pkgHead.innerHTML = '<span class="saas-collapse-title"><svg class="icon-svg" style="width:18px;height:18px;color:var(--color-primary-light);"><use href="#hi-settings"/></svg> Pengaturan Paket</span>' +
            '<svg class="saas-collapse-chev" aria-hidden="true" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 8l4 4 4-4"/></svg>';
        pkgWrap.appendChild(pkgHead);

        var pkgBody = document.createElement('div');
        pkgBody.className = 'saas-collapse-body';
        while (pkgCard.children.length) pkgBody.appendChild(pkgCard.children[0]);
        pkgWrap.appendChild(pkgBody);
        pkgCard.remove();

        wireCollapseBlock(pkgWrap);
    }
}

// ---- Buka Semua / Lipat Semua (toolbar above the cards) ----
// Returns how many blocks in the general section are currently collapsed.
function countGeneralCollapsed() {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-general .saas-collapse'));
    return blocks.filter(function (b) {
        var body = b.querySelector('.saas-collapse-body');
        return body && body.style.display === 'none';
    }).length;
}

function anyGeneralCollapsed() {
    return countGeneralCollapsed() > 0;
}

function setAllGeneralCollapse(expand) {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-general .saas-collapse'));
    blocks.forEach(function (b) {
        var body = b.querySelector('.saas-collapse-body');
        var head = b.querySelector('.saas-collapse-head');
        if (!body || !head) return;
        body.style.display = expand ? '' : 'none';
        head.setAttribute('aria-expanded', expand ? 'true' : 'false');
        head.classList.toggle('collapsed', !expand);
        try { localStorage.setItem(collapseKeyFor(b), expand ? '1' : '0'); } catch (e) {}
    });
    updateToggleAllLabel();
}

function updateToggleAllLabel() {
    var btn = document.getElementById('toggleAllGeneralBtn');
    var label = document.getElementById('toggleAllGeneralLabel');
    var icon = document.getElementById('toggleAllGeneralIcon');
    if (!btn || !label || !icon) return;
    var total = document.querySelectorAll('#section-general .saas-collapse').length;
    var collapsed = countGeneralCollapsed();
    var expand = collapsed > 0;
    label.textContent = (expand ? 'Buka Semua' : 'Lipat Semua') + ' (' + collapsed + '/' + total + ' terlipat)';
    icon.style.transform = expand ? 'rotate(0deg)' : 'rotate(180deg)';
    btn.title = (expand ? 'Buka semua bagian' : 'Lipat semua bagian') + ' (' + collapsed + '/' + total + ' terlipat)';
}

window.toggleAllGeneralCollapse = function () {
    setAllGeneralCollapse(anyGeneralCollapsed());
};

// Batch 7 (R28): aksi toolbar "Buka/Lipat Semua" via delegasi data-action.
if (window.Actions && typeof window.Actions.register === 'function') {
    window.Actions.register('general-toggle-all', function () {
        window.toggleAllGeneralCollapse();
    });
}

window.__settingsReady['general'] = function() {
    setupGeneralCollapse();
    updateToggleAllLabel();
    if (document.getElementById('emailEnabledInput')) loadSaasSettings();
    if (typeof window.initPackages === 'function') window.initPackages();
};
window.toggleGeneralCollapse = toggleGeneralCollapse;
