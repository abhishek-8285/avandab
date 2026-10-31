import React from 'react';
import { Text } from 'react-native';
import { render, fireEvent } from '@testing-library/react-native';
import { ErrorBoundary } from '../src/components/ErrorBoundary';
import { CrashReporter } from '../src/services/crashReporter';

const ThrowingComponent = ({ shouldThrow }: { shouldThrow: boolean }) => {
  if (shouldThrow) {
    throw new Error('Test crash in child component');
  }
  return <Text>Normal Child Component</Text>;
};

describe('ErrorBoundary Component', () => {
  // Prevent React from logging error boundaries in test output
  const originalError = console.error;
  beforeAll(() => {
    console.error = jest.fn();
  });
  afterAll(() => {
    console.error = originalError;
  });

  it('renders children normally when no error occurs', () => {
    const { getByText } = render(
      <ErrorBoundary>
        <ThrowingComponent shouldThrow={false} />
      </ErrorBoundary>
    );

    expect(getByText('Normal Child Component')).toBeTruthy();
  });

  it('catches render error, reports to CrashReporter, and displays fallback UI', () => {
    const reportSpy = jest.spyOn(CrashReporter, 'reportError').mockImplementation(() => {});

    const { getByTestId, getByText } = render(
      <ErrorBoundary>
        <ThrowingComponent shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(getByTestId('error-boundary-screen')).toBeTruthy();
    expect(getByText('Something went wrong')).toBeTruthy();
    expect(getByText('Test crash in child component')).toBeTruthy();

    expect(reportSpy).toHaveBeenCalledWith(
      expect.objectContaining({ message: 'Test crash in child component' }),
      expect.objectContaining({ source: 'ErrorBoundary' })
    );

    reportSpy.mockRestore();
  });

  it('allows user to restart when clicking retry button', () => {
    const errorSignal = { crash: true };
    const onReset = jest.fn();

    const FallibleComponent = () => {
      if (errorSignal.crash) {
        throw new Error('Dynamic crash');
      }
      return <Text>Recovered Component</Text>;
    };

    const { getByTestId, queryByTestId, getByText } = render(
      <ErrorBoundary onReset={onReset}>
        <FallibleComponent />
      </ErrorBoundary>
    );

    expect(getByTestId('error-boundary-screen')).toBeTruthy();

    // Clear the error condition before retrying
    errorSignal.crash = false;
    fireEvent.press(getByTestId('error-boundary-retry-button'));

    expect(onReset).toHaveBeenCalled();
    expect(queryByTestId('error-boundary-screen')).toBeNull();
    expect(getByText('Recovered Component')).toBeTruthy();
  });
});
