import React, { useState } from 'react';
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
        placeholderTextColor="#64748b"
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
        placeholderTextColor="#64748b"
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
  },
  label: {
    fontSize: 12,
    fontWeight: '600',
    color: '#cbd5e1',
    marginBottom: 6,
    marginTop: 12,
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
  disabledInput: {
    backgroundColor: '#090d16',
    color: '#64748b',
    borderColor: '#1e293b',
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
    backgroundColor: '#1e293b',
    borderWidth: 1,
    borderColor: '#334155',
  },
  langChipActive: {
    borderColor: '#06b6d4',
    backgroundColor: '#083344',
  },
  langText: {
    color: '#94a3b8',
    fontSize: 12,
    fontWeight: '600',
  },
  langTextActive: {
    color: '#38bdf8',
  },
  btn: {
    backgroundColor: '#0d9488',
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 10,
  },
  btnText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },
});
