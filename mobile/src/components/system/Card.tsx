import React from 'react';
import { StyleSheet, View } from 'react-native';
import { Colors, Radius, Spacing, Shadows } from '../../constants/theme';
export function Card({ children, style }: { children: React.ReactNode; style?: any }) { return <View style={[styles.card, style]}>{children}</View>; }
const styles = StyleSheet.create({ card: { backgroundColor: Colors.surface, borderRadius: Radius.md, padding: Spacing.lg, marginBottom: Spacing.sm, ...Shadows.card } });
