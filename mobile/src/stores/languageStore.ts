import { create } from 'zustand';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { SupportedLocale, setLocale, getLocale } from '../i18n';

interface LanguageState {
  locale: SupportedLocale;
  setLanguage: (lang: SupportedLocale) => Promise<void>;
  loadLanguage: () => Promise<void>;
}

export const useLanguageStore = create<LanguageState>((set) => ({
  locale: getLocale(),

  setLanguage: async (lang: SupportedLocale) => {
    await setLocale(lang);
    set({ locale: lang });
  },

  loadLanguage: async () => {
    try {
      const saved = await AsyncStorage.getItem('@avandab_language');
      if (saved) {
        await setLocale(saved as SupportedLocale);
        set({ locale: saved as SupportedLocale });
      }
    } catch {
      // fallback
    }
  },
}));
