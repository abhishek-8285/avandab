import React, { useEffect, useState } from 'react';
import { StyleSheet, Text, TouchableOpacity } from 'react-native';
import { t } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
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
  const locale = useLanguageStore((s) => s.locale);
  const [result, setResult] = useState<ComplianceResult | null>(null);

  useEffect(() => {
    if (!vehicleId) return;
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
      accessibilityRole="button"
      accessibilityLiveRegion="polite"
      accessibilityLabel={t(`compliance.score_${result.score}`, `compliance.score_${result.score}`, locale)}
    >
      <Text
        style={[styles.scoreText, { color: SCORE_FG[result.score] }]}
        numberOfLines={1}
        ellipsizeMode="tail"
      >
        {t(`compliance.score_${result.score}`, `compliance.score_${result.score}`, locale)}
      </Text>
      {result.score === 'amber' && (
        <Text
          style={[styles.summaryText, { color: SCORE_FG[result.score] }]}
          numberOfLines={1}
          ellipsizeMode="tail"
        >
          {`${result.expired.length} ${t('compliance.expired', 'expired', locale)} · ${result.expiringSoon.length} ${t('compliance.expiring_soon', 'expiring soon', locale)}`}
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
    fontSize: FontSize.label,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  summaryText: {
    fontSize: FontSize.caption,
    fontWeight: '700',
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
});
