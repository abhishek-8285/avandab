import React, { useState, useEffect } from 'react';
import { View, Text, TextInput, TouchableOpacity, StyleSheet, Alert } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { StatusBar } from 'expo-status-bar';
import { CameraView, useCameraPermissions } from 'expo-camera';
import QRCode from 'react-native-qrcode-svg';
import { Colors, Font, Spacing, Radius } from '../constants/theme';

type Tab = 'scan' | 'generate';

export function QRDemoScreen() {
  const [tab, setTab] = useState<Tab>('scan');
  const [perm, requestPerm] = useCameraPermissions();
  const [scanned, setScanned] = useState<string>('');
  const [value, setValue] = useState<string>('https://avandab.com');

  useEffect(() => {
    if (tab === 'scan' && !perm?.granted) requestPerm();
  }, [tab]);

  const onScan = (e: { type: string; data: string }) => {
    if (e.type === 'qr') setScanned(e.data);
  };

  return (
    <SafeAreaView style={styles.container} edges={['top', 'left', 'right']}>
      <StatusBar style="light" />
      <View style={styles.header}>
        <Text style={styles.headerTitle}>QR DEMO</Text>
      </View>

      <View style={styles.tabRow}>
        {(['scan', 'generate'] as Tab[]).map((t) => (
          <TouchableOpacity
            key={t}
            style={[styles.tab, tab === t && styles.tabActive]}
            onPress={() => { setTab(t); setScanned(''); }}
          >
            <Text style={[styles.tabText, tab === t && styles.tabTextActive]}>{t.toUpperCase()}</Text>
          </TouchableOpacity>
        ))}
      </View>

      {tab === 'scan' ? (
        <View style={styles.body}>
          {!perm?.granted ? (
            <View style={styles.card}>
              <Text style={styles.cardTitle}>CAMERA PERMISSION</Text>
              <Text style={styles.cardBody}>Grant camera access to scan QR codes.</Text>
              <TouchableOpacity style={styles.btn} onPress={() => requestPerm()}>
                <Text style={styles.btnText}>ALLOW CAMERA</Text>
              </TouchableOpacity>
            </View>
          ) : (
            <View style={styles.cameraWrap}>
              <CameraView style={StyleSheet.absoluteFill} facing="back" onBarcodeScanned={scanned ? undefined : onScan} />
              <View style={styles.scanTarget} />
              <Text style={styles.scanHint}>Point camera at a QR code</Text>
            </View>
          )}
          {scanned !== '' && (
            <View style={styles.card}>
              <Text style={styles.cardTitle}>SCANNED</Text>
              <Text style={styles.scannedText} selectable>{scanned}</Text>
              <TouchableOpacity style={styles.btn} onPress={() => setScanned('')}>
                <Text style={styles.btnText}>SCAN AGAIN</Text>
              </TouchableOpacity>
            </View>
          )}
        </View>
      ) : (
        <View style={styles.body}>
          <View style={styles.card}>
            <Text style={styles.cardTitle}>VALUE</Text>
            <TextInput
              style={styles.input}
              value={value}
              onChangeText={setValue}
              placeholder="Enter text or URL"
              placeholderTextColor={Colors.textMuted}
            />
          </View>
          <View style={styles.qrCenter}>
            <View style={styles.qrBox}>
              <QRCode value={value || ' '} size={220} color={Colors.textPrimary} backgroundColor="#ffffff" />
            </View>
            <Text style={styles.cardBody}>Scan this with another device</Text>
          </View>
        </View>
      )}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.background },
  header: { backgroundColor: Colors.chrome, paddingHorizontal: Spacing.lg, paddingVertical: Spacing.md },
  headerTitle: { color: Colors.textOnChrome, fontSize: 18, fontWeight: '900', letterSpacing: 2, fontFamily: Font.mono },
  tabRow: { flexDirection: 'row', backgroundColor: Colors.surface, borderBottomWidth: 1, borderBottomColor: Colors.borderLight },
  tab: { flex: 1, paddingVertical: Spacing.md, alignItems: 'center' },
  tabActive: { borderBottomWidth: 2, borderBottomColor: Colors.primary },
  tabText: { fontSize: 11, fontWeight: '700', color: Colors.textSecondary, letterSpacing: 1.5, fontFamily: Font.mono },
  tabTextActive: { color: Colors.primary, fontWeight: '800' },
  body: { flex: 1, padding: Spacing.lg, gap: Spacing.md },
  card: { backgroundColor: Colors.surface, borderRadius: Radius.md, padding: Spacing.lg, borderWidth: 1, borderColor: Colors.borderLight },
  cardTitle: { fontSize: 12, fontWeight: '800', color: Colors.textPrimary, letterSpacing: 1.5, fontFamily: Font.mono, marginBottom: 6 },
  cardBody: { fontSize: 12, color: Colors.textSecondary, lineHeight: 18 },
  btn: { backgroundColor: Colors.primary, paddingVertical: 12, borderRadius: Radius.sm, alignItems: 'center', marginTop: Spacing.md },
  btnText: { color: Colors.textOnPrimary, fontSize: 11, fontWeight: '800', letterSpacing: 1.5, fontFamily: Font.mono },
  cameraWrap: { height: 320, borderRadius: Radius.md, overflow: 'hidden', backgroundColor: Colors.chrome, justifyContent: 'center', alignItems: 'center' },
  scanTarget: { width: 200, height: 200, borderWidth: 2, borderColor: Colors.primary, borderRadius: Radius.sm },
  scanHint: { color: '#ffffff', fontSize: 11, fontWeight: '800', letterSpacing: 1, marginTop: 12, fontFamily: Font.mono },
  scannedText: { fontSize: 12, color: Colors.textPrimary, fontFamily: Font.mono, flexWrap: 'wrap' },
  input: { backgroundColor: Colors.surfaceSecondary, borderWidth: 1, borderColor: Colors.border, borderRadius: Radius.sm, padding: 10, fontSize: 13, color: Colors.textPrimary, fontFamily: Font.mono },
  qrCenter: { alignItems: 'center', gap: Spacing.md, marginTop: Spacing.md },
  qrBox: { padding: 16, backgroundColor: '#ffffff', borderRadius: Radius.md, borderWidth: 1, borderColor: Colors.borderLight },
});
