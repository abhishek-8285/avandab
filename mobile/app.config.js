// Dynamic Expo config — replaces app.json. Name/version stay static; runtime
// knobs come from EXPO_PUBLIC_* env vars with the same defaults CI uses.
// Location texts are reused by both the expo-location plugin config and the
// iOS Info.plist permission strings below.

const locationAlwaysAndWhenInUsePermission =
  'Avandab requires location access to track active fleet trip routes and driver position.';
const locationWhenInUsePermission =
  'Avandab requires location access to track your active trip while the app is open.';

module.exports = {
  expo: {
    name: 'Avandab Operations',
    slug: 'avandab-mobile',
    version: '1.0.0',
    orientation: 'portrait',
    icon: './assets/icon.png',
    userInterfaceStyle: 'light',
    newArchEnabled: true,
    splash: {
      image: './assets/splash-icon.png',
      resizeMode: 'contain',
      backgroundColor: '#00685f',
    },
    ios: {
      supportsTablet: true,
      infoPlist: {
        UIBackgroundModes: ['location', 'fetch'],
        NSLocationAlwaysAndWhenInUseUsageDescription: locationAlwaysAndWhenInUsePermission,
        NSLocationWhenInUseUsageDescription: locationWhenInUsePermission,
      },
    },
    android: {
      package: 'com.avandab.mobile',
      permissions: [
        'CAMERA',
        'ACCESS_COARSE_LOCATION',
        'ACCESS_FINE_LOCATION',
        'ACCESS_BACKGROUND_LOCATION',
        'READ_MEDIA_IMAGES',
        'FOREGROUND_SERVICE',
        'FOREGROUND_SERVICE_LOCATION',
        'POST_NOTIFICATIONS',
      ],
    },
    plugins: [
      [
        'expo-camera',
        {
          cameraPermission:
            'Avandab requires camera access to scan cargo barcodes and capture proof-of-delivery photos.',
        },
      ],
      [
        'expo-location',
        {
          locationAlwaysAndWhenInUsePermission,
          locationWhenInUsePermission,
        },
      ],
      [
        'expo-image-picker',
        {
          photosPermission:
            'Avandab requires gallery access to attach expense receipts and delivery photos.',
        },
      ],
      'expo-asset',
      'expo-font',
      'expo-secure-store',
      'expo-sqlite',
    ],
    web: {
      favicon: './assets/favicon.png',
    },
    extra: {
      apiScheme: process.env.EXPO_PUBLIC_API_SCHEME || 'https',
      backendHost: process.env.EXPO_PUBLIC_BACKEND_HOST || 'api.avandab.com',
      apiPort: process.env.EXPO_PUBLIC_API_PORT || '443',
      mqttScheme: process.env.EXPO_PUBLIC_MQTT_SCHEME || 'wss',
      mqttBrokerPort: process.env.EXPO_PUBLIC_MQTT_BROKER_PORT || '8883',
      eas: {
        projectId: process.env.EXPO_PUBLIC_EAS_PROJECT_ID || undefined,
      },
    },
  },
};
