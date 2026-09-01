import React, { useState } from 'react';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  ScrollView,
  Linking,
  Alert,
  Modal,
  TextInput,
  KeyboardAvoidingView,
  Platform,
} from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { SOSButton } from './SOSButton';
import { LiveDriverTrackingMap } from './LiveDriverTrackingMap';
import { Telemetry } from '../services/telemetry';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { Trip } from '../types/api';
import { useLanguageStore } from '../stores/languageStore';
import { t } from '../i18n';

type TripStage = 'TO_PICKUP' | 'IN_TRANSIT' | 'AT_DESTINATION';

interface ActiveNavigationScreenProps {
  tripId?: string;
  trip?: Trip;
  onArriveAtStop: () => void;
  onMenuToggle?: () => void;
}

export function ActiveNavigationScreen({
  tripId,
  trip,
  onArriveAtStop,
  onMenuToggle,
}: ActiveNavigationScreenProps) {
  const insets = useSafeAreaInsets();
  const { locale } = useLanguageStore();

  const [stage, setStage] = useState<TripStage>('TO_PICKUP');
  const [maxUnlockedStage, setMaxUnlockedStage] = useState<number>(1);

  const [expenseModalVisible, setExpenseModalVisible] = useState(false);
  const [expenseAmount, setExpenseAmount] = useState('');
  const [expenseCategory, setExpenseCategory] = useState<'Diesel' | 'Toll' | 'Food' | 'Other'>('Diesel');
  const [expenseSavedMsg, setExpenseSavedMsg] = useState(false);

  const [coords, setCoords] = useState<{ latitude: number; longitude: number; speedKmh: number | null }>({
    latitude: 18.5204,
    longitude: 73.8567,
    speedKmh: 48,
  });

  React.useEffect(() => {
    Telemetry.startLiveLocationTracking((lat, lng, speedKmh) => {
      setCoords({ latitude: lat, longitude: lng, speedKmh: speedKmh ?? 48 });
    });
    return () => {
      Telemetry.stopLiveLocationTracking();
    };
  }, []);

  const origin = trip?.origin || 'JNPT Port, Navi Mumbai';
  const destination = trip?.destination || 'Chakan MIDC, Pune';
  const tripNumber = trip?.tripNumber || tripId || 'TRP-8491';
  const vehiclePlate = trip?.vehiclePlate || 'DL-01-AB-1234';

  const launchNavigation = (targetLocation: string) => {
    const query = encodeURIComponent(targetLocation);
    const navUrl = `geo:0,0?q=${query}`;
    Linking.openURL(navUrl).catch(() => {
      Linking.openURL(`https://www.google.com/maps/search/?api=1&query=${query}`);
    });
  };

  const handleRecordExpense = () => {
    if (!expenseAmount.trim()) {
      Alert.alert('Missing Amount', 'Please enter expense amount in ₹');
      return;
    }
    setExpenseModalVisible(false);
    setExpenseAmount('');
    setExpenseSavedMsg(true);
    setTimeout(() => setExpenseSavedMsg(false), 3000);
  };

  const handleStepTabClick = (targetStageNum: number, targetStage: TripStage) => {
    if (targetStageNum > maxUnlockedStage) {
      if (targetStageNum === 2) {
        Alert.alert(
          'Step 2 Locked 🔒',
          'You must confirm loading and departure at JNPT Port (Step 1) before starting Highway Transit.'
        );
      } else if (targetStageNum === 3) {
        Alert.alert(
          'Step 3 Locked 🔒',
          'You must reach the destination factory (Step 2) before initiating e-POD delivery verification.'
        );
      }
      return;
    }
    setStage(targetStage);
  };

  const getStageContent = () => {
    switch (stage) {
      case 'TO_PICKUP':
        return {
          badge: 'STEP 1: EN ROUTE TO LOADING',
          badgeBg: '#fef3c7',
          badgeText: '#b45309',
          targetLabel: 'PICKUP WAREHOUSE',
          targetAddress: origin,
          subInfo: 'Terminal 4 Bay 2 • 18 Tons Steel Coils',
          navBtn: 'NAVIGATE TO PICKUP (MAPS)',
          navTarget: origin,
          footerBtn: 'CONFIRM LOADED & START TRANSIT',
          footerIcon: 'truck-fast',
        };
      case 'IN_TRANSIT':
        return {
          badge: 'STEP 2: HIGHWAY TRANSIT',
          badgeBg: '#e7ffdb',
          badgeText: '#008069',
          targetLabel: 'DELIVERY DESTINATION',
          targetAddress: destination,
          subInfo: 'Mumbai-Pune Expressway • ~128 KM (3h 15m)',
          navBtn: 'NAVIGATE TO FACTORY (MAPS)',
          navTarget: destination,
          footerBtn: 'ARRIVED AT DESTINATION FACTORY',
          footerIcon: 'map-marker-check',
        };
      case 'AT_DESTINATION':
        return {
          badge: 'STEP 3: UNLOADING & e-POD',
          badgeBg: '#e0f2fe',
          badgeText: '#0284c7',
          targetLabel: 'RECEIVING DOCK',
          targetAddress: destination,
          subInfo: 'Gate 3 Receiving Bay • Tata AutoComp Systems',
          navBtn: 'VIEW GATE DIRECTIONS (MAPS)',
          navTarget: destination,
          footerBtn: 'COMPLETE e-POD SIGNATURE',
          footerIcon: 'clipboard-check',
        };
    }
  };

  const currentContent = getStageContent();

  const handlePrimaryAction = () => {
    if (stage === 'TO_PICKUP') {
      Alert.alert(
        'Confirm Loading Complete',
        'Have you loaded the 18 Tons cargo and received the Gate Pass at JNPT Port?',
        [
          { text: 'Cancel', style: 'cancel' },
          {
            text: 'Yes, Start Transit',
            onPress: () => {
              setMaxUnlockedStage((prev) => Math.max(prev, 2));
              setStage('IN_TRANSIT');
            },
          },
        ]
      );
    } else if (stage === 'IN_TRANSIT') {
      Alert.alert(
        'Arrived at Destination',
        'Confirm arrival at Chakan MIDC factory receiving bay?',
        [
          { text: 'Cancel', style: 'cancel' },
          {
            text: 'Confirm Arrival',
            onPress: () => {
              setMaxUnlockedStage((prev) => Math.max(prev, 3));
              setStage('AT_DESTINATION');
            },
          },
        ]
      );
    } else if (stage === 'AT_DESTINATION') {
      onArriveAtStop();
    }
  };

  return (
    <SafeAreaView style={styles.safeArea} edges={['top', 'left', 'right']}>
      <StatusBar style="light" backgroundColor="#075e54" />

      {/* Clean Compact Header */}
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <TouchableOpacity
            style={styles.backBtn}
            onPress={onMenuToggle}
            accessibilityLabel="Go back"
            hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
          >
            <MaterialCommunityIcons name="arrow-left" size={22} color="#ffffff" />
          </TouchableOpacity>
          <View>
            <Text style={styles.headerTitle}>#{tripNumber}</Text>
            <Text style={styles.headerSubtitle}>{vehiclePlate} • 18 Tons</Text>
          </View>
        </View>

        <View style={styles.headerRight}>
          <SOSButton
            tripId={tripId}
            vehicleId={vehiclePlate}
            latitude={coords.latitude}
            longitude={coords.longitude}
            style={{ paddingHorizontal: 10, paddingVertical: 5, height: 32 }}
          />
        </View>
      </View>

      {/* Minimal Stepper Bar */}
      <View style={styles.stepperBar}>
        {/* Step 1 */}
        <TouchableOpacity
          style={[styles.stepItem, stage === 'TO_PICKUP' && styles.stepItemActive]}
          onPress={() => handleStepTabClick(1, 'TO_PICKUP')}
          activeOpacity={0.8}
        >
          <View style={[styles.stepCircle, maxUnlockedStage > 1 ? styles.stepCircleDone : (stage === 'TO_PICKUP' ? styles.stepCircleActive : null)]}>
            {maxUnlockedStage > 1 ? (
              <MaterialCommunityIcons name="check" size={11} color="#ffffff" />
            ) : (
              <Text style={styles.stepNum}>1</Text>
            )}
          </View>
          <Text style={[styles.stepText, stage === 'TO_PICKUP' && styles.stepTextActive]}>Pickup</Text>
        </TouchableOpacity>

        <View style={styles.stepperLine} />

        {/* Step 2 */}
        <TouchableOpacity
          style={[styles.stepItem, stage === 'IN_TRANSIT' && styles.stepItemActive]}
          onPress={() => handleStepTabClick(2, 'IN_TRANSIT')}
          activeOpacity={0.8}
        >
          <View style={[styles.stepCircle, maxUnlockedStage > 2 ? styles.stepCircleDone : (stage === 'IN_TRANSIT' ? styles.stepCircleActive : (maxUnlockedStage < 2 ? styles.stepCircleLocked : null))]}>
            {maxUnlockedStage > 2 ? (
              <MaterialCommunityIcons name="check" size={11} color="#ffffff" />
            ) : maxUnlockedStage < 2 ? (
              <MaterialCommunityIcons name="lock" size={9} color="rgba(255,255,255,0.4)" />
            ) : (
              <Text style={styles.stepNum}>2</Text>
            )}
          </View>
          <Text style={[styles.stepText, stage === 'IN_TRANSIT' && styles.stepTextActive, maxUnlockedStage < 2 && styles.stepTextLocked]}>
            Transit {maxUnlockedStage < 2 && '🔒'}
          </Text>
        </TouchableOpacity>

        <View style={styles.stepperLine} />

        {/* Step 3 */}
        <TouchableOpacity
          style={[styles.stepItem, stage === 'AT_DESTINATION' && styles.stepItemActive]}
          onPress={() => handleStepTabClick(3, 'AT_DESTINATION')}
          activeOpacity={0.8}
        >
          <View style={[styles.stepCircle, stage === 'AT_DESTINATION' ? styles.stepCircleActive : styles.stepCircleLocked]}>
            {maxUnlockedStage < 3 ? (
              <MaterialCommunityIcons name="lock" size={9} color="rgba(255,255,255,0.4)" />
            ) : (
              <Text style={styles.stepNum}>3</Text>
            )}
          </View>
          <Text style={[styles.stepText, stage === 'AT_DESTINATION' && styles.stepTextActive, maxUnlockedStage < 3 && styles.stepTextLocked]}>
            e-POD {maxUnlockedStage < 3 && '🔒'}
          </Text>
        </TouchableOpacity>
      </View>

      {/* Main Body with Clean Spacing */}
      <ScrollView
        style={styles.body}
        contentContainerStyle={[styles.scrollContent, { paddingBottom: 100 }]}
        showsVerticalScrollIndicator={false}
      >
        {expenseSavedMsg && (
          <View style={styles.toast}>
            <MaterialCommunityIcons name="check-circle" size={16} color="#008069" />
            <Text style={styles.toastText}>Expense saved to passbook!</Text>
          </View>
        )}

        {/* Hero Card: Single Unified Focus Card */}
        <View style={styles.heroCard}>
          <View style={styles.heroHeaderRow}>
            <View style={[styles.badgePill, { backgroundColor: currentContent.badgeBg }]}>
              <Text style={[styles.badgeText, { color: currentContent.badgeText }]}>{currentContent.badge}</Text>
            </View>
            <View style={styles.liveSpeedPill}>
              <View style={styles.liveDot} />
              <Text style={styles.liveSpeedText}>{coords.speedKmh ?? 48} KM/H</Text>
            </View>
          </View>

          <Text style={styles.targetLabel}>{currentContent.targetLabel}</Text>
          <Text style={styles.targetAddress}>{currentContent.targetAddress}</Text>
          <Text style={styles.subInfoText}>{currentContent.subInfo}</Text>

          {/* Big High-Contrast Maps CTA */}
          <TouchableOpacity
            style={styles.mapsCTA}
            activeOpacity={0.88}
            onPress={() => launchNavigation(currentContent.navTarget)}
          >
            <MaterialCommunityIcons name="google-maps" size={22} color="#ffffff" />
            <View style={{ flex: 1 }}>
              <Text style={styles.mapsCTATitle}>{currentContent.navBtn}</Text>
              <Text style={styles.mapsCTASub}>Live voice directions</Text>
            </View>
            <MaterialCommunityIcons name="chevron-right" size={22} color="#ffffff" />
          </TouchableOpacity>
        </View>

        {/* Live Interactive Leaflet Map with Route & Controls */}
        <LiveDriverTrackingMap
          driverLatitude={coords.latitude}
          driverLongitude={coords.longitude}
          pickupLabel={origin}
          destinationLabel={destination}
          vehicleLabel={vehiclePlate}
          speedKmh={coords.speedKmh ?? 48}
          height={240}
          onOpenExternalNav={() => launchNavigation(currentContent.navTarget)}
        />

        {/* Clean 3-Item Quick Action Row */}
        <View style={styles.actionRow}>
          <TouchableOpacity
            style={styles.actionPill}
            activeOpacity={0.8}
            onPress={() => setExpenseModalVisible(true)}
          >
            <MaterialCommunityIcons name="gas-station" size={18} color="#008069" />
            <Text style={styles.actionPillText}>+ Kharcha</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionPill}
            activeOpacity={0.8}
            onPress={() => Alert.alert('Call Dispatch', 'Calling Control Room: +91 98200 12345')}
          >
            <MaterialCommunityIcons name="phone" size={18} color="#0284c7" />
            <Text style={styles.actionPillText}>Call Hub</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={styles.actionPill}
            activeOpacity={0.8}
            onPress={() => Alert.alert('GST E-Way Bill', 'E-Way Bill: #7291-8841-0294\nValid till 31 Aug 2026\nVehicle: DL-01-AB-1234')}
          >
            <MaterialCommunityIcons name="shield-check" size={18} color="#008069" />
            <Text style={styles.actionPillText}>E-Way Bill</Text>
          </TouchableOpacity>
        </View>

        {/* Clean Route Overview Summary */}
        <View style={styles.routeCard}>
          <Text style={styles.cardHeader}>CORRIDOR TIMELINE</Text>

          <View style={styles.routeRow}>
            <View style={[styles.dot, { backgroundColor: maxUnlockedStage > 1 ? '#008069' : '#f59e0b' }]} />
            <View style={styles.routeDetails}>
              <Text style={styles.routeCity}>JNPT Port, Navi Mumbai</Text>
              <Text style={styles.routeStatus}>
                {maxUnlockedStage > 1 ? 'Loaded (10:30 AM)' : 'Loading Bay 2 (In Progress)'}
              </Text>
            </View>
          </View>

          <View style={styles.line} />

          <View style={styles.routeRow}>
            <View style={[styles.dot, { backgroundColor: maxUnlockedStage >= 3 ? '#0284c7' : '#94a3b8' }]} />
            <View style={styles.routeDetails}>
              <Text style={styles.routeCity}>Chakan MIDC, Pune</Text>
              <Text style={styles.routeStatus}>
                {maxUnlockedStage >= 3 ? 'Arrived at Gate 3' : (maxUnlockedStage === 2 ? 'In Transit • ETA 1:45 PM' : 'Step 2 (Locked)')}
              </Text>
            </View>
          </View>
        </View>
      </ScrollView>

      {/* Pinned Sticky Bottom Action Footer (Unclipped & Focused) */}
      <View style={[styles.stickyFooter, { paddingBottom: Math.max(insets.bottom, 12) }]}>
        <TouchableOpacity
          style={styles.stickyBtn}
          activeOpacity={0.88}
          onPress={handlePrimaryAction}
        >
          <MaterialCommunityIcons name={currentContent.footerIcon as any} size={20} color="#ffffff" />
          <Text style={styles.stickyBtnText}>{currentContent.footerBtn}</Text>
        </TouchableOpacity>
      </View>

      {/* Quick Kharcha Bottom Sheet Modal */}
      <Modal
        visible={expenseModalVisible}
        transparent
        animationType="slide"
        onRequestClose={() => setExpenseModalVisible(false)}
      >
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
          style={styles.modalOverlay}
        >
          <View style={styles.modalContent}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Add Highway Expense</Text>
              <TouchableOpacity onPress={() => setExpenseModalVisible(false)}>
                <MaterialCommunityIcons name="close" size={22} color="#667781" />
              </TouchableOpacity>
            </View>

            <View style={styles.chipsRow}>
              {(['Diesel', 'Toll', 'Food', 'Other'] as const).map((cat) => (
                <TouchableOpacity
                  key={cat}
                  style={[styles.chip, expenseCategory === cat && styles.chipActive]}
                  onPress={() => setExpenseCategory(cat)}
                >
                  <Text style={[styles.chipText, expenseCategory === cat && styles.chipTextActive]}>
                    {cat === 'Diesel' ? '⛽ Diesel' : cat === 'Toll' ? '🛣️ Toll' : cat === 'Food' ? '🍛 Food' : '📦 Other'}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            <TextInput
              style={styles.amountInput}
              keyboardType="numeric"
              placeholder="₹ Amount"
              placeholderTextColor="#94a3b8"
              value={expenseAmount}
              onChangeText={setExpenseAmount}
            />

            <View style={styles.quickPills}>
              {['500', '1000', '2000', '3500'].map((amt) => (
                <TouchableOpacity
                  key={amt}
                  style={styles.pill}
                  onPress={() => setExpenseAmount(amt)}
                >
                  <Text style={styles.pillText}>+₹{amt}</Text>
                </TouchableOpacity>
              ))}
            </View>

            <TouchableOpacity
              style={styles.modalBtn}
              activeOpacity={0.88}
              onPress={handleRecordExpense}
            >
              <Text style={styles.modalBtnText}>SAVE EXPENSE</Text>
            </TouchableOpacity>
          </View>
        </KeyboardAvoidingView>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: '#075e54',
  },
  header: {
    backgroundColor: '#075e54',
    paddingHorizontal: 16,
    paddingVertical: 10,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  headerLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  backBtn: {
    width: 32,
    height: 32,
    borderRadius: 16,
    backgroundColor: 'rgba(255,255,255,0.14)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  headerTitle: {
    fontSize: 16,
    fontWeight: '800',
    color: '#ffffff',
  },
  headerSubtitle: {
    fontSize: 11,
    color: '#dcf8c6',
    fontWeight: '600',
  },
  headerRight: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  stepperBar: {
    backgroundColor: '#004c3f',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 20,
    paddingVertical: 8,
  },
  stepItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingVertical: 2,
  },
  stepItemActive: {
    borderBottomWidth: 2,
    borderBottomColor: '#25d366',
  },
  stepCircle: {
    width: 16,
    height: 16,
    borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.2)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  stepCircleActive: {
    backgroundColor: '#25d366',
  },
  stepCircleDone: {
    backgroundColor: '#008069',
  },
  stepCircleLocked: {
    backgroundColor: 'rgba(255,255,255,0.1)',
  },
  stepNum: {
    fontSize: 9,
    fontWeight: '800',
    color: '#ffffff',
  },
  stepText: {
    fontSize: 11,
    fontWeight: '700',
    color: 'rgba(255,255,255,0.7)',
  },
  stepTextActive: {
    color: '#ffffff',
  },
  stepTextLocked: {
    color: 'rgba(255,255,255,0.4)',
  },
  stepperLine: {
    width: 14,
    height: 1,
    backgroundColor: 'rgba(255,255,255,0.2)',
  },
  body: {
    flex: 1,
    backgroundColor: '#efeae2',
  },
  scrollContent: {
    padding: 14,
    gap: 12,
  },
  toast: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: '#e7ffdb',
    padding: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#25d366',
  },
  toastText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#008069',
  },
  heroCard: {
    backgroundColor: '#075e54',
    borderRadius: 14,
    padding: 16,
    elevation: 3,
  },
  heroHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  badgePill: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 6,
  },
  badgeText: {
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  liveSpeedPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: 'rgba(0,0,0,0.25)',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 8,
  },
  liveDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#25d366',
  },
  liveSpeedText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '900',
    fontFamily: Font.mono,
  },
  targetLabel: {
    color: '#dcf8c6',
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 0.5,
    marginTop: 10,
  },
  targetAddress: {
    color: '#ffffff',
    fontSize: 17,
    fontWeight: '800',
    marginTop: 2,
  },
  subInfoText: {
    color: '#dcf8c6',
    fontSize: 11,
    marginTop: 2,
    marginBottom: 14,
  },
  mapsCTA: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    backgroundColor: '#008069',
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#25d366',
  },
  mapsCTATitle: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  mapsCTASub: {
    color: '#dcf8c6',
    fontSize: 9,
  },
  actionRow: {
    flexDirection: 'row',
    gap: 8,
  },
  actionPill: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#ffffff',
    paddingVertical: 10,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#e2e8f0',
    elevation: 1,
  },
  actionPillText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#1e293b',
  },
  routeCard: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 14,
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  cardHeader: {
    fontSize: 10,
    fontWeight: '800',
    color: '#64748b',
    letterSpacing: 0.5,
    marginBottom: 10,
  },
  routeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  dot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  routeDetails: {
    flex: 1,
  },
  routeCity: {
    fontSize: 13,
    fontWeight: '800',
    color: '#0f172a',
  },
  routeStatus: {
    fontSize: 10,
    color: '#64748b',
    marginTop: 1,
  },
  line: {
    width: 2,
    height: 14,
    backgroundColor: '#cbd5e1',
    marginLeft: 4,
    marginVertical: 3,
  },
  stickyFooter: {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    backgroundColor: '#ffffff',
    paddingHorizontal: 14,
    paddingTop: 10,
    borderTopWidth: 1,
    borderTopColor: '#e2e8f0',
    elevation: 8,
  },
  stickyBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#008069',
    paddingVertical: 12,
    borderRadius: 10,
  },
  stickyBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'flex-end',
  },
  modalContent: {
    backgroundColor: '#ffffff',
    borderTopLeftRadius: 18,
    borderTopRightRadius: 18,
    padding: 18,
    paddingBottom: 32,
  },
  modalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 14,
  },
  modalTitle: {
    fontSize: 14,
    fontWeight: '800',
    color: '#0f172a',
  },
  chipsRow: {
    flexDirection: 'row',
    gap: 6,
    marginBottom: 12,
  },
  chip: {
    flex: 1,
    paddingVertical: 6,
    backgroundColor: '#f1f5f9',
    borderRadius: 6,
    alignItems: 'center',
  },
  chipActive: {
    backgroundColor: '#e7ffdb',
    borderWidth: 1,
    borderColor: '#25d366',
  },
  chipText: {
    fontSize: 10,
    fontWeight: '700',
    color: '#64748b',
  },
  chipTextActive: {
    color: '#008069',
  },
  amountInput: {
    backgroundColor: '#f8fafc',
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: 16,
    fontWeight: '800',
    color: '#0f172a',
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  quickPills: {
    flexDirection: 'row',
    gap: 6,
    marginTop: 8,
  },
  pill: {
    flex: 1,
    paddingVertical: 5,
    backgroundColor: '#f1f5f9',
    borderRadius: 5,
    alignItems: 'center',
  },
  pillText: {
    fontSize: 10,
    fontWeight: '700',
    color: '#008069',
  },
  modalBtn: {
    backgroundColor: '#008069',
    paddingVertical: 12,
    borderRadius: 8,
    alignItems: 'center',
    marginTop: 14,
  },
  modalBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
  },
});
