import React, { useRef, useEffect } from 'react';
import { StyleSheet, View, Text, TouchableOpacity, Linking } from 'react-native';
import { WebView } from 'react-native-webview';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius } from '../constants/theme';
import {
  DEFAULT_DESTINATION_LATITUDE,
  DEFAULT_DESTINATION_LONGITUDE,
  DEFAULT_LATITUDE,
  DEFAULT_LONGITUDE,
} from '../constants/network';

interface LiveDriverTrackingMapProps {
  driverLatitude: number;
  driverLongitude: number;
  pickupLatitude?: number;
  pickupLongitude?: number;
  destinationLatitude?: number;
  destinationLongitude?: number;
  pickupLabel?: string;
  destinationLabel?: string;
  vehicleLabel?: string;
  speedKmh?: number;
  height?: number;
  onOpenExternalNav?: () => void;
}

export function LiveDriverTrackingMap({
  driverLatitude,
  driverLongitude,
  pickupLatitude = 18.9500, // JNPT Port, Navi Mumbai
  pickupLongitude = 72.9500,
  destinationLatitude = 18.7500, // Chakan MIDC, Pune
  destinationLongitude = 73.8500,
  pickupLabel = 'JNPT Port, Navi Mumbai',
  destinationLabel = 'Chakan MIDC, Pune',
  vehicleLabel = 'DL-01-AB-1234',
  speedKmh = 48,
  height = 280,
  onOpenExternalNav,
}: LiveDriverTrackingMapProps) {
  const webViewRef = useRef<any>(null);

  // Generate Leaflet HTML with OpenStreetMap tiles, custom markers and polyline
  const leafletHTML = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no" />
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <style>
    html, body, #map {
      margin: 0;
      padding: 0;
      width: 100%;
      height: 100%;
      background: #0f172a;
      overflow: hidden;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    }
    .leaflet-control-attribution {
      display: none !important;
    }
    .custom-div-icon {
      background: transparent;
      border: none;
    }
    .truck-marker {
      position: relative;
      display: flex;
      align-items: center;
      justify-content: center;
      width: 36px;
      height: 36px;
      border-radius: 50%;
      background: #008069;
      border: 2px solid #ffffff;
      box-shadow: 0 3px 10px rgba(0,0,0,0.5);
    }
    .truck-pulse {
      position: absolute;
      width: 48px;
      height: 48px;
      border-radius: 50%;
      background: rgba(37, 211, 102, 0.4);
      animation: pulse 2s infinite ease-out;
      pointer-events: none;
    }
    @keyframes pulse {
      0% { transform: scale(0.6); opacity: 1; }
      100% { transform: scale(1.6); opacity: 0; }
    }
    .pin-badge {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 3px 8px;
      border-radius: 12px;
      font-size: 10px;
      font-weight: 800;
      color: #fff;
      white-space: nowrap;
      box-shadow: 0 2px 6px rgba(0,0,0,0.4);
      border: 1px solid rgba(255,255,255,0.6);
    }
    .pin-pickup { background: #059669; }
    .pin-dest { background: #dc2626; }
  </style>
</head>
<body>
  <div id="map"></div>
  <script>
    var map = L.map('map', {
      zoomControl: false,
      attributionControl: false
    }).setView([${driverLatitude}, ${driverLongitude}], 11);

    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 18,
    }).addTo(map);

    // Route coordinates
    var pickup = [${pickupLatitude}, ${pickupLongitude}];
    var driver = [${driverLatitude}, ${driverLongitude}];
    var dest = [${destinationLatitude}, ${destinationLongitude}];

    // Draw route polyline
    var routeLine = L.polyline([pickup, driver, dest], {
      color: '#008069',
      weight: 5,
      opacity: 0.85,
      dashArray: '8, 6',
      lineJoin: 'round'
    }).addTo(map);

    // Pickup Marker
    var pickupIcon = L.divIcon({
      className: 'custom-div-icon',
      html: '<div class="pin-badge pin-pickup">📦 ORIGIN</div>',
      iconSize: [60, 20],
      iconAnchor: [30, 25]
    });
    L.marker(pickup, { icon: pickupIcon }).addTo(map);

    // Destination Marker
    var destIcon = L.divIcon({
      className: 'custom-div-icon',
      html: '<div class="pin-badge pin-dest">🏭 DESTINATION</div>',
      iconSize: [80, 20],
      iconAnchor: [40, 25]
    });
    L.marker(dest, { icon: destIcon }).addTo(map);

    // Live Truck Marker
    var truckIcon = L.divIcon({
      className: 'custom-div-icon',
      html: '<div class="truck-pulse"></div><div class="truck-marker"><span style="font-size:16px;">🚛</span></div>',
      iconSize: [36, 36],
      iconAnchor: [18, 18]
    });
    var truckMarker = L.marker(driver, { icon: truckIcon }).addTo(map);

    // Auto-fit bounds with padding
    var bounds = L.latLngBounds([pickup, driver, dest]);
    map.fitBounds(bounds, { padding: [40, 40] });

    // Handle updates from React Native
    window.updatePosition = function(lat, lng) {
      if (truckMarker) {
        truckMarker.setLatLng([lat, lng]);
        routeLine.setLatLngs([pickup, [lat, lng], dest]);
      }
    };

    window.recenter = function() {
      if (truckMarker) {
        map.setView(truckMarker.getLatLng(), 13, { animate: true });
      }
    };
  </script>
</body>
</html>
  `;

  // Push updated coordinates to WebView
  useEffect(() => {
    if (webViewRef.current) {
      const script = `if (window.updatePosition) { window.updatePosition(${driverLatitude}, ${driverLongitude}); }`;
      webViewRef.current.injectJavaScript(script);
    }
  }, [driverLatitude, driverLongitude]);

  const handleRecenter = () => {
    if (webViewRef.current) {
      webViewRef.current.injectJavaScript('if (window.recenter) { window.recenter(); }');
    }
  };

  const handleExternalNav = () => {
    if (onOpenExternalNav) {
      onOpenExternalNav();
      return;
    }
    const dest = encodeURIComponent(destinationLabel || 'Pune');
    const url = `google.navigation:q=${dest}`;
    Linking.canOpenURL(url).then((supported) => {
      if (supported) {
        Linking.openURL(url);
      } else {
        Linking.openURL(`https://www.google.com/maps/dir/?api=1&destination=${dest}`);
      }
    }).catch(() => {
      Linking.openURL(`https://www.google.com/maps/dir/?api=1&destination=${dest}`);
    });
  };

  return (
    <View style={[styles.container, { height }]}>
      <WebView
        ref={webViewRef}
        originWhitelist={['*']}
        source={{ html: leafletHTML }}
        style={{ width: '100%', height: height, backgroundColor: '#0f172a' }}
        containerStyle={{ width: '100%', height: height, backgroundColor: '#0f172a' }}
        javaScriptEnabled={true}
        domStorageEnabled={true}
        scrollEnabled={false}
        mixedContentMode="always"
        androidHardwareAccelerationDisabled={false}
      />

      {/* Top Status Header Overlay */}
      <View style={styles.topOverlay}>
        <View style={styles.livePill}>
          <View style={styles.pulseDot} />
          <Text style={styles.liveText}>LIVE GPS</Text>
        </View>
        <View style={styles.speedPill}>
          <MaterialCommunityIcons name="speedometer" size={12} color="#ffffff" />
          <Text style={styles.speedText}>{speedKmh} KM/H</Text>
        </View>
        <Text style={styles.vehicleText}>{vehicleLabel}</Text>
      </View>

      {/* Bottom Floating Control Toolbar */}
      <View style={styles.bottomControls}>
        <TouchableOpacity
          style={styles.recenterBtn}
          activeOpacity={0.8}
          onPress={handleRecenter}
          accessibilityLabel="Center on Vehicle"
        >
          <MaterialCommunityIcons name="crosshairs-gps" size={18} color="#008069" />
          <Text style={styles.recenterBtnText}>RECENTER</Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.navActionBtn}
          activeOpacity={0.85}
          onPress={handleExternalNav}
          accessibilityLabel="Turn-by-turn Navigation"
        >
          <MaterialCommunityIcons name="navigation-variant" size={18} color="#ffffff" />
          <Text style={styles.navActionBtnText}>TURN-BY-TURN</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    width: '100%',
    borderRadius: Radius.lg,
    overflow: 'hidden',
    backgroundColor: '#0f172a',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.12)',
    position: 'relative',
    elevation: 4,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.25,
    shadowRadius: 4,
  },
  webView: {
    width: '100%',
    height: '100%',
    backgroundColor: '#0f172a',
  },
  topOverlay: {
    position: 'absolute',
    top: 10,
    left: 10,
    right: 10,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  livePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    backgroundColor: 'rgba(0, 128, 105, 0.92)',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
  pulseDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#25d366',
  },
  liveText: {
    color: '#ffffff',
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
  speedPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: 'rgba(15, 23, 42, 0.85)',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.15)',
  },
  speedText: {
    color: '#ffffff',
    fontSize: 10,
    fontWeight: '800',
    fontFamily: Font.mono,
  },
  vehicleText: {
    marginLeft: 'auto',
    color: '#ffffff',
    fontSize: 10,
    fontWeight: '800',
    backgroundColor: 'rgba(15, 23, 42, 0.85)',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.15)',
  },
  bottomControls: {
    position: 'absolute',
    bottom: 10,
    left: 10,
    right: 10,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  recenterBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    backgroundColor: '#ffffff',
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 8,
    elevation: 3,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.2,
    shadowRadius: 2,
  },
  recenterBtnText: {
    color: '#008069',
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  navActionBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#008069',
    paddingVertical: 8,
    paddingHorizontal: 12,
    borderRadius: 8,
    elevation: 3,
  },
  navActionBtnText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
});
