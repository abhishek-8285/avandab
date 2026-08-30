import React, { useEffect } from 'react';
import { StyleSheet, Text, View, Animated } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Font, Radius } from '../constants/theme';

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
      <StatusBar style="light" backgroundColor="#075e54" />

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
          <MaterialCommunityIcons name="truck-fast" size={54} color="#008069" />
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
    backgroundColor: '#075e54',
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
    backgroundColor: '#e7ffdb',
    borderWidth: 2,
    borderColor: '#25d366',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 20,
    elevation: 4,
  },
  textContainer: {
    alignItems: 'center',
  },
  brandTitle: {
    color: '#ffffff',
    fontSize: 26,
    fontWeight: '900',
    letterSpacing: 4,
    fontFamily: Font.mono,
  },
  divider: {
    width: 44,
    height: 2,
    backgroundColor: '#25d366',
    marginVertical: 8,
  },
  brandSubtitle: {
    color: '#dcf8c6',
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 3,
    fontFamily: Font.mono,
  },
  versionTag: {
    color: 'rgba(255,255,255,0.7)',
    fontSize: 10,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 32,
  },
});
