import React from 'react';
import { StyleSheet, Text, View, TouchableOpacity } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius, Spacing, FontSize} from '../../constants/theme';
interface HeaderProps { title: string; onBack?: () => void; right?: React.ReactNode; }
export function Header({ title, onBack, right }: HeaderProps) {
  return (
    <View style={styles.header}>
      {onBack ? (<TouchableOpacity style={styles.iconButton} onPress={onBack} accessibilityRole="button" accessibilityLabel="Go back" hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}><MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textPrimary} accessible={false} /></TouchableOpacity>) : (<View style={{ width: 44 }} />)}
      <Text style={styles.headerLabel} accessibilityRole="header">{title}</Text>
      <View style={{ width: 44, alignItems: 'flex-end' }}>{right ?? <View style={{ width: 44 }} />}</View>
    </View>
  );
}
const styles = StyleSheet.create({
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingHorizontal: Spacing.lg, paddingTop: 50, paddingBottom: Spacing.md, backgroundColor: Colors.headerBg, borderBottomWidth: 1, borderBottomColor: Colors.headerBorder },
  headerLabel: { fontSize: FontSize.label, fontWeight: '700', color: Colors.textPrimary, letterSpacing: 2 },
  iconButton: { width: 44, height: 44, borderRadius: Radius.md, borderWidth: 1, borderColor: Colors.headerBorder, alignItems: 'center', justifyContent: 'center' },
});
