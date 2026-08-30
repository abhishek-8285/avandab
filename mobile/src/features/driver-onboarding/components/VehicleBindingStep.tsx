import React, { useState } from 'react';
import { View, Text, TextInput, TouchableOpacity, StyleSheet, Alert } from 'react-native';
import { OwnershipType, VehicleClaimFormData } from '../types/onboarding';
import { validateVehiclePlate } from '../schemas/onboardingSchemas';

interface Props {
  ownershipType: OwnershipType;
  initialPlate?: string;
  onNext: (data: VehicleClaimFormData) => void;
  onBack: () => void;
}

export const VehicleBindingStep: React.FC<Props> = ({
  ownershipType,
  initialPlate = '',
  onNext,
  onBack,
}) => {
  const [plate, setPlate] = useState(initialPlate);

  const handleClaim = () => {
    if (ownershipType === 'owner_operator') {
      if (!validateVehiclePlate(plate)) {
        Alert.alert('Invalid Vehicle Number', 'Please enter a valid registration number (e.g. MH04AB1234 or DL1LN9999).');
        return;
      }
      onNext({
        registrationNumber: plate.trim().toUpperCase(),
        ownershipType: 'owner_operator',
      });
    } else {
      onNext({
        registrationNumber: 'COMPANY-ASSIGNED',
        ownershipType: 'company_driver',
      });
    }
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>
        {ownershipType === 'owner_operator' ? 'Claim Your Vehicle' : 'Company Fleet Assignment'}
      </Text>
      <Text style={styles.subtitle}>
        {ownershipType === 'owner_operator'
          ? 'Enter your commercial registration plate. We will verify ownership against your RC document.'
          : 'Your fleet dispatcher will assign an authorized commercial vehicle upon onboarding verification.'}
      </Text>

      {ownershipType === 'owner_operator' ? (
        <View>
          <Text style={styles.label}>Vehicle Registration Number (RC Plate)</Text>
          <TextInput
            style={styles.input}
            value={plate}
            onChangeText={(t) => setPlate(t.toUpperCase())}
            placeholder="e.g. DL1LN9999"
            placeholderTextColor="#64748b"
            autoCapitalize="characters"
          />
          <Text style={styles.hint}>
            Standard Indian format: 2-letter state code + RTO code + series + 4 digits.
          </Text>
        </View>
      ) : (
        <View style={styles.infoBanner}>
          <Text style={styles.infoTitle}>Dispatcher Assignment Mode Active</Text>
          <Text style={styles.infoText}>
            You will be able to accept vehicle handovers from the fleet dispatch console once your license and payout accounts are approved.
          </Text>
        </View>
      )}

      <View style={styles.btnRow}>
        <TouchableOpacity style={styles.btnSecondary} onPress={onBack}>
          <Text style={styles.btnSecondaryText}>← Back</Text>
        </TouchableOpacity>
        <TouchableOpacity style={styles.btnPrimary} onPress={handleClaim}>
          <Text style={styles.btnPrimaryText}>Continue to KYC →</Text>
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
    marginBottom: 20,
    lineHeight: 18,
  },
  label: {
    fontSize: 12,
    fontWeight: '600',
    color: '#cbd5e1',
    marginBottom: 6,
  },
  input: {
    backgroundColor: '#1e293b',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    color: '#f8fafc',
    fontSize: 16,
    fontWeight: '700',
    letterSpacing: 1.5,
    borderWidth: 1,
    borderColor: '#334155',
  },
  hint: {
    fontSize: 11,
    color: '#64748b',
    marginTop: 6,
    marginBottom: 20,
  },
  infoBanner: {
    backgroundColor: '#083344',
    borderColor: '#06b6d4',
    borderWidth: 1,
    borderRadius: 12,
    padding: 16,
    marginBottom: 20,
  },
  infoTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: '#38bdf8',
    marginBottom: 4,
  },
  infoText: {
    fontSize: 12,
    color: '#94a3b8',
    lineHeight: 18,
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
