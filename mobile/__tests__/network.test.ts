import { getApiBaseURL, getBackendHost, getMQTTBrokerURL } from '../src/constants/network';

describe('network constants', () => {
  test('returns default baseURL and host', () => {
    expect(getBackendHost()).toBeDefined();
    expect(getApiBaseURL()).toBeDefined();
    expect(getMQTTBrokerURL()).toBeDefined();
  });
});

describe('network constants (env overrides)', () => {
  const ORIGINAL_DEV = (global as any).__DEV__;

  afterEach(() => {
    (global as any).__DEV__ = ORIGINAL_DEV;
  });

  test('canonical defaults use https/wss and avandab.com', () => {
    let net: any;
    jest.isolateModules(() => {
      net = require('../src/constants/network');
    });
    expect(net.API_SCHEME).toBe('https');
    expect(net.MQTT_SCHEME).toBe('wss');
    expect(net.getBackendHost()).toBe('avandab.com');
    expect(net.getApiBaseURL()).toBe('https://avandab.com');
    expect(net.getMQTTBrokerURL()).toBe('wss://avandab.com:8883');
  });

  test('custom host overrides respect setCustomBackendHost', () => {
    let net = require('../src/constants/network');
    net.setCustomBackendHost('custom.avandab.com');

    expect(net.getBackendHost()).toBe('custom.avandab.com');
    expect(net.getApiBaseURL()).toBe('https://custom.avandab.com');
    expect(net.getMQTTBrokerURL()).toBe('wss://custom.avandab.com:8883');

    net.setCustomBackendHost(null);
    expect(net.getBackendHost()).toBe('avandab.com');
  });
});
