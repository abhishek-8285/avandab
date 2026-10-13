import { resolvePlaceCoords } from '../src/utils/geocode';

describe('resolvePlaceCoords', () => {
  // Claim 9b: ActiveNavigationScreen's actual trip labels must resolve to
  // real coordinates, otherwise the map has no legs to draw.
  test('resolves the navigation screen default origin/destination labels', () => {
    expect(resolvePlaceCoords('JNPT Port, Navi Mumbai')).toEqual({ latitude: 19.076, longitude: 72.8777 });
    expect(resolvePlaceCoords('Chakan MIDC, Pune')).toEqual({ latitude: 18.5204, longitude: 73.8567 });
  });

  test('unknown or empty labels resolve to null so no leg is fabricated', () => {
    expect(resolvePlaceCoords('Somewhere, Antarctica')).toBeNull();
    expect(resolvePlaceCoords('')).toBeNull();
    expect(resolvePlaceCoords(undefined)).toBeNull();
  });

  test('resolves known hubs', () => {
    expect(resolvePlaceCoords('Pune')).toEqual({ latitude: 18.5204, longitude: 73.8567 });
    expect(resolvePlaceCoords('MUMBAI')).toEqual({ latitude: 19.076, longitude: 72.8777 });
  });

  test('normalizes case and spaces', () => {
    expect(resolvePlaceCoords('chandani chowk, delhi')).toEqual({ latitude: 28.6506, longitude: 77.2306 });
    expect(resolvePlaceCoords('BURARI, DELHI')?.latitude).toBeCloseTo(28.761, 3);
  });

  test('unknown or empty labels return null', () => {
    expect(resolvePlaceCoords('Nowhere XYZ')).toBeNull();
    expect(resolvePlaceCoords('')).toBeNull();
    expect(resolvePlaceCoords()).toBeNull();
    expect(resolvePlaceCoords('!!!')).toBeNull();
  });
});
