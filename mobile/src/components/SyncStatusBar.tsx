import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { Colors, Font } from '../constants/theme';
import { useSyncStore, type SyncStatus } from '../stores/syncStore';

const STATUS_COLOR: Record<SyncStatus, string> = {
  online_synced: Colors.success,
  syncing: Colors.info,
  offline_saved: Colors.warning,
  error: Colors.danger,
};

export function SyncStatusBar() {
  const status = useSyncStore((s) => s.status);
  const { t } = useTranslation();
  const labelKey = `sync.status_${status}`;

  return (
    <View style={styles.bar} accessibilityLabel={labelKey} accessibilityRole="text">
      <View style={[styles.dot, { backgroundColor: STATUS_COLOR[status] ?? Colors.warning }]} />
      <Text style={styles.label}>{t(labelKey)}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: Colors.chrome,
    paddingVertical: 3,
  },
  dot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  label: {
    color: Colors.textOnChromeMuted,
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
});
