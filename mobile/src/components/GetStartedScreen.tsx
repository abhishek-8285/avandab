import React, { useEffect, useRef } from 'react';
import { StyleSheet, Text, View, Image, TouchableOpacity, Animated, ScrollView } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';

interface GetStartedScreenProps {
  onGetStarted: () => void;
  onSignIn: () => void;
  onOpenQRDemo: () => void;
}

export function GetStartedScreen({ onGetStarted, onSignIn, onOpenQRDemo }: GetStartedScreenProps) {
  const pulseAnim = useRef(new Animated.Value(0)).current;
  const insets = useSafeAreaInsets();

  useEffect(() => {
    Animated.loop(
      Animated.sequence([
        Animated.timing(pulseAnim, {
          toValue: 1,
          duration: 1200,
          useNativeDriver: true,
        }),
        Animated.timing(pulseAnim, {
          toValue: 0,
          duration: 1200,
          useNativeDriver: true,
        }),
      ])
    ).start();
  }, []);

  return (
    <View style={styles.container}>
      <StatusBar style="light" translucent backgroundColor="transparent" />

      {/* Hero with dark overlay + operational HUD */}
      <View style={[styles.heroSection, { paddingTop: insets.top }]}>
        <Image
          source={require('../../assets/driver_hero.png')}
          style={styles.heroImage}
          resizeMode="cover"
          accessible={false}
        />
        <View style={styles.heroOverlay} />

        {/* Top status strip */}
        <View style={[styles.statusStrip, { top: insets.top + 12 }]}>
          <View style={styles.statusItem}>
            <Animated.View
              style={[
                styles.statusDot,
                {
                  opacity: pulseAnim.interpolate({
                    inputRange: [0, 1],
                    outputRange: [0.4, 1],
                  }),
                },
              ]}
            />
            <Text style={styles.statusText}>AVANDAB NETWORK</Text>
          </View>
          <View style={styles.statusLiveBadge}>
            <Text style={styles.statusLiveText}>● LIVE</Text>
          </View>
        </View>

        {/* Bottom hero overlay stats - driver ops focus, no earning pitch */}
        <View style={styles.heroStats}>
          <View style={styles.heroStat}>
            <Text style={styles.heroStatValue}>LIVE</Text>
            <Text style={styles.heroStatLabel}>DISPATCH</Text>
          </View>
          <View style={styles.heroStatDivider} />
          <View style={styles.heroStat}>
            <Text style={styles.heroStatValue}>GPS</Text>
            <Text style={styles.heroStatLabel}>TRACKING</Text>
          </View>
          <View style={styles.heroStatDivider} />
          <View style={styles.heroStat}>
            <Text style={styles.heroStatValue}>POD</Text>
            <Text style={styles.heroStatLabel}>VERIFIED</Text>
          </View>
        </View>
      </View>

      {/* Bottom Content Panel */}
      <View style={[styles.contentSheet, { paddingBottom: Math.max(insets.bottom, 16) + 16 }]}>
        <ScrollView
          contentContainerStyle={styles.contentScroll}
          showsVerticalScrollIndicator={false}
          bounces={false}
        >
          <Text style={styles.headline}>JOIN THE FLEET</Text>
          <View style={styles.titleUnderline} />

          <Text style={styles.description}>
            Stream live GPS telemetry, execute regional freight routes, and secure instant digital proof-of-delivery on the Avandab network.
          </Text>

          <View style={styles.featureList}>
            <View style={styles.featureRow}>
              <View style={styles.featureIconBox}>
                <View style={styles.featureIconDot} />
              </View>
              <Text style={styles.featureText}>Live trip assignments & dispatch updates</Text>
            </View>
            <View style={styles.featureRow}>
              <View style={styles.featureIconBox}>
                <View style={[styles.featureIconDot, styles.featureIconDot2]} />
              </View>
              <Text style={styles.featureText}>Photo & signature proof-of-delivery</Text>
            </View>
            <View style={styles.featureRow}>
              <View style={styles.featureIconBox}>
                <View style={[styles.featureIconDot, styles.featureIconDot3]} />
              </View>
              <Text style={styles.featureText}>Turn-by-turn navigation & GPS tracking</Text>
            </View>
          </View>

          <TouchableOpacity
            style={styles.primaryButton}
            activeOpacity={0.88}
            onPress={onGetStarted}
            accessibilityRole="button"
            accessibilityLabel="Get started"
          >
            <Text style={styles.primaryButtonText}>GET STARTED</Text>
            <Text style={styles.primaryButtonArrow}>→</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.signInButton}
            activeOpacity={0.7}
            onPress={onSignIn}
            accessibilityRole="button"
            accessibilityLabel="Sign in"
          >
            <Text style={styles.signInText}>
              Already registered? <Text style={styles.signInLink}>SIGN IN</Text>
            </Text>
          </TouchableOpacity>

          {/* QR Demo removed from public onboarding — accessible via Dev menu / QR tab after login */}
          {false && __DEV__ ? (
            <TouchableOpacity
              style={styles.qrDemoButtonDev}
              activeOpacity={0.7}
              onPress={onOpenQRDemo}
              accessibilityRole="button"
              accessibilityLabel="Open QR demo"
            >
              <MaterialCommunityIcons name="qrcode-scan" size={12} color={Colors.textMuted} />
              <Text style={styles.qrDemoTextDev}>QR SCANNER / GENERATOR (DEV)</Text>
            </TouchableOpacity>
          ) : null}
        </ScrollView>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.chrome,
  },
  heroSection: {
    height: '46%',
    minHeight: 280,
    width: '100%',
    position: 'relative',
    backgroundColor: Colors.chrome,
  },
  heroImage: {
    width: '100%',
    height: '100%',
  },
  heroOverlay: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(15, 23, 42, 0.62)',
  },
  statusStrip: {
    position: 'absolute',
    left: Spacing.lg,
    right: Spacing.lg,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  statusItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: 'rgba(15, 23, 42, 0.45)',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 9999,
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#22c55e',
  },
  statusText: {
    color: Colors.textOnChrome,
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 1.2,
    fontFamily: Font.mono,
  },
  statusLiveBadge: {
    backgroundColor: 'rgba(34, 197, 94, 0.15)',
    borderWidth: 1,
    borderColor: 'rgba(34, 197, 94, 0.3)',
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 9999,
  },
  statusLiveText: {
    color: '#22c55e',
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  heroStats: {
    position: 'absolute',
    bottom: Spacing.lg,
    left: Spacing.lg,
    right: Spacing.lg,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: 'rgba(15, 23, 42, 0.72)',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.12)',
    borderRadius: Radius.lg,
    paddingVertical: 12,
    paddingHorizontal: Spacing.md,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.18,
    shadowRadius: 8,
    elevation: 4,
  },
  heroStat: {
    flex: 1,
    alignItems: 'center',
  },
  heroStatDivider: {
    width: 1,
    height: 28,
    backgroundColor: 'rgba(255,255,255,0.12)',
  },
  heroStatValue: {
    color: '#ffffff',
    fontSize: 16,
    fontWeight: '900',
    fontFamily: Font.mono,
    letterSpacing: 0.5,
  },
  heroStatLabel: {
    color: 'rgba(255,255,255,0.65)',
    fontSize: 9,
    fontWeight: '700',
    letterSpacing: 1,
    marginTop: 2,
    fontFamily: Font.mono,
  },
  contentSheet: {
    flex: 1,
    backgroundColor: Colors.surface,
    borderTopLeftRadius: Radius.xl,
    borderTopRightRadius: Radius.xl,
    marginTop: -12,
    shadowColor: '#0f172a',
    shadowOffset: { width: 0, height: -2 },
    shadowOpacity: 0.08,
    shadowRadius: 12,
    elevation: 8,
  },
  contentScroll: {
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.lg,
    gap: 0,
  },
  headline: {
    fontSize: 24,
    fontWeight: '900',
    color: Colors.textPrimary,
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  titleUnderline: {
    width: 40,
    height: 3,
    backgroundColor: Colors.primary,
    marginTop: 8,
    marginBottom: Spacing.md,
    borderRadius: 2,
  },
  description: {
    fontSize: 13.5,
    color: Colors.textSecondary,
    lineHeight: 21,
    marginBottom: Spacing.lg,
  },
  featureList: {
    gap: 10,
    marginBottom: Spacing.xl,
  },
  featureRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  featureIconBox: {
    width: 28,
    height: 28,
    borderRadius: Radius.md,
    backgroundColor: Colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: 'rgba(15,118,110,0.12)',
  },
  featureIconDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    backgroundColor: Colors.primary,
  },
  featureIconDot2: {
    borderRadius: 2,
    width: 9,
    height: 9,
  },
  featureIconDot3: {
    width: 8,
    height: 8,
    borderRadius: 4,
    backgroundColor: Colors.primaryDark,
  },
  featureText: {
    flex: 1,
    fontSize: 13,
    color: Colors.textPrimary,
    fontWeight: '600',
    lineHeight: 18,
  },
  primaryButton: {
    width: '100%',
    height: 52,
    borderRadius: Radius.md,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 8,
    shadowColor: Colors.primary,
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.22,
    shadowRadius: 8,
    elevation: 3,
  },
  primaryButtonText: {
    color: Colors.textOnPrimary,
    fontSize: 14,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  primaryButtonArrow: {
    color: Colors.textOnPrimary,
    fontSize: 16,
    fontWeight: '700',
    marginLeft: 2,
  },
  signInButton: {
    paddingVertical: Spacing.md,
    alignItems: 'center',
    marginTop: 6,
    minHeight: 44,
    justifyContent: 'center',
  },
  signInText: {
    fontSize: 13,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    letterSpacing: 0.3,
  },
  signInLink: {
    color: Colors.primary,
    fontWeight: '800',
  },
  qrDemoButtonDev: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    marginTop: Spacing.sm,
    paddingVertical: 10,
    borderRadius: Radius.sm,
    borderWidth: 1,
    borderColor: Colors.borderLight,
    backgroundColor: Colors.surfaceSecondary,
    borderStyle: 'dashed',
  },
  qrDemoTextDev: {
    color: Colors.textMuted,
    fontSize: 10,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
});
