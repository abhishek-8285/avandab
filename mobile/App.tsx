import 'react-native-gesture-handler';
import './src/i18n';
import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, View, ScrollView, TouchableOpacity, Alert, Modal } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';
import { useFonts } from 'expo-font';
import { MaterialCommunityIcons, Ionicons } from '@expo/vector-icons';
import { NavigationContainer, useFocusEffect, useNavigation } from '@react-navigation/native';
import { createStackNavigator } from '@react-navigation/stack';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import AsyncStorage from '@react-native-async-storage/async-storage';
import * as Location from 'expo-location';
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
import { useLanguageStore } from './src/stores/languageStore';
import { NotificationService } from './src/services/notificationService';
import { t } from './src/i18n';
import { QRDemoScreen } from './src/components/QRDemoScreen';
import { useAuthStore } from './src/stores/authStore';
import { useSyncStore } from './src/stores/syncStore';
import { DriverOnboardingScreen } from './src/features/driver-onboarding/screens/DriverOnboardingScreen';
import { VoiceKharchaSheet } from './src/components/VoiceKharchaSheet';
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

function FirstTimeSetupRoute({ navigation }: { navigation: any }) {
  const token = useAuthStore((s) => s.token);
  const user = useAuthStore((s) => s.user);
  return (
    <DriverOnboardingScreen
      token={token || undefined}
      user={user || undefined}
      onComplete={() => navigation.navigate('Main')}
    />
  );
}

function AuthNavigator() {
  return (
    <AuthStack.Navigator initialRouteName="Login" screenOptions={{ headerShown: false }}>
      <AuthStack.Screen name="Login">
        {({ navigation }) => (
          <LoginScreen
            onLoginSuccess={() => {}}
            onForgotPassword={() => navigation.navigate('ForgotPassword')}
            onRegisterLink={() => navigation.navigate('Register')}
          />
        )}
      </AuthStack.Screen>
      <AuthStack.Screen name="Splash">
        {({ navigation }) => <SplashScreen onFinish={() => navigation.navigate('Login')} />}
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
          <FirstTimeSetupRoute navigation={navigation} />
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

const navTheme = {
  dark: false,
  colors: {
    primary: '#008069',
    background: '#075e54',
    card: '#075e54',
    text: '#ffffff',
    border: '#075e54',
    notification: '#25d366',
  },
};

export default function App() {
  const { isAuthenticated, isLoading, loadSession } = useAuthStore();
  const [fontsLoaded, fontError] = useFonts({
    ...MaterialCommunityIcons.font,
    ...Ionicons.font,
  });
  const [splashDone, setSplashDone] = useState(false);

  useEffect(() => {
    loadSession();
    useLanguageStore.getState().loadLanguage();
    OfflineQueue.init().catch(() => {});
    ConsentManager.init().catch(() => {});
    NotificationService.init().catch(() => {});
  }, []);

  useEffect(() => {
    if (isAuthenticated) {
      startNetworkWatcher();
    }
    return () => {
      stopNetworkWatcher();
    };
  }, [isAuthenticated]);

  if (isLoading || (!fontsLoaded && !fontError && !splashDone)) {
    return <SplashScreen onFinish={() => setSplashDone(true)} />;
  }

  return (
    <SafeAreaProvider style={{ backgroundColor: '#075e54' }}>
      <QueryClientProvider client={queryClient}>
        <StatusBar style="light" backgroundColor="#075e54" />
        <NavigationContainer theme={navTheme}>
          {isAuthenticated ? (
            <DriverNavigator />
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
  const navigation = useNavigation<any>();
  const { locale } = useLanguageStore();
  const [activeTab, setActiveTab] = useState<'trips' | 'dispatch' | 'paisa'>('trips');
  const [showQuickScanModal, setShowQuickScanModal] = useState(false);
  const [showVoiceKharchaModal, setShowVoiceKharchaModal] = useState(false);
  const [tripFilter, setTripFilter] = useState<'active' | 'history'>('active');
  const [setupCompleted, setSetupCompleted] = useState(false);

  useFocusEffect(
    React.useCallback(() => {
      AsyncStorage.getItem('@avandab_setup_completed').then((val) => {
        setSetupCompleted(val === 'true');
      });
    }, [])
  );

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
    NotificationService.setOnAcceptDispatch((tripId) => {
      if (onStartNav) {
        onStartNav({
          id: tripId,
          tripNumber: tripId,
          driverName: user?.name || 'Abhishek',
          vehiclePlate: 'DL-01-AB-1234',
          origin: 'JNPT Port, Navi Mumbai',
          destination: 'Chakan MIDC, Pune',
          status: 'IN_TRANSIT',
          startTime: '10:30 AM',
        } as any);
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

      const lat = loc.latitude ?? 19.0760;
      const lng = loc.longitude ?? 72.8777;

      const finalLoc = {
        granted: loc.granted,
        latitude: loc.granted ? lat : null,
        longitude: loc.granted ? lng : null,
        error: loc.error,
      };

      setLocationState(finalLoc);
      if (finalLoc.granted) {
        Analytics.track('driver_gps_location_acquired', { lat, lng });
        if (driverIdentifier) {
          MQTT.publishLocation(driverIdentifier, lat, lng);
        }
        Telemetry.startLiveLocationTracking((liveLat, liveLng) => {
          setLocationState((prev) => ({ ...prev, granted: true, latitude: liveLat, longitude: liveLng }));
          if (driverIdentifier) {
            MQTT.publishLocation(driverIdentifier, liveLat, liveLng);
          }
        });
      }
    } catch (e: any) {
      Analytics.track('driver_gps_error', { error: e.message });
    }
  };

  // Auto-activate location tracking on screen focus and on dispatch tab
  useEffect(() => {
    handleRequestLocation();
  }, [driverIdentifier, activeTab]);






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
      <StatusBar style="light" backgroundColor="#075e54" />

      {/* WhatsApp Signature Top Header */}
      <View style={styles.header}>
        {/* Row 1: Brand Title & Action Icons */}
        <View style={styles.headerTopRow}>
          <View style={styles.brandTitleBlock}>
            <Text style={styles.headerTitle}>{locale === 'en' ? 'Avandab' : t('app.brand', 'Avandab', locale)}</Text>
          </View>

          <View style={styles.headerActionRow}>
            <TouchableOpacity 
              style={styles.headerIconBtn} 
              onPress={() => setShowQuickScanModal(true)}
              accessibilityLabel="Quick Camera & Scan"
              hitSlop={{ top: 15, bottom: 15, left: 15, right: 15 }}
            >
              <MaterialCommunityIcons name="camera-outline" size={22} color="#ffffff" />
            </TouchableOpacity>

            <TouchableOpacity 
              style={styles.headerIconBtn} 
              onPress={() => onOpenProfile ? onOpenProfile() : navigation.navigate('Profile')}
              accessibilityLabel="Settings & Profile"
              hitSlop={{ top: 15, bottom: 15, left: 15, right: 15 }}
            >
              <MaterialCommunityIcons name="dots-vertical" size={24} color="#ffffff" />
            </TouchableOpacity>
          </View>
        </View>

        {/* Row 2: Driver identity & Sync / Online status */}
        <View style={styles.headerBottomRow}>
          <TouchableOpacity 
            style={styles.driverSubRow} 
            activeOpacity={0.8}
            onPress={() => onOpenProfile ? onOpenProfile() : navigation.navigate('Profile')}
            hitSlop={{ top: 10, bottom: 10, left: 10, right: 10 }}
          >
            <MaterialCommunityIcons name="account-circle" size={16} color="#dcf8c6" />
            <Text style={styles.headerSubtitle}>
              {user
                ? `${user.name} · #${(user.driverId || user.id || '').replace(/[^a-zA-Z0-9]/g, '').slice(-6).toUpperCase()}`
                : 'Fleet Driver'}
            </Text>
            <MaterialCommunityIcons name="chevron-right" size={14} color="#dcf8c6" />
          </TouchableOpacity>

          <SyncStatusBar />
        </View>
      </View>

      {/* WhatsApp Tabs Bar - 3 Operational Tabs */}
      <View style={styles.tabContainer}>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'trips' && styles.activeTab]}
          onPress={() => setActiveTab('trips')}
        >
          <View style={styles.tabLabelRow}>
            <MaterialCommunityIcons name="truck-fast" size={16} color={activeTab === 'trips' ? '#ffffff' : 'rgba(255,255,255,0.7)'} />
            <Text style={[styles.tabText, activeTab === 'trips' && styles.activeTabText]}>{t('tabs.trips', 'TRIPS', locale)}</Text>
          </View>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'dispatch' && styles.activeTab]}
          onPress={() => setActiveTab('dispatch')}
        >
          <View style={styles.tabLabelRow}>
            <MaterialCommunityIcons name="briefcase-plus" size={16} color={activeTab === 'dispatch' ? '#ffffff' : 'rgba(255,255,255,0.7)'} />
            <Text style={[styles.tabText, activeTab === 'dispatch' && styles.activeTabText]}>{t('tabs.dispatch', 'DISPATCH', locale)}</Text>
          </View>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, activeTab === 'paisa' && styles.activeTab]}
          onPress={() => setActiveTab('paisa')}
        >
          <View style={styles.tabLabelRow}>
            <MaterialCommunityIcons name="book-open-variant" size={16} color={activeTab === 'paisa' ? '#ffffff' : 'rgba(255,255,255,0.7)'} />
            <Text style={[styles.tabText, activeTab === 'paisa' && styles.activeTabText]}>{t('tabs.paisa', 'KHATA', locale)}</Text>
          </View>
        </TouchableOpacity>
      </View>

      {/* Discrepancy / Incomplete Setup Banner ONLY (Hidden when everything is verified and healthy) */}
      {!setupCompleted && (
        <TouchableOpacity
          style={styles.bannerContainer}
          activeOpacity={0.85}
          onPress={() => onOpenSetup && onOpenSetup()}
        >
          <View style={styles.bannerIconBox}>
            <MaterialCommunityIcons name="alert-circle" size={16} color="#b45309" />
          </View>
          <View style={styles.bannerTextContainer}>
            <Text style={styles.bannerTitle}>{t('section.documents', 'DOCUMENTS INCOMPLETE', locale)}</Text>
            <Text style={styles.bannerSub}>Bank details · Profile photo · Driver docs</Text>
          </View>
          <View style={styles.bannerBtn}>
            <Text style={styles.bannerBtnText}>SETUP</Text>
          </View>
        </TouchableOpacity>
      )}

      {/* Vehicle compliance banner (renders only when a vehicle id is resolvable) */}
      <View style={styles.complianceContainer}>
        <ComplianceBanner vehicleId={activeVehicleId} />
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
                {f === 'active' ? t('filter.active', 'Active Trips', locale) : t('filter.history', 'Trip History', locale)}
              </Text>
            </TouchableOpacity>
          ))}
        </View>
      )}

      {activeTab === 'paisa' && (
        <View style={{ flex: 1 }}>
          <PaisaScreen tripId={undefined} onOpenExpenses={() => navigation.navigate('Expenses', {})} />
        </View>
      )}
      {activeTab !== 'paisa' && (
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
              if (visibleTrips.length === 0 && tripFilter === 'active') {
                return (
                  <View style={{ gap: 12 }}>
                    <TripCard
                      tripNumber="TRP-8491"
                      driverName={user?.name || "Abhishek"}
                      vehiclePlate="DL-01-AB-1234"
                      origin="JNPT Port, Navi Mumbai"
                      destination="Chakan MIDC, Pune"
                      status="IN_TRANSIT"
                      startTime="10:30 AM"
                      cargoWeight="18 Tons"
                      advanceAmount={5000}
                      onPress={() => onStartNav && onStartNav({
                        id: 'TRP-8491',
                        tripNumber: 'TRP-8491',
                        driverName: user?.name || 'Abhishek',
                        vehiclePlate: 'DL-01-AB-1234',
                        origin: 'JNPT Port, Navi Mumbai',
                        destination: 'Chakan MIDC, Pune',
                        status: 'IN_TRANSIT',
                        startTime: '10:30 AM',
                      } as any)}
                      onNavigate={() => onStartNav && onStartNav({
                        id: 'TRP-8491',
                        tripNumber: 'TRP-8491',
                        driverName: user?.name || 'Abhishek',
                        vehiclePlate: 'DL-01-AB-1234',
                        origin: 'JNPT Port, Navi Mumbai',
                        destination: 'Chakan MIDC, Pune',
                        status: 'IN_TRANSIT',
                        startTime: '10:30 AM',
                      } as any)}
                    />
                  </View>
                );
              } else if (visibleTrips.length === 0) {
                return (
                  <View style={styles.emptyInfoCard}>
                    <View style={styles.emptyStateIconBox}>
                      <MaterialCommunityIcons name="truck-delivery-outline" size={32} color="#008069" />
                    </View>
                    <Text style={styles.infoTitle}>{t('filter.history', 'No Trip History', locale)}</Text>
                    <Text style={styles.emptyInfoBody}>
                      Completed and cancelled trips will appear here.
                    </Text>
                  </View>
                );
              }
              return visibleTrips.map((trip) => (
                <TripCard
                  key={trip.id}
                  tripNumber={trip.tripNumber}
                  driverName={trip.driverName}
                  vehiclePlate={trip.vehiclePlate}
                  origin={trip.origin}
                  destination={trip.destination}
                  status={trip.status}
                  startTime={trip.startTime}
                  advanceAmount={5000}
                  cargoWeight="18 Tons"
                  onPress={() => onStartNav && onStartNav(trip)}
                  onNavigate={() => onStartNav && onStartNav(trip)}
                />
              ));
            })()
          )
        ) : (
          <View style={{ gap: 12 }}>
            {/* Dispatch Header Banner */}
            <View style={styles.dispatchHeaderCard}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                <MaterialCommunityIcons name="briefcase-check" size={20} color="#008069" />
                <Text style={styles.dispatchHeaderTitle}>{t('dispatch.title', 'DISPATCH LOADS', locale)}</Text>
              </View>
              <Text style={styles.dispatchHeaderSub}>
                {t('dispatch.sub', 'Verified loads from Avandab Fleet Hub. Accept to auto-assign trip.', locale)}
              </Text>
            </View>

            {/* Load Card 1 */}
            <View style={styles.loadCard}>
              <View style={styles.loadTopRow}>
                <View style={styles.loadBadge}>
                  <Text style={styles.loadBadgeText}>{t('dispatch.instant', 'INSTANT DISPATCH', locale)}</Text>
                </View>
                <View style={[styles.loadBadge, { backgroundColor: '#f0fdf4' }]}>
                  <Text style={[styles.loadBadgeText, { color: '#008069' }]}>18 TONS</Text>
                </View>
              </View>

              <View style={styles.loadRoute}>
                <View style={styles.loadStop}>
                  <View style={[styles.routeDot, { backgroundColor: '#e7ffdb', width: 14, height: 14, borderRadius: 7, alignItems: 'center', justifyContent: 'center' }]}>
                    <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: '#008069' }} />
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.loadCityLabel}>{t('dispatch.origin', 'ORIGIN', locale)}</Text>
                    <Text style={styles.loadCityText}>JNPT Port, Navi Mumbai</Text>
                  </View>
                </View>

                <View style={{ width: 2, height: 12, backgroundColor: '#cbd5e1', marginLeft: 6 }} />

                <View style={styles.loadStop}>
                  <View style={[styles.routeDot, { backgroundColor: '#fee2e2', width: 14, height: 14, borderRadius: 7, alignItems: 'center', justifyContent: 'center' }]}>
                    <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: '#ef4444' }} />
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.loadCityLabel}>{t('dispatch.destination', 'DESTINATION', locale)}</Text>
                    <Text style={styles.loadCityText}>Chakan MIDC, Pune</Text>
                  </View>
                </View>
              </View>

              <View style={styles.loadMetaRow}>
                <View style={styles.loadMetaChip}>
                  <MaterialCommunityIcons name="weight" size={13} color="#667781" />
                  <Text style={styles.loadMetaText}>18 Tons Steel Coils</Text>
                </View>
                <View style={styles.loadMetaChip}>
                  <MaterialCommunityIcons name="clock-outline" size={13} color="#667781" />
                  <Text style={styles.loadMetaText}>Pickup: Today 6:00 PM</Text>
                </View>
              </View>

              <View style={styles.loadActionRow}>
                <TouchableOpacity
                  style={styles.loadCallBtn}
                  activeOpacity={0.85}
                  onPress={() => Alert.alert(t('dispatch.call', 'Call Dispatch', locale), '+91 98200 12345')}
                >
                  <MaterialCommunityIcons name="phone" size={15} color="#008069" />
                  <Text style={styles.loadCallBtnText}>{t('dispatch.call', 'CALL', locale)}</Text>
                </TouchableOpacity>

                <TouchableOpacity
                  style={styles.loadAcceptBtn}
                  activeOpacity={0.85}
                  onPress={() => {
                    const assigned = {
                      id: 'TRP-8491',
                      tripNumber: 'TRP-8491',
                      driverName: user?.name || 'Abhishek',
                      vehiclePlate: 'DL-01-AB-1234',
                      origin: 'JNPT Port, Navi Mumbai',
                      destination: 'Chakan MIDC, Pune',
                      status: 'IN_TRANSIT',
                      startTime: '10:30 AM',
                    } as any;
                    if (onStartNav) {
                      onStartNav(assigned);
                    }
                  }}
                >
                  <MaterialCommunityIcons name="check-circle" size={15} color="#ffffff" />
                  <Text style={styles.loadAcceptBtnText}>{t('dispatch.accept', 'ACCEPT LOAD', locale)}</Text>
                </TouchableOpacity>
              </View>
            </View>

            {/* Load Card 2 */}
            <View style={styles.loadCard}>
              <View style={styles.loadTopRow}>
                <View style={[styles.loadBadge, { backgroundColor: '#e0f2fe' }]}>
                  <Text style={[styles.loadBadgeText, { color: '#0284c7' }]}>{t('dispatch.scheduled', 'SCHEDULED TOMORROW', locale)}</Text>
                </View>
                <View style={[styles.loadBadge, { backgroundColor: '#f0fdf4' }]}>
                  <Text style={[styles.loadBadgeText, { color: '#008069' }]}>14 TONS</Text>
                </View>
              </View>

              <View style={styles.loadRoute}>
                <View style={styles.loadStop}>
                  <View style={[styles.routeDot, { backgroundColor: '#e7ffdb', width: 14, height: 14, borderRadius: 7, alignItems: 'center', justifyContent: 'center' }]}>
                    <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: '#008069' }} />
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.loadCityLabel}>{t('dispatch.origin', 'ORIGIN', locale)}</Text>
                    <Text style={styles.loadCityText}>Bhiwandi Logistics Park, Thane</Text>
                  </View>
                </View>

                <View style={{ width: 2, height: 12, backgroundColor: '#cbd5e1', marginLeft: 6 }} />

                <View style={styles.loadStop}>
                  <View style={[styles.routeDot, { backgroundColor: '#fee2e2', width: 14, height: 14, borderRadius: 7, alignItems: 'center', justifyContent: 'center' }]}>
                    <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: '#ef4444' }} />
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.loadCityLabel}>{t('dispatch.destination', 'DESTINATION', locale)}</Text>
                    <Text style={styles.loadCityText}>Sanand GIDC, Ahmedabad</Text>
                  </View>
                </View>
              </View>

              <View style={styles.loadMetaRow}>
                <View style={styles.loadMetaChip}>
                  <MaterialCommunityIcons name="package-variant-closed" size={13} color="#667781" />
                  <Text style={styles.loadMetaText}>14 Tons FMCG Pallets</Text>
                </View>
                <View style={styles.loadMetaChip}>
                  <MaterialCommunityIcons name="clock-outline" size={13} color="#667781" />
                  <Text style={styles.loadMetaText}>Tomorrow 8:00 AM</Text>
                </View>
              </View>

              <View style={styles.loadActionRow}>
                <TouchableOpacity
                  style={styles.loadCallBtn}
                  activeOpacity={0.85}
                  onPress={() => Alert.alert(t('dispatch.call', 'Call Dispatch', locale), '+91 98200 54321')}
                >
                  <MaterialCommunityIcons name="phone" size={15} color="#008069" />
                  <Text style={styles.loadCallBtnText}>{t('dispatch.call', 'CALL', locale)}</Text>
                </TouchableOpacity>

                <TouchableOpacity
                  style={styles.loadAcceptBtn}
                  activeOpacity={0.85}
                  onPress={() => {
                    const assigned = {
                      id: 'TRP-8492',
                      tripNumber: 'TRP-8492',
                      driverName: user?.name || 'Abhishek',
                      vehiclePlate: 'DL-01-AB-1234',
                      origin: 'Bhiwandi Logistics Park, Thane',
                      destination: 'Sanand GIDC, Ahmedabad',
                      status: 'IN_TRANSIT',
                      startTime: 'Tomorrow 8:00 AM',
                    } as any;
                    if (onStartNav) {
                      onStartNav(assigned);
                    }
                  }}
                >
                  <MaterialCommunityIcons name="check-circle" size={15} color="#ffffff" />
                  <Text style={styles.loadAcceptBtnText}>{t('dispatch.accept', 'ACCEPT LOAD', locale)}</Text>
                </TouchableOpacity>
              </View>
            </View>
          </View>
        )}
      </ScrollView>
      )}

      {/* WhatsApp Floating Action Button: 1-Tap Voice Kharcha (Vernacular Expenses) */}
      <View style={styles.floatingActionContainer} pointerEvents="box-none">
        <TouchableOpacity
          style={styles.floatingMicBtn}
          activeOpacity={0.85}
          onPress={() => setShowVoiceKharchaModal(true)}
          accessibilityLabel="Voice Kharcha"
        >
          <MaterialCommunityIcons name="microphone" size={26} color="#ffffff" />
        </TouchableOpacity>
      </View>

      {/* INSTANT VERNACULAR VOICE KHARCHA SHEET */}
      <VoiceKharchaSheet
        visible={showVoiceKharchaModal}
        onClose={() => setShowVoiceKharchaModal(false)}
        tripId={activeTrip?.tripNumber || activeTrip?.id || 'TRP-8491'}
        onSaved={() => {
          queryClient.invalidateQueries({ queryKey: ['trips'] });
        }}
      />

      {/* QUICK CAMERA & SCAN ACTION SHEET MODAL */}
      <Modal visible={showQuickScanModal} transparent animationType="slide" onRequestClose={() => setShowQuickScanModal(false)}>
        <View style={styles.quickModalOverlay}>
          <View style={styles.quickModalCard}>
            <View style={styles.quickModalHeader}>
              <Text style={styles.quickModalTitle}>{t('header.quick_scan', 'Quick Camera & Scan', locale)}</Text>
              <TouchableOpacity onPress={() => setShowQuickScanModal(false)}>
                <MaterialCommunityIcons name="close" size={24} color="#667781" />
              </TouchableOpacity>
            </View>

            <TouchableOpacity 
              style={styles.quickActionItem}
              onPress={() => {
                setShowQuickScanModal(false);
                navigation.navigate('Expenses', {});
              }}
            >
              <View style={[styles.quickIconBox, { backgroundColor: '#e7ffdb' }]}>
                <MaterialCommunityIcons name="receipt" size={22} color="#008069" />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.quickItemTitle}>{t('quick.log_fuel', 'Log Fuel / Expense Receipt', locale)}</Text>
                <Text style={styles.quickItemSub}>{t('quick.log_fuel_sub', 'Capture receipt photo for instant reimbursement', locale)}</Text>
              </View>
            </TouchableOpacity>

            <TouchableOpacity 
              style={styles.quickActionItem}
              onPress={() => {
                setShowQuickScanModal(false);
                Alert.alert(t('quick.scan_ewb', 'E-Way Bill Scanner', locale), 'Point camera at E-Way Bill or LR Barcode.');
              }}
            >
              <View style={[styles.quickIconBox, { backgroundColor: '#e0f2fe' }]}>
                <MaterialCommunityIcons name="barcode-scan" size={22} color="#0284c7" />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.quickItemTitle}>{t('quick.scan_ewb', 'Scan E-Way Bill / LR Barcode', locale)}</Text>
                <Text style={styles.quickItemSub}>{t('quick.scan_ewb_sub', 'Auto-detect GST EWB number & consignee details', locale)}</Text>
              </View>
            </TouchableOpacity>

            <TouchableOpacity 
              style={styles.quickActionItem}
              onPress={() => {
                setShowQuickScanModal(false);
                Alert.alert(t('quick.delivery_proof', 'e-POD Verification', locale), 'Capture delivery proof or gate pass photo.');
              }}
            >
              <View style={[styles.quickIconBox, { backgroundColor: '#fef3c7' }]}>
                <MaterialCommunityIcons name="camera-document" size={22} color="#b45309" />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.quickItemTitle}>{t('quick.delivery_proof', 'Proof of Delivery (e-POD)', locale)}</Text>
                <Text style={styles.quickItemSub}>{t('quick.delivery_proof_sub', 'Unloading signature & stamped gate pass capture', locale)}</Text>
              </View>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  header: {
    backgroundColor: '#075e54',
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.sm,
    paddingBottom: Spacing.md,
    borderBottomWidth: 1,
    borderBottomColor: '#004c3f',
  },
  headerTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  brandTitleBlock: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  headerTitle: {
    color: '#ffffff',
    fontSize: 20,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  headerTitleHindi: {
    fontSize: 14,
    fontWeight: '600',
    color: '#dcf8c6',
  },
  onlineDotPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    backgroundColor: 'rgba(0,0,0,0.15)',
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: Radius.full,
  },
  brandDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: '#25d366',
  },
  onlineText: {
    color: '#25d366',
    fontSize: 9,
    fontWeight: '700',
  },
  headerActionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  headerIconBtn: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: 'rgba(255,255,255,0.12)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  headerBottomRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: 8,
  },
  driverSubRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
  },
  headerSubtitle: {
    color: '#dcf8c6',
    fontSize: 12,
    fontWeight: '600',
  },
  tabContainer: {
    flexDirection: 'row',
    backgroundColor: '#075e54',
    borderBottomWidth: 2,
    borderBottomColor: '#004c3f',
  },
  tab: {
    flex: 1,
    paddingVertical: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  activeTab: {
    borderBottomWidth: 3,
    borderBottomColor: '#25d366',
  },
  tabLabelRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
  },
  tabText: {
    fontSize: 11,
    fontWeight: '700',
    color: 'rgba(255, 255, 255, 0.75)',
    letterSpacing: 0.5,
  },
  activeTabText: {
    color: '#ffffff',
    fontWeight: '800',
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
  },
  bannerContainerSuccess: {
    backgroundColor: '#e7ffdb',
    borderBottomWidth: 1,
    borderBottomColor: '#c8f5b8',
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  bannerIconBoxSuccess: {
    width: 24,
    height: 24,
    borderRadius: Radius.sm,
    backgroundColor: '#dcf8c6',
    justifyContent: 'center',
    alignItems: 'center',
  },
  bannerTitleSuccess: {
    fontSize: 11,
    fontWeight: '800',
    color: '#008069',
    letterSpacing: 0.5,
  },
  bannerSubSuccess: {
    fontSize: 10,
    fontWeight: '600',
    color: '#075e54',
    marginTop: 1,
  },
  bannerBtnOutline: {
    borderWidth: 1,
    borderColor: '#008069',
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: Radius.full,
    backgroundColor: '#ffffff',
  },
  bannerBtnOutlineText: {
    fontSize: 10,
    fontWeight: '800',
    color: '#008069',
    letterSpacing: 0.5,
  },
  complianceContainer: {
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.sm,
  },
  filterRow: {
    flexDirection: 'row',
    gap: 8,
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.sm,
  },
  filterChip: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: Radius.full,
    borderWidth: 1,
    borderColor: '#d1d7db',
    backgroundColor: '#ffffff',
  },
  filterChipActive: {
    backgroundColor: '#008069',
    borderColor: '#008069',
  },
  filterChipText: {
    fontSize: 11,
    fontWeight: '700',
    color: '#54656f',
  },
  filterChipTextActive: {
    color: '#ffffff',
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
  emptyInfoCard: {
    backgroundColor: '#ffffff',
    borderRadius: Radius.lg,
    padding: Spacing.xl,
    borderWidth: 1,
    borderColor: '#e9edef',
    alignItems: 'center',
    justifyContent: 'center',
    shadowColor: '#111b21',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.08,
    shadowRadius: 6,
    elevation: 3,
    marginTop: Spacing.md,
  },
  emptyStateIconBox: {
    width: 64,
    height: 64,
    borderRadius: 32,
    backgroundColor: '#dcf8c6',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: Spacing.md,
  },
  infoTitle: {
    fontSize: 16,
    fontWeight: '800',
    color: '#111b21',
    textAlign: 'center',
  },
  infoTitleEn: {
    fontSize: 11,
    fontWeight: '700',
    color: '#667781',
    letterSpacing: 1,
    marginTop: 2,
    textAlign: 'center',
  },
  emptyInfoBody: {
    fontSize: 13,
    color: '#54656f',
    lineHeight: 20,
    textAlign: 'center',
    marginTop: 6,
    marginBottom: Spacing.lg,
    maxWidth: 290,
  },
  emptyActionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  emptyActionPrimaryBtn: {
    backgroundColor: '#008069',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 11,
    borderRadius: Radius.full,
    shadowColor: '#008069',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.2,
    shadowRadius: 4,
    elevation: 2,
  },
  emptyActionPrimaryText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
  },
  emptyActionSecondaryBtn: {
    backgroundColor: '#ffffff',
    borderWidth: 1.5,
    borderColor: '#008069',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderRadius: Radius.full,
  },
  emptyActionSecondaryText: {
    color: '#008069',
    fontSize: 12,
    fontWeight: '800',
  },
  floatingActionContainer: {
    position: 'absolute',
    right: 20,
    bottom: 24,
    alignItems: 'center',
    gap: 12,
  },
  floatingMicBtn: {
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: '#25d366',
    alignItems: 'center',
    justifyContent: 'center',
    shadowColor: '#111b21',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.3,
    shadowRadius: 6,
    elevation: 6,
  },
  infoCardHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 6,
  },
  infoSectionTitle: {
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
  quickModalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.5)',
    justifyContent: 'flex-end',
  },
  quickModalCard: {
    backgroundColor: '#ffffff',
    borderTopLeftRadius: Radius.xl,
    borderTopRightRadius: Radius.xl,
    padding: Spacing.xl,
    gap: Spacing.md,
  },
  quickModalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 4,
  },
  quickModalTitle: {
    fontSize: 16,
    fontWeight: '800',
    color: '#111b21',
  },
  quickActionItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 14,
    paddingVertical: 12,
    paddingHorizontal: 8,
    borderRadius: Radius.md,
    backgroundColor: '#f8fafc',
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  quickIconBox: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: 'center',
    justifyContent: 'center',
  },
  quickItemTitle: {
    fontSize: 14,
    fontWeight: '800',
    color: '#111b21',
  },
  quickItemSub: {
    fontSize: 11,
    color: '#667781',
    marginTop: 2,
  },
  dispatchHeaderCard: {
    backgroundColor: '#ffffff',
    borderRadius: 12,
    padding: 12,
    borderWidth: 1,
    borderColor: '#e9edef',
    gap: 4,
  },
  dispatchHeaderTitle: {
    fontSize: 13,
    fontWeight: '800',
    color: '#111b21',
  },
  dispatchHeaderSub: {
    fontSize: 11,
    color: '#667781',
  },
  loadCard: {
    backgroundColor: '#ffffff',
    borderRadius: 14,
    padding: 14,
    borderWidth: 1,
    borderColor: '#e9edef',
    shadowColor: '#000000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.08,
    shadowRadius: 3,
    elevation: 2,
    gap: 10,
  },
  loadTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  loadBadge: {
    backgroundColor: '#e7ffdb',
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 10,
  },
  loadBadgeText: {
    color: '#008069',
    fontSize: 9,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  loadFareText: {
    color: '#008069',
    fontSize: 18,
    fontWeight: '900',
    fontFamily: Font.mono,
  },
  loadRoute: {
    gap: 2,
  },
  loadStop: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 10,
  },
  loadCityLabel: {
    fontSize: 9,
    fontWeight: '700',
    color: '#8696a0',
  },
  loadCityText: {
    fontSize: 13,
    fontWeight: '800',
    color: '#111b21',
  },
  loadMetaRow: {
    flexDirection: 'row',
    gap: 8,
    backgroundColor: '#f8fafc',
    padding: 8,
    borderRadius: 8,
  },
  loadMetaChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    flex: 1,
  },
  loadMetaText: {
    fontSize: 11,
    fontWeight: '600',
    color: '#667781',
  },
  loadActionRow: {
    flexDirection: 'row',
    gap: 10,
    marginTop: 2,
  },
  loadCallBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#f0f2f5',
    paddingVertical: 9,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#e2e8f0',
  },
  loadCallBtnText: {
    color: '#008069',
    fontSize: 11,
    fontWeight: '800',
  },
  loadAcceptBtn: {
    flex: 2,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    backgroundColor: '#008069',
    paddingVertical: 9,
    borderRadius: 10,
  },
  loadAcceptBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
});
