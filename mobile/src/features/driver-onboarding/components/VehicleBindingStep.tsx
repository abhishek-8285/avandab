import React, { useState } from 'react';
import { Colors, FontSize} from '../../../constants/theme';
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
            placeholderTextColor={Colors.textSecondary}
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
    marginBottom: 20,
    lineHeight: 18,
  },
  label: {
    fontSize: FontSize.body,
    fontWeight: '600',
    color: Colors.textSecondary,
    marginBottom: 6,
  },
  input: {
    backgroundColor: Colors.modalBg,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    color: Colors.textSecondary,
    fontSize: FontSize.heading,
    fontWeight: '700',
    letterSpacing: 1.5,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  hint: {
    fontSize: FontSize.label,
    color: Colors.textSecondary,
    marginTop: 6,
    marginBottom: 20,
  },
  infoBanner: {
    backgroundColor: Colors.onboardingBg,
    borderColor: Colors.onboardingBorder,
    borderWidth: 1,
    borderRadius: 12,
    padding: 16,
    marginBottom: 20,
  },
  infoTitle: {
    fontSize: FontSize.title,
    fontWeight: '700',
    color: Colors.onboardingText,
    marginBottom: 4,
  },
  infoText: {
    fontSize: FontSize.body,
    color: Colors.modalSub,
    lineHeight: 18,
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
