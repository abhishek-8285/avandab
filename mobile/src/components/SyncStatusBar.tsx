import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Colors, Font, Radius, FontSize} from '../constants/theme';
import { useSyncStore, type SyncStatus } from '../stores/syncStore';
import { useLanguageStore } from '../stores/languageStore';
import { t } from '../i18n';

const STATUS_COLOR: Record<SyncStatus, string> = {
  online_synced: Colors.accent,
  syncing: Colors.success,
  offline_saved: Colors.warning,
  error: Colors.danger,
};

const STATUS_FALLBACKS: Record<SyncStatus, string> = {
  online_synced: 'Online — Synced',
  syncing: 'Syncing…',
  offline_saved: 'Offline — Saved',
  error: 'Sync error',
};

export function SyncStatusBar() {
  const status = useSyncStore((s) => s.status);
  const { locale } = useLanguageStore();
  const labelKey = `sync.status_${status}`;
  const label = t(labelKey, STATUS_FALLBACKS[status] ?? 'Online', locale);

  return (
    <View style={styles.bar} accessibilityLabel={label} accessibilityRole="text" accessibilityLiveRegion="polite">
      <View style={[styles.dot, { backgroundColor: STATUS_COLOR[status] ?? Colors.accent }]} />
      <Text style={styles.label} numberOfLines={1} ellipsizeMode="tail">{label}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 5,
    backgroundColor: 'rgba(0,0,0,0.18)',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.15)',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: Radius.full,
  },
  dot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  label: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.small,
    fontWeight: '700',
  },
});
