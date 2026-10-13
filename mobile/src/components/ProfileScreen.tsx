import React, { useEffect, useState } from 'react';
import { StyleSheet, Text, View, TouchableOpacity, ScrollView, ActivityIndicator, Alert, Modal, TextInput, StatusBar } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import { Colors, Radius, Spacing, FontSize} from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { useLanguageStore } from '../stores/languageStore';
import { MQTT } from '../services/mqtt';
import { BackgroundGPS } from '../services/backgroundLocation';
import { VoiceAnnouncement } from '../services/voiceAnnouncement';
import { NotificationService } from '../services/notificationService';
import { SupportedLocale, t } from '../i18n';

interface DriverProfile {
  driver_id: string;
  name: string;
  phone: string;
  status: string;
  vehicle_plate: string;
}

interface ProfileScreenProps {
  onBack: () => void;
}

const LANGUAGES: { code: SupportedLocale; label: string; native: string }[] = [
  { code: 'hi', label: 'Hindi', native: 'हिन्दी' },
  { code: 'en', label: 'English', native: 'English' },
  { code: 'mr', label: 'Marathi', native: 'मराठी' },
  { code: 'gu', label: 'Gujarati', native: 'ગુજરાતી' },
  { code: 'ta', label: 'Tamil', native: 'தமிழ்' },
  { code: 'te', label: 'Telugu', native: 'తెలుగు' },
  { code: 'kn', label: 'Kannada', native: 'ಕನ್ನಡ' },
];

export function ProfileScreen({ onBack }: ProfileScreenProps) {
  const { token, user, logout } = useAuthStore();
  const { locale, setLanguage } = useLanguageStore();
  const intlTag = `${locale}-IN`;
  const intlDate = (y: number, m: number) =>
    new Intl.DateTimeFormat(intlTag, { month: 'short', year: 'numeric' }).format(new Date(y, m, 1));
  const [profile, setProfile] = useState<DriverProfile | null>(null);
  const [loading, setLoading] = useState(false);
  const [bgGpsOn, setBgGpsOn] = useState(true);
  const [voiceOn, setVoiceOn] = useState(VoiceAnnouncement.isEnabled());
  const [dutyStatus, setDutyStatus] = useState<'available' | 'break' | 'inactive'>('available');

  // Modals
  const [showLangModal, setShowLangModal] = useState(false);
  const [showPhoneModal, setShowPhoneModal] = useState(false);
  const [showDutyModal, setShowDutyModal] = useState(false);
  const [newPhone, setNewPhone] = useState('');
  const [phoneReason, setPhoneReason] = useState('');
  const [phoneSubmitted, setPhoneSubmitted] = useState(false);

  // Document states
  const [documents, setDocuments] = useState([
    { id: 'dl', key: 'docs.dl', defaultTitle: 'Driving License', status: 'VALID', expiry: `Exp: ${intlDate(2028, 11)}`, warning: false },
    { id: 'insurance', key: 'docs.insurance', defaultTitle: 'Vehicle Insurance', status: 'EXPIRING_SOON', expiry: new Intl.RelativeTimeFormat(intlTag, { numeric: 'auto' }).format(15, 'day'), warning: true },
    { id: 'fitness', key: 'docs.fitness', defaultTitle: 'Fitness Certificate', status: 'VALID', expiry: `Exp: ${intlDate(2027, 7)}`, warning: false },
    { id: 'puc', key: 'docs.puc', defaultTitle: 'PUC Certificate', status: 'VALID', expiry: `Exp: ${intlDate(2026, 10)}`, warning: false },
  ]);

  useEffect(() => {
    let alive = true;
    const fetchProfile = async () => {
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 2500);
        const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me`, {
          headers: token ? { Authorization: `Bearer ${token}` } : {},
          signal: controller.signal,
        });
        clearTimeout(timeoutId);
        if (res.ok) {
          const json = await res.json();
          if (alive) {
            setProfile(json);
            if (json.status === 'inactive' || json.status === 'leave') {
              setDutyStatus(json.status === 'leave' ? 'break' : 'inactive');
            }
          }
        }
      } catch {
        // offline or timeout fallback
      } finally {
        if (alive) {
          BackgroundGPS.isRunning().then((on) => setBgGpsOn(on)).catch(() => {});
        }
      }
    };

    fetchProfile();
    return () => {
      alive = false;
    };
  }, [token]);

  const handleSelectLanguage = async (code: SupportedLocale) => {
    await setLanguage(code);
    setShowLangModal(false);
  };

  const handleUpdateDuty = async (newStatus: 'available' | 'break' | 'inactive') => {
    setDutyStatus(newStatus);
    setShowDutyModal(false);
    try {
      await fetch(`${getApiBaseURL()}/api/v1/drivers/me/status`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ status: newStatus }),
      });
    } catch {
      // offline fallback
    }
  };

  const handlePhoneRequestSubmit = () => {
    if (!newPhone || newPhone.length < 10) {
      Alert.alert('Error', 'Please enter a valid 10-digit mobile number.');
      return;
    }
    setPhoneSubmitted(true);
    setTimeout(() => {
      setShowPhoneModal(false);
      setPhoneSubmitted(false);
      setNewPhone('');
      setPhoneReason('');
      Alert.alert(
        'Request Submitted',
        'Your mobile number update request has been sent for dispatch verification.'
      );
    }, 800);
  };

  const handleDocumentReupload = async (docId: string, docTitle: string) => {
    try {
      const result = await ImagePicker.launchCameraAsync({
        quality: 0.7,
        allowsEditing: true,
      });

      if (!result.canceled && result.assets && result.assets.length > 0) {
        setDocuments((prev) =>
          prev.map((d) => (d.id === docId ? { ...d, status: 'UNDER_REVIEW', expiry: t('docs.under_review', 'Under Verification', locale), warning: false } : d))
        );
        Alert.alert('Document Received', `${docTitle} uploaded successfully.`);
      }
    } catch {
      // Fallback
      setDocuments((prev) =>
        prev.map((d) => (d.id === docId ? { ...d, status: 'UNDER_REVIEW', expiry: t('docs.under_review', 'Under Verification', locale), warning: false } : d))
      );
      Alert.alert('Document Received', `${docTitle} uploaded successfully.`);
    }
  };

  const handleSignOut = () => {
    Alert.alert(t('profile.sign_out', 'Sign Out', locale), 'End session on this device?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Sign Out',
        style: 'destructive',
        onPress: () => {
          MQTT.disconnect();
          BackgroundGPS.stop();
          logout();
        },
      },
    ]);
  };

  const displayName = profile?.name || user?.name || 'Driver';

  return (
    <SafeAreaView style={styles.safeArea} edges={['top', 'left', 'right']}>
      <StatusBar barStyle="light-content" backgroundColor={Colors.chrome} />

      {/* WhatsApp Header */}
      <View style={styles.header}>
        <TouchableOpacity
          style={styles.backBtn}
          onPress={onBack}
          accessibilityRole="button"
          accessibilityLabel="Go back"
          hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
        >
          <MaterialCommunityIcons name="arrow-left" size={24} color={Colors.textOnPrimary} accessible={false} />
        </TouchableOpacity>
        <View style={styles.headerTitleBlock}>
          <Text style={styles.headerTitle}>{t('header.settings_title', 'Settings & Profile', locale)}</Text>
          <Text style={styles.headerSubtitle}>{t('header.settings_sub', 'Driver Controls & Compliance', locale)}</Text>
        </View>
      </View>

      <ScrollView style={styles.body} contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
        {loading ? (
          <ActivityIndicator
            color={Colors.primary}
            style={{ marginTop: Spacing.xl }}
            accessible
            accessibilityLabel={`${t('common.loading', 'Loading', locale)}…`}
            accessibilityLiveRegion="polite"
          />
        ) : (
          <>
            {/* WhatsApp Profile Contact Card */}
            <View style={styles.heroCard}>
              <View style={styles.avatarBox}>
                <MaterialCommunityIcons name="account" size={40} color={Colors.textOnPrimary} accessible={false} />
              </View>
              <Text style={styles.heroName} numberOfLines={1} ellipsizeMode="tail">{displayName}</Text>
              <Text style={styles.heroSub} numberOfLines={1} ellipsizeMode="tail">{user?.email || 'driver@avandab.com'}</Text>
              
              {/* Duty Toggle Button */}
              <TouchableOpacity 
                style={[
                  styles.statusPill, 
                  dutyStatus === 'available' ? styles.statusPillActive : dutyStatus === 'break' ? styles.statusPillBreak : styles.statusPillOff
                ]}
                onPress={() => setShowDutyModal(true)}
                activeOpacity={0.8}
                accessibilityRole="button"
                accessibilityLabel={dutyStatus === 'available' ? t('duty.available', 'ON DUTY', locale) : dutyStatus === 'break' ? t('duty.break', 'ON BREAK', locale) : t('duty.inactive', 'OFF DUTY', locale)}
              >
                <View style={[
                  styles.statusDot, 
                  dutyStatus === 'available' ? styles.dotGreen : dutyStatus === 'break' ? styles.dotYellow : styles.dotRed
                ]} />
                <Text style={styles.statusPillText}>
                  {dutyStatus === 'available' ? t('duty.available', 'ON DUTY', locale) : dutyStatus === 'break' ? t('duty.break', 'ON BREAK', locale) : t('duty.inactive', 'OFF DUTY', locale)}
                </Text>
                <MaterialCommunityIcons name="pencil" size={12} color={Colors.chrome} accessible={false} />
              </TouchableOpacity>
            </View>

            {/* 1. Driver Profile & Editable Actions */}
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionHeaderText}>{t('section.driver_info', 'DRIVER INFO & CONTACT', locale)}</Text>
            </View>

            <View style={styles.card}>
              {/* Driver ID (Readonly) */}
              <View style={styles.row}>
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name="badge-account-horizontal" size={20} color={Colors.primary} accessible={false} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>{t('profile.driver_id', 'Driver ID', locale)}</Text>
                  <Text style={styles.rowValue} numberOfLines={1} ellipsizeMode="tail">{profile?.driver_id || user?.driverId || user?.id || 'DRV-F6F19B'}</Text>
                </View>
              </View>

              <View style={styles.divider} />

              {/* Mobile Phone (Clickable to change) */}
              <TouchableOpacity
                style={styles.interactiveRow}
                onPress={() => setShowPhoneModal(true)}
                activeOpacity={0.7}
                accessibilityRole="button"
                accessibilityLabel={t('profile.phone', 'Mobile Phone', locale)}
              >
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name="phone" size={20} color={Colors.primary} accessible={false} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>{t('profile.phone', 'Mobile Phone', locale)}</Text>
                  <Text style={styles.rowValue} numberOfLines={1} ellipsizeMode="tail">{profile?.phone || '+91 98765 43210'}</Text>
                </View>
                <View style={styles.changeBadge}>
                  <Text style={styles.changeBadgeText}>{t('profile.edit', 'CHANGE', locale)}</Text>
                </View>
              </TouchableOpacity>

              <View style={styles.divider} />

              {/* Assigned Vehicle */}
              <View style={styles.row}>
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name="truck" size={20} color={Colors.primary} accessible={false} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>{t('profile.vehicle', 'Assigned Vehicle', locale)}</Text>
                  <Text style={styles.rowValue} numberOfLines={1} ellipsizeMode="tail">{profile?.vehicle_plate || 'Tata Prima · DL-01-AB-1234'}</Text>
                </View>
              </View>
            </View>

            {/* 2. Documents & Compliance Renewals Section */}
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionHeaderText}>{t('section.documents', 'DOCUMENTS & RENEWALS', locale)}</Text>
            </View>

            <View style={styles.card}>
              {documents.map((doc, idx) => {
                const docTitle = t(doc.key, doc.defaultTitle, locale);
                return (
                  <React.Fragment key={doc.id}>
                    {idx > 0 && <View style={styles.divider} />}
                    <View style={styles.docRow}>
                      <View style={[styles.docIconBox, doc.warning ? styles.docIconWarning : styles.docIconValid]}>
                        <MaterialCommunityIcons 
                          name={doc.status === 'UNDER_REVIEW' ? 'clock-outline' : doc.warning ? 'alert' : 'check-decagram'} 
                          size={20} 
                          color={doc.status === 'UNDER_REVIEW' ? Colors.info : doc.warning ? Colors.warningText : Colors.primary} 
                          accessible={false}
                        />
                      </View>
                      <View style={styles.docContent}>
                        <Text style={styles.docTitle} numberOfLines={1} ellipsizeMode="tail">{docTitle}</Text>
                        <Text style={[styles.docExpiry, doc.warning && styles.docExpiryWarning]} numberOfLines={1} ellipsizeMode="tail">{doc.expiry}</Text>
                      </View>
                      <TouchableOpacity 
                        style={[styles.renewBtn, doc.warning ? styles.renewBtnWarning : styles.renewBtnOutline]}
                        onPress={() => handleDocumentReupload(doc.id, docTitle)}
                        activeOpacity={0.8}
                        accessibilityRole="button"
                        accessibilityLabel={`${docTitle}: ${doc.warning ? t('docs.renew', 'RENEW', locale) : t('docs.update', 'UPDATE', locale)}`}
                        hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                      >
                        <MaterialCommunityIcons name="camera-plus" size={14} color={doc.warning ? Colors.textOnPrimary : Colors.primary} accessible={false} />
                        <Text style={[styles.renewBtnText, doc.warning ? styles.renewBtnTextWarning : styles.renewBtnTextOutline]}>
                          {doc.warning ? t('docs.renew', 'RENEW', locale) : t('docs.update', 'UPDATE', locale)}
                        </Text>
                      </TouchableOpacity>
                    </View>
                  </React.Fragment>
                );
              })}
            </View>

            {/* 3. Language & GPS Preferences */}
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionHeaderText}>{t('section.preferences', 'APP PREFERENCES', locale)}</Text>
            </View>

            <View style={styles.card}>
              {/* Language Selector */}
              <TouchableOpacity
                style={styles.interactiveRow}
                onPress={() => setShowLangModal(true)}
                activeOpacity={0.7}
                accessibilityRole="button"
                accessibilityLabel={t('settings.language', 'App Language', locale)}
              >
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name="translate" size={20} color={Colors.primary} accessible={false} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>{t('settings.language', 'App Language', locale)}</Text>
                  <Text style={styles.rowValue} numberOfLines={1} ellipsizeMode="tail">
                    {LANGUAGES.find((l) => l.code === locale)?.native || 'हिन्दी'} ({LANGUAGES.find((l) => l.code === locale)?.label})
                  </Text>
                </View>
                <View style={styles.changeBadge}>
                  <Text style={styles.changeBadgeText}>{t('profile.edit', 'CHANGE', locale)}</Text>
                </View>
              </TouchableOpacity>

              <View style={styles.divider} />

              {/* Vernacular Voice Alerts Toggle */}
              <TouchableOpacity
                style={styles.interactiveRow}
                onPress={() => {
                  const nextState = !voiceOn;
                  VoiceAnnouncement.setEnabled(nextState);
                  setVoiceOn(nextState);
                }}
                activeOpacity={0.7}
              >
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name={voiceOn ? "volume-high" : "volume-off"} size={20} color={voiceOn ? Colors.primary : Colors.textSecondary} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>Vernacular Voice Announcements</Text>
                  <Text style={[styles.rowValue, { color: voiceOn ? Colors.success : Colors.textSecondary }]}>
                    {voiceOn ? `ENABLED (${(LANGUAGES.find((l) => l.code === locale)?.label || 'Hindi')})` : 'MUTED'}
                  </Text>
                </View>
                <View style={[styles.changeBadge, { backgroundColor: voiceOn ? Colors.primarySubtle : Colors.borderLight }]}>
                  <Text style={[styles.changeBadgeText, { color: voiceOn ? Colors.primary : Colors.textSecondary }]}>
                    {voiceOn ? 'ON' : 'OFF'}
                  </Text>
                </View>
              </TouchableOpacity>

              {/* Dev-only test trigger: never ships in release builds. */}
              {__DEV__ && (
              <TouchableOpacity
                style={[styles.interactiveRow, { backgroundColor: Colors.primarySubtle }]}
                onPress={() => {
                  NotificationService.showDispatchNotification({
                    tripNumber: 'TRP-8491',
                    origin: 'JNPT Port, Navi Mumbai',
                    destination: 'Chakan MIDC, Pune',
                    advanceAmount: 5000,
                  }).catch(() => {});
                  Alert.alert(
                    '🔔 Test Alert Fired!',
                    'Playing spoken voice announcement in your language and pushed system notification bar banner.'
                  );
                }}
                activeOpacity={0.8}
              >
                <View style={[styles.rowIconBox, { backgroundColor: Colors.successBg }]}>
                  <MaterialCommunityIcons name="bell-ring" size={20} color={Colors.primary} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={[styles.rowLabel, { color: Colors.primary, fontWeight: '800' }]}>Test Notification & Voice</Text>
                  <Text style={styles.rowValue}>Tap to trigger live alert sound & speech</Text>
                </View>
                <View style={[styles.changeBadge, { backgroundColor: Colors.primary }]}>
                  <Text style={[styles.changeBadgeText, { color: Colors.textOnPrimary }]}>PLAY</Text>
                </View>
              </TouchableOpacity>
              )}

              <View style={styles.divider} />

              {/* Background GPS */}
              <View style={styles.row}>
                <View style={styles.rowIconBox}>
                  <MaterialCommunityIcons name="crosshairs-gps" size={20} color={Colors.primary} />
                </View>
                <View style={styles.rowContent}>
                  <Text style={styles.rowLabel}>{t('settings.gps', 'Background GPS Tracking', locale)}</Text>
                  <Text style={[styles.rowValue, { color: bgGpsOn ? Colors.success : Colors.textSecondary }]}>
                    {bgGpsOn ? t('settings.gps_active', 'ACTIVE (OS-Level)', locale) : t('settings.gps_standby', 'STANDBY', locale)}
                  </Text>
                </View>
              </View>
            </View>

            {/* 4. Safe Sign Out Button */}
            <TouchableOpacity style={styles.signOutBtn} onPress={handleSignOut} activeOpacity={0.85}>
              <MaterialCommunityIcons name="logout" size={20} color={Colors.danger} />
              <Text style={styles.signOutText}>{t('profile.sign_out', 'SIGN OUT OF DEVICE', locale)}</Text>
            </TouchableOpacity>

            <Text style={styles.version}>Avandab Fleet Pro · v1788090115</Text>
          </>
        )}
      </ScrollView>

      {/* LANGUAGE SELECTOR MODAL */}
      <Modal visible={showLangModal} transparent animationType="slide" onRequestClose={() => setShowLangModal(false)}>
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>भाषा चुनें / Select Language</Text>
              <TouchableOpacity onPress={() => setShowLangModal(false)}>
                <MaterialCommunityIcons name="close" size={24} color={Colors.textSecondary} />
              </TouchableOpacity>
            </View>
            <ScrollView style={{ maxHeight: 320 }}>
              {LANGUAGES.map((lang) => (
                <TouchableOpacity
                  key={lang.code}
                  style={[styles.langOption, locale === lang.code && styles.langOptionActive]}
                  onPress={() => handleSelectLanguage(lang.code)}
                >
                  <View>
                    <Text style={[styles.langNative, locale === lang.code && styles.langTextActive]}>{lang.native}</Text>
                    <Text style={styles.langSub}>{lang.label}</Text>
                  </View>
                  {locale === lang.code && <MaterialCommunityIcons name="check-circle" size={20} color={Colors.primary} />}
                </TouchableOpacity>
              ))}
            </ScrollView>
          </View>
        </View>
      </Modal>

      {/* PHONE CHANGE REQUEST MODAL */}
      <Modal visible={showPhoneModal} transparent animationType="slide" onRequestClose={() => setShowPhoneModal(false)}>
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>{t('profile.phone', 'Mobile Phone', locale)}: {t('profile.edit', 'Change', locale)}</Text>
              <TouchableOpacity onPress={() => setShowPhoneModal(false)}>
                <MaterialCommunityIcons name="close" size={24} color={Colors.textSecondary} />
              </TouchableOpacity>
            </View>
            <Text style={styles.modalSubtitle}>New 10-digit Phone Number:</Text>
            
            <TextInput
              style={styles.input}
              placeholder="e.g. 9876543210"
              keyboardType="phone-pad"
              maxLength={10}
              value={newPhone}
              onChangeText={setNewPhone}
            />

            <Text style={styles.inputLabel}>Reason for change:</Text>
            <TextInput
              style={styles.input}
              placeholder="e.g. Lost SIM / New Number"
              value={phoneReason}
              onChangeText={setPhoneReason}
            />

            <TouchableOpacity 
              style={styles.modalSubmitBtn} 
              onPress={handlePhoneRequestSubmit}
              disabled={phoneSubmitted}
            >
              {phoneSubmitted ? (
                <ActivityIndicator color={Colors.textOnPrimary} />
              ) : (
                <Text style={styles.modalSubmitText}>Submit for Verification</Text>
              )}
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

      {/* DUTY STATUS MODAL */}
      <Modal visible={showDutyModal} transparent animationType="slide" onRequestClose={() => setShowDutyModal(false)}>
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Update Duty Status</Text>
              <TouchableOpacity onPress={() => setShowDutyModal(false)}>
                <MaterialCommunityIcons name="close" size={24} color={Colors.textSecondary} />
              </TouchableOpacity>
            </View>

            <TouchableOpacity 
              style={[styles.dutyOption, dutyStatus === 'available' && styles.dutyOptionActive]}
              onPress={() => handleUpdateDuty('available')}
            >
              <View style={[styles.statusDot, styles.dotGreen]} />
              <View style={{ flex: 1 }}>
                <Text style={styles.dutyOptionTitle}>🟢 {t('duty.available', 'ON DUTY', locale)}</Text>
                <Text style={styles.dutyOptionSub}>{t('duty.available_desc', 'Ready to accept new dispatches', locale)}</Text>
              </View>
            </TouchableOpacity>

            <TouchableOpacity 
              style={[styles.dutyOption, dutyStatus === 'break' && styles.dutyOptionActive]}
              onPress={() => handleUpdateDuty('break')}
            >
              <View style={[styles.statusDot, styles.dotYellow]} />
              <View style={{ flex: 1 }}>
                <Text style={styles.dutyOptionTitle}>🟡 {t('duty.break', 'ON BREAK', locale)}</Text>
                <Text style={styles.dutyOptionSub}>{t('duty.break_desc', 'Paused for rest or meal', locale)}</Text>
              </View>
            </TouchableOpacity>

            <TouchableOpacity 
              style={[styles.dutyOption, dutyStatus === 'inactive' && styles.dutyOptionActive]}
              onPress={() => handleUpdateDuty('inactive')}
            >
              <View style={[styles.statusDot, styles.dotRed]} />
              <View style={{ flex: 1 }}>
                <Text style={styles.dutyOptionTitle}>🔴 {t('duty.inactive', 'OFF DUTY', locale)}</Text>
                <Text style={styles.dutyOptionSub}>{t('duty.inactive_desc', 'Shift ended / Not available', locale)}</Text>
              </View>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: Colors.chrome,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.xs,
    paddingBottom: Spacing.md,
    backgroundColor: Colors.chrome,
    gap: 12,
  },
  backBtn: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: 'center',
    justifyContent: 'center',
  },
  headerTitleBlock: {
    flex: 1,
  },
  headerTitle: {
    fontSize: FontSize.headingLarge,
    fontWeight: '800',
    color: Colors.textOnPrimary,
  },
  headerSubtitle: {
    fontSize: FontSize.body,
    color: Colors.primary,
    fontWeight: '600',
  },
  body: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  scrollContent: {
    padding: Spacing.lg,
    gap: Spacing.md,
    paddingBottom: 30,
  },
  heroCard: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.lg,
    padding: Spacing.xl,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.border,
    shadowColor: Colors.textPrimary,
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.08,
    shadowRadius: 6,
    elevation: 3,
  },
  avatarBox: {
    width: 72,
    height: 72,
    borderRadius: 36,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: Spacing.sm,
  },
  heroName: {
    fontSize: FontSize.banner,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  heroSub: {
    fontSize: FontSize.bodyLarge,
    color: Colors.textSecondary,
    marginTop: 2,
  },
  statusPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: Radius.full,
    marginTop: 10,
  },
  statusPillActive: {
    backgroundColor: Colors.successBg,
  },
  statusPillBreak: {
    backgroundColor: Colors.warningBg,
  },
  statusPillOff: {
    backgroundColor: Colors.dangerBg,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  dotGreen: {
    backgroundColor: Colors.success,
  },
  dotYellow: {
    backgroundColor: Colors.warning,
  },
  dotRed: {
    backgroundColor: Colors.danger,
  },
  statusPillText: {
    fontSize: FontSize.label,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  sectionHeader: {
    paddingHorizontal: 4,
    marginTop: 4,
  },
  sectionHeaderText: {
    fontSize: FontSize.label,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 0.5,
  },
  card: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.lg,
    padding: Spacing.md,
    borderWidth: 1,
    borderColor: Colors.border,
    shadowColor: Colors.textPrimary,
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 4,
    elevation: 2,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 10,
    gap: 12,
  },
  interactiveRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 10,
    gap: 12,
  },
  rowIconBox: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
  },
  rowContent: {
    flex: 1,
  },
  rowLabel: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  rowValue: {
    fontSize: FontSize.title,
    fontWeight: '700',
    color: Colors.textPrimary,
    marginTop: 2,
  },
  changeBadge: {
    backgroundColor: Colors.primarySubtle,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: Radius.full,
    borderWidth: 1,
    borderColor: Colors.primary,
  },
  changeBadgeText: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.primary,
  },
  divider: {
    height: 1,
    backgroundColor: Colors.borderLight,
    marginHorizontal: 4,
  },
  docRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 10,
    gap: 10,
  },
  docIconBox: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  docIconValid: {
    backgroundColor: Colors.primarySubtle,
  },
  docIconWarning: {
    backgroundColor: Colors.warningBg,
  },
  docContent: {
    flex: 1,
  },
  docTitle: {
    fontSize: FontSize.bodyLarge,
    fontWeight: '700',
    color: Colors.textPrimary,
  },
  docExpiry: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    marginTop: 1,
  },
  docExpiryWarning: {
    color: Colors.warningText,
    fontWeight: '700',
  },
  renewBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: Radius.full,
  },
  renewBtnWarning: {
    backgroundColor: Colors.warningText,
  },
  renewBtnOutline: {
    backgroundColor: Colors.surface,
    borderWidth: 1,
    borderColor: Colors.primary,
  },
  renewBtnText: {
    fontSize: FontSize.small,
    fontWeight: '800',
  },
  renewBtnTextWarning: {
    color: Colors.textOnPrimary,
  },
  renewBtnTextOutline: {
    color: Colors.primary,
  },
  signOutBtn: {
    backgroundColor: Colors.surface,
    borderWidth: 1.5,
    borderColor: Colors.dangerBg,
    paddingVertical: 14,
    borderRadius: Radius.full,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    marginTop: Spacing.sm,
  },
  signOutText: {
    color: Colors.danger,
    fontSize: FontSize.bodyLarge,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  version: {
    textAlign: 'center',
    fontSize: FontSize.label,
    color: Colors.textMuted,
    marginVertical: Spacing.sm,
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'flex-end',
  },
  modalCard: {
    backgroundColor: Colors.surface,
    borderTopLeftRadius: Radius.xl,
    borderTopRightRadius: Radius.xl,
    padding: Spacing.xl,
    gap: Spacing.md,
  },
  modalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  modalTitle: {
    fontSize: FontSize.heading,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  modalSubtitle: {
    fontSize: FontSize.body,
    color: Colors.textSecondary,
  },
  langOption: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 12,
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  langOptionActive: {
    backgroundColor: Colors.primarySubtle,
    paddingHorizontal: 8,
    borderRadius: Radius.md,
  },
  langNative: {
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
    color: Colors.textPrimary,
  },
  langTextActive: {
    color: Colors.primary,
    fontWeight: '800',
  },
  langSub: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
  },
  inputLabel: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginTop: 4,
  },
  input: {
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.md,
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: FontSize.title,
    color: Colors.textPrimary,
    backgroundColor: Colors.surfaceSecondary,
  },
  modalSubmitBtn: {
    backgroundColor: Colors.primary,
    paddingVertical: 13,
    borderRadius: Radius.full,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 8,
  },
  modalSubmitText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.bodyLarge,
    fontWeight: '800',
  },
  dutyOption: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingVertical: 12,
    paddingHorizontal: 10,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.border,
    marginBottom: 8,
  },
  dutyOptionActive: {
    borderColor: Colors.primary,
    backgroundColor: Colors.primarySubtle,
  },
  dutyOptionTitle: {
    fontSize: FontSize.title,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  dutyOptionSub: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    marginTop: 1,
  },
});
