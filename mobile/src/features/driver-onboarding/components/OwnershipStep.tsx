import React, { useState } from 'react';
import { Colors, FontSize} from '../../../constants/theme';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { OwnershipType } from '../types/onboarding';

interface Props {
  onSelect: (type: OwnershipType) => void;
}

export const OwnershipStep: React.FC<Props> = ({ onSelect }) => {
  const [selected, setSelected] = useState<OwnershipType>('owner_operator');

  return (
    <View style={styles.card}>
      <Text style={styles.title}>Fleet Engagement Type</Text>
      <Text style={styles.subtitle}>Select how you will operate commercial freight with Avandab.</Text>

      <TouchableOpacity
        style={[styles.optionCard, selected === 'owner_operator' && styles.optionCardActive]}
        onPress={() => setSelected('owner_operator')}
      >
        <View style={styles.optionHeader}>
          <Text style={styles.optionIcon}>🚛</Text>
          <View style={styles.optionTextCol}>
            <Text style={[styles.optionTitle, selected === 'owner_operator' && styles.optionTitleActive]}>
              Owner-Operator
            </Text>
            <Text style={styles.optionDesc}>
              I own or lease my commercial truck. I will register and verify my RC & vehicle compliance documents.
            </Text>
          </View>
        </View>
      </TouchableOpacity>

      <TouchableOpacity
        style={[styles.optionCard, selected === 'company_driver' && styles.optionCardActive]}
        onPress={() => setSelected('company_driver')}
      >
        <View style={styles.optionHeader}>
          <Text style={styles.optionIcon}>🏢</Text>
          <View style={styles.optionTextCol}>
            <Text style={[styles.optionTitle, selected === 'company_driver' && styles.optionTitleActive]}>
              Company Fleet Driver
            </Text>
            <Text style={styles.optionDesc}>
              I drive company-owned vehicles. My dispatcher will assign an authorized fleet vehicle to my shift.
            </Text>
          </View>
        </View>
      </TouchableOpacity>

      <TouchableOpacity style={styles.btn} onPress={() => onSelect(selected)}>
        <Text style={styles.btnText}>Proceed with {selected === 'owner_operator' ? 'Vehicle Claim' : 'Company Fleet'} →</Text>
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
  optionCard: {
    backgroundColor: Colors.modalBg,
    borderRadius: 12,
    padding: 16,
    marginBottom: 12,
    borderWidth: 1.5,
    borderColor: Colors.modalBorder,
  },
  optionCardActive: {
    borderColor: Colors.onboardingBorder,
    backgroundColor: Colors.onboardingBg,
  },
  optionHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
  },
  optionIcon: {
    fontSize: FontSize.display,
    marginRight: 12,
    marginTop: 2,
  },
  optionTextCol: {
    flex: 1,
  },
  optionTitle: {
    fontSize: FontSize.heading,
    fontWeight: '700',
    color: Colors.textSecondary,
    marginBottom: 4,
  },
  optionTitleActive: {
    color: Colors.onboardingText,
  },
  optionDesc: {
    fontSize: FontSize.body,
    color: Colors.modalSub,
    lineHeight: 18,
  },
  btn: {
    backgroundColor: Colors.onboardingBtn,
    borderRadius: 12,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 12,
  },
  btnText: {
    color: Colors.textOnPrimary,
    fontSize: FontSize.titleLarge,
    fontWeight: '700',
  },
});
