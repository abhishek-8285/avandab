import { Analytics } from './analytics';

export interface CrashReportContext {
  componentStack?: string;
  driverId?: string;
  tripId?: string;
  [key: string]: unknown;
}

class CrashReporterService {
  private initialized = false;
  private sentryDsn = '';
  private sentryEndpoint = '';

  init(): void {
    if (this.initialized) return;
    this.initialized = true;

    this.sentryDsn = process.env.EXPO_PUBLIC_SENTRY_DSN || '';
    if (this.sentryDsn) {
      try {
        // Parse Sentry DSN: https://<key>@<host>/<projectId>
        const url = new URL(this.sentryDsn);
        const projectId = url.pathname.replace(/^\//, '');
        const key = url.username;
        this.sentryEndpoint = `https://${url.host}/api/${projectId}/store/?sentry_version=7&sentry_key=${key}&sentry_client=avandab-mobile/1.0.0`;
      } catch {
        this.sentryEndpoint = '';
      }
    }

    // React Native global error handler
    const globalObj = global as unknown as {
      ErrorUtils?: {
        getGlobalHandler?: () => (error: Error, isFatal?: boolean) => void;
        setGlobalHandler?: (handler: (error: Error, isFatal?: boolean) => void) => void;
      };
    };

    if (globalObj.ErrorUtils && typeof globalObj.ErrorUtils.setGlobalHandler === 'function') {
      const defaultHandler = globalObj.ErrorUtils.getGlobalHandler ? globalObj.ErrorUtils.getGlobalHandler() : undefined;
      globalObj.ErrorUtils.setGlobalHandler((error: Error, isFatal?: boolean) => {
        this.reportError(error, { isFatal: !!isFatal, source: 'globalHandler' });
        if (defaultHandler) {
          defaultHandler(error, isFatal);
        }
      });
    }
  }

  reportError(error: Error | unknown, context?: CrashReportContext): void {
    const err = error instanceof Error ? error : new Error(String(error));
    const payload = {
      message: err.message,
      name: err.name,
      stack: err.stack,
      context: context || {},
      timestamp: new Date().toISOString(),
    };

    // 1. PostHog exception capture
    try {
      Analytics.track('$exception', {
        $exception_message: err.message,
        $exception_type: err.name,
        $exception_stack_trace_raw: err.stack,
        ...context,
      });
    } catch {
      // Degrade silently
    }

    // 2. Direct Sentry store API transmission if DSN is configured
    if (this.sentryEndpoint) {
      const sentryPayload = {
        event_id: Math.random().toString(36).substring(2, 15) + Math.random().toString(36).substring(2, 15),
        timestamp: new Date().toISOString(),
        platform: 'javascript',
        level: 'error',
        exception: {
          values: [
            {
              type: err.name,
              value: err.message,
              stacktrace: err.stack ? { frames: [{ filename: 'bundle.js', function: err.stack.split('\n')[1]?.trim() || 'unknown' }] } : undefined,
            },
          ],
        },
        extra: context || {},
      };

      fetch(this.sentryEndpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(sentryPayload),
      }).catch(() => {});
    }

    if (typeof __DEV__ !== 'undefined' && __DEV__) {
      console.warn('[CRASH REPORTER CAUGHT ERROR]', payload.message, payload.stack);
    }
  }
}

export const CrashReporter = new CrashReporterService();
