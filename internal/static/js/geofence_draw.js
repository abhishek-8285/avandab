/* geofence_draw.js — Interactive Leaflet Drawing Engine for Geofences (Spec 02 §8).
 * Features:
 *   - Circle drawing: center marker drag + radius slider + quick presets + direct click
 *   - Polygon drawing: vertex placement + draggable vertex markers + undo + close ring
 *   - OpenStreetMap Nominatim geocoder search + quick city jumping
 *   - Dynamic color themes linked to Zone Kind (Pickup, Drop, Depot, Restricted, No-Entry)
 *   - Real-time geometric metrics computation (Lat/Lng center & enclosed area in km²/m²)
 */
(function () {
  'use strict';

  var config = {
    tiles: 'https://mt1.google.com/vt/lyrs=m&x={x}&y={y}&z={z}&gl=IN',
    fallbackTiles: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
    maxZoom: 19
  };

  var KIND_PALETTES = {
    pickup:     { stroke: '#10b981', fill: '#34d399', badge: 'bg-emerald-500/10 text-emerald-500' },
    drop:       { stroke: '#06b6d4', fill: '#22d3ee', badge: 'bg-cyan-500/10 text-cyan-500' },
    depot:      { stroke: '#6366f1', fill: '#818cf8', badge: 'bg-indigo-500/10 text-indigo-500' },
    restricted: { stroke: '#f59e0b', fill: '#fbbf24', badge: 'bg-amber-500/10 text-amber-500' },
    no_entry:   { stroke: '#f43f5e', fill: '#fb7185', badge: 'bg-rose-500/10 text-rose-500' }
  };

  var KIND_DESCRIPTIONS = {
    pickup: 'Pickup Zone: Dwell engine automatically transitions trips from Assigned/Ready to Reached Pickup upon confirmed entry.',
    drop: 'Drop Zone: Dwell engine tracks arrival at destination and initiates unloading/in-transit completion.',
    depot: 'Depot / Hub: Dwell engine tracks vehicle stay and computes billable detention demurrage hours beyond company free allowance.',
    restricted: 'Restricted Zone: Triggers warning alert events upon unauthorized entry during off-hours or restricted operations.',
    no_entry: 'No-Entry Zone: Triggers critical security breach alarms immediately upon vehicle boundary penetration.'
  };

  function formatArea(sqMeters) {
    if (isNaN(sqMeters) || sqMeters <= 0) return '-';
    if (sqMeters >= 1000000) {
      return (sqMeters / 1000000).toFixed(2) + ' km² (' + Math.round(sqMeters).toLocaleString() + ' m²)';
    }
    return Math.round(sqMeters).toLocaleString() + ' m²';
  }

  function computePolygonArea(coords) {
    if (!coords || coords.length < 3) return 0;
    var radius = 6378137; // Earth radius in meters
    var total = 0;
    for (var i = 0; i < coords.length; i++) {
      var j = (i + 1) % coords.length;
      var lat1 = coords[i][0] * (Math.PI / 180);
      var lng1 = coords[i][1] * (Math.PI / 180);
      var lat2 = coords[j][0] * (Math.PI / 180);
      var lng2 = coords[j][1] * (Math.PI / 180);
      total += (lng2 - lng1) * (2 + Math.sin(lat1) + Math.sin(lat2));
    }
    return Math.abs((total * radius * radius) / 2.0);
  }

  function initGeofenceDrawer(root) {
    if (!root || typeof L === 'undefined') {
      return;
    }
    // Guard: initAll re-runs on hx-boost swaps AND first load.
    if (root.dataset.drawerBound) return;
    root.dataset.drawerBound = 'true';
    var shapeInput = root.querySelector('input[name="shape"]');
    var kindSelect = root.querySelector('select[name="kind"]');
    var mode = shapeInput ? shapeInput.value : 'circle';
    var mapEl = root.querySelector('#geofence-map');
    if (!mapEl) {
      return;
    }

    var centerLat = parseFloat(mapEl.dataset.centerLat || '') || 12.9716;
    var centerLng = parseFloat(mapEl.dataset.centerLng || '') || 77.5946;
    var radiusM = parseFloat(mapEl.dataset.radiusM || '') || 500;
    var polygonJSON = mapEl.dataset.polygon || '';
    var shape = mapEl.dataset.shape || '';
    var kind = (kindSelect ? kindSelect.value : mapEl.dataset.kind) || 'depot';

    var hiddenLat = root.querySelector('input[name="center_lat"]');
    var hiddenLng = root.querySelector('input[name="center_lng"]');
    var hiddenRadius = root.querySelector('input[name="radius_m"]');
    var hiddenPolygon = root.querySelector('input[name="polygon"]');

    var radiusSlider = root.querySelector('#radius-slider');
    var radiusValue = root.querySelector('#radius-value');
    var modeButtons = root.querySelectorAll('[data-draw-mode]');
    var closeBtn = root.querySelector('#close-polygon');
    var undoBtn = root.querySelector('#undo-vertex');
    var clearBtn = root.querySelector('#clear-map');
    var modeBadge = root.querySelector('#map-mode-badge');
    var instructions = root.querySelector('#map-instructions');
    var vertexCounter = root.querySelector('#vertex-counter');
    var metricCenter = root.querySelector('#metric-center');
    var metricArea = root.querySelector('#metric-area');
    var kindInfoText = root.querySelector('#kind-info-text');

    var geocoderInput = root.querySelector('#geocoder-input');
    var geocoderBtn = root.querySelector('#geocoder-btn');
    var quickJumps = root.querySelectorAll('.quick-jump');
    var radiusPresets = root.querySelectorAll('[data-preset]');

    var map = L.map(mapEl, {
      center: [centerLat, centerLng],
      zoom: 13,
      attributionControl: false
    });

    var google = L.tileLayer(config.tiles, { maxZoom: config.maxZoom, attribution: '' });
    google.on('tileerror', function () {
      if (!this._osmLoaded) {
        this._osmLoaded = true;
        L.tileLayer(config.fallbackTiles, { maxZoom: 19, attribution: '' }).addTo(map);
      }
    });
    google.addTo(map);

    var drawLayer = L.layerGroup().addTo(map);
    var currentCircle = null;
    var currentPolygon = null;
    var centerMarker = null;
    var vertexMarkers = [];
    var vertices = [];

    function getPalette() {
      var k = (kindSelect ? kindSelect.value : kind) || 'depot';
      return KIND_PALETTES[k] || KIND_PALETTES.depot;
    }

    function clearAll() {
      drawLayer.clearLayers();
      currentCircle = null;
      currentPolygon = null;
      centerMarker = null;
      vertexMarkers = [];
      vertices = [];

      if (hiddenLat) hiddenLat.value = '';
      if (hiddenLng) hiddenLng.value = '';
      if (hiddenRadius) hiddenRadius.value = '';
      if (hiddenPolygon) hiddenPolygon.value = '';
      if (metricCenter) metricCenter.textContent = '-';
      if (metricArea) metricArea.textContent = '-';
      updateUI();
    }

    function setCircle(lat, lng, radius) {
      drawLayer.clearLayers();
      vertices = [];
      vertexMarkers = [];
      currentPolygon = null;

      var pal = getPalette();

      currentCircle = L.circle([lat, lng], {
        radius: radius,
        color: pal.stroke,
        fillColor: pal.fill,
        fillOpacity: 0.25,
        weight: 2.5
      }).addTo(drawLayer);

      // Center marker with drag handle
      centerMarker = L.marker([lat, lng], {
        draggable: true,
        icon: L.divIcon({
          className: 'geofence-center-handle',
          html: '<div class="w-6 h-6 rounded-full border-2 border-white shadow-lg flex items-center justify-center animate-pulse" style="background-color: ' + pal.stroke + ';"><div class="w-2 h-2 rounded-full bg-white"></div></div>',
          iconSize: [24, 24],
          iconAnchor: [12, 12]
        })
      }).addTo(drawLayer);

      centerMarker.on('drag', function (e) {
        var pos = e.target.getLatLng();
        if (currentCircle) {
          currentCircle.setLatLng(pos);
        }
        if (hiddenLat) hiddenLat.value = pos.lat.toFixed(6);
        if (hiddenLng) hiddenLng.value = pos.lng.toFixed(6);
        if (metricCenter) metricCenter.textContent = pos.lat.toFixed(4) + ', ' + pos.lng.toFixed(4);
      });

      if (hiddenLat) { hiddenLat.value = lat.toFixed(6); }
      if (hiddenLng) { hiddenLng.value = lng.toFixed(6); }
      if (hiddenRadius) { hiddenRadius.value = Math.round(radius); }
      if (shapeInput) { shapeInput.value = 'circle'; }
      if (hiddenPolygon) { hiddenPolygon.value = ''; }
      if (radiusValue) { radiusValue.textContent = Math.round(radius) + ' m'; }
      if (radiusSlider) { radiusSlider.value = Math.round(radius); }
      if (metricCenter) { metricCenter.textContent = lat.toFixed(4) + ', ' + lng.toFixed(4); }
      if (metricArea) {
        var area = Math.PI * radius * radius;
        metricArea.textContent = formatArea(area);
      }
      updateUI();
    }

    function renderPolygon(coords) {
      if (currentPolygon) {
        drawLayer.removeLayer(currentPolygon);
      }
      if (coords.length >= 2) {
        var pal = getPalette();
        currentPolygon = L.polygon(coords, {
          color: pal.stroke,
          fillColor: pal.fill,
          fillOpacity: 0.25,
          weight: 2.5
        }).addTo(drawLayer);
      }
    }

    function syncPolygonInput() {
      if (hiddenPolygon) {
        hiddenPolygon.value = JSON.stringify(vertices.map(function (v) { return [v[0], v[1]]; }));
      }
      if (shapeInput) { shapeInput.value = 'polygon'; }
      if (hiddenLat) { hiddenLat.value = ''; }
      if (hiddenLng) { hiddenLng.value = ''; }
      if (hiddenRadius) { hiddenRadius.value = ''; }

      if (vertices.length >= 1 && metricCenter) {
        metricCenter.textContent = vertices[0][0].toFixed(4) + ', ' + vertices[0][1].toFixed(4);
      }
      if (metricArea) {
        metricArea.textContent = formatArea(computePolygonArea(vertices));
      }
      updateUI();
    }

    function rebuildVertexMarkers() {
      for (var i = 0; i < vertexMarkers.length; i++) {
        drawLayer.removeLayer(vertexMarkers[i]);
      }
      vertexMarkers = [];

      var pal = getPalette();

      vertices.forEach(function (pt, idx) {
        var vm = L.marker([pt[0], pt[1]], {
          draggable: true,
          icon: L.divIcon({
            className: 'geofence-vertex-handle',
            html: '<div class="w-4 h-4 rounded-full border-2 border-white shadow-md flex items-center justify-center text-[9px] font-bold text-white" style="background-color: ' + pal.stroke + ';">' + (idx + 1) + '</div>',
            iconSize: [16, 16],
            iconAnchor: [8, 8]
          })
        }).addTo(drawLayer);

        vm.on('drag', function (e) {
          var p = e.target.getLatLng();
          vertices[idx] = [p.lat, p.lng];
          renderPolygon(vertices);
          syncPolygonInput();
        });

        vertexMarkers.push(vm);
      });
    }

    function addVertex(latlng) {
      currentCircle = null;
      centerMarker = null;

      vertices.push([latlng.lat, latlng.lng]);
      rebuildVertexMarkers();
      renderPolygon(vertices);
      syncPolygonInput();
    }

    function undoLastVertex() {
      if (vertices.length > 0) {
        vertices.pop();
        rebuildVertexMarkers();
        renderPolygon(vertices);
        syncPolygonInput();
      }
    }

    function updateUI() {
      for (var j = 0; j < modeButtons.length; j++) {
        var btn = modeButtons[j];
        var isCurrent = btn.dataset.drawMode === mode;
        if (isCurrent) {
          btn.className = 'draw-mode-btn flex-1 flex items-center justify-center gap-2 border border-indigo-500/40 bg-indigo-500/15 text-indigo-500 font-bold rounded-lg px-3 py-2 text-sm shadow-sm transition-all';
        } else {
          btn.className = 'draw-mode-btn flex-1 flex items-center justify-center gap-2 border border-border-subtle bg-surface-container-low text-secondary hover:text-on-surface hover:border-border-default rounded-lg px-3 py-2 text-sm transition-all';
        }
      }

      var radiusCtrl = root.querySelector('#radius-control');
      if (radiusCtrl) {
        if (mode === 'circle') {
          radiusCtrl.classList.remove('hidden');
        } else {
          radiusCtrl.classList.add('hidden');
        }
      }

      if (closeBtn) {
        if (mode === 'polygon' && vertices.length >= 3) {
          closeBtn.classList.remove('hidden');
        } else {
          closeBtn.classList.add('hidden');
        }
      }

      if (undoBtn) {
        if (mode === 'polygon' && vertices.length > 0) {
          undoBtn.classList.remove('hidden');
        } else {
          undoBtn.classList.add('hidden');
        }
      }

      if (modeBadge) {
        modeBadge.textContent = mode === 'circle' ? 'Circle Mode' : 'Polygon Mode';
      }

      if (instructions) {
        if (mode === 'circle') {
          instructions.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="w-4 h-4 text-indigo-500 shrink-0"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg><span><strong>Circle Mode:</strong> Click map to place center. Drag center handle or use slider/presets to adjust radius.</span>';
        } else {
          instructions.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="w-4 h-4 text-cyan-500 shrink-0"><polygon points="12 2 22 8.5 18 21 6 21 2 8.5 12 2"/></svg><span><strong>Polygon Mode:</strong> Click map to add boundary corners. Drag vertices to adjust shape (min 3 points).</span>';
        }
      }

      if (vertexCounter) {
        if (mode === 'polygon') {
          vertexCounter.classList.remove('hidden');
          vertexCounter.textContent = vertices.length + ' vertices';
        } else {
          vertexCounter.classList.add('hidden');
        }
      }
    }

    // Geocoding Nominatim Search
    function performGeocode() {
      var query = geocoderInput ? geocoderInput.value.trim() : '';
      if (!query) return;

      if (geocoderBtn) {
        geocoderBtn.textContent = 'Searching...';
        geocoderBtn.disabled = true;
      }

      fetch('https://nominatim.openstreetmap.org/search?format=json&limit=1&q=' + encodeURIComponent(query))
        .then(function (res) { return res.json(); })
        .then(function (data) {
          if (data && data.length > 0) {
            var lat = parseFloat(data[0].lat);
            var lon = parseFloat(data[0].lon);
            map.flyTo([lat, lon], 14, { animate: true, duration: 1.2 });
            if (mode === 'circle') {
              setCircle(lat, lon, currentCircle ? currentCircle.getRadius() : radiusM);
            }
          } else {
            alert('Location not found. Try entering a city or landmark name.');
          }
        })
        .catch(function (err) {
          console.error('Geocoding error:', err);
        })
        .finally(function () {
          if (geocoderBtn) {
            geocoderBtn.textContent = 'Search';
            geocoderBtn.disabled = false;
          }
        });
    }

    if (geocoderBtn) {
      geocoderBtn.addEventListener('click', performGeocode);
    }
    if (geocoderInput) {
      geocoderInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          performGeocode();
        }
      });
    }

    // Quick City Jumps
    quickJumps.forEach(function (chip) {
      chip.addEventListener('click', function () {
        var lat = parseFloat(this.dataset.lat);
        var lng = parseFloat(this.dataset.lng);
        if (!isNaN(lat) && !isNaN(lng)) {
          map.flyTo([lat, lng], 13, { animate: true, duration: 1.0 });
          if (mode === 'circle') {
            setCircle(lat, lng, currentCircle ? currentCircle.getRadius() : radiusM);
          }
        }
      });
    });

    // Radius presets
    radiusPresets.forEach(function (btn) {
      btn.addEventListener('click', function () {
        var r = parseInt(this.dataset.preset, 10);
        if (r && mode === 'circle' && currentCircle) {
          if (radiusSlider) radiusSlider.value = r;
          currentCircle.setRadius(r);
          if (hiddenRadius) hiddenRadius.value = r;
          if (radiusValue) radiusValue.textContent = r + ' m';
          if (metricArea) metricArea.textContent = formatArea(Math.PI * r * r);
        }
      });
    });

    // Kind dropdown listener
    if (kindSelect) {
      kindSelect.addEventListener('change', function () {
        var k = this.value;
        if (kindInfoText && KIND_DESCRIPTIONS[k]) {
          kindInfoText.textContent = KIND_DESCRIPTIONS[k];
        }
        if (mode === 'circle' && currentCircle) {
          var pal = getPalette();
          currentCircle.setStyle({ color: pal.stroke, fillColor: pal.fill });
          if (centerMarker) {
            centerMarker.setIcon(L.divIcon({
              className: 'geofence-center-handle',
              html: '<div class="w-6 h-6 rounded-full border-2 border-white shadow-lg flex items-center justify-center animate-pulse" style="background-color: ' + pal.stroke + ';"><div class="w-2 h-2 rounded-full bg-white"></div></div>',
              iconSize: [24, 24],
              iconAnchor: [12, 12]
            }));
          }
        } else if (mode === 'polygon') {
          renderPolygon(vertices);
          rebuildVertexMarkers();
        }
      });
    }

    // Edit mode: render stored shape
    if (shape === 'circle') {
      setCircle(centerLat, centerLng, radiusM);
      map.setView([centerLat, centerLng], 15);
    } else if (shape === 'polygon' && polygonJSON) {
      try {
        var parsed = JSON.parse(polygonJSON);
        if (Array.isArray(parsed) && parsed.length >= 3) {
          vertices = parsed;
          rebuildVertexMarkers();
          renderPolygon(vertices);
          syncPolygonInput();
          map.fitBounds(L.polygon(vertices).getBounds(), { padding: [30, 30] });
        }
      } catch (e) {
        // Invalid polygon — start blank
      }
    }

    map.on('click', function (e) {
      if (mode === 'polygon') {
        addVertex(e.latlng);
      } else if (mode === 'circle') {
        var r = currentCircle ? currentCircle.getRadius() : radiusM;
        setCircle(e.latlng.lat, e.latlng.lng, r);
      }
    });

    if (radiusSlider) {
      radiusSlider.addEventListener('input', function () {
        if (mode === 'circle' && currentCircle) {
          var r = parseInt(this.value, 10) || 100;
          currentCircle.setRadius(r);
          var lat = currentCircle.getLatLng().lat;
          var lng = currentCircle.getLatLng().lng;
          if (hiddenLat) { hiddenLat.value = lat.toFixed(6); }
          if (hiddenLng) { hiddenLng.value = lng.toFixed(6); }
          if (hiddenRadius) { hiddenRadius.value = r; }
          if (radiusValue) { radiusValue.textContent = r + ' m'; }
          if (metricArea) { metricArea.textContent = formatArea(Math.PI * r * r); }
        }
      });
    }

    for (var i = 0; i < modeButtons.length; i++) {
      modeButtons[i].addEventListener('click', function () {
        mode = this.dataset.drawMode;
        if (shapeInput) { shapeInput.value = mode; }
        if (mode === 'circle' && !currentCircle) {
          var center = map.getCenter();
          setCircle(center.lat, center.lng, radiusM);
        }
        updateUI();
      });
    }

    if (clearBtn) {
      clearBtn.addEventListener('click', clearAll);
    }

    if (undoBtn) {
      undoBtn.addEventListener('click', undoLastVertex);
    }

    if (closeBtn) {
      closeBtn.addEventListener('click', function () {
        if (vertices.length >= 3) {
          syncPolygonInput();
        }
      });
    }

    // Default initial circle if circle mode with no shape drawn
    if (mode === 'circle' && !currentCircle && !currentPolygon) {
      setCircle(centerLat, centerLng, radiusM);
    } else {
      updateUI();
    }

    setTimeout(function () {
      map.invalidateSize();
    }, 250);
  }

  function initAll() {
    var roots = document.querySelectorAll('[data-geofence-drawer]');
    for (var i = 0; i < roots.length; i++) {
      initGeofenceDrawer(roots[i]);
    }
  }

  document.addEventListener('DOMContentLoaded', initAll);
  document.body.addEventListener('htmx:load', initAll);
})();

