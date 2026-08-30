import React from 'react';
import { StyleSheet, Text, View, ActivityIndicator, TouchableOpacity, Linking } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { useLanguageStore } from '../stores/languageStore';
import { t } from '../i18n';

interface TripCardProps {
  tripNumber: string;
  driverName: string;
  vehiclePlate: string;
  origin: string;
  destination: string;
  status: 'PENDING' | 'IN_TRANSIT' | 'COMPLETED' | 'CANCELLED';
  startTime: string;
  onPress?: () => void;
  onNavigate?: () => void;
  cargoWeight?: string;
  estimatedFare?: string;
}

export const TripCard: React.FC<TripCardProps> = ({
  tripNumber,
  driverName,
  vehiclePlate,
  origin,
  destination,
  status,
  startTime,
  onPress,
  onNavigate,
  cargoWeight,
  estimatedFare,
}) => {
  const { locale } = useLanguageStore();

  const handleStartMap = () => {
    if (onNavigate) {
      onNavigate();
      return;
    }
    if (onPress) {
      onPress();
      return;
    }
    const dest = encodeURIComponent(destination || origin || 'Pune');
    const url = `google.navigation:q=${dest}`;
    Linking.canOpenURL(url).then((canOpen) => {
      if (canOpen) {
        Linking.openURL(url);
      } else {
        Linking.openURL(`https://www.google.com/maps/dir/?api=1&destination=${dest}`);
      }
    }).catch(() => {
      Linking.openURL(`https://www.google.com/maps/dir/?api=1&destination=${dest}`);
    });
  };

  const getStatusBadge = () => {
    switch (status) {
      case 'IN_TRANSIT':
        return {
          bg: '#e7ffdb',
          text: '#008069',
          label: t('duty.available', 'IN TRANSIT', locale),
          dot: '#25d366',
        };
      case 'PENDING':
        return {
          bg: '#fef3c7',
          text: '#b45309',
          label: t('duty.break', 'READY', locale),
          dot: '#f59e0b',
        };
      case 'COMPLETED':
        return {
          bg: '#e0f2fe',
          text: '#0284c7',
          label: t('docs.under_review', 'DELIVERED', locale),
          dot: '#0284c7',
        };
      default:
        return { bg: '#fee2e2', text: '#dc2626', label: status, dot: '#dc2626' };
    }
  };

  const badge = getStatusBadge();

  return (
    <TouchableOpacity
      activeOpacity={0.92}
      onPress={handleStartMap}
      style={[
        styles.card,
        status === 'IN_TRANSIT' && styles.cardActiveBorder,
      ]}
    >
      {/* Top Bar: Trip ID & Status Badge */}
      <View style={styles.header}>
        <View style={styles.tripIdBlock}>
          <View style={styles.iconCircle}>
            <MaterialCommunityIcons name="truck-delivery" size={16} color="#008069" />
          </View>
          <View>
            <Text style={styles.tripNumber}>#{tripNumber}</Text>
            <Text style={styles.subMeta}>{startTime ? `${startTime}` : t('duty.available_desc', 'Ready for Dispatch', locale)}</Text>
          </View>
        </View>

        <View style={[styles.badge, { backgroundColor: badge.bg }]}>
          <View style={[styles.badgeDot, { backgroundColor: badge.dot }]} />
          <Text style={[styles.badgeText, { color: badge.text }]}>{badge.label}</Text>
        </View>
      </View>

      {/* Driver & Vehicle Plate Row */}
      <View style={styles.driverRow}>
        <View style={styles.driverPill}>
          <MaterialCommunityIcons name="account-tie" size={14} color="#667781" />
          <Text style={styles.driverName}>{driverName || t('profile.driver_id', 'Driver', locale)}</Text>
        </View>
        {vehiclePlate ? (
          <View style={styles.plateChip}>
            <MaterialCommunityIcons name="car-traction-control" size={13} color="#008069" />
            <Text style={styles.plateText}>{vehiclePlate}</Text>
          </View>
        ) : null}
      </View>

      <View style={styles.divider} />

      {/* Route Stops with WhatsApp-style Timeline */}
      <View style={styles.routeContainer}>
        {/* Origin */}
        <View style={styles.routeRow}>
          <View style={[styles.routeDot, styles.routeDotOrigin]}>
            <View style={styles.innerDotOrigin} />
          </View>
          <View style={styles.locationBlock}>
            <Text style={styles.locationLabel}>{t('dispatch.origin', 'ORIGIN', locale)}</Text>
            <Text style={styles.locationText} numberOfLines={1}>{origin}</Text>
          </View>
        </View>

        {/* Connector Line */}
        <View style={styles.routeConnectorLine} />

        {/* Destination */}
        <View style={styles.routeRow}>
          <View style={[styles.routeDot, styles.routeDotDest]}>
            <View style={styles.innerDotDest} />
          </View>
          <View style={styles.locationBlock}>
            <Text style={styles.locationLabel}>{t('dispatch.destination', 'DESTINATION', locale)}</Text>
            <Text style={styles.locationText} numberOfLines={1}>{destination}</Text>
          </View>
        </View>
      </View>

      {/* Action Strip */}
      <View style={styles.actionStrip}>
        <View style={styles.fareInfoBlock}>
          {cargoWeight ? (
            <Text style={styles.weightText}>📦 {cargoWeight}</Text>
          ) : (
            <Text style={styles.weightText}>📦 18 Tons</Text>
          )}
          {estimatedFare ? (
            <Text style={styles.fareText}>₹{estimatedFare}</Text>
          ) : (
            <Text style={styles.fareText}>₹24,500</Text>
          )}
        </View>

        <View style={styles.buttonGroup}>
          <TouchableOpacity
            style={styles.navigateBtn}
            activeOpacity={0.85}
            onPress={handleStartMap}
          >
            <MaterialCommunityIcons name="navigation-variant" size={15} color="#ffffff" />
            <Text style={styles.navigateBtnText}>{t('trips.start_map', 'START MAP', locale)}</Text>
          </TouchableOpacity>
        </View>
      </View>
    </TouchableOpacity>
  );
};

export const SkeletonLoader = () => (
  <View style={[styles.card, { gap: 10 }]}>
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 32, height: 32, borderRadius: 16, backgroundColor: '#f1f5f9' }} />
        <View style={{ gap: 4 }}>
          <View style={{ width: 90, height: 14, borderRadius: 4, backgroundColor: '#e2e8f0' }} />
          <View style={{ width: 60, height: 10, borderRadius: 4, backgroundColor: '#f1f5f9' }} />
        </View>
      </View>
      <View style={{ width: 65, height: 20, borderRadius: 10, backgroundColor: '#f1f5f9' }} />
    </View>

    <View style={{ flexDirection: 'row', gap: 6, marginVertical: 4 }}>
      <View style={{ width: 80, height: 14, borderRadius: 4, backgroundColor: '#f1f5f9' }} />
      <View style={{ width: 100, height: 14, borderRadius: 4, backgroundColor: '#f1f5f9' }} />
    </View>

    <View style={{ height: 1, backgroundColor: '#f1f5f9', marginVertical: 4 }} />

    <View style={{ gap: 8 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 12, height: 12, borderRadius: 6, backgroundColor: '#e2e8f0' }} />
        <View style={{ width: '70%', height: 12, borderRadius: 4, backgroundColor: '#f1f5f9' }} />
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 12, height: 12, borderRadius: 6, backgroundColor: '#cbd5e1' }} />
        <View style={{ width: '55%', height: 12, borderRadius: 4, backgroundColor: '#f1f5f9' }} />
      </View>
    </View>
  </View>
);

const styles = StyleSheet.create({
  card: {
    backgroundColor: '#ffffff',
    borderRadius: 14,
    padding: 14,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: '#e9edef',
    shadowColor: '#000000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.08,
    shadowRadius: 3,
    elevation: 2,
  },
  cardActiveBorder: {
    borderColor: '#25d366',
    borderLeftWidth: 4,
    borderLeftColor: '#008069',
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  tripIdBlock: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  iconCircle: {
    width: 32,
    height: 32,
    borderRadius: 16,
    backgroundColor: '#e7ffdb',
    alignItems: 'center',
    justifyContent: 'center',
  },
  tripNumber: {
    fontSize: 15,
    fontWeight: '800',
    color: '#111b21',
  },
  subMeta: {
    fontSize: 10,
    color: '#667781',
    fontWeight: '500',
    marginTop: 1,
  },
  badge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 12,
  },
  badgeDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  badgeText: {
    fontSize: 10,
    fontWeight: '800',
  },
  driverRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginTop: 10,
  },
  driverPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  driverName: {
    fontSize: 12,
    fontWeight: '600',
    color: '#667781',
  },
  plateChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: '#f0f2f5',
    paddingHorizontal: 7,
    paddingVertical: 3,
    borderRadius: 6,
  },
  plateText: {
    fontSize: 11,
    fontWeight: '800',
    color: '#111b21',
    fontFamily: Font.mono,
  },
  divider: {
    height: 1,
    backgroundColor: '#f0f2f5',
    marginVertical: 10,
  },
  routeContainer: {
    gap: 2,
  },
  routeRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 10,
  },
  routeDot: {
    width: 14,
    height: 14,
    borderRadius: 7,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  routeDotOrigin: {
    backgroundColor: '#e7ffdb',
  },
  innerDotOrigin: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#008069',
  },
  routeDotDest: {
    backgroundColor: '#fee2e2',
  },
  innerDotDest: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#ef4444',
  },
  locationBlock: {
    flex: 1,
  },
  locationLabel: {
    fontSize: 9,
    fontWeight: '700',
    color: '#8696a0',
    letterSpacing: 0.5,
  },
  locationText: {
    fontSize: 13,
    fontWeight: '700',
    color: '#111b21',
    marginTop: 1,
  },
  routeConnectorLine: {
    width: 2,
    height: 14,
    backgroundColor: '#cbd5e1',
    marginLeft: 6,
    marginVertical: 1,
  },
  actionStrip: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: 12,
    paddingTop: 10,
    borderTopWidth: 1,
    borderTopColor: '#f0f2f5',
  },
  fareInfoBlock: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  weightText: {
    fontSize: 11,
    fontWeight: '600',
    color: '#667781',
  },
  fareText: {
    fontSize: 14,
    fontWeight: '900',
    color: '#008069',
  },
  buttonGroup: {
    marginLeft: 'auto',
  },
  navigateBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    backgroundColor: '#008069',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 16,
  },
  navigateBtnText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
});
