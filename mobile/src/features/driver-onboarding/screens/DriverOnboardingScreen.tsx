import React, { useState, useEffect } from 'react';
import {
  View,
  Text,
  ScrollView,
  StyleSheet,
  ActivityIndicator,
  SafeAreaView,
  StatusBar,
  Alert,
} from 'react-native';
import { useDriverOnboarding } from '../hooks/useDriverOnboarding';
import { OnboardingProgress } from '../components/OnboardingProgress';
import { ProfileStep } from '../components/ProfileStep';
import { OwnershipStep } from '../components/OwnershipStep';
import { VehicleBindingStep } from '../components/VehicleBindingStep';
import { KycDocumentsStep } from '../components/KycDocumentsStep';
import { BankDetailsStep } from '../components/BankDetailsStep';
import { PendingApprovalStep } from '../components/PendingApprovalStep';
import { OnboardingStepName, OwnershipType } from '../types/onboarding';

interface Props {
  token?: string;
  user?: { name?: string; phone?: string; email?: string };
  onComplete: () => void;
}

export const DriverOnboardingScreen: React.FC<Props> = ({ token, user, onComplete }) => {
  const {
    state,
    loading,
    submitting,
    error,
    refresh,
    submitLicense,
    submitVehicleClaim,
    submitPayoutAccount,
    submitForVerification,
  } = useDriverOnboarding(token);

  const [activeStep, setActiveStep] = useState<OnboardingStepName>('profile');
  const [ownershipType, setOwnershipType] = useState<OwnershipType>('owner_operator');

  // Hydrate activeStep from remote backend state
  useEffect(() => {
    if (state?.current_step) {
      if (state.overall_status === 'submitted' || state.overall_status === 'approved') {
        setActiveStep('pending_approval');
      } else {
        setActiveStep(state.current_step);
      }
    }
  }, [state?.current_step, state?.overall_status]);

  if (loading) {
    return (
      <SafeAreaView style={styles.loadingContainer}>
        <ActivityIndicator size="large" color="#0d9488" />
        <Text style={styles.loadingText}>Syncing onboarding state with Avandab...</Text>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.safeArea}>
      <StatusBar barStyle="light-content" backgroundColor="#0a0f1d" />
      
      <View style={styles.header}>
        <Text style={styles.headerTitle}>Driver Onboarding</Text>
        <Text style={styles.headerBadge}>AVANDAB FLEET</Text>
      </View>

      <OnboardingProgress
        currentStep={activeStep}
        completedSteps={state?.completed_steps || []}
      />

      <ScrollView contentContainerStyle={styles.scrollContent}>
        {error && (
          <View style={styles.errorCard}>
            <Text style={styles.errorTitle}>Connection Note</Text>
            <Text style={styles.errorText}>{error}</Text>
          </View>
        )}

        {activeStep === 'profile' && (
          <ProfileStep
            initialData={{
              name: user?.name || '',
              phone: user?.phone || '',
              email: user?.email || '',
            }}
            onNext={() => setActiveStep('ownership_choice')}
          />
        )}

        {activeStep === 'ownership_choice' && (
          <OwnershipStep
            onSelect={(type) => {
              setOwnershipType(type);
              setActiveStep('vehicle_binding');
            }}
          />
        )}

        {activeStep === 'vehicle_binding' && (
          <VehicleBindingStep
            ownershipType={ownershipType}
            onNext={async (data) => {
              try {
                if (data.ownershipType === 'owner_operator') {
                  await submitVehicleClaim(data);
                }
                setActiveStep('kyc_documents');
              } catch (err: any) {
                Alert.alert('Submission Error', err.message);
              }
            }}
            onBack={() => setActiveStep('ownership_choice')}
          />
        )}

        {activeStep === 'kyc_documents' && (
          <KycDocumentsStep
            onNext={async (data) => {
              try {
                await submitLicense(data);
                setActiveStep('bank_details');
              } catch (err: any) {
                Alert.alert('License Error', err.message);
              }
            }}
            onBack={() => setActiveStep('vehicle_binding')}
          />
        )}

        {activeStep === 'bank_details' && (
          <BankDetailsStep
            onNext={async (data) => {
              try {
                await submitPayoutAccount(data);
                setActiveStep('pending_approval');
              } catch (err: any) {
                Alert.alert('Payout Error', err.message);
              }
            }}
            onBack={() => setActiveStep('kyc_documents')}
          />
        )}

        {activeStep === 'pending_approval' && (
          <PendingApprovalStep
            state={state}
            submitting={submitting}
            onRefresh={refresh}
            onSubmitForVerification={async () => {
              try {
                await submitForVerification();
                Alert.alert('Application Submitted', 'Your documents have been submitted for operational verification.');
              } catch (err: any) {
                Alert.alert('Error', err.message);
              }
            }}
            onEditStep={(step) => setActiveStep(step as OnboardingStepName)}
            onEnterFleet={onComplete}
          />
        )}
      </ScrollView>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: '#0a0f1d',
  },
  loadingContainer: {
    flex: 1,
    backgroundColor: '#0a0f1d',
    alignItems: 'center',
    justifyContent: 'center',
  },
  loadingText: {
    color: '#94a3b8',
    marginTop: 12,
    fontSize: 14,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 20,
    paddingVertical: 14,
    backgroundColor: '#0f172a',
    borderBottomWidth: 1,
    borderBottomColor: '#1e293b',
  },
  headerTitle: {
    color: '#f8fafc',
    fontSize: 18,
    fontWeight: '800',
    letterSpacing: 0.5,
  },
  headerBadge: {
    color: '#06b6d4',
    fontSize: 10,
    fontWeight: '800',
    letterSpacing: 1,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    backgroundColor: '#083344',
  },
  scrollContent: {
    padding: 16,
    paddingBottom: 40,
  },
  errorCard: {
    backgroundColor: '#450a0a',
    borderColor: '#ef4444',
    borderWidth: 1,
    borderRadius: 12,
    padding: 12,
    marginBottom: 16,
  },
  errorTitle: {
    color: '#f87171',
    fontWeight: '700',
    fontSize: 13,
    marginBottom: 2,
  },
  errorText: {
    color: '#fca5a5',
    fontSize: 12,
  },
});
