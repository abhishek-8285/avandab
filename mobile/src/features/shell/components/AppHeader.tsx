import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { Colors, Font, Radius, Spacing } from '../../../constants/theme';
import { useAuthStore } from '../../../stores/authStore';

export function AppHeader({ onSignOut }: { onSignOut: () => void }) {
  const user = useAuthStore((s) => s.user);
  return (
    <View style={styles.header}>
      <View style={styles.headerTopRow}>
        <View style={styles.brandBadge}>
          <View style={styles.brandDot} />
          <Text style={styles.brandBadgeText}>AVANDAB · OPS</Text>
        </View>
        <TouchableOpacity onPress={onSignOut}>
          <Text style={styles.headerClock}>SIGN OUT</Text>
        </TouchableOpacity>
      </View>
      <Text style={styles.headerTitle}>FLEET MOBILE</Text>
      <Text style={styles.headerSubtitle}>{user ? `${user.name.toUpperCase()} · ${user.driverId || user.id}` : 'LIVE DISPATCH & TRIP MGMT'}</Text>
    </View>
  );
}
const styles = StyleSheet.create({
  header: { backgroundColor: Colors.headerBg, borderBottomWidth: 1, borderBottomColor: Colors.headerBorder, paddingHorizontal: Spacing.lg, paddingTop: Spacing.md, paddingBottom: Spacing.lg },
  headerTopRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  brandBadge: { flexDirection: 'row', alignItems: 'center', gap: 6, backgroundColor: Colors.background, borderWidth: 1, borderColor: Colors.border, paddingHorizontal: 8, paddingVertical: 3, borderRadius: Radius.sm },
  brandDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#22c55e' },
  brandBadgeText: { color: Colors.textPrimary, fontSize: 9, fontWeight: '800', letterSpacing: 1.5, fontFamily: Font.mono },
  headerClock: { color: Colors.textSecondary, fontSize: 10, fontWeight: '700', letterSpacing: 1, fontFamily: Font.mono },
  headerTitle: { color: Colors.textPrimary, fontSize: 22, fontWeight: '900', letterSpacing: 2, fontFamily: Font.mono },
  headerSubtitle: { color: Colors.textSecondary, fontSize: 10, fontWeight: '600', letterSpacing: 1.5, marginTop: 2, fontFamily: Font.mono },
});
