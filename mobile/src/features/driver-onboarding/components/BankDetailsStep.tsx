import React, { useState } from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TextInput, TouchableOpacity, StyleSheet, Alert } from 'react-native';
import { BankAccountFormData } from '../types/onboarding';
import { validateIFSC, validateAccountNumber } from '../schemas/onboardingSchemas';

interface Props {
  initialBank?: Partial<BankAccountFormData>;
  onNext: (data: BankAccountFormData) => void;
  onBack: () => void;
}

export const BankDetailsStep: React.FC<Props> = ({
  initialBank,
  onNext,
  onBack,
}) => {
  const [holder, setHolder] = useState(initialBank?.accountHolderName || '');
  const [accNum, setAccNum] = useState(initialBank?.accountNumber || '');
  const [confirmAcc, setConfirmAcc] = useState(initialBank?.accountNumber || '');
  const [ifsc, setIfsc] = useState(initialBank?.ifscCode || '');
  const [bankName, setBankName] = useState(initialBank?.bankName || 'State Bank of India');

  const handleSave = () => {
    if (!holder.trim()) {
      Alert.alert('Holder Name Required', 'Please enter the account holder name matching your official identity.');
      return;
    }
    if (!validateAccountNumber(accNum)) {
      Alert.alert('Invalid Account', 'Please enter a valid bank account number (6 to 20 digits).');
      return;
    }
    if (accNum !== confirmAcc) {
      Alert.alert('Mismatch', 'Bank account numbers do not match.');
      return;
    }
    if (!validateIFSC(ifsc)) {
      Alert.alert('Invalid IFSC', 'Please enter a valid 11-character IFSC code (e.g. SBIN0001234).');
      return;
    }

    onNext({
      accountHolderName: holder.trim(),
      accountNumber: accNum.trim(),
      confirmAccountNumber: confirmAcc.trim(),
      ifscCode: ifsc.trim().toUpperCase(),
      bankName: bankName.trim(),
    });
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Direct Payout Account</Text>
      <Text style={styles.subtitle}>
        Freight trip settlements, advances, and incentives are disbursed directly into this verified account.
      </Text>

      <Text style={styles.label}>Account Holder Name (As per Bank)</Text>
      <TextInput
        style={styles.input}
        value={holder}
        onChangeText={setHolder}
        placeholder="e.g. Rajesh Kumar"
        placeholderTextColor={Colors.textSecondary}
      />

      <Text style={styles.label}>Bank Account Number</Text>
      <TextInput
        style={styles.input}
        value={accNum}
        onChangeText={setAccNum}
        placeholder="Enter bank account number"
        placeholderTextColor={Colors.textSecondary}
        keyboardType="numeric"
        secureTextEntry
      />

      <Text style={styles.label}>Confirm Account Number</Text>
      <TextInput
        style={styles.input}
        value={confirmAcc}
        onChangeText={setConfirmAcc}
        placeholder="Re-enter bank account number"
        placeholderTextColor={Colors.textSecondary}
        keyboardType="numeric"
      />

      <Text style={styles.label}>Bank IFSC Code</Text>
      <TextInput
        style={styles.input}
        value={ifsc}
        onChangeText={(t) => setIfsc(t.toUpperCase())}
        placeholder="e.g. SBIN0001234"
        placeholderTextColor={Colors.textSecondary}
        autoCapitalize="characters"
      />

      <Text style={styles.label}>Bank Name</Text>
      <TextInput
        style={styles.input}
        value={bankName}
        onChangeText={setBankName}
        placeholder="e.g. State Bank of India"
        placeholderTextColor={Colors.textSecondary}
      />

      <View style={styles.btnRow}>
        <TouchableOpacity style={styles.btnSecondary} onPress={onBack}>
          <Text style={styles.btnSecondaryText}>← Back</Text>
        </TouchableOpacity>
        <TouchableOpacity style={styles.btnPrimary} onPress={handleSave}>
          <Text style={styles.btnPrimaryText}>Review Application →</Text>
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
  btnRow: {
    flexDirection: 'row',
    gap: 10,
    marginTop: 20,
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
