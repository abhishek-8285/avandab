// Reset mocks between tests to prevent cross-test contamination.
import '@testing-library/react-native/extend-expect';

beforeEach(() => {
  jest.clearAllMocks();
});

afterEach(() => {
  jest.restoreAllMocks();
});

// Mock @expo/vector-icons and expo-font (native modules)
jest.mock('@expo/vector-icons', () => ({
  MaterialCommunityIcons: 'MaterialCommunityIcons',
  Ionicons: 'Ionicons',
  FontAwesome: 'FontAwesome',
}));

jest.mock('expo-font', () => ({
  isLoaded: jest.fn().mockReturnValue(true),
  loadAsync: jest.fn().mockResolvedValue(undefined),
}));
jest.mock('react-native-safe-area-context', () => {
  const inset = { top: 0, right: 0, bottom: 0, left: 0 };
  return {
    SafeAreaProvider: ({ children }: any) => children,
    SafeAreaConsumer: ({ children }: any) => children(inset),
    SafeAreaView: ({ children }: any) => children,
    useSafeAreaInsets: () => inset,
    useSafeAreaFrame: () => ({ x: 0, y: 0, width: 390, height: 844 }),
  };
});
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn().mockResolvedValue(null),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

// Mock expo-location (native module)
jest.mock('expo-location', () => ({
  requestForegroundPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
  getForegroundPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
  requestBackgroundPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
  hasServicesEnabledAsync: jest.fn().mockResolvedValue(true),
  getLastKnownPositionAsync: jest.fn().mockResolvedValue(null),
  getCurrentPositionAsync: jest.fn().mockResolvedValue({ coords: { latitude: 19.076, longitude: 72.8777 } }),
  watchPositionAsync: jest.fn(),
  startLocationUpdatesAsync: jest.fn().mockResolvedValue(undefined),
  stopLocationUpdatesAsync: jest.fn().mockResolvedValue(undefined),
  hasStartedLocationUpdatesAsync: jest.fn().mockResolvedValue(false),
  Accuracy: { Balanced: 3, High: 4, Low: 1 },
}));

// Mock expo-task-manager (native module)
jest.mock('expo-task-manager', () => ({
  __esModule: true,
  default: { defineTask: jest.fn() },
  defineTask: jest.fn(),
}));

// Mock expo-camera (native module)
jest.mock('expo-camera', () => ({
  Camera: { requestCameraPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }) },
  CameraView: 'CameraView',
  useCameraPermissions: jest.fn().mockReturnValue([
    { granted: true, status: 'granted' },
    jest.fn().mockResolvedValue({ granted: true, status: 'granted' }),
  ]),
  requestCameraPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
}));

// Mock expo-image-picker
jest.mock('expo-image-picker', () => ({
  requestMediaLibraryPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
  requestCameraPermissionsAsync: jest.fn().mockResolvedValue({ status: 'granted', granted: true }),
  launchImageLibraryAsync: jest.fn().mockResolvedValue({ canceled: true, assets: [] }),
  launchCameraAsync: jest.fn().mockResolvedValue({ canceled: true, assets: [] }),
  MediaTypeOptions: { Images: 'Images', Videos: 'Videos', All: 'All' },
}), { virtual: true });

// Mock expo-speech-recognition
jest.mock('expo-speech-recognition', () => ({
  ExpoSpeechRecognitionModule: {
    requestPermissionsAsync: jest.fn().mockResolvedValue({ granted: true, status: 'granted' }),
    start: jest.fn().mockResolvedValue(undefined),
    stop: jest.fn().mockResolvedValue(undefined),
    abort: jest.fn().mockResolvedValue(undefined),
  },
  useSpeechRecognitionEvent: jest.fn(),
}), { virtual: true });

// Mock react-native-signature-canvas
jest.mock('react-native-signature-canvas', () => 'SignaturePad', { virtual: true });
jest.mock('react-native-webview', () => ({
  WebView: 'WebView',
}), { virtual: true });

// Mock i18next / react-i18next
jest.mock('i18next', () => ({
  use: jest.fn().mockReturnThis(),
  init: jest.fn().mockResolvedValue(undefined),
  t: jest.fn((k: string) => k),
  changeLanguage: jest.fn().mockResolvedValue(undefined),
  language: 'en',
}), { virtual: true });
jest.mock('react-i18next', () => ({
  initReactI18next: { type: '3rdParty', init: jest.fn() },
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en', changeLanguage: jest.fn() } }),
}), { virtual: true });

// Mock @react-native-async-storage/async-storage
const mockAsyncStorageData: Record<string, string> = {};
const mockAsyncStorage = {
  getItem: jest.fn(async (key: string) => mockAsyncStorageData[key] ?? null),
  setItem: jest.fn(async (key: string, value: string) => {
    mockAsyncStorageData[key] = String(value);
  }),
  removeItem: jest.fn(async (key: string) => {
    delete mockAsyncStorageData[key];
  }),
  clear: jest.fn(async () => {
    Object.keys(mockAsyncStorageData).forEach((k) => delete mockAsyncStorageData[k]);
  }),
  getAllKeys: jest.fn(async () => Object.keys(mockAsyncStorageData)),
};
jest.mock('@react-native-async-storage/async-storage', () => ({
  __esModule: true,
  default: mockAsyncStorage,
  ...mockAsyncStorage,
}));

// In-memory mock database state for expo-sqlite
const sqliteMockState = {
  queued_pods: [] as any[],
  queued_gps: [] as any[],
  trips: [] as any[],
  offline_gps_logs: [] as any[],
  offline_expenses: [] as any[],
  consent_log: [] as any[],
};

export const getSQLiteMockState = () => sqliteMockState;

export const resetSQLiteMockState = () => {
  sqliteMockState.queued_pods = [];
  sqliteMockState.queued_gps = [];
  sqliteMockState.trips = [];
  sqliteMockState.offline_gps_logs = [];
  sqliteMockState.offline_expenses = [];
  sqliteMockState.consent_log = [];
};

// Mock expo-sqlite (native module)
jest.mock('expo-sqlite', () => ({
  openDatabaseAsync: jest.fn().mockResolvedValue({
    execAsync: jest.fn().mockResolvedValue(undefined),
    getFirstAsync: jest.fn().mockImplementation(async (query: string, params: any[] = []) => {
      if (query.includes('FROM consent_log')) {
        // Newest record for the purpose (ORDER BY id DESC LIMIT 1)
        const rows = sqliteMockState.consent_log
          .filter((c) => c.purpose === params[0])
          .sort((a, b) => b.id - a.id);
        return rows[0] ?? null;
      }
      if (query.includes('queued_pods WHERE trip_id =')) {
        if (query.includes('stop_id') && params.length >= 3 && params[2] != null) {
          return sqliteMockState.queued_pods.find((p) => p.trip_id === params[0] && p.stop_id === params[2]) || null;
        }
        return sqliteMockState.queued_pods.find((p) => p.trip_id === params[0]) || null;
      }
      return null;
    }),
    getAllAsync: jest.fn().mockImplementation(async (query: string) => {
      if (query.includes('queued_pods')) {
        return [...sqliteMockState.queued_pods];
      }
      if (query.includes('queued_gps')) {
        return [...sqliteMockState.queued_gps];
      }
      if (query.includes('trips')) {
        return [...sqliteMockState.trips];
      }
      if (query.includes('offline_gps_logs')) {
        const rows = sqliteMockState.offline_gps_logs.map((l) =>
          query.includes('accuracy AS accuracy_m')
            ? { ...l, accuracy_m: l.accuracy ?? null }
            : { ...l }
        );
        return query.includes('WHERE synced = 0') ? rows.filter((l) => l.synced === 0) : rows;
      }
      if (query.includes('offline_expenses')) {
        return [...sqliteMockState.offline_expenses];
      }
      if (query.includes('consent_log')) {
        // Newest-first: timestamp DESC, id DESC tiebreak
        return [...sqliteMockState.consent_log].sort((a, b) => {
          if (a.timestamp !== b.timestamp) return a.timestamp < b.timestamp ? 1 : -1;
          return b.id - a.id;
        });
      }
      return [];
    }),
    runAsync: jest.fn().mockImplementation(async (query: string, params: any[] = []) => {
      if (query.includes('INSERT INTO queued_pods')) {
        // New schema: (trip_id, consignee_name, consignee_phone, notes, photo_uri, latitude, longitude, pod_signature_data, quantity_short, damage_qty, refusal_reason)
        // Old schema fallback: (trip_id, consignee_name, notes, photo_uri, latitude, longitude)
        let pod: any;
        if (query.includes('stop_id')) {
          pod = {
            id: sqliteMockState.queued_pods.length + 1,
            trip_id: params[0],
            stop_id: params[1] ?? null,
            stop_sequence: params[2] ?? null,
            otp: params[3] ?? null,
            consignee_name: params[4],
            consignee_phone: params[5] ?? null,
            notes: params[6],
            photo_uri: params[7],
            latitude: params[8],
            longitude: params[9],
            pod_signature_data: params[10] ?? null,
            quantity_short: params[11] ?? null,
            damage_qty: params[12] ?? null,
            refusal_reason: params[13] ?? null,
            created_at: new Date().toISOString(),
          };
        } else if (query.includes('consignee_phone')) {
          pod = {
            id: sqliteMockState.queued_pods.length + 1,
            trip_id: params[0],
            consignee_name: params[1],
            consignee_phone: params[2] ?? null,
            notes: params[3],
            photo_uri: params[4],
            latitude: params[5],
            longitude: params[6],
            pod_signature_data: params[7] ?? null,
            quantity_short: params[8] ?? null,
            damage_qty: params[9] ?? null,
            refusal_reason: params[10] ?? null,
            created_at: new Date().toISOString(),
          };
        } else {
          // fallback old
          pod = {
            id: sqliteMockState.queued_pods.length + 1,
            trip_id: params[0],
            consignee_name: params[1],
            consignee_phone: null,
            notes: params[2],
            photo_uri: params[3],
            latitude: params[4],
            longitude: params[5],
            pod_signature_data: null,
            quantity_short: null,
            damage_qty: null,
            refusal_reason: null,
            created_at: new Date().toISOString(),
          };
        }
        sqliteMockState.queued_pods.push(pod);
      } else if (query.includes('DELETE FROM queued_pods WHERE trip_id =')) {
        if (query.includes('stop_id =') && params[1]) {
          sqliteMockState.queued_pods = sqliteMockState.queued_pods.filter(
            (p) => !(p.trip_id === params[0] && (p.stop_id === params[1] || !p.stop_id))
          );
        } else {
          sqliteMockState.queued_pods = sqliteMockState.queued_pods.filter((p) => p.trip_id !== params[0]);
        }
      } else if (query.includes('INSERT INTO queued_gps')) {
        const gps = {
          id: sqliteMockState.queued_gps.length + 1,
          driver_id: params[0],
          latitude: params[1],
          longitude: params[2],
          timestamp: params[3],
          accuracy_m: params[4],
          created_at: new Date().toISOString(),
        };
        sqliteMockState.queued_gps.push(gps);
      } else if (query.includes('DELETE FROM queued_gps WHERE id IN')) {
        const ids = params;
        sqliteMockState.queued_gps = sqliteMockState.queued_gps.filter((g) => !ids.includes(g.id));
      } else if (query.includes('INSERT INTO offline_expenses')) {
        const exp = {
          id: sqliteMockState.offline_expenses.length + 1,
          trip_id: params[0],
          expense_type: params[1],
          amount: params[2],
          receipt_uri: params[3],
          notes: params[4],
          latitude: params[5],
          longitude: params[6],
          idempotency_key: params[7] ?? null,
          created_at: new Date().toISOString(),
        };
        sqliteMockState.offline_expenses.push(exp);
      } else if (query.includes('DELETE FROM offline_expenses WHERE id =')) {
        sqliteMockState.offline_expenses = sqliteMockState.offline_expenses.filter((e) => e.id !== params[0]);
      } else if (query.includes('DELETE FROM offline_expenses WHERE id IN')) {
        const ids = params;
        sqliteMockState.offline_expenses = sqliteMockState.offline_expenses.filter((e) => !ids.includes(e.id));
      } else if (query.includes('INSERT OR REPLACE INTO trips') || query.includes('INSERT INTO trips')) {
        const trip = {
          id: params[0],
          tripNumber: params[1],
          driverName: params[2],
          vehiclePlate: params[3],
          origin: params[4],
          destination: params[5],
          status: params[6],
          startTime: params[7],
        };
        const idx = sqliteMockState.trips.findIndex((t) => t.id === trip.id);
        if (idx >= 0) sqliteMockState.trips[idx] = trip;
        else sqliteMockState.trips.push(trip);
      } else if (query.includes('INSERT INTO offline_gps_logs')) {
        const log = {
          id: sqliteMockState.offline_gps_logs.length + 1,
          latitude: params[0],
          longitude: params[1],
          timestamp: params[2],
          accuracy: params[3] ?? null,
          speed: params[4] ?? null,
          heading: params[5] ?? null,
          motion: params[6] ?? null,
          battery_level: params[7] ?? null,
          synced: 0,
        };
        sqliteMockState.offline_gps_logs.push(log);
      } else if (query.includes('UPDATE offline_gps_logs SET synced = 1')) {
        const ids = params;
        sqliteMockState.offline_gps_logs.forEach((l) => {
          if (ids.includes(l.id)) l.synced = 1;
        });
      } else if (query.includes('INSERT INTO consent_log')) {
        sqliteMockState.consent_log.push({
          id: sqliteMockState.consent_log.length + 1,
          purpose: params[0],
          user_response: params[1],
          timestamp: new Date().toISOString(),
        });
      } else if (query.includes('DELETE FROM consent_log')) {
        // Mirrors DELETE ... WHERE timestamp < datetime('now','-N years')
        const yearsMatch = query.match(/'-(\d+)\s+years'/);
        const years = yearsMatch ? parseInt(yearsMatch[1], 10) : 3;
        const cutoffMs = Date.now() - years * 365.25 * 24 * 60 * 60 * 1000;
        sqliteMockState.consent_log = sqliteMockState.consent_log.filter(
          (c) => new Date(c.timestamp).getTime() >= cutoffMs
        );
      }
      return { lastInsertRowId: 1, changes: 1 };
    }),
  }),
}));

// Mock expo-image-manipulator (native module)
jest.mock('expo-image-manipulator', () => ({
  SaveFormat: { JPEG: 'jpeg', PNG: 'png', WEBP: 'webp' },
  manipulateAsync: jest.fn().mockImplementation(async (uri: string) => ({
    uri: `${uri}_compressed.jpg`,
    width: 800,
    height: 600,
  })),
}));

// Mock @react-native-community/netinfo
jest.mock('@react-native-community/netinfo', () => ({
  addEventListener: jest.fn(),
  fetch: jest.fn().mockResolvedValue({ isConnected: true }),
}));

// Mock mqtt (network module)
jest.mock('mqtt', () => ({
  connect: jest.fn().mockReturnValue({
    on: jest.fn(),
    publish: jest.fn(),
    subscribe: jest.fn(),
    end: jest.fn(),
  }),
}));

// ── Append-only extensions below ──

// Virtual mock for optional expo-notifications so guarded require succeeds in tests
jest.mock('expo-notifications', () => ({
  setNotificationHandler: jest.fn(),
  scheduleNotificationAsync: jest.fn(),
  requestPermissionsAsync: jest.fn().mockResolvedValue(true),
  getPermissionsAsync: jest.fn().mockResolvedValue({ granted: true }),
  dismissNotificationAsync: jest.fn(),
  getExpoPushTokenAsync: jest.fn().mockResolvedValue({ data: 'ExponentPushToken[test]' }),
}), { virtual: true });
