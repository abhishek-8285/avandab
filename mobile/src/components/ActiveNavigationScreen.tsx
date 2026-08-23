import React from 'react';
import { StyleSheet, Text, View, TouchableOpacity, Alert } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { LiveDriverTrackingMap } from './LiveDriverTrackingMap';
import { EWayBillCard } from './EWayBillCard';
import { Telemetry } from '../services/telemetry';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { Trip } from '../types/api';
import { deriveNavState } from '../utils/navState';

interface ActiveNavigationScreenProps {
  tripId?: string;
  trip?: Trip;
  onArriveAtStop: () => void;
  onMenuToggle?: () => void;
}

export function ActiveNavigationScreen({
  tripId,
  trip: initialTrip,
  onArriveAtStop,
  onMenuToggle,
}: ActiveNavigationScreenProps) {
  const [trip, setTrip] = React.useState<Trip | undefined>(initialTrip);
  const [startingTrip, setStartingTrip] = React.useState(false);
  const [coords, setCoords] = React.useState<{ latitude: number; longitude: number; speedKmh: number | null }>({
    latitude: 18.5204,
    longitude: 73.8567,
    speedKmh: null,
  });

  React.useEffect(() => {
    Telemetry.startLiveLocationTracking((lat, lng, speedKmh) => {
      setCoords({ latitude: lat, longitude: lng, speedKmh: speedKmh ?? null });
    });
    return () => {
      Telemetry.stopLiveLocationTracking();
    };
  }, []);

  const nav = deriveNavState(trip ?? (tripId ? ({ id: tripId } as Trip) : null), coords.speedKmh);

  // Assigned trips must transition to `started` before the backend accepts an
  // e-POD delivery ("only active/started/in_transit trips can be marked delivered").
  const startTrip = async () => {
    if (!tripId || startingTrip) return;
    setStartingTrip(true);
    try {
      const token = useAuthStore.getState().token;
      const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${tripId}/start`, {
        method: 'POST',
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      if (trip) {
        setTrip({ ...trip, status: 'IN_TRANSIT' });
      }
    } catch (e: any) {
      Alert.alert('Could Not Start Trip', e?.message || 'Failed to start trip.');
    } finally {
      setStartingTrip(false);
    }
  };

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      {/* Map background */}
      <View style={styles.mapContainer}>
        <LiveDriverTrackingMap
          driverLatitude={coords.latitude}
          driverLongitude={coords.longitude}
          pickupLabel={trip?.origin}
          destinationLabel={trip?.destination}
          vehicleLabel={trip?.vehiclePlate ? `Vehicle #${trip.vehiclePlate}` : undefined}
        />
      </View>

      {/* Top dark HUD bar */}
      <View style={styles.topAppBar}>
        <TouchableOpacity style={styles.iconBtn} onPress={onMenuToggle}>
          <MaterialCommunityIcons name="menu" size={18} color={Colors.textOnChrome} />
        </TouchableOpacity>

        <View style={styles.brandBlock}>
          <Text style={styles.brandTitle}>NAV</Text>
          <Text style={styles.brandSub}>{nav.statusLine}</Text>
        </View>

        <TouchableOpacity style={styles.iconBtn}>
          <MaterialCommunityIcons name="bell-outline" size={16} color={Colors.textOnChrome} />
        </TouchableOpacity>
      </View>

      {/* Leg instruction HUD card — real leg data only, no turn-by-turn fabrication */}
      <View style={styles.instructionContainer}>
        <View style={styles.turnCard}>
          <View style={styles.turnIconBox}>
            <MaterialCommunityIcons
              name={nav.hasTrip ? 'map-marker-right' : 'map-marker-question'}
              size={28}
              color={Colors.textOnPrimary}
            />
          </View>

          <View style={styles.turnTextContainer}>
            <Text style={styles.turnDistance}>{nav.stepLabel}</Text>
            <Text style={styles.turnTitle}>{nav.legTitle}</Text>
            <Text style={styles.turnSubtitle} numberOfLines={1}>
              {nav.nextStopAddress}
            </Text>
          </View>
        </View>

        {/* Speed HUD — real GPS speed only */}
        <View style={styles.speedRow}>
          <View style={styles.speedBadge}>
            <Text style={styles.currentSpeed}>{nav.speedKmh != null ? nav.speedKmh : '--'}</Text>
            <Text style={styles.speedUnit}>KM/H</Text>
          </View>
        </View>
      </View>

      {/* Bottom delivery card */}
      <View style={styles.bottomCardContainer}>
        {tripId ? <EWayBillCard tripId={tripId} /> : null}
        <View style={styles.bottomCard}>
          <View style={styles.cardHeader}>
            <View style={styles.stopInfo}>
              <View style={styles.indicatorRow}>
                <View style={styles.greenDot} />
                <Text style={styles.stopLabel}>{nav.stepLabel}</Text>
              </View>
              <Text style={styles.stopAddress} numberOfLines={1}>
                {nav.nextStopAddress}
              </Text>
              {nav.refLabel && <Text style={styles.stopRef}>{nav.refLabel}</Text>}
            </View>

            <View style={styles.etaContainer}>
              <Text style={styles.etaLabel}>DEPART</Text>
              <Text style={styles.etaTime}>{trip?.startTime ? formatDeparture(trip.startTime) : '—'}</Text>
              <Text style={styles.etaDistance}>{trip?.vehiclePlate || ''}</Text>
            </View>
          </View>

          {trip && trip.status === 'PENDING' ? (
            <TouchableOpacity
              style={[styles.arriveBtn, styles.startBtn]}
              activeOpacity={0.88}
              onPress={startTrip}
              disabled={startingTrip || !nav.hasTrip}
            >
              <MaterialCommunityIcons name="play-circle-outline" size={16} color={Colors.textOnPrimary} />
              <Text style={styles.arriveBtnText}>{startingTrip ? 'STARTING…' : 'START TRIP'}</Text>
            </TouchableOpacity>
          ) : (
            <TouchableOpacity
              style={[styles.arriveBtn, !nav.hasTrip && { opacity: 0.5 }]}
              activeOpacity={0.88}
              onPress={onArriveAtStop}
              disabled={!nav.hasTrip}
            >
              <MaterialCommunityIcons name="map-marker-check" size={16} color={Colors.textOnPrimary} />
              <Text style={styles.arriveBtnText}>ARRIVE AT STOP</Text>
            </TouchableOpacity>
          )}
        </View>
      </View>
    </View>
  );
}

/** Formats an ISO departure timestamp as HH:mm for the HUD. */
function formatDeparture(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso.length <= 5 ? iso : '—';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.chrome,
  },
  mapContainer: {
    ...StyleSheet.absoluteFillObject,
  },
  topAppBar: {
    position: 'absolute',
    top: 48,
    left: Spacing.md,
    right: Spacing.md,
    height: 48,
    backgroundColor: Colors.chrome,
    borderRadius: Radius.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 10,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    zIndex: 50,
  },
  iconBtn: {
    width: 32,
    height: 32,
    borderRadius: Radius.sm,
    backgroundColor: Colors.chromeLight,
    alignItems: 'center',
    justifyContent: 'center',
  },
  brandBlock: {
    alignItems: 'center',
  },
  brandTitle: {
    fontSize: 12,
    fontWeight: '900',
    color: Colors.textOnChrome,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  brandSub: {
    fontSize: 8,
    fontWeight: '700',
    color: Colors.primary,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  instructionContainer: {
    position: 'absolute',
    top: 108,
    left: Spacing.md,
    right: Spacing.md,
    zIndex: 40,
  },
  turnCard: {
    backgroundColor: Colors.chrome,
    borderRadius: Radius.md,
    padding: Spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
  },
  turnIconBox: {
    width: 44,
    height: 44,
    borderRadius: Radius.sm,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: Spacing.md,
  },
  turnTextContainer: {
    flex: 1,
  },
  turnDistance: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.primary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  turnTitle: {
    fontSize: 16,
    fontWeight: '900',
    color: Colors.textOnChrome,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  turnSubtitle: {
    fontSize: 11,
    color: Colors.textOnChromeMuted,
    marginTop: 2,
  },
  speedRow: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    alignItems: 'center',
    marginTop: 8,
    gap: 8,
  },
  speedBadge: {
    backgroundColor: Colors.chrome,
    borderRadius: Radius.sm,
    paddingHorizontal: 10,
    paddingVertical: 6,
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: 4,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
  },
  currentSpeed: {
    fontSize: 16,
    fontWeight: '900',
    color: Colors.textOnChrome,
    fontFamily: Font.mono,
  },
  speedUnit: {
    fontSize: 9,
    color: Colors.textOnChromeMuted,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  bottomCardContainer: {
    position: 'absolute',
    bottom: Spacing.lg,
    left: Spacing.md,
    right: Spacing.md,
    zIndex: 40,
    gap: Spacing.sm,
  },
  bottomCard: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    padding: Spacing.lg,
    borderWidth: 1,
    borderColor: Colors.border,
  },
  cardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: Spacing.md,
  },
  stopInfo: {
    flex: 1,
    marginRight: Spacing.md,
  },
  indicatorRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginBottom: 4,
  },
  greenDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.primary,
  },
  stopLabel: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  stopAddress: {
    fontSize: 15,
    fontWeight: '800',
    color: Colors.textPrimary,
    marginTop: 2,
  },
  stopRef: {
    fontSize: 10,
    color: Colors.textMuted,
    fontFamily: Font.mono,
    marginTop: 2,
    letterSpacing: 0.5,
  },
  etaContainer: {
    alignItems: 'flex-end',
    borderLeftWidth: 1,
    borderColor: Colors.border,
    paddingLeft: Spacing.md,
  },
  etaLabel: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  etaTime: {
    fontSize: 20,
    fontWeight: '900',
    color: Colors.primary,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  etaDistance: {
    fontSize: 10,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  arriveBtn: {
    height: 48,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  arriveBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  startBtn: {
    backgroundColor: Colors.success,
  },
});
