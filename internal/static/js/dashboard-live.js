/* Dashboard live board: stamp, KPIs, tabs, filters, SSE snapshot, tables.
 * Extracted from dashboard.html inline script so it survives hx-boost swaps
 * (swapped-in <script> tags never execute). Loaded globally via layout.html.
 * bootDashboardLive() is idempotent: re-runs on htmx:load, resets timers
 * and streams instead of stacking them. */
(function () {
    "use strict";

    function bootDashboardLive() {
        // Reset per-boot state (timers/streams from a previous board instance).
        if (window.__dashStampTimer) { clearInterval(window.__dashStampTimer); window.__dashStampTimer = null; }
        if (window.__dashES) { try { window.__dashES.close(); } catch (e) {} window.__dashES = null; }
        if (window.__dashFallbackES) { try { window.__dashFallbackES.close(); } catch (e) {} window.__dashFallbackES = null; }
        window.__dashLastTableSig = '';
        window.__dashTablesFetching = false;

        var stamp = document.getElementById('dash-live-stamp');
        var sec = 0;
        // Not on the dashboard page (or classic variant without live stamp):
        // still wire tabs/filters below if present, skip stream.
        var onLiveBoard = !!stamp;
        if (onLiveBoard) {
            window.__dashStampTimer = setInterval(function () {
                sec += 1;
                var st = document.getElementById('dash-live-stamp');
                if (st) st.textContent = sec < 60 ? ' updated ' + sec + 's ago • SSE active' : ' updated ' + Math.floor(sec / 60) + 'm ago • SSE active';
            }, 1000);
            var refreshBtn = document.getElementById('dash-refresh-btn');
            if (refreshBtn && !refreshBtn.dataset.dlBound) {
                refreshBtn.dataset.dlBound = 'true';
                refreshBtn.addEventListener('click', function () { window.location.reload(); });
            }
        }
        // More menu — click to open, stays open while you move to it
        (function () {
            var btn = document.getElementById('dash-more-btn');
            var menu = document.getElementById('dash-more-menu');
            var wrap = document.getElementById('dash-more-wrap');
            if (!btn || !menu || !wrap || btn.dataset.dlBound) return;
            btn.dataset.dlBound = 'true';
            btn.addEventListener('click', function (e) { e.stopPropagation(); var willOpen = menu.classList.contains('hidden'); menu.classList.toggle('hidden', !willOpen); btn.setAttribute('aria-expanded', String(willOpen)); });
            if (!window.__dashMoreCloser) {
                window.__dashMoreCloser = true;
                document.addEventListener('click', function (e) {
                    var w = document.getElementById('dash-more-wrap');
                    var m = document.getElementById('dash-more-menu');
                    var b = document.getElementById('dash-more-btn');
                    if (w && m && b && !w.contains(e.target)) { m.classList.add('hidden'); b.setAttribute('aria-expanded', 'false'); }
                });
                document.addEventListener('keydown', function (e) {
                    if (e.key === 'Escape') {
                        var m = document.getElementById('dash-more-menu');
                        var b = document.getElementById('dash-more-btn');
                        if (m && b) { m.classList.add('hidden'); b.setAttribute('aria-expanded', 'false'); b.focus(); }
                    }
                });
            }
        })();
        // Dashboard tabs — Today vs Trends. Sections carry data-dashtab;
        // choice persists in localStorage. Charts live under Trends:
        // dispatch resize on show so Chart.js picks up the visible size.
        (function () {
            var btns = document.querySelectorAll('[data-dashtab-btn]');
            if (!btns.length) return;
            var activeCls = ['bg-surface-container-lowest', 'shadow-xs', 'border', 'border-border-subtle', 'text-on-surface'];
            function paint(active) {
                btns.forEach(function (btn) {
                    var on = btn.getAttribute('data-dashtab-btn') === active;
                    btn.setAttribute('aria-selected', String(on));
                    activeCls.forEach(function (c) { btn.classList.toggle(c, on); });
                    btn.classList.toggle('text-secondary', !on);
                    btn.classList.toggle('hover:text-on-surface', !on);
                });
                document.querySelectorAll('[data-dashtab]').forEach(function (s) {
                    s.classList.toggle('hidden', s.getAttribute('data-dashtab') !== active);
                });
            }
            function show(name, save) {
                paint(name);
                if (save) { try { localStorage.setItem('dash-tab', name); } catch (_) {} }
                window.dispatchEvent(new Event('resize'));
            }
            btns.forEach(function (b) {
                if (b.dataset.dlBound) return;
                b.dataset.dlBound = 'true';
                b.addEventListener('click', function () { show(b.getAttribute('data-dashtab-btn'), true); });
            });
            var initial = 'today';
            try { if (localStorage.getItem('dash-tab') === 'trends') initial = 'trends'; } catch (_) {}
            if (initial !== 'today') paint(initial);
        })();
        // Upcoming trips live filter — handles both desktop table and mobile cards
        var q = document.getElementById('upcoming-search');
        var sel = document.getElementById('upcoming-status');
        var tbody = document.getElementById('upcoming-tbody');
        var mobile = document.getElementById('upcoming-mobile');
        function applyFilter() {
            var qq = document.getElementById('upcoming-search');
            var ss = document.getElementById('upcoming-status');
            var tb = document.getElementById('upcoming-tbody');
            var mb = document.getElementById('upcoming-mobile');
            var needle = ((qq && qq.value) || '').toLowerCase().trim();
            var status = ((ss && ss.value) || '').toLowerCase().trim();
            if (tb) tb.querySelectorAll('tr[data-search]').forEach(function (tr) {
                var hay = (tr.getAttribute('data-search') || '').toLowerCase();
                var st = (tr.getAttribute('data-status') || '').toLowerCase();
                var okQ = !needle || hay.includes(needle);
                var okS = !status || st === status;
                tr.style.display = (okQ && okS) ? '' : 'none';
            });
            if (mb) mb.querySelectorAll('[data-search]').forEach(function (el) {
                var hay = (el.getAttribute('data-search') || '').toLowerCase();
                var st = (el.getAttribute('data-status') || '').toLowerCase();
                var okQ = !needle || hay.includes(needle);
                var okS = !status || st === status;
                el.style.display = (okQ && okS) ? '' : 'none';
            });
        }
        if (q && !q.dataset.dlBound) { q.dataset.dlBound = 'true'; q.addEventListener('input', applyFilter); }
        if (sel && !sel.dataset.dlBound) { sel.dataset.dlBound = 'true'; sel.addEventListener('change', applyFilter); }
        // Reset live stamp on SSE merge + live-update KPIs and charts
        // from the pushed snapshot (same payload, no extra requests).
        function setNum(id, v) {
            var el = document.getElementById(id);
            if (el && v !== undefined && v !== null) el.textContent = v;
        }
        function updateKpis(d) {
            setNum('kpi-today', d.TodaysTripsCount);
            setNum('kpi-active', d.ActiveTripsCount);
            setNum('kpi-completed', d.CompletedTripsCount);
            setNum('kpi-cancelled', d.CancelledTripsCount);
            setNum('kpi-vehicles', d.AvailableVehiclesCount);
            setNum('kpi-drivers', d.AvailableDriversCount);
            setNum('kpi-pending', d.PendingPaymentsCount);
            var rev = document.getElementById('kpi-revenue');
            if (rev && d.MonthlyRevenue !== undefined && d.MonthlyRevenue !== null) {
                rev.textContent = '₹' + Number(d.MonthlyRevenue).toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
            }
            var today = Number(d.TodaysTripsCount || 0);
            var active = Number(d.ActiveTripsCount || 0);
            var cancelled = Number(d.CancelledTripsCount || 0);
            var bar = document.getElementById('kpi-active-bar');
            if (bar) bar.style.width = today > 0 ? Math.round(active * 100 / today) + '%' : '0%';
            var pct = document.getElementById('kpi-cancel-pct');
            if (pct) pct.textContent = today > 0 ? Math.round(cancelled * 100 / today) + '% of today' : 'no trips today';
            var att = d.Attention || {};
            var an = function (v) { return Number(v) || 0; };
            var attMap = {
                'att-total': an(att.UnassignedBookings) + an(att.MaintenanceDue) + an(att.OpenWorkOrders) + an(att.GarageVehicles) + an(att.OpenAlerts) + an(att.ActiveDTCs) + an(att.ExpiringEwaybills) + an(att.PendingKharcha) + an(att.LowFastag),
                'att-unassigned': att.UnassignedBookings, 'att-due': att.MaintenanceDue,
                'att-wo': att.OpenWorkOrders, 'att-garage': att.GarageVehicles,
                'att-alerts': att.OpenAlerts, 'att-dtc': att.ActiveDTCs,
                'att-ewb': att.ExpiringEwaybills, 'att-kharcha': att.PendingKharcha,
                'att-fastag': att.LowFastag
            };
            Object.keys(attMap).forEach(function (id) {
                var v = attMap[id];
                if (v === undefined) return;
                var el = document.getElementById(id);
                if (!el) return;
                el.textContent = v;
                if (id !== 'att-total') {
                    var card = el.closest('a');
                    if (card) card.style.display = v > 0 ? '' : 'none';
                }
            });
        }
        // Exposed for automated browser tests (Playwright drives a
        // synthetic snapshot through the real update path).
        window.__updateDashboardKpis = updateKpis;
        // Tables and feeds swap server-rendered fragments when the
        // snapshot signature changes (lengths + first ids). Same
        // partials as the initial render — no markup drift.
        function tableSig(d) {
            var f = function (a, key) {
                if (!a || !a.length) return '0:';
                return a.length + ':' + ((a[0] && a[0].ID) || '') + ':' + a.map(function (r) { return (r && r[key]) || ''; }).join(',');
            };
            return [
                f(d.UpcomingTrips, 'Status'), f(d.RecentBookings, 'Status'),
                f(d.RecentPayments, 'ID'), f(d.RecentActivity, 'ID'),
                f(d.OverdueTrips, 'Status'), f(d.IdleVehicles, 'Status'),
                f(d.PendingInvoices, 'PaymentStatus')
            ].join('|');
        }
        function refreshTables() {
            if (window.__dashTablesFetching) return;
            window.__dashTablesFetching = true;
            fetch('/dashboard/tables', { headers: { 'Accept': 'application/json' } })
                .then(function (r) { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
                .then(function (payload) {
                    var regions = payload.regions || {};
                    Object.keys(regions).forEach(function (id) {
                        var el = document.getElementById(id);
                        if (el) el.innerHTML = regions[id];
                    });
                    var badges = payload.badges || {};
                    Object.keys(badges).forEach(function (id) {
                        var el = document.getElementById(id);
                        if (el) el.textContent = badges[id];
                    });
                    if (typeof applyFilter === 'function') applyFilter();
                })
                .catch(function () {})
                .finally(function () { window.__dashTablesFetching = false; });
        }
        // Exposed for automated browser tests.
        window.__dashboardTables = { refresh: refreshTables, sig: tableSig };
        function startDashStream() {
            if (!window.EventSource || window.__dashES || document.hidden) return;
            if (!document.getElementById('dash-live-stamp')) return;
            try {
                window.__dashES = new EventSource('/dashboard/stream');
                window.__dashES.addEventListener('datastar-merge-signals', function (e) {
                    sec = 0;
                    var st = document.getElementById('dash-live-stamp');
                    if (st) st.textContent = ' updated just now • SSE active';
                    try {
                        var parsed = JSON.parse(e.data);
                        var snap = parsed.dashboard || parsed;
                        updateKpis(snap);
                        if (typeof window.__updateDashboardCharts === 'function') window.__updateDashboardCharts(snap);
                        try {
                            var sig = tableSig(snap);
                            if (sig !== window.__dashLastTableSig) { window.__dashLastTableSig = sig; refreshTables(); }
                        } catch (err2) {}
                        // Soft live hint: flash KPI cards
                        document.querySelectorAll('.text-stat').forEach(function (el) {
                            el.classList.add('opacity-60');
                            setTimeout(function () { el.classList.remove('opacity-60'); }, 350);
                        });
                    } catch (err) {}
                });
                window.__dashES.onerror = function () {
                    if (document.hidden) {
                        stopDashStream();
                    } else {
                        var st = document.getElementById('dash-live-stamp');
                        if (st) st.textContent = ' reconnecting live stream…';
                    }
                };
            } catch (_) {}
        }
        function stopDashStream() {
            if (window.__dashES) {
                window.__dashES.close();
                window.__dashES = null;
            }
            var st = document.getElementById('dash-live-stamp');
            if (st && document.hidden) {
                st.textContent = ' live paused (tab hidden)';
            }
        }
        // Fallback stream for classic variant (no live stamp): keeps the
        // connection warm without KPI wiring.
        if (!document.getElementById('dash-live-stamp')) {
            if (window.EventSource && !window.__dashFallbackES && !document.hidden) {
                try {
                    window.__dashFallbackES = new EventSource('/dashboard/stream');
                    window.__dashFallbackES.onerror = function () { if (window.__dashFallbackES) { window.__dashFallbackES.close(); window.__dashFallbackES = null; } };
                } catch (_) {}
            }
        } else {
            startDashStream();
        }
        if (!window.__dashVisBound) {
            window.__dashVisBound = true;
            document.addEventListener('visibilitychange', function () {
                if (document.hidden) {
                    stopDashStream();
                } else {
                    refreshTables();
                    startDashStream();
                }
            });
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', bootDashboardLive);
    } else {
        bootDashboardLive();
    }
    document.body.addEventListener('htmx:load', bootDashboardLive);
})();
