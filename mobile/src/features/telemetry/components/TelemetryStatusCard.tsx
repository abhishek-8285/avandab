import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet, ActivityIndicator } from 'react-native';
import { DutyState } from '../battery/batteryPolicy';

interface Props {
  isTracking: boolean;
  dutyState: DutyState;
  sessionId: string | null;
  vehicleId: string | null;
  unsyncedCount: number;
  loading?: boolean;
  onToggleTracking: () => void;
}

export const TelemetryStatusCard: React.FC<Props> = ({
  isTracking,
  dutyState,
  sessionId,
  vehicleId,
  unsyncedCount,
  loading = false,
  onToggleTracking,
}) => {
  return (
    <View style={styles.card}>
      <View style={styles.headerRow}>
        <View style={styles.titleCol}>
          <Text style={styles.title}>GPS Telemetry Engine</Text>
          <Text style={styles.subtitle}>
            {vehicleId ? `Bound Vehicle: ${vehicleId}` : 'Standby Mode'}
          </Text>
        </View>
        <View style={[styles.badge, isTracking ? styles.badgeActive : styles.badgeInactive]}>
          <Text style={[styles.badgeText, isTracking ? styles.badgeTextActive : styles.badgeTextInactive]}>
            {isTracking ? '● LIVE STREAM' : '○ OFF-DUTY'}
          </Text>
        </View>
      </View>

      <View style={styles.metaRow}>
        <View style={styles.metaItem}>
          <Text style={styles.metaLabel}>DUTY STATE</Text>
          <Text style={styles.metaValue}>{dutyState}</Text>
        </View>
        <View style={styles.metaItem}>
          <Text style={styles.metaLabel}>SQLITE BUFFER</Text>
          <Text style={[styles.metaValue, unsyncedCount > 0 && styles.metaValueWarn]}>
            {unsyncedCount === 0 ? '0 pending (synced)' : `${unsyncedCount} buffered (offline)`}
          </Text>
        </View>
      </View>

      {sessionId && (
        <Text style={styles.sessionText} numberOfLines={1}>
          Active Session: {sessionId}
        </Text>
      )}

      <TouchableOpacity
        style={[styles.btn, isTracking ? styles.btnStop : styles.btnStart]}
        onPress={onToggleTracking}
        disabled={loading}
      >
        {loading ? (
          <ActivityIndicator color="#ffffff" size="small" />
        ) : (
          <Text style={styles.btnText}>
            {isTracking ? 'Pause Background Location Tracking' : 'Start Live GPS Telemetry'}
          </Text>
        )}
      </TouchableOpacity>
    </View>
  );
};

const styles = StyleSheet.create({
  card: {
    backgroundColor: '#0f172a',
    borderRadius: 16,
    padding: 16,
    borderWidth: 1,
    borderColor: '#1e293b',
    marginHorizontal: 16,
    marginVertical: 10,
  },
  headerRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    marginBottom: 12,
  },
  titleCol: {
    flex: 1,
  },
  title: {
    fontSize: 16,
    fontWeight: '700',
    color: '#f8fafc',
    marginBottom: 2,
  },
  subtitle: {
    fontSize: 12,
    color: '#94a3b8',
  },
  badge: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
  },
  badgeActive: {
    backgroundColor: '#064e3b',
    borderColor: '#10b981',
  },
  badgeInactive: {
    backgroundColor: '#1e293b',
    borderColor: '#334155',
  },
  badgeText: {
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  badgeTextActive: {
    color: '#34d399',
  },
  badgeTextInactive: {
    color: '#64748b',
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 12,
    marginBottom: 12,
  },
  metaItem: {
    flex: 1,
  },
  metaLabel: {
    fontSize: 10,
    fontWeight: '700',
    color: '#64748b',
    marginBottom: 2,
  },
  metaValue: {
    fontSize: 12,
    fontWeight: '700',
    color: '#f8fafc',
  },
  metaValueWarn: {
    color: '#fbbf24',
  },
  sessionText: {
    fontSize: 10,
    color: '#64748b',
    marginBottom: 12,
  },
  btn: {
    borderRadius: 12,
    paddingVertical: 12,
    alignItems: 'center',
  },
  btnStart: {
    backgroundColor: '#0d9488',
  },
  btnStop: {
    backgroundColor: '#334155',
  },
  btnText: {
    color: '#ffffff',
    fontSize: 13,
    fontWeight: '700',
  },
});
