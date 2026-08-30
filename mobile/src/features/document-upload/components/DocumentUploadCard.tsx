import React from 'react';
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
            <ActivityIndicator size="small" color="#f8fafc" />
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
    backgroundColor: '#0f172a',
    borderRadius: 14,
    padding: 16,
    borderWidth: 1,
    borderColor: '#1e293b',
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
    fontSize: 15,
    fontWeight: '700',
    color: '#f8fafc',
    marginBottom: 2,
  },
  desc: {
    fontSize: 12,
    color: '#94a3b8',
    lineHeight: 16,
  },
  badge: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 6,
    backgroundColor: '#1e293b',
    borderWidth: 1,
    borderColor: '#334155',
  },
  badgeSuccess: {
    backgroundColor: '#064e3b',
    borderColor: '#10b981',
  },
  badgeProgress: {
    backgroundColor: '#083344',
    borderColor: '#06b6d4',
  },
  badgeError: {
    backgroundColor: '#450a0a',
    borderColor: '#ef4444',
  },
  badgeText: {
    fontSize: 10,
    fontWeight: '700',
    color: '#94a3b8',
  },
  badgeTextSuccess: {
    color: '#34d399',
  },
  badgeTextProgress: {
    color: '#38bdf8',
  },
  badgeTextError: {
    color: '#f87171',
  },
  progressContainer: {
    height: 4,
    backgroundColor: '#1e293b',
    borderRadius: 2,
    overflow: 'hidden',
    marginBottom: 10,
  },
  progressBar: {
    height: '100%',
    backgroundColor: '#06b6d4',
  },
  errorText: {
    fontSize: 11,
    color: '#f87171',
    marginBottom: 10,
  },
  btnRow: {
    flexDirection: 'row',
    gap: 8,
  },
  actionBtn: {
    flex: 1,
    backgroundColor: '#0d9488',
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
  },
  actionBtnText: {
    color: '#ffffff',
    fontSize: 12,
    fontWeight: '700',
  },
  actionBtnSecondary: {
    flex: 1,
    backgroundColor: '#1e293b',
    paddingVertical: 10,
    borderRadius: 8,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#334155',
  },
  actionBtnSecondaryText: {
    color: '#cbd5e1',
    fontSize: 12,
    fontWeight: '600',
  },
  btnDisabled: {
    opacity: 0.6,
  },
});
