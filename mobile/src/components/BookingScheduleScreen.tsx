import React from 'react';
import { StyleSheet, Text, View, Image, TouchableOpacity } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';

interface BookingScheduleScreenProps {
  onNext: () => void;
  onBack: () => void;
}

export function BookingScheduleScreen({ onNext, onBack }: BookingScheduleScreenProps) {
  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      <View style={styles.header}>
        <TouchableOpacity onPress={onBack} style={styles.iconButton}>
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} />
        </TouchableOpacity>
        <Text style={styles.headerLabel}>ONBOARDING · 02/02</Text>
        <View style={{ width: 32 }} />
      </View>

      <View style={styles.heroContainer}>
        <View style={styles.phoneFrame}>
          <Image
            source={require('../../assets/booking_schedule.png')}
            style={styles.mockupImage}
            resizeMode="cover"
          />
        </View>
      </View>

      <View style={styles.bottomCard}>
        <Text style={styles.headline}>SHIFT SCHEDULING</Text>
        <View style={styles.titleUnderline} />
        <Text style={styles.description}>
          Track assigned routes, shift windows, and pickup slots in a unified dispatch calendar with real-time updates.
        </Text>

        <View style={styles.footerRow}>
          <View style={styles.indicators}>
            <View style={styles.dot} />
            <View style={[styles.dot, styles.dotActive]} />
          </View>

          <TouchableOpacity style={styles.nextButton} activeOpacity={0.85} onPress={onNext}>
            <MaterialCommunityIcons name="check" size={14} color={Colors.textOnPrimary} />
            <Text style={styles.nextButtonText}>FINISH</Text>
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
  iconButton: {
    width: 32,
    height: 32,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    alignItems: 'center',
    justifyContent: 'center',
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
