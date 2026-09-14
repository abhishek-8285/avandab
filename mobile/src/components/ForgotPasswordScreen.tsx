import React, { useState } from 'react';
import { StyleSheet, Text, View, TextInput, TouchableOpacity, ActivityIndicator, Linking } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';

interface ForgotPasswordScreenProps {
  onBackToLogin: () => void;
}

export function ForgotPasswordScreen({ onBackToLogin }: ForgotPasswordScreenProps) {
  const [email, setEmail] = useState('');
  const [loading, setLoading] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [devResetLink, setDevResetLink] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [linkError, setLinkError] = useState<string | null>(null);

  const handleResetPassword = async () => {
    if (!email) {
      setFormError('Please enter your registered email address.');
      return;
    }

    setFormError(null);
    setLoading(true);

    try {
      const response = await fetch(`${getApiBaseURL()}/api/v1/auth/forgot-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email }),
      });

      if (!response.ok) {
        let msg = `Request failed (HTTP ${response.status}). Check the email and try again.`;
        try {
          const errBody = await response.json();
          if (errBody?.error) msg = errBody.error;
        } catch {}
        setLoading(false);
        setFormError(msg);
        return;
      }

      const data = await response.json();
      // Development builds without a mailer get the link inline so the flow
      // is completable end-to-end. Production responses never include it.
      setDevResetLink(typeof data.reset_link === 'string' ? data.reset_link : null);
      setLoading(false);
      setSubmitted(true);
    } catch (err: any) {
      setLoading(false);
      setFormError(err?.message || 'Could not reach the server. Check your connection and try again.');
    }
  };

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      <View style={styles.header}>
        <TouchableOpacity style={styles.iconButton} onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Back to sign in">
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} accessible={false} />
        </TouchableOpacity>
        <Text style={styles.headerLabel} accessibilityRole="header">RESET PASSWORD</Text>
        <View style={{ width: 44 }} />
      </View>

      <View style={styles.card}>
        <View style={styles.iconBox}>
          <MaterialCommunityIcons name="lock-reset" size={24} color={Colors.primary} accessible={false} />
        </View>

        <Text style={styles.title}>PASSWORD RESET</Text>
        <View style={styles.titleUnderline} />
        <Text style={styles.subtitle}>
          Enter registered email. Reset instructions will be dispatched to your inbox.
        </Text>

        {submitted ? (
          <View style={styles.successBox}>
            <View style={styles.successIconBox}>
              <MaterialCommunityIcons name="check" size={24} color={Colors.success} accessible={false} />
            </View>
            <Text style={styles.successTitle}>REQUEST SUBMITTED</Text>
            <Text style={styles.successMessage}>
              If an account exists for <Text style={{ fontWeight: '700', color: Colors.textPrimary }}>{email}</Text>, reset instructions have been dispatched.
            </Text>
            {devResetLink && (
              <TouchableOpacity
                style={[styles.submitBtn, { backgroundColor: Colors.info, marginBottom: Spacing.md }]}
                onPress={() => Linking.openURL(devResetLink).catch(() =>
                  setLinkError('Could not open the reset link. Copy it manually.')
                )}
                accessibilityRole="button"
                accessibilityLabel="Open reset link (development)"
              >
                <Text style={styles.submitBtnText}>OPEN RESET LINK (DEV)</Text>
              </TouchableOpacity>
            )}
            {linkError ? (
              <Text style={styles.formError} accessibilityLiveRegion="polite">{linkError}</Text>
            ) : null}
            <TouchableOpacity style={styles.submitBtn} onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Return to sign in">
              <Text style={styles.submitBtnText}>RETURN TO SIGN IN</Text>
            </TouchableOpacity>
          </View>
        ) : (
          <View style={styles.form}>
            <View style={styles.formGroup}>
              <Text style={styles.label}>EMAIL</Text>
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
                  accessibilityLabel="Registered email"
                />
              </View>
            </View>

            {formError ? (
              <Text style={styles.formError} accessibilityLiveRegion="polite">{formError}</Text>
            ) : null}

            <TouchableOpacity
              style={styles.submitBtn}
              activeOpacity={0.88}
              onPress={handleResetPassword}
              disabled={loading}
              accessibilityRole="button"
              accessibilityLabel={loading ? 'Sending reset link…' : 'Send reset link'}
              accessibilityState={{ disabled: loading, busy: loading }}
              accessibilityLiveRegion="polite"
            >
              {loading ? (
                <ActivityIndicator color={Colors.textOnPrimary} />
              ) : (
                <Text style={styles.submitBtnText}>SEND RESET LINK</Text>
              )}
            </TouchableOpacity>

            <TouchableOpacity style={styles.backLink} onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Back to sign in">
              <Text style={styles.backLinkText}>← BACK TO SIGN IN</Text>
            </TouchableOpacity>
          </View>
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
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: Spacing.lg,
    paddingTop: 50,
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
    width: 44,
    height: 44,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    alignItems: 'center',
    justifyContent: 'center',
  },
  card: {
    backgroundColor: Colors.surface,
    margin: Spacing.lg,
    borderRadius: Radius.lg,
    padding: Spacing.xl,
    borderWidth: 1,
    borderColor: Colors.border,
    alignItems: 'center',
  },
  iconBox: {
    width: 48,
    height: 48,
    borderRadius: Radius.md,
    backgroundColor: Colors.primaryLight,
    borderWidth: 1,
    borderColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: Spacing.md,
  },
  title: {
    fontSize: 16,
    fontWeight: '900',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  titleUnderline: {
    width: 28,
    height: 2,
    backgroundColor: Colors.primary,
    marginTop: 6,
    marginBottom: Spacing.md,
  },
  subtitle: {
    fontSize: 12,
    color: Colors.textSecondary,
    textAlign: 'center',
    lineHeight: 18,
    marginBottom: Spacing.xl,
  },
  form: {
    width: '100%',
  },
  formGroup: {
    marginBottom: Spacing.lg,
  },
  label: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.textSecondary,
    letterSpacing: 1,
    marginBottom: 6,
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
  submitBtn: {
    height: 46,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 4,
    marginBottom: Spacing.md,
    width: '100%',
  },
  submitBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  backLink: {
    alignItems: 'center',
    paddingVertical: 8,
  },
  backLinkText: {
    fontSize: 10,
    color: Colors.primary,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  formError: {
    fontSize: 12,
    color: Colors.danger,
    fontFamily: Font.mono,
    marginBottom: Spacing.md,
    textAlign: 'center',
    width: '100%',
  },
  successBox: {
    alignItems: 'center',
    width: '100%',
    paddingVertical: 8,
  },
  successIconBox: {
    width: 48,
    height: 48,
    borderRadius: Radius.md,
    backgroundColor: Colors.successBg,
    borderWidth: 1,
    borderColor: '#bbf7d0',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: Spacing.md,
  },
  successTitle: {
    fontSize: 14,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
    marginBottom: 8,
  },
  successMessage: {
    fontSize: 12,
    color: Colors.textSecondary,
    textAlign: 'center',
    lineHeight: 18,
    marginBottom: Spacing.lg,
  },
});
