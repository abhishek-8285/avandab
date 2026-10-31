import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, FlatList, TouchableOpacity, RefreshControl, ActivityIndicator, Alert, Modal, TextInput } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors } from '../../constants/theme';
import { managerService, PendingExpense } from '../../services/managerService';

export function ManagerApprovalsScreen() {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [expenses, setExpenses] = useState<PendingExpense[]>([]);
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null);

  // Reject Modal State
  const [rejectModalVisible, setRejectModalVisible] = useState(false);
  const [rejectTargetId, setRejectTargetId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState('');

  const loadPending = async () => {
    try {
      const data = await managerService.fetchPendingExpenses();
      setExpenses(data);
    } catch {
      // Degrade gracefully
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const data = await managerService.fetchPendingExpenses();
        if (active) {
          setExpenses(data);
          setLoading(false);
        }
      } catch {
        if (active) {
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const handleApprove = async (id: string) => {
    try {
      setActionLoadingId(id);
      await managerService.approveExpense(id);
      setExpenses((prev) => prev.filter((item) => item.id !== id));
      Alert.alert('Approved', 'Driver expense has been approved.');
    } catch (err: any) {
      Alert.alert('Error', err?.message || 'Failed to approve expense');
    } finally {
      setActionLoadingId(null);
    }
  };

  const openRejectModal = (id: string) => {
    setRejectTargetId(id);
    setRejectReason('');
    setRejectModalVisible(true);
  };

  const handleConfirmReject = async () => {
    if (!rejectTargetId) return;
    try {
      setActionLoadingId(rejectTargetId);
      setRejectModalVisible(false);
      await managerService.rejectExpense(rejectTargetId, rejectReason || 'Insufficient proof or discrepancy');
      setExpenses((prev) => prev.filter((item) => item.id !== rejectTargetId));
      Alert.alert('Rejected', 'Expense claim has been rejected.');
    } catch (err: any) {
      Alert.alert('Error', err?.message || 'Failed to reject expense');
    } finally {
      setActionLoadingId(null);
      setRejectTargetId(null);
    }
  };

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>Driver Kharcha Approvals</Text>
        <Text style={styles.headerSub}>Verify receipts, fuel claims & toll expenses</Text>
      </View>

      {loading ? (
        <View style={styles.centerBox}>
          <ActivityIndicator size="large" color={Colors.primary} />
          <Text style={styles.loadingText}>Loading pending claims...</Text>
        </View>
      ) : expenses.length === 0 ? (
        <View style={styles.emptyBox}>
          <MaterialCommunityIcons name="check-all" size={54} color="#10b981" />
          <Text style={styles.emptyTitle}>All Caught Up!</Text>
          <Text style={styles.emptySubtitle}>No pending driver expenses requiring manager approval.</Text>
        </View>
      ) : (
        <FlatList
          data={expenses}
          keyExtractor={(item) => item.id}
          contentContainerStyle={styles.listContent}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={loadPending} colors={[Colors.primary]} />}
          renderItem={({ item }) => {
            const isActing = actionLoadingId === item.id;
            const category = (item.expense_type || 'Expense').toUpperCase();

            return (
              <View style={styles.card} testID={`expense-approval-card-${item.id}`}>
                <View style={styles.cardTop}>
                  <View style={styles.categoryBadge}>
                    <Text style={styles.categoryBadgeText}>{category}</Text>
                  </View>
                  <Text style={styles.amountText}>₹{(item.amount || 0).toFixed(2)}</Text>
                </View>

                <View style={styles.detailsBox}>
                  <Text style={styles.detailLine}>
                    <Text style={styles.detailLabel}>Driver: </Text>
                    {item.driver_name || item.driver_id || 'Assigned Driver'}
                  </Text>
                  <Text style={styles.detailLine}>
                    <Text style={styles.detailLabel}>Trip: </Text>
                    {item.trip_id || 'Unassigned'}
                  </Text>
                  {item.description ? (
                    <Text style={styles.detailLine}>
                      <Text style={styles.detailLabel}>Notes: </Text>
                      {item.description}
                    </Text>
                  ) : null}
                  {item.receipt_url ? (
                    <View style={styles.receiptRow}>
                      <MaterialCommunityIcons name="file-image-outline" size={16} color="#0284c7" />
                      <Text style={styles.receiptText}>Receipt photo attached</Text>
                    </View>
                  ) : null}
                </View>

                {/* Approval Action Buttons */}
                <View style={styles.actionsRow}>
                  <TouchableOpacity
                    style={[styles.btn, styles.rejectBtn]}
                    onPress={() => openRejectModal(item.id)}
                    disabled={isActing}
                    testID={`reject-button-${item.id}`}
                  >
                    <MaterialCommunityIcons name="close-circle-outline" size={18} color="#dc2626" />
                    <Text style={styles.rejectBtnText}>Reject</Text>
                  </TouchableOpacity>

                  <TouchableOpacity
                    style={[styles.btn, styles.approveBtn]}
                    onPress={() => handleApprove(item.id)}
                    disabled={isActing}
                    testID={`approve-button-${item.id}`}
                  >
                    {isActing ? (
                      <ActivityIndicator size="small" color="#ffffff" />
                    ) : (
                      <>
                        <MaterialCommunityIcons name="check-circle-outline" size={18} color="#ffffff" />
                        <Text style={styles.approveBtnText}>Approve</Text>
                      </>
                    )}
                  </TouchableOpacity>
                </View>
              </View>
            );
          }}
        />
      )}

      {/* Rejection Modal */}
      <Modal
        visible={rejectModalVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setRejectModalVisible(false)}
      >
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <Text style={styles.modalTitle}>Reject Expense Claim</Text>
            <Text style={styles.modalSubtitle}>Provide a reason for the driver debrief log:</Text>
            <TextInput
              style={styles.modalInput}
              placeholder="e.g. Incomplete receipt, amount mismatch..."
              placeholderTextColor="#94a3b8"
              value={rejectReason}
              onChangeText={setRejectReason}
              testID="reject-reason-input"
            />
            <View style={styles.modalButtons}>
              <TouchableOpacity
                style={styles.modalCancelBtn}
                onPress={() => setRejectModalVisible(false)}
              >
                <Text style={styles.modalCancelText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={styles.modalConfirmBtn}
                onPress={handleConfirmReject}
                testID="confirm-reject-btn"
              >
                <Text style={styles.modalConfirmText}>Confirm Reject</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f8fafc',
  },
  header: {
    backgroundColor: '#075e54',
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 16,
  },
  headerTitle: {
    fontSize: 18,
    fontWeight: '700',
    color: '#ffffff',
  },
  headerSub: {
    fontSize: 12,
    color: 'rgba(255,255,255,0.7)',
    marginTop: 2,
  },
  centerBox: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  loadingText: {
    marginTop: 12,
    color: '#64748b',
    fontSize: 14,
  },
  emptyBox: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: 32,
  },
  emptyTitle: {
    fontSize: 18,
    fontWeight: '800',
    color: '#0f172a',
    marginTop: 12,
  },
  emptySubtitle: {
    fontSize: 13,
    color: '#64748b',
    marginTop: 4,
    textAlign: 'center',
  },
  listContent: {
    padding: 16,
    gap: 12,
  },
  card: {
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 14,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 3,
    elevation: 2,
  },
  cardTop: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    borderBottomWidth: 1,
    borderBottomColor: '#f1f5f9',
    paddingBottom: 10,
  },
  categoryBadge: {
    backgroundColor: '#f1f5f9',
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
  categoryBadgeText: {
    fontSize: 11,
    fontWeight: '800',
    color: '#334155',
  },
  amountText: {
    fontSize: 18,
    fontWeight: '800',
    color: '#0f172a',
  },
  detailsBox: {
    paddingVertical: 10,
    gap: 4,
  },
  detailLine: {
    fontSize: 13,
    color: '#334155',
  },
  detailLabel: {
    fontWeight: '700',
    color: '#64748b',
  },
  receiptRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    marginTop: 4,
  },
  receiptText: {
    fontSize: 12,
    color: '#0284c7',
    fontWeight: '600',
  },
  actionsRow: {
    flexDirection: 'row',
    gap: 10,
    paddingTop: 10,
    borderTopWidth: 1,
    borderTopColor: '#f1f5f9',
  },
  btn: {
    flex: 1,
    height: 40,
    borderRadius: 8,
    flexDirection: 'row',
    justifyContent: 'center',
    alignItems: 'center',
    gap: 6,
  },
  rejectBtn: {
    backgroundColor: '#fee2e2',
  },
  rejectBtnText: {
    color: '#dc2626',
    fontWeight: '700',
    fontSize: 13,
  },
  approveBtn: {
    backgroundColor: '#008069',
  },
  approveBtnText: {
    color: '#ffffff',
    fontWeight: '700',
    fontSize: 13,
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'center',
    alignItems: 'center',
    padding: 20,
  },
  modalCard: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 20,
    width: '100%',
    maxWidth: 360,
  },
  modalTitle: {
    fontSize: 16,
    fontWeight: '800',
    color: '#0f172a',
  },
  modalSubtitle: {
    fontSize: 13,
    color: '#64748b',
    marginTop: 4,
    marginBottom: 12,
  },
  modalInput: {
    borderWidth: 1,
    borderColor: '#e2e8f0',
    borderRadius: 8,
    padding: 10,
    fontSize: 14,
    color: '#1e293b',
    marginBottom: 16,
  },
  modalButtons: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
  },
  modalCancelBtn: {
    paddingVertical: 8,
    paddingHorizontal: 14,
  },
  modalCancelText: {
    color: '#64748b',
    fontWeight: '600',
  },
  modalConfirmBtn: {
    backgroundColor: '#dc2626',
    paddingVertical: 8,
    paddingHorizontal: 16,
    borderRadius: 6,
  },
  modalConfirmText: {
    color: '#ffffff',
    fontWeight: '700',
  },
});
