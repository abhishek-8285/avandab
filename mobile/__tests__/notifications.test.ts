import {
  Notify,
  _setNotificationsModuleForTests,
} from '../src/services/notifications';

// expo-notifications is an optional dep — resolve via guarded require so tsc
// passes without type declarations; jest setup.ts virtual mock serves it.
// eslint-disable-next-line @typescript-eslint/no-require-imports
const ExpoNotifications = require('expo-notifications') as {
  scheduleNotificationAsync: jest.Mock;
  requestPermissionsAsync: jest.Mock;
};

const mockedSchedule = ExpoNotifications.scheduleNotificationAsync as jest.Mock;
const mockedRequestPermissions = ExpoNotifications.requestPermissionsAsync as jest.Mock;

describe('NotificationService — module available (setup.ts virtual mock)', () => {
  test('isAvailable true', () => {
    expect(Notify.isAvailable()).toBe(true);
  });

  test('requestPermission delegates and resolves granted', async () => {
    await expect(Notify.requestPermission()).resolves.toBe(true);
    expect(mockedRequestPermissions).toHaveBeenCalled();
  });

  test('showTripAssignedNotification delegates with content + identifier', async () => {
    await Notify.showTripAssignedNotification('TR-101', 'Pune');

    expect(mockedSchedule).toHaveBeenCalledTimes(1);
    expect(mockedSchedule).toHaveBeenCalledWith({
      content: { title: 'New trip · नया ट्रिप', body: 'TR-101 → Pune' },
      trigger: null,
      identifier: 'trip_TR-101',
    });
  });

  test('showDocumentExpiryWarning uses docexp_ identifier', async () => {
    await Notify.showDocumentExpiryWarning('DL', '2026-09-30');

    const arg = mockedSchedule.mock.calls[0][0];
    expect(arg.identifier).toBe('docexp_DL');
    expect(arg.trigger).toBeNull();
    expect(arg.content.title).toContain('Document expiring');
    expect(arg.content.body).toContain('DL');
  });
});

describe('NotificationService — forced unavailable (test seam)', () => {
  let consoleInfoSpy: jest.SpyInstance;
  const realModule = ExpoNotifications;

  beforeEach(() => {
    consoleInfoSpy = jest.spyOn(console, 'info').mockImplementation(() => {});
    _setNotificationsModuleForTests(null);
  });

  afterEach(() => {
    _setNotificationsModuleForTests(realModule);
    consoleInfoSpy.mockRestore();
  });

  test('isAvailable false', () => {
    expect(Notify.isAvailable()).toBe(false);
  });

  test('requestPermission false, warns once', async () => {
    await expect(Notify.requestPermission()).resolves.toBe(false);
    expect(consoleInfoSpy).toHaveBeenCalledTimes(1);
  });

  test('show methods are graceful no-ops; warn fires only once', async () => {
    await Notify.showTripAssignedNotification('TR-9', 'Nashik');
    await Notify.showDocumentExpiryWarning('PAN', '2027-01-01');

    expect(mockedSchedule).not.toHaveBeenCalled();
    expect(consoleInfoSpy).toHaveBeenCalledTimes(1);
  });

  test('restoring module re-enables delegation', async () => {
    _setNotificationsModuleForTests(realModule);
    await Notify.showTripAssignedNotification('TR-2', 'Mumbai');
    expect(mockedSchedule).toHaveBeenCalledWith(
      expect.objectContaining({ identifier: 'trip_TR-2' })
    );
  });
});
