import React, { useState } from 'react';
import { View, StyleSheet, TouchableOpacity, Text } from 'react-native';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { ManagerDashboardScreen } from './ManagerDashboardScreen';
import { ManagerFleetScreen } from './ManagerFleetScreen';
import { ManagerTripsScreen } from './ManagerTripsScreen';
import { ManagerApprovalsScreen } from './ManagerApprovalsScreen';

type ManagerTab = 'dashboard' | 'fleet' | 'trips' | 'approvals';

export function ManagerShell() {
  const [activeTab, setActiveTab] = useState<ManagerTab>('dashboard');

  return (
    <View style={styles.container}>
      <View style={styles.screenContainer}>
        {activeTab === 'dashboard' && <ManagerDashboardScreen onNavigateTab={setActiveTab} />}
        {activeTab === 'fleet' && <ManagerFleetScreen />}
        {activeTab === 'trips' && <ManagerTripsScreen />}
        {activeTab === 'approvals' && <ManagerApprovalsScreen />}
      </View>

      {/* Manager Bottom Tabs */}
      <View style={styles.bottomBar}>
        <TouchableOpacity
          style={styles.tabItem}
          onPress={() => setActiveTab('dashboard')}
          testID="tab-manager-dashboard"
        >
          <MaterialCommunityIcons
            name="view-dashboard-outline"
            size={22}
            color={activeTab === 'dashboard' ? '#25d366' : '#94a3b8'}
          />
          <Text style={[styles.tabLabel, activeTab === 'dashboard' && styles.activeTabLabel]}>
            Dashboard
          </Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.tabItem}
          onPress={() => setActiveTab('fleet')}
          testID="tab-manager-fleet"
        >
          <MaterialCommunityIcons
            name="truck-outline"
            size={22}
            color={activeTab === 'fleet' ? '#25d366' : '#94a3b8'}
          />
          <Text style={[styles.tabLabel, activeTab === 'fleet' && styles.activeTabLabel]}>
            Fleet
          </Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.tabItem}
          onPress={() => setActiveTab('trips')}
          testID="tab-manager-trips"
        >
          <MaterialCommunityIcons
            name="radar"
            size={22}
            color={activeTab === 'trips' ? '#25d366' : '#94a3b8'}
          />
          <Text style={[styles.tabLabel, activeTab === 'trips' && styles.activeTabLabel]}>
            Live Trips
          </Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.tabItem}
          onPress={() => setActiveTab('approvals')}
          testID="tab-manager-approvals"
        >
          <MaterialCommunityIcons
            name="clipboard-check-outline"
            size={22}
            color={activeTab === 'approvals' ? '#25d366' : '#94a3b8'}
          />
          <Text style={[styles.tabLabel, activeTab === 'approvals' && styles.activeTabLabel]}>
            Approvals
          </Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#075e54',
  },
  screenContainer: {
    flex: 1,
  },
  bottomBar: {
    flexDirection: 'row',
    height: 60,
    backgroundColor: '#075e54',
    borderTopWidth: 1,
    borderTopColor: '#0b4e46',
    alignItems: 'center',
    paddingBottom: 4,
  },
  tabItem: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 2,
  },
  tabLabel: {
    fontSize: 11,
    fontWeight: '600',
    color: '#94a3b8',
  },
  activeTabLabel: {
    color: '#25d366',
    fontWeight: '700',
  },
});
