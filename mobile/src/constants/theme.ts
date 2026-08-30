// Avandab Driver Pro - WhatsApp Style Theme tokens for Indian Commercial Drivers.

export const Colors = {
  // WhatsApp Signature Palette
  primary: '#008069',          // WhatsApp Deep Green
  primaryDark: '#075e54',      // Classic WhatsApp Header Dark Green
  primaryLight: '#dcf8c6',     // WhatsApp Message Bubble Light Green
  primarySubtle: '#e7ffdb',
  accent: '#25d366',           // WhatsApp Vibrant Green Accent
  whatsappAccent: '#25d366',   // WhatsApp Vibrant Green Floating Action

  // Header & Dark chrome
  chrome: '#075e54',           // WhatsApp Header Teal
  chromeLight: '#128c7e',      // WhatsApp Tab Bar Active
  chromeBorder: '#0b4e46',

  // Backgrounds & surfaces
  background: '#efeae2',       // Classic WhatsApp soft background
  surface: '#ffffff',
  surfaceSecondary: '#f0f2f5', // WhatsApp List Item Hover/Secondary
  bubbleGreen: '#dcf8c6',
  bubbleWhite: '#ffffff',

  // Text
  textPrimary: '#111b21',      // WhatsApp Primary Text (Very sharp dark)
  textSecondary: '#667781',    // WhatsApp Secondary / Timestamp Grey
  textMuted: '#8696a0',        // WhatsApp Muted Text
  textOnPrimary: '#ffffff',
  textOnChrome: '#ffffff',
  textOnChromeMuted: '#e9edef',

  // Status
  success: '#00a884',
  successBg: '#dcf8c6',
  warning: '#f59e0b',
  warningBg: '#fef3c7',
  danger: '#ea0038',
  dangerBg: '#fee2e2',
  info: '#0284c7',
  infoBg: '#e0f2fe',

  // Borders
  border: '#e9edef',           // Subtle WhatsApp separator
  borderLight: '#f0f2f5',

  // Shell & Navigation aliases
  headerBg: '#075e54',
  headerBorder: '#054c44',
  tabActive: '#ffffff',
  tabInactive: 'rgba(255, 255, 255, 0.75)',
};

export const Font = {
  mono: 'monospace',
  sans: 'system-ui',
};

export const Radius = {
  none: 0,
  sm: 4,
  md: 8,
  lg: 12,
  xl: 16,
  full: 9999,
};

export const Spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  xxl: 24,
};

export const Shadows = {
  card: {
    shadowColor: '#111b21',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.08,
    shadowRadius: 3,
    elevation: 2,
  },
  modal: {
    shadowColor: '#111b21',
    shadowOffset: { width: 0, height: 6 },
    shadowOpacity: 0.25,
    shadowRadius: 12,
    elevation: 8,
  },
  fab: {
    shadowColor: '#111b21',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.3,
    shadowRadius: 6,
    elevation: 6,
  },
};
