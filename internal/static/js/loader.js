(function () {
  var overlayEl = null;
  var btnSpinners = new WeakMap();
  var manual = 0;
  var fetchPending = 0;
  var delayTimer = null;

  function ensureOverlay() {
    if (overlayEl) return overlayEl;
    overlayEl = document.createElement('div');
    overlayEl.id = 'global-loader';
    overlayEl.setAttribute('role', 'status');
    overlayEl.setAttribute('aria-live', 'polite');
    overlayEl.setAttribute('aria-hidden', 'true');
    overlayEl.innerHTML =
      '<div class="loader-backdrop"></div>' +
      '<div class="loader-box">' +
      '<div class="loader-spinner" aria-hidden="true"></div>' +
      '<span class="loader-text">Loading…</span>' +
      '</div>';
    overlayEl.style.display = 'none';
    (document.body || document.documentElement).appendChild(overlayEl);
    return overlayEl;
  }

  function render() {
    var active = (manual + fetchPending) > 0;
    if (!active && !overlayEl) return;
    var el = overlayEl || ensureOverlay();
    if (active) {
      el.style.display = 'flex';
      el.setAttribute('aria-hidden', 'false');
    } else {
      el.style.display = 'none';
      el.setAttribute('aria-hidden', 'true');
    }
  }

  function showSoon() {
    if (delayTimer) return;
    delayTimer = setTimeout(function () { delayTimer = null; render(); }, 200);
  }

  function fetchStart() {
    fetchPending++;
    if (fetchPending === 1) showSoon();
  }

  function fetchEnd() {
    fetchPending = Math.max(0, fetchPending - 1);
    if (fetchPending === 0) { clearTimeout(delayTimer); delayTimer = null; render(); }
  }

  // Background polls, HTMX partial updates, and API requests must never flash
  // the fullscreen overlay — otherwise partial DOM morphs or 10s polls look like full page reloads.
  function isSilentFetch(target, opts) {
    try {
      if (opts && (opts.silent === true || opts.loader === false)) return true;
      var checkHeaders = function (h) {
        if (!h) return false;
        if (typeof h.get === 'function') {
          if (h.get('X-Silent') != null || h.get('X-Loader-Silent') != null || h.get('HX-Request') != null) return true;
          var acc = h.get('Accept') || '';
          if (acc.indexOf('application/json') !== -1 || acc.indexOf('text/event-stream') !== -1) return true;
        } else {
          for (var k in h) {
            if (/^x-(silent|loader-silent)$/i.test(k) || /^hx-request$/i.test(k)) return true;
            if (/^accept$/i.test(k) && typeof h[k] === 'string' && (h[k].indexOf('application/json') !== -1 || h[k].indexOf('text/event-stream') !== -1)) return true;
          }
        }
        return false;
      };
      if (opts && opts.headers && checkHeaders(opts.headers)) return true;
      if (target && target.headers && checkHeaders(target.headers)) return true;
    } catch (e) {}
    var s = '';
    try {
      if (typeof target === 'string') s = target;
      else if (target && target.url) s = target.url;
    } catch (e) {}
    return /^\/api\/|\/dashboard\/|\/alerts\/|\/trips\/[^\/]+\/compliance|\/static\/|\/sw\.js|\/ws/.test(s);
  }

  var Loader = {
    show: function () { manual++; render(); },
    hide: function () { manual = Math.max(0, manual - 1); render(); },
    wrap: function (p) {
      Loader.show();
      return Promise.resolve(p).finally(function () { Loader.hide(); });
    },
    spin: function (btn, label) {
      if (!btn || btnSpinners.has(btn)) return function () {};
      var prev = btn.innerHTML;
      var prevDisabled = btn.disabled;
      btn.disabled = true;
      btn.innerHTML =
        '<span class="loader-btn-spin" aria-hidden="true"></span>' +
        (label ? '<span>' + label + '</span>' : '');
      btnSpinners.set(btn, { prev: prev, prevDisabled: prevDisabled });
      return function () { Loader.unspin(btn); };
    },
    unspin: function (btn) {
      var s = btnSpinners.get(btn);
      if (!s) return;
      btn.innerHTML = s.prev;
      btn.disabled = s.prevDisabled;
      btnSpinners.delete(btn);
    },
    bindFetch: function () {
      if (window.__loaderFetchBound) return;
      window.__loaderFetchBound = true;
      if (!window.fetch) return;
      var orig = window.fetch.bind(window);
      window.fetch = function () {
        if (isSilentFetch(arguments[0], arguments[1])) {
          try { return orig.apply(window, arguments); }
          catch (e) { throw e; }
        }
        fetchStart();
        try { return orig.apply(window, arguments).finally(fetchEnd); }
        catch (e) { fetchEnd(); throw e; }
      };
    }
  };
  window.Loader = Loader;

  function init() { Loader.bindFetch(); }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
