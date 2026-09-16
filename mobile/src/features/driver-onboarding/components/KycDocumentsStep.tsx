import React, { useState } from 'react';
import { Colors, FontSize} from '../../../constants/theme';
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
        placeholderTextColor={Colors.textSecondary}
        autoCapitalize="characters"
      />

      <Text style={styles.label}>Issuing Authority / RTO</Text>
      <TextInput
        style={styles.input}
        value={authority}
        onChangeText={setAuthority}
        placeholder="e.g. Delhi RTO (DL04)"
        placeholderTextColor={Colors.textSecondary}
      />

      <Text style={styles.label}>License Expiry Date (YYYY-MM-DD)</Text>
      <TextInput
        style={styles.input}
        value={expiry}
        onChangeText={setExpiry}
        placeholder="YYYY-MM-DD"
        placeholderTextColor={Colors.textSecondary}
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
  label: {
    fontSize: FontSize.body,
    fontWeight: '600',
    color: Colors.textSecondary,
    marginBottom: 6,
    marginTop: 10,
  },
  input: {
    backgroundColor: Colors.modalBg,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    color: Colors.textSecondary,
    fontSize: FontSize.title,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
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
    backgroundColor: Colors.modalBg,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  classChipActive: {
    borderColor: Colors.onboardingBorder,
    backgroundColor: Colors.onboardingBg,
  },
  classText: {
    color: Colors.modalSub,
    fontSize: FontSize.bodyLarge,
    fontWeight: '600',
  },
  classTextActive: {
    color: Colors.onboardingText,
  },
  btnRow: {
    flexDirection: 'row',
    gap: 10,
    marginTop: 10,
  },
  btnSecondary: {
    flex: 1,
    backgroundColor: Colors.modalBg,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  btnSecondaryText: {
    color: Colors.modalSub,
    fontSize: FontSize.title,
    fontWeight: '600',
  },
  btnPrimary: {
    flex: 2,
    backgroundColor: Colors.onboardingBtn,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
  },
  btnPrimaryText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.title,
    fontWeight: '700',
  },
});
