export const API_SCHEME = process.env.EXPO_PUBLIC_API_SCHEME || 'https';
export const MQTT_SCHEME = process.env.EXPO_PUBLIC_MQTT_SCHEME || 'wss';
export const BACKEND_HOST = process.env.EXPO_PUBLIC_BACKEND_HOST || 'avandab.com';
export const API_PORT = process.env.EXPO_PUBLIC_API_PORT ? Number(process.env.EXPO_PUBLIC_API_PORT) : 443;
export const MQTT_BROKER_PORT = process.env.EXPO_PUBLIC_MQTT_BROKER_PORT ? Number(process.env.EXPO_PUBLIC_MQTT_BROKER_PORT) : 8883;

export const API_BASE_URL = API_PORT === 443 || API_PORT === 80
  ? `${API_SCHEME}://${BACKEND_HOST}`
  : `${API_SCHEME}://${BACKEND_HOST}:${API_PORT}`;
export const MQTT_BROKER_URL = MQTT_BROKER_PORT === 443 || MQTT_BROKER_PORT === 80
  ? `${MQTT_SCHEME}://${BACKEND_HOST}`
  : `${MQTT_SCHEME}://${BACKEND_HOST}:${MQTT_BROKER_PORT}`;

let customBackendHost: string | null = null;
export function setCustomBackendHost(host: string | null): void {
  customBackendHost = host;
}

export function getBackendHost(): string {
  if (customBackendHost) return customBackendHost;
  return process.env['EXPO_PUBLIC_BACKEND_HOST'] || 'avandab.com';
}

export function getApiBaseURL(): string {
  const scheme = process.env['EXPO_PUBLIC_API_SCHEME'] || 'https';
  const host = getBackendHost();
  const port = process.env['EXPO_PUBLIC_API_PORT'] ? Number(process.env['EXPO_PUBLIC_API_PORT']) : 443;
  return port === 443 || port === 80 ? `${scheme}://${host}` : `${scheme}://${host}:${port}`;
}

export function getMQTTBrokerURL(): string {
  const scheme = process.env['EXPO_PUBLIC_MQTT_SCHEME'] || 'wss';
  const host = getBackendHost();
  const port = process.env['EXPO_PUBLIC_MQTT_BROKER_PORT'] ? Number(process.env['EXPO_PUBLIC_MQTT_BROKER_PORT']) : 8883;
  return port === 443 || port === 80 ? `${scheme}://${host}` : `${scheme}://${host}:${port}`;
}

export const DEFAULT_LATITUDE = 19.076;
export const DEFAULT_LONGITUDE = 72.8777;
export const DEFAULT_DESTINATION_LATITUDE = 18.5204;
export const DEFAULT_DESTINATION_LONGITUDE = 73.8567;
