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

    function inr(v) {
        return "₹" + Number(v || 0).toLocaleString("en-IN", { maximumFractionDigits: 0 });
    }

    function hasData(list) {
        return list && list.length > 0;
    }

    function initChart(canvasId, emptyId, build) {
        var canvas = document.getElementById(canvasId);
        if (!canvas) return;
        var data = build();
        if (!data) {
            canvas.style.display = "none";
            var empty = document.getElementById(emptyId);
            if (empty) { empty.classList.remove("hidden"); empty.style.display = "flex"; }
            return;
        }
        // ensure empty hidden when we have data
        var empty2 = document.getElementById(emptyId);
        if (empty2) { empty2.style.display = "none"; empty2.classList.add("hidden"); }
        canvas.style.display = "";
        new Chart(canvas, data);
    }

    function initRevenue() {
        initChart("chart-revenue", "chart-revenue-empty", function () {
            if (!hasData(CFG.revenueByDay)) return null;
            return {
                type: "line",
                data: {
                    labels: CFG.revenueByDay.map(function (d) { return d.Day ? d.Day.slice(5) : ""; }),
                    datasets: [{
                        label: "Revenue",
                        data: CFG.revenueByDay.map(function (d) { return d.Total; }),
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
        initChart("chart-status", "chart-status-empty", function () {
            var counts = CFG.statusCounts || {};
            var entries = Object.keys(counts).filter(function (k) { return counts[k] > 0; });
            if (entries.length === 0) return null;
            var colors = {
                scheduled: "#8b5cf6",
                assigned: "#6366f1",
                started: "#f97316",
                reached_pickup: "#2563eb",
                in_transit: "#0ea5a5",
                completed: "#059669",
                cancelled: "#e11d48",
                draft: "#94a3b8"
            };
            return {
                type: "doughnut",
                data: {
                    labels: entries.map(function (k) { return k.replace(/_/g, " "); }),
                    datasets: [{
                        data: entries.map(function (k) { return counts[k]; }),
                        backgroundColor: entries.map(function (k) { return colors[k] || PALETTE.slate; }),
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
        initChart("chart-bookings", "chart-bookings-empty", function () {
            if (!hasData(CFG.bookingsByDay)) return null;
            return {
                type: "bar",
                data: {
                    labels: CFG.bookingsByDay.map(function (d) { return d.Day ? d.Day.slice(5) : ""; }),
                    datasets: [{
                        label: "Bookings",
                        data: CFG.bookingsByDay.map(function (d) { return d.Count; }),
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
