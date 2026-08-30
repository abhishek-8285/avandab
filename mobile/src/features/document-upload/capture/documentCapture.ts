import * as ImagePicker from 'expo-image-picker';
import * as ImageManipulator from 'expo-image-manipulator';
import * as FileSystem from 'expo-file-system';

export interface CapturedDocument {
  uri: string;
  fileSizeBytes: number;
  mimeType: string;
  width: number;
  height: number;
}

export async function captureDocumentFromCamera(): Promise<CapturedDocument | null> {
  const permission = await ImagePicker.requestCameraPermissionsAsync();
  if (!permission.granted) {
    throw new Error('Camera permission is required to capture documents.');
  }

  const result = await ImagePicker.launchCameraAsync({
    mediaTypes: ['images'],
    allowsEditing: true,
    quality: 0.8,
  });

  if (result.canceled || !result.assets || result.assets.length === 0) {
    return null;
  }

  const asset = result.assets[0];
  return processAndCompressImage(asset.uri, asset.width, asset.height);
}

export async function pickDocumentFromGallery(): Promise<CapturedDocument | null> {
  const permission = await ImagePicker.requestMediaLibraryPermissionsAsync();
  if (!permission.granted) {
    throw new Error('Photo library permission is required to select documents.');
  }

  const result = await ImagePicker.launchImageLibraryAsync({
    mediaTypes: ['images'],
    allowsEditing: true,
    quality: 0.8,
  });

  if (result.canceled || !result.assets || result.assets.length === 0) {
    return null;
  }

  const asset = result.assets[0];
  return processAndCompressImage(asset.uri, asset.width, asset.height);
}

async function processAndCompressImage(
  rawUri: string,
  origWidth: number,
  origHeight: number
): Promise<CapturedDocument> {
  // Normalize and compress (max width 1920, JPEG compression 0.8)
  const targetWidth = Math.min(origWidth || 1920, 1920);
  const manipResult = await ImageManipulator.manipulateAsync(
    rawUri,
    [{ resize: { width: targetWidth } }],
    { compress: 0.8, format: ImageManipulator.SaveFormat.JPEG }
  );

  const fileInfo = await FileSystem.getInfoAsync(manipResult.uri);
  const size = (fileInfo as any).size || 1024;

  return {
    uri: manipResult.uri,
    fileSizeBytes: size,
    mimeType: 'image/jpeg',
    width: manipResult.width,
    height: manipResult.height,
  };
}
