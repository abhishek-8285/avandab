import { Platform } from 'react-native';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

let NotificationsModule: any = null;
let DeviceModule: any = null;
try {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  NotificationsModule = require('expo-notifications');
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  DeviceModule = require('expo-device');
} catch {
  // Optional notification modules
}

// Foreground handler — show banner even when app is open
if (NotificationsModule && typeof NotificationsModule.setNotificationHandler === 'function') {
  NotificationsModule.setNotificationHandler({
    handleNotification: async () => ({
      shouldShowAlert: true,
      shouldPlaySound: true,
      shouldSetBadge: false,
      shouldShowBanner: true,
      shouldShowList: true,
    }),
  });
}

export async function registerForPushNotificationsAsync(): Promise<string | null> {
  if (!DeviceModule?.isDevice || !NotificationsModule) return null;
  try {
    if (Platform.OS === 'android' && NotificationsModule.setNotificationChannelAsync) {
      await NotificationsModule.setNotificationChannelAsync('dispatch', {
        name: 'Dispatch',
        importance: NotificationsModule.AndroidImportance?.MAX ?? 5,
        vibrationPattern: [0, 250, 250, 250],
        lightColor: '#00685f',
      });
    }
    const perm = await NotificationsModule.getPermissionsAsync();
    let granted = perm?.granted;
    if (!granted && NotificationsModule.requestPermissionsAsync) {
      const req = await NotificationsModule.requestPermissionsAsync();
      granted = req?.granted;
    }
    if (!granted) return null;
    const token = (await NotificationsModule.getExpoPushTokenAsync())?.data;
    // Best-effort register with backend — enqueues for retry if offline
    const authToken = useAuthStore.getState().token;
    if (authToken && token) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me/push-token`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${authToken}`,
        },
        body: JSON.stringify({ expo_push_token: token, platform: Platform.OS }),
      }).catch(() => {});
    }
    return token ?? null;
  } catch {
    return null;
  }
}

export function addPushListener(
  onNotification: (data: Record<string, any>) => void
): () => void {
  if (!NotificationsModule) return () => {};
  const sub = NotificationsModule.addNotificationReceivedListener?.((n: any) => {
    onNotification(n?.request?.content?.data as Record<string, any>);
  });
  const respSub = NotificationsModule.addNotificationResponseReceivedListener?.((r: any) => {
    onNotification(r?.notification?.request?.content?.data as Record<string, any>);
  });
  return () => {
    sub?.remove?.();
    respSub?.remove?.();
  };
}
