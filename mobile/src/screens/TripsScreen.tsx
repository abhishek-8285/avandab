import React, { useState, useEffect, useCallback } from 'react';
import { ScrollView, StyleSheet, Text, TouchableOpacity, View, RefreshControl, Alert } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { t, type SupportedLocale } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
import { Colors, Radius, Spacing, FontSize} from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { TripCard, SkeletonLoader } from '../components/TripCard';
import { StateView } from '../components/system/StateView';
import { DB } from '../services/storage';
import { useAuthStore } from '../stores/authStore';
import { Trip } from '../types/api';
import { mapTripStatus, RawTrip } from '../utils/tripMapper';
import { canTransition } from '../domain/trip/tripMachine';
import { withTransaction } from '../db';

interface Props {
  onStartNav: (trip: Trip) => void;
}

function slaLabel(startTime: string, locale: SupportedLocale): string | null {
  if (!startTime) return null;
  const d = new Date(startTime);
  if (isNaN(d.getTime())) return null;
  const diffMs = d.getTime() - Date.now();
  if (diffMs <= 0) return t('trips.depart_now', 'DEPART NOW', locale);
  const mins = Math.floor(diffMs / 60000);
  const unitMin = t('trips.unit_min', 'm', locale);
  const unitHr = t('trips.unit_hr', 'h', locale);
  const left = t('trips.left', 'left', locale);
  if (mins < 60) return `${mins}${unitMin} ${left}`;
  const hrs = Math.floor(mins / 60);
  return `${hrs}${unitHr} ${mins % 60}${unitMin} ${left}`;
}

export function TripsScreen({ onStartNav }: Props) {
  const locale = useLanguageStore((s) => s.locale);
  const [tripFilter, setTripFilter] = useState<'active' | 'history'>('active');
  const [actingId, setActingId] = useState<string | null>(null);
  const [, setTick] = useState(0);
  const { token } = useAuthStore();
  const driverId = useAuthStore.getState().user?.driverId || useAuthStore.getState().user?.id || '';
  const queryClient = useQueryClient();

  // SLA countdown tick every 60s
  useEffect(() => {
    const id = setInterval(() => setTick((x) => x + 1), 60000);
    return () => clearInterval(id);
  }, []);

  const { data: trips, isLoading, refetch, isRefetching } = useQuery<Trip[]>({
    queryKey: ['trips', driverId, token],
    queryFn: async () => {
      if (!token) return [];
      try {
        const res = await fetch(`${getApiBaseURL()}/api/v1/trips?driver_id=me&page=1&limit=50`, {
          headers: { Authorization: `Bearer ${token}` },
        });
        if (res.status === 401) {
          useAuthStore.getState().logout();
          return [];
        }
        if (res.ok) {
          const json = await res.json();
          const mapped = ((json.trips as RawTrip[]) || []).map(mapTripStatus);
          if (mapped.length > 0) await DB.saveTrips(mapped);
          return mapped;
        }
      } catch {}
      return await DB.getTrips();
    },
    enabled: !!token,
  });

  const handleAccept = useCallback(
    async (trip: Trip) => {
      if (!canTransition(trip.status, 'ACCEPT')) {
        Alert.alert(t('trips.alert_not_allowed', 'Not allowed', locale), t('trips.alert_bad_state', 'Trip is not in an actionable state.', locale));
        return;
      }
      setActingId(trip.id);
      const idempotencyKey = `accept-${trip.id}-${Date.now()}`;
      try {
        // Command → local Tx (optimistic) + outbox before network (offline-ready)
        await withTransaction(async (tx) => {
          await tx.runAsync(`INSERT OR IGNORE INTO outbox (command, payload, idempotency_key) VALUES (?, ?, ?)`, [
            'ACCEPT_TRIP',
            JSON.stringify({ tripId: trip.id }),
            idempotencyKey,
          ]);
        });
        const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${trip.id}/start`, {
          method: 'POST',
          headers: {
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
            'Idempotency-Key': idempotencyKey,
          },
        });
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          throw new Error(body.error || `HTTP ${res.status}`);
        }
        // Server authoritative → reconcile local DB
        await withTransaction(async (tx) => {
          await tx.runAsync(`UPDATE trips SET status=? WHERE id=?`, ['IN_TRANSIT', trip.id]);
          await tx.runAsync(`DELETE FROM outbox WHERE idempotency_key=?`, [idempotencyKey]);
        });
        queryClient.invalidateQueries({ queryKey: ['trips', driverId, token] });
        onStartNav({ ...trip, status: 'IN_TRANSIT' });
      } catch (e: any) {
        Alert.alert(t('trips.alert_accept_failed', 'Could not accept trip', locale), e?.message || t('trips.alert_accept_failed_body', 'Failed to start trip.', locale));
      } finally {
        setActingId(null);
      }
    },
    [token, driverId, queryClient, onStartNav, locale]
  );

  const handleReject = useCallback(
    async (trip: Trip) => {
      if (!canTransition(trip.status, 'CANCEL')) {
        Alert.alert(t('trips.alert_not_allowed', 'Not allowed', locale), t('trips.alert_bad_state', 'Trip is not in an actionable state.', locale));
        return;
      }
      Alert.alert(t('trips.alert_reject_title', 'Reject trip?', locale), `${trip.tripNumber} · ${trip.origin}→${trip.destination}`, [
        { text: t('trips.alert_reject_cancel', 'Cancel', locale), style: 'cancel' },
        {
          text: t('trip.reject', 'Reject', locale),
          style: 'destructive',
          onPress: async () => {
            setActingId(trip.id);
            const idempotencyKey = `cancel-${trip.id}-${Date.now()}`;
            try {
              await withTransaction(async (tx) => {
                await tx.runAsync(`INSERT OR IGNORE INTO outbox (command, payload, idempotency_key) VALUES (?, ?, ?)`, [
                  'CANCEL_TRIP',
                  JSON.stringify({ tripId: trip.id }),
                  idempotencyKey,
                ]);
              });
              const res = await fetch(`${getApiBaseURL()}/api/v1/trips/${trip.id}/cancel`, {
                method: 'POST',
                headers: {
                  ...(token ? { Authorization: `Bearer ${token}` } : {}),
                  'Idempotency-Key': idempotencyKey,
                },
              });
              if (!res.ok) {
                const body = await res.json().catch(() => ({}));
                throw new Error(body.error || `HTTP ${res.status}`);
              }
              await withTransaction(async (tx) => {
                await tx.runAsync(`UPDATE trips SET status=? WHERE id=?`, ['CANCELLED', trip.id]);
                await tx.runAsync(`DELETE FROM outbox WHERE idempotency_key=?`, [idempotencyKey]);
              });
              queryClient.invalidateQueries({ queryKey: ['trips', driverId, token] });
            } catch (e: any) {
              Alert.alert(t('trips.alert_reject_failed', 'Could not reject trip', locale), e?.message || t('trips.alert_reject_failed_body', 'Failed to cancel.', locale));
            } finally {
              setActingId(null);
            }
          },
        },
      ]);
    },
    [token, driverId, queryClient, locale]
  );

  return (
    <View style={styles.container}>
      <View style={styles.filterRow}>
        {(['active', 'history'] as const).map((f) => (
          <TouchableOpacity
            key={f}
            style={[styles.filterChip, tripFilter === f && styles.filterChipActive]}
            onPress={() => setTripFilter(f)}
          >
            <Text style={[styles.filterChipText, tripFilter === f && styles.filterChipTextActive]}>
              {f === 'active' ? t('trips.chip_active', 'ACTIVE', locale) : t('trips.chip_history', 'HISTORY', locale)}
            </Text>
          </TouchableOpacity>
        ))}
      </View>
      <ScrollView
        style={styles.content}
        contentContainerStyle={styles.contentPadding}
        refreshControl={<RefreshControl refreshing={isRefetching ?? false} onRefresh={() => refetch()} tintColor={Colors.primary} />}
      >
        {isLoading ? (
          <>
            <SkeletonLoader />
            <SkeletonLoader />
          </>
        ) : (
          (() => {
            const visibleTrips = (trips ?? []).filter((t) =>
              tripFilter === 'active' ? t.status === 'PENDING' || t.status === 'IN_TRANSIT' : t.status === 'COMPLETED' || t.status === 'CANCELLED'
            );
            if (visibleTrips.length === 0)
              return (
                <StateView
                  state="empty"
                  title={tripFilter === 'active' ? t('trips.no_active_title', 'No Active Trips', locale) : t('trips.no_history_title', 'No trip history', locale)}
                  message={tripFilter === 'active' ? t('trips.no_active_desc', 'You have no dispatched trips. Pull down to refresh.', locale) : t('trips.no_history_desc', 'Completed trips will appear here.', locale)}
                  icon={tripFilter === 'active' ? 'truck-outline' : 'history'}
                />
              );
            const grouped: Record<string, typeof visibleTrips> = {};
            visibleTrips.forEach((trip) => {
              const d = trip.startTime ? new Date(trip.startTime) : new Date();
              const key = isNaN(d.getTime())
                ? t('trips.today', 'TODAY', locale)
                : d.toLocaleDateString('en-IN', { day: '2-digit', month: 'short', year: 'numeric' }).toUpperCase();
              if (!grouped[key]) grouped[key] = [];
              grouped[key].push(trip);
            });
            return Object.entries(grouped).map(([date, group]) => (
              <View key={date}>
                <Text style={styles.dateHeader}>{date}</Text>
                {group.map((trip) => {
                  const isPending = trip.status === 'PENDING';
                  const sla = isPending ? slaLabel(trip.startTime, locale) : null;
                  const busy = actingId === trip.id;
                  return (
                    <View key={trip.id} style={styles.cardWrap}>
                      <TouchableOpacity activeOpacity={0.9} onPress={() => onStartNav(trip)}>
                        <TripCard
                          tripNumber={trip.tripNumber}
                          driverName={trip.driverName}
                          vehiclePlate={trip.vehiclePlate}
                          origin={trip.origin}
                          destination={trip.destination}
                          status={trip.status}
                          startTime={trip.startTime}
                        />
                      </TouchableOpacity>
                      {isPending ? (
                        <View style={styles.actionRow}>
                          <View style={styles.slaPill}>
                            <MaterialCommunityIcons name="clock-outline" size={12} color={Colors.warning} />
                            <Text style={styles.slaText}>{sla ?? t('trips.new_assignment', 'NEW ASSIGNMENT', locale)}</Text>
                          </View>
                          <View style={styles.actionBtns}>
                            <TouchableOpacity
                              style={[styles.rejectBtn, busy && { opacity: 0.5 }]}
                              onPress={() => handleReject(trip)}
                              disabled={busy}
                            >
                              <Text style={styles.rejectText}>{t('trip.reject', 'Reject', locale)}</Text>
                            </TouchableOpacity>
                            <TouchableOpacity
                              style={[styles.acceptBtn, busy && { opacity: 0.5 }]}
                              onPress={() => handleAccept(trip)}
                              disabled={busy}
                            >
                              <MaterialCommunityIcons name="check" size={14} color={Colors.textOnPrimary} />
                              <Text style={styles.acceptText}>{busy ? '…' : t('trip.accept', 'Accept', locale)}</Text>
                            </TouchableOpacity>
                          </View>
                        </View>
                      ) : (
                        <TouchableOpacity style={styles.navigateBtn} onPress={() => onStartNav(trip)}>
                          <MaterialCommunityIcons name="navigation" size={14} color={Colors.primary} />
                          <Text style={styles.navigateText}>{t('trips.navigate', 'NAVIGATE', locale)}</Text>
                        </TouchableOpacity>
                      )}
                    </View>
                  );
                })}
              </View>
            ));
          })()
        )}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.background },
  filterRow: { flexDirection: 'row', gap: 8, paddingHorizontal: Spacing.lg, paddingTop: Spacing.sm },
  filterChip: {
    paddingHorizontal: 12,
    paddingVertical: 5,
    borderRadius: 9999,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
  },
  filterChipActive: { backgroundColor: Colors.primary, borderColor: Colors.primary },
  filterChipText: { fontSize: FontSize.small, fontWeight: '800', letterSpacing: 1, color: Colors.textSecondary },
  filterChipTextActive: { color: Colors.textOnPrimary },
  content: { flex: 1 },
  contentPadding: { padding: Spacing.lg, gap: Spacing.md },
  dateHeader: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.textMuted,
    letterSpacing: 1,
    marginTop: Spacing.md,
    marginBottom: Spacing.sm,
    paddingHorizontal: 2,
  },
  cardWrap: {
    marginBottom: Spacing.sm,
    borderRadius: Radius.md,
    overflow: 'hidden',
  },
  actionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: Colors.surface,
    borderWidth: 1,
    borderTopWidth: 0,
    borderColor: Colors.border,
    borderBottomLeftRadius: Radius.md,
    borderBottomRightRadius: Radius.md,
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    gap: Spacing.sm,
  },
  slaPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: Colors.warningBg,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 9999,
  },
  slaText: { fontSize: FontSize.caption, fontWeight: '800', color: Colors.warning, letterSpacing: 0.5 },
  actionBtns: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  rejectBtn: {
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: Radius.sm,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
  },
  rejectText: { fontSize: FontSize.small, fontWeight: '800', color: Colors.textSecondary, letterSpacing: 1 },
  acceptBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: Radius.sm,
    backgroundColor: Colors.primary,
  },
  acceptText: { fontSize: FontSize.small, fontWeight: '800', color: Colors.textOnPrimary, letterSpacing: 1 },
  navigateBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    paddingVertical: 10,
    backgroundColor: Colors.surface,
    borderWidth: 1,
    borderTopWidth: 0,
    borderColor: Colors.border,
    borderBottomLeftRadius: Radius.md,
    borderBottomRightRadius: Radius.md,
  },
  navigateText: { fontSize: FontSize.small, fontWeight: '800', color: Colors.primary, letterSpacing: 1 },
});
