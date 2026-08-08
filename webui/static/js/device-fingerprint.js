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
 */
(function () {
  'use strict';

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

  function init() {
    var forms = document.querySelectorAll('form');
    if (forms.length === 0) {
      return;
    }
    generate().then(function (fp) {
      for (var i = 0; i < forms.length; i++) {
        fillForm(forms[i], fp);
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
