/* GENERATED from the merged settings page — see templates/admin/settings.html.
   Loaded lazily when the Pengaturan Umum tab is first opened.
   Owns two cards moved here in the 5-tab redesign:
     - SaaS & SMTP Email Settings (loadSaasSettings, defined in admin.js)
     - Pengaturan Paket (initPackages, defined in settings-packages.js)
   Also wires the accordion that lets each titled sub-section of the SaaS form
   collapse (the form carries many settings), and makes the packages card
   collapsible too. State persists per-user in localStorage.                */

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

// Turn an <h4> into the clickable accordion header of its block: click toggles
// the body (all content after the heading, whether it lives in the heading's
// wrapper div or as later siblings of that wrapper).
function makeCollapsibleBlock(h, index) {
    var wrapper = h.closest('div[style*="border-top"]') || h.parentNode;
    var heads = Array.prototype.slice.call(wrapper.parentNode.querySelectorAll('h4'));
    var i = heads.indexOf(h);
    var next = heads[i + 1];
    var nextWrapper = next ? (next.closest('div[style*="border-top"]') || next.parentNode) : null;
    var stop = nextWrapper || document.getElementById('saveSaasSettingsBtn');

    // Assemble the block: everything from the heading's wrapper up to (not
    // including) the next wrapper — so fields sitting outside the wrapper
    // (e.g. the Versi Aplikasi rows) still collapse with their heading.
    var parent = wrapper.parentNode;
    var block = document.createElement('div');
    block.className = 'saas-collapse';
    block.setAttribute('data-collapse-id', 'saas-' + index);
    // Insert the block at the wrapper's position BEFORE moving nodes, then
    // sweep the wrapper and its following siblings (up to the next heading's
    // wrapper) into it.
    parent.insertBefore(block, wrapper);
    var node = wrapper;
    while (node && node !== stop) {
        var nx = node.nextSibling;
        block.appendChild(node);
        node = nx;
    }

    // Move the heading out of its wrapper so it becomes the block's header.
    h.remove();
    block.insertBefore(h, block.firstChild);

    // The rest of the block (wrapper leftovers + later siblings) is the body.
    var body = document.createElement('div');
    body.className = 'saas-collapse-body';
    while (block.children.length > 1) body.appendChild(block.children[1]);
    block.appendChild(body);

    // Drop wrappers left empty after the heading moved out.
    if (wrapper.parentNode === body && !wrapper.textContent.trim()) wrapper.remove();

    // Heading behaviour.
    h.classList.add('saas-collapse-head');
    h.setAttribute('aria-expanded', 'true');
    h.setAttribute('role', 'button');
    h.setAttribute('tabindex', '0');
    var chev = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    chev.setAttribute('class', 'saas-collapse-chev');
    chev.setAttribute('viewBox', '0 0 20 20');
    chev.setAttribute('fill', 'none');
    chev.setAttribute('stroke', 'currentColor');
    chev.setAttribute('stroke-width', '2');
    chev.setAttribute('stroke-linecap', 'round');
    chev.setAttribute('stroke-linejoin', 'round');
    chev.innerHTML = '<path d="M6 8l4 4 4-4"/>';
    h.appendChild(chev);
    var toggle = function (e) {
        if (e && e.type === 'keydown' && e.key !== 'Enter' && e.key !== ' ') return;
        if (e && e.type === 'keydown') e.preventDefault();
        toggleGeneralCollapse(h);
    };
    h.addEventListener('click', toggle);
    h.addEventListener('keydown', toggle);

    // Restore persisted state (default: collapsed so the page stays compact;
    // a stored '1' means the user explicitly opened this block before).
    var saved = '0';
    try { saved = localStorage.getItem(collapseKeyFor(block)); } catch (e) {}
    if (saved !== '1') {
        body.style.display = 'none';
        h.setAttribute('aria-expanded', 'false');
        h.classList.add('collapsed');
    }
}

function setupGeneralCollapse() {
    var form = document.getElementById('saasSettingsForm');
    if (form && !form.dataset.collapseReady) {
        form.dataset.collapseReady = '1';
        Array.prototype.slice.call(form.querySelectorAll('h4')).forEach(makeCollapsibleBlock);
    }

    // The Pengaturan Paket card (outside the SaaS form) is collapsible too:
    // replace it with a .saas-collapse whose body holds its former children.
    var pkgCard = document.getElementById('packages-card');
    if (pkgCard && !pkgCard.dataset.collapseReady) {
        pkgCard.dataset.collapseReady = '1';
        var pkgWrap = document.createElement('div');
        pkgWrap.className = 'saas-collapse';
        pkgWrap.setAttribute('data-collapse-id', 'packages');
        pkgCard.parentNode.insertBefore(pkgWrap, pkgCard);

        var pkgHead = document.createElement('button');
        pkgHead.type = 'button';
        pkgHead.className = 'saas-collapse-head packages-head';
        pkgHead.setAttribute('aria-expanded', 'true');
        pkgHead.innerHTML = '<span class="saas-collapse-title"><svg class="icon-svg" style="width:18px;height:18px;color:#a5b4fc;"><use href="#hi-settings"/></svg> Pengaturan Paket</span>' +
            '<svg class="saas-collapse-chev" aria-hidden="true" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 8l4 4 4-4"/></svg>';
        pkgHead.addEventListener('click', function () { toggleGeneralCollapse(pkgHead); });
        pkgWrap.appendChild(pkgHead);

        var pkgBody = document.createElement('div');
        pkgBody.className = 'saas-collapse-body';
        while (pkgCard.children.length) pkgBody.appendChild(pkgCard.children[0]);
        pkgWrap.appendChild(pkgBody);
        pkgCard.remove();

        var saved = '0';
        try { saved = localStorage.getItem(collapseKeyFor(pkgWrap)); } catch (e) {}
        if (saved !== '1') {
            pkgBody.style.display = 'none';
            pkgHead.setAttribute('aria-expanded', 'false');
            pkgHead.classList.add('collapsed');
        }
    }
}

// ---- Buka Semua / Lipat Semua (top-right button of the SaaS card) ----
// Returns true when at least one block in the general section is collapsed.
function anyGeneralCollapsed() {
    var blocks = Array.prototype.slice.call(document.querySelectorAll('#section-general .saas-collapse'));
    return blocks.some(function (b) {
        var body = b.querySelector('.saas-collapse-body');
        return body && body.style.display === 'none';
    });
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
    var expand = anyGeneralCollapsed();
    label.textContent = expand ? 'Buka Semua' : 'Lipat Semua';
    icon.style.transform = expand ? 'rotate(0deg)' : 'rotate(180deg)';
    btn.title = expand ? 'Buka semua bagian' : 'Lipat semua bagian';
}

window.toggleAllGeneralCollapse = function () {
    setAllGeneralCollapse(anyGeneralCollapsed());
};

window.__settingsReady['general'] = function() {
    setupGeneralCollapse();
    updateToggleAllLabel();
    if (document.getElementById('emailEnabledInput')) loadSaasSettings();
    if (typeof window.initPackages === 'function') window.initPackages();
};
window.toggleGeneralCollapse = toggleGeneralCollapse;
