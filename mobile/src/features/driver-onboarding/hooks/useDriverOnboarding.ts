import { useState, useEffect, useCallback } from 'react';
import { onboardingApi } from '../api/onboardingApi';
import {
  OnboardingState,
  OnboardingStepName,
  LicenseFormData,
  VehicleClaimFormData,
  BankAccountFormData,
} from '../types/onboarding';

export function useDriverOnboarding(token?: string) {
  const [state, setState] = useState<OnboardingState | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [submitting, setSubmitting] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchState = useCallback(async () => {
    if (!token) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const remoteState = await onboardingApi.fetchOnboardingState(token);
      setState(remoteState);
    } catch (err: any) {
      setError(err.message || 'Could not connect to onboarding service');
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    fetchState();
  }, [fetchState]);

  const submitLicense = async (data: LicenseFormData) => {
    if (!token) return;
    setSubmitting(true);
    setError(null);
    try {
      await onboardingApi.submitLicense(token, data);
      await fetchState();
    } catch (err: any) {
      setError(err.message);
      throw err;
    } finally {
      setSubmitting(false);
    }
  };

  const submitVehicleClaim = async (data: VehicleClaimFormData) => {
    if (!token) return;
    setSubmitting(true);
    setError(null);
    try {
      await onboardingApi.submitVehicleClaim(token, data);
      await fetchState();
    } catch (err: any) {
      setError(err.message);
      throw err;
    } finally {
      setSubmitting(false);
    }
  };

  const submitPayoutAccount = async (data: BankAccountFormData) => {
    if (!token) return;
    setSubmitting(true);
    setError(null);
    try {
      await onboardingApi.submitPayoutAccount(token, data);
      await fetchState();
    } catch (err: any) {
      setError(err.message);
      throw err;
    } finally {
      setSubmitting(false);
    }
  };

  const submitForVerification = async () => {
    if (!token) return;
    setSubmitting(true);
    setError(null);
    try {
      await onboardingApi.submitForVerification(token);
      await fetchState();
    } catch (err: any) {
      setError(err.message);
      throw err;
    } finally {
      setSubmitting(false);
    }
  };

  return {
    state,
    loading,
    submitting,
    error,
    refresh: fetchState,
    submitLicense,
    submitVehicleClaim,
    submitPayoutAccount,
    submitForVerification,
  };
}
