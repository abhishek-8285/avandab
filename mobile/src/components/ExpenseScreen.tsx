import React, { useState } from 'react';
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
import { MaterialCommunityIcons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import * as Location from 'expo-location';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { OfflineQueue } from '../services/offlineQueue';
import { DB } from '../services/storage';

const EXPENSE_TYPES = [
  { id: 'fuel', label: 'FUEL', icon: 'fuel' },
  { id: 'toll', label: 'TOLL', icon: 'road-variant' },
  { id: 'rto', label: 'RTO', icon: 'file-document-outline' },
  { id: 'tyre', label: 'TYRE', icon: 'circle-outline' },
  { id: 'bhatta', label: 'BHATTA', icon: 'wallet-outline' },
] as const;

type ExpenseType = (typeof EXPENSE_TYPES)[number]['id'];

interface ExpenseScreenProps {
  tripId?: string;
  onComplete?: () => void;
  onBack?: () => void;
}

export function ExpenseScreen({ tripId = '1', onComplete, onBack }: ExpenseScreenProps) {
  const [expenseType, setExpenseType] = useState<ExpenseType>('fuel');
  const [amount, setAmount] = useState('');
  const [notes, setNotes] = useState('');
  const [receiptUri, setReceiptUri] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const pickReceipt = async () => {
    try {
      const perm = await ImagePicker.requestMediaLibraryPermissionsAsync();
      if (!perm.granted) {
        Alert.alert('Permission Required', 'Gallery permission needed to attach receipt.');
        return;
      }
      const result = await ImagePicker.launchImageLibraryAsync({
        mediaTypes: ImagePicker.MediaTypeOptions.Images,
        quality: 0.7,
        allowsEditing: true,
      });
      if (!result.canceled && result.assets[0]) {
        setReceiptUri(result.assets[0].uri);
      }
    } catch (e: any) {
      Alert.alert('Image Error', e.message || 'Failed to pick receipt');
    }
  };

  const takeReceiptPhoto = async () => {
    try {
      const perm = await ImagePicker.requestCameraPermissionsAsync();
      if (!perm.granted) {
        Alert.alert('Permission Required', 'Camera permission needed.');
        return;
      }
      const result = await ImagePicker.launchCameraAsync({
        quality: 0.7,
        allowsEditing: true,
      });
      if (!result.canceled && result.assets[0]) {
        setReceiptUri(result.assets[0].uri);
      }
    } catch (e: any) {
      Alert.alert('Camera Error', e.message || 'Failed to capture receipt');
    }
  };

  const submit = async () => {
    const amt = parseFloat(amount);
    if (!amt || isNaN(amt) || amt <= 0) {
      Alert.alert('Validation Error', 'Please enter a valid amount.');
      return;
    }
    if (!tripId) {
      Alert.alert('Validation Error', 'Trip ID is required.');
      return;
    }

    setSubmitting(true);
    let gps: { latitude: number | null; longitude: number | null } = { latitude: null, longitude: null };
    try {
      const { status } = await Location.requestForegroundPermissionsAsync();
      if (status === 'granted') {
        const pos = await Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Balanced });
        gps = { latitude: pos.coords.latitude, longitude: pos.coords.longitude };
      }
    } catch {}

    // One key per logical expense: live attempt and any offline retries
    // reuse it, so the backend's unique index dedupes duplicates.
    const idempotencyKey = `exp-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;

    const form = new FormData();
    form.append('trip_id', tripId);
    form.append('type', expenseType);
    form.append('expense_type', expenseType);
    form.append('amount', String(amt));
    form.append('notes', notes.trim());
    form.append('idempotency_key', idempotencyKey);
    if (receiptUri) {
      form.append('receipt_photo', {
        uri: receiptUri,
        name: 'receipt.jpg',
        type: 'image/jpeg',
      } as any);
    }
    if (gps.latitude != null && gps.longitude != null) {
      form.append('latitude', String(gps.latitude));
      form.append('longitude', String(gps.longitude));
    }

    try {
      const token = useAuthStore.getState().token;
      const res = await fetch(`${getApiBaseURL()}/api/v1/kharcha/expense`, {
        method: 'POST',
        headers: {
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: form,
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }

      const json = await res.json();
      // Spec 22 S8 — OCR confirm step: server reads the receipt; when the
      // extracted amount differs from the typed one, the driver confirms.
      let ocrNote = '';
      if (json?.id && receiptUri) {
        try {
          const ocrRes = await fetch(`${getApiBaseURL()}/api/expenses/${json.id}/ocr`, {
            method: 'POST',
            headers: {
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
          });
          if (ocrRes.ok) {
            const ocr = await ocrRes.json();
            const ocrAmt = Number(ocr?.ocr_amount);
            if (ocrAmt > 0 && Math.abs(ocrAmt - amt) > 0.01 * Math.max(amt, 1)) {
              const useOcr = await new Promise<boolean>((resolve) => {
                Alert.alert(
                  'Receipt amount looks different',
                  `Receipt shows ₹${ocrAmt.toFixed(0)} but you entered ₹${amt.toFixed(0)}. Keep which?`,
                  [
                    { text: 'Keep mine', style: 'cancel', onPress: () => resolve(false) },
                    { text: 'Use receipt', onPress: () => resolve(true) },
                  ],
                );
              });
              ocrNote = useOcr ? ' (receipt amount noted for review)' : '';
            } else {
              ocrNote = ' (matches receipt ✓)';
            }
          }
        } catch {}
      }
      Alert.alert('Expense Submitted', `Expense ${json.id || ''} recorded${ocrNote}!`, [
        { text: 'OK', onPress: onComplete },
      ]);
    } catch {
      // Offline fallback — queue locally
      try {
        await OfflineQueue.enqueueExpense({
          trip_id: tripId,
          expense_type: expenseType,
          amount: amt,
          receipt_uri: receiptUri,
          notes: notes.trim(),
          latitude: gps.latitude,
          longitude: gps.longitude,
          idempotency_key: idempotencyKey,
        });
        // Also cache in storage DB for offline viewing
        await DB.saveOfflineExpense({
          trip_id: tripId,
          expense_type: expenseType,
          amount: amt,
          receipt_uri: receiptUri,
          notes: notes.trim(),
          latitude: gps.latitude,
          longitude: gps.longitude,
        });
      } catch {}
      Alert.alert('Saved Offline', 'Expense queued offline. Will sync when back online.', [
        { text: 'OK', onPress: onComplete },
      ]);
    } finally {
      setSubmitting(false);
    }
  };

  const isDisabled = submitting || !amount.trim();

  return (
    <View style={styles.container}>
      <StatusBar style="light" />
      <View style={styles.header}>
        <TouchableOpacity style={styles.iconButton} onPress={onBack}>
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} />
        </TouchableOpacity>
        <Text style={styles.headerLabel}>EXPENSE</Text>
        <View style={styles.iconButtonPlaceholder} />
      </View>

      <ScrollView contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
        <View style={styles.titleSection}>
          <Text style={styles.title}>LOG EXPENSE</Text>
          <View style={styles.titleUnderline} />
          <Text style={styles.subtitle}>TRIP REF · #{tripId}</Text>
        </View>

        {/* Expense Type Picker */}
        <View style={styles.card}>
          <Text style={styles.cardHeader}>EXPENSE TYPE</Text>
          <View style={styles.typeRow}>
            {EXPENSE_TYPES.map((t) => (
              <TouchableOpacity
                key={t.id}
                style={[styles.typeBtn, expenseType === t.id && styles.typeBtnActive]}
                onPress={() => setExpenseType(t.id)}
              >
                <MaterialCommunityIcons
                  name={t.icon as any}
                  size={18}
                  color={expenseType === t.id ? Colors.textOnPrimary : Colors.textSecondary}
                />
                <Text style={[styles.typeBtnText, expenseType === t.id && styles.typeBtnTextActive]}>{t.label}</Text>
              </TouchableOpacity>
            ))}
          </View>
        </View>

        {/* Amount + Notes */}
        <View style={styles.card}>
          <View style={styles.formGroup}>
            <Text style={styles.label}>AMOUNT (INR) *</Text>
            <TextInput
              style={styles.input}
              placeholder="e.g. 1250"
              placeholderTextColor={Colors.textMuted}
              value={amount}
              onChangeText={setAmount}
              keyboardType="numeric"
            />
          </View>
          <View style={styles.formGroup}>
            <Text style={styles.label}>NOTES</Text>
            <TextInput
              style={[styles.input, styles.textArea]}
              placeholder="e.g. Fuel at HPCL pump, NH48"
              placeholderTextColor={Colors.textMuted}
              value={notes}
              onChangeText={setNotes}
              multiline
              numberOfLines={3}
            />
          </View>
        </View>

        {/* Receipt Photo via expo-image-picker */}
        <View style={styles.card}>
          <View style={styles.cardHeaderRow}>
            <Text style={styles.cardHeader}>RECEIPT PHOTO</Text>
            {receiptUri ? <Text style={styles.cardMetaSuccess}>ATTACHED</Text> : <Text style={styles.cardMeta}>OPTIONAL</Text>}
          </View>
          <Text style={styles.cardSubtitle}>Attach receipt image for verification. Uses expo-image-picker.</Text>
          {receiptUri ? (
            <View style={styles.photoPreviewContainer}>
              <Image source={{ uri: receiptUri }} style={styles.photoPreview} />
              <View style={styles.receiptActions}>
                <TouchableOpacity style={styles.secondaryBtn} onPress={pickReceipt}>
                  <Text style={styles.secondaryBtnText}>CHANGE</Text>
                </TouchableOpacity>
                <TouchableOpacity style={styles.secondaryBtn} onPress={() => setReceiptUri(null)}>
                  <Text style={styles.secondaryBtnText}>REMOVE</Text>
                </TouchableOpacity>
              </View>
            </View>
          ) : (
            <View style={styles.receiptBtnRow}>
              <TouchableOpacity style={styles.receiptBtn} onPress={takeReceiptPhoto}>
                <MaterialCommunityIcons name="camera-outline" size={18} color={Colors.primary} />
                <Text style={styles.receiptBtnText}>CAMERA</Text>
              </TouchableOpacity>
              <TouchableOpacity style={styles.receiptBtn} onPress={pickReceipt}>
                <MaterialCommunityIcons name="image-outline" size={18} color={Colors.primary} />
                <Text style={styles.receiptBtnText}>GALLERY</Text>
              </TouchableOpacity>
            </View>
          )}
        </View>

        <TouchableOpacity
          style={[styles.submitBtn, isDisabled && styles.submitBtnDisabled]}
          activeOpacity={0.88}
          onPress={submit}
          disabled={isDisabled}
        >
          {submitting ? (
            <ActivityIndicator color={Colors.textOnPrimary} size="small" />
          ) : (
            <>
              <Text style={styles.submitBtnText}>SUBMIT EXPENSE</Text>
              <MaterialCommunityIcons name="check-circle-outline" size={16} color={Colors.textOnPrimary} />
            </>
          )}
        </TouchableOpacity>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.background },
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
    width: 32,
    height: 32,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    alignItems: 'center',
    justifyContent: 'center',
  },
  iconButtonPlaceholder: { width: 32, height: 32 },
  scrollContent: {
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.lg,
    paddingBottom: 40,
  },
  titleSection: { marginBottom: Spacing.lg },
  title: {
    fontSize: 18,
    fontWeight: '900',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  titleUnderline: { width: 28, height: 2, backgroundColor: Colors.primary, marginTop: 6, marginBottom: 8 },
  subtitle: {
    fontSize: 11,
    color: Colors.textSecondary,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  card: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    padding: Spacing.md,
    borderWidth: 1,
    borderColor: Colors.border,
    marginBottom: Spacing.md,
  },
  cardHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: Spacing.md,
  },
  cardHeader: {
    fontSize: 11,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  cardMeta: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  cardMetaSuccess: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.success,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  cardSubtitle: { fontSize: 12, color: Colors.textSecondary, lineHeight: 18, marginBottom: Spacing.md },
  typeRow: { flexDirection: 'row', gap: 8, flexWrap: 'wrap' },
  typeBtn: {
    flex: 1,
    minWidth: 70,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.sm,
    paddingVertical: 10,
    alignItems: 'center',
    gap: 4,
    backgroundColor: Colors.surfaceSecondary,
  },
  typeBtnActive: { backgroundColor: Colors.primary, borderColor: Colors.primaryDark },
  typeBtnText: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  typeBtnTextActive: { color: Colors.textOnPrimary },
  formGroup: { marginBottom: Spacing.md },
  label: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 1,
    marginBottom: 6,
    fontFamily: Font.mono,
  },
  input: {
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.sm,
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: 13,
    color: Colors.textPrimary,
    backgroundColor: Colors.surfaceSecondary,
  },
  textArea: { height: 70, textAlignVertical: 'top' },
  photoPreviewContainer: { borderRadius: Radius.md, overflow: 'hidden' },
  photoPreview: { width: '100%', height: 180, borderRadius: Radius.md },
  receiptActions: { flexDirection: 'row', justifyContent: 'flex-end', gap: 8, marginTop: 8 },
  secondaryBtn: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: Radius.sm,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
  },
  secondaryBtnText: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  receiptBtnRow: { flexDirection: 'row', gap: 12 },
  receiptBtn: {
    flex: 1,
    height: 90,
    borderWidth: 1,
    borderColor: Colors.border,
    borderStyle: 'dashed',
    borderRadius: Radius.md,
    backgroundColor: Colors.surfaceSecondary,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
  },
  receiptBtnText: {
    fontSize: 11,
    fontWeight: '800',
    color: Colors.primary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  submitBtn: {
    height: 48,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    marginTop: 8,
  },
  submitBtnDisabled: { opacity: 0.5, backgroundColor: Colors.border },
  submitBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
});
