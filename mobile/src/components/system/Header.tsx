import React from 'react';
import { StyleSheet, Text, View, TouchableOpacity } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius, Spacing } from '../../constants/theme';
interface HeaderProps { title: string; onBack?: () => void; right?: React.ReactNode; }
export function Header({ title, onBack, right }: HeaderProps) {
  return (
    <View style={styles.header}>
      {onBack ? (<TouchableOpacity style={styles.iconButton} onPress={onBack}><MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textPrimary} /></TouchableOpacity>) : (<View style={{ width: 32 }} />)}
      <Text style={styles.headerLabel}>{title}</Text>
      <View style={{ width: 32, alignItems: 'flex-end' }}>{right ?? <View style={{ width: 32 }} />}</View>
    </View>
  );
}
const styles = StyleSheet.create({
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingHorizontal: Spacing.lg, paddingTop: 50, paddingBottom: Spacing.md, backgroundColor: Colors.headerBg, borderBottomWidth: 1, borderBottomColor: Colors.headerBorder },
  headerLabel: { fontSize: 11, fontWeight: '700', color: Colors.textPrimary, letterSpacing: 2 },
  iconButton: { width: 32, height: 32, borderRadius: Radius.md, borderWidth: 1, borderColor: Colors.headerBorder, alignItems: 'center', justifyContent: 'center' },
});
