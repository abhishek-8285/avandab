import mqtt from 'mqtt';
import { getMQTTBrokerURL } from '../constants/network';
import { useAuthStore } from '../stores/authStore';
import { NotificationService } from './notificationService';
import { getBrokerCredential, type BrokerCredential } from './mqttCredentials';

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
  // The hardened broker's ACL binds an authenticated username to a topic level
  // (`pattern write avandab/telemetry/drivers/%u/gps`), so publishes must use
  // the credential's identity — publishing under a different id is dropped by
  // the broker with PUBACK RC:0, i.e. silently.
  private topicIdentity: string | null = null;

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

  async connect(driverId: string, clean = false): Promise<void> {
    try {
      const brokerUrl = getMQTTBrokerURL();
      const token = useAuthStore.getState().token;
      this.reconnectAttempt = 0;

      // A hardened broker (allow_anonymous false) needs the per-driver
      // credential; an anonymous dev broker has none to hand out, and then the
      // JWT keeps working exactly as before.
      let cred: BrokerCredential | null = null;
      try {
        cred = await getBrokerCredential();
      } catch (e: any) {
        console.log('[MQTT] broker credential unavailable:', e?.message);
      }
      this.topicIdentity = cred?.username ?? null;
      if (cred && cred.username !== driverId) {
        console.log(`[MQTT] publishing as broker identity ${cred.username} (app identity ${driverId})`);
      }

      const options: mqtt.IClientOptions = {
        // Persistent session: deterministic clientId (no random suffix — a
        // random one breaks broker queueing for offline drivers) + clean:false
        // so missed dispatches survive disconnects/restarts.
        clientId: `driver_${driverId}`,
        clean,
        keepalive: 60,
        reconnectPeriod: RECONNECT_BASE_MS,
        username: cred?.username ?? driverId,
        // The tunnel routes only this path to mosquitto's WS listener; a bare
        // "/" would be served by the web app instead (mqtt.js defaults to "/").
        path: '/mqtt',
      };
      if (cred) {
        options.password = cred.password;
      } else if (token) {
        options.password = token;
      }

      // Connect to MQTT Broker over WebSockets. The app re-runs its bootstrap
      // effect when the driver identity resolves (user.driverId lands from
      // GET /drivers/me), so this can run twice per session: end the previous
      // socket instead of orphaning a live client nothing can publish through.
      this.client?.end(true);
      this.isConnected = false;
      this.client = mqtt.connect(brokerUrl, options);
      // One identity for publish and subscribe: the broker ACL binds the
      // credential to a topic level (`%u`), so subscribing under the app
      // identity while publishing under the credential's would miss.
      const identity = this.topicIdentity ?? driverId;

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
          `avandab/trips/drivers/${identity}/updates`,
          `avandab/drivers/${identity}/updates`,
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

  // Publish high-frequency live GPS coordinates over MQTT.
  // Additive-only payload evolution: stale fixes add `is_stale: true` so the
  // server can tell a flagged re-observation from a fresh measurement.
  // Coarse viewport fallbacks must never reach here (callers gate with
  // canPublishFix) — this layer publishes whatever it is handed.
  publishLocation(driverId: string, latitude: number, longitude: number, opts?: { isStale?: boolean }): void {
    if (this.client && this.isConnected) {
      // Publish under the identity the broker credential is bound to; the
      // broker drops (not rejects) anything published under another id.
      const identity = this.topicIdentity ?? driverId;
      const topic = `avandab/telemetry/drivers/${identity}/gps`;
      const payload = JSON.stringify({
        driver_id: identity,
        latitude,
        longitude,
        timestamp: new Date().toISOString(),
        ...(opts?.isStale ? { is_stale: true } : {}),
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
