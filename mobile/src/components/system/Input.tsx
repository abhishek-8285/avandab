import React from 'react';
import { View, Text, TextInput, StyleSheet, TextInputProps } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius, Spacing } from '../../constants/theme';

interface Props extends TextInputProps {
  label: string;
  icon?: string;
  error?: string;
}

export function Input({ label, icon, error, style, ...rest }: Props) {
  return (
    <View style={styles.wrap}>
      <Text style={styles.label}>{label}</Text>
      <View style={[styles.inputWrap, error && { borderColor: Colors.danger }]}>
        {icon ? <MaterialCommunityIcons name={icon as any} size={16} color={Colors.textMuted} style={styles.icon} /> : null}
        <TextInput
          style={[styles.input, icon && { paddingLeft: 34 }, style as any]}
          placeholderTextColor={Colors.textMuted}
          {...rest}
        />
      </View>
      {error ? <Text style={styles.error}>{error}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 6 },
  label: { fontSize: 10, fontWeight: '700', color: Colors.textSecondary, letterSpacing: 1, fontFamily: 'monospace' },
  inputWrap: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.md,
    height: 44,
  },
  icon: { position: 'absolute', left: 10, zIndex: 1 },
  input: { flex: 1, paddingHorizontal: 12, fontSize: 13, color: Colors.textPrimary, fontFamily: 'monospace' },
  error: { fontSize: 10, color: Colors.danger },
});
