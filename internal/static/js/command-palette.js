/* Quick navigation palette — Ctrl+K / Cmd+K.
 *
 * The console has 60+ sidebar links and a search box that is hidden below the
 * sm breakpoint, so on a phone there is no way to reach a page without opening
 * the drawer and scrolling. This adds a single, keyboard- and touch-friendly
 * entry point that indexes the links the server already rendered for the
 * current user's role — so it never exposes a page RBAC hid.
 *
 * Progressive enhancement: the trigger in layout.html is a real link to
 * /search. Without JavaScript it still works; with JavaScript we intercept it
 * and show the palette instead. No framework, no network calls, no state on
 * the server.
 */
(function () {
  'use strict';

  var overlay = null;
  var input = null;
  var results = null;
  var commands = [];
  var matches = [];
  var active = 0;
  var lastFocus = null;

  function visibleSearch() {
    var el = document.getElementById('global-search-input');
    return el && el.offsetParent !== null ? el : null;
  }

  // A search action that focuses the topbar input when it is on screen, and
  // falls back to the full search page on phones where that input is hidden.
  function searchCommand() {
    return {
      label: 'Search records…', section: 'Actions', href: '/search',
      action: function () {
        var el = visibleSearch();
        if (el) { el.focus(); el.select(); } else { window.location.assign('/search'); }
      },
    };
  }

  function collectCommands() {
    var out = [searchCommand()];
    var nav = document.getElementById('sidebar-nav');
    if (!nav) return out;
    var section = '';
    // Sidebar children alternate between .nav-section-label and nav links.
    // Section names are inherited so a result can say where it lives.
    Array.prototype.forEach.call(nav.children, function (el) {
      if (el.classList && el.classList.contains('nav-section-label')) {
        section = (el.textContent || '').trim();
        return;
      }
      if (el.tagName !== 'A') return;
      var href = el.getAttribute('href');
      if (!href || href.charAt(0) !== '/') return;
      var labelEl = el.querySelector('span');
      var label = ((labelEl || el).textContent || '').trim();
      if (!label) return;
      out.push({ label: label, section: section, href: href, action: null });
    });
    return out;
  }

  // Ranked fuzzy match: exact > prefix > substring > section > path >
  // subsequence. Never throws on an empty query — everything ranks equally.
  function score(cmd, q) {
    if (!q) return 1;
    var label = cmd.label.toLowerCase();
    var section = (cmd.section || '').toLowerCase();
    var href = cmd.href.toLowerCase();
    if (label === q) return 100;
    if (label.indexOf(q) === 0) return 80;
    if (label.indexOf(q) !== -1) return 60;
    if (section.indexOf(q) !== -1) return 40;
    if (href.indexOf(q) !== -1) return 20;
    var i = 0;
    for (var n = 0; n < label.length && i < q.length; n++) {
      if (label.charAt(n) === q.charAt(i)) i++;
    }
    return i === q.length ? 10 : 0;
  }

  function markActive() {
    Array.prototype.forEach.call(results.querySelectorAll('.cp-option'), function (el, i) {
      var on = i === active;
      el.classList.toggle('is-active', on);
      el.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    if (overlay && input) {
      if (matches.length) input.setAttribute('aria-activedescendant', 'cp-option-' + active);
      else input.removeAttribute('aria-activedescendant');
    }
  }

  // Labels come from the DOM, so build option nodes with textContent rather
  // than innerHTML: a translated label containing '<' must stay text.
  function render() {
    var q = (input.value || '').trim().toLowerCase();
    matches = commands
      .map(function (cmd) { return { cmd: cmd, s: score(cmd, q) }; })
      .filter(function (x) { return x.s > 0; })
      .sort(function (a, b) { return b.s - a.s || a.cmd.label.localeCompare(b.cmd.label); })
      .slice(0, 9)
      .map(function (x) { return x.cmd; });

    active = 0;
    results.replaceChildren();

    if (!matches.length) {
      var empty = document.createElement('p');
      empty.className = 'cp-empty';
      empty.textContent = 'No matching pages — press Enter to search all records.';
      results.appendChild(empty);
      input.removeAttribute('aria-activedescendant');
      return;
    }

    matches.forEach(function (cmd, i) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.id = 'cp-option-' + i;
      btn.className = 'cp-option' + (i === 0 ? ' is-active' : '');
      btn.setAttribute('role', 'option');
      btn.setAttribute('aria-selected', i === 0 ? 'true' : 'false');

      var label = document.createElement('span');
      label.className = 'cp-option-label';
      label.textContent = cmd.label;
      btn.appendChild(label);

      if (cmd.section) {
        var section = document.createElement('span');
        section.className = 'cp-option-section';
        section.textContent = cmd.section;
        btn.appendChild(section);
      }
      btn.addEventListener('click', function () { activate(i); });
      results.appendChild(btn);
    });
    input.setAttribute('aria-activedescendant', 'cp-option-0');
  }

  function move(step) {
    if (!matches.length) return;
    active = (active + step + matches.length) % matches.length;
    markActive();
    var el = document.getElementById('cp-option-' + active);
    if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' });
  }

  function activate(index) {
    var cmd = index === undefined ? matches[active] : matches[index];
    // Empty result list still offers a useful action: a full-text search.
    if (!cmd) cmd = searchCommand();
    close();
    if (cmd.action) cmd.action();
    else window.location.assign(cmd.href);
  }

  function onKeyDown(e) {
    if (e.key === 'Escape') { e.preventDefault(); close(); return; }
    if (e.key === 'ArrowDown') { e.preventDefault(); move(1); return; }
    if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); return; }
    if (e.key === 'Enter') { e.preventDefault(); activate(); return; }
    // Roving tabindex lives on the input; do not let Tab escape the dialog.
    if (e.key === 'Tab') e.preventDefault();
  }

  function close() {
    if (!overlay || overlay.hidden) return;
    overlay.hidden = true;
    document.removeEventListener('keydown', onKeyDown, true);
    if (lastFocus && lastFocus.focus) lastFocus.focus();
    lastFocus = null;
  }

  function open() {
    ensureOverlay();
    if (!overlay) return;
    lastFocus = document.activeElement;
    commands = collectCommands();
    input.value = '';
    render();
    overlay.hidden = false;
    document.addEventListener('keydown', onKeyDown, true);
    input.focus();
  }

  // HTMX boost replaces <body>, taking the overlay with it; rebuild lazily.
  function ensureOverlay() {
    var existing = document.getElementById('command-palette');
    if (existing && existing.__cpReady) {
      overlay = existing;
      input = document.getElementById('cp-input');
      results = document.getElementById('cp-results');
      return;
    }
    if (existing) existing.parentNode.removeChild(existing);

    overlay = document.createElement('div');
    overlay.id = 'command-palette';
    overlay.className = 'cp-overlay';
    overlay.hidden = true;
    overlay.innerHTML =
      '<div class="cp-dialog" role="dialog" aria-modal="true" aria-label="Quick navigation">' +
        '<div class="cp-search">' +
          '<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>' +
          '<input id="cp-input" type="text" role="combobox" aria-expanded="true" aria-controls="cp-results" aria-autocomplete="list" aria-label="Search pages" autocomplete="off" spellcheck="false" placeholder="Jump to a page or search records…">' +
          '<kbd class="cp-kbd">Esc</kbd>' +
        '</div>' +
        '<div id="cp-results" class="cp-results" role="listbox" aria-label="Pages"></div>' +
        '<div class="cp-footer">' +
          '<span><kbd class="cp-kbd">↑</kbd><kbd class="cp-kbd">↓</kbd> navigate</span>' +
          '<span><kbd class="cp-kbd">↵</kbd> open</span>' +
          '<span><kbd class="cp-kbd">Esc</kbd> close</span>' +
        '</div>' +
      '</div>';
    document.body.appendChild(overlay);
    overlay.__cpReady = true;

    input = document.getElementById('cp-input');
    results = document.getElementById('cp-results');
    input.addEventListener('input', render);
    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) close();
    });
  }

  // Only authenticated pages carry the shell; public/auth layouts do not.
  if (!document.body || document.body.getAttribute('data-authenticated') !== 'true') return;

  document.addEventListener('keydown', function (e) {
    if ((e.ctrlKey || e.metaKey) && String(e.key).toLowerCase() === 'k') {
      e.preventDefault();
      open();
    }
  });
  // Capture before htmx's boosted-link handler so clicking the trigger opens
  // the palette instead of navigating straight to /search.
  document.addEventListener('click', function (e) {
    var trigger = e.target && e.target.closest ? e.target.closest('[data-cp-open]') : null;
    if (trigger) { e.preventDefault(); e.stopPropagation(); open(); }
  }, true);
  document.addEventListener('htmx:afterSwap', function () {
    if (overlay && !document.contains(overlay)) {
      overlay = null; input = null; results = null;
    }
  });
})();
