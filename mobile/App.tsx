import 'react-native-gesture-handler';
import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, View, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import { NavigationContainer } from '@react-navigation/native';
import { createStackNavigator } from '@react-navigation/stack';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { Colors, Font, Radius, Spacing } from './src/constants/theme';
import { getApiBaseURL } from './src/constants/network';
import { TripCard, SkeletonLoader } from './src/components/TripCard';
import { SplashScreen } from './src/components/SplashScreen';
import { GetStartedScreen } from './src/components/GetStartedScreen';
import { OnboardingOverviewScreen } from './src/components/OnboardingOverviewScreen';
import { BookingScheduleScreen } from './src/components/BookingScheduleScreen';
import { LoginScreen } from './src/components/LoginScreen';
import { RegisterScreen } from './src/components/RegisterScreen';
import { ForgotPasswordScreen } from './src/components/ForgotPasswordScreen';
import { FirstTimeSetupScreen } from './src/components/FirstTimeSetupScreen';
import { DeliveryVerificationScreen } from './src/components/DeliveryVerificationScreen';
import { ActiveNavigationScreen } from './src/components/ActiveNavigationScreen';
import { ExpenseScreen } from './src/components/ExpenseScreen';
import { ProfileScreen } from './src/components/ProfileScreen';
import { IssuesScreen } from './src/components/IssuesScreen';
import { DB } from './src/services/storage';
import { Telemetry } from './src/services/telemetry';
import { Analytics } from './src/services/analytics';
import { MQTT } from './src/services/mqtt';
import { SyncEngine, startNetworkWatcher, stopNetworkWatcher } from './src/services/syncEngine';
import { TripPoller } from './src/services/tripPoller';
import { OfflineQueue } from './src/services/offlineQueue';
import ConsentManager from './src/services/consentManager';
import { SyncStatusBar } from './src/components/SyncStatusBar';
import { ComplianceBanner } from './src/components/ComplianceBanner';
import { PaisaScreen } from './src/components/PaisaScreen';
import { QRDemoScreen } from './src/components/QRDemoScreen';
import { useAuthStore } from './src/stores/authStore';
import { useSyncStore } from './src/stores/syncStore';
import { Trip } from './src/types/api';
import { mapTripStatus, RawTrip } from './src/utils/tripMapper';

const queryClient = new QueryClient();

type AuthStackParamList = {
  Splash: undefined;
  GetStarted: undefined;
  OnboardingOverview: undefined;
  BookingSchedule: undefined;
  Login: undefined;
  Register: undefined;
  ForgotPassword: undefined;
  QRDemo: undefined;
};

type DriverStackParamList = {
  Main: undefined;
  FirstTimeSetup: undefined;
  ActiveNavigation: { tripId: string; trip?: Trip } | undefined;
  DeliveryVerification: { tripId: string } | undefined;
  Expenses: { tripId?: string } | undefined;
  Profile: undefined;
  Issues: { tripId?: string } | undefined;
};

const AuthStack = createStackNavigator<AuthStackParamList>();
const DriverStack = createStackNavigator<DriverStackParamList>();

function AuthNavigator() {
  return (
    <AuthStack.Navigator initialRouteName="Splash" screenOptions={{ headerShown: false }}>
      <AuthStack.Screen name="Splash">
        {({ navigation }) => <SplashScreen onFinish={() => navigation.navigate('GetStarted')} />}
      </AuthStack.Screen>
      <AuthStack.Screen name="GetStarted">
        {({ navigation }) => (
          <GetStartedScreen
            onGetStarted={() => navigation.navigate('OnboardingOverview')}
            onSignIn={() => navigation.navigate('Login')}
            onOpenQRDemo={() => navigation.navigate('QRDemo')}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="QRDemo">
        {() => <QRDemoScreen />}
      </AuthStack.Screen>
      <AuthStack.Screen name="OnboardingOverview">
        {({ navigation }) => (
          <OnboardingOverviewScreen
            onNext={() => navigation.navigate('BookingSchedule')}
            onSkip={() => navigation.navigate('Login')}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="BookingSchedule">
        {({ navigation }) => (
          <BookingScheduleScreen
            onNext={() => navigation.navigate('Login')}
            onBack={() => navigation.goBack()}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="Login">
        {({ navigation }) => (
          <LoginScreen
            onLoginSuccess={() => {}}
            onForgotPassword={() => navigation.navigate('ForgotPassword')}
            onRegisterLink={() => navigation.navigate('Register')}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="Register">
        {({ navigation }) => (
          <RegisterScreen
            onRegisterSuccess={() => {}}
            onBackToLogin={() => navigation.navigate('Login')}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="ForgotPassword">
        {({ navigation }) => (
          <ForgotPasswordScreen onBackToLogin={() => navigation.navigate('Login')} />
        )}
      </AuthStack.Screen>
    </AuthStack.Navigator>
  );
}

function DriverNavigator() {
  return (
    <DriverStack.Navigator screenOptions={{ headerShown: false }}>
      <DriverStack.Screen name="Main">
        {({ navigation }) => (
          <MainScreen
            onOpenSetup={() => navigation.navigate('FirstTimeSetup')}
            onStartNav={(trip) => navigation.navigate('ActiveNavigation', { tripId: trip.id, trip })}
            onOpenExpenses={(tripId) => navigation.navigate('Expenses', { tripId })}
            onOpenProfile={() => navigation.navigate('Profile')}
            onOpenIssues={() => navigation.navigate('Issues', {})}
          />
        )}
      </DriverStack.Screen>
      <DriverStack.Screen name="FirstTimeSetup">
        {({ navigation }) => (
          <FirstTimeSetupScreen
            onCompleteSetup={() => navigation.navigate('ActiveNavigation')}
            onBack={() => navigation.goBack()}
          />
        )}
      </DriverStack.Screen>
      <DriverStack.Screen name="ActiveNavigation">
        {({ navigation, route }) => (
          <ActiveNavigationScreen
            tripId={route.params?.tripId}
            trip={route.params?.trip}
            onArriveAtStop={() => {
              if (!route.params?.tripId) {
                Alert.alert('No Trip Selected', 'Open a trip from the trip list first.');
                return;
              }
              navigation.navigate('DeliveryVerification', { tripId: route.params.tripId });
            }}
            onMenuToggle={() => navigation.navigate('Main')}
          />
        )}
      </DriverStack.Screen>
      <DriverStack.Screen name="DeliveryVerification">
        {({ navigation, route }) => (
          <DeliveryVerificationScreen
            tripId={route.params?.tripId}
            onComplete={() => navigation.navigate('Main')}
            onBack={() => navigation.goBack()}
          />
        )}
      </DriverStack.Screen>
      <DriverStack.Screen name="Issues">
        {({ navigation, route }) => (
          <IssuesScreen tripId={route.params?.tripId} onBack={() => navigation.goBack()} />
        )}
      </DriverStack.Screen>
      <DriverStack.Screen name="Profile">
        {({ navigation }) => <ProfileScreen onBack={() => navigation.goBack()} />}
      </DriverStack.Screen>
      <DriverStack.Screen name="Expenses">
        {({ navigation, route }) => (
          <ExpenseScreen
            tripId={route.params?.tripId}
            onComplete={() => navigation.navigate('Main')}
            onBack={() => navigation.goBack()}
          />
        )}
      </DriverStack.Screen>
    </DriverStack.Navigator>
  );
}

export default function App() {
  const { isAuthenticated, isLoading, loadSession } = useAuthStore();

  useEffect(() => {
    loadSession();
    OfflineQueue.init().catch(() => {});
    ConsentManager.init().catch(() => {});
  }, []);

  useEffect(() => {
    if (isAuthenticated) {
      startNetworkWatcher();
    }
    return () => {
      stopNetworkWatcher();
    };
  }, [isAuthenticated]);

  if (isLoading) {
    return <SplashScreen onFinish={() => {}} />;
  }

  return (
    <SafeAreaProvider>
      <QueryClientProvider client={queryClient}>
        <StatusBar style="light" />
        <NavigationContainer>
          {isAuthenticated ? (
            <>
              <SyncStatusBar />
              <DriverNavigator />
            </>
          ) : (
            <AuthNavigator />
          )}
        </NavigationContainer>
      </QueryClientProvider>
    </SafeAreaProvider>
  );
}

interface MainScreenProps {
  onOpenSetup?: () => void;
  onStartNav?: (trip: Trip) => void;
  onOpenExpenses?: (tripId?: string) => void;
  onOpenProfile?: () => void;
  onOpenIssues?: () => void;
}

function MainScreen({ onOpenSetup, onStartNav, onOpenExpenses, onOpenProfile, onOpenIssues }: MainScreenProps) {
  const [activeTab, setActiveTab] = useState<'trips' | 'dispatch' | 'paisa' | 'qr'>('trips');
  const [tripFilter, setTripFilter] = useState<'active' | 'history'>('active');
  const [locationState, setLocationState] = useState<{
    granted: boolean;
    latitude: number | null;
    longitude: number | null;
    error: string | null;
  }>({
    granted: false,
    latitude: null,
    longitude: null,
    error: null,
  });

  const { token, user, logout, loadSession } = useAuthStore();
  const driverIdentifier = user?.driverId || user?.id || '';

  useEffect(() => {
    Analytics.init();
    if (user?.id) {
      Analytics.identify(user.id, { role: 'fleet_driver', driver_id: user.driverId });
    }
    loadSession().then(() => {
      const activeId = user?.driverId || user?.id;
      if (activeId) {
        MQTT.connect(activeId);
        SyncEngine.startAutoSync(activeId, 15000);
        SyncEngine.flushOfflineQueues().catch(() => {});
        TripPoller.start(activeId);
      }
    });
    // In-app dispatch alerts (trip assignment/status pushed over MQTT)
    const unsubscribeDispatch = MQTT.onDispatch((u) => {
      Alert.alert('DISPATCH UPDATE', `Trip ${u.trip_id} · ${u.status || 'update'}`);
      queryClient.invalidateQueries({ queryKey: ['trips', driverIdentifier, token] });
    });
    // Auto-accepted trips (poller loop) refresh the list without user action
    const unsubscribeTripAccepted = TripPoller.onTripAccepted(() => {
      queryClient.invalidateQueries({ queryKey: ['trips', driverIdentifier, token] });
    });
    return () => {
      unsubscribeDispatch();
      unsubscribeTripAccepted();
      SyncEngine.stopAutoSync();
    };
  }, [user?.id, user?.driverId]);


  const handleRequestLocation = async () => {
    try {
      Analytics.track('driver_gps_permission_requested');
      const loc = await Telemetry.requestLocationPermission();

      if (loc.latitude == null || loc.longitude == null) {
        setLocationState({ granted: false, latitude: null, longitude: null, error: loc.error || 'GPS coordinates unavailable' });
        Alert.alert(
          'Location Unavailable',
          loc.error || 'GPS coordinates unavailable. Enable location services and try again.'
        );
        return;
      }

      const finalLoc = {
        granted: true,
        latitude: loc.latitude,
        longitude: loc.longitude,
        error: loc.error,
      };

      setLocationState(finalLoc);
      Analytics.track('driver_gps_location_acquired', { lat: finalLoc.latitude, lng: finalLoc.longitude });

      if (driverIdentifier) {
        MQTT.publishLocation(driverIdentifier, finalLoc.latitude, finalLoc.longitude);
      }

      Alert.alert(
        'GPS Access Granted',
        `Latitude: ${finalLoc.latitude.toFixed(4)}, Longitude: ${finalLoc.longitude.toFixed(4)}\nStreamed over MQTT & Saved to SQLite.`
      );

      Telemetry.startLiveLocationTracking((lat, lng) => {
        setLocationState((prev) => ({ ...prev, latitude: lat, longitude: lng }));
        if (driverIdentifier) {
          MQTT.publishLocation(driverIdentifier, lat, lng);
        }
      });
    } catch (e: any) {
      Analytics.track('driver_gps_error', { error: e.message });
      Alert.alert('Location Error', e.message || 'Failed to request location');
    }
  };






  const handleSignOut = () => {
    // Full teardown: no live listeners may survive a logout.
    TripPoller.stop();
    MQTT.disconnect();
    Telemetry.stopLiveLocationTracking();
    SyncEngine.stopAutoSync();
    stopNetworkWatcher();
    useSyncStore.getState().markSynced();
    useSyncStore.getState().setPendingCount(0);
    logout();
  };

  const { data: trips, isLoading } = useQuery<Trip[]>({
    queryKey: ['trips', driverIdentifier, token],
    queryFn: async () => {
      if (!token) return [];
      try {
        const res = await fetch(
          `${getApiBaseURL()}/api/v1/trips?driver_id=me&page=1&limit=50`,
          { headers: { Authorization: `Bearer ${token}` } }
        );
        if (res.ok) {
          const json = await res.json();
          const mapped = ((json.trips as RawTrip[]) || []).map(mapTripStatus);
          if (mapped.length > 0) {
            await DB.saveTrips(mapped);
          }
          return mapped;
        }
      } catch (err) {
        console.log('[TRIP FETCH WARNING]', err);
      }
      return await DB.getTrips();
    },
    enabled: !!token,
  });

  // Trip context for map labels + expense logging: prefer an in-transit trip.
  const activeTrip =
    trips?.find((t) => t.status === 'IN_TRANSIT') ?? trips?.find((t) => t.status === 'PENDING') ?? null;
  // Backend trips expose vehiclePlate only today; surface vehicleId once added.
  const activeVehicleId = activeTrip
    ? ((activeTrip as unknown as { vehicleId?: string }).vehicleId ?? null)
    : null;

  return (
    <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
      <StatusBar style="light" />

      {/* Header with Avandab Brand Colors */}
      <View style={styles.header}>
        <View style={styles.headerTopRow}>
          <View style={styles.brandBadge}>
            <View style={styles.brandDot} />
            <Text style={styles.brandBadgeText}>AVANDAB · OPS</Text>
          </View>
          <TouchableOpacity onPress={handleSignOut}>
            <Text style={styles.headerClock}>SIGN OUT</Text>
          </TouchableOpacity>
        </View>
        <Text style={styles.headerTitle}>FLEET MOBILE</Text>
        <Text style={styles.headerSubtitle}>
          {user ? `${user.name.toUpperCase()} · ${user.driverId || user.id}` : 'LIVE DISPATCH & TRIP MGMT'}
        </Text>
      </View>

      {/* Non-Blocking Setup Prompt Banner */}
      <View style={styles.bannerContainer}>
        <View style={styles.bannerIconBox}>
          <MaterialCommunityIcons name="clipboard-alert-outline" size={14} color={Colors.warning} />
        </View>
        <View style={styles.bannerTextContainer}>
          <Text style={styles.bannerTitle}>PROFILE SETUP INCOMPLETE</Text>
          <Text style={styles.bannerSub}>Bank details · Profile photo · Driver docs</Text>
        </View>
        <TouchableOpacity
          style={styles.bannerBtn}
          activeOpacity={0.85}
          onPress={() => onOpenSetup && onOpenSetup()}
        >
          <Text style={styles.bannerBtnText}>SETUP</Text>
        </TouchableOpacity>
      </View>

      {/* Vehicle compliance banner (renders only when a vehicle id is resolvable) */}
      <View style={styles.complianceContainer}>
        <ComplianceBanner vehicleId={activeVehicleId} />
      </View>

      {/* Tabs */}
      <View style={styles.tabContainer}>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'trips' && styles.activeTab]}
          onPress={() => setActiveTab('trips')}
        >
          <Text style={[styles.tabText, activeTab === 'trips' && styles.activeTabText]}>TRIPS</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'dispatch' && styles.activeTab]}
          onPress={() => setActiveTab('dispatch')}
        >
          <Text style={[styles.tabText, activeTab === 'dispatch' && styles.activeTabText]}>DISPATCH</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'paisa' && styles.activeTab]}
          onPress={() => setActiveTab('paisa')}
        >
          <Text style={[styles.tabText, activeTab === 'paisa' && styles.activeTabText]}>PAISA</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'qr' && styles.activeTab]}
          onPress={() => setActiveTab('qr')}
        >
          <Text style={[styles.tabText, activeTab === 'qr' && styles.activeTabText]}>QR</Text>
        </TouchableOpacity>
      </View>

      {/* Trip filter chips (trips tab only) */}
      {activeTab === 'trips' && (
        <View style={styles.filterRow}>
          {(['active', 'history'] as const).map((f) => (
            <TouchableOpacity
              key={f}
              style={[styles.filterChip, tripFilter === f && styles.filterChipActive]}
              onPress={() => setTripFilter(f)}
            >
              <Text style={[styles.filterChipText, tripFilter === f && styles.filterChipTextActive]}>
                {f === 'active' ? 'ACTIVE' : 'HISTORY'}
              </Text>
            </TouchableOpacity>
          ))}
        </View>
      )}

      {/* Content */}
      {activeTab === 'paisa' && (
        <View style={{ flex: 1, paddingHorizontal: 12 }}>
          <PaisaScreen tripId={undefined} />
        </View>
      )}
      {activeTab === 'qr' && <QRDemoScreen />}
      {activeTab !== 'paisa' && activeTab !== 'qr' && (
      <ScrollView style={styles.content} contentContainerStyle={styles.contentPadding}>
        {activeTab === 'trips' ? (
          isLoading ? (
            <>
              <SkeletonLoader />
              <SkeletonLoader />
            </>
          ) : (
            (() => {
              // ACTIVE: pending/in-progress work. HISTORY: delivered/completed/cancelled.
              const visibleTrips = (trips ?? []).filter((t) =>
                tripFilter === 'active'
                  ? t.status === 'PENDING' || t.status === 'IN_TRANSIT'
                  : t.status === 'COMPLETED' || t.status === 'CANCELLED'
              );
              if (visibleTrips.length === 0) {
                return (
                  <View style={styles.infoCard}>
                    <Text style={styles.infoTitle}>{tripFilter === 'active' ? 'NO ACTIVE TRIPS' : 'NO TRIP HISTORY'}</Text>
                    <Text style={styles.infoBody}>
                      {tripFilter === 'active'
                        ? 'You currently have no dispatched trips assigned. Contact dispatch or check back later.'
                        : 'Completed and cancelled trips will appear here.'}
                    </Text>
                  </View>
                );
              }
              return visibleTrips.map((trip) => (
                <TouchableOpacity key={trip.id} activeOpacity={0.9} onPress={() => onStartNav && onStartNav(trip)}>
                  <TripCard
                    tripNumber={trip.tripNumber}
                    driverName={trip.driverName}
                    vehiclePlate={trip.vehiclePlate}
                    origin={trip.origin}
                    destination={trip.destination}
                    status={trip.status}
                    startTime={trip.startTime}
                  />
                </TouchableOpacity>
              ));
            })()
          )
        ) : (
          <View style={{ gap: Spacing.md }}>
            {/* Active trip summary + quick actions - no debug telemetry */}
            {activeTrip ? (
              <View style={styles.infoCard}>
                <View style={styles.infoCardHeader}>
                  <Text style={styles.infoTitle}>ACTIVE TRIP</Text>
                  <Text style={styles.infoMeta}>{activeTrip.tripNumber}</Text>
                </View>
                <View style={styles.routeContainer}>
                  <View style={styles.routeRow}>
                    <View style={[styles.routeDot, styles.routeDotOrigin]} />
                    <Text style={styles.locationText} numberOfLines={1}>{activeTrip.origin}</Text>
                  </View>
                  <View style={styles.routeConnector} />
                  <View style={styles.routeRow}>
                    <View style={[styles.routeDot, styles.routeDotDest]} />
                    <Text style={styles.locationText} numberOfLines={1}>{activeTrip.destination}</Text>
                  </View>
                </View>
                <View style={{ flexDirection: 'row', gap: 8, marginTop: Spacing.md }}>
                  <TouchableOpacity
                    style={[styles.actionBtn, styles.actionBtnTeal, { flex: 1 }]}
                    onPress={() => onStartNav && onStartNav(activeTrip)}
                  >
                    <MaterialCommunityIcons name="navigation" size={14} color={Colors.textOnPrimary} />
                    <Text style={styles.actionBtnText}>NAVIGATE</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={[styles.actionBtn, { flex: 1, backgroundColor: Colors.surface, borderWidth: 1, borderColor: Colors.border }]}
                    onPress={() => onOpenExpenses && onOpenExpenses(activeTrip.id)}
                  >
                    <MaterialCommunityIcons name="receipt" size={14} color={Colors.primary} />
                    <Text style={[styles.actionBtnText, { color: Colors.primary }]}>EXPENSE</Text>
                  </TouchableOpacity>
                </View>
                <TouchableOpacity
                  style={[styles.actionBtn, { backgroundColor: Colors.surface, borderWidth: 1, borderColor: Colors.border, marginTop: 8 }]}
                  onPress={() => onOpenIssues && onOpenIssues()}
                >
                  <MaterialCommunityIcons name="alert-circle-outline" size={14} color={Colors.warning} />
                  <Text style={[styles.actionBtnText, { color: Colors.textPrimary }]}>REPORT ISSUE</Text>
                </TouchableOpacity>
              </View>
            ) : (
              <View style={styles.infoCard}>
                <Text style={styles.infoTitle}>NO ACTIVE TRIP</Text>
                <Text style={styles.infoBody}>You have no dispatched trips. Pull to refresh or contact dispatch.</Text>
              </View>
            )}

            {/* Minimal status - no raw LAT/LNG dump, no DB rows, no camera finder */}
            <View style={styles.infoCard}>
              <View style={styles.telemetryRow}>
                <Text style={styles.telemetryLabel}>GPS</Text>
                <View style={[styles.statusPill, locationState.granted ? styles.statusPillActive : styles.statusPillPending]}>
                  <View style={[styles.statusPillDot, { backgroundColor: locationState.granted ? Colors.success : Colors.warning }]} />
                  <Text style={[styles.telemetryValue, { color: locationState.granted ? Colors.success : Colors.warning }]}>
                    {locationState.granted ? 'ON' : 'OFF'}
                  </Text>
                </View>
              </View>
              {!locationState.granted && (
                <TouchableOpacity style={[styles.actionBtn, { marginTop: Spacing.sm }]} onPress={handleRequestLocation}>
                  <MaterialCommunityIcons name="crosshairs-gps" size={14} color={Colors.textOnPrimary} />
                  <Text style={styles.actionBtnText}>ENABLE LOCATION</Text>
                </TouchableOpacity>
              )}
              <Text style={[styles.hint, { marginTop: Spacing.sm }]}>Diagnostics & background tracking moved to Profile.</Text>
            </View>
          </View>
        )}
      </ScrollView>
      )}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  header: {
    backgroundColor: Colors.chrome,
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.md,
    paddingBottom: Spacing.lg,
  },
  headerTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 8,
  },
  brandBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    backgroundColor: Colors.chromeLight,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: Radius.sm,
  },
  brandDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#22c55e',
  },
  brandBadgeText: {
    color: Colors.textOnChrome,
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  headerClock: {
    color: Colors.textOnChromeMuted,
    fontSize: 10,
    fontWeight: '700',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  headerTitle: {
    color: Colors.textOnChrome,
    fontSize: 22,
    fontWeight: '900',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  headerSubtitle: {
    color: Colors.textOnChromeMuted,
    fontSize: 10,
    fontWeight: '600',
    letterSpacing: 1.5,
    marginTop: 2,
    fontFamily: Font.mono,
  },
  bannerContainer: {
    backgroundColor: '#fffbeb',
    borderBottomWidth: 1,
    borderBottomColor: '#fef3c7',
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  bannerIconBox: {
    width: 24,
    height: 24,
    borderRadius: Radius.sm,
    backgroundColor: '#fef3c7',
    justifyContent: 'center',
    alignItems: 'center',
  },
  bannerTextContainer: {
    flex: 1,
  },
  bannerTitle: {
    fontSize: 10,
    fontWeight: '800',
    color: '#92400e',
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
  bannerSub: {
    fontSize: 9,
    fontWeight: '500',
    color: '#b45309',
    marginTop: 1,
  },
  bannerBtn: {
    backgroundColor: Colors.warning,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: Radius.sm,
  },
  bannerBtnText: {
    fontSize: 9,
    fontWeight: '800',
    color: '#ffffff',
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
  complianceContainer: {
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.sm,
  },
  tabContainer: {
    flexDirection: 'row',
    backgroundColor: Colors.surface,
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  filterRow: {
    flexDirection: 'row',
    gap: 8,
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.sm,
  },
  filterChip: {
    paddingHorizontal: 12,
    paddingVertical: 5,
    borderRadius: 9999,
    borderWidth: 1,
    borderColor: Colors.border,
    backgroundColor: Colors.surface,
  },
  filterChipActive: {
    backgroundColor: Colors.primary,
    borderColor: Colors.primary,
  },
  filterChipText: {
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 1,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
  },
  filterChipTextActive: {
    color: Colors.textOnPrimary,
  },
  tab: {
    flex: 1,
    paddingVertical: Spacing.md,
    alignItems: 'center',
  },
  activeTab: {
    borderBottomWidth: 2,
    borderBottomColor: Colors.primary,
  },
  tabText: {
    fontSize: 11,
    fontWeight: '700',
    color: Colors.textSecondary,
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  activeTabText: {
    color: Colors.primary,
    fontWeight: '800',
  },
  content: {
    flex: 1,
  },
  contentPadding: {
    padding: Spacing.lg,
    gap: Spacing.md,
  },
  infoCard: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    padding: Spacing.lg,
    borderWidth: 1,
    borderColor: Colors.borderLight,
  },
  infoCardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 6,
  },
  infoTitle: {
    fontSize: 12,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  infoMeta: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  infoBody: {
    fontSize: 12,
    color: Colors.textSecondary,
    lineHeight: 18,
    marginBottom: Spacing.md,
  },
  telemetrySection: {
    gap: 8,
  },
  telemetryRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  telemetryLabel: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  statusPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 9999,
  },
  statusPillActive: {
    backgroundColor: Colors.successBg,
  },
  statusPillPending: {
    backgroundColor: Colors.warningBg,
  },
  statusPillDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
  },
  telemetryValue: {
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  gpsDisplayBox: {
    backgroundColor: Colors.surface,
    padding: 10,
    borderRadius: Radius.sm,
    gap: 4,
    borderWidth: 1,
    borderColor: Colors.borderLight,
  },
  gpsRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  gpsLabel: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  gpsValue: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  gpsSuccessText: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.success,
    letterSpacing: 0.5,
    fontFamily: Font.mono,
  },
  dbFetchBtn: {
    backgroundColor: Colors.info,
    paddingVertical: 8,
    paddingHorizontal: 10,
    borderRadius: Radius.sm,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    marginTop: 6,
  },
  dbSyncBtn: {
    backgroundColor: Colors.primary,
  },
  bgGpsOnBtn: {
    backgroundColor: Colors.success,
  },
  bgGpsOffBtn: {
    backgroundColor: Colors.chrome,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
  },
  dbFetchBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  dbLogsContainer: {
    marginTop: 10,
    paddingTop: 10,
    borderTopWidth: 1,
    borderTopColor: Colors.borderLight,
  },
  dbLogsTitle: {
    fontSize: 9,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1,
    marginBottom: 6,
    fontFamily: Font.mono,
  },
  dbLogRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 4,
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  dbLogId: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.primary,
    fontFamily: Font.mono,
  },
  dbLogCoords: {
    fontSize: 10,
    fontFamily: Font.mono,
    color: Colors.textPrimary,
  },
  dbLogTime: {
    fontSize: 10,
    color: Colors.textSecondary,
    fontFamily: Font.mono,
  },
  divider: {
    height: 1,
    backgroundColor: Colors.borderLight,
    marginVertical: Spacing.md,
  },
  routeContainer: {
    gap: 0,
  },
  routeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  routeDot: {
    width: 8,
    height: 8,
    borderRadius: 2,
  },
  routeDotOrigin: {
    backgroundColor: Colors.success,
  },
  routeDotDest: {
    backgroundColor: Colors.danger,
  },
  locationText: {
    fontSize: 12,
    fontWeight: '700',
    color: Colors.textPrimary,
    flex: 1,
  },
  routeConnector: {
    width: 1,
    height: 10,
    backgroundColor: Colors.border,
    marginLeft: 3.5,
  },
  hint: {
    fontSize: 10,
    color: Colors.textMuted,
    fontStyle: 'italic',
  },
  actionBtn: {
    backgroundColor: Colors.chrome,
    paddingVertical: 12,
    paddingHorizontal: 14,
    borderRadius: Radius.sm,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    marginTop: 6,
  },
  actionBtnTeal: {
    backgroundColor: Colors.primary,
  },
  actionBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  cameraContainer: {
    marginTop: 10,
    borderRadius: Radius.md,
    overflow: 'hidden',
  },
  cameraView: {
    height: 220,
    width: '100%',
    justifyContent: 'center',
    alignItems: 'center',
  },
  scannerOverlay: {
    flex: 1,
    width: '100%',
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'center',
    alignItems: 'center',
  },
  scanTargetBox: {
    width: 180,
    height: 120,
    borderWidth: 2,
    borderColor: Colors.primary,
    borderRadius: Radius.sm,
    backgroundColor: 'transparent',
  },
  scanInstructionText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1.5,
    marginTop: 10,
    fontFamily: Font.mono,
  },
  closeCameraBtn: {
    backgroundColor: Colors.danger,
    paddingVertical: 10,
    alignItems: 'center',
  },
  closeCameraBtnText: {
    color: '#ffffff',
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
});
