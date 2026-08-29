import React from 'react';
import { StyleSheet, Text, View, ActivityIndicator, TouchableOpacity } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Spacing } from '../../constants/theme';
import { Card } from './Card';
type State = 'loading' | 'empty' | 'error' | 'offline';
export function StateView({ state, title, message, icon, onRetry }: { state: State; title?: string; message?: string; icon?: string; onRetry?: () => void }) {
  if (state === 'loading') return (<View style={styles.center}><ActivityIndicator color={Colors.primary} /></View>);
  const map: any = { empty: { defaultTitle: 'Nothing here', defaultIcon: 'inbox-outline' }, error: { defaultTitle: 'Something went wrong', defaultIcon: 'alert-circle-outline' }, offline: { defaultTitle: 'You are offline', defaultIcon: 'wifi-off' } };
  const cfg = map[state];
  return (<Card style={styles.card}><View style={styles.iconBox}><MaterialCommunityIcons name={(icon as any) || cfg.defaultIcon} size={28} color={Colors.textMuted} /></View><Text style={styles.title}>{title || cfg.defaultTitle}</Text>{message ? <Text style={styles.message}>{message}</Text> : null}{onRetry ? (<TouchableOpacity style={styles.retryBtn} onPress={onRetry}><Text style={styles.retryText}>Retry</Text></TouchableOpacity>) : null}</Card>);
}
const styles = StyleSheet.create({
  center: { padding: Spacing.xl, alignItems: 'center' }, card: { alignItems: 'center', paddingVertical: 32 },
  iconBox: { width: 64, height: 64, borderRadius: 32, backgroundColor: Colors.background, alignItems: 'center', justifyContent: 'center', marginBottom: 12 },
  title: { fontSize: 14, fontWeight: '700', color: Colors.textPrimary, textAlign: 'center' }, message: { fontSize: 12, color: Colors.textSecondary, textAlign: 'center', marginTop: 8, lineHeight: 18 },
  retryBtn: { marginTop: 12, backgroundColor: Colors.primary, paddingHorizontal: 16, paddingVertical: 8, borderRadius: 8 }, retryText: { color: Colors.textOnPrimary, fontSize: 12, fontWeight: '700' },
});
