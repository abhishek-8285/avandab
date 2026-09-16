import React, { useEffect } from 'react';
import { StyleSheet, Text, View, Animated, StatusBar } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, FontSize} from '../constants/theme';

interface SplashScreenProps {
  onFinish: () => void;
}

export function SplashScreen({ onFinish }: SplashScreenProps) {
  const logoOpacity = new Animated.Value(0);
  const logoTranslateY = new Animated.Value(10);
  const textOpacity = new Animated.Value(0);
  const textTranslateY = new Animated.Value(10);

  useEffect(() => {
    Animated.sequence([
      Animated.parallel([
        Animated.timing(logoOpacity, {
          toValue: 1,
          duration: 400,
          useNativeDriver: true,
        }),
        Animated.timing(logoTranslateY, {
          toValue: 0,
          duration: 400,
          useNativeDriver: true,
        }),
      ]),
      Animated.parallel([
        Animated.timing(textOpacity, {
          toValue: 1,
          duration: 300,
          useNativeDriver: true,
        }),
        Animated.timing(textTranslateY, {
          toValue: 0,
          duration: 300,
          useNativeDriver: true,
        }),
      ]),
    ]).start();

    const timer = setTimeout(() => {
      onFinish();
    }, 250);

    return () => clearTimeout(timer);
  }, []);

  return (
    <View style={styles.container}>
      <StatusBar barStyle="light-content" backgroundColor={Colors.chrome} />

      <View style={styles.content}>
        <Animated.View
          style={[
            styles.logoContainer,
            {
              opacity: logoOpacity,
              transform: [{ translateY: logoTranslateY }],
            },
          ]}
        >
          <MaterialCommunityIcons name="truck-fast" size={54} color={Colors.primary} />
        </Animated.View>

        <Animated.View
          style={[
            styles.textContainer,
            {
              opacity: textOpacity,
              transform: [{ translateY: textTranslateY }],
            },
          ]}
        >
          <Text style={styles.brandTitle}>AVANDAB</Text>
          <View style={styles.divider} />
          <Text style={styles.brandSubtitle}>DRIVER OPS</Text>
        </Animated.View>

        <Text style={styles.versionTag}>v2.4.1 · FLEET MOBILE</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.chrome,
    justifyContent: 'center',
    alignItems: 'center',
  },
  content: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  logoContainer: {
    width: 92,
    height: 92,
    borderRadius: Radius.lg,
    backgroundColor: Colors.primarySubtle,
    borderWidth: 2,
    borderColor: Colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 20,
    elevation: 4,
  },
  textContainer: {
    alignItems: 'center',
  },
  brandTitle: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.displayLarge,
    fontWeight: '900',
    letterSpacing: 4,
    fontFamily: Font.mono,
  },
  divider: {
    width: 44,
    height: 2,
    backgroundColor: Colors.accent,
    marginVertical: 8,
  },
  brandSubtitle: {
    color: Colors.primary,
    fontSize: FontSize.body,
    fontWeight: '800',
    letterSpacing: 3,
    fontFamily: Font.mono,
  },
  versionTag: {
    color: 'rgba(255,255,255,0.7)',
    fontSize: FontSize.small,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 32,
  },
});