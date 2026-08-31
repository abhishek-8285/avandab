import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';

// Configure foreground notification behavior (alert, sound, badge)
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowAlert: true,
    shouldPlaySound: true,
    shouldSetBadge: true,
    shouldShowBanner: true,
    priority: Notifications.AndroidNotificationPriority.MAX,
  }),
});

class MobileNotificationService {
  private initialized = false;

  async init(): Promise<boolean> {
    if (this.initialized) return true;

    try {
      if (Platform.OS === 'android') {
        // High-priority channel for load dispatches
        await Notifications.setNotificationChannelAsync('dispatches', {
          name: 'Trip & Dispatch Alerts',
          importance: Notifications.AndroidImportance.MAX,
          vibrationPattern: [0, 250, 250, 250],
          lightColor: '#008069',
          sound: 'default',
          enableLights: true,
          enableVibrate: true,
          showBadge: true,
          lockscreenVisibility: Notifications.AndroidNotificationVisibility.PUBLIC,
        });

        // Emergency / SOS channel
        await Notifications.setNotificationChannelAsync('emergency', {
          name: 'Emergency & Safety Alerts',
          importance: Notifications.AndroidImportance.MAX,
          vibrationPattern: [0, 500, 250, 500, 250, 500],
          lightColor: '#EF4444',
          sound: 'default',
          enableLights: true,
          enableVibrate: true,
          showBadge: true,
          lockscreenVisibility: Notifications.AndroidNotificationVisibility.PUBLIC,
        });

        // General notifications channel
        await Notifications.setNotificationChannelAsync('general', {
          name: 'General Updates & Compliance',
          importance: Notifications.AndroidImportance.HIGH,
          vibrationPattern: [0, 200],
          lightColor: '#25D366',
          sound: 'default',
        });
      }

      const { status: existingStatus } = await Notifications.getPermissionsAsync();
      let finalStatus = existingStatus;
      if (existingStatus !== 'granted') {
        const { status } = await Notifications.requestPermissionsAsync();
        finalStatus = status;
      }

      this.initialized = true;
      console.log('[NOTIFICATION SERVICE] Initialized with permission status:', finalStatus);
      return finalStatus === 'granted';
    } catch (e: any) {
      console.log('[NOTIFICATION INIT WARNING]', e?.message);
      return false;
    }
  }

  // Trigger instant local notification in system notification bar
  async showDispatchNotification(tripNumber: string, origin: string, destination: string): Promise<void> {
    try {
      await this.init();
      await Notifications.scheduleNotificationAsync({
        content: {
          title: `🚚 New Trip Assigned: #${tripNumber}`,
          body: `Route: ${origin} ➔ ${destination}\nTap to start navigation.`,
          data: { tripNumber, origin, destination, type: 'dispatch' },
          sound: 'default',
          badge: 1,
        },
        trigger: null, // trigger immediately
      });
    } catch (e: any) {
      console.log('[NOTIFICATION DISPATCH ERROR]', e?.message);
    }
  }

  async showSOSAlert(tripNumber?: string): Promise<void> {
    try {
      await this.init();
      await Notifications.scheduleNotificationAsync({
        content: {
          title: '🚨 Emergency SOS Dispatched',
          body: `Panic alert broadcasted with GPS coordinates to Avandab Fleet Response Team.`,
          data: { tripNumber, type: 'sos' },
          sound: 'default',
          priority: Notifications.AndroidNotificationPriority.MAX,
        },
        trigger: null,
      });
    } catch (e: any) {
      console.log('[NOTIFICATION SOS ERROR]', e?.message);
    }
  }

  async showGeneralAlert(title: string, body: string, data?: Record<string, any>): Promise<void> {
    try {
      await this.init();
      await Notifications.scheduleNotificationAsync({
        content: {
          title,
          body,
          data: data || {},
          sound: 'default',
        },
        trigger: null,
      });
    } catch (e: any) {
      console.log('[NOTIFICATION GENERAL ERROR]', e?.message);
    }
  }
}

export const NotificationService = new MobileNotificationService();
