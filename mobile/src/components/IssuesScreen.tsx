import React, { useCallback, useEffect, useState } from 'react';
import { StyleSheet, Text, View, TouchableOpacity, ScrollView, TextInput, ActivityIndicator, Alert } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

const CATEGORIES = [
  { id: 'vehicle', label: 'VEHICLE', icon: 'truck-alert' },
  { id: 'road', label: 'ROAD', icon: 'road-variant' },
  { id: 'cargo', label: 'CARGO', icon: 'package-variant-closed' },
  { id: 'customer', label: 'CUSTOMER', icon: 'account-alert' },
  { id: 'accident', label: 'ACCIDENT', icon: 'car-crash' },
  { id: 'other', label: 'OTHER', icon: 'dots-horizontal' },
] as const;

const SEVERITIES = ['low', 'medium', 'high', 'critical'] as const;

interface IssueRow {
  id: string;
  trip_id?: string;
  category: string;
  severity: string;
  message: string;
  photo_url?: string;
  status: string;
  created_at: string;
}

interface IssuesScreenProps {
  tripId?: string;
  onBack: () => void;
}

async function fetchIssuesList(token: string | null): Promise<IssueRow[] | null> {
  try {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/issues`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (res.ok) {
      const json = await res.json();
      return json.issues ?? [];
    }
  } catch {
    // offline — list stays stale
  }
  return null;
}

export function IssuesScreen({ tripId, onBack }: IssuesScreenProps) {
  const { token } = useAuthStore();
  const [category, setCategory] = useState<string>('vehicle');
  const [severity, setSeverity] = useState<string>('low');
  const [message, setMessage] = useState('');
  const [photoUri, setPhotoUri] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [issues, setIssues] = useState<IssueRow[]>([]);
  const [loadingList, setLoadingList] = useState(true);

  const loadIssues = useCallback(async () => {
    const rows = await fetchIssuesList(token);
    if (rows !== null) setIssues(rows);
    setLoadingList(false);
  }, [token]);

  useEffect(() => {
    void (async () => {
      const rows = await fetchIssuesList(token);
      if (rows !== null) setIssues(rows);
      setLoadingList(false);
    })();
  }, [token]);

  const pickPhoto = async () => {
    const perm = await ImagePicker.requestCameraPermissionsAsync();
    if (!perm.granted) {
      Alert.alert('Permission Required', 'Camera permission needed for issue photos.');
      return;
    }
    const result = await ImagePicker.launchCameraAsync({ quality: 0.7 });
    if (!result.canceled && result.assets[0]) setPhotoUri(result.assets[0].uri);
  };

  const submit = async () => {
    if (!message.trim()) {
      Alert.alert('Missing', 'Describe the issue first.');
      return;
    }
    setSubmitting(true);
    try {
      const form = new FormData();
      form.append('message', message.trim());
      form.append('category', category);
      form.append('severity', severity);
      if (tripId) form.append('trip_id', tripId);
      if (photoUri) {
        form.append('photo', { uri: photoUri, name: 'issue.jpg', type: 'image/jpeg' } as any);
      }
      const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/issues`, {
        method: 'POST',
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        body: form,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      setMessage('');
      setPhotoUri(null);
      Alert.alert('Reported', 'Issue submitted to operations.');
      loadIssues();
    } catch (e: any) {
      Alert.alert('Failed', e?.message || 'Could not submit issue.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <View style={styles.container}>
      <StatusBar style="light" />
      <View style={styles.header}>
        <TouchableOpacity style={styles.iconBtn} onPress={onBack}>
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} />
        </TouchableOpacity>
        <Text style={styles.headerLabel}>REPORT ISSUE</Text>
        <View style={{ width: 32 }} />
      </View>

      <ScrollView contentContainerStyle={styles.content} showsVerticalScrollIndicator={false}>
        <Text style={styles.sectionLabel}>CATEGORY</Text>
        <View style={styles.chipWrap}>
          {CATEGORIES.map((c) => (
            <TouchableOpacity
              key={c.id}
              style={[styles.chip, category === c.id && styles.chipActive]}
              onPress={() => setCategory(c.id)}
            >
              <MaterialCommunityIcons
                name={c.icon as any}
                size={13}
                color={category === c.id ? Colors.textOnPrimary : Colors.textSecondary}
              />
              <Text style={[styles.chipText, category === c.id && styles.chipTextActive]}>{c.label}</Text>
            </TouchableOpacity>
          ))}
        </View>

        <Text style={styles.sectionLabel}>SEVERITY</Text>
        <View style={styles.chipWrap}>
          {SEVERITIES.map((s) => (
            <TouchableOpacity
              key={s}
              style={[styles.chip, severity === s && styles.chipActive]}
              onPress={() => setSeverity(s)}
            >
              <Text style={[styles.chipText, severity === s && styles.chipTextActive]}>{s.toUpperCase()}</Text>
            </TouchableOpacity>
          ))}
        </View>

        <Text style={styles.sectionLabel}>WHAT HAPPENED?</Text>
        <TextInput
          style={styles.input}
          placeholder="Describe the issue…"
          placeholderTextColor={Colors.textMuted}
          value={message}
          onChangeText={setMessage}
          multiline
          numberOfLines={4}
        />

        <TouchableOpacity style={styles.photoBtn} onPress={pickPhoto}>
          <MaterialCommunityIcons name="camera-plus" size={15} color={Colors.primary} />
          <Text style={styles.photoBtnText}>{photoUri ? 'PHOTO ATTACHED ✓' : 'ATTACH PHOTO (OPTIONAL)'}</Text>
        </TouchableOpacity>

        <TouchableOpacity style={styles.submitBtn} onPress={submit} disabled={submitting}>
          {submitting ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <Text style={styles.submitText}>SUBMIT TO OPERATIONS</Text>
          )}
        </TouchableOpacity>

        {tripId && <Text style={styles.tripRef}>Linked to trip #{tripId}</Text>}

        <Text style={styles.sectionLabel}>YOUR RECENT ISSUES</Text>
        {loadingList ? (
          <ActivityIndicator color={Colors.primary} />
        ) : issues.length === 0 ? (
          <Text style={styles.empty}>Nothing reported yet.</Text>
        ) : (
          issues.map((i) => (
            <View key={i.id} style={styles.issueRow}>
              <View style={{ flex: 1 }}>
                <Text style={styles.issueMsg} numberOfLines={2}>{i.message}</Text>
                <Text style={styles.issueMeta}>
                  {i.category.toUpperCase()} · {i.severity.toUpperCase()} · {i.status.toUpperCase()}
                </Text>
              </View>
            </View>
          ))
        )}
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
  iconBtn: {
    width: 32,
    height: 32,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    alignItems: 'center',
    justifyContent: 'center',
  },
  content: { padding: Spacing.lg, gap: Spacing.sm },
  sectionLabel: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: Spacing.sm,
  },
  chipWrap: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: 9999,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
  },
  chipActive: { backgroundColor: Colors.primary, borderColor: Colors.primary },
  chipText: {
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 0.5,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
  },
  chipTextActive: { color: Colors.textOnPrimary },
  input: {
    backgroundColor: Colors.surface,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.md,
    padding: Spacing.md,
    minHeight: 90,
    textAlignVertical: 'top',
    color: Colors.textPrimary,
    fontSize: 13,
  },
  photoBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingVertical: 12,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
    justifyContent: 'center',
  },
  photoBtnText: {
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1,
    color: Colors.primary,
    fontFamily: Font.mono,
  },
  submitBtn: {
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    paddingVertical: 14,
    alignItems: 'center',
  },
  submitText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  tripRef: {
    fontSize: 10,
    color: Colors.textMuted,
    textAlign: 'center',
    fontFamily: Font.mono,
  },
  empty: { fontSize: 12, color: Colors.textMuted },
  issueRow: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.sm,
    borderWidth: 1,
    borderColor: Colors.borderLight,
    padding: Spacing.md,
  },
  issueMsg: { fontSize: 12, color: Colors.textPrimary, fontWeight: '600' },
  issueMeta: {
    fontSize: 9,
    color: Colors.textMuted,
    fontFamily: Font.mono,
    marginTop: 3,
    letterSpacing: 0.5,
  },
});
