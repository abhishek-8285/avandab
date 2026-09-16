import React from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TouchableOpacity, StyleSheet, ActivityIndicator } from 'react-native';
import { OnboardingState } from '../types/onboarding';

interface Props {
  state: OnboardingState | null;
  submitting: boolean;
  onRefresh: () => void;
  onSubmitForVerification: () => void;
  onEditStep: (step: string) => void;
  onEnterFleet: () => void;
}

export const PendingApprovalStep: React.FC<Props> = ({
  state,
  submitting,
  onRefresh,
  onSubmitForVerification,
  onEditStep,
  onEnterFleet,
}) => {
  const overall = state?.overall_status || 'in_progress';
  const isApproved = overall === 'approved' || state?.is_eligible;
  const isSubmitted = overall === 'submitted';
  const isRejected = overall === 'rejected';

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Onboarding & Verification Status</Text>
      <Text style={styles.subtitle}>
        Avandab compliance review status for commercial dispatch.
      </Text>

      {/* Status Card Banner */}
      <View
        style={[
          styles.statusBanner,
          isApproved && styles.bannerApproved,
          isSubmitted && styles.bannerSubmitted,
          isRejected && styles.bannerRejected,
        ]}
      >
        <Text style={styles.statusIcon}>
          {isApproved ? '✅' : isSubmitted ? '⏳' : isRejected ? '❌' : '📋'}
        </Text>
        <View style={styles.statusCol}>
          <Text style={styles.statusHeadline}>
            {isApproved
              ? 'Verified & Approved'
              : isSubmitted
              ? 'Verification in Progress'
              : isRejected
              ? 'Action Required / Rejected'
              : 'Ready for Final Submission'}
          </Text>
          <Text style={styles.statusSubtext}>
            {isApproved
              ? 'Your KYC, vehicle binding, and payout account are verified. You are eligible for dispatch.'
              : isSubmitted
              ? 'Our compliance desk is verifying your Driving License & vehicle RC with Parivahan records.'
              : isRejected
              ? state?.rejection_reason || 'Please correct the highlighted requirements below.'
              : 'Review your submitted items and click submit for operational verification.'}
          </Text>
        </View>
      </View>

      {/* Checklist / Requirements */}
      <Text style={styles.sectionHeader}>Verification Checklist</Text>
      <View style={styles.checklist}>
        <View style={styles.checkItem}>
          <Text style={styles.checkIcon}>{state?.license_status === 'verified' ? '🟢' : state?.license_status === 'pending' ? '🟡' : '⚪'}</Text>
          <Text style={styles.checkText}>Driving License: {state?.license_status || 'Pending'}</Text>
          {state?.license_status !== 'verified' && (
            <TouchableOpacity onPress={() => onEditStep('kyc_documents')}>
              <Text style={styles.editBtn}>Edit</Text>
            </TouchableOpacity>
          )}
        </View>

        <View style={styles.checkItem}>
          <Text style={styles.checkIcon}>{state?.vehicle_status === 'approved' ? '🟢' : state?.vehicle_status === 'pending_claim_review' ? '🟡' : '⚪'}</Text>
          <Text style={styles.checkText}>Vehicle Registration / RC: {state?.vehicle_status || 'Pending'}</Text>
          {state?.vehicle_status !== 'approved' && (
            <TouchableOpacity onPress={() => onEditStep('vehicle_binding')}>
              <Text style={styles.editBtn}>Edit</Text>
            </TouchableOpacity>
          )}
        </View>

        <View style={styles.checkItem}>
          <Text style={styles.checkIcon}>{state?.bank_status === 'verified' ? '🟢' : state?.bank_status === 'pending' ? '🟡' : '⚪'}</Text>
          <Text style={styles.checkText}>Direct Payout Account: {state?.bank_status || 'Pending'}</Text>
          {state?.bank_status !== 'verified' && (
            <TouchableOpacity onPress={() => onEditStep('bank_details')}>
              <Text style={styles.editBtn}>Edit</Text>
            </TouchableOpacity>
          )}
        </View>
      </View>

      {/* Action Buttons */}
      {isApproved ? (
        <TouchableOpacity style={styles.btnLaunch} onPress={onEnterFleet}>
          <Text style={styles.btnLaunchText}>Launch Fleet Dispatch Console 🚀</Text>
        </TouchableOpacity>
      ) : isSubmitted ? (
        <TouchableOpacity style={styles.btnRefresh} onPress={onRefresh} disabled={submitting}>
          {submitting ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <Text style={styles.btnRefreshText}>Check Verification Status 🔄</Text>
          )}
        </TouchableOpacity>
      ) : (
        <TouchableOpacity
          style={styles.btnSubmit}
          onPress={onSubmitForVerification}
          disabled={submitting}
        >
          {submitting ? (
            <ActivityIndicator color={Colors.textOnPrimary} />
          ) : (
            <Text style={styles.btnSubmitText}>Submit for Verification →</Text>
          )}
        </TouchableOpacity>
      )}
    </View>
  );
};

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.mapDark,
    borderRadius: 16,
    padding: 20,
    borderWidth: 1,
    borderColor: Colors.modalBg,
  },
  title: {
    fontSize: FontSize.banner,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 4,
  },
  subtitle: {
    fontSize: FontSize.bodyLarge,
    color: Colors.modalSub,
    marginBottom: 16,
    lineHeight: 18,
  },
  statusBanner: {
    flexDirection: 'row',
    borderRadius: 12,
    padding: 16,
    backgroundColor: Colors.modalBg,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
    marginBottom: 20,
  },
  bannerApproved: {
    backgroundColor: Colors.onboardingOkBg,
    borderColor: Colors.onboardingOkBorder,
  },
  bannerSubmitted: {
    backgroundColor: Colors.onboardingBg,
    borderColor: Colors.onboardingBorder,
  },
  bannerRejected: {
    backgroundColor: Colors.onboardingErrBg,
    borderColor: Colors.danger,
  },
  statusIcon: {
    fontSize: FontSize.display,
    marginRight: 12,
    marginTop: 2,
  },
  statusCol: {
    flex: 1,
  },
  statusHeadline: {
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 4,
  },
  statusSubtext: {
    fontSize: FontSize.body,
    color: Colors.textSecondary,
    lineHeight: 17,
  },
  sectionHeader: {
    fontSize: FontSize.title,
    fontWeight: '700',
    color: Colors.modalSub,
    marginBottom: 10,
    textTransform: 'uppercase',
    letterSpacing: 0.8,
  },
  checklist: {
    backgroundColor: Colors.modalBg,
    borderRadius: 12,
    padding: 14,
    gap: 12,
    marginBottom: 20,
  },
  checkItem: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  checkIcon: {
    fontSize: FontSize.body,
    marginRight: 8,
  },
  checkText: {
    flex: 1,
    fontSize: FontSize.bodyLarge,
    color: Colors.textSecondary,
    fontWeight: '500',
  },
  editBtn: {
    color: Colors.onboardingText,
    fontSize: FontSize.body,
    fontWeight: '700',
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
  btnLaunch: {
    backgroundColor: Colors.successDark,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnLaunchText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
  },
  btnRefresh: {
    backgroundColor: Colors.modalBg,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  btnRefreshText: {
    color: Colors.textSecondary,
    fontSize: FontSize.title,
    fontWeight: '600',
  },
  btnSubmit: {
    backgroundColor: Colors.onboardingBtn,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnSubmitText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
  },
});
