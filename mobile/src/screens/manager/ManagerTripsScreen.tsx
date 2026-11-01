import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, FlatList, RefreshControl, ActivityIndicator } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors } from '../../constants/theme';
import { managerService, ManagerTrip } from '../../services/managerService';

export function ManagerTripsScreen() {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [trips, setTrips] = useState<ManagerTrip[]>([]);

  const loadTrips = async () => {
    try {
      const data = await managerService.fetchActiveTrips();
      setTrips(data);
    } catch {
      // Degrade gracefully
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const data = await managerService.fetchActiveTrips();
        if (active) {
          setTrips(data);
          setLoading(false);
        }
      } catch {
        if (active) {
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>Control Tower Live Trips</Text>
        <Text style={styles.headerSub}>Active dispatch monitoring & telemetry</Text>
      </View>

      {loading ? (
        <View style={styles.centerBox}>
          <ActivityIndicator size="large" color={Colors.primary} />
          <Text style={styles.loadingText}>Connecting to Control Tower...</Text>
        </View>
      ) : trips.length === 0 ? (
        <View style={styles.emptyBox}>
          <MaterialCommunityIcons name="radar" size={48} color="#cbd5e1" />
          <Text style={styles.emptyTitle}>No Active Trips</Text>
          <Text style={styles.emptySubtitle}>All fleet vehicles are currently idle or at base.</Text>
        </View>
      ) : (
        <FlatList
          data={trips}
          keyExtractor={(item) => item.trip_id || item.trip_number}
          contentContainerStyle={styles.listContent}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={loadTrips} colors={[Colors.primary]} />}
          renderItem={({ item }) => {
            const hasSOS = !!item.safety?.has_active_sos;
            const progress = item.progression?.progress_percent ?? 0;
            const speed = item.telemetry?.speed ?? 0;
            const remainingKM = item.telemetry?.remaining_km ?? 0;

            return (
              <View style={[styles.tripCard, hasSOS && styles.sosCard]} testID={`trip-card-${item.trip_number}`}>
                {/* SOS Emergency Banner */}
                {hasSOS ? (
                  <View style={styles.sosBanner}>
                    <MaterialCommunityIcons name="alarm-light" size={18} color="#ffffff" />
                    <Text style={styles.sosBannerText}>ACTIVE SOS EMERGENCY ON TRIP</Text>
                  </View>
                ) : null}

                {/* Top Info */}
                <View style={styles.cardHeader}>
                  <View>
                    <Text style={styles.tripNumber}>{item.trip_number || item.trip_id}</Text>
                    <Text style={styles.driverText}>
                      👤 {item.driver?.name || 'Driver Assigned'} • 🚛 {item.vehicle?.registration_number || 'Vehicle'}
                    </Text>
                  </View>
                  <View style={styles.statusBadge}>
                    <Text style={styles.statusBadgeText}>{(item.status || 'IN TRANSIT').toUpperCase()}</Text>
                  </View>
                </View>

                {/* Route */}
                <View style={styles.routeBox}>
                  <View style={styles.routePoint}>
                    <View style={[styles.dot, { backgroundColor: '#059669' }]} />
                    <Text style={styles.routeText} numberOfLines={1}>{item.origin || 'Origin'}</Text>
                  </View>
                  <MaterialCommunityIcons name="arrow-right-thin" size={20} color="#94a3b8" />
                  <View style={styles.routePoint}>
                    <View style={[styles.dot, { backgroundColor: '#dc2626' }]} />
                    <Text style={styles.routeText} numberOfLines={1}>{item.destination || 'Destination'}</Text>
                  </View>
                </View>

                {/* Progress Bar */}
                <View style={styles.progressContainer}>
                  <View style={styles.progressLabelRow}>
                    <Text style={styles.progressLabel}>Route Progression</Text>
                    <Text style={styles.progressValue}>{Math.round(progress)}%</Text>
                  </View>
                  <View style={styles.progressBarTrack}>
                    <View style={[styles.progressBarFill, { width: `${Math.min(100, Math.max(0, progress))}%` }]} />
                  </View>
                </View>

                {/* Telemetry Metrics */}
                <View style={styles.metricsFooter}>
                  <View style={styles.metricItem}>
                    <MaterialCommunityIcons name="speedometer" size={16} color="#64748b" />
                    <Text style={styles.metricText}>{Math.round(speed)} km/h</Text>
                  </View>
                  <View style={styles.metricItem}>
                    <MaterialCommunityIcons name="map-marker-distance" size={16} color="#64748b" />
                    <Text style={styles.metricText}>{remainingKM ? `${remainingKM.toFixed(1)} km left` : 'En Route'}</Text>
                  </View>
                  <View style={styles.metricItem}>
                    <MaterialCommunityIcons name="map-marker-check" size={16} color="#64748b" />
                    <Text style={styles.metricText}>
                      {item.progression?.completed_stops ?? 0}/{item.progression?.total_stops ?? 0} stops
                    </Text>
                  </View>
                </View>
              </View>
            );
          }}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f8fafc',
  },
  header: {
    backgroundColor: '#075e54',
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 16,
  },
  headerTitle: {
    fontSize: 18,
    fontWeight: '700',
    color: '#ffffff',
  },
  headerSub: {
    fontSize: 12,
    color: 'rgba(255,255,255,0.7)',
    marginTop: 2,
  },
  centerBox: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  loadingText: {
    marginTop: 12,
    color: '#64748b',
    fontSize: 14,
  },
  emptyBox: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: 32,
  },
  emptyTitle: {
    fontSize: 16,
    fontWeight: '700',
    color: '#475569',
    marginTop: 12,
  },
  emptySubtitle: {
    fontSize: 13,
    color: '#94a3b8',
    marginTop: 4,
    textAlign: 'center',
  },
  listContent: {
    padding: 16,
    gap: 12,
  },
  tripCard: {
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 14,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 3,
    elevation: 2,
  },
  sosCard: {
    borderColor: '#ef4444',
    borderWidth: 2,
  },
  sosBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: '#dc2626',
    marginHorizontal: -14,
    marginTop: -14,
    marginBottom: 12,
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderTopLeftRadius: 8,
    borderTopRightRadius: 8,
    gap: 6,
  },
  sosBannerText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  cardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
  },
  tripNumber: {
    fontSize: 16,
    fontWeight: '800',
    color: '#0f172a',
  },
  driverText: {
    fontSize: 12,
    color: '#64748b',
    marginTop: 2,
  },
  statusBadge: {
    backgroundColor: '#e0f2fe',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
  },
  statusBadgeText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#0284c7',
  },
  routeBox: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: '#f8fafc',
    borderRadius: 8,
    padding: 10,
    marginVertical: 12,
  },
  routePoint: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  routeText: {
    fontSize: 13,
    fontWeight: '600',
    color: '#1e293b',
  },
  progressContainer: {
    marginBottom: 10,
  },
  progressLabelRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 4,
  },
  progressLabel: {
    fontSize: 11,
    color: '#64748b',
  },
  progressValue: {
    fontSize: 11,
    fontWeight: '700',
    color: '#0f172a',
  },
  progressBarTrack: {
    height: 6,
    backgroundColor: '#e2e8f0',
    borderRadius: 3,
    overflow: 'hidden',
  },
  progressBarFill: {
    height: '100%',
    backgroundColor: '#008069',
  },
  metricsFooter: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    borderTopWidth: 1,
    borderTopColor: '#f1f5f9',
    paddingTop: 10,
  },
  metricItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  metricText: {
    fontSize: 12,
    color: '#475569',
    fontWeight: '500',
  },
});
