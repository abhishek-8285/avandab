// Coverage strengthening for MQTTTelemetryService edge branches:
// subscribe ack/error callbacks, malformed dispatch payloads, error events,
// and connect-time crashes.
import { MQTT } from '../src/services/mqtt';
import { useAuthStore } from '../src/stores/authStore';

jest.mock('mqtt', () => {
  const mockClient = {
    on: jest.fn(),
    subscribe: jest.fn(),
    publish: jest.fn(),
    end: jest.fn(),
    options: {} as Record<string, unknown>,
  };
  const connect = jest.fn(() => mockClient);
  return { connect, __getClient: () => mockClient };
});

interface MockMqttModule {
  connect: jest.Mock;
  __getClient: () => {
    on: jest.Mock;
    subscribe: jest.Mock;
    publish: jest.Mock;
    end: jest.Mock;
    options: Record<string, unknown>;
  };
}

const mqttModule = (() => jest.requireMock('mqtt'))() as MockMqttModule;

function emit(event: string, ...args: unknown[]): void {
  const handler = mqttModule
    .__getClient()
    .on.mock.calls.find(([e]) => e === event)?.[1] as ((...a: unknown[]) => void) | undefined;
  if (!handler) throw new Error(`No registered handler for event "${event}"`);
  handler(...args);
}

function subscriberCallbacks(): ((err?: Error | null) => void)[] {
  return mqttModule
    .__getClient()
    .subscribe.mock.calls.map(([, cb]) => cb as (err?: Error | null) => void);
}

// The broker credential is fetched asynchronously before connecting; the
// anonymous path (dev broker) is the default here unless a test overrides it.
jest.mock('../src/services/mqttCredentials', () => ({
  getBrokerCredential: jest.fn().mockResolvedValue(null),
  storedBrokerCredential: jest.fn().mockResolvedValue(null),
  clearCachedBrokerCredential: jest.fn(),
}));

describe('MQTT edge branches', () => {
  let logSpy: jest.SpyInstance;

  beforeEach(async () => {
    logSpy = jest.spyOn(console, 'log').mockImplementation(() => {});
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    MQTT.disconnect();
    logSpy.mockRestore();
  });

  test('subscribe ack logs the topic; subscribe errors are tolerated silently', async () => {
    await MQTT.connect('drv_1');
    emit('connect');

    const cbs = subscriberCallbacks();
    expect(cbs).toHaveLength(2);

    expect(() => cbs[0](null)).not.toThrow();
    expect(logSpy).toHaveBeenCalledWith(expect.stringContaining('[MQTT SUBSCRIBED]'));

    const callsBefore = logSpy.mock.calls.length;
    expect(() => cbs[1](new Error('subscription rejected'))).not.toThrow();
    expect(logSpy.mock.calls.length).toBe(callsBefore); // no success log for failures
  });

  test('messages on non-updates topics are ignored without JSON parsing', async () => {
    const listener = jest.fn();
    MQTT.onDispatch(listener);
    await MQTT.connect('drv_1');

    expect(() =>
      emit('message', 'avandab/telemetry/drivers/drv_1/gps', Buffer.from('not json at all'))
    ).not.toThrow();
    expect(listener).not.toHaveBeenCalled();
  });

  test('malformed JSON on an updates topic is swallowed', async () => {
    const listener = jest.fn();
    MQTT.onDispatch(listener);
    await MQTT.connect('drv_1');

    expect(() =>
      emit('message', 'avandab/drivers/drv_1/updates', Buffer.from('{broken'))
    ).not.toThrow();
    expect(listener).not.toHaveBeenCalled();
  });

  test('payload with non-string trip_id is dropped; missing status/time default to empty strings', async () => {
    const listener = jest.fn();
    MQTT.onDispatch(listener);
    await MQTT.connect('drv_1');

    // trip_id must be a string — numbers are not a dispatch update.
    emit('message', 'avandab/drivers/drv_1/updates', Buffer.from(JSON.stringify({ trip_id: 42 })));
    expect(listener).not.toHaveBeenCalled();

    emit('message', 'avandab/drivers/drv_1/updates', Buffer.from(JSON.stringify({ trip_id: 't9' })));
    expect(listener).toHaveBeenCalledWith({ trip_id: 't9', status: '', time: '' });
  });

  test('broker error events are logged and never thrown', async () => {
    await MQTT.connect('drv_1');
    expect(() => emit('error', new Error('ECONNREFUSED'))).not.toThrow();
    expect(logSpy).toHaveBeenCalledWith(expect.stringContaining('[MQTT MOBILE WARNING]'), expect.anything());
  });

  test('connect-time crash is contained behind an init warning', async () => {
    mqttModule.connect.mockImplementationOnce(() => {
      throw new Error('no such broker');
    });

    await expect(MQTT.connect('drv_1')).resolves.toBeUndefined();
    expect(logSpy).toHaveBeenCalledWith('[MQTT INIT WARNING]', 'no such broker');
  });

  test('disconnect on a never-connected singleton is a safe no-op', () => {
    // The shared singleton keeps its ended client reference after disconnect(),
    // so use a fresh module instance to exercise the no-client branch.
    let Fresh: typeof import('../src/services/mqtt') | undefined;
    jest.isolateModules(() => {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      Fresh = require('../src/services/mqtt');
    });

    const endsBefore = mqttModule.__getClient().end.mock.calls.length;
    expect(Fresh!.MQTT.disconnect()).toBeUndefined();
    expect(mqttModule.connect).not.toHaveBeenCalled();
    expect(mqttModule.__getClient().end.mock.calls.length).toBe(endsBefore);
  });
});
