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
    setTimeout(() => {
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

// Escape HTML to prevent XSS
function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// Localize UTC timestamp to browser timezone
function localizeUTC(utcStr) {
    if (!utcStr) return '—';
    try {
        let iso = utcStr.trim();
        if (iso.includes(' ')) iso = iso.replace(' ', 'T');
        if (!iso.endsWith('Z') && !iso.includes('+')) iso += 'Z';
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

        const card = document.createElement('div');
        card.className = 'modal-card';
        card.style.maxWidth = '420px';
        card.innerHTML = `
            <div class="confirm-dialog-body">
                <div class="confirm-dialog-icon">⚠️</div>
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

        const cleanup = () => overlay.remove();

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

// ===== Auto-refresh Dashboard =====
let autoRefreshInterval = null;
let lastUserActivity = Date.now();

function onUserActivity() {
    lastUserActivity = Date.now();
}

function startAutoRefresh(intervalSec = 120) {
    stopAutoRefresh();
    // Track user activity
    document.addEventListener('keydown', onUserActivity, true);
    document.addEventListener('mousedown', onUserActivity, true);
    document.addEventListener('touchstart', onUserActivity, true);
    document.addEventListener('scroll', onUserActivity, true);

    autoRefreshInterval = setInterval(() => {
        // Only refresh if page is visible, not typing, no modal open, and user inactive for 30s+
        if (!document.hidden) {
            const activeTag = document.activeElement?.tagName || '';
            const isTyping = ['INPUT', 'TEXTAREA', 'SELECT'].includes(activeTag);
            const modalOpen = document.querySelector('.modal-overlay')?.style?.display === 'flex'
                || document.getElementById('questionsModal')?.style?.display === 'flex';
            const userActive = (Date.now() - lastUserActivity) < 30000;
            if (!isTyping && !modalOpen && !userActive) {
                location.reload();
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

// ===== Init All =====
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        initMenuToggle();
    });
} else {
    initMenuToggle();
}
