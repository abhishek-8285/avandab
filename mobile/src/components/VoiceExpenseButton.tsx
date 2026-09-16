import React, { useState } from 'react';
import { Alert, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { t } from '../i18n';
import { useLanguageStore } from '../stores/languageStore';
import { Colors, Font, Radius, Spacing, FontSize} from '../constants/theme';
import { buildExpenseDraft, parseExpenseUtterance } from '../services/speech';
import { OfflineQueue } from '../services/offlineQueue';

interface VoiceExpenseButtonProps {
  tripId?: string | null;
  onSaved?: () => void;
  disabled?: boolean;
}

export function VoiceExpenseButton({ tripId, onSaved, disabled = false }: VoiceExpenseButtonProps) {
  const locale = useLanguageStore((s) => s.locale);
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
        t('expense.title', 'Log Expense', locale),
        `${t('voice.parsed_amount', 'Amount detected', locale)}: ${draft.amount} · ${t('voice.parsed_vendor', 'Vendor detected', locale)}: ${parsed.vendor ?? '-'}`
      );
      setText('');
      setPanelOpen(false);
      onSaved?.();
    } catch {
      Alert.alert(t('expense.title', 'Log Expense', locale), t('expense.save_failed', 'Could not save expense. Please try again.', locale));
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
        accessibilityLabel={panelOpen ? t('voice.a11y_close', 'Close voice expense entry', locale) : t('voice.a11y_open', 'Add expense by voice', locale)}
        accessibilityState={{ expanded: panelOpen }}
      >
        <MaterialCommunityIcons name="microphone" size={26} color={Colors.textOnPrimary} accessible={false} />
      </TouchableOpacity>

      {panelOpen && !disabled && (
        <View style={styles.panel}>
          <TextInput
            style={styles.input}
            placeholder={t('voice.hint', 'Say your expense, e.g. "Diesel ₹2500 at HPCL"', locale)}
            placeholderTextColor={Colors.textMuted}
            value={text}
            onChangeText={setText}
            multiline
            accessibilityLabel={t('voice.a11y_input', 'Voice expense description', locale)}
          />
          <TouchableOpacity
            style={[styles.confirmBtn, saving && { opacity: 0.6 }]}
            onPress={handleConfirm}
            disabled={saving}
            accessibilityRole="button"
            accessibilityLabel={t('voice.a11y_save', 'Save voice expense', locale)}
            accessibilityState={{ disabled: saving }}
          >
            {saving ? (
              <Text style={styles.confirmText} accessibilityLiveRegion="polite">{t('voice.saving', 'Saving…', locale)}</Text>
            ) : (
              <Text style={styles.confirmText}>{t('expense.submit', 'Submit Expense', locale)}</Text>
            )}
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
    fontSize: FontSize.body,
  },
  confirmBtn: {
    backgroundColor: Colors.primary,
    paddingVertical: 10,
    borderRadius: Radius.sm,
    alignItems: 'center',
  },
  confirmText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.label,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
});
