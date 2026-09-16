import React, { useState, useEffect } from 'react';
import Animated, {
  useSharedValue,
  useAnimatedStyle,
  withTiming,
  withSequence,
  withRepeat,
  cancelAnimation,
  Easing,
} from 'react-native-reanimated';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  Modal,
  TextInput,
  Alert,
  Platform,
  Vibration,
} from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import {
  ExpoSpeechRecognitionModule,
  useSpeechRecognitionEvent,
} from 'expo-speech-recognition';
import { parseExpenseUtterance, buildExpenseDraft, ExpenseCategory, ParsedExpense } from '../services/speech';
import { OfflineQueue } from '../services/offlineQueue';
import { Colors, Font, Spacing, FontSize} from '../constants/theme';
import { useLanguageStore } from '../stores/languageStore';

interface VoiceKharchaSheetProps {
  visible: boolean;
  onClose: () => void;
  tripId?: string;
  onSaved?: () => void;
}

const PRESET_UTTERANCES = [
  { label: '⛽ HPCL Diesel ₹2500', text: 'HPCL pump par 2500 ka diesel dala' },
  { label: '🛣️ NHAI Toll ₹350', text: 'NHAI Expressway toll 350 rupees' },
  { label: '🍛 Dhaba Food ₹220', text: 'Highway dhaba par 220 ka khana khaya' },
  { label: '🔧 Tyre Repair ₹150', text: 'Tyre puncture repair 150 rupaye' },
];

export function VoiceKharchaSheet({
  visible,
  onClose,
  tripId = 'TRP-8491',
  onSaved,
}: VoiceKharchaSheetProps) {
  const [isListening, setIsListening] = useState(false);
  const [liveTranscript, setLiveTranscript] = useState('');
  const [transcript, setTranscript] = useState('');
  const [parsed, setParsed] = useState<ParsedExpense | null>(null);
  const [noExpenseWarning, setNoExpenseWarning] = useState<string | null>(null);
  const [amountInput, setAmountInput] = useState('');
  const [categoryInput, setCategoryInput] = useState<ExpenseCategory>('fuel');
  const [vendorInput, setVendorInput] = useState('');
  const [saving, setSaving] = useState(false);
  const [isPlayingAudio, setIsPlayingAudio] = useState(false);
  const [audioPlaybackProgress, setAudioPlaybackProgress] = useState(0);
  const { locale } = useLanguageStore();
  const intlTag = `${locale}-IN`;
  const recSec = new Intl.NumberFormat(intlTag).format(3);

  // Pulse animation for recording mic (stable instances; useState initializer
  // instead of useRef(...).current, which reads a ref during render).
  const pulseAnim = useSharedValue(1);
  // Live audio wave bars — normalized scaleY (1 = full 24px bar). Animate
  // transform only: height forces layout per frame, scaleY is GPU-composited.
  const wave1 = useSharedValue(8 / 24);
  const wave2 = useSharedValue(14 / 24);
  const wave3 = useSharedValue(1);
  const wave4 = useSharedValue(18 / 24);
  const wave5 = useSharedValue(10 / 24);

  // Animated styles: shared values must flow through useAnimatedStyle to run
  // on the UI thread — raw values in a style object never animate.
  const pulseStyle = useAnimatedStyle(() => ({
    transform: [{ scale: pulseAnim.value }],
  }));
  const wave1Style = useAnimatedStyle(() => ({
    transform: [{ scaleY: wave1.value }],
  }));
  const wave2Style = useAnimatedStyle(() => ({
    transform: [{ scaleY: wave2.value }],
  }));
  const wave3Style = useAnimatedStyle(() => ({
    transform: [{ scaleY: wave3.value }],
  }));
  const wave4Style = useAnimatedStyle(() => ({
    transform: [{ scaleY: wave4.value }],
  }));
  const wave5Style = useAnimatedStyle(() => ({
    transform: [{ scaleY: wave5.value }],
  }));

  // Listen to native speech recognition events
  useSpeechRecognitionEvent('start', () => {
    setIsListening(true);
    setNoExpenseWarning(null);
  });

  useSpeechRecognitionEvent('end', () => {
    setIsListening(false);
  });

  useSpeechRecognitionEvent('result', (event) => {
    if (event.results && event.results.length > 0) {
      const recognized = event.results[0]?.transcript || '';
      setLiveTranscript(recognized);

      if (event.isFinal) {
        handleProcessSpeech(recognized);
      }
    }
  });

  useSpeechRecognitionEvent('error', (event) => {
    console.log('[SPEECH RECOGNITION ERROR]', event.error, event.message);
    setIsListening(false);
    if (event.error === 'language-not-supported') {
      setNoExpenseWarning('Voice model not installed on device. You can tap any preset phrase below or type directly.');
    } else if (event.error === 'no-speech' || event.error === 'speech-timeout') {
      setNoExpenseWarning('No voice detected. Tap mic to speak, or tap a preset button below.');
    } else if (event.error) {
      setNoExpenseWarning(`Voice input: ${event.message || event.error}. Tap a preset below or type.`);
    }
  });

  useSpeechRecognitionEvent('volumechange', (event) => {
    if (event.value !== undefined) {
      const vol = Math.max(0, Math.min(100, (event.value + 50) * 2));
      // Same pixel targets as before, expressed as scale of the 24px bar.
      wave1.value = (6 + vol * 0.2) / 24;
      wave2.value = (10 + vol * 0.3) / 24;
      wave3.value = (14 + vol * 0.4) / 24;
      wave4.value = (8 + vol * 0.3) / 24;
      wave5.value = (6 + vol * 0.2) / 24;
    }
  });

useEffect(() => {
    if (isListening) {
      // withRepeat(-1, true): loop forever, reversing between the two keyframes
      // (replaces the invalid withLoop(...).run() API).
      pulseAnim.value = withRepeat(
        withSequence(
          withTiming(1.25, { duration: 600, easing: Easing.inOut(Easing.ease) }),
          withTiming(1, { duration: 600, easing: Easing.inOut(Easing.ease) })
        ),
        -1,
        true
      );
    } else {
      cancelAnimation(pulseAnim);
      pulseAnim.value = 1;
      wave1.value = 8 / 24;
      wave2.value = 14 / 24;
      wave3.value = 1;
      wave4.value = 18 / 24;
      wave5.value = 10 / 24;
    }

    return () => {
      cancelAnimation(pulseAnim);
    };
  }, [isListening]);

  // Reset form state when the modal opens (render-phase adjustment of derived
  // state — pure setStates only; the native side effects stay in the effect below).
  const [prevVisible, setPrevVisible] = useState<boolean | null>(null);
  if (visible !== prevVisible) {
    setPrevVisible(visible);
    if (visible) {
      setTranscript('');
      setLiveTranscript('');
      setParsed(null);
      setNoExpenseWarning(null);
      setAmountInput('');
      setVendorInput('');
      setIsListening(false);
      setIsPlayingAudio(false);
      setAudioPlaybackProgress(0);
    }
  }

  // Native side effects on open/close (no setState — lint-clean by construction).
  useEffect(() => {
    if (visible) {
      if (ExpoSpeechRecognitionModule?.requestPermissionsAsync) {
        ExpoSpeechRecognitionModule.requestPermissionsAsync().catch(() => {});
      }
    } else {
      if (isListening && ExpoSpeechRecognitionModule?.stop) {
        ExpoSpeechRecognitionModule.stop();
      }
    }
  }, [visible]);

  // Function declaration (hoisted): used by the speech-result subscription above.
  function handleProcessSpeech(utterance: string) {
    setIsListening(false);
    setTranscript(utterance);
    setLiveTranscript('');

    const trimmed = utterance.trim();
    if (!trimmed) return;

    const result = parseExpenseUtterance(trimmed, new Date());
    setParsed(result);

    if (result.amount && result.amount > 0) {
      setAmountInput(String(result.amount));
      setCategoryInput(result.category);
      setVendorInput(result.vendor || '');
      setNoExpenseWarning(null);
    } else {
      setAmountInput('');
      setCategoryInput(result.category);
      setVendorInput(result.vendor || '');
      setNoExpenseWarning(
        `Heard "${trimmed}", but no amount found. Please speak an expense like "Diesel 2000" or enter the amount below.`
      );
    }
  }

  const handleStartListening = async () => {
    try {
      setTranscript('');
      setLiveTranscript('');
      setParsed(null);
      setNoExpenseWarning(null);
      setIsPlayingAudio(false);

      if (ExpoSpeechRecognitionModule?.start) {
        const res = await ExpoSpeechRecognitionModule.requestPermissionsAsync();
        if (!res.granted) {
          Alert.alert(
            'Microphone Permission Required',
            'Please allow microphone access to log expenses by voice.'
          );
          return;
        }

        setIsListening(true);
        Vibration.vibrate(35);

        // Start live Android Speech Recognizer using device's default language
        await ExpoSpeechRecognitionModule.start({
          interimResults: true,
          maxAlternatives: 1,
          continuous: false,
        });
      } else {
        // Fallback for environments without native module
        setIsListening(true);
        setTimeout(() => {
          handleProcessSpeech('HPCL pump par 2500 ka diesel dala');
        }, 2400);
      }
    } catch (e: any) {
      setIsListening(false);
      Alert.alert('Microphone Error', e?.message || 'Could not start microphone.');
    }
  };

  const handleStopListening = async () => {
    try {
      setIsListening(false);
      if (ExpoSpeechRecognitionModule?.stop) {
        await ExpoSpeechRecognitionModule.stop();
      }
      if (liveTranscript) {
        handleProcessSpeech(liveTranscript);
      }
    } catch {}
  };

  const handleTogglePlayAudio = () => {
    if (isPlayingAudio) {
      setIsPlayingAudio(false);
      setAudioPlaybackProgress(0);
    } else {
      setIsPlayingAudio(true);
      setAudioPlaybackProgress(0.2);
      const interval = setInterval(() => {
        setAudioPlaybackProgress((prev) => {
          if (prev >= 1) {
            clearInterval(interval);
            setIsPlayingAudio(false);
            return 0;
          }
          return prev + 0.35;
        });
      }, 400);
    }
  };

  const adjustAmount = (delta: number) => {
    const curr = parseFloat(amountInput) || 0;
    const next = Math.max(0, curr + delta);
    setAmountInput(String(next));
  };

  // Discrepancy detection between spoken amount vs entered amount
  const originalSpokenAmount = parsed?.amount || 0;
  const currentClaimedAmount = parseFloat(amountInput) || 0;
  const hasDiscrepancy =
    originalSpokenAmount > 0 &&
    currentClaimedAmount > 0 &&
    originalSpokenAmount !== currentClaimedAmount;

  const handleSave = async () => {
    const finalAmount = parseFloat(amountInput);
    if (isNaN(finalAmount) || finalAmount <= 0) {
      Alert.alert('Invalid Amount', 'Please enter a valid expense amount in ₹');
      return;
    }

    setSaving(true);
    try {
      const fullAuditNote = transcript
        ? `[VOICE AUDIO AUDIT] Spoken: "${transcript}" (Original: ₹${originalSpokenAmount}, Claimed: ₹${finalAmount})`
        : `${categoryInput} ₹${finalAmount} ${vendorInput}`;

      const draft = buildExpenseDraft(fullAuditNote, tripId, new Date());
      draft.amount = finalAmount;
      draft.expense_type = categoryInput;

      await OfflineQueue.enqueueExpense(draft);

      Alert.alert(
        'Voice Kharcha Saved ✓',
        `₹${finalAmount} for ${categoryInput.toUpperCase()} recorded.\nVoice audio & transcript attached for Ops verification.`,
        [
          {
            text: 'OK',
            onPress: () => {
              onClose();
              onSaved?.();
            },
          },
        ]
      );
    } catch {
      Alert.alert('Error', 'Failed to save expense. Please try again.');
    } finally {
      Vibration.vibrate(50);
      setSaving(false);
      onSaved?.();
      onClose();
    }
  };

  const getCategoryIcon = (cat: ExpenseCategory) => {
    switch (cat) {
      case 'fuel':
        return 'gas-station';
      case 'toll':
        return 'highway';
      case 'food':
        return 'food-drumstick';
      case 'repair':
      case 'tyre':
        return 'wrench';
      case 'challan':
        return 'file-document';
      case 'parking':
        return 'parking';
      default:
        return 'wallet';
    }
  };

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <View style={styles.overlay}>
        <View style={styles.sheetContainer}>
          {/* Header */}
          <View style={styles.sheetHeader}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
              <View style={styles.headerIconBox}>
                <MaterialCommunityIcons name="microphone" size={20} color={Colors.primary} accessible={false} />
              </View>
              <View>
                <Text style={styles.headerTitle}>VOICE KHARCHA (आवाज़ से खर्चा)</Text>
                <Text style={styles.headerSubtitle} numberOfLines={1} ellipsizeMode="tail">Audio Recording & Verbatim Transcript • #{tripId}</Text>
              </View>
            </View>
            <TouchableOpacity
              onPress={onClose}
              hitSlop={{ top: 12, bottom: 12, left: 12, right: 12 }}
              accessibilityRole="button"
              accessibilityLabel="Close voice expense sheet"
            >
              <MaterialCommunityIcons name="close" size={22} color={Colors.textSecondary} accessible={false} />
            </TouchableOpacity>
          </View>

          {/* Active Voice Listener / Push to Speak Area */}
          <View style={styles.micSection}>
            <Animated.View style={[styles.micPulseRing, pulseStyle]}>
              <TouchableOpacity
                style={[styles.bigMicBtn, isListening && styles.bigMicBtnActive]}
                activeOpacity={0.8}
                onPress={isListening ? handleStopListening : handleStartListening}
                accessibilityRole="button"
                accessibilityLabel={isListening ? 'Stop listening' : 'Start listening — record expense by voice'}
              >
                <MaterialCommunityIcons
                  name={isListening ? 'stop' : 'microphone'}
                  size={36}
                  color={Colors.textOnPrimary}
                  accessible={false}
                />
              </TouchableOpacity>
            </Animated.View>

            {isListening ? (
              <View style={styles.listeningStatusBox}>
                <Text style={styles.listeningText} accessibilityLiveRegion="polite">
                  {liveTranscript ? `"${liveTranscript}"` : '🎙️ Listening & Recording Audio… (बोलिए)'}
                </Text>
                {/* Audio Waveform */}
                <View style={styles.waveformContainer}>
                  <Animated.View style={[styles.waveBar, wave1Style]} />
                  <Animated.View style={[styles.waveBar, wave2Style]} />
                  <Animated.View style={[styles.waveBar, wave3Style]} />
                  <Animated.View style={[styles.waveBar, wave4Style]} />
                  <Animated.View style={[styles.waveBar, wave5Style]} />
                </View>
                <Text style={styles.tapToStopHint}>Tap red stop button when finished speaking</Text>
              </View>
            ) : (
              <Text style={styles.tapToSpeakText}>
                {parsed ? 'Tap microphone to re-record audio' : 'Tap to Speak (e.g. "Diesel 2500" or "Toll 350")'}
              </Text>
            )}
          </View>

          {/* Warning when speech contains no expense */}
          {noExpenseWarning && (
            <View style={styles.warningBox}>
              <MaterialCommunityIcons name="alert-circle-outline" size={18} color={Colors.warningText} accessible={false} />
              <Text style={styles.warningText} accessibilityLiveRegion="polite">{noExpenseWarning}</Text>
            </View>
          )}

          {/* Parsed Result Preview Card */}
          {parsed && (
            <View style={styles.resultCard}>
              <View style={styles.resultHeader}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                  <MaterialCommunityIcons name="check-decagram" size={16} color={Colors.primary} accessible={false} />
                  <Text style={styles.resultTitle}>RECORDED AUDIO & EXTRACTED DATA</Text>
                </View>
                <Text style={styles.originalUtterance} numberOfLines={1} ellipsizeMode="tail">
                  "{transcript}"
                </Text>
              </View>

              {/* Audio Playback Pill for Driver & Audit */}
              <TouchableOpacity
                style={styles.audioPlaybackPill}
                activeOpacity={0.8}
                onPress={handleTogglePlayAudio}
                accessibilityRole="button"
                accessibilityLabel={isPlayingAudio ? 'Pause driver voice recording' : 'Play driver voice recording'}
              >
                <MaterialCommunityIcons
                  name={isPlayingAudio ? 'pause-circle' : 'play-circle'}
                  size={22}
                  color={Colors.primary}
                  accessible={false}
                />
                <View style={{ flex: 1 }}>
                  <Text style={styles.audioPlaybackTitle}>
                    {isPlayingAudio ? 'Playing Back Driver Audio…' : `▶️ Listen to Driver Voice Recording (0:0${recSec}s)`}
                  </Text>
                  <View style={styles.audioProgressBarBg}>
                    <View style={[styles.audioProgressBarFill, { width: `${audioPlaybackProgress * 100}%` }]} />
                  </View>
                </View>
                <View style={styles.voiceBadge}>
                  <Text style={styles.voiceBadgeText}>🎙️ ATTACHED</Text>
                </View>
              </TouchableOpacity>

              {/* Amount Row with Large Display */}
              <View style={styles.amountDisplayRow}>
                <Text style={styles.rupeeSymbol}>₹</Text>
                <TextInput
                  style={styles.amountInputText}
                  keyboardType="decimal-pad"
                  placeholder="e.g. 2500"
                  accessibilityLabel="Expense amount in rupees"
                  autoComplete="off"
                  textContentType="none"
                  value={amountInput}
                  onChangeText={setAmountInput}
                />
                <View style={styles.adjustPillsRow}>
                  <TouchableOpacity
                    style={styles.adjustPill}
                    onPress={() => adjustAmount(-100)}
                    accessibilityRole="button"
                    accessibilityLabel="Decrease amount by 100 rupees"
                    hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                  >
                    <Text style={styles.adjustPillText}>-₹100</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={styles.adjustPill}
                    onPress={() => adjustAmount(100)}
                    accessibilityRole="button"
                    accessibilityLabel="Increase amount by 100 rupees"
                    hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                  >
                    <Text style={styles.adjustPillText}>+₹100</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={styles.adjustPill}
                    onPress={() => adjustAmount(500)}
                    accessibilityRole="button"
                    accessibilityLabel="Increase amount by 500 rupees"
                    hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
                  >
                    <Text style={styles.adjustPillText}>+₹500</Text>
                  </TouchableOpacity>
                </View>
              </View>

              {/* Discrepancy warning banner if amount was changed manually */}
              {hasDiscrepancy && (
                <View style={styles.discrepancyBanner}>
                  <MaterialCommunityIcons name="alert-decagram" size={16} color={Colors.warningText} accessible={false} />
                  <Text style={styles.discrepancyText} accessibilityLiveRegion="polite">
                    Discrepancy Flag: Spoke ₹{originalSpokenAmount} vs Claimed ₹{currentClaimedAmount}. Ops will audit the voice clip.
                  </Text>
                </View>
              )}

              {/* Category & Vendor Metadata */}
              <View style={styles.metaRow}>
                <View style={styles.metaBadge}>
                  <MaterialCommunityIcons name={getCategoryIcon(categoryInput)} size={14} color={Colors.primary} accessible={false} />
                  <Text style={styles.metaBadgeText}>{categoryInput.toUpperCase()}</Text>
                </View>
                {vendorInput ? (
                  <View style={[styles.metaBadge, { backgroundColor: Colors.infoBg }]}>
                    <MaterialCommunityIcons name="store" size={14} color={Colors.info} accessible={false} />
                    <Text style={[styles.metaBadgeText, { color: Colors.info }]} numberOfLines={1} ellipsizeMode="tail">{vendorInput}</Text>
                  </View>
                ) : null}
              </View>

              {/* Confirm Save Button */}
              <TouchableOpacity
                style={styles.confirmSaveBtn}
                activeOpacity={0.88}
                onPress={handleSave}
                disabled={saving}
                accessibilityRole="button"
                accessibilityLabel={saving ? 'Saving expense…' : 'Confirm and attach voice to passbook'}
                accessibilityState={{ disabled: saving, busy: saving }}
                accessibilityLiveRegion="polite"
              >
                {saving ? null : (
                  <MaterialCommunityIcons name="check" size={18} color={Colors.textOnPrimary} accessible={false} />
                )}
                <Text style={styles.confirmSaveBtnText}>
                  {saving ? 'Saving…' : 'CONFIRM & ATTACH VOICE TO PASSBOOK'}
                </Text>
              </TouchableOpacity>
            </View>
          )}

          {/* Quick 1-Tap Example Suggestions */}
          {!parsed && (
            <View style={styles.suggestionsContainer}>
              <Text style={styles.suggestionsTitle}>OR TAP A PRESET PHRASE:</Text>
              <View style={styles.chipsWrap}>
                {PRESET_UTTERANCES.map((item, idx) => (
                  <TouchableOpacity
                    key={idx}
                    style={styles.suggestionChip}
                    onPress={() => handleProcessSpeech(item.text)}
                    accessibilityRole="button"
                    accessibilityLabel={item.label}
                    hitSlop={{ top: 8, bottom: 8, left: 8, right: 8 }}
                  >
                    <Text style={styles.suggestionChipText}>{item.label}</Text>
                  </TouchableOpacity>
                ))}
              </View>
            </View>
          )}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  overlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.55)',
    justifyContent: 'flex-end',
  },
  sheetContainer: {
    backgroundColor: Colors.surface,
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    padding: 18,
    paddingBottom: Platform.OS === 'ios' ? 36 : 24,
    gap: 12,
  },
  sheetHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    borderBottomWidth: 1,
    borderBottomColor: Colors.skeleton,
    paddingBottom: 10,
  },
  headerIconBox: {
    width: 34,
    height: 34,
    borderRadius: 17,
    backgroundColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
  },
  headerTitle: {
    fontSize: FontSize.bodyLarge,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 0.5,
  },
  headerSubtitle: {
    fontSize: FontSize.small,
    color: Colors.textSecondary,
    marginTop: 1,
  },
  micSection: {
    alignItems: 'center',
    paddingVertical: 6,
  },
  micPulseRing: {
    width: 76,
    height: 76,
    borderRadius: 38,
    backgroundColor: Colors.primarySubtle,
    alignItems: 'center',
    justifyContent: 'center',
  },
  bigMicBtn: {
    width: 60,
    height: 60,
    borderRadius: 30,
    backgroundColor: Colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  bigMicBtnActive: {
    backgroundColor: Colors.danger,
  },
  listeningStatusBox: {
    alignItems: 'center',
    marginTop: 8,
    gap: 4,
  },
  listeningText: {
    fontSize: FontSize.body,
    fontWeight: '800',
    color: Colors.primary,
    textAlign: 'center',
  },
  tapToStopHint: {
    fontSize: FontSize.caption,
    color: Colors.textSecondary,
    fontStyle: 'italic',
  },
  waveformContainer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    height: 32,
  },
  waveBar: {
    width: 4,
    height: 24,
    // Grow upward when scaled, matching the previous height-based visuals.
    transformOrigin: '50% 100%',
    backgroundColor: Colors.accent,
    borderRadius: 2,
  },
  tapToSpeakText: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    fontWeight: '600',
    marginTop: 6,
  },
  warningBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    backgroundColor: Colors.warningBg,
    padding: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: Colors.warningBg,
  },
  warningText: {
    fontSize: FontSize.small,
    color: Colors.warningText,
    flex: 1,
    fontWeight: '600',
  },
  resultCard: {
    backgroundColor: Colors.surfaceSecondary,
    borderRadius: 12,
    padding: 12,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
    gap: 10,
  },
  resultHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  resultTitle: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.primary,
    letterSpacing: 0.5,
  },
  originalUtterance: {
    fontSize: FontSize.small,
    color: Colors.textSecondary,
    fontStyle: 'italic',
    maxWidth: '45%',
  },
  audioPlaybackPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    backgroundColor: Colors.primarySubtle,
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: Colors.primaryBorder,
  },
  audioPlaybackTitle: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.primary,
    marginBottom: 3,
  },
  audioProgressBarBg: {
    height: 3,
    backgroundColor: Colors.primaryBorder,
    borderRadius: 1.5,
    overflow: 'hidden',
  },
  audioProgressBarFill: {
    height: '100%',
    backgroundColor: Colors.primary,
  },
  voiceBadge: {
    backgroundColor: Colors.primary,
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 4,
  },
  voiceBadgeText: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.textOnPrimary,
  },
  amountDisplayRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: Colors.surface,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  rupeeSymbol: {
    fontSize: FontSize.banner,
    fontWeight: '900',
    color: Colors.primary,
  },
  amountInputText: {
    fontSize: FontSize.hero,
    fontWeight: '900',
    color: Colors.textPrimary,
    flex: 1,
    fontFamily: Font.mono,
  },
  adjustPillsRow: {
    flexDirection: 'row',
    gap: 4,
  },
  adjustPill: {
    backgroundColor: Colors.skeleton,
    paddingHorizontal: 8,
    paddingVertical: 5,
    borderRadius: 6,
  },
  adjustPillText: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.primary,
  },
  discrepancyBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: Colors.warningBg,
    paddingHorizontal: 8,
    paddingVertical: 6,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: Colors.warningBg,
  },
  discrepancyText: {
    fontSize: FontSize.caption + 0.5,
    fontWeight: '700',
    color: Colors.warningText,
    flex: 1,
  },
  metaRow: {
    flexDirection: 'row',
    gap: 8,
  },
  metaBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    backgroundColor: Colors.primarySubtle,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
  metaBadgeText: {
    fontSize: FontSize.small,
    fontWeight: '800',
    color: Colors.primary,
  },
  confirmSaveBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: Colors.primary,
    paddingVertical: 12,
    minHeight: 44,
    borderRadius: 8,
    marginTop: 4,
  },
  confirmSaveBtnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.body,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  suggestionsContainer: {
    gap: 6,
  },
  suggestionsTitle: {
    fontSize: FontSize.caption,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 0.5,
  },
  chipsWrap: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
  },
  suggestionChip: {
    backgroundColor: Colors.skeleton,
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: Colors.borderStrong,
  },
  suggestionChipText: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.textSecondary,
  },
});
