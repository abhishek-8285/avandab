import * as Speech from 'expo-speech';
import { useLanguageStore } from '../stores/languageStore';

class VoiceAnnouncementService {
  private enabled = true;

  setEnabled(val: boolean) {
    this.enabled = val;
  }

  isEnabled() {
    return this.enabled;
  }

  async announceDispatch(params: {
    tripNumber: string;
    origin: string;
    destination: string;
    advanceAmount?: number;
    locale?: string;
  }): Promise<void> {
    if (!this.enabled) return;

    try {
      const activeLocale = params.locale || useLanguageStore.getState().locale || 'hi';
      const advance = params.advanceAmount || 5000;
      let text = '';
      let langCode = 'hi-IN';

      if (activeLocale === 'hi') {
        text = `नया ट्रिप असाइन हुआ। ${params.origin} से ${params.destination}। ड्राइवर एडवांस ₹${advance}।`;
        langCode = 'hi-IN';
      } else if (activeLocale === 'mr') {
        text = `नवीन ट्रिप लोड प्राप्त झाले। ${params.origin} ते ${params.destination}। ऍडव्हान्स ₹${advance}।`;
        langCode = 'mr-IN';
      } else {
        text = `New trip assigned. From ${params.origin} to ${params.destination}. Advance ${advance} rupees.`;
        langCode = 'en-IN';
      }

      await Speech.stop();
      Speech.speak(text, {
        language: langCode,
        pitch: 1.0,
        rate: 0.95,
      });
    } catch (e: any) {
      console.log('[VOICE ANNOUNCEMENT ERROR]', e?.message);
    }
  }

  async announceSOS(locale?: string): Promise<void> {
    if (!this.enabled) return;

    try {
      const activeLocale = locale || useLanguageStore.getState().locale || 'hi';
      let text = 'इमरजेंसी अलार्म भेज दिया गया है। कंट्रोल रूम से संपर्क किया जा रहा है।';
      let langCode = 'hi-IN';

      if (activeLocale === 'mr') {
        text = 'आपत्कालीन अलार्म पाठवला गेला आहे. कंट्रोल रूम अलर्ट केली आहे.';
        langCode = 'mr-IN';
      } else if (activeLocale === 'en') {
        text = 'Emergency SOS sent. Control room alerted.';
        langCode = 'en-IN';
      }

      await Speech.stop();
      Speech.speak(text, {
        language: langCode,
        pitch: 1.05,
        rate: 1.0,
      });
    } catch (e: any) {
      console.log('[VOICE SOS ERROR]', e?.message);
    }
  }

  async stop(): Promise<void> {
    try {
      await Speech.stop();
    } catch {}
  }
}

export const VoiceAnnouncement = new VoiceAnnouncementService();
