import React, { useEffect, useRef } from 'react';
import { StyleSheet, Text, View, Image, TouchableOpacity, Animated } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';

interface GetStartedScreenProps {
  onGetStarted: () => void;
  onSignIn: () => void;
}

export function GetStartedScreen({ onGetStarted, onSignIn }: GetStartedScreenProps) {
  const pulseAnim = useRef(new Animated.Value(0)).current;

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
      <View style={styles.heroSection}>
        <Image
          source={require('../../assets/driver_hero.png')}
          style={styles.heroImage}
          resizeMode="cover"
        />
        <View style={styles.heroOverlay} />

        {/* Top status strip */}
        <View style={styles.statusStrip}>
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
            <Text style={styles.statusText}>NETWORK ONLINE</Text>
          </View>
          <Text style={styles.timestamp}>14:32 IST</Text>
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
      <View style={styles.contentSheet}>
        <Text style={styles.headline}>JOIN THE FLEET</Text>
        <View style={styles.titleUnderline} />

        <Text style={styles.description}>
          Stream live GPS telemetry, execute regional freight routes, and secure instant digital proof-of-delivery on the Avandab network.
        </Text>

        <View style={styles.featureList}>
          <View style={styles.featureRow}>
            <MaterialCommunityIcons name="radar" size={14} color={Colors.primary} />
            <Text style={styles.featureText}>Live trip assignments & dispatch updates</Text>
          </View>
          <View style={styles.featureRow}>
            <MaterialCommunityIcons name="barcode-scan" size={14} color={Colors.primary} />
            <Text style={styles.featureText}>Photo & signature proof-of-delivery</Text>
          </View>
          <View style={styles.featureRow}>
            <MaterialCommunityIcons name="map-marker-path" size={14} color={Colors.primary} />
            <Text style={styles.featureText}>Turn-by-turn navigation & GPS tracking</Text>
          </View>
        </View>

        <TouchableOpacity
          style={styles.primaryButton}
          activeOpacity={0.88}
          onPress={onGetStarted}
        >
          <Text style={styles.primaryButtonText}>GET STARTED</Text>
          <MaterialCommunityIcons name="arrow-right" size={16} color={Colors.textOnPrimary} />
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.signInButton}
          activeOpacity={0.7}
          onPress={onSignIn}
        >
          <Text style={styles.signInText}>
            Already registered? <Text style={styles.signInLink}>SIGN IN</Text>
          </Text>
        </TouchableOpacity>
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
    height: '48%',
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
    backgroundColor: 'rgba(15, 23, 42, 0.7)',
  },
  statusStrip: {
    position: 'absolute',
    top: 56,
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
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  timestamp: {
    color: Colors.textOnChromeMuted,
    fontSize: 10,
    fontWeight: '600',
    fontFamily: Font.mono,
    letterSpacing: 1,
  },
  heroStats: {
    position: 'absolute',
    bottom: Spacing.lg,
    left: Spacing.lg,
    right: Spacing.lg,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: 'rgba(15, 23, 42, 0.6)',
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    borderRadius: Radius.lg,
    paddingVertical: 10,
    paddingHorizontal: Spacing.md,
  },
  heroStat: {
    flex: 1,
    alignItems: 'center',
  },
  heroStatDivider: {
    width: 1,
    height: 22,
    backgroundColor: Colors.chromeBorder,
  },
  heroStatValue: {
    color: Colors.textOnChrome,
    fontSize: 16,
    fontWeight: '800',
    fontFamily: Font.mono,
  },
  heroStatLabel: {
    color: Colors.textOnChromeMuted,
    fontSize: 8,
    fontWeight: '700',
    letterSpacing: 0.5,
    marginTop: 2,
    fontFamily: Font.mono,
  },
  contentSheet: {
    flex: 1,
    backgroundColor: Colors.surface,
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.xxl,
    borderTopLeftRadius: Radius.lg,
    borderTopRightRadius: Radius.lg,
  },
  headline: {
    fontSize: 22,
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
    marginBottom: Spacing.lg,
  },
  featureList: {
    gap: 8,
    marginBottom: Spacing.xl,
  },
  featureRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  featureText: {
    fontSize: 12,
    color: Colors.textPrimary,
    fontWeight: '600',
  },
  primaryButton: {
    width: '100%',
    height: 50,
    borderRadius: Radius.md,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 8,
  },
  primaryButtonText: {
    color: Colors.textOnPrimary,
    fontSize: 14,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  signInButton: {
    paddingVertical: Spacing.md,
    alignItems: 'center',
    marginTop: 8,
  },
  signInText: {
    fontSize: 12,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    letterSpacing: 0.5,
  },
  signInLink: {
    color: Colors.primary,
    fontWeight: '800',
  },
});
