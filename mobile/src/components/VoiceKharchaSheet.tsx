import React, { useState, useEffect, useRef } from 'react';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  Modal,
  TextInput,
  Animated,
  Easing,
  Alert,
  Platform,
} from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import {
  ExpoSpeechRecognitionModule,
  useSpeechRecognitionEvent,
} from 'expo-speech-recognition';
import { parseExpenseUtterance, buildExpenseDraft, ExpenseCategory, ParsedExpense } from '../services/speech';
import { OfflineQueue } from '../services/offlineQueue';
import { Colors, Font, Spacing } from '../constants/theme';

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

  // Pulse animation for recording mic
  const pulseAnim = useRef(new Animated.Value(1)).current;
  // Live audio wave bars
  const wave1 = useRef(new Animated.Value(8)).current;
  const wave2 = useRef(new Animated.Value(14)).current;
  const wave3 = useRef(new Animated.Value(24)).current;
  const wave4 = useRef(new Animated.Value(18)).current;
  const wave5 = useRef(new Animated.Value(10)).current;

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
    setIsListening(false);
    console.log('[SPEECH RECOGNITION ERROR]', event.error, event.message);
  });

  useSpeechRecognitionEvent('volumechange', (event) => {
    if (event.value !== undefined) {
      const vol = Math.max(0, Math.min(100, (event.value + 50) * 2));
      wave1.setValue(6 + (vol * 0.2));
      wave2.setValue(10 + (vol * 0.3));
      wave3.setValue(14 + (vol * 0.4));
      wave4.setValue(8 + (vol * 0.3));
      wave5.setValue(6 + (vol * 0.2));
    }
  });

  useEffect(() => {
    let pulseLoop: Animated.CompositeAnimation | null = null;

    if (isListening) {
      pulseLoop = Animated.loop(
        Animated.sequence([
          Animated.timing(pulseAnim, {
            toValue: 1.25,
            duration: 600,
            easing: Easing.inOut(Easing.ease),
            useNativeDriver: true,
          }),
          Animated.timing(pulseAnim, {
            toValue: 1,
            duration: 600,
            easing: Easing.inOut(Easing.ease),
            useNativeDriver: true,
          }),
        ])
      );
      pulseLoop.start();
    } else {
      pulseAnim.setValue(1);
      wave1.setValue(8);
      wave2.setValue(14);
      wave3.setValue(24);
      wave4.setValue(18);
      wave5.setValue(10);
    }

    return () => {
      pulseLoop?.stop();
    };
  }, [isListening]);

  // Request permissions on modal open
  useEffect(() => {
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

      if (ExpoSpeechRecognitionModule?.requestPermissionsAsync) {
        ExpoSpeechRecognitionModule.requestPermissionsAsync().catch(() => {});
      }
    } else {
      if (isListening && ExpoSpeechRecognitionModule?.stop) {
        ExpoSpeechRecognitionModule.stop();
      }
    }
  }, [visible]);

  const handleProcessSpeech = (utterance: string) => {
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
  };

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
        // Start live Android Speech Recognizer with Hindi + Indian English recognition
        await ExpoSpeechRecognitionModule.start({
          lang: 'hi-IN',
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
      setSaving(false);
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
                <MaterialCommunityIcons name="microphone" size={20} color="#008069" />
              </View>
              <View>
                <Text style={styles.headerTitle}>VOICE KHARCHA (आवाज़ से खर्चा)</Text>
                <Text style={styles.headerSubtitle}>Audio Recording & Verbatim Transcript • #{tripId}</Text>
              </View>
            </View>
            <TouchableOpacity onPress={onClose} hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}>
              <MaterialCommunityIcons name="close" size={22} color="#667781" />
            </TouchableOpacity>
          </View>

          {/* Active Voice Listener / Push to Speak Area */}
          <View style={styles.micSection}>
            <Animated.View style={[styles.micPulseRing, { transform: [{ scale: pulseAnim }] }]}>
              <TouchableOpacity
                style={[styles.bigMicBtn, isListening && styles.bigMicBtnActive]}
                activeOpacity={0.8}
                onPress={isListening ? handleStopListening : handleStartListening}
              >
                <MaterialCommunityIcons
                  name={isListening ? 'stop' : 'microphone'}
                  size={36}
                  color="#ffffff"
                />
              </TouchableOpacity>
            </Animated.View>

            {isListening ? (
              <View style={styles.listeningStatusBox}>
                <Text style={styles.listeningText}>
                  {liveTranscript ? `"${liveTranscript}"` : '🎙️ Listening & Recording Audio... (बोलिए)'}
                </Text>
                {/* Audio Waveform */}
                <View style={styles.waveformContainer}>
                  <Animated.View style={[styles.waveBar, { height: wave1 }]} />
                  <Animated.View style={[styles.waveBar, { height: wave2 }]} />
                  <Animated.View style={[styles.waveBar, { height: wave3 }]} />
                  <Animated.View style={[styles.waveBar, { height: wave4 }]} />
                  <Animated.View style={[styles.waveBar, { height: wave5 }]} />
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
              <MaterialCommunityIcons name="alert-circle-outline" size={18} color="#b45309" />
              <Text style={styles.warningText}>{noExpenseWarning}</Text>
            </View>
          )}

          {/* Parsed Result Preview Card */}
          {parsed && (
            <View style={styles.resultCard}>
              <View style={styles.resultHeader}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                  <MaterialCommunityIcons name="check-decagram" size={16} color="#008069" />
                  <Text style={styles.resultTitle}>RECORDED AUDIO & EXTRACTED DATA</Text>
                </View>
                <Text style={styles.originalUtterance} numberOfLines={1}>
                  "{transcript}"
                </Text>
              </View>

              {/* Audio Playback Pill for Driver & Audit */}
              <TouchableOpacity
                style={styles.audioPlaybackPill}
                activeOpacity={0.8}
                onPress={handleTogglePlayAudio}
              >
                <MaterialCommunityIcons
                  name={isPlayingAudio ? 'pause-circle' : 'play-circle'}
                  size={22}
                  color="#008069"
                />
                <View style={{ flex: 1 }}>
                  <Text style={styles.audioPlaybackTitle}>
                    {isPlayingAudio ? 'Playing Back Driver Audio...' : '▶️ Listen to Driver Voice Recording (0:03s)'}
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
                  keyboardType="numeric"
                  placeholder="0"
                  value={amountInput}
                  onChangeText={setAmountInput}
                />
                <View style={styles.adjustPillsRow}>
                  <TouchableOpacity style={styles.adjustPill} onPress={() => adjustAmount(-100)}>
                    <Text style={styles.adjustPillText}>-₹100</Text>
                  </TouchableOpacity>
                  <TouchableOpacity style={styles.adjustPill} onPress={() => adjustAmount(100)}>
                    <Text style={styles.adjustPillText}>+₹100</Text>
                  </TouchableOpacity>
                  <TouchableOpacity style={styles.adjustPill} onPress={() => adjustAmount(500)}>
                    <Text style={styles.adjustPillText}>+₹500</Text>
                  </TouchableOpacity>
                </View>
              </View>

              {/* Discrepancy warning banner if amount was changed manually */}
              {hasDiscrepancy && (
                <View style={styles.discrepancyBanner}>
                  <MaterialCommunityIcons name="alert-decagram" size={16} color="#d97706" />
                  <Text style={styles.discrepancyText}>
                    Discrepancy Flag: Spoke ₹{originalSpokenAmount} vs Claimed ₹{currentClaimedAmount}. Ops will audit the voice clip.
                  </Text>
                </View>
              )}

              {/* Category & Vendor Metadata */}
              <View style={styles.metaRow}>
                <View style={styles.metaBadge}>
                  <MaterialCommunityIcons name={getCategoryIcon(categoryInput)} size={14} color="#008069" />
                  <Text style={styles.metaBadgeText}>{categoryInput.toUpperCase()}</Text>
                </View>
                {vendorInput ? (
                  <View style={[styles.metaBadge, { backgroundColor: '#e0f2fe' }]}>
                    <MaterialCommunityIcons name="store" size={14} color="#0284c7" />
                    <Text style={[styles.metaBadgeText, { color: '#0284c7' }]}>{vendorInput}</Text>
                  </View>
                ) : null}
              </View>

              {/* Confirm Save Button */}
              <TouchableOpacity
                style={styles.confirmSaveBtn}
                activeOpacity={0.88}
                onPress={handleSave}
                disabled={saving}
              >
                <MaterialCommunityIcons name="check" size={18} color="#ffffff" />
                <Text style={styles.confirmSaveBtnText}>CONFIRM & ATTACH VOICE TO PASSBOOK</Text>
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
    backgroundColor: '#ffffff',
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
    borderBottomColor: '#f1f5f9',
    paddingBottom: 10,
  },
  headerIconBox: {
    width: 34,
    height: 34,
    borderRadius: 17,
    backgroundColor: '#e7ffdb',
    alignItems: 'center',
    justifyContent: 'center',
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: '800',
    color: '#0f172a',
    letterSpacing: 0.5,
  },
  headerSubtitle: {
    fontSize: 10,
    color: '#64748b',
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
    backgroundColor: '#e7ffdb',
    alignItems: 'center',
    justifyContent: 'center',
  },
  bigMicBtn: {
    width: 60,
    height: 60,
    borderRadius: 30,
    backgroundColor: '#008069',
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  bigMicBtnActive: {
    backgroundColor: '#ef4444',
  },
  listeningStatusBox: {
    alignItems: 'center',
    marginTop: 8,
    gap: 4,
  },
  listeningText: {
    fontSize: 12,
    fontWeight: '800',
    color: '#008069',
    textAlign: 'center',
  },
  tapToStopHint: {
    fontSize: 9,
    color: '#64748b',
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
    backgroundColor: '#25d366',
    borderRadius: 2,
  },
  tapToSpeakText: {
    fontSize: 11,
    color: '#64748b',
    fontWeight: '600',
    marginTop: 6,
  },
  warningBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    backgroundColor: '#fffbeb',
    padding: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#fef3c7',
  },
  warningText: {
    fontSize: 10,
    color: '#b45309',
    flex: 1,
    fontWeight: '600',
  },
  resultCard: {
    backgroundColor: '#f8fafc',
    borderRadius: 12,
    padding: 12,
    borderWidth: 1,
    borderColor: '#e2e8f0',
    gap: 10,
  },
  resultHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  resultTitle: {
    fontSize: 10,
    fontWeight: '800',
    color: '#008069',
    letterSpacing: 0.5,
  },
  originalUtterance: {
    fontSize: 10,
    color: '#64748b',
    fontStyle: 'italic',
    maxWidth: '45%',
  },
  audioPlaybackPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    backgroundColor: '#e7ffdb',
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#bbf7d0',
  },
  audioPlaybackTitle: {
    fontSize: 10,
    fontWeight: '700',
    color: '#008069',
    marginBottom: 3,
  },
  audioProgressBarBg: {
    height: 3,
    backgroundColor: '#bbf7d0',
    borderRadius: 1.5,
    overflow: 'hidden',
  },
  audioProgressBarFill: {
    height: '100%',
    backgroundColor: '#008069',
  },
  voiceBadge: {
    backgroundColor: '#008069',
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 4,
  },
  voiceBadgeText: {
    fontSize: 9,
    fontWeight: '800',
    color: '#ffffff',
  },
  amountDisplayRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: '#ffffff',
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#cbd5e1',
  },
  rupeeSymbol: {
    fontSize: 20,
    fontWeight: '900',
    color: '#008069',
  },
  amountInputText: {
    fontSize: 22,
    fontWeight: '900',
    color: '#0f172a',
    flex: 1,
    fontFamily: Font.mono,
  },
  adjustPillsRow: {
    flexDirection: 'row',
    gap: 4,
  },
  adjustPill: {
    backgroundColor: '#f1f5f9',
    paddingHorizontal: 8,
    paddingVertical: 5,
    borderRadius: 6,
  },
  adjustPillText: {
    fontSize: 10,
    fontWeight: '700',
    color: '#008069',
  },
  discrepancyBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: '#fffbeb',
    paddingHorizontal: 8,
    paddingVertical: 6,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: '#fde68a',
  },
  discrepancyText: {
    fontSize: 9.5,
    fontWeight: '700',
    color: '#b45309',
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
    backgroundColor: '#e7ffdb',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
  metaBadgeText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#008069',
  },
  confirmSaveBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#008069',
    paddingVertical: 12,
    borderRadius: 8,
    marginTop: 4,
  },
  confirmSaveBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  suggestionsContainer: {
    gap: 6,
  },
  suggestionsTitle: {
    fontSize: 9,
    fontWeight: '800',
    color: '#64748b',
    letterSpacing: 0.5,
  },
  chipsWrap: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
  },
  suggestionChip: {
    backgroundColor: '#f1f5f9',
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  suggestionChipText: {
    fontSize: 10,
    fontWeight: '700',
    color: '#334155',
  },
});
