import React, { useCallback, useEffect, useState } from 'react';
import { Alert, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { generateEwayBill, getEwayBill } from '../services/ewaybill';

interface EWayBillCardProps {
  tripId?: string | null;
  totalValue?: number | null;
}

interface NormalizedEwb {
  number: string;
  validUntil: string;
  qrData: string;
}

// GST rule threshold mirrored client-side for the generate guard.
const EWB_THRESHOLD = 50000;

// Backend may return snake_case or camelCase — normalize defensively.
function normalizeEwb(raw: unknown): NormalizedEwb {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    number:
      typeof r.ewayBillNumber === 'string'
        ? r.ewayBillNumber
        : typeof r.eway_bill_number === 'string'
          ? r.eway_bill_number
          : '',
    validUntil:
      typeof r.validUntil === 'string'
        ? r.validUntil
        : typeof r.valid_until === 'string'
          ? r.valid_until
          : '',
    qrData:
      typeof r.qrData === 'string'
        ? r.qrData
        : typeof r.qr_data === 'string'
          ? r.qr_data
          : '',
  };
}

export function EWayBillCard({ tripId, totalValue }: EWayBillCardProps) {
  const { t } = useTranslation();
  const [bill, setBill] = useState<NormalizedEwb | null>(null);
  const [loading, setLoading] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!tripId) return;
    setLoading(true);
    setLoadError(null);
    try {
      const res = await getEwayBill(tripId);
      setBill(res ? normalizeEwb(res) : null);
    } catch (e: any) {
      setBill(null);
      setLoadError(e?.message || 'Failed to load E-Way Bill');
    } finally {
      setLoading(false);
    }
  }, [tripId]);

  useEffect(() => {
    setBill(null);
    setLoadError(null);
    load();
  }, [load]);

  if (!tripId) return null;

  const handleGenerate = async () => {
    if (!tripId || generating || totalValue == null) return;
    setGenerating(true);
    try {
      await generateEwayBill(tripId, { totalValue });
      await load();
    } catch (e: any) {
      Alert.alert(t('ewb.title'), e?.message || 'Failed to generate E-Way Bill');
    } finally {
      setGenerating(false);
    }
  };

  if (loading) {
    return (
      <View style={styles.card}>
        <Text style={styles.title}>{t('ewb.title')}</Text>
        <Text style={styles.hint}>{t('common.loading')}</Text>
      </View>
    );
  }

  if (loadError) {
    return (
      <View style={styles.card}>
        <Text style={styles.title}>{t('ewb.title')}</Text>
        <Text style={styles.errorText}>{loadError}</Text>
        <TouchableOpacity
          style={styles.generateBtn}
          onPress={load}
          accessibilityRole="button"
          accessibilityLabel="ewb-retry"
        >
          <Text style={styles.generateBtnText}>{t('common.retry')}</Text>
        </TouchableOpacity>
      </View>
    );
  }

  if (bill) {
    return (
      <View style={styles.card}>
        <Text style={styles.title}>{t('ewb.title')}</Text>
        <Text style={styles.number}>{bill.number}</Text>
        <Text style={styles.meta}>{t('ewb.valid_until', { date: bill.validUntil })}</Text>
        <Text style={styles.qrLabel}>QR PAYLOAD</Text>
        <Text selectable style={styles.qrData}>
          {bill.qrData}
        </Text>
      </View>
    );
  }

  if (totalValue == null || totalValue < EWB_THRESHOLD) {
    return (
      <View style={styles.card}>
        <Text style={styles.title}>{t('ewb.title')}</Text>
        <Text style={styles.hint}>{t('ewb.below_threshold')}</Text>
      </View>
    );
  }

  return (
    <View style={styles.card}>
      <Text style={styles.title}>{t('ewb.title')}</Text>
      <TouchableOpacity
        style={[styles.generateBtn, generating && { opacity: 0.6 }]}
        onPress={handleGenerate}
        disabled={generating}
        accessibilityRole="button"
        accessibilityLabel="ewb-generate"
      >
        <Text style={styles.generateBtnText}>{t('ewb.generate')}</Text>
      </TouchableOpacity>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.sm,
    padding: Spacing.md,
    borderWidth: 1,
    borderColor: Colors.borderLight,
    gap: Spacing.xs,
  },
  title: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  number: {
    fontSize: 16,
    fontWeight: '800',
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  meta: {
    fontSize: 10,
    fontWeight: '600',
    color: Colors.textSecondary,
  },
  qrLabel: {
    marginTop: Spacing.xs,
    fontSize: 8,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  qrData: {
    fontSize: 9,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
  },
  hint: {
    fontSize: 11,
    color: Colors.textMuted,
  },
  errorText: {
    fontSize: 10,
    color: Colors.danger,
  },
  generateBtn: {
    marginTop: Spacing.xs,
    backgroundColor: Colors.primary,
    paddingVertical: 10,
    paddingHorizontal: Spacing.md,
    borderRadius: Radius.sm,
    alignItems: 'center',
  },
  generateBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
});
