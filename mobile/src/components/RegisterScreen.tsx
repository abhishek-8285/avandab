import React, { useState } from 'react';
import { StyleSheet, Text, View, TextInput, TouchableOpacity, ScrollView, ActivityIndicator } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

interface RegisterScreenProps {
  onRegisterSuccess: () => void;
  onBackToLogin: () => void;
}

export function RegisterScreen({ onRegisterSuccess, onBackToLogin }: RegisterScreenProps) {
  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [password, setPassword] = useState('');
  const [vehicleNumber, setVehicleNumber] = useState('');
  const [loading, setLoading] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const setAuth = useAuthStore((state) => state.setAuth);

  const handleRegister = async () => {
    if (!fullName || !email || !password) {
      setFormError('Please fill in your name, email and password.');
      return;
    }

    setFormError(null);
    setLoading(true);

    try {
      const targetUrl = `${getApiBaseURL()}/api/v1/auth/register`;
      const payload: Record<string, string> = {
        name: fullName,
        email,
        phone,
        password,
      };
      if (vehicleNumber.trim()) {
        payload.vehicle_number = vehicleNumber.trim();
      }
      const response = await fetch(targetUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        const errText = await response.text();
        setLoading(false);
        setFormError(errText || `Registration failed (HTTP ${response.status}). Check your details and try again.`);
        return;
      }

      const data = await response.json();

      if (!data.token) {
        setLoading(false);
        setFormError('Registration failed: no credentials returned. Please try again.');
        return;
      }

      // Backend returns a NESTED user object on register (unlike the flat
      // token endpoint). Accept both shapes defensively.
      const serverUser = data.user || {};
      const userId = serverUser.id || data.user_id || '';
      if (!userId) {
        setLoading(false);
        setFormError('Registration failed: account was not created. Please try again.');
        return;
      }

      await setAuth(data.token, {
        id: userId,
        name: serverUser.name || fullName,
        role: serverUser.role || 'viewer',
        email: serverUser.email || email,
      });
      setLoading(false);
      onRegisterSuccess();
    } catch (err: any) {
      setLoading(false);
      setFormError(err?.message || 'Unable to reach the server. Check your connection and try again.');
    }
  };

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      <View style={styles.header}>
        <TouchableOpacity style={styles.iconButton} onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Back to sign in">
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} accessible={false} />
        </TouchableOpacity>
        <Text style={styles.headerLabel} accessibilityRole="header">DRIVER REGISTRATION</Text>
        <TouchableOpacity onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Cancel registration" hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
          <Text style={styles.cancelText}>CANCEL</Text>
        </TouchableOpacity>
      </View>

      <ScrollView contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
        <View style={styles.titleSection}>
          <Text style={styles.title}>DRIVER PROFILE</Text>
          <View style={styles.titleUnderline} />
          <Text style={styles.subtitle}>Submit details to begin accepting dispatches.</Text>
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>FULL NAME</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="account-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="e.g. Rajesh Kumar" accessibilityLabel="Full name" autoComplete="name" textContentType="name"
              placeholderTextColor={Colors.textMuted}
              value={fullName}
              onChangeText={setFullName}
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>EMAIL</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="email-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="driver@avandab.com" accessibilityLabel="Email" autoComplete="email" textContentType="emailAddress"
              placeholderTextColor={Colors.textMuted}
              value={email}
              onChangeText={setEmail}
              keyboardType="email-address"
              autoCapitalize="none"
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>PHONE</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="phone-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="+91 98765 43210" accessibilityLabel="Phone" autoComplete="tel" textContentType="telephoneNumber"
              placeholderTextColor={Colors.textMuted}
              value={phone}
              onChangeText={setPhone}
              keyboardType="phone-pad"
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>VEHICLE REGISTRATION</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="truck-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="MH-12-AB-9942" accessibilityLabel="Vehicle registration number" autoComplete="off"
              placeholderTextColor={Colors.textMuted}
              value={vehicleNumber}
              onChangeText={setVehicleNumber}
              autoCapitalize="characters"
            />
          </View>
        </View>

        <View style={styles.formGroup}>
          <Text style={styles.label}>PASSWORD</Text>
          <View style={styles.inputWrapper}>
            <MaterialCommunityIcons name="lock-outline" size={16} color={Colors.textMuted} style={styles.inputIcon} accessible={false} />
            <TextInput
              style={styles.input}
              placeholder="Create a password" accessibilityLabel="Password" autoComplete="new-password" textContentType="newPassword"
              placeholderTextColor={Colors.textMuted}
              value={password}
              onChangeText={setPassword}
              secureTextEntry
            />
          </View>
        </View>

        {formError ? (
          <Text style={styles.formError} accessibilityLiveRegion="polite">{formError}</Text>
        ) : null}

        <TouchableOpacity
          style={styles.submitBtn}
          activeOpacity={0.88}
          onPress={handleRegister}
          disabled={loading}
          accessibilityRole="button"
          accessibilityLabel={loading ? 'Submitting registration…' : 'Submit registration'}
          accessibilityState={{ disabled: loading, busy: loading }}
          accessibilityLiveRegion="polite"
        >
          {loading ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <View style={styles.btnContent}>
              <Text style={styles.submitBtnText}>SUBMIT REGISTRATION</Text>
              <MaterialCommunityIcons name="arrow-right" size={14} color={Colors.textOnPrimary} accessible={false} />
            </View>
          )}
        </TouchableOpacity>

        <TouchableOpacity style={styles.loginLink} onPress={onBackToLogin} accessibilityRole="button" accessibilityLabel="Back to sign in">
          <Text style={styles.loginLinkText}>
            Already registered? <Text style={styles.loginLinkHighlight}>SIGN IN</Text>
          </Text>
        </TouchableOpacity>
      </ScrollView>
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
    fontSize: FontSize.label,
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
  cancelText: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.textOnChrome,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  scrollContent: {
    paddingHorizontal: Spacing.xl,
    paddingTop: Spacing.xl,
    paddingBottom: 40,
  },
  titleSection: {
    marginBottom: Spacing.xl,
  },
  title: {
    fontSize: FontSize.headingLarge,
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
    fontSize: FontSize.body,
    color: Colors.textSecondary,
    lineHeight: 18,
  },
  formGroup: {
    marginBottom: Spacing.md,
  },
  label: {
    fontSize: FontSize.small,
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
    fontSize: FontSize.bodyLarge,
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  submitBtn: {
    height: 46,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 8,
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
  loginLink: {
    alignItems: 'center',
    paddingVertical: 8,
  },
  loginLinkText: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
    letterSpacing: 0.5,
  },
  loginLinkHighlight: {
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
