/* EXAMVAN Admin Panel - Core Utilities */

// CSRF Token Helper
function getCsrfToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute('content') : '';
}

// Wrapper for fetch with CSRF headers
function apiFetch(url, options = {}) {
    const method = (options.method || 'GET').toUpperCase();
    if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
        options.headers = options.headers || {};
        options.headers['X-CSRF-Token'] = getCsrfToken();
    }
    return window.fetch.call(window, url, options);
}

// Toast notification — improved: icons, close, duration per type, a11y
const TOAST_ICONS = {
    success: '<svg class="toast-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
    error: '<svg class="toast-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
    warning: '<svg class="toast-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"/></svg>',
    info: '<svg class="toast-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
};
const TOAST_DURATION = { success: 3000, error: 5000, warning: 4000, info: 3500 };
const MAX_TOASTS = 5;

function showToast(message, type = 'success') {
    const container = document.getElementById('toastContainer');
    if (!container) return;

    // De-duplicate: an identical visible toast gets its lifetime extended
    // instead of stacking (rapid repeated actions previously piled up to 5
    // copies of the same message).
    for (const existing of container.children) {
        const msgEl = existing.querySelector('.toast-msg');
        if (msgEl && msgEl.textContent === message && existing.classList.contains('toast-' + type)) {
            if (existing.__dismissTimer) clearTimeout(existing.__dismissTimer);
            existing.__dismissTimer = setTimeout(() => {
                if (existing.isConnected) dismissToast(existing);
            }, TOAST_DURATION[type] || 3500);
            return;
        }
    }

    // Limit visible toasts
    while (container.children.length >= MAX_TOASTS) {
        const oldest = container.firstElementChild;
        if (oldest) oldest.remove();
    }

    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.setAttribute('role', 'alert');
    toast.innerHTML = `
        <span class="toast-body">
            ${TOAST_ICONS[type] || TOAST_ICONS.info}
            <span class="toast-msg">${escapeHtml(message)}</span>
        </span>
        <button class="toast-close" aria-label="Tutup">✕</button>
    `;

    // Click-to-dismiss
    toast.addEventListener('click', function (e) {
        if (e.target.closest('.toast-close') || e.target === this) {
            dismissToast(this);
        }
    });

    container.appendChild(toast);

    const duration = TOAST_DURATION[type] || 3500;
    toast.__dismissTimer = setTimeout(() => {
        if (toast.isConnected) dismissToast(toast);
    }, duration);
}

function dismissToast(toast) {
    if (toast.classList.contains('toast-exit')) return;
    toast.classList.add('toast-exit');
    setTimeout(() => {
        if (toast.isConnected) toast.remove();
    }, 300);
}

// Generic click-to-copy helper with toast feedback. Pages that need custom
// behaviour (e.g. the vouchers badge animation) may define their own copyCode
// which will override this one because their inline script loads afterwards.
// Accepts copyCode(text) or copyCode(element, text).
function copyCode(elOrText, maybeText) {
    var text = typeof elOrText === 'string' ? elOrText : (maybeText || (elOrText && elOrText.textContent) || '');
    text = (text || '').trim();
    if (!text) return;
    var ok = function () { showToast('"' + text + '" tersalin ke clipboard', 'success'); };
    var fail = function () { showToast('Gagal menyalin', 'error'); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(ok).catch(fail);
    } else {
        try {
            var ta = document.createElement('textarea');
            ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
            document.body.appendChild(ta); ta.select(); document.execCommand('copy');
            document.body.removeChild(ta); ok();
        } catch (e) { fail(); }
    }
}

// Escape HTML to prevent XSS — also escapes single quotes for safe use in HTML attributes
function escapeHtml(str) {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

// Escape string for JavaScript string literal context (e.g. onclick attribute values)
// This handles the case where HTML entity escaping is not sufficient because &#39;
// would be decoded back to ' by the HTML parser in attribute values.
function jsEscape(str) {
    return String(str)
        .replace(/\\/g, '\\\\')
        .replace(/'/g, "\\'")
        .replace(/\n/g, '\\n')
        .replace(/\r/g, '\\r');
}

// Localize UTC timestamp to browser timezone
function localizeUTC(utcStr) {
    if (!utcStr) return '—';
    try {
        let iso = String(utcStr).trim();
        if (iso.includes(' ') && !iso.includes('T')) iso = iso.replace(' ', 'T');
        // Append Z only if no timezone info present
        if (!iso.endsWith('Z') && !iso.includes('+') && !(/-\d{2}:\d{2}$/.test(iso))) iso += 'Z';
        const dt = new Date(iso);
        if (isNaN(dt.getTime())) return utcStr;
        const year = dt.getFullYear();
        const month = String(dt.getMonth() + 1).padStart(2, '0');
        const day = String(dt.getDate()).padStart(2, '0');
        const hours = String(dt.getHours()).padStart(2, '0');
        const mins = String(dt.getMinutes()).padStart(2, '0');
        return `${year}-${month}-${day} ${hours}:${mins}`;
    } catch (_) { return utcStr; }
}

// Dropdown Menu Toggle
let _menuToggleInitialized = false;

function initMenuToggle() {
    const menuToggle = document.getElementById('menuToggleBtn');
    const dropdownContent = document.getElementById('menuDropdownContent');
    if (menuToggle && dropdownContent) {
        menuToggle.onclick = (e) => {
            e.stopPropagation();
            dropdownContent.classList.toggle('show');
        };
        if (!_menuToggleInitialized) {
            document.addEventListener('click', (e) => {
                if (!menuToggle.contains(e.target) && !dropdownContent.contains(e.target)) {
                    dropdownContent.classList.remove('show');
                }
                const pengaturanDropdown = document.getElementById('pengaturanDropdown');
                if (pengaturanDropdown && !pengaturanDropdown.contains(e.target) && !e.target.closest('.nav-link')) {
                    pengaturanDropdown.classList.remove('show');
                }
            });
            document.addEventListener('keydown', function(e) {
                if (e.key === 'Escape') {
                    const openDropdown = document.querySelector('.topbar-dropdown-content.show');
                    if (openDropdown) openDropdown.classList.remove('show');
                }
            });
            _menuToggleInitialized = true;
        }
    }
}

// ===== New Utility Functions =====

// Debounce helper
function debounce(fn, delay = 300) {
    let timer;
    return function (...args) {
        clearTimeout(timer);
        timer = setTimeout(() => fn.apply(this, args), delay);
    };
}

// Password visibility toggle — SVG icons in/out, no FOUC
function togglePasswordVisibility(inputId, btnId) {
    const input = document.getElementById(inputId);
    const btn = document.getElementById(btnId);
    if (!input || !btn) return;

    btn.addEventListener('click', function () {
        const isPassword = input.type === 'password';
        input.type = isPassword ? 'text' : 'password';
        // Swap the SVG content
        const svg = btn.querySelector('svg');
        if (svg) {
            svg.classList.toggle('pw-eye', isPassword);
            svg.classList.toggle('pw-eye-off', !isPassword);
            // Swap paths: eye icon has 2 paths, eye-off has 5+1
            if (isPassword) {
                svg.innerHTML = '<path d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z"/><path d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/>';
            } else {
                svg.innerHTML = '<path d="M3.98 8.223A10.477 10.477 0 0 0 1.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0 1 12 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 0 1-4.293 5.774M6.228 6.228 3 3m3.228 3.228 3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 1 0-4.243-4.243m4.242 4.242L9.88 9.88"/>';
            }
        }
        btn.setAttribute('aria-label', isPassword ? 'Sembunyikan password' : 'Tampilkan password');
    });
}

// Custom confirm dialog (replaces native confirm())
function showConfirm(message, detailText = '', confirmLabel = 'Ya, Hapus', cancelLabel = 'Batal') {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.className = 'modal-overlay';
        overlay.style.display = 'flex';
        overlay.setAttribute('role', 'dialog');
        overlay.setAttribute('aria-modal', 'true');
        overlay.setAttribute('aria-label', message.replace(/<[^>]*>/g, ''));

        const card = document.createElement('div');
        card.className = 'modal-card';
        card.style.maxWidth = '420px';
        card.innerHTML = `
            <div class="confirm-dialog-body">
                <div class="confirm-dialog-icon"><svg class="icon-svg" style="width:32px;height:32px;" aria-hidden="true"><use href="#hi-exclamation"/></svg></div>
                <div class="confirm-dialog-msg">${escapeHtml(message)}</div>
                ${detailText ? `<div class="confirm-dialog-detail">${escapeHtml(detailText)}</div>` : ''}
            </div>
            <div class="confirm-dialog-footer">
                <button class="btn-sm" id="confirmCancelBtn" style="min-width:100px; justify-content:center; padding:10px 20px; font-size:13px;">${cancelLabel}</button>
                <button class="btn-sm btn-delete" id="confirmOkBtn" style="min-width:100px; justify-content:center; padding:10px 20px; font-size:13px;">${confirmLabel}</button>
            </div>
        `;
        overlay.appendChild(card);
        document.body.appendChild(overlay);

        // Focus trap
        const focusableEls = overlay.querySelectorAll('button:not([disabled])');
        const firstFocus = focusableEls[0];
        const lastFocus = focusableEls[focusableEls.length - 1];
        if (firstFocus) setTimeout(() => firstFocus.focus(), 50);

        const trapHandler = (e) => {
            if (e.key === 'Tab') {
                if (e.shiftKey && document.activeElement === firstFocus) {
                    e.preventDefault();
                    lastFocus.focus();
                } else if (!e.shiftKey && document.activeElement === lastFocus) {
                    e.preventDefault();
                    firstFocus.focus();
                }
            }
            if (e.key === 'Escape') {
                cleanup();
                resolve(false);
            }
        };
        overlay.addEventListener('keydown', trapHandler);

        const cleanup = () => {
            overlay.removeEventListener('keydown', trapHandler);
            overlay.remove();
        };

        document.getElementById('confirmOkBtn').addEventListener('click', () => { cleanup(); resolve(true); });
        document.getElementById('confirmCancelBtn').addEventListener('click', () => { cleanup(); resolve(false); });
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) { cleanup(); resolve(false); }
        });
    });
}

// Skeleton loading helpers
function showSkeleton(containerId, count = 3, type = 'card') {
    const container = document.getElementById(containerId);
    if (!container) return;
    container.innerHTML = '';
    for (let i = 0; i < count; i++) {
        const skeleton = document.createElement('div');
        if (type === 'card') {
            skeleton.className = 'skeleton-card';
            skeleton.innerHTML = `
                <div class="skeleton skeleton-block" style="width:40%;"></div>
                <div class="skeleton skeleton-block" style="width:80%;"></div>
                <div class="skeleton skeleton-block" style="width:60%;"></div>
            `;
        } else if (type === 'row') {
            skeleton.style.cssText = 'display:flex; gap:12px; padding:14px 0; border-bottom:1px solid rgba(255,255,255,0.04);';
            skeleton.innerHTML = `
                <div class="skeleton skeleton-circle"></div>
                <div style="flex:1;">
                    <div class="skeleton skeleton-block" style="width:50%;"></div>
                    <div class="skeleton skeleton-block" style="width:70%;"></div>
                </div>
            `;
        } else {
            skeleton.className = 'skeleton skeleton-block';
            skeleton.style.width = type === 'wide' ? '90%' : '60%';
        }
        container.appendChild(skeleton);
    }
}

// ===== Skeleton Loading for Dashboard =====
function showDashboardSkeletons() {
    showSkeleton('statsGrid', 4, 'card');
    showSkeleton('examTableBody', 5, 'row');
}

// Keyboard shortcuts
let shortcutsVisible = false;

function toggleShortcuts() {
    const hint = document.getElementById('shortcutsHint');
    if (!hint) return;
    shortcutsVisible = !shortcutsVisible;
    hint.classList.toggle('show', shortcutsVisible);
}

function initKeyboardShortcuts() {
    document.addEventListener('keydown', function (e) {
        // Don't trigger if user is typing in an input
        const tag = document.activeElement?.tagName || '';
        if (['INPUT', 'TEXTAREA', 'SELECT'].includes(tag)) return;

        switch (true) {
            case e.key === '/' && !e.ctrlKey && !e.metaKey:
                e.preventDefault();
                const searchInput = document.getElementById('searchExam');
                if (searchInput) { searchInput.focus(); searchInput.select(); }
                break;
            case e.key === '?' && e.shiftKey:
                e.preventDefault();
                toggleShortcuts();
                break;
        }

        // Ctrl+ shortcuts
        if (e.ctrlKey || e.metaKey) {
            switch (e.key) {
                case 'u':
                    e.preventDefault();
                    document.getElementById('examName')?.focus();
                    document.getElementById('examName')?.scrollIntoView({ behavior: 'smooth' });
                    break;
                case 'f':
                    e.preventDefault();
                    const search = document.getElementById('searchExam');
                    if (search) { search.focus(); search.select(); }
                    break;
            }
        }
    });
}

// ===== Auto-refresh Dashboard (AJAX-based, no full page reload) =====
let autoRefreshInterval = null;
let lastUserActivity = Date.now();

function onUserActivity() {
    lastUserActivity = Date.now();
}

async function refreshDashboardStats() {
    try {
        const resp = await apiFetch('/admin/api/stats');
        const data = await resp.json();
        if (data.success) {
            refreshUserInterface(data.data);
        }
    } catch (e) {
        // Silent fail — don't disrupt the user
        console.debug('Dashboard auto-refresh failed (expected on non-dashboard pages)');
        // Fallback: jika skeleton masih terlihat, reload untuk tampilkan data dari server
        const skeleton = document.querySelector('.skeleton-card');
        if (skeleton && document.getElementById('statsGrid')) {
            setTimeout(() => location.reload(), 3000);
        }
    }
}

function refreshUserInterface(stats) {
    // Update stats by card class (skip instansi card)
    var totalEl = document.querySelector('.stat-total .stat-value');
    if (totalEl) totalEl.textContent = stats.total ?? '0';
    var activeEl = document.querySelector('.stat-status .stat-value');
    if (activeEl) activeEl.textContent = stats.active ?? '0';
    // inactive is the second .stat-value in .stat-status
    var statusEls = document.querySelectorAll('.stat-status .stat-value');
    if (statusEls.length >= 2) statusEls[1].textContent = (stats.total - stats.active) ?? '0';
    var storageEl = document.querySelector('.stat-storage .stat-value');
    if (storageEl) storageEl.textContent = (stats.storage_mb ?? '0') + ' MB';
}

function startAutoRefresh(intervalSec = 120) {
    stopAutoRefresh();
    // Track user activity
    document.addEventListener('keydown', onUserActivity, true);
    document.addEventListener('mousedown', onUserActivity, true);
    document.addEventListener('touchstart', onUserActivity, true);
    document.addEventListener('scroll', onUserActivity, true);

    autoRefreshInterval = setInterval(() => {
        if (!document.hidden) {
            const activeTag = document.activeElement?.tagName || '';
            const isTyping = ['INPUT', 'TEXTAREA', 'SELECT'].includes(activeTag);
            const modalOpen = document.querySelector('.modal-overlay')?.style?.display === 'flex'
                || document.getElementById('questionsModal')?.style?.display === 'flex';
            const userActive = (Date.now() - lastUserActivity) < 30000;
            if (!isTyping && !modalOpen && !userActive) {
                refreshDashboardStats();
            }
        }
    }, intervalSec * 1000);
}

function stopAutoRefresh() {
    if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
    }
    document.removeEventListener('keydown', onUserActivity, true);
    document.removeEventListener('mousedown', onUserActivity, true);
    document.removeEventListener('touchstart', onUserActivity, true);
    document.removeEventListener('scroll', onUserActivity, true);
}

// ===== Password Strength Meter =====
function initPasswordStrengthMeter(inputId, meterId) {
    const input = document.getElementById(inputId);
    const meter = document.getElementById(meterId);
    if (!input || !meter) return;

    const updateStrength = debounce(function() {
        const val = input.value;
        let score = 0;

        // Length contributions
        if (val.length >= 8) score += 1;
        if (val.length >= 12) score += 1;

        // Character variety contributions
        if (/[a-z]/.test(val)) score += 1;
        if (/[A-Z]/.test(val)) score += 1;
        if (/[0-9]/.test(val)) score += 1;
        if (/[^a-zA-Z0-9]/.test(val)) score += 1;

        const labels = { weak: 'Lemah', medium: 'Sedang', strong: 'Kuat', 'very-strong': 'Sangat Kuat' };
        const colors = { weak: '#ef4444', medium: '#f59e0b', strong: '#22c55e', 'very-strong': '#16a34a' };

        if (val.length === 0) {
            meter.style.width = '0';
            meter.style.background = 'transparent';
            meter.textContent = '';
            return;
        }

        let strength, pct;
        if (score <= 2) { strength = 'weak'; pct = 25; }
        else if (score <= 3) { strength = 'medium'; pct = 50; }
        else if (score <= 4) { strength = 'strong'; pct = 75; }
        else { strength = 'very-strong'; pct = 100; }

        meter.style.width = pct + '%';
        meter.style.background = colors[strength];
        meter.textContent = labels[strength];
    }, 100);

    input.addEventListener('input', updateStrength);
}

// ===== Skip Link =====
function initSkipLink() {
    const skipLink = document.querySelector('.skip-link');
    if (!skipLink) return;
    skipLink.addEventListener('focus', function() { this.style.left = '8px'; });
    skipLink.addEventListener('blur', function() { this.style.left = '-9999px'; });
    // Skip link hanya bereaksi terhadap keyboard focus, bukan mouse hover (WCAG 2.4.1)
    // mouseenter/mouseleave tidak ditambahkan untuk menghindari skip link muncul
    // saat mouse melewati area atas halaman, yang mengganggu pengguna visual.
}

// ===== Init All =====
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        initMenuToggle();
        initSkipLink();
        // Init password visibility toggles
        togglePasswordVisibility('currentPassword', 'toggleCurPassword');
        togglePasswordVisibility('newPassword', 'toggleNewPassword');
        togglePasswordVisibility('confirmNewPassword', 'toggleConfirmPassword');
        // Data stats di-render server, langsung pakai API untuk refresh
        if (document.getElementById('statsGrid')) {
            refreshDashboardStats();
        }
    });
} else {
    initMenuToggle();
    initSkipLink();
    togglePasswordVisibility('currentPassword', 'toggleCurPassword');
    togglePasswordVisibility('newPassword', 'toggleNewPassword');
    togglePasswordVisibility('confirmNewPassword', 'toggleConfirmPassword');
    if (document.getElementById('statsGrid')) {
        refreshDashboardStats();
    }
}

// Keyboard accessibility: let elements promoted to role="button" (div/span/strong
// with an onclick) be activated with Enter/Space like a native button. Native
// <button>/<a> already handle this, so they are excluded.
document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return;
    var el = e.target;
    if (!el || el.getAttribute('role') !== 'button') return;
    var tag = el.tagName;
    if (tag === 'BUTTON' || tag === 'A' || tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    // Skip elements that already define their own keyboard handling, otherwise
    // both their inline onkeydown and this handler would fire (double action).
    if (el.hasAttribute('onkeydown')) return;
    e.preventDefault();
    el.click();
});

// ===== Global modal manager =====
// Every modal in the app opens by toggling inline display (or a .show class)
// on a .modal-overlay / .modal-backdrop element. Rather than rewriting each
// call site, this manager observes those state changes and layers on the
// behavior dialogs are expected to have: body scroll-lock while any modal is
// open, Escape-to-close, a Tab focus trap inside the top-most dialog, initial
// focus into the dialog, and focus restore to the trigger on close.
(function () {
    const OVERLAY_SELECTOR = '.modal-overlay, .modal-backdrop';
    let openSet = new Set();
    let lastFocused = null;

    function isOpen(el) {
        return document.contains(el) && getComputedStyle(el).display !== 'none';
    }

    function openOverlays() {
        return Array.from(document.querySelectorAll(OVERLAY_SELECTOR)).filter(isOpen);
    }

    function focusables(overlay) {
        return Array.from(overlay.querySelectorAll(
            'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        )).filter(el => el.offsetParent !== null);
    }

    function syncState() {
        const open = openOverlays();
        const openNow = new Set(open);

        for (const overlay of open) {
            if (!openSet.has(overlay)) {
                // First modal of a stack: remember where focus came from.
                if (openSet.size === 0 && !lastFocused) lastFocused = document.activeElement;
                if (!overlay.contains(document.activeElement)) {
                    const f = focusables(overlay);
                    if (f.length) setTimeout(() => {
                        if (isOpen(overlay) && !overlay.contains(document.activeElement)) f[0].focus();
                    }, 40);
                }
            }
        }

        if (open.length > 0) {
            document.body.classList.add('modal-open');
        } else {
            document.body.classList.remove('modal-open');
            if (openSet.size > 0 && lastFocused && document.contains(lastFocused) &&
                typeof lastFocused.focus === 'function') {
                try { lastFocused.focus(); } catch (_) { /* detached */ }
            }
            if (openSet.size > 0) lastFocused = null;
        }
        openSet = openNow;
    }

    new MutationObserver(syncState).observe(document.documentElement, {
        subtree: true,
        childList: true,
        attributes: true,
        attributeFilter: ['style', 'class']
    });
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', syncState);
    } else {
        syncState();
    }

    // Force-close fallback: dispatch a click on the backdrop first so any
    // page-specific close routine (or showConfirm's promise resolution) runs;
    // only if the overlay is still open afterwards, hide it directly. Id-less
    // overlays are left alone at that point — promise-based dialogs manage
    // their own lifecycle and must not be removed out from under their caller.
    function forceClose(overlay) {
        if (!isOpen(overlay)) return;
        overlay.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
        if (isOpen(overlay)) {
            if (overlay.classList.contains('show')) overlay.classList.remove('show');
            else if (overlay.id) overlay.style.display = 'none';
        }
    }

    // Backdrop click: pages with their own handlers run first (this listener
    // defers via setTimeout); anything still open afterwards gets hidden.
    document.addEventListener('click', function (e) {
        const el = e.target;
        if (!el.classList || !(el.classList.contains('modal-overlay') || el.classList.contains('modal-backdrop'))) return;
        setTimeout(() => {
            if (isOpen(el)) {
                if (el.classList.contains('show')) el.classList.remove('show');
                else if (el.id) el.style.display = 'none';
            }
        }, 0);
    });

    document.addEventListener('keydown', function (e) {
        if (e.defaultPrevented) return;
        const open = openOverlays();
        if (!open.length) return;
        const top = open[open.length - 1];

        if (e.key === 'Escape') {
            // An open dropdown takes priority: let its own Escape handler
            // close it without also dismissing the modal underneath.
            if (document.querySelector('.exam-action-dropdown-content.show, .topbar-dropdown-content.show')) return;
            forceClose(top);
            return;
        }

        if (e.key === 'Tab') {
            const f = focusables(top);
            if (!f.length) return;
            const first = f[0];
            const last = f[f.length - 1];
            if (!top.contains(document.activeElement)) {
                e.preventDefault();
                first.focus();
            } else if (e.shiftKey && document.activeElement === first) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && document.activeElement === last) {
                e.preventDefault();
                first.focus();
            }
        }
    });
})();
