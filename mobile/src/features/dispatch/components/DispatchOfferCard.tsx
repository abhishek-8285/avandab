import React from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { DispatchOffer } from '../types/dispatch';

interface Props {
  offer: DispatchOffer;
  onAccept: (offerId: string) => void;
  onReject: (offerId: string) => void;
  isBusy?: boolean;
}

export const DispatchOfferCard: React.FC<Props> = ({
  offer,
  onAccept,
  onReject,
  isBusy = false,
}) => {
  return (
    <View style={styles.card}>
      <View style={styles.headerRow}>
        <View style={styles.badge}>
          <Text style={styles.badgeText}>NEW DISPATCH OFFER</Text>
        </View>
        <Text style={styles.bookingId}>#{offer.booking_id.substring(0, 8)}</Text>
      </View>

      <View style={styles.routeContainer}>
        <View style={styles.routePoint}>
          <View style={[styles.dot, styles.dotOrigin]} />
          <Text style={styles.routeText}>{offer.origin || '—'}</Text>
        </View>
        <View style={styles.routeLine} />
        <View style={styles.routePoint}>
          <View style={[styles.dot, styles.dotDest]} />
          <Text style={styles.routeText}>{offer.destination || '—'}</Text>
        </View>
      </View>

      <View style={styles.detailsRow}>
        <View style={styles.detailCol}>
          <Text style={styles.detailLabel}>PAYOUT</Text>
          <Text style={styles.detailValueHighlight}>
            ₹{offer.payout ? offer.payout.toLocaleString() : '—'}
          </Text>
        </View>
        <View style={styles.detailCol}>
          <Text style={styles.detailLabel}>CARGO</Text>
          <Text style={styles.detailValue}>{offer.cargo_type || 'Industrial Goods'}</Text>
        </View>
        <View style={styles.detailCol}>
          <Text style={styles.detailLabel}>VEHICLE</Text>
          <Text style={styles.detailValue}>{offer.vehicle_id || 'MH04AB1234'}</Text>
        </View>
      </View>

      <View style={styles.btnRow}>
        <TouchableOpacity
          style={[styles.btn, styles.btnReject]}
          onPress={() => onReject(offer.id)}
          disabled={isBusy}
        >
          <Text style={styles.btnRejectText}>Decline</Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.btn, styles.btnAccept]}
          onPress={() => onAccept(offer.id)}
          disabled={isBusy}
        >
          <Text style={styles.btnAcceptText}>Accept Dispatch</Text>
        </TouchableOpacity>
      </View>
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
    marginVertical: 8,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  badge: {
    backgroundColor: Colors.onboardingBg,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: Colors.onboardingBorder,
  },
  badgeText: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.onboardingText,
    letterSpacing: 0.5,
  },
  bookingId: {
    fontSize: FontSize.body,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  routeContainer: {
    marginBottom: 14,
  },
  routePoint: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    marginRight: 10,
  },
  dotOrigin: {
    backgroundColor: Colors.onboardingOkBorder,
  },
  dotDest: {
    backgroundColor: Colors.info,
  },
  routeLine: {
    width: 2,
    height: 12,
    backgroundColor: Colors.modalBorder,
    marginLeft: 3,
    marginVertical: 2,
  },
  routeText: {
    fontSize: FontSize.title,
    fontWeight: '600',
    color: Colors.textSecondary,
  },
  detailsRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    backgroundColor: Colors.modalBg,
    borderRadius: 10,
    padding: 10,
    marginBottom: 14,
  },
  detailCol: {
    flex: 1,
  },
  detailLabel: {
    fontSize: FontSize.caption,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 2,
  },
  detailValue: {
    fontSize: FontSize.body,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  detailValueHighlight: {
    fontSize: FontSize.title,
    fontWeight: '800',
    color: Colors.accent,
  },
  btnRow: {
    flexDirection: 'row',
    gap: 10,
  },
  btn: {
    flex: 1,
    paddingVertical: 12,
    borderRadius: 10,
    alignItems: 'center',
  },
  btnAccept: {
    backgroundColor: Colors.onboardingBtn,
  },
  btnAcceptText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.bodyLarge,
    fontWeight: '700',
  },
  btnReject: {
    backgroundColor: Colors.modalBg,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  btnRejectText: {
    color: Colors.modalSub,
    fontSize: FontSize.bodyLarge,
    fontWeight: '600',
  },
});
