import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Colors, Radius, Spacing } from '../constants/theme';
import {
  AdvanceRequest,
  DriverBalance,
  DriverSettlement,
  getAdvanceRequests,
  getDriverBalance,
  getDriverSettlements,
  requestAdvance,
} from '../services/driverMoney';

/**
 * Paisa tab (Spec 22 §4.3): balance card, settlement history and advance
 * requests. Balance comes from GET /api/driver/balance — the same §5.2
 * formula the admin console uses, so driver and admin always agree.
 */
interface PaisaScreenProps {
  tripId?: string;
}

const money = (n: number) => `₹${Number(n).toLocaleString('en-IN', { maximumFractionDigits: 0 })}`;

export function PaisaScreen({ tripId }: PaisaScreenProps) {
  const [balance, setBalance] = useState<DriverBalance | null>(null);
  const [settlements, setSettlements] = useState<DriverSettlement[]>([]);
  const [advances, setAdvances] = useState<AdvanceRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [amount, setAmount] = useState('');
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setError(null);
      const [bal, stl, adv] = await Promise.all([
        getDriverBalance(),
        getDriverSettlements(),
        getAdvanceRequests(),
      ]);
      if (bal === null) {
        setError('Paisa is not enabled for your account yet.');
        return;
      }
      setBalance(bal);
      setSettlements(stl);
      setAdvances(adv);
    } catch (e: any) {
      setError(e?.message || 'Could not load your money view.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const submitAdvance = async () => {
    const amt = Number(amount);
    if (!amt || amt <= 0) return;
    setSubmitting(true);
    try {
      await requestAdvance({ trip_id: tripId, amount: amt, reason });
      setAmount('');
      setReason('');
      await refresh();
    } catch (e: any) {
      setError(e?.message || 'Advance request failed.');
    } finally {
      setSubmitting(false);
    }
  };

  const statusChip = {
    paid: { bg: Colors.success + '22', text: Colors.success },
    approved: { bg: Colors.primary + '22', text: Colors.primary },
    pending: { bg: Colors.warning + '22', text: Colors.warning },
    rejected: { bg: Colors.danger + '22', text: Colors.danger },
    disputed: { bg: Colors.danger + '22', text: Colors.danger },
    processing: { bg: Colors.primary + '22', text: Colors.primary },
  } as const;

  if (loading) {
    return (
      <View style={styles.center}>
        <ActivityIndicator size="large" color={Colors.primary} />
      </View>
    );
  }

  return (
    <FlatList
      style={styles.screen}
      data={settlements}
      keyExtractor={(s) => s.id}
      ListHeaderComponent={
        <View style={{ marginBottom: Spacing.md }}>
          {/* Balance card */}
          <View style={[styles.card, styles.balanceCard]}>
            <Text style={styles.balanceLabel}>RUNNING BALANCE</Text>
            <Text style={styles.balanceValue}>
              {balance ? money(balance.running_balance) : '—'}
            </Text>
            {balance && balance.pending_advances > 0 && (
              <Text style={styles.balanceSub}>
                {balance.pending_advances} advance request
                {balance.pending_advances > 1 ? 's' : ''} pending
              </Text>
            )}
            {error ? <Text style={styles.error}>{error}</Text> : null}
          </View>

          {/* Advance request */}
          <View style={styles.card}>
            <Text style={styles.sectionTitle}>REQUEST ADVANCE</Text>
            <TextInput
              placeholder="Amount (₹)"
              placeholderTextColor={Colors.textMuted}
              keyboardType="numeric"
              value={amount}
              onChangeText={setAmount}
              style={styles.input}
            />
            <TextInput
              placeholder="Reason (tyre puncture, fuel…)"
              placeholderTextColor={Colors.textMuted}
              value={reason}
              onChangeText={setReason}
              style={styles.input}
            />
            <TouchableOpacity
              style={[styles.button, submitting && { opacity: 0.6 }]}
              onPress={submitAdvance}
              disabled={submitting}
            >
              <Text style={styles.buttonText}>
                {submitting ? 'Sending…' : 'Request advance'}
              </Text>
            </TouchableOpacity>
          </View>

          {/* My advances */}
          {advances.length > 0 && (
            <View style={styles.card}>
              <Text style={styles.sectionTitle}>MY ADVANCES</Text>
              {advances.slice(0, 5).map((a) => {
                const chip = statusChip[a.status as keyof typeof statusChip] ?? statusChip.pending;
                return (
                  <View key={a.id} style={styles.rowBetween}>
                    <Text style={styles.rowText}>
                      {money(a.amount)} · {a.reason || 'advance'}
                    </Text>
                    <View style={[styles.chip, { backgroundColor: chip.bg }]}>
                      <Text style={[styles.chipText, { color: chip.text }]}>{a.status}</Text>
                    </View>
                  </View>
                );
              })}
            </View>
          )}

          <Text style={styles.sectionTitle}>SETTLEMENTS</Text>
        </View>
      }
      renderItem={({ item }) => {
        const chip = statusChip[item.status as keyof typeof statusChip] ?? statusChip.pending;
        return (
          <View style={[styles.card, styles.settleRow]}>
            <View>
              <Text style={styles.rowText}>{item.id}</Text>
              <Text style={styles.subText}>
                gross {money(item.gross)} · tds {money(item.tds)}
              </Text>
            </View>
            <View style={{ alignItems: 'flex-end' }}>
              <Text style={styles.netText}>{money(item.net)}</Text>
              <View style={[styles.chip, { backgroundColor: chip.bg }]}>
                <Text style={[styles.chipText, { color: chip.text }]}>{item.status}</Text>
              </View>
            </View>
          </View>
        );
      }}
      ListEmptyComponent={<Text style={styles.empty}>No settlements yet.</Text>}
    />
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: Colors.background, padding: Spacing.md },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center' },
  card: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    padding: Spacing.md,
    marginBottom: Spacing.md,
  },
  balanceCard: { alignItems: 'center', paddingVertical: Spacing.lg },
  balanceLabel: { color: Colors.textMuted, fontSize: 11, letterSpacing: 1 },
  balanceValue: { color: Colors.textPrimary, fontSize: 34, fontWeight: '700' },
  balanceSub: { color: Colors.warning, marginTop: 4, fontSize: 12 },
  sectionTitle: {
    color: Colors.textMuted,
    fontSize: 11,
    letterSpacing: 1,
    fontWeight: '700',
    marginBottom: Spacing.sm,
  },
  input: {
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    borderRadius: Radius.sm,
    color: Colors.textPrimary,
    paddingHorizontal: Spacing.sm,
    paddingVertical: 8,
    marginBottom: Spacing.sm,
  },
  button: {
    backgroundColor: Colors.primary,
    borderRadius: Radius.sm,
    paddingVertical: 10,
    alignItems: 'center',
  },
  buttonText: { color: Colors.primaryLight, fontWeight: '600' },
  rowBetween: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  rowText: { color: Colors.textPrimary, fontSize: 14 },
  subText: { color: Colors.textMuted, fontSize: 12 },
  netText: { color: Colors.textPrimary, fontSize: 16, fontWeight: '700' },
  settleRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  chip: { borderRadius: 999, paddingHorizontal: 8, paddingVertical: 2, marginTop: 2 },
  chipText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  error: { color: Colors.danger, marginTop: 6, textAlign: 'center' },
  empty: { color: Colors.textMuted, textAlign: 'center', padding: Spacing.lg },
});
