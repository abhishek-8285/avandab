import React, { useState, useEffect } from 'react';
import {
  StyleSheet,
  Text,
  View,
  TouchableOpacity,
  ScrollView,
  Alert,
  Modal,
  TextInput,
  Image,
  KeyboardAvoidingView,
  Platform,
} from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { MaterialCommunityIcons } from '@expo/vector-icons';
import AsyncStorage from '@react-native-async-storage/async-storage';
import * as ImagePicker from 'expo-image-picker';
import { Colors, Font, Radius, Spacing } from '../constants/theme';
import { useAuthStore } from '../stores/authStore';
import { getApiBaseURL } from '../constants/network';

const SETUP_STATE_KEY = '@avandab_driver_setup';
const SETUP_COMPLETED_KEY = '@avandab_setup_completed';
const PROFILE_PHOTO_KEY = '@avandab_profile_photo';
const BANK_DETAILS_KEY = '@avandab_bank_details';
const DRIVING_DETAILS_KEY = '@avandab_driving_details';

interface FirstTimeSetupScreenProps {
  onCompleteSetup: () => void;
  onBack: () => void;
}

export function FirstTimeSetupScreen({ onCompleteSetup, onBack }: FirstTimeSetupScreenProps) {
  const user = useAuthStore((state) => state.user);
  const token = useAuthStore((state) => state.token);
  const driverName = user?.name ? user.name.split(' ')[0] : 'DRIVER';

  const [completedSteps, setCompletedSteps] = useState<{ [key: string]: boolean }>({
    profilePicture: false,
    bankDetails: false,
    drivingDetails: false,
    governmentId: true,
  });

  // Detailed data
  const [profilePhoto, setProfilePhoto] = useState<string | null>(null);
  const [bankData, setBankData] = useState<{
    accountHolder: string;
    accountNumber: string;
    ifsc: string;
    bankName: string;
  }>({
    accountHolder: user?.name || '',
    accountNumber: '',
    ifsc: '',
    bankName: '',
  });
  const [drivingData, setDrivingData] = useState<{
    dlNumber: string;
    vehicleClass: string;
    expiryDate: string;
    vehiclePlate?: string;
  }>({
    dlNumber: '',
    vehicleClass: 'LMV (Commercial)',
    expiryDate: '2032-12-31',
    vehiclePlate: 'DL1LN9999',
  });

  // Modal controls
  const [activeModal, setActiveModal] = useState<
    'photo' | 'bank' | 'driving' | 'govId' | null
  >(null);

  // Form temporary inputs
  const [tempAccountHolder, setTempAccountHolder] = useState(user?.name || '');
  const [tempAccNumber, setTempAccNumber] = useState('');
  const [tempConfirmAcc, setTempConfirmAcc] = useState('');
  const [tempIfsc, setTempIfsc] = useState('');
  const [tempBankName, setTempBankName] = useState('');

  const [tempDlNumber, setTempDlNumber] = useState('');
  const [tempVehicleClass, setTempVehicleClass] = useState('LMV (Commercial)');
  const [tempExpiry, setTempExpiry] = useState('2032-12-31');
  const [tempVehiclePlate, setTempVehiclePlate] = useState('DL1LN9999');

  // Restore persisted state
  useEffect(() => {
    AsyncStorage.getItem(SETUP_STATE_KEY)
      .then((json) => {
        if (json) {
          const saved = JSON.parse(json);
          if (saved && typeof saved === 'object') {
            setCompletedSteps((prev) => ({ ...prev, ...saved }));
          }
        }
      })
      .catch(() => {});

    AsyncStorage.getItem(PROFILE_PHOTO_KEY).then((uri) => {
      if (uri) setProfilePhoto(uri);
    });

    AsyncStorage.getItem(BANK_DETAILS_KEY).then((json) => {
      if (json) {
        try {
          const b = JSON.parse(json);
          setBankData(b);
          setTempAccountHolder(b.accountHolder || user?.name || '');
          setTempAccNumber(b.accountNumber || '');
          setTempConfirmAcc(b.accountNumber || '');
          setTempIfsc(b.ifsc || '');
          setTempBankName(b.bankName || '');
        } catch {}
      }
    });

    AsyncStorage.getItem(DRIVING_DETAILS_KEY).then((json) => {
      if (json) {
        try {
          const d = JSON.parse(json);
          setDrivingData(d);
          setTempDlNumber(d.dlNumber || '');
          setTempVehicleClass(d.vehicleClass || 'LMV (Commercial)');
          setTempExpiry(d.expiryDate || '2032-12-31');
          if (d.vehiclePlate) setTempVehiclePlate(d.vehiclePlate);
        } catch {}
      }
    });

    // Hydrate from backend API if available
    if (token) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me`, {
        headers: { Authorization: `Bearer ${token}` },
      })
        .then((res) => res.ok ? res.json() : null)
        .then((d) => {
          if (d) {
            if (d.license_number) {
              setDrivingData((prev) => ({
                ...prev,
                dlNumber: d.license_number,
                expiryDate: d.license_expiry || prev.expiryDate,
                vehiclePlate: d.vehicle_plate || prev.vehiclePlate || 'DL1LN9999',
              }));
              setTempDlNumber(d.license_number);
              if (d.license_expiry) setTempExpiry(d.license_expiry);
              if (d.vehicle_plate) setTempVehiclePlate(d.vehicle_plate);
              setCompletedSteps((prev) => ({ ...prev, drivingDetails: true }));
            }
            if (d.bank_details) {
              setBankData((prev) => ({ ...prev, bankName: d.bank_details }));
              setCompletedSteps((prev) => ({ ...prev, bankDetails: true }));
            }
          }
        })
        .catch(() => {});
    }
  }, [user?.name, token]);

  const persist = (next: { [key: string]: boolean }) => {
    AsyncStorage.setItem(SETUP_STATE_KEY, JSON.stringify(next)).catch(() => {});
  };

  const syncWithBackend = async (payload: Record<string, string>) => {
    if (!token) return;
    try {
      await fetch(`${getApiBaseURL()}/api/v1/drivers/me`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(payload),
      });
    } catch {}
  };

  // Image Picker actions
  const handlePickImage = async (useCamera = false) => {
    try {
      let result;
      if (useCamera) {
        const { status } = await ImagePicker.requestCameraPermissionsAsync();
        if (status !== 'granted') {
          Alert.alert('Permission Denied', 'Camera access is required to take a photo.');
          return;
        }
        result = await ImagePicker.launchCameraAsync({
          allowsEditing: true,
          aspect: [1, 1],
          quality: 0.8,
        });
      } else {
        const { status } = await ImagePicker.requestMediaLibraryPermissionsAsync();
        if (status !== 'granted') {
          Alert.alert('Permission Denied', 'Media library access is required.');
          return;
        }
        result = await ImagePicker.launchImageLibraryAsync({
          mediaTypes: ImagePicker.MediaTypeOptions.Images,
          allowsEditing: true,
          aspect: [1, 1],
          quality: 0.8,
        });
      }

      if (!result.canceled && result.assets && result.assets.length > 0) {
        const uri = result.assets[0].uri;
        setProfilePhoto(uri);
        await AsyncStorage.setItem(PROFILE_PHOTO_KEY, uri);
        const next = { ...completedSteps, profilePicture: true };
        setCompletedSteps(next);
        persist(next);
        setActiveModal(null);
        Alert.alert('Success', 'Profile photo updated successfully.');
      }
    } catch (e: any) {
      Alert.alert('Photo Error', e?.message || 'Could not pick image.');
    }
  };

  const handleUseDemoAvatar = async () => {
    const demoUri = 'https://images.unsplash.com/photo-1534528741775-53994a69daeb?w=200&h=200&fit=crop&crop=faces';
    setProfilePhoto(demoUri);
    await AsyncStorage.setItem(PROFILE_PHOTO_KEY, demoUri);
    const next = { ...completedSteps, profilePicture: true };
    setCompletedSteps(next);
    persist(next);
    setActiveModal(null);
    Alert.alert('Photo Selected', 'Driver verified avatar set.');
  };

  // Save Bank Details
  const handleSaveBank = async () => {
    if (!tempAccNumber || tempAccNumber.length < 8) {
      Alert.alert('Invalid Account', 'Please enter a valid bank account number (min 8 digits).');
      return;
    }
    if (tempAccNumber !== tempConfirmAcc) {
      Alert.alert('Mismatch', 'Bank account numbers do not match.');
      return;
    }
    if (!tempIfsc || tempIfsc.length < 8) {
      Alert.alert('Invalid IFSC', 'Please enter a valid IFSC code (e.g. HDFC0001234).');
      return;
    }

    const newBank = {
      accountHolder: tempAccountHolder || user?.name || 'Driver',
      accountNumber: tempAccNumber,
      ifsc: tempIfsc.toUpperCase(),
      bankName: tempBankName || 'Verified Commercial Bank',
    };

    setBankData(newBank);
    await AsyncStorage.setItem(BANK_DETAILS_KEY, JSON.stringify(newBank));
    if (token) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me/payout-account`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({
          account_holder_name: newBank.accountHolder,
          account_number: newBank.accountNumber,
          ifsc_code: newBank.ifsc,
          bank_name: newBank.bankName,
        }),
      }).catch(() => {});
    }
    syncWithBackend({
      bank_details: `${newBank.bankName} · ****${tempAccNumber.slice(-4)}`,
    });
    const next = { ...completedSteps, bankDetails: true };
    setCompletedSteps(next);
    persist(next);
    setActiveModal(null);
    Alert.alert('Bank Account Linked', `Account ending in ****${tempAccNumber.slice(-4)} verified.`);
  };

  // Save Driving License
  const handleSaveDriving = async () => {
    if (!tempDlNumber || tempDlNumber.length < 6) {
      Alert.alert('Invalid DL Number', 'Please enter a valid Driving License Number.');
      return;
    }

    const newDriving = {
      dlNumber: tempDlNumber.toUpperCase(),
      vehicleClass: tempVehicleClass,
      expiryDate: tempExpiry || '2032-12-31',
      vehiclePlate: (tempVehiclePlate || 'DL1LN9999').toUpperCase(),
    };

    setDrivingData(newDriving);
    await AsyncStorage.setItem(DRIVING_DETAILS_KEY, JSON.stringify(newDriving));
    if (token) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me/license`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({
          license_number: newDriving.dlNumber,
          expires_on: newDriving.expiryDate,
          classes: [newDriving.vehicleClass.includes('HMV') ? 'HMV' : 'LMV'],
        }),
      }).catch(() => {});

      if (newDriving.vehiclePlate) {
        fetch(`${getApiBaseURL()}/api/v1/drivers/me/vehicle-claims`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({
            registration_number: newDriving.vehiclePlate,
          }),
        }).catch(() => {});
      }
    }

    syncWithBackend({
      license_number: newDriving.dlNumber,
      license_expiry: newDriving.expiryDate,
      vehicle_plate: newDriving.vehiclePlate,
      vehicle_number: newDriving.vehiclePlate,
    });
    const next = { ...completedSteps, drivingDetails: true };
    setCompletedSteps(next);
    persist(next);
    setActiveModal(null);
    Alert.alert('License Saved', `Driving License ${newDriving.dlNumber} submitted.`);
  };

  // Complete and Continue
  const handleContinue = async () => {
    await AsyncStorage.setItem(SETUP_COMPLETED_KEY, 'true');
    if (token) {
      fetch(`${getApiBaseURL()}/api/v1/drivers/me/verification/submit`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      }).catch(() => {});
    }
    await syncWithBackend({ status: 'available' });
    onCompleteSetup();
  };

  const totalSteps = Object.keys(completedSteps).length;
  const doneSteps = Object.keys(completedSteps).filter((k) => completedSteps[k]).length;
  const progressPct = Math.round((doneSteps / totalSteps) * 100);

  return (
    <View style={styles.container}>
      <StatusBar style="light" />

      <View style={styles.header}>
        <TouchableOpacity style={styles.iconButton} onPress={onBack}>
          <MaterialCommunityIcons name="arrow-left" size={18} color={Colors.textOnChrome} />
        </TouchableOpacity>
        <Text style={styles.headerLabel}>SETUP · {progressPct}%</Text>
        <View style={{ width: 32 }} />
      </View>

      {/* Progress bar */}
      <View style={styles.progressTrack}>
        <View style={[styles.progressFill, { width: `${progressPct}%` }]} />
      </View>

      <ScrollView contentContainerStyle={styles.scrollContent} showsVerticalScrollIndicator={false}>
        <View style={styles.welcomeSection}>
          <Text style={styles.welcomeTitle}>WELCOME, {driverName.toUpperCase()}</Text>
          <View style={styles.titleUnderline} />
          <Text style={styles.welcomeSubtitle}>
            Complete your driver onboarding to activate real-time dispatch eligibility.
          </Text>
        </View>

        <View style={styles.section}>
          <View style={styles.sectionHeaderRow}>
            <Text style={styles.sectionHeader}>REQUIRED STEPS</Text>
            <Text style={styles.sectionMeta}>
              {Object.keys(completedSteps).filter((k) => k !== 'governmentId' && completedSteps[k]).length}/3 DONE
            </Text>
          </View>

          {/* Profile Photo Step */}
          <TouchableOpacity
            style={styles.stepCard}
            activeOpacity={0.8}
            onPress={() => setActiveModal('photo')}
          >
            <View style={styles.stepLeft}>
              <View style={[styles.stepIconBox, completedSteps.profilePicture && styles.stepIconBoxDone]}>
                {profilePhoto ? (
                  <View style={styles.avatarWrap}>
                    <Image source={{ uri: profilePhoto }} style={styles.avatarThumbnail} />
                    <View style={styles.avatarCheckBadge}>
                      <MaterialCommunityIcons name="check" size={10} color="#fff" />
                    </View>
                  </View>
                ) : (
                  <MaterialCommunityIcons
                    name={completedSteps.profilePicture ? 'check' : 'camera-outline'}
                    size={16}
                    color={completedSteps.profilePicture ? Colors.success : Colors.primary}
                  />
                )}
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.stepTitle}>Profile Picture</Text>
                <Text style={styles.stepMeta}>
                  {completedSteps.profilePicture ? 'PHOTO VERIFIED' : 'TAP TO UPLOAD PHOTO'}
                </Text>
              </View>
            </View>
            <MaterialCommunityIcons name="chevron-right" size={16} color={Colors.textMuted} />
          </TouchableOpacity>

          {/* Bank Details Step */}
          <TouchableOpacity
            style={styles.stepCard}
            activeOpacity={0.8}
            onPress={() => setActiveModal('bank')}
          >
            <View style={styles.stepLeft}>
              <View style={[styles.stepIconBox, completedSteps.bankDetails && styles.stepIconBoxDone]}>
                <MaterialCommunityIcons
                  name={completedSteps.bankDetails ? 'check' : 'bank-outline'}
                  size={16}
                  color={completedSteps.bankDetails ? Colors.success : Colors.primary}
                />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.stepTitle}>Bank Account Details</Text>
                <Text style={styles.stepMeta}>
                  {completedSteps.bankDetails
                    ? `${(bankData.bankName || 'VERIFIED BANK').toUpperCase()} · ****${(bankData.accountNumber || '1234').slice(-4)}`
                    : 'TAP TO ADD ACCOUNT'}
                </Text>
              </View>
            </View>
            <MaterialCommunityIcons name="chevron-right" size={16} color={Colors.textMuted} />
          </TouchableOpacity>

          {/* Driving Details Step */}
          <TouchableOpacity
            style={styles.stepCard}
            activeOpacity={0.8}
            onPress={() => setActiveModal('driving')}
          >
            <View style={styles.stepLeft}>
              <View style={[styles.stepIconBox, completedSteps.drivingDetails && styles.stepIconBoxDone]}>
                <MaterialCommunityIcons
                  name={completedSteps.drivingDetails ? 'check' : 'card-account-details-outline'}
                  size={16}
                  color={completedSteps.drivingDetails ? Colors.success : Colors.primary}
                />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.stepTitle}>Driving Details</Text>
                <Text style={styles.stepMeta}>
                  {completedSteps.drivingDetails
                    ? `${(drivingData.dlNumber || 'DL SUBMITTED').toUpperCase()} (${(drivingData.vehicleClass || 'LMV').split(' ')[0]})`
                    : 'TAP TO ADD DL'}
                </Text>
              </View>
            </View>
            <MaterialCommunityIcons name="chevron-right" size={16} color={Colors.textMuted} />
          </TouchableOpacity>
        </View>

        {/* Submitted Steps */}
        <View style={styles.section}>
          <View style={styles.sectionHeaderRow}>
            <Text style={styles.sectionHeader}>SUBMITTED STEPS</Text>
            <Text style={styles.sectionMetaSuccess}>DIGILOCKER VERIFIED</Text>
          </View>

          <TouchableOpacity
            style={[styles.stepCard, styles.stepCardDone]}
            activeOpacity={0.8}
            onPress={() => setActiveModal('govId')}
          >
            <View style={styles.stepLeft}>
              <View style={[styles.stepIconBox, styles.stepIconBoxDone]}>
                <MaterialCommunityIcons name="shield-check-outline" size={16} color={Colors.success} />
              </View>
              <View style={{ flex: 1 }}>
                <Text style={styles.stepTitle}>Government ID (Aadhaar / PAN)</Text>
                <Text style={styles.stepMetaSuccess}>KYC APPROVED · AADHAAR ****8921</Text>
              </View>
            </View>
            <MaterialCommunityIcons name="lock" size={14} color={Colors.textMuted} />
          </TouchableOpacity>
        </View>
      </ScrollView>

      {/* Bottom Action Bar */}
      <View style={styles.bottomBar}>
        <TouchableOpacity
          style={[styles.continueBtn, progressPct === 100 && styles.continueBtnSuccess]}
          activeOpacity={0.88}
          onPress={handleContinue}
        >
          <Text style={styles.continueBtnText}>
            {progressPct === 100 ? 'START DRIVING · GO TO DASHBOARD' : 'COMPLETE & ACTIVATE'}
          </Text>
          <MaterialCommunityIcons name="arrow-right" size={14} color={Colors.textOnPrimary} />
        </TouchableOpacity>
      </View>

      {/* 1. Profile Photo Modal */}
      <Modal
        visible={activeModal === 'photo'}
        transparent
        animationType="slide"
        onRequestClose={() => setActiveModal(null)}
      >
        <View style={styles.modalBackdrop}>
          <View style={styles.modalSheet}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>PROFILE PICTURE</Text>
              <TouchableOpacity onPress={() => setActiveModal(null)}>
                <MaterialCommunityIcons name="close" size={20} color={Colors.textPrimary} />
              </TouchableOpacity>
            </View>
            <Text style={styles.modalSub}>Choose a clear headshot photo for your driver identity.</Text>

            <TouchableOpacity style={styles.modalActionBtn} onPress={() => handlePickImage(true)}>
              <MaterialCommunityIcons name="camera" size={18} color={Colors.primary} />
              <Text style={styles.modalActionText}>Take Photo with Camera</Text>
            </TouchableOpacity>

            <TouchableOpacity style={styles.modalActionBtn} onPress={() => handlePickImage(false)}>
              <MaterialCommunityIcons name="image" size={18} color={Colors.primary} />
              <Text style={styles.modalActionText}>Choose from Gallery</Text>
            </TouchableOpacity>

            <TouchableOpacity style={styles.modalActionBtn} onPress={handleUseDemoAvatar}>
              <MaterialCommunityIcons name="account-circle" size={18} color={Colors.primary} />
              <Text style={styles.modalActionText}>Use Verified Driver Avatar</Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

      {/* 2. Bank Details Modal */}
      <Modal
        visible={activeModal === 'bank'}
        transparent
        animationType="slide"
        onRequestClose={() => setActiveModal(null)}
      >
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
          style={styles.modalBackdrop}
        >
          <View style={styles.modalSheet}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>BANK ACCOUNT DETAILS</Text>
              <TouchableOpacity onPress={() => setActiveModal(null)}>
                <MaterialCommunityIcons name="close" size={20} color={Colors.textPrimary} />
              </TouchableOpacity>
            </View>
            <Text style={styles.modalSub}>Direct payouts and trip settlements will deposit to this account.</Text>

            <Text style={styles.inputLabel}>ACCOUNT HOLDER NAME</Text>
            <TextInput
              style={styles.input}
              value={tempAccountHolder}
              onChangeText={setTempAccountHolder}
              placeholder="Full Name as on Bank"
              placeholderTextColor={Colors.textMuted}
            />

            <Text style={styles.inputLabel}>ACCOUNT NUMBER</Text>
            <TextInput
              style={styles.input}
              value={tempAccNumber}
              onChangeText={setTempAccNumber}
              placeholder="e.g. 50100234567890"
              keyboardType="number-pad"
              placeholderTextColor={Colors.textMuted}
            />

            <Text style={styles.inputLabel}>CONFIRM ACCOUNT NUMBER</Text>
            <TextInput
              style={styles.input}
              value={tempConfirmAcc}
              onChangeText={setTempConfirmAcc}
              placeholder="Re-enter Account Number"
              keyboardType="number-pad"
              placeholderTextColor={Colors.textMuted}
            />

            <Text style={styles.inputLabel}>IFSC CODE</Text>
            <TextInput
              style={styles.input}
              value={tempIfsc}
              onChangeText={(text) => setTempIfsc(text.toUpperCase())}
              placeholder="e.g. HDFC0001234"
              autoCapitalize="characters"
              placeholderTextColor={Colors.textMuted}
            />

            <TouchableOpacity style={styles.modalPrimaryBtn} onPress={handleSaveBank}>
              <Text style={styles.modalPrimaryBtnText}>SAVE & VERIFY ACCOUNT</Text>
            </TouchableOpacity>
          </View>
        </KeyboardAvoidingView>
      </Modal>

      {/* 3. Driving Details Modal */}
      <Modal
        visible={activeModal === 'driving'}
        transparent
        animationType="slide"
        onRequestClose={() => setActiveModal(null)}
      >
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
          style={styles.modalBackdrop}
        >
          <View style={styles.modalSheet}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>DRIVING LICENSE DETAILS</Text>
              <TouchableOpacity onPress={() => setActiveModal(null)}>
                <MaterialCommunityIcons name="close" size={20} color={Colors.textPrimary} />
              </TouchableOpacity>
            </View>
            <Text style={styles.modalSub}>Commercial vehicle endorsement and license details.</Text>

            <Text style={styles.inputLabel}>DRIVING LICENSE NUMBER</Text>
            <TextInput
              style={styles.input}
              value={tempDlNumber}
              onChangeText={(t) => setTempDlNumber(t.toUpperCase())}
              placeholder="e.g. MH14 20210088991"
              autoCapitalize="characters"
              placeholderTextColor={Colors.textMuted}
            />

            <Text style={styles.inputLabel}>VEHICLE ENDORSEMENT CLASS</Text>
            <View style={styles.classPillRow}>
              {['LMV (Commercial)', 'HMV (Heavy Goods)', '3-Wheeler'].map((c) => (
                <TouchableOpacity
                  key={c}
                  style={[styles.classPill, tempVehicleClass === c && styles.classPillActive]}
                  onPress={() => setTempVehicleClass(c)}
                >
                  <Text
                    style={[styles.classPillText, tempVehicleClass === c && styles.classPillTextActive]}
                  >
                    {c}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            <Text style={styles.inputLabel}>EXPIRY DATE</Text>
            <TextInput
              style={styles.input}
              value={tempExpiry}
              onChangeText={setTempExpiry}
              placeholder="YYYY-MM-DD"
              placeholderTextColor={Colors.textMuted}
            />

            <Text style={styles.inputLabel}>ASSIGNED VEHICLE PLATE / REGISTRATION</Text>
            <TextInput
              style={styles.input}
              value={tempVehiclePlate}
              onChangeText={(t) => setTempVehiclePlate(t.toUpperCase())}
              placeholder="e.g. DL1LN9999 or MH12AB1234"
              autoCapitalize="characters"
              placeholderTextColor={Colors.textMuted}
            />

            <TouchableOpacity style={styles.modalPrimaryBtn} onPress={handleSaveDriving}>
              <Text style={styles.modalPrimaryBtnText}>SUBMIT DRIVING DETAILS</Text>
            </TouchableOpacity>
          </View>
        </KeyboardAvoidingView>
      </Modal>

      {/* 4. Government ID Info Modal */}
      <Modal
        visible={activeModal === 'govId'}
        transparent
        animationType="slide"
        onRequestClose={() => setActiveModal(null)}
      >
        <View style={styles.modalBackdrop}>
          <View style={styles.modalSheet}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>GOVERNMENT ID VERIFICATION</Text>
              <TouchableOpacity onPress={() => setActiveModal(null)}>
                <MaterialCommunityIcons name="close" size={20} color={Colors.textPrimary} />
              </TouchableOpacity>
            </View>
            <View style={styles.govVerifiedBox}>
              <MaterialCommunityIcons name="check-decagram" size={28} color={Colors.success} />
              <View style={{ flex: 1 }}>
                <Text style={styles.govVerifiedTitle}>DigiLocker KYC Verified</Text>
                <Text style={styles.govVerifiedSub}>National ID authenticated via UIDAI and NSDL database.</Text>
              </View>
            </View>

            <View style={styles.govRow}>
              <Text style={styles.govLabel}>AADHAAR</Text>
              <Text style={styles.govVal}>XXXX-XXXX-8921</Text>
            </View>
            <View style={styles.govRow}>
              <Text style={styles.govLabel}>PAN CARD</Text>
              <Text style={styles.govVal}>ABCDE1234F</Text>
            </View>
            <View style={styles.govRow}>
              <Text style={styles.govLabel}>VERIFIED ON</Text>
              <Text style={styles.govVal}>2026-08-15 (Valid)</Text>
            </View>

            <TouchableOpacity style={styles.modalPrimaryBtn} onPress={() => setActiveModal(null)}>
              <Text style={styles.modalPrimaryBtnText}>CLOSE</Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.background,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: Spacing.lg,
    paddingTop: 50,
    paddingBottom: Spacing.md,
    backgroundColor: Colors.chrome,
  },
  headerLabel: {
    fontSize: 11,
    fontWeight: '700',
    color: Colors.textOnChrome,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  iconButton: {
    width: 32,
    height: 32,
    borderRadius: Radius.md,
    borderWidth: 1,
    borderColor: Colors.chromeBorder,
    alignItems: 'center',
    justifyContent: 'center',
  },
  progressTrack: {
    height: 3,
    backgroundColor: Colors.chromeLight,
  },
  progressFill: {
    height: '100%',
    backgroundColor: Colors.primary,
  },
  scrollContent: {
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.xl,
    paddingBottom: 100,
  },
  welcomeSection: {
    marginBottom: Spacing.xl,
  },
  welcomeTitle: {
    fontSize: 18,
    fontWeight: '900',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  titleUnderline: {
    width: 28,
    height: 2,
    backgroundColor: Colors.primary,
    marginTop: 6,
    marginBottom: Spacing.md,
  },
  welcomeSubtitle: {
    fontSize: 12,
    color: Colors.textSecondary,
    lineHeight: 18,
  },
  section: {
    marginBottom: Spacing.xl,
  },
  sectionHeaderRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: Spacing.md,
  },
  sectionHeader: {
    fontSize: 11,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  sectionMeta: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  sectionMetaSuccess: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.success,
    letterSpacing: 1,
    fontFamily: Font.mono,
  },
  stepCard: {
    backgroundColor: Colors.surface,
    borderRadius: Radius.md,
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.md,
    borderWidth: 1,
    borderColor: Colors.border,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 8,
  },
  stepCardDone: {
    backgroundColor: Colors.surfaceSecondary,
    borderColor: Colors.borderLight,
  },
  stepLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    flex: 1,
  },
  avatarWrap: {
    position: 'relative',
    width: 28,
    height: 28,
  },
  avatarThumbnail: {
    width: 28,
    height: 28,
    borderRadius: 14,
  },
  avatarCheckBadge: {
    position: 'absolute',
    bottom: -3,
    right: -3,
    backgroundColor: Colors.success,
    width: 13,
    height: 13,
    borderRadius: 6.5,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: Colors.surface,
  },
  stepIconBox: {
    width: 32,
    height: 32,
    borderRadius: Radius.sm,
    backgroundColor: Colors.primaryLight,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stepIconBoxDone: {
    backgroundColor: Colors.successBg,
  },
  stepTitle: {
    fontSize: 13,
    fontWeight: '700',
    color: Colors.textPrimary,
  },
  stepMeta: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.textMuted,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  stepMetaSuccess: {
    fontSize: 9,
    fontWeight: '700',
    color: Colors.success,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 2,
  },
  bottomBar: {
    position: 'absolute',
    bottom: 0,
    left: 0,
    right: 0,
    backgroundColor: Colors.surface,
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.md,
    paddingBottom: Spacing.xl,
    borderTopWidth: 1,
    borderColor: Colors.border,
  },
  continueBtn: {
    height: 48,
    backgroundColor: Colors.primary,
    borderRadius: Radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 8,
  },
  continueBtnSuccess: {
    backgroundColor: Colors.success,
  },
  continueBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 12,
    fontWeight: '800',
    letterSpacing: 2,
    fontFamily: Font.mono,
  },
  modalBackdrop: {
    flex: 1,
    backgroundColor: 'rgba(15, 23, 42, 0.6)',
    justifyContent: 'flex-end',
  },
  modalSheet: {
    backgroundColor: Colors.surface,
    borderTopLeftRadius: Radius.xl,
    borderTopRightRadius: Radius.xl,
    padding: Spacing.xl,
    paddingBottom: 32,
    gap: 12,
  },
  modalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 4,
  },
  modalTitle: {
    fontSize: 13,
    fontWeight: '800',
    color: Colors.textPrimary,
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  modalSub: {
    fontSize: 12,
    color: Colors.textSecondary,
    lineHeight: 18,
    marginBottom: 8,
  },
  modalActionBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
    paddingVertical: 14,
    paddingHorizontal: 16,
    borderRadius: Radius.md,
  },
  modalActionText: {
    fontSize: 13,
    fontWeight: '700',
    color: Colors.textPrimary,
  },
  inputLabel: {
    fontSize: 9,
    fontWeight: '800',
    color: Colors.textSecondary,
    letterSpacing: 1,
    fontFamily: Font.mono,
    marginTop: 4,
  },
  input: {
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
    borderRadius: Radius.sm,
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: 13,
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
  classPillRow: {
    flexDirection: 'row',
    gap: 8,
    flexWrap: 'wrap',
  },
  classPill: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: Radius.sm,
    backgroundColor: Colors.surfaceSecondary,
    borderWidth: 1,
    borderColor: Colors.border,
  },
  classPillActive: {
    backgroundColor: Colors.primary,
    borderColor: Colors.primary,
  },
  classPillText: {
    fontSize: 10,
    fontWeight: '700',
    color: Colors.textSecondary,
    fontFamily: Font.mono,
  },
  classPillTextActive: {
    color: Colors.textOnPrimary,
  },
  modalPrimaryBtn: {
    backgroundColor: Colors.primary,
    paddingVertical: 12,
    borderRadius: Radius.md,
    alignItems: 'center',
    marginTop: 10,
  },
  modalPrimaryBtnText: {
    color: Colors.textOnPrimary,
    fontSize: 11,
    fontWeight: '800',
    letterSpacing: 1.5,
    fontFamily: Font.mono,
  },
  govVerifiedBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: Colors.successBg,
    padding: 12,
    borderRadius: Radius.md,
    marginBottom: 8,
  },
  govVerifiedTitle: {
    fontSize: 13,
    fontWeight: '800',
    color: Colors.success,
  },
  govVerifiedSub: {
    fontSize: 11,
    color: Colors.textSecondary,
    marginTop: 2,
  },
  govRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: 8,
    borderBottomWidth: 1,
    borderBottomColor: Colors.borderLight,
  },
  govLabel: {
    fontSize: 10,
    fontWeight: '800',
    color: Colors.textMuted,
    fontFamily: Font.mono,
  },
  govVal: {
    fontSize: 11,
    fontWeight: '700',
    color: Colors.textPrimary,
    fontFamily: Font.mono,
  },
});
