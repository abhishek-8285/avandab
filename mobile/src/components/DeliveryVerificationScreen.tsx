import React, { useState, useRef } from 'react';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  ScrollView,
  StatusBar,
  TextInput,
  ActivityIndicator,
  Alert,
} from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { CameraView, useCameraPermissions } from 'expo-camera';
import * as ImageManipulator from 'expo-image-manipulator';
import * as Location from 'expo-location';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
import { t } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
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
  const locale = useLanguageStore((s) => s.locale);
  const insets = useSafeAreaInsets();
  const [permission, requestPermission] = useCameraPermissions();
  const [cameraActive, setCameraActive] = useState(false);
  const [capturedPhoto, setCapturedPhoto] = useState<string | null>(null);
  const [cameraRef, setCameraRef] = useState<any>(null);

  const [activeTab, setActiveTab] = useState<VerificationTab>('PHOTO');
  const [consigneeName, setConsigneeName] = useState(initialConsigneeName);
  const [consigneePhone, setConsigneePhone] = useState(initialConsigneePhone);
  const [otp, setOtp] = useState('');
  // Chip selection stores stable keys; English canonicals go to the server
  // as notes while the driver sees translated labels.
  const [selectedChips, setSelectedChips] = useState<string[]>(['seal']);
  const [showExceptions, setShowExceptions] = useState(false);
  const [quantityShort, setQuantityShort] = useState('');
  const [damageQty, setDamageQty] = useState('');
  const [refusalReason, setRefusalReason] = useState('');
  const [signatureData, setSignatureData] = useState<string | null>(null);
  const [showSignaturePad, setShowSignaturePad] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const signatureRef = useRef<any>(null);

  const CHIP_EN: Record<string, string> = {
    seal: 'Seal Intact',
    ontime: 'On-Time Unload',
    gate: 'Verified by Gate',
    payment: 'Payment Received',
  };

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
        Alert.alert(t('delivery.alert_cam', 'Camera Error', locale), t('delivery.alert_cam_body', 'Failed to capture photo proof.', locale));
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
      Alert.alert(t('delivery.alert_no_trip', 'No Trip Selected', locale), t('delivery.alert_no_trip_body', 'Open a trip from the trip list before submitting proof of delivery.', locale));
      return;
    }

    if (!capturedPhoto && !otp.trim() && !signatureData) {
      Alert.alert(
        t('delivery.alert_proof', 'Proof Required', locale),
        t('delivery.alert_proof_body', 'Please snap a photo of the stamped POD/LR, enter the receiver OTP, or capture a signature.', locale)
      );
      return;
    }

    setSubmitting(true);
    const gps = await getCurrentGPS();
    const shortVal = quantityShort ? parseFloat(quantityShort) : 0;
    const damageVal = damageQty ? parseFloat(damageQty) : 0;
    const combinedNotes = selectedChips.map((k) => CHIP_EN[k] || k).join(', ');

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
        ? `${getApiBaseURL()}/api/v1/trips/${tripId}/stops/${stopId}/pod`
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
      const title = stopId ? `${t('delivery.stop_prefix', 'Stop', locale)} ${stopSequence || ''} ${t('delivery.verified', 'Verified', locale)}` : t('delivery.alert_delivered', 'Delivered', locale);
      const msg = stopId
        ? t('delivery.alert_pod_ok', 'Proof of delivery recorded successfully!', locale)
        : `${t('delivery.trip_word', 'Trip', locale)} ${json.trip_number || tripId} ${t('delivery.completed_msg', 'completed & marked delivered!', locale)}`;
      Alert.alert(title, msg, [
        { text: t('common.ok', 'OK', locale), onPress: onComplete },
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
      Alert.alert(t('delivery.saved_offline', 'Saved Offline', locale), t('delivery.offline_body', 'Delivery proof queued in offline storage. Will sync when back online.', locale), [
        { text: t('common.ok', 'OK', locale), onPress: onComplete },
      ]);
    } finally {
      setSubmitting(false);
    }
  };

  const isVerified = Boolean(capturedPhoto || otp.length >= 4 || signatureData);
  const methodsDone = [capturedPhoto ? 1 : 0, otp.length >= 4 ? 1 : 0, signatureData ? 1 : 0].reduce((a, b) => a + b, 0);

  return (
    <SafeAreaView style={styles.safeArea} edges={['top', 'left', 'right']}>
      <StatusBar barStyle="light-content" backgroundColor={Colors.chrome} />

      {/* Header */}
      <View style={styles.header}>
        <TouchableOpacity style={styles.backBtn} onPress={onBack} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_back', 'Go back', locale)} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
          <MaterialCommunityIcons name="arrow-left" size={22} color={Colors.textOnPrimary} accessible={false} />
        </TouchableOpacity>
        <View style={{ flex: 1, marginLeft: 10 }}>
          <Text style={styles.headerTitle}>{t('delivery.header_title', 'PROOF OF DELIVERY (e-POD)', locale)}</Text>
          <Text style={styles.headerSubtitle}>{t('delivery.trip_prefix', 'Trip #', locale)}{tripId || 'TRP-8491'}</Text>
        </View>
        <View style={[styles.readyPill, isVerified ? styles.readyPillDone : styles.readyPillPending]}>
          <Text style={[styles.readyPillText, isVerified ? styles.readyPillTextDone : styles.readyPillTextPending]}>
            {isVerified ? t('delivery.proof_ready', 'READY', locale) : t('delivery.proof_pending', 'PENDING', locale)}
          </Text>
        </View>
      </View>

      {/* Proof progress: how many of the 3 methods are attached */}
      {!cameraActive && !showSignaturePad && (
        <View style={styles.progressStrip} accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 3, now: methodsDone }}>
          <View style={styles.progressTrack}>
            <View style={[styles.progressFill, { width: `${(methodsDone / 3) * 100}%` }]} />
          </View>
          <Text style={styles.progressText}>
            {methodsDone}/3 {t('delivery.progress_attached', 'attached', locale)}
          </Text>
        </View>
      )}

      {cameraActive ? (
        <View style={styles.cameraContainer}>
          {!permission?.granted ? (
            <View style={styles.permissionBox}>
              <Text style={styles.permissionText}>{t('delivery.cam_need', 'Camera permission required to capture stamped POD.', locale)}</Text>
              <TouchableOpacity style={styles.primaryActionBtn} onPress={requestPermission} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_grant_cam', 'Grant camera permission', locale)} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
                <Text style={styles.primaryActionBtnText}>{t('delivery.cam_grant', 'GRANT CAMERA PERMISSION', locale)}</Text>
              </TouchableOpacity>
              <TouchableOpacity style={{ marginTop: 12, minHeight: 44, minWidth: 44, justifyContent: 'center', alignItems: 'center' }} onPress={() => setCameraActive(false)} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_cancel_cam', 'Cancel camera', locale)} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
                <Text style={{ color: Colors.textOnPrimary, fontWeight: '700' }}>{t('delivery.cancel', 'CANCEL', locale)}</Text>
              </TouchableOpacity>
            </View>
          ) : (
            <CameraView style={styles.cameraView} ref={(ref) => setCameraRef(ref)}>
              <View style={styles.cameraOverlay}>
                <View style={styles.scannerFrame} />
                <Text style={styles.cameraGuideText}>{t('delivery.cam_guide', 'ALIGN STAMPED BILTY / GATE PASS IN FRAME', locale)}</Text>
                <TouchableOpacity style={styles.captureBtn} onPress={takePhoto} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_capture', 'Capture photo proof', locale)}>
                  <View style={styles.captureInnerCircle} />
                </TouchableOpacity>
                <TouchableOpacity style={styles.closeCameraBtn} onPress={() => setCameraActive(false)} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_cancel_cam', 'Cancel camera', locale)} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
                  <MaterialCommunityIcons name="close" size={24} color={Colors.textOnPrimary} accessible={false} />
                </TouchableOpacity>
              </View>
            </CameraView>
          )}
        </View>
      ) : showSignaturePad ? (
        <View style={styles.signaturePadContainer}>
          <View style={styles.signatureHeader}>
            <Text style={styles.signatureHeaderText}>{t('delivery.sign_sheet_title', 'RECEIVER SIGNATURE ON SCREEN', locale)}</Text>
            <TouchableOpacity onPress={() => setShowSignaturePad(false)} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_close_sign', 'Close signature pad', locale)} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }} style={{ minHeight: 44, minWidth: 44, justifyContent: 'center', alignItems: 'center' }}>
              <MaterialCommunityIcons name="close" size={24} color={Colors.textPrimary} accessible={false} />
            </TouchableOpacity>
          </View>
          {SignaturePad ? (
            <SignaturePad
              ref={signatureRef}
              onOK={handleSignatureOK}
              onEmpty={() => Alert.alert(t('delivery.sign_empty_title', 'Empty Signature', locale), t('delivery.sign_empty_body', 'Please sign above.', locale))}
              descriptionText={t('delivery.sign_desc', 'Receiver: Sign with finger above', locale)}
              clearText={t('delivery.sign_clear', 'Clear', locale)}
              confirmText={t('delivery.sign_done', 'Done', locale)}
              webStyle={`.m-signature-pad {box-shadow: none; border: 2px dashed ${Colors.primary};} .m-signature-pad--body {border: none;}`}
            />
          ) : (
            <View style={styles.signatureFallback}>
              <Text style={{ color: Colors.textSecondary, textAlign: 'center' }}>
                Signature pad ready. Tap Done to save.
              </Text>
            </View>
          )}
          <View style={styles.signatureActions}>
            <TouchableOpacity style={styles.secBtn} onPress={clearSignature} accessibilityRole="button" accessibilityLabel={t('delivery.a11y_clear_sign', 'Clear signature', locale)} hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}>
              <Text style={styles.secBtnText}>{t('delivery.sign_clear', 'CLEAR', locale)}</Text>
            </TouchableOpacity>
            <TouchableOpacity
              style={styles.primaryActionBtn}
              onPress={() => signatureRef.current?.readSignature()}
              accessibilityRole="button"
              accessibilityLabel={t('delivery.a11y_confirm_sign', 'Confirm signature', locale)}
              hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}
            >
              <Text style={styles.primaryActionBtnText}>{t('delivery.sign_confirm', 'CONFIRM SIGNATURE', locale)}</Text>
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
                <MaterialCommunityIcons name="factory" size={14} color={Colors.primary} accessible={false} />
                <Text style={styles.receivingBadgeText}>{t('delivery.dest_consignee', 'DESTINATION CONSIGNEE', locale)}</Text>
              </View>
              <Text style={styles.autoFilledTag}>{t('delivery.auto_verified', '✓ AUTO-VERIFIED', locale)}</Text>
            </View>
            <Text style={styles.consigneeNameText} numberOfLines={1} ellipsizeMode="tail">{consigneeName}</Text>
            <Text style={styles.consigneeSubText} numberOfLines={2} ellipsizeMode="tail">Gate 3 Receiving Bay • Chakan MIDC, Pune</Text>
            <View style={styles.divider} />
            <View style={styles.metaRow}>
              <Text style={styles.metaText} accessibilityLabel="18 Tons Steel Coils" numberOfLines={1} ellipsizeMode="tail">📦 18 Tons Steel Coils</Text>
              <Text style={styles.metaText} accessibilityLabel="E-way bill 7291-8841-0294" numberOfLines={1} ellipsizeMode="tail">📄 EWB #7291-8841-0294</Text>
            </View>
          </View>

          {/* Verification Method Tabs */}
          <Text style={styles.sectionTitle}>{t('delivery.choose_method', 'CHOOSE 1 PROOF METHOD', locale)}</Text>
          <View style={styles.tabsRow}>
            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'PHOTO' && styles.tabBtnActive]}
              onPress={() => setActiveTab('PHOTO')}
              accessibilityRole="radio"
              accessibilityState={{ selected: activeTab === 'PHOTO' }}
              accessibilityLabel={t('delivery.a11y_tab_photo', 'Photo Bilty', locale)}
              hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            >
              <MaterialCommunityIcons
                name="camera"
                size={18}
                color={activeTab === 'PHOTO' ? Colors.primary : Colors.textSecondary}
                accessible={false}
              />
              <Text style={[styles.tabText, activeTab === 'PHOTO' && styles.tabTextActive]}>
                {t('delivery.tab_photo', '📸 Photo Bilty', locale)}
              </Text>
              {capturedPhoto && <View style={styles.tabDoneDot} />}
            </TouchableOpacity>

            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'OTP' && styles.tabBtnActive]}
              onPress={() => setActiveTab('OTP')}
              accessibilityRole="radio"
              accessibilityState={{ selected: activeTab === 'OTP' }}
              accessibilityLabel={t('delivery.a11y_tab_otp', '4-Digit OTP', locale)}
              hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            >
              <MaterialCommunityIcons
                name="numeric"
                size={18}
                color={activeTab === 'OTP' ? Colors.primary : Colors.textSecondary}
                accessible={false}
              />
              <Text style={[styles.tabText, activeTab === 'OTP' && styles.tabTextActive]}>
                {t('delivery.tab_otp', '🔢 4-Digit OTP', locale)}
              </Text>
              {otp.length >= 4 && <View style={styles.tabDoneDot} />}
            </TouchableOpacity>

            <TouchableOpacity
              style={[styles.tabBtn, activeTab === 'SIGN' && styles.tabBtnActive]}
              onPress={() => setActiveTab('SIGN')}
              accessibilityRole="radio"
              accessibilityState={{ selected: activeTab === 'SIGN' }}
              accessibilityLabel={t('delivery.a11y_tab_sign', 'Screen Sign', locale)}
              hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
            >
              <MaterialCommunityIcons
                name="draw"
                size={18}
                color={activeTab === 'SIGN' ? Colors.primary : Colors.textSecondary}
                accessible={false}
              />
              <Text style={[styles.tabText, activeTab === 'SIGN' && styles.tabTextActive]}>
                {t('delivery.tab_sign', '✍️ Screen Sign', locale)}
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
                      <MaterialCommunityIcons name="check-circle" size={16} color={Colors.primary} accessible={false} />
                      <Text style={styles.verifiedText}>{t('delivery.photo_attached', 'Stamped POD Attached', locale)}</Text>
                    </View>
                    <TouchableOpacity
                      style={styles.retakePill}
                      onPress={() => setCameraActive(true)}
                      accessibilityRole="button"
                      accessibilityLabel={t('delivery.a11y_retake', 'Retake photo', locale)}
                      hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                    >
                      <MaterialCommunityIcons name="camera-retake" size={14} color={Colors.primary} accessible={false} />
                      <Text style={styles.retakeText}>{t('delivery.retake', 'Retake Photo', locale)}</Text>
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
                  accessibilityRole="button"
                  accessibilityLabel={t('delivery.a11y_snap', 'Snap stamped bilty photo', locale)}
                >
                  <View style={styles.cameraIconCircle}>
                    <MaterialCommunityIcons name="camera" size={32} color={Colors.primary} accessible={false} />
                  </View>
                  <Text style={styles.cameraBtnTitle}>{t('delivery.snap_title', 'SNAP STAMPED BILTY / GATE PASS', locale)}</Text>
                  <Text style={styles.cameraBtnSub}>{t('delivery.snap_sub', 'Tap to open camera and snap 1 photo', locale)}</Text>
                </TouchableOpacity>
              )}
            </View>
          )}

          {/* Method 2: OTP Entry */}
          {activeTab === 'OTP' && (
            <View style={styles.methodBox}>
              <Text style={styles.otpPrompt}>{t('delivery.otp_prompt', 'Ask Receiver for 4-Digit SMS Delivery Code:', locale)}</Text>
              <TextInput
                style={styles.otpInput}
                keyboardType="numeric"
                maxLength={6}
                placeholder={t('delivery.placeholder_otp', 'e.g. 1234', locale)}
                placeholderTextColor={Colors.modalSub}
                value={otp}
                onChangeText={setOtp}
                accessibilityLabel={t('delivery.a11y_otp', 'Delivery OTP', locale)}
                autoComplete="sms-otp"
                textContentType="oneTimeCode"
              />
              <Text style={styles.otpHint} accessibilityLiveRegion="polite">{t('delivery.otp_hint', 'OTP sent automatically to consignee mobile', locale)}</Text>
            </View>
          )}

          {/* Method 3: Screen Sign */}
          {activeTab === 'SIGN' && (
            <View style={styles.methodBox}>
              {signatureData ? (
                <View style={styles.signAttachedBox}>
                  <Image source={{ uri: signatureData }} style={styles.signThumb} contentFit="contain" />
                  <View style={{ flex: 1 }}>
                    <View style={styles.verifiedRow}>
                      <MaterialCommunityIcons name="check-circle" size={16} color={Colors.primary} accessible={false} />
                      <Text style={styles.verifiedText}>{t('delivery.sign_attached', 'Signature Recorded', locale)}</Text>
                    </View>
                    <TouchableOpacity
                      style={styles.retakePill}
                      onPress={() => setShowSignaturePad(true)}
                      accessibilityRole="button"
                      accessibilityLabel={t('delivery.a11y_sign_again', 'Sign again', locale)}
                      hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                    >
                      <Text style={styles.retakeText}>{t('delivery.sign_again', 'Sign Again', locale)}</Text>
                    </TouchableOpacity>
                  </View>
                </View>
              ) : (
                <TouchableOpacity
                  style={styles.bigSignBtn}
                  activeOpacity={0.85}
                  onPress={() => setShowSignaturePad(true)}
                  accessibilityRole="button"
                  accessibilityLabel={t('delivery.a11y_get_sign', 'Get receiver signature', locale)}
                >
                  <MaterialCommunityIcons name="draw" size={32} color={Colors.primary} accessible={false} />
                  <Text style={styles.cameraBtnTitle}>{t('delivery.tap_sign', 'TAP TO GET RECEIVER SIGNATURE', locale)}</Text>
                  <Text style={styles.cameraBtnSub}>{t('delivery.tap_sign_sub', 'Receiver signs with finger on screen', locale)}</Text>
                </TouchableOpacity>
              )}
            </View>
          )}

          {/* Quick Remarks Chips (Zero Typing) */}
          <Text style={styles.sectionTitle}>{t('delivery.chips_title', 'QUICK STATUS', locale)}</Text>
          <View style={styles.chipsWrap}>
            {[
              { key: 'seal', label: t('delivery.chip_seal', 'Seal Intact', locale) },
              { key: 'ontime', label: t('delivery.chip_ontime', 'On-Time Unload', locale) },
              { key: 'gate', label: t('delivery.chip_gate', 'Verified by Gate', locale) },
              { key: 'payment', label: t('delivery.chip_payment', 'Payment Received', locale) },
            ].map((chip) => {
              const active = selectedChips.includes(chip.key);
              return (
                <TouchableOpacity
                  key={chip.key}
                  style={[styles.statusChip, active && styles.statusChipActive]}
                  onPress={() => toggleChip(chip.key)}
                  accessibilityRole="button"
                  accessibilityState={{ selected: active }}
                  accessibilityLabel={chip.label}
                  hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
                >
                  <MaterialCommunityIcons
                    name={active ? 'checkbox-marked-circle' : 'plus-circle-outline'}
                    size={14}
                    color={active ? Colors.primary : Colors.textSecondary}
                    accessible={false}
                  />
                  <Text style={[styles.statusChipText, active && styles.statusChipTextActive]}>
                    {chip.label}
                  </Text>
                </TouchableOpacity>
              );
            })}
          </View>

          {/* Collapsed Exception Toggle (Clean for 98% Normal Trips) */}
          <TouchableOpacity
            style={styles.exceptionToggle}
            onPress={() => setShowExceptions(!showExceptions)}
            accessibilityRole="button"
            accessibilityState={{ expanded: showExceptions }}
            accessibilityLabel={showExceptions ? t('delivery.a11y_exc_hide', 'Hide cargo issues', locale) : t('delivery.a11y_exc_show', 'Report cargo shortage or damage, optional', locale)}
            hitSlop={{ top: 6, bottom: 6, left: 6, right: 6 }}
          >
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
              <MaterialCommunityIcons name="alert-circle-outline" size={16} color={Colors.warningText} accessible={false} />
              <Text style={styles.exceptionToggleText}>
                {showExceptions ? t('delivery.exc_hide', 'Hide Cargo Issues', locale) : t('delivery.exc_show', '⚠️ Report Cargo Shortage or Damage (Optional)', locale)}
              </Text>
            </View>
            <MaterialCommunityIcons
              name={showExceptions ? 'chevron-up' : 'chevron-down'}
              size={18}
              color={Colors.warningText}
              accessible={false}
            />
          </TouchableOpacity>

          {showExceptions && (
            <View style={styles.exceptionsDrawer}>
              <View style={styles.rowInputs}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.fieldLabel}>{t('delivery.short_qty', 'Short Qty', locale)} ({t('delivery.qty_unit', 'TONS/BOXES', locale)})</Text>
                  <TextInput
                    style={styles.numberInput}
                    keyboardType="decimal-pad"
                    placeholder={t('delivery.placeholder_short', 'e.g. 2', locale)}
                    value={quantityShort}
                    onChangeText={setQuantityShort}
                    accessibilityLabel={t('delivery.a11y_short', 'Short quantity in tons or boxes', locale)}
                  />
                </View>
                <View style={{ width: 12 }} />
                <View style={{ flex: 1 }}>
                  <Text style={styles.fieldLabel}>{t('delivery.damage_qty', 'Damage Qty', locale)}</Text>
                  <TextInput
                    style={styles.numberInput}
                    keyboardType="decimal-pad"
                    placeholder={t('delivery.placeholder_damage', 'e.g. 1', locale)}
                    value={damageQty}
                    onChangeText={setDamageQty}
                    accessibilityLabel={t('delivery.a11y_damage', 'Damaged quantity', locale)}
                  />
                </View>
              </View>

              <Text style={[styles.fieldLabel, { marginTop: 10 }]}>{t('delivery.refusal_reason', 'Refusal Reason', locale)}</Text>
              <TextInput
                style={styles.textInput}
                placeholder={t('delivery.placeholder_reason', 'e.g. Broken seal, water leak, box crushed', locale)}
                value={refusalReason}
                onChangeText={setRefusalReason}
                accessibilityLabel={t('delivery.a11y_reason', 'Damage reason', locale)}
              />
            </View>
          )}

          {/* Big Confirm CTA */}
          <TouchableOpacity
            style={[styles.submitBtn, !isVerified && styles.submitBtnDimmed]}
            activeOpacity={0.88}
            onPress={submit}
            disabled={submitting}
            accessibilityRole="button"
            accessibilityLabel={t('delivery.a11y_confirm', 'Confirm delivery and close trip', locale)}
          >
            {submitting ? (
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                <ActivityIndicator color={Colors.textOnPrimary} size="small" />
                <Text style={styles.submitBtnText} accessibilityLiveRegion="polite">{t('delivery.confirming', 'Confirming…', locale)}</Text>
              </View>
            ) : (
              <>
                <MaterialCommunityIcons name="check-decagram" size={20} color={Colors.textOnPrimary} accessible={false} />
                <Text style={styles.submitBtnText}>{t('delivery.confirm', 'CONFIRM DELIVERY & CLOSE TRIP', locale)}</Text>
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
    backgroundColor: Colors.chrome,
  },
  header: {
    backgroundColor: Colors.chrome,
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
    fontSize: FontSize.titleLarge,
    fontWeight: '800',
    color: Colors.textOnPrimary,
  },
  headerSubtitle: {
    fontSize: FontSize.label,
    color: Colors.primary,
    fontWeight: '600',
  },
  readyPill: {
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: 9999,
    borderWidth: 1,
  },
  readyPillDone: {
    backgroundColor: Colors.success,
    borderColor: Colors.success,
  },
  readyPillPending: {
    backgroundColor: 'transparent',
    borderColor: Colors.textOnChromeMuted,
  },
  readyPillText: {
    fontSize: FontSize.small,
    fontWeight: '800',
    letterSpacing: 1,
  },
  readyPillTextDone: {
    color: Colors.textOnPrimary,
  },
  readyPillTextPending: {
    color: Colors.textOnChromeMuted,
  },
  progressStrip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingHorizontal: 16,
    paddingVertical: 8,
    backgroundColor: Colors.chrome,
  },
  progressTrack: {
    flex: 1,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.chromeBorder,
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    backgroundColor: Colors.accent,
    borderRadius: 3,
  },
  progressText: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.textOnChromeMuted,
  },
  body: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  scrollContent: {
    padding: 14,
    gap: 12,
  },
  consigneeCard: {
    backgroundColor: Colors.surface,
    borderRadius: 12,
    padding: 14,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
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
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.primary,
    letterSpacing: 0.5,
  },
  autoFilledTag: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.primary,
    backgroundColor: Colors.primarySubtle,
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 4,
  },
  consigneeNameText: {
    fontSize: FontSize.heading,
    fontWeight: '800',
    color: Colors.textPrimary,
  },
  consigneeSubText: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    marginTop: 2,
  },
  divider: {
    height: 1,
    backgroundColor: Colors.skeleton,
    marginVertical: 10,
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  metaText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  sectionTitle: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.textSecondary,
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
    backgroundColor: Colors.surface,
    paddingVertical: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  tabBtnActive: {
    backgroundColor: Colors.primarySubtle,
    borderColor: Colors.accent,
  },
  tabText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  tabTextActive: {
    color: Colors.primary,
    fontWeight: '800',
  },
  tabDoneDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.accent,
    position: 'absolute',
    top: 4,
    right: 4,
  },
  methodBox: {
    backgroundColor: Colors.surface,
    borderRadius: 12,
    padding: 16,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
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
    backgroundColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 10,
  },
  cameraBtnTitle: {
    fontSize: FontSize.body,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 0.5,
  },
  cameraBtnSub: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
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
    fontSize: FontSize.body,
    fontWeight: '800',
    color: Colors.primary,
  },
  retakePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    marginTop: 6,
  },
  retakeText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.primary,
  },
  otpPrompt: {
    fontSize: FontSize.body,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 8,
  },
  otpInput: {
    backgroundColor: Colors.surfaceSecondary,
    borderRadius: 10,
    paddingHorizontal: 20,
    paddingVertical: 10,
    fontSize: FontSize.display,
    fontWeight: '900',
    letterSpacing: 10,
    textAlign: 'center',
    width: 200,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
    color: Colors.textPrimary,
  },
  otpHint: {
    fontSize: FontSize.small,
    color: Colors.modalSub,
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
    backgroundColor: Colors.surfaceSecondary,
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
    backgroundColor: Colors.surface,
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  statusChipActive: {
    backgroundColor: Colors.primarySubtle,
    borderColor: Colors.accent,
  },
  statusChipText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
  statusChipTextActive: {
    color: Colors.primary,
  },
  exceptionToggle: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 8,
    paddingHorizontal: 10,
    backgroundColor: Colors.warningBg,
    borderRadius: 8,
  },
  exceptionToggleText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.warningText,
  },
  exceptionsDrawer: {
    backgroundColor: Colors.surface,
    borderRadius: 10,
    padding: 12,
    borderWidth: 1,
    borderColor: Colors.warningBg,
  },
  rowInputs: {
    flexDirection: 'row',
  },
  fieldLabel: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.textSecondary,
    marginBottom: 4,
  },
  numberInput: {
    backgroundColor: Colors.surfaceSecondary,
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 6,
    fontSize: FontSize.title,
    fontWeight: '800',
    color: Colors.textPrimary,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  textInput: {
    backgroundColor: Colors.surfaceSecondary,
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 8,
    fontSize: FontSize.body,
    color: Colors.textPrimary,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  submitBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    backgroundColor: Colors.primary,
    paddingVertical: 14,
    borderRadius: 10,
    marginTop: 8,
    elevation: 3,
  },
  submitBtnDimmed: {
    backgroundColor: Colors.primary,
    opacity: 0.9,
  },
  submitBtnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.body,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  cameraContainer: {
    flex: 1,
    backgroundColor: Colors.textPrimary,
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
    borderColor: Colors.accent,
    borderRadius: 12,
  },
  cameraGuideText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.label,
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
    borderColor: Colors.surface,
    alignItems: 'center',
    justifyContent: 'center',
  },
  captureInnerCircle: {
    width: 52,
    height: 52,
    borderRadius: 26,
    backgroundColor: Colors.accent,
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
    color: Colors.textOnPrimary,
    fontSize: FontSize.bodyLarge,
    textAlign: 'center',
    marginBottom: 16,
  },
  primaryActionBtn: {
    backgroundColor: Colors.primary,
    paddingHorizontal: 20,
    paddingVertical: 12,
    borderRadius: 8,
  },
  primaryActionBtnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.body,
    fontWeight: '800',
  },
  signaturePadContainer: {
    flex: 1,
    backgroundColor: Colors.surface,
    padding: 16,
  },
  signatureHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 12,
  },
  signatureHeaderText: {
    fontSize: FontSize.bodyLarge,
    fontWeight: '800',
    color: Colors.textPrimary,
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
    backgroundColor: Colors.skeleton,
  },
  secBtnText: {
    fontSize: FontSize.label,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
});
