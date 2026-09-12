/**
 * Avandab Fleet & Operations - Universal 3-Way Theme Controller
 * Supports: Day (Light), Night (Dark), System (Auto)
 * Synchronizes: document.documentElement.classList, localStorage, user_theme cookie,
 * OS matchMedia prefers-color-scheme, and backend DB API (/api/v1/users/me/preferences).
 */
(function () {
    function getCookie(name) {
        var match = document.cookie.match(new RegExp('(^|;\\s*)' + name + '=([^;]*)'));
        return match ? decodeURIComponent(match[2]) : null;
    }

    function setCookie(name, val, days) {
        var d = new Date();
        d.setTime(d.getTime() + (days * 24 * 60 * 60 * 1000));
        document.cookie = name + "=" + encodeURIComponent(val) + ";path=/;max-age=" + (days * 86400) + ";SameSite=Lax";
    }

    function getCurrentThemeMode() {
        return getCookie('user_theme') || localStorage.getItem('theme_mode') || localStorage.getItem('theme') || 'system';
    }

    function isDarkModeActive(mode) {
        if (mode === 'dark') return true;
        if (mode === 'light') return false;
        return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
    }

    var SVG_SUN = '<svg class="w-4 h-4 text-amber-500 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>';
    var SVG_MOON = '<svg class="w-4 h-4 text-indigo-400 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>';
    var SVG_SYSTEM = '<svg class="w-4 h-4 text-slate-400 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/></svg>';

    function applyTheme(mode) {
        var isDark = isDarkModeActive(mode);
        if (isDark) {
            document.documentElement.classList.add('dark');
        } else {
            document.documentElement.classList.remove('dark');
        }

        // Update 3-way dropdown trigger icons with vector SVGs
        document.querySelectorAll('[data-current-theme-icon]').forEach(function (icon) {
            if (mode === 'light') icon.innerHTML = SVG_SUN;
            else if (mode === 'dark') icon.innerHTML = SVG_MOON;
            else icon.innerHTML = SVG_SYSTEM;
        });

        // Update legacy/single button icons with vector SVGs
        document.querySelectorAll('[data-theme-icon]').forEach(function (icon) {
            icon.innerHTML = isDark ? SVG_SUN : SVG_MOON;
        });

        // Update 3-way dropdown checkmarks
        document.querySelectorAll('[data-theme-check]').forEach(function (check) {
            if (check.getAttribute('data-theme-check') === mode) {
                check.classList.remove('hidden');
            } else {
                check.classList.add('hidden');
            }
        });

        // Update aria attributes
        var toggleBtns = document.querySelectorAll('#theme-toggle, [data-theme-toggle]');
        toggleBtns.forEach(function (btn) {
            btn.setAttribute('aria-pressed', isDark ? 'true' : 'false');
        });
    }

    function setThemeMode(mode) {
        if (mode !== 'light' && mode !== 'dark' && mode !== 'system') {
            mode = 'system';
        }

        try {
            localStorage.setItem('theme_mode', mode);
            localStorage.setItem('theme', mode);
            setCookie('user_theme', mode, 30);
        } catch (e) {}

        applyTheme(mode);

        // Close any open theme dropdowns
        document.querySelectorAll('#theme-dropdown, [data-theme-dropdown]').forEach(function (dd) {
            dd.classList.add('hidden');
        });

        // Synchronize with database only when the server rendered an
        // authenticated page. On public pages there is no session, so the
        // request would always 401 — cookie + localStorage already persist
        // the choice and it syncs on the next authenticated page.
        var isAuthed = document.body && document.body.dataset.authenticated === 'true';
        if (isAuthed) {
            try {
                fetch('/api/v1/users/me/preferences', {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ theme: mode })
                }).catch(function () {});
            } catch (e) {}
        }
    }

    // Expose global controller
    window.AvandabTheme = {
        getMode: getCurrentThemeMode,
        setMode: setThemeMode,
        apply: applyTheme,
        toggleNext: function () {
            var current = getCurrentThemeMode();
            // Cycle: light -> dark -> system -> light
            if (current === 'light') setThemeMode('dark');
            else if (current === 'dark') setThemeMode('system');
            else setThemeMode('light');
        }
    };

    function initThemeEvents() {
        var mode = getCurrentThemeMode();
        applyTheme(mode);

        // 1. Setup 3-way dropdowns (per-button guard: re-runs on hx-boost swaps)
        var dropdownBtns = document.querySelectorAll('#theme-menu-btn, [data-theme-menu-btn]');
        dropdownBtns.forEach(function (btn) {
            if (btn.dataset.themeBound) return;
            btn.dataset.themeBound = 'true';
            btn.addEventListener('click', function (e) {
                e.stopPropagation();
                var wrapper = btn.closest('#theme-menu-wrapper, [data-theme-wrapper]') || btn.parentElement;
                var dropdown = wrapper ? wrapper.querySelector('#theme-dropdown, [data-theme-dropdown]') : null;
                if (dropdown) {
                    var isHidden = dropdown.classList.toggle('hidden');
                    btn.setAttribute('aria-expanded', isHidden ? 'false' : 'true');
                }
            });
        });

        // Close dropdown when clicking outside (document-level: bind once)
        if (!window.__themeDocBound) {
            window.__themeDocBound = true;
            document.addEventListener('click', function (e) {
            document.querySelectorAll('#theme-dropdown, [data-theme-dropdown]').forEach(function (dd) {
                var wrapper = dd.closest('#theme-menu-wrapper, [data-theme-wrapper]') || dd.parentElement;
                if (wrapper && !wrapper.contains(e.target)) {
                    dd.classList.add('hidden');
                    var btn = wrapper.querySelector('#theme-menu-btn, [data-theme-menu-btn]');
                    if (btn) btn.setAttribute('aria-expanded', 'false');
                }
            });
            });
        }

        // 2. Setup theme choices inside dropdowns
        document.querySelectorAll('[data-theme-choice]').forEach(function (btn) {
            if (btn.dataset.themeBound) return;
            btn.dataset.themeBound = 'true';
            btn.addEventListener('click', function (e) {
                e.stopPropagation();
                var choice = btn.getAttribute('data-theme-choice');
                setThemeMode(choice);
            });
        });

        // 3. Setup single toggle buttons (e.g., in public header or mobile bars)
        document.querySelectorAll('#theme-toggle, [data-theme-toggle]').forEach(function (btn) {
            if (btn.dataset.themeBound) return;
            // Only attach if it's not a dropdown trigger
            if (!btn.hasAttribute('data-theme-menu-btn') && btn.id !== 'theme-menu-btn') {
                btn.dataset.themeBound = 'true';
                btn.addEventListener('click', function (e) {
                    e.preventDefault();
                    window.AvandabTheme.toggleNext();
                });
            }
        });

        // 4. Live OS theme change listener (bind once)
        if (!window.__themeMqBound) {
            window.__themeMqBound = true;
            try {
                var mq = window.matchMedia('(prefers-color-scheme: dark)');
                var onOSChange = function () {
                    if (getCurrentThemeMode() === 'system') {
                        applyTheme('system');
                    }
                };
                if (mq.addEventListener) {
                    mq.addEventListener('change', onOSChange);
                } else if (mq.addListener) {
                    mq.addListener(onOSChange);
                }
            } catch (e) {}
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initThemeEvents);
    } else {
        initThemeEvents();
    }
    document.body.addEventListener('htmx:load', initThemeEvents);
})();
