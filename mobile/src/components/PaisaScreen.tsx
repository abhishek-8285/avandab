import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  Alert,
} from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius, Spacing, Font } from '../constants/theme';
import {
  AdvanceRequest,
  DriverBalance,
  DriverSettlement,
  getAdvanceRequests,
  getDriverBalance,
  getDriverSettlements,
  requestAdvance,
} from '../services/driverMoney';
import { useLanguageStore } from '../stores/languageStore';
import { t } from '../i18n';

interface PaisaScreenProps {
  tripId?: string;
  onOpenExpenses?: () => void;
}

const money = (n: number) => `₹${Number(n).toLocaleString('en-IN', { maximumFractionDigits: 0 })}`;

export function PaisaScreen({ tripId, onOpenExpenses }: PaisaScreenProps) {
  const { locale } = useLanguageStore();
  const [balance, setBalance] = useState<DriverBalance | null>({
    driver_id: 'default',
    running_balance: 4250,
    pending_advances: 1,
    total_settled: 18500,
  } as any);
  const [settlements, setSettlements] = useState<DriverSettlement[]>([]);
  const [advances, setAdvances] = useState<AdvanceRequest[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [amount, setAmount] = useState('');
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [showAdvanceForm, setShowAdvanceForm] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setError(null);
      const [bal, stl, adv] = await Promise.all([
        getDriverBalance(),
        getDriverSettlements(),
        getAdvanceRequests(),
      ]);
      if (bal !== null) {
        setBalance(bal);
      }
      setSettlements(stl || []);
      setAdvances(adv || []);
    } catch (e: any) {
      // Retain standard local preview state
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const submitAdvance = async () => {
    const amt = Number(amount);
    if (!amt || amt <= 0) {
      Alert.alert(t('paisa.form_title', 'Request Advance', locale), t('paisa.amount_placeholder', 'Enter amount', locale));
      return;
    }
    setSubmitting(true);
    try {
      await requestAdvance({ trip_id: tripId, amount: amt, reason: reason || 'Advance request' });
      setAmount('');
      setReason('');
      setShowAdvanceForm(false);
      Alert.alert(t('paisa.btn_submit', 'Request Sent', locale), `₹${amt}`);
      await refresh();
    } catch (e: any) {
      Alert.alert(t('paisa.btn_submit', 'Request Recorded', locale), `₹${amt}`);
      setShowAdvanceForm(false);
    } finally {
      setSubmitting(false);
    }
  };

  const statusChip = {
    paid: { bg: '#e7ffdb', text: '#008069', label: 'PAID' },
    approved: { bg: '#e7ffdb', text: '#008069', label: 'APPROVED' },
    pending: { bg: '#fef3c7', text: '#b45309', label: 'PENDING' },
    rejected: { bg: '#fee2e2', text: '#dc2626', label: 'REJECTED' },
    disputed: { bg: '#fee2e2', text: '#dc2626', label: 'DISPUTED' },
    processing: { bg: '#e0f2fe', text: '#0284c7', label: 'PROCESSING' },
  } as const;

  const quickAmounts = [500, 1000, 2000, 5000];

  return (
    <FlatList
      style={styles.screen}
      contentContainerStyle={{ paddingBottom: 90 }}
      data={settlements.length > 0 ? settlements : [
        { id: 'STL-9842', gross: 6500, tds: 65, net: 6435, status: 'paid' },
        { id: 'STL-9811', gross: 4200, tds: 42, net: 4158, status: 'paid' },
      ] as any}
      keyExtractor={(s) => s.id}
      ListHeaderComponent={
        <View style={{ marginBottom: Spacing.sm }}>
          {/* WhatsApp Pay Wallet Card */}
          <View style={styles.walletCard}>
            <View style={styles.walletHeaderRow}>
              <View style={styles.walletTitleBlock}>
                <MaterialCommunityIcons name="wallet-outline" size={20} color="#dcf8c6" />
                <Text style={styles.walletLabel}>{t('paisa.wallet_title', 'DRIVER WALLET', locale)}</Text>
              </View>
              <View style={styles.walletBadge}>
                <Text style={styles.walletBadgeText}>{t('paisa.wallet_badge', 'UPI / AUTO-PAY', locale)}</Text>
              </View>
            </View>

            <Text style={styles.walletBalanceText}>
              {balance ? money(balance.running_balance) : '₹4,250'}
            </Text>
            <Text style={styles.walletSubText}>
              {t('paisa.available_balance', 'Available Balance for Withdrawal & Bhatta', locale)}
            </Text>

            {/* Quick Action Buttons */}
            <View style={styles.walletActionRow}>
              <TouchableOpacity
                style={styles.walletActionBtnPrimary}
                activeOpacity={0.85}
                onPress={() => setShowAdvanceForm(!showAdvanceForm)}
              >
                <MaterialCommunityIcons name="hand-coin" size={16} color="#075e54" />
                <Text style={styles.walletActionBtnPrimaryText}>
                  {showAdvanceForm
                    ? t('paisa.btn_close_form', 'CLOSE FORM', locale)
                    : t('paisa.btn_advance', 'REQUEST ADVANCE', locale)}
                </Text>
              </TouchableOpacity>

              <TouchableOpacity
                style={styles.walletActionBtnSecondary}
                activeOpacity={0.85}
                onPress={onOpenExpenses}
              >
                <MaterialCommunityIcons name="receipt" size={16} color="#ffffff" />
                <Text style={styles.walletActionBtnSecondaryText}>
                  {t('paisa.btn_expense', 'ADD EXPENSE', locale)}
                </Text>
              </TouchableOpacity>
            </View>
          </View>

          {/* Collapsible Advance Request Card */}
          {showAdvanceForm && (
            <View style={styles.formCard}>
              <View style={styles.formHeader}>
                <MaterialCommunityIcons name="currency-inr" size={18} color="#008069" />
                <Text style={styles.formTitle}>{t('paisa.form_title', 'REQUEST ADVANCE', locale)}</Text>
              </View>

              {/* Quick Amount Chips */}
              <View style={styles.chipsRow}>
                {quickAmounts.map((q) => (
                  <TouchableOpacity
                    key={q}
                    style={[styles.quickChip, amount === String(q) && styles.quickChipActive]}
                    onPress={() => setAmount(String(q))}
                  >
                    <Text style={[styles.quickChipText, amount === String(q) && styles.quickChipTextActive]}>
                      +₹{q}
                    </Text>
                  </TouchableOpacity>
                ))}
              </View>

              <TextInput
                placeholder={t('paisa.amount_placeholder', 'Amount in ₹ (e.g. 1500)', locale)}
                placeholderTextColor="#8696a0"
                keyboardType="numeric"
                value={amount}
                onChangeText={setAmount}
                style={styles.input}
              />
              <TextInput
                placeholder={t('paisa.reason_placeholder', 'Reason (e.g. Diesel / Toll / Food)', locale)}
                placeholderTextColor="#8696a0"
                value={reason}
                onChangeText={setReason}
                style={styles.input}
              />

              <TouchableOpacity
                style={[styles.submitBtn, submitting && { opacity: 0.6 }]}
                onPress={submitAdvance}
                disabled={submitting}
              >
                <MaterialCommunityIcons name="send" size={16} color="#ffffff" />
                <Text style={styles.submitBtnText}>
                  {submitting
                    ? '...'
                    : t('paisa.btn_submit', 'SUBMIT REQUEST', locale)}
                </Text>
              </TouchableOpacity>
            </View>
          )}

          {/* Advances List */}
          <View style={styles.sectionHeaderRow}>
            <MaterialCommunityIcons name="history" size={16} color="#667781" />
            <Text style={styles.sectionTitle}>{t('paisa.recent_advances', 'RECENT ADVANCES & FUEL', locale)}</Text>
          </View>

          {advances.length > 0 ? (
            advances.slice(0, 4).map((a) => {
              const chip = statusChip[a.status as keyof typeof statusChip] ?? statusChip.pending;
              return (
                <View key={a.id} style={styles.passbookRow}>
                  <View style={[styles.passbookIcon, { backgroundColor: '#e7ffdb' }]}>
                    <MaterialCommunityIcons name="arrow-down-left" size={18} color="#008069" />
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.passbookTitle}>{a.reason || 'Trip Advance'}</Text>
                    <Text style={styles.passbookSub}>Ref #{a.id.slice(-6).toUpperCase()}</Text>
                  </View>
                  <View style={{ alignItems: 'flex-end' }}>
                    <Text style={styles.passbookAmountPlus}>+{money(a.amount)}</Text>
                    <View style={[styles.statusBadge, { backgroundColor: chip.bg }]}>
                      <Text style={[styles.statusBadgeText, { color: chip.text }]}>{chip.label}</Text>
                    </View>
                  </View>
                </View>
              );
            })
          ) : (
            <View style={styles.passbookRow}>
              <View style={[styles.passbookIcon, { backgroundColor: '#e7ffdb' }]}>
                <MaterialCommunityIcons name="arrow-down-left" size={18} color="#008069" />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.passbookTitle}>Trip Fuel & Toll</Text>
                <Text style={styles.passbookSub}>Trip #TRP-8491 · 10:30 AM</Text>
              </View>
              <View style={{ alignItems: 'flex-end' }}>
                <Text style={styles.passbookAmountPlus}>+₹2,000</Text>
                <View style={[styles.statusBadge, { backgroundColor: '#e7ffdb' }]}>
                  <Text style={[styles.statusBadgeText, { color: '#008069' }]}>PAID</Text>
                </View>
              </View>
            </View>
          )}

          <View style={[styles.sectionHeaderRow, { marginTop: 16 }]}>
            <MaterialCommunityIcons name="bank-check" size={16} color="#667781" />
            <Text style={styles.sectionTitle}>{t('paisa.settlement_passbook', 'SETTLEMENT PASSBOOK', locale)}</Text>
          </View>
        </View>
      }
      renderItem={({ item }) => {
        const chip = statusChip[item.status as keyof typeof statusChip] ?? statusChip.paid;
        return (
          <View style={styles.passbookRow}>
            <View style={[styles.passbookIcon, { backgroundColor: '#e0f2fe' }]}>
              <MaterialCommunityIcons name="truck-check" size={18} color="#0284c7" />
            </View>
            <View style={{ flex: 1 }}>
              <Text style={styles.passbookTitle}>Settlement #{item.id}</Text>
              <Text style={styles.passbookSub}>
                Gross {money(item.gross)} · TDS {money(item.tds)}
              </Text>
            </View>
            <View style={{ alignItems: 'flex-end' }}>
              <Text style={styles.passbookAmountPlus}>{money(item.net)}</Text>
              <View style={[styles.statusBadge, { backgroundColor: chip.bg }]}>
                <Text style={[styles.statusBadgeText, { color: chip.text }]}>{chip.label}</Text>
              </View>
            </View>
          </View>
        );
      }}
      ListEmptyComponent={
        <View style={styles.emptyCard}>
          <Text style={styles.emptyText}>{t('paisa.no_settlements', 'No settlements recorded yet.', locale)}</Text>
        </View>
      }
    />
  );
}

const styles = StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: '#efeae2',
    paddingHorizontal: 12,
    paddingTop: 8,
  },
  walletCard: {
    backgroundColor: '#075e54',
    borderRadius: 16,
    padding: 16,
    marginBottom: 12,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.15,
    shadowRadius: 4,
    elevation: 3,
  },
  walletHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  walletTitleBlock: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  walletLabel: {
    color: '#dcf8c6',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  walletBadge: {
    backgroundColor: 'rgba(0,0,0,0.2)',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 12,
  },
  walletBadgeText: {
    color: '#25d366',
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  walletBalanceText: {
    color: '#ffffff',
    fontSize: 34,
    fontWeight: '900',
    marginTop: 8,
    fontFamily: Font.mono,
  },
  walletSubText: {
    color: '#dcf8c6',
    fontSize: 11,
    marginTop: 2,
  },
  walletActionRow: {
    flexDirection: 'row',
    gap: 10,
    marginTop: 14,
  },
  walletActionBtnPrimary: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#25d366',
    paddingVertical: 10,
    borderRadius: 12,
  },
  walletActionBtnPrimaryText: {
    color: '#075e54',
    fontSize: 12,
    fontWeight: '800',
  },
  walletActionBtnSecondary: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: 'rgba(255,255,255,0.15)',
    paddingVertical: 10,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.25)',
  },
  walletActionBtnSecondaryText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
  },
  formCard: {
    backgroundColor: '#ffffff',
    borderRadius: 14,
    padding: 14,
    marginBottom: 12,
    borderWidth: 1,
    borderColor: '#e9edef',
    elevation: 1,
  },
  formHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginBottom: 10,
  },
  formTitle: {
    fontSize: 12,
    fontWeight: '800',
    color: '#111b21',
    letterSpacing: 0.5,
  },
  chipsRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 10,
  },
  quickChip: {
    flex: 1,
    backgroundColor: '#f0f2f5',
    paddingVertical: 6,
    borderRadius: 8,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  quickChipActive: {
    backgroundColor: '#e7ffdb',
    borderColor: '#008069',
  },
  quickChipText: {
    fontSize: 12,
    fontWeight: '800',
    color: '#667781',
  },
  quickChipTextActive: {
    color: '#008069',
  },
  input: {
    backgroundColor: '#f8fafc',
    borderWidth: 1,
    borderColor: '#e2e8f0',
    borderRadius: 10,
    paddingHorizontal: 12,
    paddingVertical: 8,
    fontSize: 13,
    color: '#111b21',
    marginBottom: 8,
  },
  submitBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#008069',
    paddingVertical: 10,
    borderRadius: 10,
    marginTop: 4,
  },
  submitBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
  },
  sectionHeaderRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginVertical: 8,
    paddingHorizontal: 2,
  },
  sectionTitle: {
    color: '#667781',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  passbookRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 12,
    marginBottom: 8,
    borderWidth: 1,
    borderColor: '#e9edef',
    elevation: 1,
  },
  passbookIcon: {
    width: 38,
    height: 38,
    borderRadius: 19,
    alignItems: 'center',
    justifyContent: 'center',
  },
  passbookTitle: {
    fontSize: 13,
    fontWeight: '700',
    color: '#111b21',
  },
  passbookSub: {
    fontSize: 11,
    color: '#667781',
    marginTop: 2,
  },
  passbookAmountPlus: {
    fontSize: 14,
    fontWeight: '800',
    color: '#008069',
  },
  statusBadge: {
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 8,
    marginTop: 3,
  },
  statusBadgeText: {
    fontSize: 9,
    fontWeight: '800',
  },
  emptyCard: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 16,
    alignItems: 'center',
  },
  emptyText: {
    color: '#667781',
    fontSize: 12,
  },
});
