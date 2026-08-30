import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { OnboardingStepName } from '../types/onboarding';

interface Props {
  currentStep: OnboardingStepName;
  completedSteps: string[];
}

const STEPS: { key: OnboardingStepName; label: string }[] = [
  { key: 'profile', label: 'Profile' },
  { key: 'ownership_choice', label: 'Type' },
  { key: 'vehicle_binding', label: 'Vehicle' },
  { key: 'kyc_documents', label: 'KYC / DL' },
  { key: 'bank_details', label: 'Bank' },
  { key: 'pending_approval', label: 'Review' },
];

export const OnboardingProgress: React.FC<Props> = ({ currentStep, completedSteps }) => {
  const currentIndex = STEPS.findIndex((s) => s.key === currentStep);

  return (
    <View style={styles.container}>
      <View style={styles.stepsRow}>
        {STEPS.map((step, index) => {
          const isDone = completedSteps.includes(step.key) || (currentIndex > index && currentStep !== 'pending_approval');
          const isCurrent = step.key === currentStep;

          return (
            <View key={step.key} style={styles.stepWrapper}>
              <View
                style={[
                  styles.circle,
                  isDone && styles.circleDone,
                  isCurrent && styles.circleCurrent,
                ]}
              >
                <Text
                  style={[
                    styles.circleText,
                    (isDone || isCurrent) && styles.circleTextActive,
                  ]}
                >
                  {isDone ? '✓' : index + 1}
                </Text>
              </View>
              <Text
                style={[
                  styles.stepLabel,
                  (isDone || isCurrent) && styles.stepLabelActive,
                ]}
                numberOfLines={1}
              >
                {step.label}
              </Text>
              {index < STEPS.length - 1 && (
                <View
                  style={[
                    styles.connector,
                    isDone && styles.connectorDone,
                  ]}
                />
              )}
            </View>
          );
        })}
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    paddingVertical: 12,
    paddingHorizontal: 16,
    backgroundColor: '#0f172a',
    borderBottomWidth: 1,
    borderBottomColor: '#1e293b',
  },
  stepsRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  stepWrapper: {
    alignItems: 'center',
    flex: 1,
    position: 'relative',
  },
  circle: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: '#1e293b',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: 4,
    borderWidth: 1.5,
    borderColor: '#334155',
  },
  circleDone: {
    backgroundColor: '#059669',
    borderColor: '#10b981',
  },
  circleCurrent: {
    borderColor: '#06b6d4',
    backgroundColor: '#083344',
  },
  circleText: {
    color: '#64748b',
    fontSize: 12,
    fontWeight: '700',
  },
  circleTextActive: {
    color: '#ffffff',
  },
  stepLabel: {
    fontSize: 10,
    color: '#64748b',
    fontWeight: '600',
  },
  stepLabelActive: {
    color: '#f8fafc',
  },
  connector: {
    position: 'absolute',
    top: 14,
    left: '65%',
    right: '-35%',
    height: 2,
    backgroundColor: '#1e293b',
    zIndex: -1,
  },
  connectorDone: {
    backgroundColor: '#059669',
  },
});
