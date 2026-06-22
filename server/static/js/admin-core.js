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

// Toast notification
function showToast(message, type = 'success') {
    const container = document.getElementById('toastContainer');
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transform = 'translateX(40px)';
        toast.style.transition = 'all 0.3s';
        setTimeout(() => toast.remove(), 300);
    }, 3500);
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
        const year = dt.getFullYear();
        const month = String(dt.getMonth() + 1).padStart(2, '0');
        const day = String(dt.getDate()).padStart(2, '0');
        const hours = String(dt.getHours()).padStart(2, '0');
        const mins = String(dt.getMinutes()).padStart(2, '0');
        return `${year}-${month}-${day} ${hours}:${mins}`;
    } catch (_) { return utcStr; }
}

// Dropdown Menu Toggle
function initMenuToggle() {
    const menuToggle = document.getElementById('menuToggleBtn');
    const dropdownContent = document.getElementById('menuDropdownContent');
    if (menuToggle && dropdownContent) {
        menuToggle.onclick = (e) => {
            e.stopPropagation();
            dropdownContent.classList.toggle('show');
        };
        document.addEventListener('click', (e) => {
            if (!menuToggle.contains(e.target) && !dropdownContent.contains(e.target)) {
                dropdownContent.classList.remove('show');
            }
        });
    }
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMenuToggle);
} else {
    initMenuToggle();
}
