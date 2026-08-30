import React, { useState } from 'react';
import { View, Text, TextInput, TouchableOpacity, StyleSheet, Alert } from 'react-native';
import { LicenseFormData } from '../types/onboarding';
import { validateLicenseNumber } from '../schemas/onboardingSchemas';

interface Props {
  initialLicense?: Partial<LicenseFormData>;
  onNext: (data: LicenseFormData) => void;
  onBack: () => void;
}

const AVAILABLE_CLASSES = ['LMV', 'HMV', 'TRANS', 'MCWG'];

export const KycDocumentsStep: React.FC<Props> = ({
  initialLicense,
  onNext,
  onBack,
}) => {
  const [dlNumber, setDlNumber] = useState(initialLicense?.licenseNumber || '');
  const [authority, setAuthority] = useState(initialLicense?.issuingAuthority || 'Delhi RTO');
  const [expiry, setExpiry] = useState(initialLicense?.expiresOn || '2032-12-31');
  const [selectedClasses, setSelectedClasses] = useState<string[]>(
    initialLicense?.classes && initialLicense.classes.length > 0 ? initialLicense.classes : ['LMV', 'TRANS']
  );

  const toggleClass = (c: string) => {
    if (selectedClasses.includes(c)) {
      if (selectedClasses.length === 1) return;
      setSelectedClasses(selectedClasses.filter((x) => x !== c));
    } else {
      setSelectedClasses([...selectedClasses, c]);
    }
  };

  const handleSave = () => {
    if (!validateLicenseNumber(dlNumber)) {
      Alert.alert('Invalid Driving License', 'Please enter a valid DL number (e.g. DL-0420110012345).');
      return;
    }

    onNext({
      licenseNumber: dlNumber.trim().toUpperCase(),
      issuingAuthority: authority,
      issuedOn: '2020-01-01',
      expiresOn: expiry,
      classes: selectedClasses,
    });
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Driving License (KYC)</Text>
      <Text style={styles.subtitle}>
        Avandab validates your commercial driving endorsement with Sarathi / Parivahan records.
      </Text>

      <Text style={styles.label}>Driving License Number</Text>
      <TextInput
        style={styles.input}
        value={dlNumber}
        onChangeText={(t) => setDlNumber(t.toUpperCase())}
        placeholder="e.g. DL-1420110012345"
        placeholderTextColor="#64748b"
        autoCapitalize="characters"
      />

      <Text style={styles.label}>Issuing Authority / RTO</Text>
      <TextInput
        style={styles.input}
        value={authority}
        onChangeText={setAuthority}
        placeholder="e.g. Delhi RTO (DL04)"
        placeholderTextColor="#64748b"
      />

      <Text style={styles.label}>License Expiry Date (YYYY-MM-DD)</Text>
      <TextInput
        style={styles.input}
        value={expiry}
        onChangeText={setExpiry}
        placeholder="YYYY-MM-DD"
        placeholderTextColor="#64748b"
      />

      <Text style={styles.label}>Endorsed Vehicle Classes</Text>
      <View style={styles.classRow}>
        {AVAILABLE_CLASSES.map((cls) => {
          const isSelected = selectedClasses.includes(cls);
          return (
            <TouchableOpacity
              key={cls}
              style={[styles.classChip, isSelected && styles.classChipActive]}
              onPress={() => toggleClass(cls)}
            >
              <Text style={[styles.classText, isSelected && styles.classTextActive]}>
                {cls} {isSelected ? '✓' : ''}
              </Text>
            </TouchableOpacity>
          );
        })}
      </View>

      <View style={styles.btnRow}>
        <TouchableOpacity style={styles.btnSecondary} onPress={onBack}>
          <Text style={styles.btnSecondaryText}>← Back</Text>
        </TouchableOpacity>
        <TouchableOpacity style={styles.btnPrimary} onPress={handleSave}>
          <Text style={styles.btnPrimaryText}>Continue to Banking →</Text>
        </TouchableOpacity>
      </View>
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
  label: {
    fontSize: 12,
    fontWeight: '600',
    color: '#cbd5e1',
    marginBottom: 6,
    marginTop: 10,
  },
  input: {
    backgroundColor: '#1e293b',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    color: '#f8fafc',
    fontSize: 14,
    borderWidth: 1,
    borderColor: '#334155',
  },
  classRow: {
    flexDirection: 'row',
    gap: 8,
    marginTop: 6,
    marginBottom: 20,
  },
  classChip: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: '#1e293b',
    borderWidth: 1,
    borderColor: '#334155',
  },
  classChipActive: {
    borderColor: '#06b6d4',
    backgroundColor: '#083344',
  },
  classText: {
    color: '#94a3b8',
    fontSize: 13,
    fontWeight: '600',
  },
  classTextActive: {
    color: '#38bdf8',
  },
  btnRow: {
    flexDirection: 'row',
    gap: 10,
    marginTop: 10,
  },
  btnSecondary: {
    flex: 1,
    backgroundColor: '#1e293b',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#334155',
  },
  btnSecondaryText: {
    color: '#94a3b8',
    fontSize: 14,
    fontWeight: '600',
  },
  btnPrimary: {
    flex: 2,
    backgroundColor: '#0d9488',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnPrimaryText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '700',
  },
});
