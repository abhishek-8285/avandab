(function () {
    "use strict";

    var CFG = window.__DASHBOARD_CHARTS__ || {};
    // Premium ops palette — align with src/input.css --color-primary #2563eb
    var PALETTE = {
        primary: "#2563eb",
        primarySoft: "rgba(37, 99, 235, 0.10)",
        primaryMid: "rgba(37, 99, 235, 0.75)",
        teal: "#0ea5a5",
        tealSoft: "rgba(14,165,165,0.10)",
        blue: "#2563eb",
        indigo: "#6366f1",
        violet: "#8b5cf6",
        purple: "#7c3aed",
        orange: "#f97316",
        amber: "#f59e0b",
        green: "#059669",
        emerald: "#10b981",
        red: "#e11d48",
        slate: "#64748b",
        slateGrid: "rgba(148, 163, 184, 0.14)"
    };

    // Live chart instances, keyed by chart name. Filled at boot, refreshed
    // on every SSE tick via window.__updateDashboardCharts (no reload).
    var charts = {};

    function inr(v) {
        return "₹" + Number(v || 0).toLocaleString("en-IN", { maximumFractionDigits: 0 });
    }

    // Non-empty means at least one nonzero point: zero-filled series must
    // still show the friendly empty-state copy, not a flat line.
    function hasData(list, key) {
        return !!list && list.some(function (d) { return Number(d[key]) > 0; });
    }

    function setEmpty(canvasId, emptyId, isEmpty) {
        var canvas = document.getElementById(canvasId);
        var empty = document.getElementById(emptyId);
        if (!canvas) return;
        if (isEmpty) {
            canvas.style.display = "none";
            if (empty) { empty.classList.remove("hidden"); empty.style.display = "flex"; }
        } else {
            if (empty) { empty.style.display = "none"; empty.classList.add("hidden"); }
            canvas.style.display = "";
        }
    }

    function initChart(name, canvasId, emptyId, build) {
        var canvas = document.getElementById(canvasId);
        if (!canvas || typeof Chart === "undefined") return;
        var data = build();
        if (!data) {
            setEmpty(canvasId, emptyId, true);
            return;
        }
        setEmpty(canvasId, emptyId, false);
        charts[name] = new Chart(canvas, data);
    }

    function revenuePoints(list) {
        return {
            labels: list.map(function (d) { return d.Day ? d.Day.slice(5) : ""; }),
            values: list.map(function (d) { return d.Total; })
        };
    }

    function bookingPoints(list) {
        return {
            labels: list.map(function (d) { return d.Day ? d.Day.slice(5) : ""; }),
            values: list.map(function (d) { return d.Count; })
        };
    }

    var STATUS_COLORS = {
        scheduled: "#8b5cf6",
        assigned: "#6366f1",
        started: "#f97316",
        reached_pickup: "#2563eb",
        in_transit: "#0ea5a5",
        completed: "#059669",
        cancelled: "#e11d48",
        draft: "#94a3b8"
    };

    function statusEntries(counts) {
        counts = counts || {};
        var keys = Object.keys(counts).filter(function (k) { return counts[k] > 0; });
        return {
            labels: keys.map(function (k) { return k.replace(/_/g, " "); }),
            values: keys.map(function (k) { return counts[k]; }),
            colors: keys.map(function (k) { return STATUS_COLORS[k] || PALETTE.slate; })
        };
    }

    function initRevenue() {
        initChart("revenue", "chart-revenue", "chart-revenue-empty", function () {
            var list = CFG.revenueByDay || [];
            if (!hasData(list, "Total")) return null;
            var pts = revenuePoints(list);
            return {
                type: "line",
                data: {
                    labels: pts.labels,
                    datasets: [{
                        label: "Revenue",
                        data: pts.values,
                        borderColor: PALETTE.primary,
                        backgroundColor: function(ctx){
                            const c = ctx.chart.ctx;
                            const g = c.createLinearGradient(0,0,0,180);
                            g.addColorStop(0, "rgba(37,99,235,0.18)");
                            g.addColorStop(1, "rgba(37,99,235,0.00)");
                            return g;
                        },
                        fill: true,
                        tension: 0.38,
                        borderWidth: 2.5,
                        pointRadius: 0,
                        pointHoverRadius: 5,
                        pointHitRadius: 10,
                        pointBackgroundColor: PALETTE.primary,
                        pointBorderColor: "#ffffff",
                        pointBorderWidth: 2
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    interaction: { intersect: false, mode: 'index' },
                    plugins: {
                        legend: { display: false },
                        tooltip: {
                            backgroundColor: "#0f172a",
                            titleFont: { size: 11, weight: "600" },
                            bodyFont: { size: 12 },
                            padding: 10,
                            cornerRadius: 10,
                            displayColors: false,
                            callbacks: {
                                label: function (ctx) { return " " + inr(ctx.parsed.y); }
                            }
                        }
                    },
                    scales: {
                        x: { ticks: { maxTicksLimit: 8, font: { size: 10, weight: 500 }, color: "#64748b" }, grid: { display: false }, border: { display: false } },
                        y: {
                            ticks: { font: { size: 10 }, color: "#64748b", callback: function (v) { return inr(v); } },
                            grid: { color: PALETTE.slateGrid },
                            border: { display: false }
                        }
                    }
                }
            };
        });
    }

    function initStatus() {
        initChart("status", "chart-status", "chart-status-empty", function () {
            var e = statusEntries(CFG.statusCounts);
            if (e.labels.length === 0) return null;
            return {
                type: "doughnut",
                data: {
                    labels: e.labels,
                    datasets: [{
                        data: e.values,
                        backgroundColor: e.colors,
                        borderWidth: 2,
                        borderColor: "#ffffff",
                        hoverOffset: 6
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    cutout: "64%",
                    plugins: {
                        legend: {
                            position: "bottom",
                            labels: { boxWidth: 10, boxHeight: 10, font: { size: 11, weight: 500 }, color: "#475569", padding: 14, usePointStyle: true, pointStyle: "circle" }
                        },
                        tooltip: {
                            backgroundColor: "#0f172a",
                            cornerRadius: 10,
                            padding: 10,
                            titleFont: { size: 11 },
                            bodyFont: { size: 12 }
                        }
                    }
                }
            };
        });
    }

    function initBookings() {
        initChart("bookings", "chart-bookings", "chart-bookings-empty", function () {
            var list = CFG.bookingsByDay || [];
            if (!hasData(list, "Count")) return null;
            var pts = bookingPoints(list);
            return {
                type: "bar",
                data: {
                    labels: pts.labels,
                    datasets: [{
                        label: "Bookings",
                        data: pts.values,
                        backgroundColor: PALETTE.primaryMid,
                        hoverBackgroundColor: PALETTE.primary,
                        borderRadius: 8,
                        borderSkipped: false,
                        barPercentage: 0.72,
                        categoryPercentage: 0.78
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                        legend: { display: false },
                        tooltip: {
                            backgroundColor: "#0f172a",
                            cornerRadius: 10,
                            padding: 10,
                            displayColors: false,
                            callbacks: {
                                label: function (ctx) { return " " + ctx.parsed.y + " bookings"; }
                            }
                        }
                    },
                    scales: {
                        x: { ticks: { maxTicksLimit: 8, font: { size: 10, weight: 500 }, color: "#64748b" }, grid: { display: false }, border: { display: false } },
                        y: { beginAtZero: true, ticks: { font: { size: 10 }, color: "#64748b", precision: 0 }, grid: { color: PALETTE.slateGrid }, border: { display: false } }
                    }
                }
            };
        });
    }

    // Live refresh from an SSE dashboard snapshot (TitleCase Go keys).
    // Updates series in place; toggles empty states; rebuilds a chart that
    // had no data at boot but does now.
    function refreshSeries(name, canvasId, emptyId, list, key, apply) {
        var chart = charts[name];
        if (!hasData(list, key)) {
            if (chart) { chart.destroy(); delete charts[name]; }
            setEmpty(canvasId, emptyId, true);
            return;
        }
        if (!chart) {
            // Data arrived after an empty boot: rebuild once.
            if (name === "revenue") initRevenue();
            else if (name === "bookings") initBookings();
            return;
        }
        setEmpty(canvasId, emptyId, false);
        apply(chart);
        chart.update();
    }

    window.__updateDashboardCharts = function (snap) {
        if (!snap || typeof Chart === "undefined") return;
        var counts = snap.statusCounts || snap.StatusCounts || {};
        var rev = snap.revenueByDay || snap.RevenueByDay || [];
        var bok = snap.bookingsByDay || snap.BookingsByDay || [];
        // Refresh the boot config so charts rebuilt after an empty boot
        // read the live series, not the page-load snapshot.
        CFG.statusCounts = counts;
        CFG.revenueByDay = rev;
        CFG.bookingsByDay = bok;
        refreshSeries("revenue", "chart-revenue", "chart-revenue-empty", rev, "Total", function (chart) {
            var pts = revenuePoints(rev);
            chart.data.labels = pts.labels;
            chart.data.datasets[0].data = pts.values;
        });
        refreshSeries("bookings", "chart-bookings", "chart-bookings-empty", bok, "Count", function (chart) {
            var pts = bookingPoints(bok);
            chart.data.labels = pts.labels;
            chart.data.datasets[0].data = pts.values;
        });
        var e = statusEntries(counts);
        var sc = charts["status"];
        if (e.labels.length === 0) {
            if (sc) { sc.destroy(); delete charts["status"]; }
            setEmpty("chart-status", "chart-status-empty", true);
        } else if (!sc) {
            initStatus();
        } else {
            setEmpty("chart-status", "chart-status-empty", false);
            sc.data.labels = e.labels;
            sc.data.datasets[0].data = e.values;
            sc.data.datasets[0].backgroundColor = e.colors;
            sc.update();
        }
    };

    function trackClick(target) {
        if (!window.fetch || !CFG.variant) return;
        var body = {
            experiment: "dashboard_v2",
            variant: CFG.variant,
            event: "dashboard_click",
            meta: { target: target }
        };
        fetch("/dashboard/event", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
            keepalive: true
        }).catch(function () {});
    }

    document.addEventListener("click", function (e) {
        var el = e.target.closest ? e.target.closest("[data-drill]") : null;
        if (!el) return;
        trackClick(el.getAttribute("data-drill") || "row");
    });

    function boot() {
        if (typeof Chart === "undefined") return;
        initRevenue();
        initStatus();
        initBookings();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", boot);
    } else {
        boot();
    }
})();
