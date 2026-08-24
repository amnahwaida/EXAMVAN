/**
 * Device-fingerprint helper for public forms.
 *
 * Generates a stable client-side fingerprint with FingerprintJS and stores it
 * in each form's hidden `device_fingerprint` field so the rate limiter can use
 * it as a second, parallel dimension alongside the client IP (see
 * middleware/ratelimit.go getRateLimitKeys).
 *
 * The fingerprint is a one-way hash of browser/device signals — it is not
 * personal data and the server only ever hashes it again. It degrades
 * gracefully: if FingerprintJS fails to load (offline, ad-blocker) the field
 * is left empty and the request is limited by IP alone.
 *
 * R44: generate() tetap async ratusan ms meski urutan defer benar — autofill
 * password-manager + Enter bisa mengirim form dengan device_fingerprint masih
 * kosong (rate limiter jatuh ke dimensi IP saja). Saat submit bila field
 * kosong & generate() masih in-flight, pengiriman DITAHAN via Promise.race
 * dengan timeout ±1,5 detik; setelah itu dikirim apa adanya (tanpa mengisi
 * nilai palsu).
 */
(function () {
  'use strict';

  // Timeout tunggu in-flight saat submit. Override-able untuk test vm
  // (window.__deviceFingerprintTimeout) — di produksi selalu 1500ms.
  var SUBMIT_WAIT_TIMEOUT_MS = (typeof window !== 'undefined'
    && parseInt(window.__deviceFingerprintTimeout, 10)) || 1500;

  // Promise generate() yang sedang berjalan (null bila sudah selesai/gagal).
  var pendingPromise = null;

  function generate() {
    if (!window.FingerprintJS) {
      return Promise.resolve('');
    }
    // 1) Load an agent; 2) compute a fingerprint hash. No visitorId is kept
    // across requests — the hash is recomputed per page load.
    return window.FingerprintJS.load()
      .then(function (fp) { return fp.get(); })
      .then(function (result) {
        return result.visitorId || '';
      })
      .catch(function () { return ''; });
  }

  function fillForm(form, fp) {
    var input = form.querySelector('input[name="device_fingerprint"]');
    if (input) {
      input.value = fp;
    }
  }

  /**
   * R44: selesaikan field fingerprint satu form. Bila field masih kosong dan
   * generate() in-flight, tunggu promise tersebut dibatasi timeout
   * (Promise.race); setelah itu field dikirim apa adanya.
   */
  function waitForFingerprint(form) {
    var input = form.querySelector('input[name="device_fingerprint"]');
    if (!input || input.value || !pendingPromise) {
      return Promise.resolve();
    }
    return Promise.race([
      pendingPromise.then(function (fp) { fillForm(form, fp); }),
      new Promise(function (resolve) { setTimeout(resolve, SUBMIT_WAIT_TIMEOUT_MS); }),
    ]);
  }

  /** Pasang penahan submit: cegah kirim → tunggu fingerprint → kirim ulang. */
  function wireSubmitHold(form) {
    form.addEventListener('submit', function (e) {
      var input = form.querySelector('input[name="device_fingerprint"]');
      // Field sudah terisi / tidak ada generate in-flight → biarkan normal.
      if (!pendingPromise || !input || input.value) return;
      e.preventDefault();
      waitForFingerprint(form).then(function () {
        // Kirim ulang secara native (bypass listener ini — tidak loop).
        form.submit();
      });
    });
  }

  function init() {
    var forms = document.querySelectorAll('form');
    if (forms.length === 0) {
      return;
    }
    for (var i = 0; i < forms.length; i++) {
      wireSubmitHold(forms[i]);
    }
    // pendingPromise resolve dengan STRING fingerprint (dipakai race R44);
    // pengisian form adalah cabang tersendiri dari promise yang sama.
    pendingPromise = generate();
    pendingPromise.then(function (fp) {
      for (var i = 0; i < forms.length; i++) {
        fillForm(forms[i], fp);
      }
    }).catch(function () {
      /* fingerprint gagal — field dibiarkan kosong (degradasi R22) */
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
