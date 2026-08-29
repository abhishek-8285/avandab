import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors } from '../../../constants/theme';

type Tab = 'trips' | 'dispatch' | 'paisa';
export function BottomTabs({ activeTab, onChange }: { activeTab: Tab; onChange: (t: Tab) => void }) {
  const tabs: { id: Tab; icon: string; label: string }[] = [
    { id: 'trips', icon: 'truck-delivery', label: 'Trips' },
    { id: 'dispatch', icon: 'navigation', label: 'Dispatch' },
    { id: 'paisa', icon: 'wallet-outline', label: 'Wallet' },
  ];
  return (
    <View style={styles.tabContainer}>
      {tabs.map((tb) => (
        <TouchableOpacity key={tb.id} style={styles.tab} onPress={() => onChange(tb.id)}>
          <MaterialCommunityIcons name={tb.icon as any} size={22} color={activeTab === tb.id ? Colors.tabActive : Colors.tabInactive} />
          <Text style={[styles.tabText, activeTab === tb.id && styles.activeTabText]}>{tb.label}</Text>
        </TouchableOpacity>
      ))}
    </View>
  );
}
const styles = StyleSheet.create({
  tabContainer: { flexDirection: 'row', backgroundColor: Colors.headerBg, borderTopWidth: 1, borderTopColor: Colors.headerBorder, paddingTop: 6, paddingBottom: 6, height: 58 },
  tab: { flex: 1, paddingVertical: 4, alignItems: 'center', justifyContent: 'center', gap: 2 },
  tabText: { fontSize: 10, fontWeight: '600', color: Colors.tabInactive, marginTop: 2 },
  activeTabText: { color: Colors.tabActive, fontWeight: '700' },
});
