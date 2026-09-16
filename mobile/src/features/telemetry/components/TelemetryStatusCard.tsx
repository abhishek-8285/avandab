import React from 'react';
import { Colors, FontSize} from '../../../constants/theme';
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
          <ActivityIndicator color={Colors.textOnPrimary} size="small" />
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
    backgroundColor: Colors.mapDark,
    borderRadius: 16,
    padding: 16,
    borderWidth: 1,
    borderColor: Colors.modalBg,
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
    fontSize: FontSize.heading,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 2,
  },
  subtitle: {
    fontSize: FontSize.body,
    color: Colors.modalSub,
  },
  badge: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
  },
  badgeActive: {
    backgroundColor: Colors.onboardingOkBg,
    borderColor: Colors.onboardingOkBorder,
  },
  badgeInactive: {
    backgroundColor: Colors.modalBg,
    borderColor: Colors.modalBorder,
  },
  badgeText: {
    fontSize: FontSize.small,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  badgeTextActive: {
    color: Colors.accent,
  },
  badgeTextInactive: {
    color: Colors.textSecondary,
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    backgroundColor: Colors.modalBg,
    borderRadius: 10,
    padding: 12,
    marginBottom: 12,
  },
  metaItem: {
    flex: 1,
  },
  metaLabel: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 2,
  },
  metaValue: {
    fontSize: FontSize.body,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  metaValueWarn: {
    color: Colors.warning,
  },
  sessionText: {
    fontSize: FontSize.small,
    color: Colors.textSecondary,
    marginBottom: 12,
  },
  btn: {
    borderRadius: 12,
    paddingVertical: 12,
    alignItems: 'center',
  },
  btnStart: {
    backgroundColor: Colors.onboardingBtn,
  },
  btnStop: {
    backgroundColor: Colors.modalBorder,
  },
  btnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.bodyLarge,
    fontWeight: '700',
  },
});
