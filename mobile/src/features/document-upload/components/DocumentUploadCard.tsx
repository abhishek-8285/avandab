import React from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TouchableOpacity, StyleSheet, ActivityIndicator } from 'react-native';
import { DocumentCategory, DocumentUploadTask } from '../types/document';

interface Props {
  category: DocumentCategory;
  title: string;
  description: string;
  task?: DocumentUploadTask;
  isBusy: boolean;
  onCapture: (source: 'camera' | 'gallery') => void;
}

export const DocumentUploadCard: React.FC<Props> = ({
  title,
  description,
  task,
  isBusy,
  onCapture,
}) => {
  const isComplete = task?.state === 'COMPLETE';
  const isUploading = task?.state === 'UPLOADING' || task?.state === 'QUEUED';
  const isError = task?.state === 'REJECTED' || task?.state === 'RETRY_WAIT';

  return (
    <View style={styles.card}>
      <View style={styles.headerRow}>
        <View style={styles.titleCol}>
          <Text style={styles.title}>{title}</Text>
          <Text style={styles.desc}>{description}</Text>
        </View>
        <View
          style={[
            styles.badge,
            isComplete && styles.badgeSuccess,
            isUploading && styles.badgeProgress,
            isError && styles.badgeError,
          ]}
        >
          <Text
            style={[
              styles.badgeText,
              isComplete && styles.badgeTextSuccess,
              isUploading && styles.badgeTextProgress,
              isError && styles.badgeTextError,
            ]}
          >
            {isComplete ? 'UPLOADED ✓' : isUploading ? 'UPLOADING...' : isError ? 'RETRYING' : 'PENDING'}
          </Text>
        </View>
      </View>

      {/* Progress Indicator */}
      {isUploading && (
        <View style={styles.progressContainer}>
          <View style={[styles.progressBar, { width: `${task?.progressPct || 20}%` }]} />
        </View>
      )}

      {/* Error text if any */}
      {isError && task?.errorMessage && (
        <Text style={styles.errorText}>⚠️ {task.errorMessage}</Text>
      )}

      {/* Action Buttons */}
      <View style={styles.btnRow}>
        <TouchableOpacity
          style={[styles.actionBtn, isBusy && styles.btnDisabled]}
          onPress={() => onCapture('camera')}
          disabled={isBusy}
        >
          {isBusy ? (
            <ActivityIndicator size="small" color={Colors.textSecondary} />
          ) : (
            <Text style={styles.actionBtnText}>📷 Camera Scan</Text>
          )}
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.actionBtnSecondary, isBusy && styles.btnDisabled]}
          onPress={() => onCapture('gallery')}
          disabled={isBusy}
        >
          <Text style={styles.actionBtnSecondaryText}>📁 Upload File</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  card: {
    backgroundColor: Colors.mapDark,
    borderRadius: 14,
    padding: 16,
    borderWidth: 1,
    borderColor: Colors.modalBg,
    marginBottom: 12,
  },
  headerRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    marginBottom: 12,
  },
  titleCol: {
    flex: 1,
    marginRight: 10,
  },
  title: {
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 2,
  },
  desc: {
    fontSize: FontSize.body,
    color: Colors.modalSub,
    lineHeight: 16,
  },
  badge: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    backgroundColor: Colors.modalBg,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  badgeSuccess: {
    backgroundColor: Colors.onboardingOkBg,
    borderColor: Colors.onboardingOkBorder,
  },
  badgeProgress: {
    backgroundColor: Colors.onboardingBg,
    borderColor: Colors.onboardingBorder,
  },
  badgeError: {
    backgroundColor: Colors.onboardingErrBg,
    borderColor: Colors.danger,
  },
  badgeText: {
    fontSize: FontSize.small,
    fontWeight: '700',
    color: Colors.modalSub,
  },
  badgeTextSuccess: {
    color: Colors.accent,
  },
  badgeTextProgress: {
    color: Colors.onboardingText,
  },
  badgeTextError: {
    color: Colors.danger,
  },
  progressContainer: {
    height: 4,
    backgroundColor: Colors.modalBg,
    borderRadius: 2,
    overflow: 'hidden',
    marginBottom: 10,
  },
  progressBar: {
    height: '100%',
    backgroundColor: Colors.onboardingBorder,
  },
  errorText: {
    fontSize: FontSize.label,
    color: Colors.danger,
    marginBottom: 10,
  },
  btnRow: {
    flexDirection: 'row',
    gap: 8,
  },
  actionBtn: {
    flex: 1,
    backgroundColor: Colors.onboardingBtn,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
  },
  actionBtnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.body,
    fontWeight: '700',
  },
  actionBtnSecondary: {
    flex: 1,
    backgroundColor: Colors.modalBg,
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  actionBtnSecondaryText: {
    color: Colors.textSecondary,
    fontSize: FontSize.body,
    fontWeight: '600',
  },
  btnDisabled: {
    opacity: 0.6,
  },
});
