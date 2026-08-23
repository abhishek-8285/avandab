import { MQTT, TripDispatchUpdate } from '../src/services/mqtt';
import { useAuthStore } from '../src/stores/authStore';

// Richer local mock capturing connect options + event handlers.
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

const session = () => ({
  id: 'u_1',
  name: 'Raj',
  role: 'driver',
  email: 'r@x.com',
  driverId: 'drv_1',
});

describe('MQTTTelemetryService', () => {
  beforeEach(async () => {
    await useAuthStore.getState().setAuth('tok', session());
  });

  test('connect uses persistent-session options with credentials', async () => {
    MQTT.connect('drv_1');

    expect(mqttModule.connect).toHaveBeenCalledTimes(1);
    const [, options] = mqttModule.connect.mock.calls[0];
    expect(options).toMatchObject({
      clientId: 'driver_drv_1', // deterministic — no random suffix
      clean: false,
      username: 'drv_1',
      password: 'tok',
      reconnectPeriod: 1000,
    });
  });

  test('clientId is deterministic across reconnects', () => {
    MQTT.connect('drv_1');
    const id1 = mqttModule.connect.mock.calls[0][1].clientId;
    MQTT.connect('drv_1');
    const id2 = mqttModule.connect.mock.calls[1][1].clientId;
    expect(id1).toBe(id2);
  });

  test('omits password when no token is present', () => {
    useAuthStore.setState({ token: null });
    MQTT.connect('drv_1');
    const [, options] = mqttModule.connect.mock.calls[0];
    expect(options.password).toBeUndefined();
  });

  test('connect passes clean flag through', () => {
    MQTT.connect('drv_1', true);
    const [, options] = mqttModule.connect.mock.calls[0];
    expect(options.clean).toBe(true);
  });

  test('subscribes to both update topics on connect', () => {
    MQTT.connect('drv_9');
    emit('connect');

    const { subscribe } = mqttModule.__getClient();
    expect(subscribe).toHaveBeenCalledWith(
      'avandab/trips/drivers/drv_9/updates',
      expect.any(Function),
    );
    expect(subscribe).toHaveBeenCalledWith(
      'avandab/drivers/drv_9/updates',
      expect.any(Function),
    );
  });

  test('message on updates topic emits dispatch to listeners', () => {
    const listener = jest.fn();
    const unsub = MQTT.onDispatch((u: TripDispatchUpdate) => listener(u));

    MQTT.connect('drv_1');
    emit(
      'message',
      'avandab/drivers/drv_1/updates',
      Buffer.from(JSON.stringify({ trip_id: 'trip_x', status: 'assigned', time: 'now' })),
    );
    expect(listener).toHaveBeenCalledWith({ trip_id: 'trip_x', status: 'assigned', time: 'now' });

    unsub();
    emit(
      'message',
      'avandab/trips/drivers/drv_1/updates',
      Buffer.from(JSON.stringify({ trip_id: 'trip_y', status: 'started', time: 'later' })),
    );
    expect(listener).toHaveBeenCalledTimes(1); // unsubscribed
  });

  test('publishLocation publishes QoS 1 GPS only when connected', () => {
    MQTT.disconnect(); // reset singleton state from earlier tests
    MQTT.publishLocation('drv_1', 19.076, 72.8777);
    expect(mqttModule.__getClient().publish).not.toHaveBeenCalled(); // not connected yet

    MQTT.connect('drv_1');
    emit('connect');
    MQTT.publishLocation('drv_1', 19.076, 72.8777);

    const { publish } = mqttModule.__getClient();
    expect(publish).toHaveBeenCalledWith(
      'avandab/telemetry/drivers/drv_1/gps',
      expect.stringContaining('"latitude":19.076'),
      { qos: 1 },
    );

    emit('close'); // disconnected mid-flight
    publish.mockClear();
    MQTT.publishLocation('drv_1', 20.0, 73.0);
    expect(publish).not.toHaveBeenCalled();
  });

  test('reconnect backoff grows exponentially on close and resets on connect', () => {
    MQTT.connect('drv_1');
    const client = mqttModule.__getClient();

    emit('close');
    expect(client.options.reconnectPeriod).toBe(2000);
    emit('close');
    expect(client.options.reconnectPeriod).toBe(4000);
    emit('close');
    expect(client.options.reconnectPeriod).toBe(8000);

    emit('connect');
    expect(client.options.reconnectPeriod).toBe(1000); // reset

    for (let i = 0; i < 10; i++) emit('close');
    expect(client.options.reconnectPeriod).toBe(30000); // capped at max
  });

  test('disconnect ends the client', () => {
    MQTT.connect('drv_1');
    MQTT.disconnect();
    expect(mqttModule.__getClient().end).toHaveBeenCalled();
  });
});
