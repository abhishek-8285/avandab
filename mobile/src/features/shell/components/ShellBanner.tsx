import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../../../constants/theme';
import { ComplianceBanner } from '../../../components/ComplianceBanner';

export function ShellBanner({ vehicleId, onSetup }: { vehicleId: string | null; onSetup?: () => void }) {
  return (
    <>
      <View style={styles.bannerContainer}>
        <View style={styles.bannerIconBox}>
          <MaterialCommunityIcons name="clipboard-alert-outline" size={14} color={Colors.warning} />
        </View>
        <View style={styles.bannerTextContainer}>
          <Text style={styles.bannerTitle}>PROFILE SETUP INCOMPLETE</Text>
          <Text style={styles.bannerSub}>Bank details · Profile photo · Driver docs</Text>
        </View>
        <TouchableOpacity style={styles.bannerBtn} activeOpacity={0.85} onPress={onSetup}>
          <Text style={styles.bannerBtnText}>SETUP</Text>
        </TouchableOpacity>
      </View>
      <View style={styles.complianceContainer}>
        <ComplianceBanner vehicleId={vehicleId} />
      </View>
    </>
  );
}
const styles = StyleSheet.create({
  bannerContainer: { backgroundColor: '#fffbeb', borderBottomWidth: 1, borderBottomColor: '#fef3c7', paddingHorizontal: Spacing.lg, paddingVertical: Spacing.sm, flexDirection: 'row', alignItems: 'center', gap: 10 },
  bannerIconBox: { width: 24, height: 24, borderRadius: Radius.sm, backgroundColor: '#fef3c7', justifyContent: 'center', alignItems: 'center' },
  bannerTextContainer: { flex: 1 },
  bannerTitle: { fontSize: 10, fontWeight: '800', color: '#92400e', letterSpacing: 0.5, fontFamily: Font.mono },
  bannerSub: { fontSize: 9, fontWeight: '500', color: '#b45309', marginTop: 1 },
  bannerBtn: { backgroundColor: Colors.warning, paddingHorizontal: 8, paddingVertical: 4, borderRadius: Radius.sm },
  bannerBtnText: { fontSize: 9, fontWeight: '800', color: '#ffffff', letterSpacing: 0.5, fontFamily: Font.mono },
  complianceContainer: { paddingHorizontal: Spacing.lg, paddingTop: Spacing.sm },
});
