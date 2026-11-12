import React, { Component, ReactNode, ErrorInfo } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, ScrollView } from 'react-native';
import { CrashReporter } from '../services/crashReporter';
import { Colors, Radius, Spacing, Font } from '../constants/theme';

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
  onReset?: () => void;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
    };
  }

  static getDerivedStateFromError(error: Error): State {
    return {
      hasError: true,
      error,
    };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    CrashReporter.reportError(error, {
      componentStack: errorInfo.componentStack ?? undefined,
      source: 'ErrorBoundary',
    });
  }

  handleRestart = (): void => {
    if (this.props.onReset) {
      this.props.onReset();
    }
    this.setState({
      hasError: false,
      error: null,
    });
  };

  render(): ReactNode {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <View style={styles.container} testID="error-boundary-screen">
          <View style={styles.card}>
            <Text style={styles.icon}>⚠️</Text>
            <Text style={styles.title}>Something went wrong</Text>
            <Text style={styles.subtitle}>कुछ गलत हो गया। कृपया पुनः प्रयास करें।</Text>

            <ScrollView style={styles.errorDetails} contentContainerStyle={styles.errorContent}>
              <Text style={styles.errorMessage}>
                {this.state.error?.message || 'An unexpected error occurred.'}
              </Text>
            </ScrollView>

            <TouchableOpacity
              style={styles.retryButton}
              onPress={this.handleRestart}
              activeOpacity={0.8}
              testID="error-boundary-retry-button"
            >
              <Text style={styles.retryButtonText}>Restart App / पुनः प्रयास करें</Text>
            </TouchableOpacity>
          </View>
        </View>
      );
    }

    return this.props.children;
  }
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#075e54',
    justifyContent: 'center',
    alignItems: 'center',
    padding: Spacing?.lg || 24,
  },
  card: {
    backgroundColor: '#ffffff',
    borderRadius: Radius?.lg || 16,
    padding: Spacing?.xl || 24,
    width: '100%',
    maxWidth: 400,
    alignItems: 'center',
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.15,
    shadowRadius: 12,
    elevation: 6,
  },
  icon: {
    fontSize: 48,
    marginBottom: Spacing?.md || 16,
  },
  title: {
    fontSize: 20,
    fontWeight: 'bold',
    color: Colors?.textPrimary || '#111827',
    marginBottom: 4,
    textAlign: 'center',
  },
  subtitle: {
    fontSize: 14,
    color: Colors?.textSecondary || '#6b7280',
    marginBottom: Spacing?.lg || 16,
    textAlign: 'center',
  },
  errorDetails: {
    maxHeight: 120,
    width: '100%',
    backgroundColor: '#f3f4f6',
    borderRadius: Radius?.sm || 8,
    marginBottom: Spacing?.lg || 16,
  },
  errorContent: {
    padding: Spacing?.sm || 12,
  },
  errorMessage: {
    fontSize: 12,
    color: '#b91c1c',
    fontFamily: Font?.mono || 'monospace',
  },
  retryButton: {
    backgroundColor: Colors?.primary || '#008069',
    borderRadius: Radius?.md || 8,
    paddingVertical: 14,
    paddingHorizontal: 24,
    width: '100%',
    alignItems: 'center',
  },
  retryButtonText: {
    color: '#ffffff',
    fontSize: 16,
    fontWeight: '600',
  },
});
