import { CrashReporter } from '../src/services/crashReporter';
import { Analytics } from '../src/services/analytics';

describe('CrashReporter Service', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    jest.clearAllMocks();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it('initializes without throwing and handles subsequent init calls', () => {
    expect(() => CrashReporter.init()).not.toThrow();
    // Subsequent call hits early return
    expect(() => CrashReporter.init()).not.toThrow();
  });

  it('captures errors and routes to Analytics.track with $exception', () => {
    const trackSpy = jest.spyOn(Analytics, 'track').mockImplementation(() => {});
    const testErr = new Error('Database connection failed');

    CrashReporter.reportError(testErr, { tripId: 'trip_123', driverId: 'drv_456' });

    expect(trackSpy).toHaveBeenCalledWith(
      '$exception',
      expect.objectContaining({
        $exception_message: 'Database connection failed',
        $exception_type: 'Error',
        tripId: 'trip_123',
        driverId: 'drv_456',
      })
    );

    trackSpy.mockRestore();
  });

  it('handles non-Error objects safely', () => {
    const trackSpy = jest.spyOn(Analytics, 'track').mockImplementation(() => {});

    CrashReporter.reportError('String exception error', { extra: 'context' });

    expect(trackSpy).toHaveBeenCalledWith(
      '$exception',
      expect.objectContaining({
        $exception_message: 'String exception error',
        $exception_type: 'Error',
        extra: 'context',
      })
    );

    trackSpy.mockRestore();
  });

  it('handles analytics track failure gracefully', () => {
    const trackSpy = jest.spyOn(Analytics, 'track').mockImplementation(() => {
      throw new Error('Analytics failed');
    });

    expect(() => CrashReporter.reportError(new Error('Test error'))).not.toThrow();
    trackSpy.mockRestore();
  });
});
