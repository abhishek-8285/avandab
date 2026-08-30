import React, { useState, useRef } from 'react';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  ScrollView,
  Image,
  TextInput,
  ActivityIndicator,
  Alert,
} from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { CameraView, useCameraPermissions } from 'expo-camera';
import * as ImageManipulator from 'expo-image-manipulator';
import * as Location from 'expo-location';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { OfflineQueue } from '../services/offlineQueue';

// react-native-signature-canvas is WebView based; fallback to placeholder if not installed in test
let SignaturePad: any = null;
try {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  SignaturePad = require('react-native-signature-canvas').default;
} catch {
  SignaturePad = null;
}

interface DeliveryVerificationScreenProps {
  tripId?: string;
  stopId?: string;
  stopSequence?: number;
  totalStops?: number;
  stopType?: string;
  requiresOTP?: boolean;
  requiresPOD?: boolean;
  initialConsigneeName?: string;
  initialConsigneePhone?: string;
  onComplete: () => void;
  onBack: () => void;
}

type VerificationTab = 'PHOTO' | 'OTP' | 'SIGN';

export function DeliveryVerificationScreen({
  tripId,
  stopId,
  stopSequence,
  totalStops,
  stopType,
  requiresOTP,
  requiresPOD,
  initialConsigneeName = 'Tata AutoComp Systems Ltd',
  initialConsigneePhone = '+91 98765 43210',
  onComplete,
  onBack,
}: DeliveryVerificationScreenProps) {
  const insets = useSafeAreaInsets();
  const [permission, requestPermission] = useCameraPermissions();
  const [cameraActive, setCameraActive] = useState(false);
  const [capturedPhoto, setCapturedPhoto] = useState<string | null>(null);
  const [cameraRef, setCameraRef] = useState<any>(null);

  const [activeTab, setActiveTab] = useState<VerificationTab>('PHOTO');
  const [consigneeName, setConsigneeName] = useState(initialConsigneeName);
  const [consigneePhone, setConsigneePhone] = useState(initialConsigneePhone);
  const [otp, setOtp] = useState('');
  const [selectedChips, setSelectedChips] = useState<string[]>(['Seal Intact']);
  const [showExceptions, setShowExceptions] = useState(false);
  const [quantityShort, setQuantityShort] = useState('');
  const [damageQty, setDamageQty] = useState('');
  const [refusalReason, setRefusalReason] = useState('');
  const [signatureData, setSignatureData] = useState<string | null>(null);
  const [showSignaturePad, setShowSignaturePad] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const signatureRef = useRef<any>(null);

  const toggleChip = (chip: string) => {
    if (selectedChips.includes(chip)) {
      setSelectedChips(selectedChips.filter((c) => c !== chip));
    } else {
      setSelectedChips([...selectedChips, chip]);
    }
  };

  const compressPhoto = async (uri: string): Promise<string> => {
    try {
      const info = await ImageManipulator.manipulateAsync(uri, [], {
        compress: 0.7,
        format: ImageManipulator.SaveFormat.JPEG,
      });
      return info.uri;
    } catch {
      return uri;
    }
  };

  const takePhoto = async () => {
    if (cameraRef) {
      try {
        const photo = await cameraRef.takePictureAsync();
        const compressedUri = await compressPhoto(photo.uri);
        setCapturedPhoto(compressedUri);
        setCameraActive(false);
      } catch {
        Alert.alert('Camera Error', 'Failed to capture photo proof.');
      }
    }
  };

  const handleSignatureOK = (sig: string) => {
    setSignatureData(sig);
    setShowSignaturePad(false);
  };

  const clearSignature = () => {
    signatureRef.current?.clearSignature();
    setSignatureData(null);
  };

  const getCurrentGPS = async (): Promise<{ latitude: number | null; longitude: number | null }> => {
    try {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status !== 'granted') return { latitude: null, longitude: null };
      const pos = await Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Balanced });
      return { latitude: pos.coords.latitude, longitude: pos.coords.longitude };
    } catch {
      return { latitude: null, longitude: null };
    }
  };

  const submit = async () => {
    if (!tripId) {
      Alert.alert('No Trip Selected', 'Open a trip from the trip list before submitting proof of delivery.');
      return;
    }

    if (!capturedPhoto && !otp.trim() && !signatureData) {
      Alert.alert(
        'Proof Required',
        'Please snap a photo of the stamped POD/LR, enter the receiver OTP, or capture a signature.'
      );
      return;
    }

    setSubmitting(true);
    const gps = await getCurrentGPS();
    const shortVal = quantityShort ? parseFloat(quantityShort) : 0;
    const damageVal = damageQty ? parseFloat(damageQty) : 0;
    const combinedNotes = selectedChips.join(', ');

    const form = new FormData();
    form.append('consignee_name', consigneeName.trim() || 'Tata AutoComp Systems Ltd');
    if (consigneePhone.trim()) {
      form.append('consignee_phone', consigneePhone.trim());
    }
    if (otp.trim()) {
      form.append('otp', otp.trim());
    }
    if (combinedNotes) {
      form.append('notes', combinedNotes);
    }
    if (capturedPhoto) {
      form.append('pod_photo', {
        uri: capturedPhoto,
        name: 'pod.jpg',
        type: 'image/jpeg',
      } as any);
    }
    if (signatureData) {
      form.append('pod_signature_data', signatureData);
      form.append('signature_dataurl', signatureData);
    }
    if (!isNaN(shortVal) && shortVal > 0) {
      form.append('quantity_short', String(shortVal));
    }
    if (!isNaN(damageVal) && damageVal > 0) {
      form.append('damage_qty', String(damageVal));
    }
    if (refusalReason.trim()) {
      form.append('refusal_reason', refusalReason.trim());
    }
    if (stopId) {
      form.append('stop_id', stopId);
      if (stopSequence != null) {
        form.append('stop_sequence', String(stopSequence));
      }
      if (capturedPhoto) {
        form.append('pod_url', capturedPhoto);
      }
      if (signatureData) {
        form.append('signature_url', signatureData);
      }
    }

    try {
      const token = useAuthStore.getState().token;
      const targetUrl = stopId
        ? `${getApiBaseURL()}/trips/${tripId}/stops/${stopId}/pod`
        : `${getApiBaseURL()}/api/v1/trips/${tripId}/deliver-pod`;

      const res = await fetch(targetUrl, {
        method: 'POST',
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: form,
      });

      if (!res.ok) {
        const errBody = await res.json().catch(() => ({}));
        throw new Error(errBody.error || `HTTP ${res.status}`);
      }

      const json = await res.json();
      await OfflineQueue.clearPOD(tripId, stopId);
      const title = stopId ? `Stop ${stopSequence || ''} Verified` : 'Delivered';
      const msg = stopId
        ? `Proof of delivery recorded successfully!`
        : `Trip ${json.trip_number || tripId} completed & marked delivered!`;
      Alert.alert(title, msg, [
        { text: 'OK', onPress: onComplete },
      ]);
    } catch {
      await OfflineQueue.enqueuePOD(tripId, {
        stop_id: stopId || null,
        stop_sequence: stopSequence || null,
        otp: otp.trim() || null,
        consignee_name: consigneeName.trim() || 'Tata AutoComp Systems Ltd',
        consignee_phone: consigneePhone.trim() || null,
        notes: combinedNotes,
        photo_uri: capturedPhoto,
        latitude: gps.latitude,
        longitude: gps.longitude,
        pod_signature_data: signatureData,
        quantity_short: isNaN(shortVal) ? null : shortVal,
        damage_qty: isNaN(damageVal) ? null : damageVal,
        refusal_reason: refusalReason.trim() || null,
      });
      Alert.alert('Saved Offline', 'Delivery proof queued in offline storage. Will sync when back online.', [
        { text: 'OK', onPress: onComplete },
      ]);
    } finally {
      setSubmitting(false);
    }
  };

  const isVerified = Boolean(capturedPhoto || otp.length >= 4 || signatureData);

  return (
    <SafeAreaView style={styles.safeArea} edges={['top', 'left', 'right']}>
      <StatusBar style="light" backgroundColor="#075e54" />

      {/* Header */}
      <View style={styles.header}>
        <TouchableOpacity style={styles.backBtn} onPress={onBack}>
          <MaterialCommunityIcons name="arrow-left" size={22} color="#ffffff" />
        </TouchableOpacity>
        <View style={{ flex: 1, marginLeft: 10 }}>
          <Text style={styles.headerTitle}>PROOF OF DELIVERY (e-POD)</Text>
          <Text style={styles.headerSubtitle}>Trip #{tripId || 'TRP-8491'}</Text>
        </View>
      </View>

      {cameraActive ? (
        <View style={styles.cameraContainer}>
          {!permission?.granted ? (
            <View style={styles.permissionBox}>
              <Text style={styles.permissionText}>Camera permission required to capture stamped POD.</Text>
              <TouchableOpacity style={styles.primaryActionBtn} onPress={requestPermission}>
                <Text style={styles.primaryActionBtnText}>GRANT CAMERA PERMISSION</Text>
              </TouchableOpacity>
              <TouchableOpacity style={{ marginTop: 12 }} onPress={() => setCameraActive(false)}>
                <Text style={{ color: '#ffffff', fontWeight: '700' }}>CANCEL</Text>
              </TouchableOpacity>
            </View>
          ) : (
            <CameraView style={styles.cameraView} ref={(ref) => setCameraRef(ref)}>
              <View style={styles.cameraOverlay}>
                <View style={styles.scannerFrame} />
                <Text style={styles.cameraGuideText}>ALIGN STAMPED BILTY / GATE PASS IN FRAME</Text>
                <TouchableOpacity style={styles.captureBtn} onPress={takePhoto}>
                  <View style={styles.captureInnerCircle} />
                </TouchableOpacity>
                <TouchableOpacity style={styles.closeCameraBtn} onPress={() => setCameraActive(false)}>
                  <MaterialCommunityIcons name="close" size={24} color="#ffffff" />
                </TouchableOpacity>
              </View>
            </CameraView>
          )}
        </View>
      ) : showSignaturePad ? (
        <View style={styles.signaturePadContainer}>
          <View style={styles.signatureHeader}>
            <Text style={styles.signatureHeaderText}>RECEIVER SIGNATURE ON SCREEN</Text>
            <TouchableOpacity onPress={() => setShowSignaturePad(false)}>
              <MaterialCommunityIcons name="close" size={24} color="#111b21" />
            </TouchableOpacity>
          </View>
          {SignaturePad ? (
            <SignaturePad
              ref={signatureRef}
              onOK={handleSignatureOK}
              onEmpty={() => Alert.alert('Empty Signature', 'Please sign above.')}
              descriptionText="Receiver: Sign with finger above"
              clearText="Clear"
              confirmText="Done"
              webStyle={`.m-signature-pad {box-shadow: none; border: 2px dashed #008069;} .m-signature-pad--body {border: none;}`}
            />
          ) : (
            <View style={styles.signatureFallback}>
              <Text style={{ color: '#667781', textAlign: 'center' }}>
                Signature pad ready. Tap Done to save.
              </Text>
            </View>
          )}
          <View style={styles.signatureActions}>
            <TouchableOpacity style={styles.secBtn} onPress={clearSignature}>
              <Text style={styles.secBtnText}>CLEAR</Text>
            </TouchableOpacity>
            <TouchableOpacity
              style={styles.primaryActionBtn}
              onPress={() => signatureRef.current?.readSignature()}
            >
              <Text style={styles.primaryActionBtnText}>CONFIRM SIGNATURE</Text>
            </TouchableOpacity>
          </View>
        </View>
      ) : (
        <ScrollView
          style={styles.body}
          contentContainerStyle={[styles.scrollContent, { paddingBottom: Math.max(insets.bottom + 20, 30) }]}
          showsVerticalScrollIndicator={false}
        >
          {/* Pre-Filled Consignee Info Card (Zero Driver Typing) */}
          <View style={styles.consigneeCard}>
            <View style={styles.consigneeTopRow}>
              <View style={styles.receivingBadge}>
                <MaterialCommunityIcons name="factory" size={14} color="#008069" />
                <Text style={styles.receivingBadgeText}>DESTINATION CONSIGNEE</Text>
              </View>
              <Text style={styles.autoFilledTag}>✓ AUTO-VERIFIED</Text>
            </View>
            <Text style={styles.consigneeNameText}>{consigneeName}</Text>
            <Text style={styles.consigneeSubText}>Gate 3 Receiving Bay • Chakan MIDC, Pune</Text>
            <View style={styles.divider} />
            <View style={styles.metaRow}>
              <Text style={styles.metaText}>📦 18 Tons Steel Coils</Text>
              <Text style={styles.metaText}>📄 EWB #7291-8841-0294</Text>
            </View>
          </View>

          {/* Verification Method Tabs */}
          <Text style={styles.sectionTitle}>CHOOSE 1 PROOF METHOD</Text>
          <View style={styles.tabsRow}>
            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'PHOTO' && styles.tabBtnActive]}
              onPress={() => setActiveTab('PHOTO')}
            >
              <MaterialCommunityIcons
                name="camera"
                size={18}
                color={activeTab === 'PHOTO' ? '#008069' : '#667781'}
              />
              <Text style={[styles.tabText, activeTab === 'PHOTO' && styles.tabTextActive]}>
                📸 Photo Bilty
              </Text>
              {capturedPhoto && <View style={styles.tabDoneDot} />}
            </TouchableOpacity>

            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'OTP' && styles.tabBtnActive]}
              onPress={() => setActiveTab('OTP')}
            >
              <MaterialCommunityIcons
                name="numeric"
                size={18}
                color={activeTab === 'OTP' ? '#008069' : '#667781'}
              />
              <Text style={[styles.tabText, activeTab === 'OTP' && styles.tabTextActive]}>
                🔢 4-Digit OTP
              </Text>
              {otp.length >= 4 && <View style={styles.tabDoneDot} />}
            </TouchableOpacity>

            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'SIGN' && styles.tabBtnActive]}
              onPress={() => setActiveTab('SIGN')}
            >
              <MaterialCommunityIcons
                name="draw"
                size={18}
                color={activeTab === 'SIGN' ? '#008069' : '#667781'}
              />
              <Text style={[styles.tabText, activeTab === 'SIGN' && styles.tabTextActive]}>
                ✍️ Screen Sign
              </Text>
              {signatureData && <View style={styles.tabDoneDot} />}
            </TouchableOpacity>
          </View>

          {/* Method 1: Photo of Stamped POD */}
          {activeTab === 'PHOTO' && (
            <View style={styles.methodBox}>
              {capturedPhoto ? (
                <View style={styles.photoAttachedBox}>
                  <Image source={{ uri: capturedPhoto }} style={styles.photoThumb} />
                  <View style={{ flex: 1 }}>
                    <View style={styles.verifiedRow}>
                      <MaterialCommunityIcons name="check-circle" size={16} color="#008069" />
                      <Text style={styles.verifiedText}>Stamped POD Attached</Text>
                    </View>
                    <TouchableOpacity
                      style={styles.retakePill}
                      onPress={() => setCameraActive(true)}
                    >
                      <MaterialCommunityIcons name="camera-retake" size={14} color="#008069" />
                      <Text style={styles.retakeText}>Retake Photo</Text>
                    </TouchableOpacity>
                  </View>
                </View>
              ) : (
                <TouchableOpacity
                  style={styles.bigCameraBtn}
                  activeOpacity={0.85}
                  onPress={() => {
                    if (!permission?.granted) {
                      requestPermission().then((res) => {
                        if (res.granted) setCameraActive(true);
                      });
                    } else {
                      setCameraActive(true);
                    }
                  }}
                >
                  <View style={styles.cameraIconCircle}>
                    <MaterialCommunityIcons name="camera" size={32} color="#008069" />
                  </View>
                  <Text style={styles.cameraBtnTitle}>SNAP STAMPED BILTY / GATE PASS</Text>
                  <Text style={styles.cameraBtnSub}>Tap to open camera and snap 1 photo</Text>
                </TouchableOpacity>
              )}
            </View>
          )}

          {/* Method 2: OTP Entry */}
          {activeTab === 'OTP' && (
            <View style={styles.methodBox}>
              <Text style={styles.otpPrompt}>Ask Receiver for 4-Digit SMS Delivery Code:</Text>
              <TextInput
                style={styles.otpInput}
                keyboardType="numeric"
                maxLength={6}
                placeholder="• • • •"
                placeholderTextColor="#94a3b8"
                value={otp}
                onChangeText={setOtp}
              />
              <Text style={styles.otpHint}>OTP sent automatically to consignee mobile</Text>
            </View>
          )}

          {/* Method 3: Screen Sign */}
          {activeTab === 'SIGN' && (
            <View style={styles.methodBox}>
              {signatureData ? (
                <View style={styles.signAttachedBox}>
                  <Image source={{ uri: signatureData }} style={styles.signThumb} resizeMode="contain" />
                  <View style={{ flex: 1 }}>
                    <View style={styles.verifiedRow}>
                      <MaterialCommunityIcons name="check-circle" size={16} color="#008069" />
                      <Text style={styles.verifiedText}>Signature Recorded</Text>
                    </View>
                    <TouchableOpacity
                      style={styles.retakePill}
                      onPress={() => setShowSignaturePad(true)}
                    >
                      <Text style={styles.retakeText}>Sign Again</Text>
                    </TouchableOpacity>
                  </View>
                </View>
              ) : (
                <TouchableOpacity
                  style={styles.bigSignBtn}
                  activeOpacity={0.85}
                  onPress={() => setShowSignaturePad(true)}
                >
                  <MaterialCommunityIcons name="draw" size={32} color="#008069" />
                  <Text style={styles.cameraBtnTitle}>TAP TO GET RECEIVER SIGNATURE</Text>
                  <Text style={styles.cameraBtnSub}>Receiver signs with finger on screen</Text>
                </TouchableOpacity>
              )}
            </View>
          )}

          {/* Quick Remarks Chips (Zero Typing) */}
          <Text style={styles.sectionTitle}>QUICK STATUS</Text>
          <View style={styles.chipsWrap}>
            {['Seal Intact', 'On-Time Unload', 'Verified by Gate', 'Payment Received'].map((chip) => {
              const active = selectedChips.includes(chip);
              return (
                <TouchableOpacity
                  key={chip}
                  style={[styles.statusChip, active && styles.statusChipActive]}
                  onPress={() => toggleChip(chip)}
                >
                  <MaterialCommunityIcons
                    name={active ? 'checkbox-marked-circle' : 'plus-circle-outline'}
                    size={14}
                    color={active ? '#008069' : '#64748b'}
                  />
                  <Text style={[styles.statusChipText, active && styles.statusChipTextActive]}>
                    {chip}
                  </Text>
                </TouchableOpacity>
              );
            })}
          </View>

          {/* Collapsed Exception Toggle (Clean for 98% Normal Trips) */}
          <TouchableOpacity
            style={styles.exceptionToggle}
            onPress={() => setShowExceptions(!showExceptions)}
          >
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
              <MaterialCommunityIcons name="alert-circle-outline" size={16} color="#b45309" />
              <Text style={styles.exceptionToggleText}>
                {showExceptions ? 'Hide Cargo Issues' : '⚠️ Report Cargo Shortage or Damage (Optional)'}
              </Text>
            </View>
            <MaterialCommunityIcons
              name={showExceptions ? 'chevron-up' : 'chevron-down'}
              size={18}
              color="#b45309"
            />
          </TouchableOpacity>

          {showExceptions && (
            <View style={styles.exceptionsDrawer}>
              <View style={styles.rowInputs}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.fieldLabel}>SHORT QTY (TONS/BOXES)</Text>
                  <TextInput
                    style={styles.numberInput}
                    keyboardType="numeric"
                    placeholder="0"
                    value={quantityShort}
                    onChangeText={setQuantityShort}
                  />
                </View>
                <View style={{ width: 12 }} />
                <View style={{ flex: 1 }}>
                  <Text style={styles.fieldLabel}>DAMAGED QTY</Text>
                  <TextInput
                    style={styles.numberInput}
                    keyboardType="numeric"
                    placeholder="0"
                    value={damageQty}
                    onChangeText={setDamageQty}
                  />
                </View>
              </View>

              <Text style={[styles.fieldLabel, { marginTop: 10 }]}>DAMAGE REASON</Text>
              <TextInput
                style={styles.textInput}
                placeholder="e.g. Broken seal, water leak, box crushed"
                value={refusalReason}
                onChangeText={setRefusalReason}
              />
            </View>
          )}

          {/* Big Confirm CTA */}
          <TouchableOpacity
            style={[styles.submitBtn, !isVerified && styles.submitBtnDimmed]}
            activeOpacity={0.88}
            onPress={submit}
            disabled={submitting}
          >
            {submitting ? (
              <ActivityIndicator color="#ffffff" size="small" />
            ) : (
              <>
                <MaterialCommunityIcons name="check-decagram" size={20} color="#ffffff" />
                <Text style={styles.submitBtnText}>CONFIRM DELIVERY & CLOSE TRIP</Text>
              </>
            )}
          </TouchableOpacity>
        </ScrollView>
      )}
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
    paddingVertical: 12,
    flexDirection: 'row',
    alignItems: 'center',
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
    fontSize: 15,
    fontWeight: '800',
    color: '#ffffff',
  },
  headerSubtitle: {
    fontSize: 11,
    color: '#dcf8c6',
    fontWeight: '600',
  },
  body: {
    flex: 1,
    backgroundColor: '#efeae2',
  },
  scrollContent: {
    padding: 14,
    gap: 12,
  },
  consigneeCard: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 14,
    borderWidth: 1,
    borderColor: '#e2e8f0',
    elevation: 1,
  },
  consigneeTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 6,
  },
  receivingBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  receivingBadgeText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#008069',
    letterSpacing: 0.5,
  },
  autoFilledTag: {
    fontSize: 9,
    fontWeight: '800',
    color: '#008069',
    backgroundColor: '#e7ffdb',
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 4,
  },
  consigneeNameText: {
    fontSize: 16,
    fontWeight: '800',
    color: '#0f172a',
  },
  consigneeSubText: {
    fontSize: 11,
    color: '#64748b',
    marginTop: 2,
  },
  divider: {
    height: 1,
    backgroundColor: '#f1f5f9',
    marginVertical: 10,
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  metaText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#334155',
  },
  sectionTitle: {
    fontSize: 10,
    fontWeight: '800',
    color: '#64748b',
    letterSpacing: 0.5,
    marginTop: 4,
  },
  tabsRow: {
    flexDirection: 'row',
    gap: 8,
  },
  tabBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 5,
    backgroundColor: '#ffffff',
    paddingVertical: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  tabBtnActive: {
    backgroundColor: '#e7ffdb',
    borderColor: '#25d366',
  },
  tabText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#64748b',
  },
  tabTextActive: {
    color: '#008069',
    fontWeight: '800',
  },
  tabDoneDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#25d366',
    position: 'absolute',
    top: 4,
    right: 4,
  },
  methodBox: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 16,
    borderWidth: 1,
    borderColor: '#e2e8f0',
    alignItems: 'center',
  },
  bigCameraBtn: {
    alignItems: 'center',
    paddingVertical: 12,
    width: '100%',
  },
  cameraIconCircle: {
    width: 60,
    height: 60,
    borderRadius: 30,
    backgroundColor: '#e7ffdb',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 10,
  },
  cameraBtnTitle: {
    fontSize: 12,
    fontWeight: '800',
    color: '#0f172a',
    letterSpacing: 0.5,
  },
  cameraBtnSub: {
    fontSize: 11,
    color: '#64748b',
    marginTop: 2,
  },
  photoAttachedBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    width: '100%',
  },
  photoThumb: {
    width: 64,
    height: 64,
    borderRadius: 8,
  },
  verifiedRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  verifiedText: {
    fontSize: 12,
    fontWeight: '800',
    color: '#008069',
  },
  retakePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    marginTop: 6,
  },
  retakeText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#008069',
  },
  otpPrompt: {
    fontSize: 12,
    fontWeight: '700',
    color: '#334155',
    marginBottom: 8,
  },
  otpInput: {
    backgroundColor: '#f8fafc',
    borderRadius: 10,
    paddingHorizontal: 20,
    paddingVertical: 10,
    fontSize: 24,
    fontWeight: '900',
    letterSpacing: 10,
    textAlign: 'center',
    width: 200,
    borderWidth: 1,
    borderColor: '#cbd5e1',
    color: '#0f172a',
  },
  otpHint: {
    fontSize: 10,
    color: '#94a3b8',
    marginTop: 6,
  },
  bigSignBtn: {
    alignItems: 'center',
    paddingVertical: 12,
    width: '100%',
  },
  signAttachedBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    width: '100%',
  },
  signThumb: {
    width: 80,
    height: 50,
    borderRadius: 6,
    backgroundColor: '#f8fafc',
  },
  chipsWrap: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
  },
  statusChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    backgroundColor: '#ffffff',
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  statusChipActive: {
    backgroundColor: '#e7ffdb',
    borderColor: '#25d366',
  },
  statusChipText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#64748b',
  },
  statusChipTextActive: {
    color: '#008069',
  },
  exceptionToggle: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 8,
    paddingHorizontal: 10,
    backgroundColor: '#fef3c7',
    borderRadius: 8,
  },
  exceptionToggleText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#b45309',
  },
  exceptionsDrawer: {
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 12,
    borderWidth: 1,
    borderColor: '#fde68a',
  },
  rowInputs: {
    flexDirection: 'row',
  },
  fieldLabel: {
    fontSize: 9,
    fontWeight: '800',
    color: '#64748b',
    marginBottom: 4,
  },
  numberInput: {
    backgroundColor: '#f8fafc',
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 6,
    fontSize: 14,
    fontWeight: '800',
    color: '#0f172a',
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  textInput: {
    backgroundColor: '#f8fafc',
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 8,
    fontSize: 12,
    color: '#0f172a',
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  submitBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    backgroundColor: '#008069',
    paddingVertical: 14,
    borderRadius: 10,
    marginTop: 8,
    elevation: 3,
  },
  submitBtnDimmed: {
    backgroundColor: '#008069',
    opacity: 0.9,
  },
  submitBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  cameraContainer: {
    flex: 1,
    backgroundColor: '#000000',
  },
  cameraView: {
    flex: 1,
  },
  cameraOverlay: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  scannerFrame: {
    width: 280,
    height: 200,
    borderWidth: 2,
    borderColor: '#25d366',
    borderRadius: 12,
  },
  cameraGuideText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    marginTop: 16,
    backgroundColor: 'rgba(0,0,0,0.6)',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 6,
  },
  captureBtn: {
    position: 'absolute',
    bottom: 30,
    width: 68,
    height: 68,
    borderRadius: 34,
    borderWidth: 4,
    borderColor: '#ffffff',
    alignItems: 'center',
    justifyContent: 'center',
  },
  captureInnerCircle: {
    width: 52,
    height: 52,
    borderRadius: 26,
    backgroundColor: '#25d366',
  },
  closeCameraBtn: {
    position: 'absolute',
    top: 40,
    right: 20,
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: 'rgba(0,0,0,0.5)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  permissionBox: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  permissionText: {
    color: '#ffffff',
    fontSize: 13,
    textAlign: 'center',
    marginBottom: 16,
  },
  primaryActionBtn: {
    backgroundColor: '#008069',
    paddingHorizontal: 20,
    paddingVertical: 12,
    borderRadius: 8,
  },
  primaryActionBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
  },
  signaturePadContainer: {
    flex: 1,
    backgroundColor: '#ffffff',
    padding: 16,
  },
  signatureHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  signatureHeaderText: {
    fontSize: 13,
    fontWeight: '800',
    color: '#0f172a',
  },
  signatureFallback: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  signatureActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
    marginTop: 12,
  },
  secBtn: {
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderRadius: 8,
    backgroundColor: '#f1f5f9',
  },
  secBtnText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#64748b',
  },
});
