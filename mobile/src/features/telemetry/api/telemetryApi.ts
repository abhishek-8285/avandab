import { getApiBaseURL } from '../../../constants/network';
import { BufferedTelemetryFrame } from '../buffering/sqliteBuffer';

export interface IngestResponse {
  status: 'ACCEPTED' | 'DUPLICATE' | 'STALE' | 'INVALID_SESSION' | 'INVALID_COORDINATES' | 'UNAUTHORIZED';
  message: string;
  client_event_id: string;
  event_id?: string;
  vehicle_id?: string;
}

export class TelemetryApiClient {
  private getHeaders(token: string) {
    return {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    };
  }

  async startSession(
    token: string,
    installationId: string,
    appVersion: string,
    osVersion: string
  ): Promise<{ id: string; vehicle_id?: string }> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/telemetry/sessions/start`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        installation_id: installationId,
        app_version: appVersion,
        os_version: osVersion,
      }),
    });
    if (!res.ok) {
      throw new Error(`Failed starting session: ${res.statusText}`);
    }
    return res.json();
  }

  async endSession(token: string, sessionId: string): Promise<void> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/telemetry/sessions/end`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        session_id: sessionId,
      }),
    });
    if (!res.ok) {
      throw new Error(`Failed ending session: ${res.statusText}`);
    }
  }

  async sendTelemetryEvent(
    token: string,
    frame: BufferedTelemetryFrame
  ): Promise<IngestResponse> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/telemetry/events`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        session_id: frame.session_id,
        installation_id: frame.installation_id,
        client_event_id: frame.client_event_id,
        occurred_at: frame.occurred_at,
        latitude: frame.latitude,
        longitude: frame.longitude,
        accuracy_meters: frame.accuracy_meters,
        speed_kmph: frame.speed_kmph,
        heading_degrees: frame.heading_degrees,
        battery_level_pct: frame.battery_level_pct,
        battery_state: frame.battery_state,
        trip_id: frame.trip_id,
      }),
    });

    if (!res.ok && res.status !== 401 && res.status !== 422) {
      throw new Error(`HTTP Error ${res.status}`);
    }
    return res.json();
  }
}

export const telemetryApi = new TelemetryApiClient();
