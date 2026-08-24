/* Command Center client (Spec 22 §4.1): fleet strip → context panel,
 * palette search, inline actions, mobile bottom-sheet. No framework. */
(function () {
    "use strict";

    var state = { selected: null, ctx: null };

    // ── helpers ─────────────────────────────────────────────────────
    function $(id) { return document.getElementById(id); }

    function api(path, opts) {
        opts = opts || {};
        opts.credentials = "same-origin";
        if (opts.body && typeof opts.body !== "string") {
            opts.headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
            opts.body = JSON.stringify(opts.body);
        }
        return fetch(path, opts).then(function (resp) {
            if (!resp.ok) { throw new Error("HTTP " + resp.status); }
            return resp.json();
        });
    }

    function toast(msg, ok) {
        if (window.FlyToast) { window.FlyToast.show(msg, { success: ok !== false }); }
    }

    function fmtMoney(v) {
        if (v === null || v === undefined) { return "—"; }
        return "₹" + Number(v).toLocaleString("en-IN", { maximumFractionDigits: 2 });
    }

    function esc(s) {
        return String(s === null || s === undefined ? "" : s)
            .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    // ── fleet strip ─────────────────────────────────────────────────
    function statusClass(status) {
        if (status === "running" || status === "active") { return "bg-status-success"; }
        if (status === "idle" || status === "available") { return "bg-status-warning"; }
        return "bg-error";
    }

    function loadFleet() {
        var wrap = $("fleet-cards");
        if (!wrap) { return; }
        api("/api/fleet").then(function (data) {
            var vehicles = data.vehicles || [];
            var count = $("fleet-count");
            if (count) { count.textContent = vehicles.length + " vehicles"; }
            wrap.innerHTML = "";
            vehicles.forEach(function (v) {
                var btn = document.createElement("button");
                btn.type = "button";
                btn.setAttribute("data-vehicle-id", v.id);
                btn.className = "text-left w-full rounded-lg border border-border-subtle px-3 py-2.5 " +
                    "hover:bg-surface-container transition-colors cursor-pointer";
                btn.innerHTML =
                    '<div class="flex items-center gap-2">' +
                    '<span class="w-2 h-2 rounded-full ' + statusClass(v.status) + '"></span>' +
                    '<span class="font-medium text-on-surface text-sm truncate">' + esc(v.number) + "</span>" +
                    '<span class="ml-auto text-xs text-text-muted">' +
                    (v.speed_kmph != null ? Math.round(v.speed_kmph) + " km/h" : esc(v.status || "")) +
                    "</span></div>";
                btn.addEventListener("click", function () { selectVehicle(v.id, btn); });
                wrap.appendChild(btn);
            });
        }).catch(function () {
            wrap.innerHTML = '<p class="text-xs text-text-muted px-1">Fleet unavailable.</p>';
        });
    }

    function markSelected(btn) {
        document.querySelectorAll("#fleet-cards [data-vehicle-id]").forEach(function (el) {
            el.classList.remove("ring-2", "ring-primary", "bg-primary-container");
        });
        if (btn) {
            btn.classList.add("ring-2", "ring-primary", "bg-primary-container");
        }
    }

    // ── context panel ───────────────────────────────────────────────
    function setCtx(id, text) {
        var el = $(id);
        if (el) { el.textContent = text || "—"; }
    }

    function feedback(msg, isErr) {
        var el = $("ctx-feedback");
        if (!el) { return; }
        el.textContent = msg;
        el.classList.toggle("hidden", !msg);
        el.classList.toggle("bg-error-container", !!isErr);
        el.classList.toggle("text-on-error-container", !!isErr);
        if (msg && !isErr) { setTimeout(function () { el.classList.add("hidden"); }, 4000); }
    }

    function selectVehicle(vehicleID, btn) {
        state.selected = vehicleID;
        markSelected(btn);
        openMobileSheet();
        ["ctx-position", "ctx-trip", "ctx-driver", "ctx-pnl", "ctx-fastag", "ctx-eway"]
            .forEach(function (id) { setCtx(id, "…"); });
        api("/api/fleet/" + encodeURIComponent(vehicleID) + "/context").then(function (c) {
            state.ctx = c;
            renderContext(c);
        }).catch(function () {
            feedback("Could not load vehicle context.", true);
        });
    }

    function renderContext(c) {
        var v = c.vehicle || {};
        var title = $("ctx-title");
        if (title) { title.textContent = v.number || v.id || "Vehicle context"; }
        var status = $("ctx-status");
        if (status) { status.textContent = v.status || ""; }

        setCtx("ctx-position", c.position
            ? (Number(c.position.lat).toFixed(4) + ", " + Number(c.position.lng).toFixed(4) +
                " @ " + Math.round(c.position.speed_kmph || 0) + " km/h")
            : "no GPS");

        setCtx("ctx-trip", c.trip ? ((c.trip.route || c.trip.id || "") +
            (c.trip.eta_at ? " · ETA " + new Date(c.trip.eta_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "")) : "no active trip");

        setCtx("ctx-driver", c.driver ? c.driver.name : "unassigned");
        var call = $("ctx-call");
        if (call) {
            if (c.driver && c.driver.phone) {
                call.href = "tel:" + c.driver.phone;
                call.classList.remove("hidden");
            } else {
                call.classList.add("hidden");
            }
        }

        setCtx("ctx-pnl", c.pnl_km_today ? ("₹" + Number(c.pnl_km_today).toFixed(2)) : "—");
        setCtx("ctx-fastag", c.fastag_balance != null ? fmtMoney(c.fastag_balance) : "—");

        var kharcha = $("ctx-kharcha");
        if (kharcha) {
            kharcha.innerHTML = (c.kharcha_pending || []).length
                ? c.kharcha_pending.map(function (k) {
                    return "<li>" + fmtMoney(k.amount) + " · " + esc(k.category) + "</li>";
                }).join("")
                : "<li>none</li>";
        }

        var tripActions = $("ctx-trip-actions");
        if (tripActions) {
            tripActions.classList.toggle("hidden", !(c.trip && c.trip.id));
            tripActions.dataset.tripId = (c.trip && c.trip.id) || "";
        }

        setCtx("ctx-eway", c.eway_bill ? ("valid till " + new Date(c.eway_bill.expires_at).toLocaleString()) : "none");
        var extendBtn = $("ctx-eway-extend");
        if (extendBtn) {
            extendBtn.classList.toggle("hidden", !(c.eway_bill && c.eway_bill.id));
            extendBtn.dataset.ewbId = (c.eway_bill && c.eway_bill.id) || "";
        }

        var docs = $("ctx-docs");
        if (docs) {
            docs.innerHTML = (c.docs_expiring || []).length
                ? c.docs_expiring.map(function (d) {
                    return "<li>" + esc(d.kind) + " · " + esc(d.expires_on) + "</li>";
                }).join("")
                : "<li>nothing expiring in 30 days</li>";
        }
    }

    // ── inline actions ──────────────────────────────────────────────
    function wireActions() {
        var extend = $("ctx-eway-extend");
        if (extend) {
            extend.addEventListener("click", function () {
                var ewbID = extend.dataset.ewbId;
                if (!ewbID) { return; }
                extend.disabled = true;
                api("/api/ewaybill/" + encodeURIComponent(ewbID) + "/extend", {
                    method: "POST", body: { valid_upto_hours: 4 },
                }).then(function () {
                    toast("E-way bill extended 4h");
                    if (state.selected) { selectVehicle(state.selected); }
                }).catch(function () {
                    feedback("EWB extension failed — try again or use the compliance page.", true);
                }).finally(function () { extend.disabled = false; });
            });
        }

        var share = $("ctx-share");
        if (share) {
            share.addEventListener("click", function () {
                var tripID = ($("ctx-trip-actions") || {}).dataset ? $("ctx-trip-actions").dataset.tripId : "";
                if (!tripID) { return; }
                api("/trips/" + encodeURIComponent(tripID) + "/share", { method: "POST" })
                    .then(function (res) {
                        var url = res.url || res.link || "";
                        if (url && navigator.clipboard) {
                            navigator.clipboard.writeText(url).then(function () {
                                toast("Tracking link copied");
                            });
                        } else if (url) {
                            toast("Share link: " + url);
                        }
                    })
                    .catch(function () { feedback("Could not create share link.", true); });
            });
        }

        var settle = $("ctx-settle");
        if (settle) {
            settle.addEventListener("click", function () {
                var tripID = ($("ctx-trip-actions") || {}).dataset ? $("ctx-trip-actions").dataset.tripId : "";
                if (tripID) { window.location.href = "/settlements?trip=" + encodeURIComponent(tripID); }
            });
        }
    }

    // ── money strip refresh ─────────────────────────────────────────
    function refreshMoneyStrip() {
        api("/api/dashboard/money-strip").then(function (m) {
            var cells = {
                revenue: document.querySelector("[data-strip='revenue']"),
                spent: document.querySelector("[data-strip='spent']"),
                receivables: document.querySelector("[data-strip='receivables']"),
            };
            if (cells.revenue) { cells.revenue.textContent = fmtMoney(m.revenue); }
            if (cells.spent) { cells.spent.textContent = fmtMoney(m.spent); }
            if (cells.receivables) { cells.receivables.textContent = fmtMoney(m.receivables); }
        }).catch(function () { /* strip keeps last values */ });
    }

    // ── ⌘K palette ──────────────────────────────────────────────────
    function initPalette() {
        var overlay = $("search-palette");
        var input = $("palette-input");
        var results = $("palette-results");
        var trigger = $("palette-trigger");
        if (!overlay || !input || !results) { return; }

        function open() {
            overlay.classList.remove("hidden");
            input.value = "";
            results.innerHTML = "Type to search across the fleet.";
            input.focus();
        }
        function close() { overlay.classList.add("hidden"); }

        if (trigger) { trigger.addEventListener("click", open); }
        if ($("palette-backdrop")) { $("palette-backdrop").addEventListener("click", close); }
        document.addEventListener("keydown", function (e) {
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
                e.preventDefault();
                overlay.classList.contains("hidden") ? open() : close();
            }
            if (e.key === "Escape") { close(); }
        });

        var timer = null;
        input.addEventListener("input", function () {
            clearTimeout(timer);
            var q = input.value.trim();
            if (q.length < 2) { results.textContent = "Type to search across the fleet."; return; }
            timer = setTimeout(function () {
                api("/api/search?q=" + encodeURIComponent(q)).then(function (data) {
                    results.innerHTML = "";
                    var groups = [
                        ["vehicles", "Vehicles"],
                        ["drivers", "Drivers"],
                        ["bookings", "Bookings"],
                        ["invoices", "Invoices"],
                        ["eway_bills", "E-way bills"],
                    ];
                    var any = false;
                    groups.forEach(function (g) {
                        var items = data[g[0]] || [];
                        if (!items.length) { return; }
                        any = true;
                        var head = document.createElement("p");
                        head.className = "px-3 pt-2 pb-1 text-[10px] uppercase tracking-wide text-text-muted";
                        head.textContent = g[1];
                        results.appendChild(head);
                        items.slice(0, 5).forEach(function (item) {
                            var a = document.createElement("a");
                            a.className = "block px-3 py-1.5 rounded-md hover:bg-surface-container text-on-surface";
                            if (item.href) {
                                a.href = item.href;
                                a.addEventListener("click", close);
                            } else {
                                a.href = "#";
                            }
                            a.innerHTML = '<span class="font-medium">' + esc(item.title) + "</span>" +
                                (item.sub ? ' <span class="text-text-muted">· ' + esc(item.sub) + "</span>" : "");
                            results.appendChild(a);
                        });
                    });
                    if (!any) { results.textContent = "No matches."; }
                }).catch(function () { results.textContent = "Search failed."; });
            }, 200);
        });
    }

    // ── mobile bottom sheet (<lg via max-lg classes) ────────────────
    function sheetEl() { return document.getElementById("context-panel"); }
    function closeMobileSheet() {
        var el = sheetEl();
        if (el) { el.classList.add("max-lg:translate-y-full"); }
    }
    function openMobileSheet() {
        if (window.innerWidth >= 1024) { return; }
        var el = sheetEl();
        if (el) {
            el.classList.remove("max-lg:translate-y-full");
            var closeBtn = document.getElementById("ctx-sheet-close");
            if (closeBtn && !closeBtn.dataset.wired) {
                closeBtn.dataset.wired = "1";
                closeBtn.addEventListener("click", closeMobileSheet);
            }
        }
    }

    // ── boot ────────────────────────────────────────────────────────
    document.addEventListener("DOMContentLoaded", function () {
        if (!$("fleet-cards")) { return; } // not on the console page
        initMobileSheet();
        loadFleet();
        wireActions();
        initPalette();
        setInterval(refreshMoneyStrip, 60000);
    });
})();
