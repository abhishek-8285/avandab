import { getApiBaseURL, getBackendHost } from '../constants/network';
import { useAuthStore } from '../stores/authStore';

export interface ChatMessage {
  id: string;
  from: 'driver' | 'dispatch';
  text: string;
  at: string;
}

export interface ChatProvider {
  connect(tripId: string): void;
  disconnect(): void;
  send(tripId: string, text: string): Promise<void>;
  onMessage(cb: (msg: ChatMessage) => void): () => void;
}

export class NoOpChatProvider implements ChatProvider {
  connect() {}
  disconnect() {}
  async send() {
    throw new Error('CHAT_PROVIDER_UNAVAILABLE');
  }
  onMessage() {
    return () => {};
  }
}

export class HttpChatProvider implements ChatProvider {
  private cb: ((msg: ChatMessage) => void) | null = null;
  private pollId: ReturnType<typeof setInterval> | null = null;
  private tripId: string | null = null;

  connect(tripId: string) {
    this.tripId = tripId;
    this.poll();
    this.pollId = setInterval(() => this.poll(), 8000);
  }

  disconnect() {
    if (this.pollId) clearInterval(this.pollId);
    this.pollId = null;
    this.tripId = null;
  }

  async send(tripId: string, text: string) {
    const token = useAuthStore.getState().token;
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/issues`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      body: JSON.stringify({ trip_id: tripId, category: 'other', severity: 'medium', message: text }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }

  onMessage(cb: (msg: ChatMessage) => void) {
    this.cb = cb;
    return () => {
      this.cb = null;
    };
  }

  private async poll() {
    if (!this.tripId || !this.cb) return;
    // Best-effort: reuse issues as chat history fallback
    try {
      const token = useAuthStore.getState().token;
      const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/issues?trip_id=${this.tripId}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!res.ok) return;
      const j = await res.json();
      const issues: any[] = j.issues ?? j.data ?? [];
      issues.slice(0, 3).forEach((it) => {
        this.cb?.({ id: it.id, from: 'dispatch', text: it.message || it.title || '', at: it.created_at || new Date().toISOString() });
      });
    } catch {}
  }
}

export const Chat = new HttpChatProvider();
