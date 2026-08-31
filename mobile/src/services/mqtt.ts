import mqtt from 'mqtt';
import { getMQTTBrokerURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { NotificationService } from './notificationService';

export interface TripDispatchUpdate {
  trip_id: string;
  status: string;
  time: string;
}

type DispatchListener = (update: TripDispatchUpdate) => void;

// Custom exponential reconnect backoff bounds
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

class MQTTTelemetryService {
  private client: mqtt.MqttClient | null = null;
  private isConnected = false;
  private dispatchListeners: Set<DispatchListener> = new Set();
  private reconnectAttempt = 0;

  /**
   * Subscribe to in-app dispatch notifications (trip assignments/status
   * changes pushed over the driver updates topics). Returns an
   * unsubscribe function.
   */
  onDispatch(listener: DispatchListener): () => void {
    this.dispatchListeners.add(listener);
    return () => this.dispatchListeners.delete(listener);
  }

  private emitDispatch(update: TripDispatchUpdate): void {
    this.dispatchListeners.forEach((fn) => {
      try {
        fn(update);
      } catch {
        // listener errors never break the MQTT loop
      }
    });
  }

  connect(driverId: string, clean = false): void {
    try {
      const brokerUrl = getMQTTBrokerURL();
      const token = useAuthStore.getState().token;
      this.reconnectAttempt = 0;

      const options: mqtt.IClientOptions = {
        // Persistent session: deterministic clientId (no random suffix — a
        // random one breaks broker queueing for offline drivers) + clean:false
        // so missed dispatches survive disconnects/restarts.
        clientId: `driver_${driverId}`,
        clean,
        keepalive: 60,
        reconnectPeriod: RECONNECT_BASE_MS,
        username: driverId,
      };
      if (token) {
        options.password = token;
      }

      // Connect to MQTT Broker over WebSockets
      this.client = mqtt.connect(brokerUrl, options);

      this.client.on('connect', () => {
        this.isConnected = true;
        // Reset custom backoff after a successful connect
        this.reconnectAttempt = 0;
        if (this.client) {
          this.client.options.reconnectPeriod = RECONNECT_BASE_MS;
        }
        console.log('[MQTT MOBILE SUCCESS] Connected to MQTT Telemetry Broker');

        // Subscribe to both driver update topics (legacy + spec)
        const topics = [
          `avandab/trips/drivers/${driverId}/updates`,
          `avandab/drivers/${driverId}/updates`,
        ];
        topics.forEach((topic) => {
          this.client?.subscribe(topic, (err) => {
            if (!err) {
              console.log(`[MQTT SUBSCRIBED] Listening on topic: ${topic}`);
            }
          });
        });
      });

      this.client.on('close', () => {
        this.isConnected = false;
        // Custom exponential backoff: 1s doubling up to 30s
        this.reconnectAttempt += 1;
        if (this.client) {
          this.client.options.reconnectPeriod = Math.min(
            RECONNECT_BASE_MS * Math.pow(2, this.reconnectAttempt),
            RECONNECT_MAX_MS,
          );
        }
      });

      this.client.on('message', (topic, message) => {
        console.log(`[MQTT RECV] Topic: ${topic} Payload: ${message.toString()}`);
        if (topic.includes('/updates')) {
          try {
            const parsed = JSON.parse(message.toString());
            if (parsed && typeof parsed.trip_id === 'string') {
              this.emitDispatch({
                trip_id: parsed.trip_id,
                status: typeof parsed.status === 'string' ? parsed.status : '',
                time: typeof parsed.time === 'string' ? parsed.time : '',
              });
              // Push system notification bar alert
              NotificationService.showDispatchNotification(
                parsed.trip_id,
                parsed.origin || 'Fleet Hub',
                parsed.destination || 'Delivery Location',
              ).catch(() => {});
            }
          } catch {
            // non-JSON payload — log only
          }
        }
      });

      this.client.on('error', (err) => {
        console.log('[MQTT MOBILE WARNING] Connection warning (fallback to HTTP):', err.message);
      });
    } catch (e: any) {
      console.log('[MQTT INIT WARNING]', e.message);
    }
  }

  // Publish high-frequency live GPS coordinates over MQTT
  publishLocation(driverId: string, latitude: number, longitude: number): void {
    if (this.client && this.isConnected) {
      const topic = `avandab/telemetry/drivers/${driverId}/gps`;
      const payload = JSON.stringify({
        driver_id: driverId,
        latitude,
        longitude,
        timestamp: new Date().toISOString(),
      });
      this.client.publish(topic, payload, { qos: 1 });
      console.log(`[MQTT PUBLISHED GPS] Lat: ${latitude.toFixed(4)}, Lng: ${longitude.toFixed(4)} -> ${topic}`);
    }
  }

  disconnect(): void {
    if (this.client) {
      this.client.end();
      this.isConnected = false;
    }
  }
}

export const MQTT = new MQTTTelemetryService();
