import React from 'react';
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
          <Text style={styles.routeText}>{offer.origin || 'Mumbai Port / Nhava Sheva'}</Text>
        </View>
        <View style={styles.routeLine} />
        <View style={styles.routePoint}>
          <View style={[styles.dot, styles.dotDest]} />
          <Text style={styles.routeText}>{offer.destination || 'Pune Logistics Hub / Chakan'}</Text>
        </View>
      </View>

      <View style={styles.detailsRow}>
        <View style={styles.detailCol}>
          <Text style={styles.detailLabel}>PAYOUT</Text>
          <Text style={styles.detailValueHighlight}>
            ₹{offer.payout ? offer.payout.toLocaleString() : '8,500'}
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
    backgroundColor: '#0f172a',
    borderRadius: 16,
    padding: 16,
    borderWidth: 1,
    borderColor: '#1e293b',
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
    backgroundColor: '#083344',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: '#06b6d4',
  },
  badgeText: {
    fontSize: 9,
    fontWeight: '800',
    color: '#38bdf8',
    letterSpacing: 0.5,
  },
  bookingId: {
    fontSize: 12,
    fontWeight: '700',
    color: '#64748b',
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
    backgroundColor: '#10b981',
  },
  dotDest: {
    backgroundColor: '#3b82f6',
  },
  routeLine: {
    width: 2,
    height: 12,
    backgroundColor: '#334155',
    marginLeft: 3,
    marginVertical: 2,
  },
  routeText: {
    fontSize: 14,
    fontWeight: '600',
    color: '#f8fafc',
  },
  detailsRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 10,
    marginBottom: 14,
  },
  detailCol: {
    flex: 1,
  },
  detailLabel: {
    fontSize: 9,
    fontWeight: '700',
    color: '#64748b',
    marginBottom: 2,
  },
  detailValue: {
    fontSize: 12,
    fontWeight: '700',
    color: '#f8fafc',
  },
  detailValueHighlight: {
    fontSize: 14,
    fontWeight: '800',
    color: '#34d399',
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
    backgroundColor: '#0d9488',
  },
  btnAcceptText: {
    color: '#ffffff',
    fontSize: 13,
    fontWeight: '700',
  },
  btnReject: {
    backgroundColor: '#1e293b',
    borderWidth: 1,
    borderColor: '#334155',
  },
  btnRejectText: {
    color: '#94a3b8',
    fontSize: 13,
    fontWeight: '600',
  },
});
