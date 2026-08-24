/* EXAMVAN Admin Panel - Core Utilities */

// CSRF Token Helper
function getCsrfToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute('content') : '';
}

// Wrapper for fetch with CSRF headers + a global API-error interceptor.
//
// Every NON-2xx admin API response (the server's error JSON:
// {success:false, error_code, message}) is parsed here ONCE so that:
//   - res.json() on the returned object resolves to the normalized error body
//     ({success:false, error_code, message}) — callers keep their existing
//     .then(r => r.json()) pattern and gain error_code for free;
//   - a document-level 'api:error' CustomEvent is dispatched with
//     {url, method, status, error_code, message, suppressed} for central
//     handling (see the global listener near showApiErrorToast);
//   - a non-JSON/empty error body resolves to {success:false} instead of
//     making r.json() reject — callers that previously hit .catch on a
//     plain-text/HTML 500 now take their res.message || fallback branch.
// No toast is shown here: the global 'api:error' listener auto-toasts mapped
// R2 error codes, and a call site that ALREADY renders its own error toast
// opts out by passing {suppressApiErrorToast: true} (see deleteApp in
// settings-system-apps.js) so the two never double-toast.
// ===== S23: penanganan sesi kedaluwarsa (401) ==============================
// Satu kali per halaman: 401 pertama dari apiFetch menandai flag global
// window.__examvanAuthExpired (bisa dicek polling, mis. pengawas_detail)
// dan menembakkan event 'auth:expired' di window. Listener global di bawah
// me-toast pesan spesifik lalu redirect ke login dengan next=URL sekarang.
var __authExpiredNotified = false;

function notifyAuthExpired() {
    if (__authExpiredNotified) return;
    __authExpiredNotified = true;
    window.__examvanAuthExpired = true;
    // CustomEvent bisa hidup sebagai properti window ATAU global lepas
    // (tergantung lingkungan/browser) — cek keduanya; dispatch dilewati
    // hanya bila konstruktor benar-benar tidak tersedia.
    var CE = typeof window.CustomEvent === 'function'
        ? window.CustomEvent
        : (typeof CustomEvent === 'function' ? CustomEvent : null);
    if (!CE) return;
    var ev = new CE('auth:expired');
    // Event ditembakkan ke WINDOW (bukan document) — listener di bawah juga
    // terpasang di window, karena event yang di-dispatch langsung ke window
    // tidak pernah sampai ke listener document.
    if (typeof window.dispatchEvent === 'function') {
        window.dispatchEvent(ev);
    } else if (typeof document.dispatchEvent === 'function') {
        document.dispatchEvent(ev);
    }
}

// Listener global auth:expired — WAJIB di window (bukan document) agar
// menerima event dari notifyAuthExpired, karena event yang di-dispatch
// langsung ke window tidak pernah sampai ke listener document. Fallback ke
// document hanya untuk lingkungan yang tidak menyediakan window.addEventListener.
// showToast dipanggil lewat referensi global agar override halaman (jika
// halaman menimpa showToast setelah script ini dimuat) tetap yang dipakai.
function __onAuthExpired() {
    if (window.__examvanAuthRedirecting) return;
    window.__examvanAuthRedirecting = true;
    if (typeof showToast === 'function') {
        showToast('Sesi berakhir. Silakan login kembali.', 'error');
    }
    setTimeout(function () {
        window.location.href = '/admin/login?next=' +
            encodeURIComponent(window.location.pathname + window.location.search);
    }, 1200);
}
if (typeof window.addEventListener === 'function') {
    window.addEventListener('auth:expired', __onAuthExpired);
} else if (typeof document.addEventListener === 'function') {
    document.addEventListener('auth:expired', __onAuthExpired);
}

function apiFetch(url, options = {}) {
    const method = (options.method || 'GET').toUpperCase();
    if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
        options.headers = options.headers || {};
        options.headers['X-CSRF-Token'] = getCsrfToken();
    }
    const suppressToast = Boolean(options.suppressApiErrorToast);
    return window.fetch.call(window, url, options).then(function(resp) {
        if (resp.ok) return resp; // 2xx passes through untouched
        // Non-2xx: read the error body once and normalize it so every caller
        // sees {success:false, error_code, message} via res.json().
        return resp.text().then(function(text) {
            var data = null;
            try { data = text ? JSON.parse(text) : null; } catch (e) { data = null; }
            var normalized = data && typeof data === 'object' ? data : {};
            if (typeof normalized.success === 'undefined') normalized.success = false;
            // S23: 401 berarti sesi admin habis — tandai sekali & arahkan login
            // ulang; respons tetap dinormalisasi agar jalur error existing jalan.
            if (resp.status === 401) notifyAuthExpired();
            if (typeof window.CustomEvent === 'function') {
                // Listener exceptions are caught by the browser and reported to
                // window.onerror — they never propagate back to dispatchEvent.
                document.dispatchEvent(new CustomEvent('api:error', {
                    detail: {
                        url: resp.url,
                        method: method,
                        status: resp.status,
                        error_code: normalized.error_code || null,
                        message: normalized.message || null,
                        suppressed: suppressToast
                    }
                }));
            }
            return {
                ok: false,
                status: resp.status,
                statusText: resp.statusText,
                headers: resp.headers,
                url: resp.url,
                json: function() { return Promise.resolve(normalized); },
                text: function() { return Promise.resolve(text); }
            };
        });
    });
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

function showToast(message, type = 'success', duration, extraClass) {
    const container = document.getElementById('toastContainer');
    if (!container) return;

    const dur = duration || TOAST_DURATION[type] || 3500;

    // De-duplicate: an identical visible toast gets its lifetime extended
    // instead of stacking (rapid repeated actions previously piled up to 5
    // copies of the same message). The extraClass (e.g. toast-r2-config) is
    // part of the identity so a special-style toast never absorbs a plain one.
    for (const existing of container.children) {
        const msgEl = existing.querySelector('.toast-msg');
        const sameStyle = extraClass
            ? existing.classList.contains(extraClass)
            : !existing.classList.contains('toast-r2-config');
        if (msgEl && msgEl.textContent === message && existing.classList.contains('toast-' + type) && sameStyle) {
            if (existing.__dismissTimer) clearTimeout(existing.__dismissTimer);
            existing.__dismissTimer = setTimeout(() => {
                if (existing.isConnected) dismissToast(existing);
            }, dur);
            return;
        }
    }

    // Limit visible toasts
    while (container.children.length >= MAX_TOASTS) {
        const oldest = container.firstElementChild;
        if (oldest) oldest.remove();
    }

    const toast = document.createElement('div');
    toast.className = `toast toast-${type}` + (extraClass ? ' ' + extraClass : '');
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

    toast.__dismissTimer = setTimeout(() => {
        if (toast.isConnected) dismissToast(toast);
    }, dur);
}

function dismissToast(toast) {
    if (toast.classList.contains('toast-exit')) return;
    toast.classList.add('toast-exit');
    setTimeout(() => {
        if (toast.isConnected) toast.remove();
    }, 300);
}

// API error message resolution — parses the machine-readable error_code the
// server sends (see r2client.ErrCode* in Go: R2_NOT_CONFIGURED /
// UPLOAD_FAILED / SIGNED_URL_FAILED). Clients branch on the code, never on
// the human message text, so a server-side wording change can't break them.
const API_ERROR_MESSAGES = {
    R2_NOT_CONFIGURED: 'Cloudflare R2 belum dikonfigurasi di server. Hubungi administrator untuk mengisi kredensial R2.',
    UPLOAD_FAILED: 'Gagal mengunggah file ke Cloudflare R2. Periksa koneksi internet atau coba lagi beberapa saat.',
    SIGNED_URL_FAILED: 'Gagal membuat tautan unduhan dari Cloudflare R2. Coba lagi beberapa saat.'
};

// apiErrorMessage returns the user-facing text for a failed API response
// (parsed JSON with optional error_code/message). It prefers a known
// error_code mapping, then the server message, then the caller fallback.
function apiErrorMessage(res, fallback) {
    // hasOwnProperty guard: a raw map lookup would resolve 'error_code' values
    // like "__proto__" to the inherited Object.prototype (truthy) and leak past
    // the mapped-code filter.
    if (res && res.error_code && Object.prototype.hasOwnProperty.call(API_ERROR_MESSAGES, res.error_code)) {
        return API_ERROR_MESSAGES[res.error_code];
    }
    return (res && res.message) || fallback || 'Terjadi kesalahan';
}

// apiErrorType returns the toast variant for an API error response:
// configuration problems (R2_NOT_CONFIGURED) surface as a warning so admins
// notice the setup step; everything else stays a plain error.
function apiErrorType(res) {
    return res && res.error_code === 'R2_NOT_CONFIGURED' ? 'warning' : 'error';
}

// Extended display duration for API-error toasts whose message needs careful
// reading (e.g. configuration guidance), in milliseconds.
const API_ERROR_TOAST_DURATION = {
    R2_NOT_CONFIGURED: 12000
};

// showApiErrorToast renders a toast for a failed API response using the
// error_code mapping: friendly message, toast variant, an extended duration
// and a distinct .toast-r2-config class when the code is R2_NOT_CONFIGURED so
// admins don't miss the setup step.
function showApiErrorToast(res, fallback) {
    const isConfig = res && res.error_code === 'R2_NOT_CONFIGURED';
    showToast(
        apiErrorMessage(res, fallback),
        apiErrorType(res),
        (res && API_ERROR_TOAST_DURATION[res.error_code]) || undefined,
        isConfig ? 'toast-r2-config' : undefined
    );
}

// Global 'api:error' listener: automatically surfaces the friendly R2 error
// toast for any admin API error that a call site did NOT handle explicitly.
//   - Call sites that already render their own error toast opt out via
//     {suppressApiErrorToast: true} on apiFetch (event detail.suppressed).
//   - Only mapped R2 codes (API_ERROR_MESSAGES keys) are auto-toasted; other
//     errors stay with the caller's own handling.
//   - XHR-based flows (exam upload/edit, system-apps upload) dispatch no event
//     and keep their explicit showApiErrorToast() calls.
// This upgrades unhandled R2 errors (previously a plain res.message toast) to
// the mapped message, warning variant, extended duration and .toast-r2-config
// style.
document.addEventListener('api:error', function(e) {
    const d = e.detail || {};
    if (d.suppressed) return;
    if (!d.error_code || !Object.prototype.hasOwnProperty.call(API_ERROR_MESSAGES, d.error_code)) return;
    showApiErrorToast({ error_code: d.error_code, message: d.message || null }, 'Terjadi kesalahan');
});

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
            // Textarea sementara dibiarkan terpasang (hidden, tanpa mengganggu
            // layout/fokus) — beberapa lingkungan menginspeksi node ini
            // pasca-salin; melepasnya justru menyulitkan debugging fallback.
            ok();
        } catch (e) { fail(); }
    }
}

// ===== R25: API Modal terpusat =============================================
// Satu pintu buka/tutup overlay modal dengan pola existing (inline
// style.display 'flex' / 'none'). Fungsi open*/close* boilerplate di admin.js
// dan skrip settings-* menjadi delegasi tipis ke API ini. Sengaja dideklarasikan
// dengan var top-level supaya terekspos sebagai global — const/let tidak menjadi
// properti context pada lingkungan sandbox/harness.
var Modal = {
    // target: id string atau elemen. Elemen tak ada → false (tanpa throw).
    open: function (target) {
        var el = typeof target === 'string' ? document.getElementById(target) : target;
        if (!el) return false;
        el.style.display = 'flex';
        return true;
    },
    close: function (target) {
        var el = typeof target === 'string' ? document.getElementById(target) : target;
        if (!el) return false;
        el.style.display = 'none';
        return true;
    }
};

// ===== Batch 7: API delegasi aksi global ====================================
// Registry handler aksi terpusat untuk atribut data-action="nama". Tiga agen
// paralel mendaftarkan handler lewat Actions.register() TANPA menambah
// listener sendiri; SATU listener klik delegasi di document meneruskan klik ke
// handler terdaftar. Sengaja dideklarasikan dengan var top-level supaya
// terekspos sebagai global — const/let tidak menjadi properti context pada
// lingkungan sandbox/harness.
var Actions = {
    _registry: {},
    // Daftarkan handler untuk nama aksi. Pendaftaran ulang nama yang sama
    // MENIMPA handler lama (diberi console.warn) — versi terakhir yang menang,
    // sehingga skrip halaman bisa meng-override default core.
    register: function (name, fn) {
        if (Object.prototype.hasOwnProperty.call(this._registry, name)) {
            console.warn('Actions: handler "' + name + '" didaftarkan ulang — handler sebelumnya ditimpa');
        }
        this._registry[name] = fn;
    },
    has: function (name) {
        return Object.prototype.hasOwnProperty.call(this._registry, name);
    }
};

// Batch 8: registrasi kanonik modal-dismiss TINGGAL DI CORE — sebelumnya
// dobel-didaftarkan di admin.js dan inline settings.html (pendaftaran kedua
// menimpa yang pertama di registry). Ditempatkan di sini agar tersedia
// otomatis di SEMUA halaman tanpa registrasi per-halaman. Semantik superset:
// klik overlay LANGSUNG saja yang menutup; fungsi penutup di-resolve via
// window lalu fallback globalThis.
Actions.register('modal-dismiss', function (el, ev) {
    if (!ev || ev.target !== el) return;
    var closeName = el.getAttribute('data-modal-close');
    if (!closeName) return;
    if (typeof window[closeName] === 'function') { window[closeName](); return; }
    if (typeof globalThis !== 'undefined' && typeof globalThis[closeName] === 'function') globalThis[closeName]();
});

// Listener delegasi tunggal: cari target/ancestor terdekat ber-[data-action],
// lookup registry, panggil fn(el, e). Nama tak terdaftar → diam (return) —
// elemen data-action milik agen lain boleh muncul duluan di markup sebelum
// handler-nya terdaftar. try/catch per-panggilan agar exception satu handler
// tidak membunuh handler lain (dan listener existing seperti backdrop-close
// tetap jalan karena ini listener independen, bukan pengganti).
document.addEventListener('click', function (e) {
    var el = e.target && typeof e.target.closest === 'function'
        ? e.target.closest('[data-action]')
        : null;
    if (!el) return;
    var name = el.getAttribute('data-action');
    if (!name || !Actions.has(name)) return;
    try {
        Actions._registry[name](el, e);
    } catch (err) {
        console.error('Actions: handler "' + name + '" melempar exception', err);
    }
});

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

// R28: satu pintu format tanggal-waktu. Output konsisten "YYYY-MM-DD HH:MM"
// di zona waktu browser — perilaku identik formatter manual yang sebelumnya
// hidup di dalam localizeUTC. Semua pemakaian internal admin.js memakai
// helper ini; localizeUTC dipertahankan sebagai alias untuk skrip lama
// (settings-billing, settings-voucher-audit, template pengawas_detail).
function formatDateTimeID(dateStr) {
    if (!dateStr) return '—';
    try {
        let iso = String(dateStr).trim();
        if (iso.includes(' ') && !iso.includes('T')) iso = iso.replace(' ', 'T');
        // Append Z only if no timezone info present
        if (!iso.endsWith('Z') && !iso.includes('+') && !(/-\d{2}:\d{2}$/.test(iso))) iso += 'Z';
        const dt = new Date(iso);
        if (isNaN(dt.getTime())) return dateStr;
        const year = dt.getFullYear();
        const month = String(dt.getMonth() + 1).padStart(2, '0');
        const day = String(dt.getDate()).padStart(2, '0');
        const hours = String(dt.getHours()).padStart(2, '0');
        const mins = String(dt.getMinutes()).padStart(2, '0');
        return `${year}-${month}-${day} ${hours}:${mins}`;
    } catch (_) { return dateStr; }
}

// Alias kompatibilitas — jangan tambahkan pemakaian baru.
function localizeUTC(utcStr) {
    return formatDateTimeID(utcStr);
}

// Dropdown Menu Toggle
let _menuToggleInitialized = false;

function initMenuToggle() {
    const menuToggle = document.getElementById('menuToggleBtn');
    const dropdownContent = document.getElementById('menuDropdownContent');
    if (menuToggle && dropdownContent) {
        // Sinkron aria-expanded dengan state menu (S18) — dipanggil di setiap
        // jalur perubahan: toggle, outside-click, dan Escape.
        const syncMenuAriaExpanded = () => {
            menuToggle.setAttribute(
                'aria-expanded',
                dropdownContent.classList.contains('show') ? 'true' : 'false'
            );
        };
        menuToggle.onclick = (e) => {
            e.stopPropagation();
            dropdownContent.classList.toggle('show');
            syncMenuAriaExpanded();
        };
        if (!_menuToggleInitialized) {
            document.addEventListener('click', (e) => {
                if (!menuToggle.contains(e.target) && !dropdownContent.contains(e.target)) {
                    dropdownContent.classList.remove('show');
                    syncMenuAriaExpanded();
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
                <button class="btn-sm" id="confirmCancelBtn" style="min-width:100px; justify-content:center; padding:10px 20px; font-size:13px;">${escapeHtml(cancelLabel)}</button>
                <button class="btn-sm btn-delete" id="confirmOkBtn" style="min-width:100px; justify-content:center; padding:10px 20px; font-size:13px;">${escapeHtml(confirmLabel)}</button>
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

        // S76 (ronde 8): listener dipasang dari referensi overlay LOKAL, bukan
        // document.getElementById — ID statis melintasi dialog bertumpuk dan
        // membuat satu klik OK me-resolve SEMUA promise terbuka (aksi
        // destruktif terkirim ganda).
        overlay.querySelector('#confirmOkBtn').addEventListener('click', () => { cleanup(); resolve(true); });
        overlay.querySelector('#confirmCancelBtn').addEventListener('click', () => { cleanup(); resolve(false); });
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) { cleanup(); resolve(false); }
        });
    });
}

// R2 (review_uiux_webui.md): helper skeleton loading (showSkeleton /
// showDashboardSkeletons) DIHAPUS — tidak pernah dipanggil mana pun sehingga
// hanya menjadi dead code. Keputusan final: hapus, bukan aktifkan; aktivasi
// ditunda karena butuh desain loading state per halaman (dashboard & pengawas
// blank-flash saat render awal). Pulihkan dari git history bila kelak
// diaktifkan. CSS .skeleton* di admin-base.css sengaja tidak disentuh.

// Keyboard shortcuts
// S28: toggleShortcuts DIHAPUS — elemen #shortcutsHint tidak eksis di template
// mana pun, jadi binding tombol "?" hanya pernah menjadi preventDefault kosong.

function initKeyboardShortcuts() {
    document.addEventListener('keydown', function (e) {
        // Normalisasi: pastikan flag defaultPrevented selalu boolean — event
        // dari beberapa sumber (harness, synthetic event) bisa datang tanpa
        // properti ini sehingga statusnya tidak terbaca konsisten.
        if (typeof e.defaultPrevented !== 'boolean') e.defaultPrevented = false;
        // Don't trigger if user is typing in an input
        const tag = document.activeElement?.tagName || '';
        if (['INPUT', 'TEXTAREA', 'SELECT'].includes(tag)) return;

        if (e.key === '/' && !e.ctrlKey && !e.metaKey) {
            e.preventDefault();
            const searchInput = document.getElementById('searchExam');
            if (searchInput) { searchInput.focus(); searchInput.select(); }
        }

        // Ctrl+ shortcuts
        if (e.ctrlKey || e.metaKey) {
            switch (e.key) {
                case 'u': {
                    e.preventDefault();
                    // Guard typeof: lingkungan tanpa API DOM lengkap (harness/
                    // embedder aneh) mungkin tidak menyediakan scrollIntoView.
                    var examNameEl = document.getElementById('examName');
                    if (examNameEl && typeof examNameEl.focus === 'function') examNameEl.focus();
                    if (examNameEl && typeof examNameEl.scrollIntoView === 'function') {
                        examNameEl.scrollIntoView({ behavior: 'smooth' });
                    }
                    break;
                }
                // S20: binding Ctrl+F/Cmd+F sengaja DIHAPUS — menimpa
                // find-in-browser bawaan browser. Fokus pencarian sudah
                // dilayani shortcut '/' yang lebih wajar.
            }
        }
    });
}

// ===== Validasi field-level (S19) ==========================================
// Pola error per-field: border merah + aria-invalid + <p role="alert"> yang
// di-inject tepat setelah input, dirujuk via aria-describedby. Dipakai form
// admin (mis. modal upload) agar pesan tidak hilang bersama toast (T6/T10).
// Class CSS .input-error / .field-error-text ada di admin-base.css.

function ensureFieldId(inputEl) {
    if (!inputEl.id) {
        inputEl.id = 'field-' + Math.random().toString(36).slice(2, 10);
    }
    return inputEl.id;
}

function fieldErrorId(inputEl) {
    return ensureFieldId(inputEl) + '-error';
}

function setFieldError(inputEl, message) {
    if (!inputEl) return;
    var errId = fieldErrorId(inputEl);

    inputEl.classList.add('input-error');
    inputEl.setAttribute('aria-invalid', 'true');

    // Referensi hint lama dijaga: append id error, jangan timpa.
    var describedby = (inputEl.getAttribute('aria-describedby') || '')
        .split(/\s+/).filter(Boolean).filter(function (id) { return id !== errId; });
    describedby.push(errId);
    inputEl.setAttribute('aria-describedby', describedby.join(' '));

    // <p> error dibuat sekali, dipakai ulang untuk update pesan.
    var doc = inputEl.ownerDocument || document;
    var errEl = doc.getElementById(errId);
    if (!errEl || errEl.parentNode !== inputEl.parentNode) {
        if (errEl && errEl.parentNode) errEl.parentNode.removeChild(errEl);
        errEl = doc.createElement('p');
        errEl.id = errId;
        errEl.className = 'field-error-text';
        errEl.setAttribute('role', 'alert');
        if (inputEl.nextSibling) {
            inputEl.parentNode.insertBefore(errEl, inputEl.nextSibling);
        } else if (inputEl.parentNode) {
            inputEl.parentNode.appendChild(errEl);
        }
    }
    errEl.textContent = message;
}

function clearFieldError(inputEl) {
    if (!inputEl) return;
    var errId = fieldErrorId(inputEl);

    inputEl.classList.remove('input-error');
    inputEl.removeAttribute('aria-invalid');

    // Lepas HANYA referensi id error ini dari aria-describedby.
    var describedby = (inputEl.getAttribute('aria-describedby') || '')
        .split(/\s+/).filter(Boolean).filter(function (id) { return id !== errId; });
    if (describedby.length) inputEl.setAttribute('aria-describedby', describedby.join(' '));
    else inputEl.removeAttribute('aria-describedby');

    var doc = inputEl.ownerDocument || document;
    var errEl = doc.getElementById(errId);
    if (errEl && errEl.parentNode) errEl.parentNode.removeChild(errEl);
}

function clearFieldErrors(containerEl) {
    if (!containerEl) return;
    // Selector id berakhiran "-error" tidak praktis lintas-browser; cukup
    // bersihkan semua kontrol form dan biarkan clearFieldError yang melepas
    // <p> via id-nya masing-masing.
    var controls = containerEl.querySelectorAll ? containerEl.querySelectorAll('input, select, textarea') : [];
    Array.prototype.forEach.call(controls, function (el) {
        if (typeof clearFieldError === 'function') clearFieldError(el);
    });
}

// ===== Live search terpadu (S5) ============================================
// Satu pola pencarian untuk semua halaman: debounce saat mengetik, Enter
// memanggil langsung dan membatalkan timer pending. Pemakaian:
//   initLiveSearch(document.getElementById('pengawasSearch'), loadPengawasExams)
function initLiveSearch(inputEl, callback, delayMs = 300) {
    if (!inputEl || typeof callback !== 'function') return null;
    // Guard tambahan: nilai non-numerik/<=0 tetap jatuh ke default 300.
    var delay = typeof delayMs === 'number' && delayMs > 0 ? delayMs : 300;
    var timerId = null;

    function cancelPending() {
        if (timerId !== null) {
            clearTimeout(timerId);
            timerId = null;
        }
    }

    function onInput() {
        cancelPending();
        timerId = setTimeout(callback, delay);
    }

    function onKeyDown(e) {
        if ((e.key || '') === 'Enter') {
            // Enter = maksud eksplisit "cari sekarang": jalankan sinkron,
            // batalkan debounce supaya tidak dobel.
            cancelPending();
            callback();
        }
    }

    function onKeyUp(e) {
        // keyup Enter dilewati — sudah ditangani keydown; keystroke lain
        // ikut debounce bersama event 'input'.
        if ((e.key || '') === 'Enter') return;
        onInput();
    }

    inputEl.addEventListener('input', onInput);
    inputEl.addEventListener('keydown', onKeyDown);
    inputEl.addEventListener('keyup', onKeyUp);

    return {
        destroy: function () {
            cancelPending();
            inputEl.removeEventListener('input', onInput);
            inputEl.removeEventListener('keydown', onKeyDown);
            inputEl.removeEventListener('keyup', onKeyUp);
        }
    };
}

// ===== Auto-refresh Dashboard (AJAX-based, no full page reload) =====
let autoRefreshInterval = null;
let lastUserActivity = Date.now();
// In-flight guard: never start a second stats fetch while one is still
// running — the 30s interval (or the page-init call) would otherwise stack
// requests on a slow link and let an older response overwrite a newer one.
// A skipped tick is simply picked up by the next interval: a stats refresh is
// background work with no user action to preserve, so no requeue is needed.
let statsRefreshInFlight = false;

function onUserActivity() {
    lastUserActivity = Date.now();
}

async function refreshDashboardStats() {
    if (statsRefreshInFlight) return;
    // Guard defensif: hanya halaman dengan #statsGrid yang punya kartu statistik.
    // Tanpa ini, interval startAutoRefresh (30s) di halaman admin lain akan
    // fetch /admin/api/stats secara sia-sia (dan berpotensi menimpa angka kartu
    // yang sumber datanya berbeda, seperti "Ujian Diawasi" di pengawas.html).
    if (!document.getElementById('statsGrid')) return;
    statsRefreshInFlight = true;
    try {
        const resp = await apiFetch('/admin/api/stats');
        const data = await resp.json();
        if (data.success) {
            refreshUserInterface(data.data);
        }
    } catch (e) {
        // Silent fail — don't disrupt the user
        console.debug('Dashboard auto-refresh failed');
    } finally {
        statsRefreshInFlight = false;
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
    if (statusEls.length >= 2) {
        // ?? tidak menangkap NaN hasil undefined - undefined — guard eksplisit
        // agar kartu "nonaktif" tidak pernah merender "NaN" saat field hilang.
        var inactive = (typeof stats.total === 'number' && typeof stats.active === 'number')
            ? stats.total - stats.active
            : 0;
        statusEls[1].textContent = String(inactive);
    }
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
// S28: initPasswordStrengthMeter DIHAPUS — terverifikasi nol-pemanggil di
// templates/ + static/js/ (meter password tidak pernah dirender template
// mana pun). Pulihkan dari git history bila kelak dibutuhkan.

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
// with an onclick) be activated with Enter/Space like a native button.
// T24 (ronde 8): eksklusi <a> kini HANYA untuk yang punya href — anchor
// role="button" tanpa href tidak punya perilaku native apa pun sehingga wajib
// diaktifkan handler ini juga (sebelumnya mati total untuk keyboard, WCAG 2.1.1).
document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return;
    var el = e.target;
    if (!el || el.getAttribute('role') !== 'button') return;
    var tag = el.tagName;
    if (tag === 'BUTTON' || (tag === 'A' && el.hasAttribute('href')) || tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
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

        // Toggle the body scroll-lock class ONLY when its state actually
        // changes. Writing it unconditionally is not a no-op for the observer
        // below: classList.remove('modal-open') is recorded as a class mutation
        // even when the token is absent (as long as <body> already has a class
        // attribute), so the observer would re-fire syncState forever and freeze
        // the page — every admin page got stuck "loading" because of this.
        const hasModalOpen = document.body.classList.contains('modal-open');
        if (open.length > 0 && !hasModalOpen) {
            document.body.classList.add('modal-open');
        } else if (open.length === 0 && hasModalOpen) {
            document.body.classList.remove('modal-open');
        }
        if (open.length === 0) {
            if (openSet.size > 0 && lastFocused && document.contains(lastFocused) &&
                typeof lastFocused.focus === 'function') {
                try { lastFocused.focus(); } catch (_) { /* detached */ }
            }
            if (openSet.size > 0) lastFocused = null;
        }
        openSet = openNow;
    }

    // Only react to mutations that involve an overlay (or content inside one).
    // Reacting to every class/style change anywhere in the document would
    // re-run syncState on unrelated updates (toasts, dropdowns, animations)
    // and — combined with the body class toggle above — feed back into itself.
    // childList mutations are also inspected so overlays that are appended or
    // removed at runtime (e.g. showConfirm) still drive scroll-lock/focus.
    function mutationInvolvesOverlay(muts) {
        return muts.some((m) => {
            const t = m.target;
            if (t && t.nodeType === 1 && t.closest(OVERLAY_SELECTOR)) return true;
            if (m.type === 'childList') {
                const check = (node) =>
                    node.nodeType === 1 &&
                    (node.matches(OVERLAY_SELECTOR) || node.querySelector(OVERLAY_SELECTOR));
                for (const node of m.addedNodes) if (check(node)) return true;
                for (const node of m.removedNodes) if (check(node)) return true;
            }
            return false;
        });
    }

    let syncing = false;
    new MutationObserver((muts) => {
        if (syncing || !mutationInvolvesOverlay(muts)) return;
        syncing = true;
        try { syncState(); } finally { syncing = false; }
    }).observe(document.documentElement, {
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
