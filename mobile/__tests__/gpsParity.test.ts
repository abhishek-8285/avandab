// GPS provider-parity (server migration 00117): speed/heading/motion derived
// from platform coords, battery from expo-battery — all must reach the SQLite
// queue that the sync engine flushes. Battery=null must never break a fix.
import * as Battery from 'expo-battery';
import * as SQLite from 'expo-sqlite';
import { backgroundGPSTask } from '../src/services/backgroundLocation';
import { getSQLiteMockState, resetSQLiteMockState } from '../jest/setup';

jest.mock('expo-battery', () => ({
  getBatteryLevelAsync: jest.fn(),
}));
// expo-location import is required for the task module to load under jest-expo.
jest.mock('expo-location', () => ({}));
jest.mock('../src/services/mqtt', () => ({ MQTT: { publishLocation: jest.fn() } }));

const handler = backgroundGPSTask as (evt: any) => Promise<void>;
const batteryMock = Battery.getBatteryLevelAsync as jest.Mock;

describe('GPS provider-parity capture', () => {
  beforeEach(() => {
    resetSQLiteMockState();
    batteryMock.mockReset();
  });

  test('speed/heading/motion/battery_level land in the offline GPS queue', async () => {
    batteryMock.mockResolvedValue(0.44); // 44%

    await handler({
      data: {
        locations: [
          { coords: { latitude: 19.07, longitude: 72.87, speed: 8.3, heading: 240, accuracy: 6 } },
        ],
      },
    });

    const rows = getSQLiteMockState().offline_gps_logs;
    expect(rows).toHaveLength(1);
    expect(rows[0].speed).toBe(8.3);
    expect(rows[0].heading).toBe(240);
    expect(rows[0].motion).toBe(1); // 8.3 m/s > 0.5 → moving
    expect(rows[0].battery_level).toBe(44);
  });

  test('stationary fix derives motion=0', async () => {
    batteryMock.mockResolvedValue(0.9);
    await handler({
      data: {
        locations: [
          { coords: { latitude: 19.07, longitude: 72.87, speed: 0.1, heading: 0, accuracy: 5 } },
        ],
      },
    });
    expect(getSQLiteMockState().offline_gps_logs[0].motion).toBe(0);
  });

  test('battery failure (null) must never break the GPS fix', async () => {
    batteryMock.mockRejectedValue(new Error('native module missing'));

    await handler({
      data: {
        locations: [
          { coords: { latitude: 19.07, longitude: 72.87, speed: 3, heading: 90, accuracy: 5 } },
        ],
      },
    });

    const rows = getSQLiteMockState().offline_gps_logs;
    expect(rows).toHaveLength(1); // fix persisted
    expect(rows[0].battery_level).toBeNull(); // battery unknown, not fabricated
  });
});
