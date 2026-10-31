import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, FlatList, TextInput, TouchableOpacity, RefreshControl, ActivityIndicator } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { Colors } from '../../constants/theme';
import { managerService, ManagerVehicle } from '../../services/managerService';

export function ManagerFleetScreen() {
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [vehicles, setVehicles] = useState<ManagerVehicle[]>([]);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | 'available' | 'in_transit' | 'maintenance'>('all');

  const loadVehicles = async () => {
    try {
      const data = await managerService.fetchVehicles();
      setVehicles(data);
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
        const data = await managerService.fetchVehicles();
        if (active) {
          setVehicles(data);
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

  const filteredVehicles = vehicles.filter((v) => {
    const reg = (v.registration_number || v.vehicle_number || '').toLowerCase();
    const type = (v.vehicle_type || '').toLowerCase();
    const matchesSearch = reg.includes(search.toLowerCase()) || type.includes(search.toLowerCase());

    if (!matchesSearch) return false;
    if (statusFilter === 'all') return true;
    const s = (v.status || '').toLowerCase();
    if (statusFilter === 'available') return s === 'available';
    if (statusFilter === 'in_transit') return s === 'in_transit' || s === 'running';
    if (statusFilter === 'maintenance') return s === 'maintenance' || s === 'repair';
    return true;
  });

  const getStatusColor = (status: string) => {
    const s = (status || '').toLowerCase();
    if (s === 'available') return { bg: '#dcf8c6', text: '#059669', label: 'AVAILABLE' };
    if (s === 'in_transit' || s === 'running') return { bg: '#e0f2fe', text: '#0284c7', label: 'IN TRANSIT' };
    if (s === 'maintenance' || s === 'repair') return { bg: '#fee2e2', text: '#b91c1c', label: 'MAINTENANCE' };
    return { bg: '#f1f5f9', text: '#64748b', label: s.toUpperCase() || 'IDLE' };
  };

  return (
    <View style={styles.container}>
      {/* Search Bar */}
      <View style={styles.searchHeader}>
        <View style={styles.searchBox}>
          <MaterialCommunityIcons name="magnify" size={20} color="#64748b" />
          <TextInput
            style={styles.searchInput}
            placeholder="Search vehicle number or type..."
            placeholderTextColor="#94a3b8"
            value={search}
            onChangeText={setSearch}
            testID="fleet-search-input"
          />
          {search ? (
            <TouchableOpacity onPress={() => setSearch('')}>
              <MaterialCommunityIcons name="close-circle" size={18} color="#94a3b8" />
            </TouchableOpacity>
          ) : null}
        </View>

        {/* Filter Pills */}
        <View style={styles.filterRow}>
          {(['all', 'available', 'in_transit', 'maintenance'] as const).map((filter) => (
            <TouchableOpacity
              key={filter}
              style={[styles.filterPill, statusFilter === filter && styles.activeFilterPill]}
              onPress={() => setStatusFilter(filter)}
            >
              <Text style={[styles.filterText, statusFilter === filter && styles.activeFilterText]}>
                {filter === 'all'
                  ? 'All Fleet'
                  : filter === 'in_transit'
                  ? 'In Transit'
                  : filter.charAt(0).toUpperCase() + filter.slice(1)}
              </Text>
            </TouchableOpacity>
          ))}
        </View>
      </View>

      {/* Vehicles List */}
      {loading ? (
        <View style={styles.centerBox}>
          <ActivityIndicator size="large" color={Colors.primary} />
          <Text style={styles.loadingText}>Fetching fleet assets...</Text>
        </View>
      ) : filteredVehicles.length === 0 ? (
        <View style={styles.emptyBox}>
          <MaterialCommunityIcons name="truck-outline" size={48} color="#cbd5e1" />
          <Text style={styles.emptyTitle}>No Vehicles Found</Text>
          <Text style={styles.emptySubtitle}>Try adjusting your search or status filter.</Text>
        </View>
      ) : (
        <FlatList
          data={filteredVehicles}
          keyExtractor={(item) => item.id || item.registration_number}
          contentContainerStyle={styles.listContent}
          refreshControl={<RefreshControl refreshing={refreshing} onRefresh={loadVehicles} colors={[Colors.primary]} />}
          renderItem={({ item }) => {
            const badge = getStatusColor(item.status);
            return (
              <View style={styles.vehicleCard} testID={`vehicle-card-${item.registration_number}`}>
                <View style={styles.cardTop}>
                  <View style={styles.regBox}>
                    <MaterialCommunityIcons name="truck" size={22} color="#075e54" />
                    <Text style={styles.regNumber}>{item.registration_number || item.vehicle_number || 'N/A'}</Text>
                  </View>
                  <View style={[styles.statusBadge, { backgroundColor: badge.bg }]}>
                    <Text style={[styles.statusText, { color: badge.text }]}>{badge.label}</Text>
                  </View>
                </View>

                <View style={styles.metaRow}>
                  <View style={styles.metaItem}>
                    <Text style={styles.metaLabel}>Type</Text>
                    <Text style={styles.metaValue}>{item.vehicle_type || 'Commercial Truck'}</Text>
                  </View>
                  <View style={styles.metaItem}>
                    <Text style={styles.metaLabel}>Fuel</Text>
                    <Text style={styles.metaValue}>{(item.fuel_type || 'Diesel').toUpperCase()}</Text>
                  </View>
                  <View style={styles.metaItem}>
                    <Text style={styles.metaLabel}>Odometer</Text>
                    <Text style={styles.metaValue}>
                      {item.current_mileage ? `${item.current_mileage.toLocaleString()} KM` : '—'}
                    </Text>
                  </View>
                </View>
              </View>
            );
          }}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f8fafc',
  },
  searchHeader: {
    backgroundColor: '#075e54',
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 16,
  },
  searchBox: {
    backgroundColor: '#ffffff',
    borderRadius: 8,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 12,
    height: 44,
  },
  searchInput: {
    flex: 1,
    paddingHorizontal: 8,
    fontSize: 14,
    color: '#1e293b',
  },
  filterRow: {
    flexDirection: 'row',
    marginTop: 12,
    gap: 8,
  },
  filterPill: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 16,
    backgroundColor: 'rgba(255,255,255,0.15)',
  },
  activeFilterPill: {
    backgroundColor: '#25d366',
  },
  filterText: {
    fontSize: 12,
    fontWeight: '600',
    color: '#ffffff',
  },
  activeFilterText: {
    color: '#075e54',
    fontWeight: '800',
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
    fontSize: 16,
    fontWeight: '700',
    color: '#475569',
    marginTop: 12,
  },
  emptySubtitle: {
    fontSize: 13,
    color: '#94a3b8',
    marginTop: 4,
    textAlign: 'center',
  },
  listContent: {
    padding: 16,
    gap: 12,
  },
  vehicleCard: {
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
  regBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  regNumber: {
    fontSize: 16,
    fontWeight: '800',
    color: '#0f172a',
  },
  statusBadge: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
  },
  statusText: {
    fontSize: 11,
    fontWeight: '800',
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingTop: 10,
  },
  metaItem: {
    flex: 1,
  },
  metaLabel: {
    fontSize: 11,
    color: '#94a3b8',
    textTransform: 'uppercase',
  },
  metaValue: {
    fontSize: 13,
    fontWeight: '600',
    color: '#334155',
    marginTop: 2,
  },
});
