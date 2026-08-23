import React, { useState } from 'react';
import { Alert, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { useTranslation } from 'react-i18next';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { buildExpenseDraft, parseExpenseUtterance } from '../services/speech';
import { OfflineQueue } from '../services/offlineQueue';

interface VoiceExpenseButtonProps {
  tripId?: string | null;
  onSaved?: () => void;
  disabled?: boolean;
}

export function VoiceExpenseButton({ tripId, onSaved, disabled = false }: VoiceExpenseButtonProps) {
  const { t } = useTranslation();
  const [panelOpen, setPanelOpen] = useState(false);
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);

  const handleConfirm = async () => {
    const trimmed = text.trim();
    if (!trimmed || saving) return;
    setSaving(true);
    try {
      const parsed = parseExpenseUtterance(trimmed, new Date());
      const draft = buildExpenseDraft(trimmed, tripId ?? '', new Date());
      await OfflineQueue.enqueueExpense(draft);
      Alert.alert(
        t('expense.title'),
        `${t('voice.parsed_amount')}: ${draft.amount} · ${t('voice.parsed_vendor')}: ${parsed.vendor ?? '-'}`
      );
      setText('');
      setPanelOpen(false);
      onSaved?.();
    } catch {
      Alert.alert(t('expense.title'), 'Could not save expense. Please try again.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <View
      style={[styles.wrapper, disabled && styles.disabledWrapper]}
      pointerEvents={disabled ? 'none' : 'auto'}
    >
      <TouchableOpacity
        style={styles.micBtn}
        onPress={() => setPanelOpen((o) => !o)}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel="voice-expense-mic"
      >
        <MaterialCommunityIcons name="microphone" size={26} color={Colors.textOnPrimary} />
      </TouchableOpacity>

      {panelOpen && !disabled && (
        <View style={styles.panel}>
          <TextInput
            style={styles.input}
            placeholder={t('voice.hint')}
            placeholderTextColor={Colors.textMuted}
            value={text}
            onChangeText={setText}
            multiline
          />
          <TouchableOpacity
            style={[styles.confirmBtn, saving && { opacity: 0.6 }]}
            onPress={handleConfirm}
            disabled={saving}
            accessibilityRole="button"
            accessibilityLabel="voice-expense-confirm"
          >
            <Text style={styles.confirmText}>{t('expense.submit')}</Text>
          </TouchableOpacity>
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrapper: {
    marginTop: 8,
  },
  disabledWrapper: {
    opacity: 0.4,
  },
  micBtn: {
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
  },
  panel: {
    marginTop: Spacing.sm,
    padding: Spacing.md,
    borderRadius: Radius.sm,
    backgroundColor: Colors.surface,
    borderWidth: 1,
    borderColor: Colors.borderLight,
    gap: Spacing.sm,
  },
  input: {
    minHeight: 40,
    borderRadius: Radius.sm,
    borderWidth: 1,
    borderColor: Colors.border,
    paddingHorizontal: Spacing.sm,
    paddingVertical: Spacing.xs,
    color: Colors.textPrimary,
    fontSize: 12,
  },
  confirmBtn: {
    backgroundColor: Colors.primary,
    paddingVertical: 10,
    borderRadius: Radius.sm,
    alignItems: 'center',
  },
  confirmText: {
    color: Colors.textOnPrimary,
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
});
