import { Chat, HttpChatProvider, NoOpChatProvider } from '../src/services/chat';

const globalFetch = global.fetch;
const globalSetInterval = global.setInterval;
const globalClearInterval = global.clearInterval;

describe('chat providers', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    global.fetch = globalFetch;
    global.setInterval = globalSetInterval;
    global.clearInterval = globalClearInterval;
  });

  test('NoOp provider is inert', async () => {
    const p = new NoOpChatProvider();
    expect(() => p.connect()).not.toThrow();
    expect(() => p.disconnect()).not.toThrow();
    await expect(p.send()).rejects.toThrow('CHAT_PROVIDER_UNAVAILABLE');
    const off = p.onMessage();
    expect(() => off()).not.toThrow();
  });

  test('Http provider polls issues as chat fallback and sends', async () => {
    const seen: any[] = [];
    global.fetch = jest.fn().mockImplementation(async (url: string, opts: any) => {
      if (opts?.method === 'POST') return { ok: true, status: 200, json: async () => ({}) };
      return {
        ok: true,
        status: 200,
        json: async () => ({ issues: [{ id: 'm1', message: 'hello driver', created_at: '2026-01-01' }] }),
      };
    }) as any;
    const p = new HttpChatProvider();
    const off = p.onMessage((m) => seen.push(m));
    p.connect('trip-1');
    await jest.advanceTimersByTimeAsync(50);
    expect(seen).toHaveLength(1);
    expect(seen[0].text).toBe('hello driver');
    await p.send('trip-1', 'copy that');
    expect((global.fetch as jest.Mock).mock.calls.some((c) => c[1]?.method === 'POST')).toBe(true);
    p.disconnect();
    off();
  });

  test('Http provider send throws on HTTP error', async () => {
    global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 500 }) as any;
    await expect(new HttpChatProvider().send('t', 'x')).rejects.toThrow('HTTP 500');
  });

  test('default Chat export is an Http provider', () => {
    expect(Chat).toBeInstanceOf(HttpChatProvider);
  });
});
