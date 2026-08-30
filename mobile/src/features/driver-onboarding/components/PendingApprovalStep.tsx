import React from 'react';
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
            <ActivityIndicator color="#ffffff" />
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
            <ActivityIndicator color="#ffffff" />
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
    backgroundColor: '#0f172a',
    borderRadius: 16,
    padding: 20,
    borderWidth: 1,
    borderColor: '#1e293b',
  },
  title: {
    fontSize: 20,
    fontWeight: '700',
    color: '#f8fafc',
    marginBottom: 4,
  },
  subtitle: {
    fontSize: 13,
    color: '#94a3b8',
    marginBottom: 16,
    lineHeight: 18,
  },
  statusBanner: {
    flexDirection: 'row',
    borderRadius: 12,
    padding: 16,
    backgroundColor: '#1e293b',
    borderWidth: 1,
    borderColor: '#334155',
    marginBottom: 20,
  },
  bannerApproved: {
    backgroundColor: '#064e3b',
    borderColor: '#10b981',
  },
  bannerSubmitted: {
    backgroundColor: '#083344',
    borderColor: '#06b6d4',
  },
  bannerRejected: {
    backgroundColor: '#450a0a',
    borderColor: '#ef4444',
  },
  statusIcon: {
    fontSize: 24,
    marginRight: 12,
    marginTop: 2,
  },
  statusCol: {
    flex: 1,
  },
  statusHeadline: {
    fontSize: 15,
    fontWeight: '700',
    color: '#f8fafc',
    marginBottom: 4,
  },
  statusSubtext: {
    fontSize: 12,
    color: '#cbd5e1',
    lineHeight: 17,
  },
  sectionHeader: {
    fontSize: 14,
    fontWeight: '700',
    color: '#94a3b8',
    marginBottom: 10,
    textTransform: 'uppercase',
    letterSpacing: 0.8,
  },
  checklist: {
    backgroundColor: '#1e293b',
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
    fontSize: 12,
    marginRight: 8,
  },
  checkText: {
    flex: 1,
    fontSize: 13,
    color: '#f8fafc',
    fontWeight: '500',
  },
  editBtn: {
    color: '#38bdf8',
    fontSize: 12,
    fontWeight: '700',
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
  btnLaunch: {
    backgroundColor: '#059669',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnLaunchText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },
  btnRefresh: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#334155',
  },
  btnRefreshText: {
    color: '#f8fafc',
    fontSize: 14,
    fontWeight: '600',
  },
  btnSubmit: {
    backgroundColor: '#0d9488',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnSubmitText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },
});
