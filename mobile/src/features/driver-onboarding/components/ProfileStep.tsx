import React, { useState } from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TextInput, TouchableOpacity, StyleSheet } from 'react-native';
import { ProfileFormData } from '../types/onboarding';

interface Props {
  initialData: { name: string; phone: string; email: string };
  onNext: (data: ProfileFormData) => void;
}

export const ProfileStep: React.FC<Props> = ({ initialData, onNext }) => {
  const [name, setName] = useState(initialData.name || '');
  const [phone, setPhone] = useState(initialData.phone || '');
  const [email, setEmail] = useState(initialData.email || '');
  const [preferredLang, setPreferredLang] = useState('English');

  const languages = ['English', 'हिंदी (Hindi)', 'मराठी (Marathi)', 'ગુજરાતી (Gujarati)'];

  const handleSubmit = () => {
    onNext({
      name,
      phone,
      email,
      preferredLanguage: preferredLang,
    });
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Driver Profile</Text>
      <Text style={styles.subtitle}>Confirm your operational identity on the Avandab fleet network.</Text>

      <Text style={styles.label}>Full Legal Name</Text>
      <TextInput
        style={styles.input}
        value={name}
        onChangeText={setName}
        placeholder="Enter full name"
        placeholderTextColor={Colors.textSecondary}
      />

      <Text style={styles.label}>Mobile Phone (Registered)</Text>
      <TextInput
        style={[styles.input, styles.disabledInput]}
        value={phone}
        editable={false}
      />

      <Text style={styles.label}>Email Address</Text>
      <TextInput
        style={styles.input}
        value={email}
        onChangeText={setEmail}
        placeholder="Enter email address"
        placeholderTextColor={Colors.textSecondary}
        keyboardType="email-address"
        autoCapitalize="none"
      />

      <Text style={styles.label}>Preferred Dispatch Language</Text>
      <View style={styles.langGrid}>
        {languages.map((lang) => (
          <TouchableOpacity
            key={lang}
            style={[styles.langChip, preferredLang === lang && styles.langChipActive]}
            onPress={() => setPreferredLang(lang)}
          >
            <Text style={[styles.langText, preferredLang === lang && styles.langTextActive]}>
              {lang}
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      <TouchableOpacity style={styles.btn} onPress={handleSubmit}>
        <Text style={styles.btnText}>Continue to Fleet Assignment →</Text>
      </TouchableOpacity>
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
  },
  label: {
    fontSize: FontSize.body,
    fontWeight: '600',
    color: Colors.textSecondary,
    marginBottom: 6,
    marginTop: 12,
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
  disabledInput: {
    backgroundColor: Colors.onboardingInk,
    color: Colors.textSecondary,
    borderColor: Colors.modalBg,
  },
  langGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginTop: 6,
    marginBottom: 20,
  },
  langChip: {
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: Colors.modalBg,
    borderWidth: 1,
    borderColor: Colors.modalBorder,
  },
  langChipActive: {
    borderColor: Colors.onboardingBorder,
    backgroundColor: Colors.onboardingBg,
  },
  langText: {
    color: Colors.modalSub,
    fontSize: FontSize.body,
    fontWeight: '600',
  },
  langTextActive: {
    color: Colors.onboardingText,
  },
  btn: {
    backgroundColor: Colors.onboardingBtn,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 10,
  },
  btnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
  },
});
