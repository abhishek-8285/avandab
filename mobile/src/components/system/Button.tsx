import React from 'react';
import { TouchableOpacity, Text, StyleSheet, ActivityIndicator, ViewStyle, TextStyle } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius } from '../../constants/theme';

type Variant = 'primary' | 'secondary' | 'danger' | 'ghost';
type Size = 'sm' | 'md' | 'lg';

interface Props {
  title: string;
  variant?: Variant;
  size?: Size;
  icon?: string;
  loading?: boolean;
  disabled?: boolean;
  onPress?: () => void;
  style?: ViewStyle;
  textStyle?: TextStyle;
}

const VARIANT: Record<Variant, { bg: string; fg: string; border?: string }> = {
  primary: { bg: Colors.primary, fg: Colors.textOnPrimary },
  secondary: { bg: Colors.surface, fg: Colors.textPrimary, border: Colors.border },
  danger: { bg: Colors.danger, fg: '#fff' },
  ghost: { bg: 'transparent', fg: Colors.primary },
};

const SIZE: Record<Size, { h: number; font: number }> = {
  sm: { h: 32, font: 10 },
  md: { h: 44, font: 11 },
  lg: { h: 48, font: 12 },
};

export function Button({ title, variant = 'primary', size = 'md', icon, loading, disabled, onPress, style, textStyle }: Props) {
  const v = VARIANT[variant];
  const s = SIZE[size];
  return (
    <TouchableOpacity
      activeOpacity={0.88}
      disabled={disabled || loading}
      onPress={onPress}
      style={[
        styles.base,
        { height: s.h, backgroundColor: v.bg, borderColor: v.border ?? v.bg, opacity: disabled ? 0.5 : 1 },
        style,
      ]}
    >
      {loading ? (
        <ActivityIndicator color={v.fg} />
      ) : (
        <>
          {icon ? <MaterialCommunityIcons name={icon as any} size={14} color={v.fg} /> : null}
          <Text style={[styles.text, { color: v.fg, fontSize: s.font }, textStyle]}>{title}</Text>
        </>
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  base: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    borderRadius: Radius.md,
    borderWidth: 1,
    paddingHorizontal: 14,
  },
  text: { fontWeight: '800', letterSpacing: 1.5, fontFamily: 'monospace' },
});
