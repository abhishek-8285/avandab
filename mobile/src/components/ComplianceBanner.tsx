import React, { useEffect, useState } from 'react';
import { StyleSheet, Text, TouchableOpacity } from 'react-native';
import { useTranslation } from 'react-i18next';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { fetchCompliance, type ComplianceResult } from '../services/compliance';

interface ComplianceBannerProps {
  vehicleId?: string | null;
  onPressDetails?: () => void;
}

const SCORE_BG: Record<ComplianceResult['score'], string> = {
  green: Colors.successBg,
  amber: Colors.warningBg,
  red: Colors.dangerBg,
};

const SCORE_FG: Record<ComplianceResult['score'], string> = {
  green: Colors.success,
  amber: Colors.warning,
  red: Colors.danger,
};

export function ComplianceBanner({ vehicleId, onPressDetails }: ComplianceBannerProps) {
  const { t } = useTranslation();
  const [result, setResult] = useState<ComplianceResult | null>(null);

  useEffect(() => {
    if (!vehicleId) {
      setResult(null);
      return;
    }
    let cancelled = false;
    fetchCompliance(vehicleId)
      .then((res) => {
        if (!cancelled) setResult(res);
      })
      .catch(() => {
        // Fetch failure degrades to no banner — never crash
        if (!cancelled) setResult(null);
      });
    return () => {
      cancelled = true;
    };
  }, [vehicleId]);

  if (!vehicleId || !result) return null;

  return (
    <TouchableOpacity
      style={[styles.banner, { backgroundColor: SCORE_BG[result.score] }]}
      activeOpacity={onPressDetails ? 0.85 : 1}
      onPress={onPressDetails}
      disabled={!onPressDetails}
      accessibilityRole="text"
      accessibilityLabel={t(`compliance.score_${result.score}`)}
    >
      <Text style={[styles.scoreText, { color: SCORE_FG[result.score] }]}>
        {t(`compliance.score_${result.score}`)}
      </Text>
      {result.score === 'amber' && (
        <Text style={[styles.summaryText, { color: SCORE_FG[result.score] }]}>
          {`${result.expired.length} expired · ${result.expiringSoon.length} expiring soon`}
        </Text>
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  banner: {
    width: '100%',
    minHeight: 48,
    borderRadius: Radius.sm,
    padding: Spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  scoreText: {
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  summaryText: {
    fontSize: 9,
    fontWeight: '700',
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
});
