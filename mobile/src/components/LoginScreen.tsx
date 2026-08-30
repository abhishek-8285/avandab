import React, { useState } from 'react';
import { StyleSheet, Text, View, TextInput, TouchableOpacity, ActivityIndicator, Alert } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

interface LoginScreenProps {
  onLoginSuccess: () => void;
  onForgotPassword?: () => void;
  onRegisterLink?: () => void;
}

export function LoginScreen({ onLoginSuccess, onForgotPassword, onRegisterLink }: LoginScreenProps) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);

  const setAuth = useAuthStore((state) => state.setAuth);

  const handleSignIn = async () => {
    if (!email || !password) {
      Alert.alert('Missing Fields', 'Please enter both email and password.');
      return;
    }

    setLoading(true);

    try {
      const targetUrl = `${getApiBaseURL()}/api/v1/auth/token`;

      const response = await fetch(targetUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });

      if (!response.ok) {
        const errText = await response.text();
        setLoading(false);
        Alert.alert('Sign In Failed', errText || `Server returned HTTP ${response.status}.`);
        return;
      }

      const data = await response.json();

      if (!data.token || !data.user_id) {
        setLoading(false);
        Alert.alert('Sign In Failed', data.error || 'Server response missing token or user_id.');
        return;
      }

      // Use server-provided name/email when available for consistency with registration
      const serverName = (data.name as string) || email.split('@')[0];
      const serverEmail = (data.email as string) || email;
      const serverRole = (data.role as string) || 'driver';
      await setAuth(data.token, {
        id: data.user_id,
        name: serverName,
        role: serverRole,
        email: serverEmail,
      });

      // Fetch driver profile to retrieve driverId only - do not overwrite name
      // to keep register/login consistent (both use users.Name). Driver profile
      // name divergence (e.g. abhishek vs testcheck) is handled by not clobbering.
      try {
        const meRes = await fetch(`${getApiBaseURL()}/api/v1/drivers/me`, {
          headers: { Authorization: `Bearer ${data.token}` },
        });
        if (meRes.ok) {
          const me = await meRes.json();
          if (me.driver_id) {
            useAuthStore.getState().setDriverId(me.driver_id);
          }
        }
      } catch {
        // Driver profile fetch failed; proceed with basic auth
      }

      setLoading(false);
      onLoginSuccess();
    } catch (err: any) {
      setLoading(false);
      Alert.alert('Sign In Failed', err?.message || 'Unable to reach the server. Please try again.');
    }
  };

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      {/* Dark chrome header */}
      <View style={styles.header}>
        <Text style={styles.brandTitle}>AVANDAB</Text>
        <Text style={styles.brandSubtitle}>DRIVER OPS · AUTH</Text>
      </View>

      {/* Main Login Card */}
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Text style={styles.cardTitle}>SIGN IN</Text>
          <View style={styles.headerUnderline} />
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>EMAIL</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="email-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} />
            <TextInput
              style={styles.input}
              placeholder="driver@avandab.com"
              placeholderTextColor={Colors.textMuted}
              value={email}
              onChangeText={setEmail}
              keyboardType="email-address"
              autoCapitalize="none"
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <View style={styles.labelRow}>
            <Text style={styles.label}>PASSWORD</Text>
            {onForgotPassword && (
              <TouchableOpacity onPress={onForgotPassword}>
                <Text style={styles.forgotText}>FORGOT?</Text>
              </TouchableOpacity>
            )}
          </View>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="lock-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} />
            <TextInput
              style={[styles.input, { paddingRight: 40 }]}
              placeholder="••••••••"
              placeholderTextColor={Colors.textMuted}
              value={password}
              onChangeText={setPassword}
              secureTextEntry={!showPassword}
            />
            <TouchableOpacity style={styles.eyeIcon} onPress={() => setShowPassword(!showPassword)}>
              <MaterialCommunityIcons
                name={showPassword ? 'eye-off-outline' : 'eye-outline'}
                size={16}
                color={Colors.textMuted}
              />
            </TouchableOpacity>
          </View>
        </View>

        <TouchableOpacity
          style={styles.submitBtn}
          activeOpacity={0.88}
          onPress={handleSignIn}
          disabled={loading}
        >
          {loading ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <View style={styles.btnContent}>
              <Text style={styles.submitBtnText}>ENTER DUTY (ड्यूटी शुरू करें)</Text>
              <MaterialCommunityIcons name="arrow-right" size={16} color={Colors.textOnPrimary} />
            </View>
          )}
        </TouchableOpacity>

        {/* 1-Tap Quick Driver Demo Sign-In */}
        <TouchableOpacity
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 8,
            backgroundColor: '#e7ffdb',
            borderWidth: 1,
            borderColor: '#bbf7d0',
            paddingVertical: 12,
            borderRadius: Radius.md,
            marginTop: 12,
          }}
          activeOpacity={0.85}
          onPress={() => {
            setEmail('driver@avandab.com');
            setPassword('password123');
          }}
        >
          <MaterialCommunityIcons name="truck-fast" size={16} color="#008069" />
          <Text style={{ fontSize: 11, fontWeight: '800', color: '#008069' }}>
            AUTO-FILL DRIVER (Abhishek • DL-01)
          </Text>
        </TouchableOpacity>

        {onRegisterLink && (
          <TouchableOpacity style={styles.registerLink} onPress={onRegisterLink}>
            <Text style={styles.registerLinkText}>
              No driver account? <Text style={styles.registerLinkHighlight}>REGISTER</Text>
            </Text>
          </TouchableOpacity>
        )}
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
    backgroundColor: Colors.chrome,
    paddingHorizontal: Spacing.xl,
    paddingTop: 56,
    paddingBottom: Spacing.xl,
  },
  brandTitle: {
    fontSize: 22,
    fontWeight: '900',
    color: Colors.textOnChrome,
    letterSpacing: 4,
    fontFamily: Font.mono,
  },
  brandSubtitle: {
    fontSize: 10,
    color: Colors.textOnChromeMuted,
    fontWeight: '700',
    letterSpacing: 2,
    fontFamily: Font.mono,
    marginTop: 4,
  },
  card: {
    flex: 1,
    backgroundColor: Colors.surface,
    margin: Spacing.lg,
    borderRadius: Radius.lg,
    padding: Spacing.xl,
    borderWidth: 1,
    borderColor: Colors.border,
  },
  cardHeader: {
    marginBottom: Spacing.xl,
  },
  cardTitle: {
    fontSize: 16,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  headerUnderline: {
    width: 28,
    height: 2,
    backgroundColor: Colors.primary,
    marginTop: 6,
  },
  formGroup: {
    marginBottom: Spacing.lg,
  },
  labelRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 6,
  },
  label: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  forgotText: {
    fontSize: 10,
    color: Colors.primary,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  inputWrapper: {
    position: 'relative',
    justifyContent: 'center',
  },
  inputIcon: {
    position: 'absolute',
    left: 10,
    zIndex: 10,
  },
  input: {
    height: 44,
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.md,
    paddingLeft: 34,
    paddingRight: 12,
    fontSize: 13,
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  eyeIcon: {
    position: 'absolute',
    right: 10,
    padding: 4,
  },
  submitBtn: {
    height: 46,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 4,
    marginBottom: Spacing.lg,
  },
  btnContent: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  submitBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  registerLink: {
    alignItems: 'center',
    paddingVertical: 8,
  },
  registerLinkText: {
    fontSize: 11,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    letterSpacing: 0.5,
  },
  registerLinkHighlight: {
    color: Colors.primary,
    fontWeight: '800',
  },
});
