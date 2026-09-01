import * as Notifications from 'expo-notifications';
import { Linking, Platform } from 'react-native';
import { VoiceAnnouncement } from './voiceAnnouncement';
import { getApiBaseURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

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

export interface DispatchNotificationPayload {
  tripNumber: string;
  origin: string;
  destination: string;
  advanceAmount?: number;
  tripId?: string;
}

class MobileNotificationService {
  private initialized = false;
  private responseListenerSub: Notifications.Subscription | null = null;
  private onAcceptDispatchCallback: ((tripId: string) => void) | null = null;

  setOnAcceptDispatch(cb: (tripId: string) => void) {
    this.onAcceptDispatchCallback = cb;
  }

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

      // Configure category with actionable buttons for the notification bar
      await Notifications.setNotificationCategoryAsync('dispatch_actions', [
        {
          identifier: 'ACCEPT_LOAD',
          buttonTitle: '✓ ACCEPT LOAD',
          options: { opensAppToForeground: true },
        },
        {
          identifier: 'CALL_HUB',
          buttonTitle: '📞 CALL HUB',
          options: { opensAppToForeground: false },
        },
      ]);

      // Listen for notification action taps from the system notification bar
      if (!this.responseListenerSub) {
        this.responseListenerSub = Notifications.addNotificationResponseReceivedListener((response) => {
          const actionId = response.actionIdentifier;
          const data = response.notification.request.content.data as any;

          if (actionId === 'ACCEPT_LOAD' || actionId === Notifications.DEFAULT_ACTION_IDENTIFIER) {
            const targetTripId = data?.tripId || data?.tripNumber;
            if (targetTripId && this.onAcceptDispatchCallback) {
              this.onAcceptDispatchCallback(targetTripId);
            }
          } else if (actionId === 'CALL_HUB') {
            Linking.openURL('tel:+919820012345').catch(() => {});
          }
        });
      }

      const { status: existingStatus } = await Notifications.getPermissionsAsync();
      let finalStatus = existingStatus;
      if (existingStatus !== 'granted') {
        const { status } = await Notifications.requestPermissionsAsync();
        finalStatus = status;
      }

      this.initialized = true;
      console.log('[NOTIFICATION SERVICE] Initialized with actionable categories');
      if (finalStatus === 'granted') {
        this.syncPushToken().catch(() => {});
      }
      return finalStatus === 'granted';
    } catch (e: any) {
      console.log('[NOTIFICATION INIT WARNING]', e?.message);
      return false;
    }
  }

  async syncPushToken(): Promise<void> {
    try {
      const token = useAuthStore.getState().token;
      if (!token) return;

      const pushTokenData = await Notifications.getExpoPushTokenAsync().catch(() => null);
      if (!pushTokenData?.data) return;

      const modelName = (Platform.constants as any)?.Model || (Platform.constants as any)?.Brand || 'android';
      const deviceId = `${Platform.OS}-${modelName}`;
      await fetch(`${getApiBaseURL()}/api/v1/drivers/me/push-token`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          device_id: deviceId,
          push_token: pushTokenData.data,
          platform: Platform.OS,
        }),
      });
      console.log('[PUSH TOKEN REGISTERED]', pushTokenData.data);
    } catch (e: any) {
      console.log('[PUSH TOKEN SYNC WARNING]', e?.message);
    }
  }

  // Trigger instant local notification in system notification bar + vernacular voice
  async showDispatchNotification(payload: DispatchNotificationPayload | string, origin?: string, destination?: string): Promise<void> {
    try {
      await this.init();

      const tripNumber = typeof payload === 'string' ? payload : payload.tripNumber;
      const tripOrigin = typeof payload === 'string' ? (origin || 'Fleet Hub') : payload.origin;
      const tripDest = typeof payload === 'string' ? (destination || 'Delivery Location') : payload.destination;
      const tripId = typeof payload === 'object' ? (payload.tripId || tripNumber) : tripNumber;
      const advance = typeof payload === 'object' ? payload.advanceAmount : 5000;

      // 1. Android Notification Bar Heads-Up Banner with Action Buttons
      await Notifications.scheduleNotificationAsync({
        content: {
          title: `🚚 New Trip Assigned: #${tripNumber}`,
          body: `Route: ${tripOrigin} ➔ ${tripDest}\nAdvance: ₹${(advance || 5000).toLocaleString('en-IN')}`,
          data: { tripNumber, tripId, origin: tripOrigin, destination: tripDest, type: 'dispatch' },
          categoryIdentifier: 'dispatch_actions',
          sound: 'default',
          badge: 1,
        },
        trigger: {
          channelId: 'dispatches',
        } as any,
      });

      // 2. Vernacular Voice Prompt
      await VoiceAnnouncement.announceDispatch({
        tripNumber,
        origin: tripOrigin,
        destination: tripDest,
        advanceAmount: advance,
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
        trigger: {
          channelId: 'emergency',
        } as any,
      });

      await VoiceAnnouncement.announceSOS();
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
        trigger: {
          channelId: 'general',
        } as any,
      });
    } catch (e: any) {
      console.log('[NOTIFICATION GENERAL ERROR]', e?.message);
    }
  }
}

export const NotificationService = new MobileNotificationService();
