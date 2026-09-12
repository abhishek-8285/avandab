(function () {
  'use strict';

  if (typeof window === 'undefined' || !window.localStorage) return;

  var CHANNEL_NAME = 'avandab_session_guard';
  var STORAGE_KEY = 'avandab_primary_tab_session';
  var TAB_ID = 'tab_' + Date.now() + '_' + Math.random().toString(36).substring(2, 9);

  var isPrimary = false;
  var modalEl = null;
  var broadcastChannel = null;

  if ('BroadcastChannel' in window) {
    try {
      broadcastChannel = new BroadcastChannel(CHANNEL_NAME);
    } catch (e) {
      broadcastChannel = null;
    }
  }

  function postMessage(msg) {
    msg.tabId = TAB_ID;
    msg.timestamp = Date.now();
    if (broadcastChannel) {
      try {
        broadcastChannel.postMessage(msg);
      } catch (e) {}
    }
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(msg));
    } catch (e) {}
  }

  function createModal() {
    if (modalEl) return modalEl;

    modalEl = document.createElement('div');
    modalEl.id = 'session-conflict-modal';
    modalEl.className = 'fixed inset-0 z-[99999] flex items-center justify-center p-4 bg-background/80 backdrop-blur-md transition-all duration-300';
    modalEl.setAttribute('role', 'dialog');
    modalEl.setAttribute('aria-modal', 'true');
    modalEl.setAttribute('aria-labelledby', 'session-conflict-title');

    modalEl.innerHTML = 
      '<div class="w-full max-w-md bg-surface-container-lowest border border-border-subtle rounded-2xl shadow-2xl p-6 sm:p-8 text-center animate-in fade-in zoom-in-95 duration-200">' +
        '<div class="w-16 h-16 mx-auto mb-5 rounded-2xl bg-primary/10 text-primary flex items-center justify-center shadow-inner">' +
          '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-8 h-8">' +
            '<rect width="20" height="14" x="2" y="3" rx="2"/>' +
            '<line x1="8" x2="16" y1="21" y2="21"/>' +
            '<line x1="12" x2="12" y1="17" y2="21"/>' +
          '</svg>' +
        '</div>' +
        '<h2 id="session-conflict-title" class="text-xl sm:text-2xl font-bold text-on-surface mb-2 font-display">' +
          'Avandab is open in another window' +
        '</h2>' +
        '<p class="text-secondary text-sm sm:text-base leading-relaxed mb-6">' +
          'You are currently using Avandab in another browser tab or window. Click <span class="font-semibold text-on-surface">"Use Here"</span> to switch control to this window.' +
        '</p>' +
        '<div class="flex flex-col sm:flex-row items-center justify-center gap-3">' +
          '<button id="session-use-here-btn" type="button" class="w-full sm:w-auto px-6 py-2.5 bg-primary text-on-primary hover:bg-primary/90 rounded-lg font-medium text-sm transition-all duration-150 active:scale-[0.98] shadow-sm flex items-center justify-center gap-2 cursor-pointer">' +
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4">' +
              '<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>' +
            '</svg>' +
            '<span>Use Here</span>' +
          '</button>' +
        '</div>' +
      '</div>';

    document.body.appendChild(modalEl);

    var useHereBtn = modalEl.querySelector('#session-use-here-btn');
    if (useHereBtn) {
      useHereBtn.addEventListener('click', function () {
        claimPrimary(true);
      });
    }

    return modalEl;
  }

  function showConflictModal() {
    isPrimary = false;
    var modal = createModal();
    modal.style.display = 'flex';
    document.body.style.overflow = 'hidden';

    // Disconnect or pause active EventSources to avoid background stream load
    if (window.__activeEventSources && Array.isArray(window.__activeEventSources)) {
      window.__activeEventSources.forEach(function (es) {
        try { es.close(); } catch (e) {}
      });
    }
  }

  function hideConflictModal() {
    if (modalEl) {
      modalEl.style.display = 'none';
      document.body.style.overflow = '';
    }
  }

  function claimPrimary(isUserInitiated) {
    isPrimary = true;
    hideConflictModal();
    postMessage({ type: 'CLAIM_PRIMARY' });

    if (isUserInitiated) {
      // Re-trigger datastar signals or reload if needed
      if (typeof window.Datastar !== 'undefined' && window.Datastar.refresh) {
        try { window.Datastar.refresh(); } catch (e) {}
      }
    }
  }

  function handleIncomingMessage(msg) {
    if (!msg || typeof msg !== 'object') return;
    if (msg.tabId === TAB_ID) return;

    if (msg.type === 'CLAIM_PRIMARY') {
      if (isPrimary) {
        showConflictModal();
      }
    }
  }

  if (broadcastChannel) {
    broadcastChannel.onmessage = function (event) {
      handleIncomingMessage(event.data);
    };
  }

  window.addEventListener('storage', function (event) {
    if (event.key === STORAGE_KEY && event.newValue) {
      try {
        var data = JSON.parse(event.newValue);
        handleIncomingMessage(data);
      } catch (e) {}
    }
  });

  window.addEventListener('beforeunload', function () {
    if (isPrimary) {
      postMessage({ type: 'RELEASE_PRIMARY' });
    }
  });

  function init() {
    // Re-asserts primary on hx-boost swaps; same-tab no-op for others
    // (handleIncomingMessage ignores our own TAB_ID).
    claimPrimary(false);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
  document.body.addEventListener('htmx:load', init);

  window.AvandabSessionGuard = {
    tabId: TAB_ID,
    isPrimary: function () { return isPrimary; },
    claimPrimary: function () { claimPrimary(true); },
    showModal: showConflictModal,
    hideModal: hideConflictModal
  };
})();
