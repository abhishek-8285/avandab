// Critical-alert channel: trip assignments + document expiry warnings.
// FCM/APNs wake is server-side; this is the in-app/local surface.
// expo-notifications may be absent (tests/dev builds) — guarded require.

let NotificationsModule: any = null;
try {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  NotificationsModule = require('expo-notifications');
} catch {
  // expo-notifications not installed — service degrades to no-op
}

// Pragmatic test seam: force-unavailable path without fighting the jest mock
export function _setNotificationsModuleForTests(mod: any): void {
  NotificationsModule = mod;
  warnedUnavailable = false;
}

let warnedUnavailable = false;

function resolveModule(): any {
  if (
    NotificationsModule &&
    typeof NotificationsModule.scheduleNotificationAsync === 'function'
  ) {
    return NotificationsModule;
  }
  return null;
}

function warnUnavailableOnce(): void {
  if (!warnedUnavailable) {
    console.info('expo-notifications unavailable — local notifications disabled');
    warnedUnavailable = true;
  }
}

async function schedule(identifier: string, title: string, body: string): Promise<void> {
  const mod = resolveModule();
  if (!mod) {
    warnUnavailableOnce();
    return;
  }
  await mod.scheduleNotificationAsync({
    content: { title, body },
    trigger: null,
    identifier,
  });
}

export class NotificationService {
  isAvailable(): boolean {
    return resolveModule() !== null;
  }

  async requestPermission(): Promise<boolean> {
    const mod = resolveModule();
    if (!mod || typeof mod.requestPermissionsAsync !== 'function') {
      warnUnavailableOnce();
      return false;
    }
    try {
      const result = await mod.requestPermissionsAsync();
      if (result === true) return true;
      return Boolean(result?.granted);
    } catch {
      return false;
    }
  }

  async showTripAssignedNotification(tripNumber: string, destination: string): Promise<void> {
    await schedule(`trip_${tripNumber}`, 'New trip · नया ट्रिप', `${tripNumber} → ${destination}`);
  }

  async showDocumentExpiryWarning(docLabel: string, expiryDate: string): Promise<void> {
    await schedule(
      `docexp_${docLabel}`,
      'Document expiring · दस्तावेज़ समाप्त होने वाला है',
      `${docLabel} · ${expiryDate}`
    );
  }
}

export const Notify = new NotificationService();
