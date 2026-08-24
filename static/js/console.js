/* Console fleet strip + map + context panel wiring (Spec 22 §4.1).
 * Selection follows the live SSE stream when a vehicle is selected;
 * falls back to 30s polling on stream error (Spec 04 pattern). */
(function () {
  "use strict";

  var state = { vehicles: [], selected: null, markers: {}, map: null, es: null, pollTimer: null };

  function el(id) { return document.getElementById(id); }

  function initMap() {
    if (!window.L || !el("console-map") || state.map) return;
    state.map = L.map("console-map", { zoomControl: true });
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19,
      attribution: "© OpenStreetMap",
    }).addTo(state.map);
    state.map.setView([22.9734, 78.6569], 5); // India centroid
  }

  function fmt(n, digits) {
    return Number(n).toFixed(digits === undefined ? 0 : digits);
  }

  function statusClass(status) {
    switch (status) {
      case "running": return "bg-primary/10 text-primary";
      case "maintenance": return "bg-warning-container text-on-warning-container";
      case "inactive": return "bg-surface-container-high text-text-muted";
      default: return "bg-surface-container text-on-surface"; // available
    }
  }

  function renderStrip() {
    var wrap = el("fleet-cards");
    if (!wrap) return;
    if (!state.vehicles.length) {
      wrap.innerHTML = '<p class="text-xs text-text-muted px-1">No vehicles with GPS.</p>';
      var c = el("fleet-count"); if (c) c.textContent = "0";
      return;
    }
    var count = el("fleet-count");
    if (count) count.textContent = state.vehicles.length + " vehicles";
    wrap.innerHTML = "";
    state.vehicles.forEach(function (v) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.dataset.vehicleId = v.id;
      btn.className =
        "w-full text-left rounded-lg border px-3 py-2 transition-colors cursor-pointer " +
        (state.selected === v.id
          ? "border-primary bg-primary/5"
          : "border-border-subtle bg-surface-container-lowest hover:bg-surface-container");
      var pos = v.lat !== undefined && v.lng !== undefined
        ? '<span class="text-[10px] text-text-muted">' + fmt(v.speed_kmph || 0, 0) + ' km/h</span>'
        : '<span class="text-[10px] text-error">no GPS</span>';
      btn.innerHTML =
        '<div class="flex items-center justify-between gap-2">' +
          '<span class="text-sm font-medium text-on-surface truncate">' + v.number + "</span>" +
          '<span class="shrink-0 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium ' + statusClass(v.status) + '">' + v.status + "</span>" +
        "</div>" + pos;
      btn.addEventListener("click", function () { select(v.id); });
      wrap.appendChild(btn);
    });
  }

  function upsertMarker(v) {
    if (!state.map || v.lat === undefined || v.lng === undefined) return;
    var ll = [v.lat, v.lng];
    if (!state.markers[v.id]) {
      state.markers[v.id] = L.marker(ll).addTo(state.map);
      state.markers[v.id].on("click", function () { select(v.id); });
    } else {
      state.markers[v.id].setLatLng(ll);
    }
  }

  function fitMap() {
    if (!state.map) return;
    var pts = state.vehicles
      .filter(function (v) { return v.lat !== undefined; })
      .map(function (v) { return [v.lat, v.lng]; });
    if (!pts.length) return;
    if (pts.length === 1) { state.map.setView(pts[0], 12); return; }
    state.map.fitBounds(pts, { padding: [24, 24], maxZoom: 14 });
  }

  function loadFleet() {
    fetch("/api/fleet", { credentials: "same-origin" })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
      .then(function (data) {
        state.vehicles = data.vehicles || [];
        state.vehicles.forEach(upsertMarker);
        renderStrip();
        fitMap();
      })
      .catch(function () { /* flag off or network — leave placeholder */ });
  }

  function setText(id, value) {
    var n = el(id);
    if (n) n.textContent = value;
  }

  function renderContext(ctxData) {
    setText("ctx-title", ctxData.vehicle ? ctxData.vehicle.number : "Vehicle context");
    setText("ctx-status", ctxData.vehicle ? ctxData.vehicle.status : "");

    var pos = ctxData.position
      ? fmt(ctxData.position.lat, 4) + ", " + fmt(ctxData.position.lng, 4) +
        " · " + fmt(ctxData.position.speed_kmph, 0) + " km/h"
      : "No GPS device";
    setText("ctx-position", pos);

    setText("ctx-trip", ctxData.trip
      ? ctxData.trip.route + (ctxData.trip.eta_at ? " · ETA " + new Date(ctxData.trip.eta_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "")
      : "No active trip");

    setText("ctx-driver", ctxData.driver ? ctxData.driver.name : "—");
    var call = el("ctx-call");
    if (call) {
      if (ctxData.driver && ctxData.driver.phone) {
        call.href = "tel:" + ctxData.driver.phone; // Spec 22 edge case 12: hidden without phone
        call.classList.remove("hidden");
      } else {
        call.classList.add("hidden");
      }
    }

    setText("ctx-pnl", ctxData.pnl_km_today ? "₹" + fmt(ctxData.pnl_km_today, 2) : "—");
    setText("ctx-fastag",
      ctxData.fastag_balance !== null && ctxData.fastag_balance !== undefined
        ? "₹" + fmt(ctxData.fastag_balance)
        : "—");

    var kh = el("ctx-kharcha");
    kh.innerHTML = (ctxData.kharcha_pending || []).length
      ? ctxData.kharcha_pending.map(function (k) {
          return "<li>₹" + fmt(k.amount) + " · " + k.category + "</li>";
        }).join("")
      : "<li>None</li>";

    var ewb = el("ctx-eway");
    var extendBtn = el("ctx-eway-extend");
    if (ctxData.eway_bill) {
      ewb.textContent = "#" + ctxData.eway_bill.id + " · expires " + new Date(ctxData.eway_bill.expires_at).toLocaleString();
      if (extendBtn) extendBtn.classList.remove("hidden");
    } else {
      ewb.textContent = "None active";
      if (extendBtn) extendBtn.classList.add("hidden");
    }

    var docs = el("ctx-docs");
    docs.innerHTML = (ctxData.docs_expiring || []).length
      ? ctxData.docs_expiring.map(function (d) {
          return "<li>" + d.kind + " · " + d.expires_on + "</li>";
        }).join("")
      : "<li>None</li>";
  }

  function loadContext(vehicleId) {
    fetch("/api/fleet/" + encodeURIComponent(vehicleId) + "/context", { credentials: "same-origin" })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
      .then(renderContext)
      .catch(function () { /* keep previous panel state (edge case 2) */ });
  }

  function select(vehicleId) {
    state.selected = vehicleId;
    renderStrip();
    loadContext(vehicleId);
    followOnStream(vehicleId);
    if (state.map) {
      var marker = state.markers[vehicleId];
      if (marker) state.map.panTo(marker.getLatLng());
    }
  }

  /* Live positions via existing SSE hub; poll fallback on failure. */
  function startStream() {
    if (!window.EventSource || state.es) return;
    try {
      state.es = new EventSource("/api/v1/telemetry/stream");
      state.es.onmessage = function (ev) {
        var msg;
        try { msg = JSON.parse(ev.data); } catch (e) { return; }
        applyLiveUpdate(msg);
      };
      state.es.onerror = function () {
        stopStream();
        startPolling(); // edge case 2: 30s fallback
      };
    } catch (e) { startPolling(); }
  }

  function stopStream() {
    if (state.es) { state.es.close(); state.es = null; }
  }

  function startPolling() {
    if (state.pollTimer) return;
    state.pollTimer = setInterval(loadFleet, 30000);
  }

  function applyLiveUpdate(msg) {
    if (!msg) return;
    var vid = msg.vehicle_id || (msg.payload && msg.payload.vehicle_id);
    if (!vid) return;
    var lat = msg.latitude !== undefined ? msg.latitude : (msg.lat !== undefined ? msg.lat : undefined);
    var lng = msg.longitude !== undefined ? msg.longitude : (msg.lng !== undefined ? msg.lng : undefined);
    var speed = msg.speed_kmph !== undefined ? msg.speed_kmph : msg.speed;
    var found = null;
    state.vehicles.forEach(function (v) { if (v.id === vid) found = v; });
    if (!found) { found = { id: vid, number: vid, status: "running" }; state.vehicles.push(found); }
    if (lat !== undefined && lng !== undefined) { found.lat = lat; found.lng = lng; }
    if (speed !== undefined) found.speed_kmph = speed;
    upsertMarker(found);
    renderStrip();
    if (state.selected === vid && lat !== undefined) loadContext(vid);
  }

  function init() {
    if (!el("fleet-cards")) return; // not on console page
    initMap();
    loadFleet();
    startStream();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
