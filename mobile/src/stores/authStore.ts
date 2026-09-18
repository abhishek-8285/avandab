import { create } from 'zustand';
import * as SecureStore from 'expo-secure-store';

export interface UserSession {
  id: string;          // backend user_id (flat)
  name: string;
  role: string;        // client concept: 'driver' | 'dispatcher' | 'admin' | 'viewer'
  email: string;
  driverId?: string;   // from GET /api/v1/drivers/me
}

interface AuthState {
  token: string | null;
  user: UserSession | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  setAuth: (token: string, user: UserSession) => Promise<void>;
  setDriverId: (driverId: string) => void;
  logout: () => Promise<void>;
  loadSession: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  isAuthenticated: false,
  isLoading: true,

  setAuth: async (token: string, user: UserSession) => {
    await SecureStore.setItemAsync('auth_token', token);
    await SecureStore.setItemAsync('auth_user', JSON.stringify(user));
    set({ token, user, isAuthenticated: true, isLoading: false });
  },

  setDriverId: (driverId: string) => {
    const { user } = get();
    if (user) {
      const updated = { ...user, driverId };
      SecureStore.setItemAsync('auth_user', JSON.stringify(updated));
      set({ user: updated });
    }
  },

  logout: async () => {
    await SecureStore.deleteItemAsync('auth_token');
    await SecureStore.deleteItemAsync('auth_user');
    set({ token: null, user: null, isAuthenticated: false, isLoading: false });
  },

  loadSession: async () => {
    try {
      const token = await SecureStore.getItemAsync('auth_token');
      const userJson = await SecureStore.getItemAsync('auth_user');
      if (token && userJson) {
        const user = JSON.parse(userJson) as UserSession;
        set({ token, user, isAuthenticated: true, isLoading: false });
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
