import React from 'react';
import { StyleSheet, Text, View, Image, TouchableOpacity } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { Colors, Font, Radius, Spacing } from '../constants/theme';

interface OnboardingOverviewScreenProps {
  onNext: () => void;
  onSkip: () => void;
}

export function OnboardingOverviewScreen({ onNext, onSkip }: OnboardingOverviewScreenProps) {
  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      {/* Dark chrome header */}
      <View style={styles.header}>
        <Text style={styles.headerLabel}>ONBOARDING · 01/02</Text>
        <TouchableOpacity onPress={onSkip} style={styles.skipButton}>
          <Text style={styles.skipText}>SKIP</Text>
        </TouchableOpacity>
      </View>

      {/* Phone mockup with HUD frame */}
      <View style={styles.heroContainer}>
        <View style={styles.phoneFrame}>
          <Image
            source={require('../../assets/onboarding_overview.png')}
            style={styles.mockupImage}
            resizeMode="cover"
          />
          <View style={styles.phoneCorners} />
        </View>
      </View>

      {/* Bottom panel */}
      <View style={styles.bottomCard}>
        <Text style={styles.headline}>DISPATCH READY</Text>
        <View style={styles.titleUnderline} />
        <Text style={styles.description}>
          Receive assigned trips, get real-time dispatch updates and navigate to delivery with live GPS guidance.
        </Text>

        <View style={styles.footerRow}>
          <View style={styles.indicators}>
            <View style={[styles.dot, styles.dotActive]} />
            <View style={styles.dot} />
          </View>

          <TouchableOpacity style={styles.nextButton} activeOpacity={0.85} onPress={onNext}>
            <Text style={styles.nextButtonText}>NEXT</Text>
            <Text style={styles.nextArrow}>→</Text>
          </TouchableOpacity>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: Spacing.lg,
    paddingTop: 54,
    paddingBottom: Spacing.md,
    backgroundColor: Colors.chrome,
  },
  headerLabel: {
    fontSize: 11,
    fontWeight: '700',
    color: Colors.textOnChrome,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  skipButton: {
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    borderRadius: Radius.md,
  },
  skipText: {
    fontSize: 11,
    fontWeight: '700',
    color: Colors.textOnChrome,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  heroContainer: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: Spacing.xl,
    backgroundColor: Colors.background,
  },
  phoneFrame: {
    width: 220,
    height: 380,
    borderRadius: Radius.lg,
    overflow: 'hidden',
    borderWidth: 2,
    borderColor: Colors.chrome,
    backgroundColor: Colors.surface,
  },
  mockupImage: {
    width: '100%',
    height: '100%',
  },
  phoneCorners: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    borderWidth: 1,
    borderColor: 'rgba(15, 23, 42, 0.1)',
    borderRadius: Radius.lg,
  },
  bottomCard: {
    backgroundColor: Colors.surface,
    borderTopWidth: 1,
    borderTopColor: Colors.border,
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.xxl,
  },
  headline: {
    fontSize: 20,
    fontWeight: '900',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  titleUnderline: {
    width: 32,
    height: 2,
    backgroundColor: Colors.primary,
    marginTop: 6,
    marginBottom: Spacing.md,
  },
  description: {
    fontSize: 13,
    color: Colors.textSecondary,
    lineHeight: 20,
    marginBottom: Spacing.xl,
  },
  footerRow: {
    width: '100%',
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  indicators: {
    flexDirection: 'row',
    gap: 6,
  },
  dot: {
    width: 24,
    height: 3,
    backgroundColor: Colors.border,
  },
  dotActive: {
    backgroundColor: Colors.primary,
  },
  nextButton: {
    height: 44,
    paddingHorizontal: 18,
    borderRadius: Radius.md,
    backgroundColor: Colors.primary,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  nextButtonText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  nextArrow: {
    color: Colors.textOnPrimary,
    fontSize: 14,
    fontWeight: '700',
  },
});
