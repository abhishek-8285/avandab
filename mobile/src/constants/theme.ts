// Avandab Driver Pro - WhatsApp Style Theme tokens for Indian Commercial Drivers.

export const Colors = {
  // WhatsApp Signature Palette
  primary: '#008069',          // WhatsApp Deep Green
  primaryDark: '#075e54',      // Classic WhatsApp Header Dark Green
  primaryLight: '#dcf8c6',     // WhatsApp Message Bubble Light Green
  primarySubtle: '#e7ffdb',
  accent: '#25d366',           // WhatsApp Vibrant Green Accent
  whatsappAccent: '#25d366',   // WhatsApp Vibrant Green Floating Action
  primaryBorder: '#bbf7d0',    // Soft green border (demo/outline surfaces)
  successDark: '#059669',      // Deep green (map pins, earned states)

  // Header & Dark chrome
  chrome: '#075e54',           // WhatsApp Header Teal
  chromeDark: '#004c3f',       // Deep chrome (nav/route headers)
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
  warningText: '#b45309',      // Readable amber text on warningBg
  danger: '#ea0038',
  dangerBg: '#fee2e2',
  info: '#0284c7',
  infoBg: '#e0f2fe',
  infoBorder: '#bae6fd',       // Info card border
  skeleton: '#f1f5f9',         // Loading placeholder blocks

  // Borders
  border: '#e9edef',           // Subtle WhatsApp separator
  borderLight: '#f0f2f5',
  borderStrong: '#cbd5e1',     // High-contrast divider (route lines, chip borders)

  // Emergency / modal dark surfaces (SOS confirm sheet)
  modalBg: '#1E293B',
  modalBorder: '#334155',
  modalTitle: '#F8FAFC',
  modalText: '#E2E8F0',
  modalSub: '#94A3B8',
  overlayDim: 'rgba(0, 0, 0, 0.65)',
  dangerGlow: 'rgba(239, 68, 68, 0.15)',
  dangerSoft: '#fca5a5',       // Light red text on dark-red surfaces

  // Map surfaces
  mapDark: '#0f172a',          // Dark map chrome / labels

  // Onboarding dark-cyan theme (driver onboarding flow)
  onboardingBg: '#083344',
  onboardingBorder: '#06b6d4',
  onboardingText: '#38bdf8',
  onboardingBtn: '#0d9488',
  onboardingOkBg: '#064e3b',
  onboardingOkBorder: '#10b981',
  onboardingErrBg: '#450a0a',
  onboardingInk: '#090d16',

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

// Type scale — every text size in the app comes from here.
// Values preserve the exact sizes the design shipped with.
export const FontSize = {
  caption: 9, // tiny meta, badges sub-text
  small: 10, // chips, pills, eyebrows
  label: 11, // buttons, tab labels
  body: 12, // default reading text
  bodyLarge: 13, // inputs, emphasized body
  title: 14, // card titles, modal text
  titleLarge: 15, // large card titles
  heading: 16, // screen headings
  section: 17, // large section titles
  headingLarge: 18, // feature headers
  banner: 20, // modal titles, amounts
  hero: 22, // brand, header titles
  display: 24, // onboarding headers
  displayLarge: 26, // splash headline
  jumbo: 34, // wallet balance hero
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
