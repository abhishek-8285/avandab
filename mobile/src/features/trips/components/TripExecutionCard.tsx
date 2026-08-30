import React from 'react';
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
          <ActivityIndicator color="#ffffff" size="small" />
        ) : (
          <Text style={styles.btnText}>{getActionLabel(trip.status)}</Text>
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
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    marginBottom: 16,
  },
  title: {
    fontSize: 15,
    fontWeight: '800',
    color: '#f8fafc',
    marginBottom: 2,
  },
  subtitle: {
    fontSize: 12,
    color: '#94a3b8',
  },
  statusBadge: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
    borderWidth: 1,
  },
  badgeActive: {
    backgroundColor: '#083344',
    borderColor: '#06b6d4',
  },
  badgeComplete: {
    backgroundColor: '#064e3b',
    borderColor: '#10b981',
  },
  statusText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#38bdf8',
  },
  btn: {
    backgroundColor: '#0d9488',
    paddingVertical: 14,
    borderRadius: 12,
    alignItems: 'center',
  },
  btnDisabled: {
    backgroundColor: '#334155',
    opacity: 0.7,
  },
  btnText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '700',
  },
});
