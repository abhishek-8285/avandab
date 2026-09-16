import React from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TouchableOpacity, StyleSheet, ActivityIndicator } from 'react-native';
import { ActiveTrip, TripExecutionStatus } from '../types/trip';

interface Props {
  trip: ActiveTrip;
  isProcessing: boolean;
  onAdvance: () => void;
}

export const TripExecutionCard: React.FC<Props> = ({
  trip,
  isProcessing,
  onAdvance,
}) => {
  const getActionLabel = (status: TripExecutionStatus) => {
    switch (status) {
      case 'assigned':
        return '▶ Start Trip to Pickup';
      case 'reached_pickup':
        return '📦 Begin Cargo Loading';
      case 'loading':
        return '🚚 Cargo Loaded — Start Transit';
      case 'in_transit':
        return '📍 Arrived at Delivery Destination';
      case 'reached_delivery':
        return '📥 Begin Cargo Unloading';
      case 'unloading':
        return '✅ Complete Delivery & POD';
      case 'delivered':
        return 'Trip Completed';
      default:
        return 'Advance';
    }
  };

  const isComplete = trip.status === 'delivered';

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <View>
          <Text style={styles.title}>ACTIVE TRIP #{trip.id.substring(0, 8)}</Text>
          <Text style={styles.subtitle}>{trip.origin || 'Mumbai Port'} ➔ {trip.destination || 'Pune Hub'}</Text>
        </View>
        <View style={[styles.statusBadge, isComplete ? styles.badgeComplete : styles.badgeActive]}>
          <Text style={styles.statusText}>{trip.status.toUpperCase()}</Text>
        </View>
      </View>

      <TouchableOpacity
        style={[styles.btn, isComplete && styles.btnDisabled]}
        onPress={onAdvance}
        disabled={isProcessing || isComplete}
      >
        {isProcessing ? (
          <ActivityIndicator color={Colors.textOnPrimary} size="small" />
        ) : (
          <Text style={styles.btnText}>{getActionLabel(trip.status)}</Text>
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
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: 16,
  },
  title: {
    fontSize: FontSize.titleLarge,
    fontWeight: '800',
    color: Colors.textSecondary,
    marginBottom: 2,
  },
  subtitle: {
    fontSize: FontSize.body,
    color: Colors.modalSub,
  },
  statusBadge: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
    borderWidth: 1,
  },
  badgeActive: {
    backgroundColor: Colors.onboardingBg,
    borderColor: Colors.onboardingBorder,
  },
  badgeComplete: {
    backgroundColor: Colors.onboardingOkBg,
    borderColor: Colors.onboardingOkBorder,
  },
  statusText: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.onboardingText,
  },
  btn: {
    backgroundColor: Colors.onboardingBtn,
    paddingVertical: 14,
    borderRadius: 12,
    alignItems: 'center',
  },
  btnDisabled: {
    backgroundColor: Colors.modalBorder,
    opacity: 0.7,
  },
  btnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.title,
    fontWeight: '700',
  },
});
