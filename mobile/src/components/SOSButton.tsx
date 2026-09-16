import React, { useState } from 'react';
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  Modal,
  ActivityIndicator,
  Alert,
} from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { t } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
import { sosService, SOSTriggerResult } from '../services/sosService';
import { NotificationService } from '../services/notificationService';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';

interface SOSButtonProps {
  tripId?: string;
  vehicleId?: string;
  latitude?: number;
  longitude?: number;
  accuracy?: number;
  batteryLevel?: number;
  onSOSSent?: (result: SOSTriggerResult) => void;
  style?: any;
}

export function SOSButton({
  tripId,
  vehicleId,
  latitude = 0,
  longitude = 0,
  accuracy = 10,
  batteryLevel = 100,
  onSOSSent,
  style,
}: SOSButtonProps) {
  const locale = useLanguageStore((s) => s.locale);
  const [modalVisible, setModalVisible] = useState(false);
  const [sending, setSending] = useState(false);
  const [lastResult, setLastResult] = useState<SOSTriggerResult | null>(null);

  const handlePressSOS = () => {
    setModalVisible(true);
  };

  const confirmSOS = async () => {
    setSending(true);
    try {
      const result = await sosService.triggerSOS({
        tripId,
        vehicleId,
        latitude,
        longitude,
        accuracy,
        batteryLevel,
        reason: 'Driver Emergency Panic Trigger',
      });

      setLastResult(result);
      setSending(false);
      setModalVisible(false);

      // Trigger system notification bar alert
      NotificationService.showSOSAlert(tripId).catch(() => {});

      if (onSOSSent) {
        onSOSSent(result);
      }

      if (result.queued) {
        Alert.alert(
          t('sos.queued_title', 'SOS Queued Locally (Offline)', locale),
          t('sos.queued_body', 'Your emergency signal was safely saved to the offline queue and will transmit automatically once connectivity is restored.', locale),
          [{ text: t('sos.ok', 'OK', locale) }]
        );
      } else if (result.success) {
        Alert.alert(
          t('sos.sent_title', 'Emergency SOS Dispatched!', locale),
          t('sos.sent_body', 'Dispatchers and response teams have received your emergency alert and current GPS location.', locale),
          [{ text: t('sos.ok', 'OK', locale) }]
        );
      } else {
        Alert.alert(t('sos.failed_title', 'SOS Not Sent', locale), (result.error || 'Dispatch failed.') + ' ' + t('sos.failed_body', 'Check your connection — the signal stays queued and will retry automatically.', locale), [{ text: t('sos.ok', 'OK', locale) }]);
      }
    } catch (err: any) {
      setSending(false);
      setModalVisible(false);
      Alert.alert(t('sos.failed_title', 'SOS Not Sent', locale), (err.message || 'Unexpected error.') + ' ' + t('sos.failed_body', 'Check your connection — the signal stays queued and will retry automatically.', locale), [{ text: t('sos.ok', 'OK', locale) }]);
    }
  };

  return (
    <>
      <TouchableOpacity
        style={[styles.sosButton, style]}
        onPress={handlePressSOS}
        activeOpacity={0.8}
        testID="driver-sos-button"
        accessibilityLabel={t('sos.a11y_button', 'Emergency SOS button', locale)}
        accessibilityRole="button"
      >
        <MaterialCommunityIcons name="alert-octagon" size={24} color={Colors.textOnPrimary} accessible={false} />
        <Text style={styles.sosButtonText}>SOS</Text>
      </TouchableOpacity>

      <Modal
        visible={modalVisible}
        transparent
        animationType="fade"
        onRequestClose={() => !sending && setModalVisible(false)}
      >
        <View style={styles.modalOverlay}>
          <View style={styles.modalContent}>
            <View style={styles.warningIconContainer}>
              <MaterialCommunityIcons name="shield-alert" size={48} color={Colors.danger} accessible={false} />
            </View>

            <Text style={styles.modalTitle}>{t('sos.modal_title', 'Trigger Emergency SOS?', locale)}</Text>
            <Text style={styles.modalSubtitle}>
              {t('sos.modal_sub', 'This will immediately broadcast your coordinates and panic alert to dispatchers and safety teams.', locale)}
            </Text>

            {sending ? (
              <View style={styles.loadingContainer}>
                <ActivityIndicator size="large" color={Colors.danger} />
                <Text style={styles.loadingText}>{t('sos.sending', 'Dispatching emergency alert…', locale)}</Text>
              </View>
            ) : (
              <View style={styles.modalActions}>
                <TouchableOpacity
                  style={styles.cancelButton}
                  onPress={() => setModalVisible(false)}
                  testID="sos-cancel-button"
                  accessibilityRole="button"
                  accessibilityLabel={t('sos.a11y_cancel', 'Cancel emergency SOS', locale)}
                >
                  <Text style={styles.cancelButtonText}>{t('sos.cancel', 'Cancel', locale)}</Text>
                </TouchableOpacity>

                <TouchableOpacity
                  style={styles.confirmButton}
                  onPress={confirmSOS}
                  testID="sos-confirm-button"
                  accessibilityRole="button"
                  accessibilityLabel={sending ? t('sos.sending', 'Dispatching emergency alert…', locale) : t('sos.a11y_send', 'Send SOS now', locale)}
                  accessibilityState={{ busy: sending }}
                >
                  <Text style={styles.confirmButtonText}>{t('sos.send', 'SEND SOS NOW', locale)}</Text>
                </TouchableOpacity>
              </View>
            )}
          </View>
        </View>
      </Modal>
    </>
  );
}

const styles = StyleSheet.create({
  sosButton: {
    backgroundColor: Colors.danger,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: Radius.md,
    shadowColor: Colors.danger,
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.35,
    shadowRadius: 6,
    elevation: 6,
    gap: 6,
  },
  sosButtonText: {
    color: Colors.textOnPrimary,
    fontWeight: '800',
    fontSize: FontSize.heading,
    letterSpacing: 0.5,
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: Colors.overlayDim,
    justifyContent: 'center',
    alignItems: 'center',
    padding: 24,
  },
  modalContent: {
    backgroundColor: Colors.modalBg,
    borderRadius: Radius.lg,
    padding: 24,
    width: '100%',
    maxWidth: 360,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  warningIconContainer: {
    width: 80,
    height: 80,
    borderRadius: 40,
    backgroundColor: Colors.dangerGlow,
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 16,
  },
  modalTitle: {
    fontSize: FontSize.banner,
    fontWeight: '700',
    color: Colors.modalTitle,
    textAlign: 'center',
    marginBottom: 8,
  },
  modalSubtitle: {
    fontSize: FontSize.title,
    color: Colors.modalSub,
    textAlign: 'center',
    lineHeight: 20,
    marginBottom: 24,
  },
  loadingContainer: {
    alignItems: 'center',
    gap: 12,
    paddingVertical: 12,
  },
  loadingText: {
    color: Colors.danger,
    fontSize: FontSize.title,
    fontWeight: '600',
  },
  modalActions: {
    flexDirection: 'row',
    gap: 12,
    width: '100%',
  },
  cancelButton: {
    flex: 1,
    paddingVertical: 12,
    minHeight: 44,
    justifyContent: 'center',
    borderRadius: Radius.md,
    backgroundColor: Colors.modalBorder,
    alignItems: 'center',
  },
  cancelButtonText: {
    color: Colors.modalText,
    fontWeight: '600',
    fontSize: FontSize.title,
  },
  confirmButton: {
    flex: 1.4,
    paddingVertical: 12,
    minHeight: 44,
    justifyContent: 'center',
    borderRadius: Radius.md,
    backgroundColor: Colors.danger,
    alignItems: 'center',
  },
  confirmButtonText: {
    color: Colors.textOnPrimary,
    fontWeight: '700',
    fontSize: FontSize.title,
  },
});
