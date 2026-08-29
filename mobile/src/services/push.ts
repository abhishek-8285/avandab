import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import { Platform } from 'react-native';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

// Foreground handler — show banner even when app is open
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowAlert: true,
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

export async function registerForPushNotificationsAsync(): Promise<string | null> {
  if (!Device.isDevice) return null;
  try {
    if (Platform.OS === 'android') {
      await Notifications.setNotificationChannelAsync('dispatch', {
        name: 'Dispatch',
        importance: Notifications.AndroidImportance.MAX,
        vibrationPattern: [0, 250, 250, 250],
        lightColor: '#00685f',
      });
    }
    const perm = await Notifications.getPermissionsAsync();
    let granted = perm.granted;
    if (!granted) {
      const req = await Notifications.requestPermissionsAsync();
      granted = req.granted;
    }
    if (!granted) return null;
    const token = (await Notifications.getExpoPushTokenAsync()).data;
    // Best-effort register with backend — enqueues for retry if offline
    const authToken = useAuthStore.getState().token;
    if (authToken) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me/push-token`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${authToken}`,
        },
        body: JSON.stringify({ expo_push_token: token, platform: Platform.OS }),
      }).catch(() => {});
    }
    return token;
  } catch {
    return null;
  }
}

export function addPushListener(
  onNotification: (data: Record<string, any>) => void
): () => void {
  const sub = Notifications.addNotificationReceivedListener((n) => {
    onNotification(n.request.content.data as Record<string, any>);
  });
  const respSub = Notifications.addNotificationResponseReceivedListener((r) => {
    onNotification(r.notification.request.content.data as Record<string, any>);
  });
  return () => {
    sub.remove();
    respSub.remove();
  };
}
