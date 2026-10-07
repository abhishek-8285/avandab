// Claim 2c: the hardened broker (allow_anonymous false + acl_file) binds an
// authenticated username to a topic level. A phone that connects anonymously —
// or that publishes GPS under an identity other than the one its credential is
// bound to — has its fixes dropped by the broker while still seeing
// PUBACK RC:0, so nothing on the device reveals the loss.
import { MQTT } from '../src/services/mqtt';
import { getBrokerCredential } from '../src/services/mqttCredentials';
import { useAuthStore } from '../src/stores/authStore';

jest.mock('mqtt', () => {
  const mockClient = {
    on: jest.fn(),
    subscribe: jest.fn(),
    publish: jest.fn(),
    end: jest.fn(),
    options: {} as Record<string, unknown>,
  };
  return { connect: jest.fn(() => mockClient), __getClient: () => mockClient };
});

jest.mock('../src/services/mqttCredentials', () => ({
  getBrokerCredential: jest.fn(),
  storedBrokerCredential: jest.fn(),
  clearCachedBrokerCredential: jest.fn(),
}));

const mqttModule = jest.requireMock('mqtt') as {
  connect: jest.Mock;
  __getClient: () => { on: jest.Mock; publish: jest.Mock; options: Record<string, unknown> };
};
const credMock = getBrokerCredential as jest.MockedFunction<typeof getBrokerCredential>;

function emit(event: string, ...args: unknown[]): void {
  const handler = mqttModule
    .__getClient()
    .on.mock.calls.find(([e]) => e === event)?.[1] as ((...a: unknown[]) => void) | undefined;
  if (!handler) throw new Error(`No handler for "${event}"`);
  handler(...args);
}

async function connectAndEmitConnected(driverId: string): Promise<Record<string, unknown>> {
  await MQTT.connect(driverId);
  emit('connect');
  return mqttModule.connect.mock.calls[0][1] as Record<string, unknown>;
}

describe('MQTT broker credential', () => {
  beforeEach(async () => {
    // Start from a clean singleton but keep the mqtt module mock intact.
    MQTT.disconnect();
    jest.clearAllMocks();
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
      driverId: 'drv_1',
    } as any);
  });

  test('connects with the provisioned credential instead of the session token', async () => {
    credMock.mockResolvedValue({ username: 'DRV-BROKER-1', password: 'broker-secret' });

    const options = await connectAndEmitConnected('drv_1');

    expect(options.username).toBe('DRV-BROKER-1');
    expect(options.password).toBe('broker-secret');
    expect(options.password).not.toBe('tok');
  });

  test('publishes GPS under the identity the credential is bound to', async () => {
    credMock.mockResolvedValue({ username: 'DRV-BROKER-1', password: 'broker-secret' });
    await connectAndEmitConnected('drv_1');

    MQTT.publishLocation('drv_1', 19.07, 72.83);

    const [topic, payload] = mqttModule.__getClient().publish.mock.calls[0];
    expect(topic).toBe('avandab/telemetry/drivers/DRV-BROKER-1/gps');
    expect(JSON.parse(payload as string).driver_id).toBe('DRV-BROKER-1');
  });

  // Dev brokers are anonymous: without a credential the app must keep working
  // exactly as before, publishing under the app identity with the JWT.
  test('falls back to the app identity and session token when no credential exists', async () => {
    credMock.mockResolvedValue(null);

    const options = await connectAndEmitConnected('drv_1');
    expect(options.username).toBe('drv_1');
    expect(options.password).toBe('tok');

    MQTT.publishLocation('drv_1', 19.07, 72.83);
    const [topic, payload] = mqttModule.__getClient().publish.mock.calls[0];
    expect(topic).toBe('avandab/telemetry/drivers/drv_1/gps');
    expect(JSON.parse(payload as string).driver_id).toBe('drv_1');
  });

  // A credential service that throws (offline, 500, bad JSON) must not take
  // the dispatch connection down with it.
  test('a credential lookup failure degrades to anonymous instead of crashing', async () => {
    credMock.mockRejectedValue(new Error('network down'));

    const options = await connectAndEmitConnected('drv_1');

    expect(options.username).toBe('drv_1');
    expect(options.password).toBe('tok');
  });
});
