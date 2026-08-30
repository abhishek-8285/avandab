import { getApiBaseURL } from '../../../constants/network';
import {
  OnboardingState,
  LicenseFormData,
  VehicleClaimFormData,
  BankAccountFormData,
} from '../types/onboarding';

export class OnboardingApiClient {
  private getHeaders(token: string) {
    return {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    };
  }

  async fetchOnboardingState(token: string): Promise<OnboardingState> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/onboarding`, {
      headers: this.getHeaders(token),
    });
    if (!res.ok) {
      throw new Error(`Failed to fetch onboarding state: ${res.statusText}`);
    }
    return res.json();
  }

  async submitLicense(token: string, data: LicenseFormData): Promise<void> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/license`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        license_number: data.licenseNumber,
        issuing_authority: data.issuingAuthority,
        issued_on: data.issuedOn,
        expires_on: data.expiresOn,
        classes: data.classes,
      }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Submission failed' }));
      throw new Error(err.error || 'Failed to submit license');
    }
  }

  async submitVehicleClaim(token: string, data: VehicleClaimFormData): Promise<string> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/vehicle-claims`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        registration_number: data.registrationNumber,
        rc_document_id: data.rcDocumentId,
      }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Claim failed' }));
      throw new Error(err.error || 'Failed to claim vehicle');
    }
    const json = await res.json();
    return json.claim_id;
  }

  async submitPayoutAccount(token: string, data: BankAccountFormData): Promise<string> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/payout-account`, {
      method: 'POST',
      headers: this.getHeaders(token),
      body: JSON.stringify({
        account_holder_name: data.accountHolderName,
        account_number: data.accountNumber,
        ifsc_code: data.ifscCode,
        bank_name: data.bankName,
      }),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Payout submission failed' }));
      throw new Error(err.error || 'Failed to link bank account');
    }
    const json = await res.json();
    return json.account_id;
  }

  async submitForVerification(token: string): Promise<void> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/verification/submit`, {
      method: 'POST',
      headers: this.getHeaders(token),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Final submission failed' }));
      throw new Error(err.error || 'Failed to submit for verification');
    }
  }
}

export const onboardingApi = new OnboardingApiClient();
