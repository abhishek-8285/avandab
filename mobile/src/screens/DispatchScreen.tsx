import React, { useEffect, useState } from 'react';
import { StyleSheet, Text, View, TouchableOpacity, Alert } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigation } from '@react-navigation/native';
import { Alert as RNAlert, Share } from 'react-native';
import { Colors, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL, getBackendHost } from '../constants/network';
import { Card } from '../components/system/Card';
import { DB } from '../services/storage';
import { Telemetry } from '../services/telemetry';
import { MQTT } from '../services/mqtt';
import { LocationService } from '../native/locationService';
import { useAuthStore } from '../stores/authStore';
import { Trip } from '../types/api';
import { mapTripStatus, RawTrip } from '../utils/tripMapper';
export function DispatchScreen() {
  const navigation = useNavigation<any>(); const { token } = useAuthStore(); const driverId = useAuthStore.getState().user?.driverId || useAuthStore.getState().user?.id || '';
  const [locationGranted, setLocationGranted] = useState(false);
  const { data: trips } = useQuery<Trip[]>({ queryKey: ['trips', driverId, token], queryFn: async () => { if (!token) return []; try { const res = await fetch(`${getApiBaseURL()}/api/v1/trips?driver_id=me&page=1&limit=50`, { headers: { Authorization: `Bearer ${token}` } }); if (res.status === 401) { useAuthStore.getState().logout(); return []; } if (res.ok) { const json = await res.json(); const mapped = ((json.trips as RawTrip[]) || []).map(mapTripStatus); if (mapped.length > 0) await DB.saveTrips(mapped); return mapped; } } catch {} return await DB.getTrips(); }, enabled: !!token });
  const activeTrip = trips?.find((t) => t.status === 'IN_TRANSIT') ?? trips?.find((t) => t.status === 'PENDING') ?? null;
  const detentionMins = (() => {
    if (!activeTrip?.startTime) return 0;
    const d = new Date(activeTrip.startTime);
    if (isNaN(d.getTime())) return 0;
    const mins = Math.floor((Date.now() - d.getTime()) / 60000);
    return mins > 30 ? mins - 30 : 0;
  })();
  const detentionCharge = detentionMins > 0 ? Math.round(detentionMins * 5) : 0;
  useEffect(() => {
    LocationService.requestPermissions().then((s) => setLocationGranted(s.granted)).catch(() => {});
  }, []);
  const handleEnableLocation = async () => {
    const perms = await LocationService.requestPermissions();
    if (perms.granted) {
      setLocationGranted(true);
      const loc = await Telemetry.requestLocationPermission();
      if (driverId && loc.latitude && loc.longitude && LocationService.shouldAcceptFix(null, null)) {
        MQTT.publishLocation(driverId, loc.latitude, loc.longitude);
      }
      Telemetry.startLiveLocationTracking((lat, lng, speedKmh) => {
        if (!driverId) return;
        if (!LocationService.shouldAcceptFix(null, speedKmh ?? null)) return;
        MQTT.publishLocation(driverId, lat, lng);
      });
    } else Alert.alert('Location', perms.error || 'Permission denied');
  };
  const onStartNav = (trip: Trip) => navigation.navigate('ActiveNavigation', { tripId: trip.id, trip });
  const onOpenExpenses = (tripId?: string) => navigation.navigate('Expenses', { tripId });
  const onOpenIssues = () => navigation.navigate('Issues', {});
  const shareLiveLink = async () => {
    if (!activeTrip) return;
    const url = `https://${getBackendHost()}/t/${activeTrip.id}`;
    try {
      await Share.share({ message: `Live tracking ${activeTrip.tripNumber}: ${url}`, url });
    } catch {}
  };
  return (<View style={styles.container}>{activeTrip ? (<Card><View style={styles.infoCardHeader}><Text style={styles.infoTitle}>ACTIVE TRIP</Text><Text style={styles.infoMeta}>{activeTrip.tripNumber}</Text></View><View style={styles.routeContainer}><View style={styles.routeRow}><View style={[styles.routeDot, styles.routeDotOrigin]} /><Text style={styles.locationText} numberOfLines={1}>{activeTrip.origin}</Text></View><View style={styles.routeConnector} /><View style={styles.routeRow}><View style={[styles.routeDot, styles.routeDotDest]} /><Text style={styles.locationText} numberOfLines={1}>{activeTrip.destination}</Text></View></View>{detentionCharge > 0 ? (<View style={{ marginTop: Spacing.sm, backgroundColor: Colors.warningBg, borderRadius: Radius.sm, padding: Spacing.sm, flexDirection: 'row', justifyContent: 'space-between' }}><Text style={{ fontSize: 10, fontWeight: '800', color: Colors.warning }}>DETENTION {detentionMins}MIN</Text><Text style={{ fontSize: 10, fontWeight: '800', color: Colors.warning }}>₹{detentionCharge}</Text></View>) : null}<View style={{ flexDirection: 'row', gap: 8, marginTop: Spacing.md }}><TouchableOpacity style={[styles.actionBtn, styles.actionBtnTeal, { flex: 1 }]} onPress={() => onStartNav(activeTrip)}><MaterialCommunityIcons name="navigation" size={14} color={Colors.textOnPrimary} /><Text style={styles.actionBtnText}>NAVIGATE</Text></TouchableOpacity><TouchableOpacity style={[styles.actionBtn, { flex: 1, backgroundColor: Colors.surface, borderWidth: 1, borderColor: Colors.border }]} onPress={() => onOpenExpenses(activeTrip.id)}><MaterialCommunityIcons name="receipt" size={14} color={Colors.primary} /><Text style={[styles.actionBtnText, { color: Colors.primary }]}>EXPENSE</Text></TouchableOpacity></View><View style={{ flexDirection: 'row', gap: 8, marginTop: 8 }}><TouchableOpacity style={[styles.actionBtn, { flex: 1, backgroundColor: Colors.surface, borderWidth: 1, borderColor: Colors.border }]} onPress={onOpenIssues}><MaterialCommunityIcons name="alert-circle-outline" size={14} color={Colors.warning} /><Text style={[styles.actionBtnText, { color: Colors.textPrimary }]}>REPORT ISSUE</Text></TouchableOpacity><TouchableOpacity style={[styles.actionBtn, { flex: 1, backgroundColor: Colors.surface, borderWidth: 1, borderColor: Colors.border }]} onPress={shareLiveLink}><MaterialCommunityIcons name="share-variant" size={14} color={Colors.primary} /><Text style={[styles.actionBtnText, { color: Colors.primary }]}>SHARE LIVE</Text></TouchableOpacity></View></Card>) : (<Card><Text style={styles.infoTitle}>NO ACTIVE TRIP</Text><Text style={styles.infoBody}>You have no dispatched trips. Pull to refresh or contact dispatch.</Text></Card>)}<Card><View style={styles.telemetryRow}><Text style={styles.telemetryLabel}>GPS</Text><View style={[styles.statusPill, locationGranted ? styles.statusPillActive : styles.statusPillPending]}><View style={[styles.statusPillDot, { backgroundColor: locationGranted ? Colors.success : Colors.warning }]} /><Text style={[styles.telemetryValue, { color: locationGranted ? Colors.success : Colors.warning }]}>{locationGranted ? 'ON' : 'OFF'}</Text></View></View>{!locationGranted && (<TouchableOpacity style={[styles.actionBtn, { marginTop: Spacing.sm }]} onPress={handleEnableLocation}><MaterialCommunityIcons name="crosshairs-gps" size={14} color={Colors.textOnPrimary} /><Text style={styles.actionBtnText}>ENABLE LOCATION</Text></TouchableOpacity>)}<Text style={[styles.hint, { marginTop: Spacing.sm }]}>Diagnostics in Profile.</Text></Card></View>);
}
const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.background, padding: Spacing.lg, gap: Spacing.md },
  infoCardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 },
  infoTitle: { fontSize: 12, fontWeight: '800', color: Colors.textPrimary, letterSpacing: 1.5 },
  infoMeta: { fontSize: 9, fontWeight: '700', color: Colors.textMuted, letterSpacing: 1 },
  infoBody: { fontSize: 12, color: Colors.textSecondary, lineHeight: 18, marginBottom: Spacing.md },
  routeContainer: { gap: 0 }, routeRow: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  routeDot: { width: 8, height: 8, borderRadius: 2 }, routeDotOrigin: { backgroundColor: Colors.success }, routeDotDest: { backgroundColor: Colors.danger },
  locationText: { fontSize: 12, fontWeight: '700', color: Colors.textPrimary, flex: 1 },
  routeConnector: { width: 1, height: 10, backgroundColor: Colors.border, marginLeft: 3.5 },
  hint: { fontSize: 10, color: Colors.textMuted, fontStyle: 'italic' },
  telemetryRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  telemetryLabel: { fontSize: 10, fontWeight: '800', color: Colors.textPrimary, letterSpacing: 1 },
  statusPill: { flexDirection: 'row', alignItems: 'center', gap: 5, paddingHorizontal: 8, paddingVertical: 3, borderRadius: 9999 },
  statusPillActive: { backgroundColor: Colors.successBg }, statusPillPending: { backgroundColor: Colors.warningBg },
  statusPillDot: { width: 5, height: 5, borderRadius: 2.5 },
  telemetryValue: { fontSize: 9, fontWeight: '800', letterSpacing: 1 },
  actionBtn: { backgroundColor: Colors.chrome, paddingVertical: 12, paddingHorizontal: 14, borderRadius: Radius.sm, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 6 },
  actionBtnTeal: { backgroundColor: Colors.primary }, actionBtnText: { color: Colors.textOnPrimary, fontSize: 11, fontWeight: '800', letterSpacing: 1.5 },
});
