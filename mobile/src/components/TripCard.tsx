import React from 'react';
import { StyleSheet, Text, View, ActivityIndicator, TouchableOpacity, Linking, Alert } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
import { useLanguageStore } from '../stores/languageStore';
import { NotificationService } from '../services/notificationService';
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
  advanceAmount?: number;
}

const TripCardBase: React.FC<TripCardProps> = ({
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
  advanceAmount,
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
          bg: Colors.primarySubtle,
          text: Colors.primary,
          label: t('duty.available', 'IN TRANSIT', locale),
          dot: Colors.accent,
        };
      case 'PENDING':
        return {
          bg: Colors.warningBg,
          text: Colors.warningText,
          label: t('duty.break', 'READY', locale),
          dot: Colors.warning,
        };
      case 'COMPLETED':
        return {
          bg: Colors.infoBg,
          text: Colors.info,
          label: t('docs.under_review', 'DELIVERED', locale),
          dot: Colors.info,
        };
      default:
        return { bg: Colors.dangerBg, text: Colors.danger, label: status, dot: Colors.danger };
    }
  };

  const badge = getStatusBadge();

  return (
    <View
      style={[
        styles.card,
        status === 'IN_TRANSIT' && styles.cardActiveBorder,
      ]}
    >
      {/* Top Bar: Trip ID & Status Badge */}
      <View style={styles.header}>
        <View style={styles.tripIdBlock}>
          <View style={styles.iconCircle}>
            <MaterialCommunityIcons name="truck-delivery" size={16} color={Colors.primary} accessible={false} />
          </View>
          <View>
            <Text style={styles.tripNumber} numberOfLines={1} ellipsizeMode="tail" accessibilityLabel={`Trip ${tripNumber}`}>#{tripNumber}</Text>
            <Text style={styles.subMeta} numberOfLines={1} ellipsizeMode="tail">{startTime ? `${startTime}` : t('duty.available_desc', 'Ready for Dispatch', locale)}</Text>
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
          <MaterialCommunityIcons name="account-tie" size={14} color={Colors.textSecondary} accessible={false} />
          <Text style={styles.driverName} numberOfLines={1} ellipsizeMode="tail">{driverName || t('profile.driver_id', 'Driver', locale)}</Text>
        </View>
        {vehiclePlate ? (
          <View style={styles.plateChip}>
            <MaterialCommunityIcons name="car-traction-control" size={13} color={Colors.primary} accessible={false} />
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
            <Text style={styles.locationText} numberOfLines={1} ellipsizeMode="tail">{origin}</Text>
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
            <Text style={styles.locationText} numberOfLines={1} ellipsizeMode="tail">{destination}</Text>
          </View>
        </View>
      </View>

      {/* Cargo & Route Details Strip (only when the trip carries cargo info) */}
      {cargoWeight ? (
      <View style={styles.summaryStrip}>
        <View style={styles.cargoChip}>
          <Text style={styles.weightText} accessibilityLabel={cargoWeight} numberOfLines={1} ellipsizeMode="tail">{`📦 ${cargoWeight}`}</Text>
        </View>
        <View style={styles.routeStatusBadge}>
          <View style={styles.statusPulseDot} />
          <Text style={styles.routeStatusLabel}>{status === 'IN_TRANSIT' ? 'LIVE ROUTE' : 'DISPATCH READY'}</Text>
        </View>
      </View>
      ) : null}

      {/* Action Toolbar: Quick Contact Icons + Full-Width Navigate */}
      <View style={styles.actionStrip}>
        <View style={styles.leftActions}>
          {/* Dispatcher contact: the trip record carries no phone number,
          so never dial a hardcoded one — say so honestly. */}
          <TouchableOpacity
            style={styles.actionIconBtn}
            activeOpacity={0.85}
            accessibilityRole="button"
            accessibilityLabel="Call Dispatcher"
            hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            onPress={() => Alert.alert('Dispatcher', 'Dispatcher contact is not available for this trip.')}
          >
            <MaterialCommunityIcons name="phone" size={17} color={Colors.primary} />
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionIconBtn}
            activeOpacity={0.85}
            accessibilityRole="button"
            accessibilityLabel="WhatsApp Hub"
            hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            onPress={() => {
              Alert.alert('WhatsApp Hub', 'Dispatcher contact is not available for this trip.');
            }}
          >
            <MaterialCommunityIcons name="whatsapp" size={19} color={Colors.accent} />
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionIconBtn}
            activeOpacity={0.85}
            accessibilityRole="button"
            accessibilityLabel="Play Voice Alert & Push Notification"
            hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            onPress={() => {
              NotificationService.showDispatchNotification({
                tripNumber,
                origin,
                destination,
                advanceAmount,
              }).catch(() => {});
            }}
          >
            <MaterialCommunityIcons name="bell-ring-outline" size={17} color={Colors.primary} />
          </TouchableOpacity>
        </View>

        <TouchableOpacity
          style={styles.navigateBtn}
          activeOpacity={0.85}
          onPress={handleStartMap}
          accessibilityRole="button"
          accessibilityLabel="Start map navigation"
          hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
        >
          <MaterialCommunityIcons name="navigation-variant" size={16} color={Colors.textOnPrimary} accessible={false} />
          <Text style={styles.navigateBtnText}>{t('trips.start_map', 'START MAP', locale)}</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
};

export const SkeletonLoader = () => (
  <View style={[styles.card, { gap: 10 }]}>
    <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 32, height: 32, borderRadius: 16, backgroundColor: Colors.skeleton }} />
        <View style={{ gap: 4 }}>
          <View style={{ width: 90, height: 14, borderRadius: 4, backgroundColor: Colors.borderStrong }} />
          <View style={{ width: 60, height: 10, borderRadius: 4, backgroundColor: Colors.skeleton }} />
        </View>
      </View>
      <View style={{ width: 65, height: 20, borderRadius: 10, backgroundColor: Colors.skeleton }} />
    </View>

    <View style={{ flexDirection: 'row', gap: 6, marginVertical: 4 }}>
      <View style={{ width: 80, height: 14, borderRadius: 4, backgroundColor: Colors.skeleton }} />
      <View style={{ width: 100, height: 14, borderRadius: 4, backgroundColor: Colors.skeleton }} />
    </View>

    <View style={{ height: 1, backgroundColor: Colors.skeleton, marginVertical: 4 }} />

    <View style={{ gap: 8 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 12, height: 12, borderRadius: 6, backgroundColor: Colors.borderStrong }} />
        <View style={{ width: '70%', height: 12, borderRadius: 4, backgroundColor: Colors.skeleton }} />
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        <View style={{ width: 12, height: 12, borderRadius: 6, backgroundColor: Colors.borderStrong }} />
        <View style={{ width: '55%', height: 12, borderRadius: 4, backgroundColor: Colors.skeleton }} />
      </View>
    </View>
  </View>
);

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.surface,
    borderRadius: 14,
    padding: 14,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: Colors.border,
    shadowColor: Colors.textPrimary,
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.08,
    shadowRadius: 3,
    elevation: 2,
  },
  cardActiveBorder: {
    borderColor: Colors.accent,
    borderLeftWidth: 4,
    borderLeftColor: Colors.primary,
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
    backgroundColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
  },
  tripNumber: {
    fontSize: FontSize.titleLarge,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  subMeta: {
    fontSize: FontSize.small,
    color: Colors.textSecondary,
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
    fontSize: FontSize.small,
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
    fontSize: FontSize.body,
    fontWeight: '600',
    color: Colors.textSecondary,
  },
  plateChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: Colors.borderLight,
    paddingHorizontal: 7,
    paddingVertical: 3,
    borderRadius: 6,
  },
  plateText: {
    fontSize: FontSize.label,
    fontWeight: '800',
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  divider: {
    height: 1,
    backgroundColor: Colors.borderLight,
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
    backgroundColor: Colors.primarySubtle,
  },
  innerDotOrigin: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.primary,
  },
  routeDotDest: {
    backgroundColor: Colors.dangerBg,
  },
  innerDotDest: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.danger,
  },
  locationBlock: {
    flex: 1,
  },
  locationLabel: {
    fontSize: FontSize.caption,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 0.5,
  },
  locationText: {
    fontSize: FontSize.bodyLarge,
    fontWeight: '700',
    color: Colors.textPrimary,
    marginTop: 1,
  },
  routeConnectorLine: {
    width: 2,
    height: 14,
    backgroundColor: Colors.borderStrong,
    marginLeft: 6,
    marginVertical: 1,
  },
  summaryStrip: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: 12,
    paddingTop: 10,
    borderTopWidth: 1,
    borderTopColor: Colors.borderLight,
  },
  cargoChip: {
    backgroundColor: Colors.surfaceSecondary,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  weightText: {
    fontSize: FontSize.body,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  routeStatusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: Colors.infoBg,
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: Colors.infoBorder,
  },
  statusPulseDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.info,
  },
  routeStatusLabel: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.info,
    letterSpacing: 0.5,
  },
  actionStrip: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: 10,
    gap: 10,
  },
  leftActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  actionIconBtn: {
    width: 38,
    height: 38,
    borderRadius: 19,
    backgroundColor: Colors.primarySubtle,
    borderWidth: 1,
    borderColor: Colors.primaryBorder,
    alignItems: 'center',
    justifyContent: 'center',
  },
  navigateBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: Colors.primary,
    height: 38,
    borderRadius: 19,
    shadowColor: Colors.primary,
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.25,
    shadowRadius: 3,
    elevation: 3,
  },
  navigateBtnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.body,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
});

// Memoized export: list parents (FlashList/ScrollView) re-render on scroll
// state changes; memo skips unchanged cards when props are referentially
// stable (list-performance-item-memo).
export const TripCard = React.memo(TripCardBase);
