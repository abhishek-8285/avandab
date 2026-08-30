import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { Trip } from '../types/api';

const STAGES: { key: Trip['status']; label: string; icon: string }[] = [
  { key: 'PENDING', label: 'ASSIGNED', icon: 'clipboard-check-outline' },
  { key: 'IN_TRANSIT', label: 'IN TRANSIT', icon: 'truck-fast' },
  { key: 'COMPLETED', label: 'DELIVERED', icon: 'check-decagram' },
];

function stageIdx(status: Trip['status']): number {
  if (status === 'PENDING') return 0;
  if (status === 'IN_TRANSIT') return 1;
  if (status === 'COMPLETED') return 2;
  return 0;
}

export function TripTimeline({ trip }: { trip?: Trip | null }) {
  if (!trip) return null;

  if (trip.stops && trip.stops.length > 0) {
    const stops = [...trip.stops].sort((a, b) => a.stopSequence - b.stopSequence);
    return (
      <View style={styles.card}>
        <Text style={styles.title}>MULTI-STOP ROUTE ({stops.length} STOPS)</Text>
        <View style={styles.row}>
          {stops.map((s, i) => {
            const done = s.status === 'completed' || s.status === 'skipped';
            const active = s.status === 'arrived' || s.status === 'servicing' || s.status === 'en_route';
            const iconName =
              s.stopType === 'pickup'
                ? 'arrow-up-bold-box-outline'
                : s.stopType === 'drop'
                  ? 'arrow-down-bold-box-outline'
                  : 'map-marker-outline';

            return (
              <View key={s.id || `stop-${i}`} style={styles.stage}>
                <View style={[styles.dot, done && styles.dotDone, active && styles.dotActive]}>
                  <MaterialCommunityIcons
                    name={done ? 'check' : (iconName as any)}
                    size={14}
                    color={done || active ? '#fff' : Colors.textMuted}
                  />
                </View>
                <Text style={[styles.label, done && styles.labelDone]} numberOfLines={1}>
                  {s.locationName || `Stop ${s.stopSequence}`}
                </Text>
                {i < stops.length - 1 && <View style={[styles.connector, done && styles.connectorDone]} />}
              </View>
            );
          })}
        </View>
        <Text style={styles.hint}>
          Ref {trip.tripNumber || trip.id} · {stops[0]?.locationName || trip.origin} → {stops[stops.length - 1]?.locationName || trip.destination}
        </Text>
      </View>
    );
  }

  const idx = stageIdx(trip.status);
  return (
    <View style={styles.card}>
      <Text style={styles.title}>TRIP TIMELINE</Text>
      <View style={styles.row}>
        {STAGES.map((s, i) => {
          const done = i <= idx;
          const active = i === idx;
          return (
            <View key={s.key} style={styles.stage}>
              <View style={[styles.dot, done && styles.dotDone, active && styles.dotActive]}>
                <MaterialCommunityIcons name={s.icon as any} size={14} color={done ? '#fff' : Colors.textMuted} />
              </View>
              <Text style={[styles.label, done && styles.labelDone]}>{s.label}</Text>
              {i < STAGES.length - 1 && <View style={[styles.connector, done && styles.connectorDone]} />}
            </View>
          );
        })}
      </View>
      <Text style={styles.hint}>Ref {trip.tripNumber} · {trip.origin}→{trip.destination}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    padding: Spacing.md,
    borderWidth: 1,
    borderColor: Colors.borderLight,
    gap: Spacing.sm,
  },
  title: { fontSize: 10, fontWeight: '800', color: Colors.textMuted, letterSpacing: 1, fontFamily: Font.mono },
  row: { flexDirection: 'row', alignItems: 'flex-start', justifyContent: 'space-between' },
  stage: { flex: 1, alignItems: 'center', position: 'relative' },
  dot: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dotDone: { backgroundColor: Colors.primary, borderColor: Colors.primary },
  dotActive: { borderWidth: 2, borderColor: Colors.primary },
  label: { fontSize: 9, fontWeight: '700', color: Colors.textMuted, letterSpacing: 0.5, marginTop: 6, fontFamily: Font.mono },
  labelDone: { color: Colors.textPrimary },
  connector: { position: 'absolute', top: 14, left: '60%', right: '-40%', height: 2, backgroundColor: Colors.border },
  connectorDone: { backgroundColor: Colors.primary },
  hint: { fontSize: 10, color: Colors.textMuted, fontFamily: Font.mono },
});
