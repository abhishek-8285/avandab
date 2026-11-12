const path = require('path');

const CONFIG_PATH = path.join(__dirname, '..', 'app.config.js');

// Loads a fresh app.config.js with EXPO_PUBLIC_API_SCHEME forced to `scheme`
// (or unset when undefined), then restores the caller's env.
function loadConfig(scheme) {
  jest.resetModules();
  const previous = process.env.EXPO_PUBLIC_API_SCHEME;
  if (scheme === undefined) {
    delete process.env.EXPO_PUBLIC_API_SCHEME;
  } else {
    process.env.EXPO_PUBLIC_API_SCHEME = scheme;
  }
  try {
    return require(CONFIG_PATH);
  } finally {
    if (previous === undefined) {
      delete process.env.EXPO_PUBLIC_API_SCHEME;
    } else {
      process.env.EXPO_PUBLIC_API_SCHEME = previous;
    }
  }
}

describe('app.config', () => {
  it('sets the iOS bundle identifier to match the Android package', () => {
    const { expo } = loadConfig();
    expect(expo.ios.bundleIdentifier).toBe('com.avandab.mobile');
    expect(expo.android.package).toBe('com.avandab.mobile');
  });

  it('blocks cleartext traffic by default (https)', () => {
    const { expo } = loadConfig();
    expect(expo.android.usesCleartextTraffic).toBe(false);
  });

  it('allows cleartext traffic only for http dev servers', () => {
    const { expo } = loadConfig('http');
    expect(expo.android.usesCleartextTraffic).toBe(true);
  });
});
