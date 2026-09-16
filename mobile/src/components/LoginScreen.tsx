import React, { useState } from 'react';
import { StyleSheet, Text, View, TextInput, TouchableOpacity, ActivityIndicator } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
import { t } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

interface LoginScreenProps {
  onLoginSuccess: () => void;
  onForgotPassword?: () => void;
  onRegisterLink?: () => void;
}

export function LoginScreen({ onLoginSuccess, onForgotPassword, onRegisterLink }: LoginScreenProps) {
  const locale = useLanguageStore((s) => s.locale);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const setAuth = useAuthStore((state) => state.setAuth);

  const handleSignIn = async () => {
    if (!email || !password) {
      setFormError(t('auth.err_both', 'Please enter both email and password.', locale));
      return;
    }

    setFormError(null);
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
        setFormError(errText || t('auth.err_http', `Sign in failed (HTTP ${response.status}). Check your connection and try again.`, locale));
        return;
      }

      const data = await response.json();

      if (!data.token || !data.user_id) {
        setLoading(false);
        setFormError(data.error || t('auth.err_bad_response', 'Sign in failed: server response missing credentials. Please try again.', locale));
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
      setFormError(err?.message || t('auth.err_offline', 'Unable to reach the server. Check your connection and try again.', locale));
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
          <Text style={styles.cardTitle}>{t('auth.title', 'SIGN IN', locale)}</Text>
          <View style={styles.headerUnderline} />
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>{t('auth.email', 'EMAIL', locale)}</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="email-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="driver@avandab.com"
              placeholderTextColor={Colors.textMuted}
              value={email}
              onChangeText={setEmail}
              keyboardType="email-address"
              autoCapitalize="none"
              autoComplete="email"
              textContentType="emailAddress"
              accessibilityLabel={t('auth.a11y_email', 'Email', locale)}
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <View style={styles.labelRow}>
            <Text style={styles.label}>{t('auth.password', 'PASSWORD', locale)}</Text>
            {onForgotPassword && (
              <TouchableOpacity onPress={onForgotPassword} accessibilityRole="button" accessibilityLabel={t('auth.a11y_forgot', 'Forgot password', locale)} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
                <Text style={styles.forgotText}>{t('auth.forgot', 'FORGOT?', locale)}</Text>
              </TouchableOpacity>
            )}
          </View>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="lock-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={[styles.input, { paddingRight: 40 }]}
              placeholder={t('auth.password_placeholder', 'Enter password', locale)}
              placeholderTextColor={Colors.textMuted}
              value={password}
              onChangeText={setPassword}
              secureTextEntry={!showPassword}
              autoComplete="password"
              textContentType="password"
              accessibilityLabel={t('auth.a11y_password', 'Password', locale)}
            />
            <TouchableOpacity style={styles.eyeIcon} onPress={() => setShowPassword(!showPassword)} accessibilityRole="button" accessibilityLabel={showPassword ? t('auth.a11y_hide_pw', 'Hide password', locale) : t('auth.a11y_show_pw', 'Show password', locale)} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
              <MaterialCommunityIcons
                name={showPassword ? 'eye-off-outline' : 'eye-outline'}
                size={16}
                color={Colors.textMuted}
                accessible={false}
              />
            </TouchableOpacity>
          </View>
        </View>

        {formError ? (
          <Text style={styles.formError} accessibilityLiveRegion="polite">{formError}</Text>
        ) : null}

        <TouchableOpacity
          style={styles.submitBtn}
          activeOpacity={0.88}
          onPress={handleSignIn}
          disabled={loading}
          accessibilityRole="button"
          accessibilityLabel={loading ? t('auth.a11y_signing_in', 'Signing in', locale) : t('auth.a11y_submit', 'Enter duty', locale)}
          accessibilityState={{ disabled: loading, busy: loading }}
          accessibilityLiveRegion="polite"
        >
          {loading ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <View style={styles.btnContent}>
              <Text style={styles.submitBtnText}>{t('auth.submit', 'ENTER DUTY', locale)}</Text>
              <MaterialCommunityIcons name="arrow-right" size={16} color={Colors.textOnPrimary} accessible={false} />
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
            backgroundColor: Colors.primarySubtle,
            borderWidth: 1,
            borderColor: Colors.primaryBorder,
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
          <MaterialCommunityIcons name="truck-fast" size={16} color={Colors.primary} accessible={false} />
          <Text style={{ fontSize: FontSize.label, fontWeight: '800', color: Colors.primary }}>
            {t('auth.demo_fill', 'AUTO-FILL DRIVER (Abhishek • DL-01)', locale)}
          </Text>
        </TouchableOpacity>

        {onRegisterLink && (
          <TouchableOpacity style={styles.registerLink} onPress={onRegisterLink} accessibilityRole="button" accessibilityLabel={t('auth.a11y_register', 'Register for a driver account', locale)}>
            <Text style={styles.registerLinkText}>
              {t('auth.no_account', 'No driver account? ', locale)}<Text style={styles.registerLinkHighlight}>{t('auth.register', 'REGISTER', locale)}</Text>
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
    fontSize: FontSize.hero,
    fontWeight: '900',
    color: Colors.textOnChrome,
    letterSpacing: 4,
    fontFamily: Font.mono,
  },
  brandSubtitle: {
    fontSize: FontSize.small,
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
    fontSize: FontSize.heading,
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
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  forgotText: {
    fontSize: FontSize.small,
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
    fontSize: FontSize.bodyLarge,
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
    fontSize: FontSize.body,
    fontWeight: '800',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  registerLink: {
    alignItems: 'center',
    paddingVertical: 8,
  },
  registerLinkText: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    letterSpacing: 0.5,
  },
  registerLinkHighlight: {
    color: Colors.primary,
    fontWeight: '800',
  },
  formError: {
    fontSize: FontSize.body,
    color: Colors.danger,
    fontFamily: Font.mono,
    marginBottom: Spacing.md,
  },
});
