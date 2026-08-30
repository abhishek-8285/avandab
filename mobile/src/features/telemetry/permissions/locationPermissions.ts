import * as Location from 'expo-location';

export interface PermissionStatus {
  foregroundGranted: boolean;
  backgroundGranted: boolean;
  canAskAgain: boolean;
}

export async function checkLocationPermissions(): Promise<PermissionStatus> {
  const fg = await Location.getForegroundPermissionsAsync();
  const bg = await Location.getBackgroundPermissionsAsync();

  return {
    foregroundGranted: fg.granted,
    backgroundGranted: bg.granted,
    canAskAgain: fg.canAskAgain && bg.canAskAgain,
  };
}

export async function requestLocationPermissions(): Promise<PermissionStatus> {
  const fg = await Location.requestForegroundPermissionsAsync();
  if (!fg.granted) {
    return {
      foregroundGranted: false,
      backgroundGranted: false,
      canAskAgain: fg.canAskAgain,
    };
  }

  const bg = await Location.requestBackgroundPermissionsAsync();
  return {
    foregroundGranted: true,
    backgroundGranted: bg.granted,
    canAskAgain: bg.canAskAgain,
  };
}
