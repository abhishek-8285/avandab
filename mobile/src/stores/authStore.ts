import { create } from 'zustand';
import * as SecureStore from 'expo-secure-store';

export interface UserSession {
  id: string;          // backend user_id (flat)
  name: string;
  role: string;        // client concept: 'driver' | 'dispatcher' | 'admin' | 'viewer'
  email: string;
  driverId?: string;   // from GET /api/v1/drivers/me
}

export type AppMode = 'driver' | 'manager';

function defaultModeForRole(role?: string): AppMode {
  const r = (role || '').toLowerCase();
  if (r === 'admin' || r === 'dispatcher' || r === 'manager' || r === 'owner') {
    return 'manager';
  }
  return 'driver';
}

interface AuthState {
  token: string | null;
  user: UserSession | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  activeMode: AppMode;
  setAuth: (token: string, user: UserSession) => Promise<void>;
  setDriverId: (driverId: string) => void;
  setMode: (mode: AppMode) => void;
  toggleMode: () => void;
  logout: () => Promise<void>;
  loadSession: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  isAuthenticated: false,
  isLoading: true,
  activeMode: 'driver',

  setAuth: async (token: string, user: UserSession) => {
    await SecureStore.setItemAsync('auth_token', token);
    await SecureStore.setItemAsync('auth_user', JSON.stringify(user));
    const mode = defaultModeForRole(user.role);
    set({ token, user, isAuthenticated: true, isLoading: false, activeMode: mode });
  },

  setDriverId: (driverId: string) => {
    const { user } = get();
    if (user) {
      const updated = { ...user, driverId };
      SecureStore.setItemAsync('auth_user', JSON.stringify(updated));
      set({ user: updated });
    }
  },

  setMode: (mode: AppMode) => {
    set({ activeMode: mode });
  },

  toggleMode: () => {
    const next = get().activeMode === 'manager' ? 'driver' : 'manager';
    set({ activeMode: next });
  },

  logout: async () => {
    await SecureStore.deleteItemAsync('auth_token');
    await SecureStore.deleteItemAsync('auth_user');
    set({ token: null, user: null, isAuthenticated: false, isLoading: false, activeMode: 'driver' });
  },

  loadSession: async () => {
    try {
      const token = await SecureStore.getItemAsync('auth_token');
      const userJson = await SecureStore.getItemAsync('auth_user');
      if (token && userJson) {
        const user = JSON.parse(userJson) as UserSession;
        const mode = defaultModeForRole(user.role);
        set({ token, user, isAuthenticated: true, isLoading: false, activeMode: mode });
      } else {
        set({ isLoading: false });
      }
    } catch {
      set({ isLoading: false });
    }
  },
}));

// Stable account identity for partitioning offline cache/queues by owner.
// user.id is the backend user_id (flat); driverId is a mutable profile field.
// Prefer id so a driverId refresh never orphans queued work.
export function currentAccountId(): string | null {
  try {
    const u = useAuthStore.getState().user;
    return u?.id ?? u?.driverId ?? null;
  } catch {
    return null;
  }
}

export function currentDriverId(): string | null {
  try {
    const u = useAuthStore.getState().user;
    return u?.driverId ?? u?.id ?? null;
  } catch {
    return null;
  }
}
