import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, RefreshControl, ActivityIndicator } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors, Radius } from '../../constants/theme';
import { useAuthStore } from '../../stores/authStore';
import { managerService, ManagerDashboardKPIs } from '../../services/managerService';

interface Props {
  onNavigateTab: (tab: 'dashboard' | 'fleet' | 'trips' | 'approvals') => void;
}

export function ManagerDashboardScreen({ onNavigateTab }: Props) {
  const { user, toggleMode } = useAuthStore();
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [kpis, setKpis] = useState<ManagerDashboardKPIs | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadData = async () => {
    try {
      const data = await managerService.fetchDashboard();
      setKpis(data);
      setError(null);
    } catch (err: any) {
      setError(err?.message || 'Failed to load dashboard');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const data = await managerService.fetchDashboard();
        if (active) {
          setKpis(data);
          setError(null);
          setLoading(false);
        }
      } catch (err: any) {
        if (active) {
          setError(err?.message || 'Failed to load dashboard');
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  const onRefresh = () => {
    setRefreshing(true);
    loadData();
  };

  return (
    <View style={styles.container}>
      {/* Header */}
      <View style={styles.header}>
        <View>
          <Text style={styles.greeting}>Welcome, {user?.name || 'Fleet Manager'}</Text>
          <View style={styles.badgeRow}>
            <View style={styles.managerBadge}>
              <Text style={styles.managerBadgeText}>MANAGER COCKPIT</Text>
            </View>
          </View>
        </View>
        <TouchableOpacity
          style={styles.modeSwitchButton}
          onPress={toggleMode}
          testID="switch-to-driver-mode"
          accessibilityLabel="Switch to Driver Mode"
        >
          <MaterialCommunityIcons name="steering" size={16} color="#ffffff" />
          <Text style={styles.modeSwitchText}>Driver Mode</Text>
        </TouchableOpacity>
      </View>

      <ScrollView
        contentContainerStyle={styles.scrollContent}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={[Colors.primary]} />}
      >
        {loading ? (
          <View style={styles.centerContainer}>
            <ActivityIndicator size="large" color={Colors.primary} />
            <Text style={styles.loadingText}>Loading Fleet Operations...</Text>
          </View>
        ) : error ? (
          <View style={styles.errorCard}>
            <MaterialCommunityIcons name="alert-circle-outline" size={32} color={Colors.danger} />
            <Text style={styles.errorText}>{error}</Text>
            <TouchableOpacity style={styles.retryButton} onPress={loadData}>
              <Text style={styles.retryButtonText}>Retry</Text>
            </TouchableOpacity>
          </View>
        ) : (
          <>
            {/* Primary KPI Cards */}
            <Text style={styles.sectionTitle}>Fleet Overview Today</Text>
            <View style={styles.kpiGrid}>
              <TouchableOpacity
                style={[styles.kpiCard, { borderLeftColor: Colors.primary }]}
                onPress={() => onNavigateTab('trips')}
                activeOpacity={0.8}
                testID="kpi-card-active-trips"
              >
                <View style={styles.kpiHeader}>
                  <Text style={styles.kpiLabel}>Active Trips</Text>
                  <MaterialCommunityIcons name="truck-delivery" size={20} color={Colors.primary} />
                </View>
                <Text style={styles.kpiValue}>{kpis?.active_trips_count ?? 0}</Text>
                <Text style={styles.kpiSub}>In transit right now</Text>
              </TouchableOpacity>

              <TouchableOpacity
                style={[styles.kpiCard, { borderLeftColor: Colors.success }]}
                onPress={() => onNavigateTab('fleet')}
                activeOpacity={0.8}
                testID="kpi-card-available-vehicles"
              >
                <View style={styles.kpiHeader}>
                  <Text style={styles.kpiLabel}>Available Fleet</Text>
                  <MaterialCommunityIcons name="truck-check" size={20} color={Colors.success} />
                </View>
                <Text style={styles.kpiValue}>{kpis?.available_vehicles_count ?? 0}</Text>
                <Text style={styles.kpiSub}>Ready for assignment</Text>
              </TouchableOpacity>

              <TouchableOpacity
                style={[styles.kpiCard, { borderLeftColor: Colors.warning }]}
                onPress={() => onNavigateTab('approvals')}
                activeOpacity={0.8}
                testID="kpi-card-pending-approvals"
              >
                <View style={styles.kpiHeader}>
                  <Text style={styles.kpiLabel}>Kharcha Pending</Text>
                  <MaterialCommunityIcons name="cash-clock" size={20} color={Colors.warning} />
                </View>
                <Text style={styles.kpiValue}>{kpis?.attention?.pending_kharcha ?? 0}</Text>
                <Text style={styles.kpiSub}>Claims awaiting review</Text>
              </TouchableOpacity>

              <View style={[styles.kpiCard, { borderLeftColor: Colors.info }]}>
                <View style={styles.kpiHeader}>
                  <Text style={styles.kpiLabel}>Monthly Revenue</Text>
                  <MaterialCommunityIcons name="currency-inr" size={20} color={Colors.info} />
                </View>
                <Text style={styles.kpiValue}>
                  ₹{(kpis?.monthly_revenue ?? 0).toLocaleString('en-IN', { maximumFractionDigits: 0 })}
                </Text>
                <Text style={styles.kpiSub}>Completed trip billing</Text>
              </View>
            </View>

            {/* Attention Strip */}
            <Text style={styles.sectionTitle}>Actionable Attention</Text>
            <View style={styles.attentionContainer}>
              <View style={styles.attentionRow}>
                <View style={styles.attentionItem}>
                  <MaterialCommunityIcons name="alert-octagon" size={18} color={Colors.danger} />
                  <Text style={styles.attentionLabel}>Open Alerts</Text>
                  <Text style={[styles.attentionCount, { color: Colors.danger }]}>
                    {kpis?.attention?.open_alerts ?? 0}
                  </Text>
                </View>
                <View style={styles.attentionDivider} />
                <View style={styles.attentionItem}>
                  <MaterialCommunityIcons name="wrench-clock" size={18} color={Colors.warningText} />
                  <Text style={styles.attentionLabel}>Maintenance Due</Text>
                  <Text style={[styles.attentionCount, { color: Colors.warningText }]}>
                    {kpis?.attention?.maintenance_due ?? 0}
                  </Text>
                </View>
              </View>

              <View style={styles.attentionRow}>
                <View style={styles.attentionItem}>
                  <MaterialCommunityIcons name="tag-outline" size={18} color={Colors.info} />
                  <Text style={styles.attentionLabel}>Low FASTag</Text>
                  <Text style={[styles.attentionCount, { color: Colors.info }]}>
                    {kpis?.attention?.low_fastag ?? 0}
                  </Text>
                </View>
                <View style={styles.attentionDivider} />
                <View style={styles.attentionItem}>
                  <MaterialCommunityIcons name="file-clock-outline" size={18} color="#854d0e" />
                  <Text style={styles.attentionLabel}>Expiring EWBs</Text>
                  <Text style={[styles.attentionCount, { color: '#854d0e' }]}>
                    {kpis?.attention?.expiring_ewaybills ?? 0}
                  </Text>
                </View>
              </View>
            </View>

            {/* Quick Action Navigation */}
            <Text style={styles.sectionTitle}>Operations Quick Jump</Text>
            <View style={styles.actionsContainer}>
              <TouchableOpacity
                style={styles.actionBtn}
                onPress={() => onNavigateTab('trips')}
                activeOpacity={0.8}
              >
                <View style={[styles.actionIconBox, { backgroundColor: '#e0f2fe' }]}>
                  <MaterialCommunityIcons name="radar" size={24} color="#0284c7" />
                </View>
                <View style={styles.actionTextBox}>
                  <Text style={styles.actionTitle}>Control Tower Live Trips</Text>
                  <Text style={styles.actionDesc}>Real-time GPS tracking, stops & delay alerts</Text>
                </View>
                <MaterialCommunityIcons name="chevron-right" size={20} color="#9ca3af" />
              </TouchableOpacity>

              <TouchableOpacity
                style={styles.actionBtn}
                onPress={() => onNavigateTab('fleet')}
                activeOpacity={0.8}
              >
                <View style={[styles.actionIconBox, { backgroundColor: '#dcf8c6' }]}>
                  <MaterialCommunityIcons name="truck-fast" size={24} color="#008069" />
                </View>
                <View style={styles.actionTextBox}>
                  <Text style={styles.actionTitle}>Fleet Assets & Status</Text>
                  <Text style={styles.actionDesc}>Vehicle availability, health & mileage</Text>
                </View>
                <MaterialCommunityIcons name="chevron-right" size={20} color="#9ca3af" />
              </TouchableOpacity>

              <TouchableOpacity
                style={styles.actionBtn}
                onPress={() => onNavigateTab('approvals')}
                activeOpacity={0.8}
              >
                <View style={[styles.actionIconBox, { backgroundColor: '#fef3c7' }]}>
                  <MaterialCommunityIcons name="clipboard-check-outline" size={24} color="#b45309" />
                </View>
                <View style={styles.actionTextBox}>
                  <Text style={styles.actionTitle}>Driver Kharcha Approvals</Text>
                  <Text style={styles.actionDesc}>Review claims, receipts & audit discrepancies</Text>
                </View>
                <MaterialCommunityIcons name="chevron-right" size={20} color="#9ca3af" />
              </TouchableOpacity>
            </View>
          </>
        )}
      </ScrollView>
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
    paddingBottom: 20,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  greeting: {
    fontSize: 18,
    fontWeight: '700',
    color: '#ffffff',
  },
  badgeRow: {
    flexDirection: 'row',
    marginTop: 4,
  },
  managerBadge: {
    backgroundColor: '#25d366',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 4,
  },
  managerBadgeText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#075e54',
  },
  modeSwitchButton: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: 'rgba(255,255,255,0.2)',
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 20,
    gap: 6,
  },
  modeSwitchText: {
    fontSize: 12,
    fontWeight: '600',
    color: '#ffffff',
  },
  scrollContent: {
    padding: 16,
    paddingBottom: 40,
  },
  centerContainer: {
    paddingVertical: 60,
    alignItems: 'center',
  },
  loadingText: {
    marginTop: 12,
    fontSize: 14,
    color: '#64748b',
  },
  errorCard: {
    backgroundColor: '#fee2e2',
    padding: 20,
    borderRadius: Radius?.md || 8,
    alignItems: 'center',
    marginVertical: 20,
  },
  errorText: {
    color: '#b91c1c',
    fontSize: 14,
    marginTop: 8,
    textAlign: 'center',
  },
  retryButton: {
    marginTop: 12,
    backgroundColor: Colors.danger,
    paddingHorizontal: 20,
    paddingVertical: 8,
    borderRadius: Radius?.sm || 6,
  },
  retryButtonText: {
    color: '#ffffff',
    fontWeight: '600',
  },
  sectionTitle: {
    fontSize: 15,
    fontWeight: '700',
    color: '#1e293b',
    marginTop: 16,
    marginBottom: 10,
    textTransform: 'uppercase',
    letterSpacing: 0.5,
  },
  kpiGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 12,
  },
  kpiCard: {
    flexBasis: '48%',
    flexGrow: 1,
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 14,
    borderLeftWidth: 4,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 3,
    elevation: 2,
  },
  kpiHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  kpiLabel: {
    fontSize: 12,
    fontWeight: '600',
    color: '#64748b',
  },
  kpiValue: {
    fontSize: 22,
    fontWeight: '800',
    color: '#0f172a',
    marginTop: 6,
  },
  kpiSub: {
    fontSize: 11,
    color: '#94a3b8',
    marginTop: 2,
  },
  attentionContainer: {
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 12,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 3,
    elevation: 2,
  },
  attentionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 8,
  },
  attentionItem: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  attentionLabel: {
    fontSize: 13,
    color: '#334155',
    flex: 1,
  },
  attentionCount: {
    fontSize: 14,
    fontWeight: '700',
    marginRight: 8,
  },
  attentionDivider: {
    width: 1,
    height: 20,
    backgroundColor: '#e2e8f0',
    marginHorizontal: 8,
  },
  actionsContainer: {
    gap: 10,
  },
  actionBtn: {
    backgroundColor: '#ffffff',
    borderRadius: 10,
    padding: 14,
    flexDirection: 'row',
    alignItems: 'center',
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.05,
    shadowRadius: 3,
    elevation: 2,
  },
  actionIconBox: {
    width: 44,
    height: 44,
    borderRadius: 8,
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: 12,
  },
  actionTextBox: {
    flex: 1,
  },
  actionTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: '#0f172a',
  },
  actionDesc: {
    fontSize: 12,
    color: '#64748b',
    marginTop: 2,
  },
});
