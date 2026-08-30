export type OnboardingStepName =
  | 'profile'
  | 'ownership_choice'
  | 'vehicle_binding'
  | 'kyc_documents'
  | 'bank_details'
  | 'pending_approval'
  | 'completed';

export type OwnershipType = 'owner_operator' | 'company_driver';

export interface OnboardingRequirement {
  code: string;
  status: 'pending' | 'verified' | 'rejected';
  message: string;
}

export interface OnboardingState {
  driver_id: string;
  current_step: OnboardingStepName;
  identity_status: string;
  license_status: string;
  vehicle_status: string;
  bank_status: string;
  overall_status: 'in_progress' | 'submitted' | 'approved' | 'rejected';
  completed_steps: string[];
  requirements: OnboardingRequirement[];
  can_submit: boolean;
  rejection_reason?: string;
  is_eligible: boolean;
}

export interface ProfileFormData {
  name: string;
  phone: string;
  email: string;
  preferredLanguage: string;
}

export interface LicenseFormData {
  licenseNumber: string;
  issuingAuthority: string;
  issuedOn: string;
  expiresOn: string;
  classes: string[];
}

export interface VehicleClaimFormData {
  registrationNumber: string;
  ownershipType: OwnershipType;
  rcDocumentId?: string;
}

export interface BankAccountFormData {
  accountHolderName: string;
  accountNumber: string;
  confirmAccountNumber: string;
  ifscCode: string;
  bankName: string;
}
