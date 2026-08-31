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
import { sosService, SOSTriggerResult } from '../services/sosService';
import { NotificationService } from '../services/notificationService';
import { Colors, Font, Radius, Spacing } from '../constants/theme';

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
          'SOS Queued Locally (Offline)',
          'Your emergency signal was safely saved to the offline queue and will transmit automatically once connectivity is restored.',
          [{ text: 'OK' }]
        );
      } else if (result.success) {
        Alert.alert(
          'Emergency SOS Dispatched!',
          'Dispatchers and response teams have received your emergency alert and current GPS location.',
          [{ text: 'OK' }]
        );
      } else {
        Alert.alert('SOS Error', result.error || 'Failed to dispatch SOS', [{ text: 'OK' }]);
      }
    } catch (err: any) {
      setSending(false);
      setModalVisible(false);
      Alert.alert('SOS Error', err.message || 'Unexpected error sending SOS', [{ text: 'OK' }]);
    }
  };

  return (
    <>
      <TouchableOpacity
        style={[styles.sosButton, style]}
        onPress={handlePressSOS}
        activeOpacity={0.8}
        testID="driver-sos-button"
        accessibilityLabel="Emergency SOS button"
        accessibilityRole="button"
      >
        <MaterialCommunityIcons name="alert-octagon" size={24} color="#FFFFFF" />
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
              <MaterialCommunityIcons name="shield-alert" size={48} color="#EF4444" />
            </View>

            <Text style={styles.modalTitle}>Trigger Emergency SOS?</Text>
            <Text style={styles.modalSubtitle}>
              This will immediately broadcast your coordinates and panic alert to dispatchers and safety teams.
            </Text>

            {sending ? (
              <View style={styles.loadingContainer}>
                <ActivityIndicator size="large" color="#EF4444" />
                <Text style={styles.loadingText}>Dispatching Emergency Alert...</Text>
              </View>
            ) : (
              <View style={styles.modalActions}>
                <TouchableOpacity
                  style={styles.cancelButton}
                  onPress={() => setModalVisible(false)}
                  testID="sos-cancel-button"
                >
                  <Text style={styles.cancelButtonText}>Cancel</Text>
                </TouchableOpacity>

                <TouchableOpacity
                  style={styles.confirmButton}
                  onPress={confirmSOS}
                  testID="sos-confirm-button"
                >
                  <Text style={styles.confirmButtonText}>SEND SOS NOW</Text>
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
    backgroundColor: '#DC2626',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: Radius.md,
    shadowColor: '#DC2626',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.35,
    shadowRadius: 6,
    elevation: 6,
    gap: 6,
  },
  sosButtonText: {
    color: '#FFFFFF',
    fontWeight: '800',
    fontSize: 16,
    letterSpacing: 0.5,
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0, 0, 0, 0.65)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: 24,
  },
  modalContent: {
    backgroundColor: '#1E293B',
    borderRadius: Radius.lg,
    padding: 24,
    width: '100%',
    maxWidth: 360,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#334155',
  },
  warningIconContainer: {
    width: 80,
    height: 80,
    borderRadius: 40,
    backgroundColor: 'rgba(239, 68, 68, 0.15)',
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 16,
  },
  modalTitle: {
    fontSize: 20,
    fontWeight: '700',
    color: '#F8FAFC',
    textAlign: 'center',
    marginBottom: 8,
  },
  modalSubtitle: {
    fontSize: 14,
    color: '#94A3B8',
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
    color: '#EF4444',
    fontSize: 14,
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
    borderRadius: Radius.md,
    backgroundColor: '#334155',
    alignItems: 'center',
  },
  cancelButtonText: {
    color: '#E2E8F0',
    fontWeight: '600',
    fontSize: 14,
  },
  confirmButton: {
    flex: 1.4,
    paddingVertical: 12,
    borderRadius: Radius.md,
    backgroundColor: '#DC2626',
    alignItems: 'center',
  },
  confirmButtonText: {
    color: '#FFFFFF',
    fontWeight: '700',
    fontSize: 14,
  },
});
