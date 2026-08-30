import AsyncStorage from '@react-native-async-storage/async-storage';
import { generateCommandId } from './idempotency';

export type CommandState = 'PENDING' | 'SYNCING' | 'SYNCED' | 'FAILED';

export interface QueuedCommand {
  commandId: string;
  type: string;
  payload: Record<string, any>;
  state: CommandState;
  retryCount: number;
  maxRetries: number;
  errorMessage?: string;
  response?: Record<string, any>;
  createdAt: number;
  updatedAt: number;
}

const COMMAND_QUEUE_STORAGE_KEY = 'avandab_offline_command_queue_v1';

export class CommandQueue {
  private memoryStore: QueuedCommand[] = [];

  async getCommands(): Promise<QueuedCommand[]> {
    try {
      const raw = await AsyncStorage.getItem(COMMAND_QUEUE_STORAGE_KEY);
      if (raw) {
        return JSON.parse(raw);
      }
      return this.memoryStore;
    } catch {
      return this.memoryStore;
    }
  }

  async saveCommands(commands: QueuedCommand[]): Promise<void> {
    this.memoryStore = [...commands];
    try {
      await AsyncStorage.setItem(COMMAND_QUEUE_STORAGE_KEY, JSON.stringify(commands));
    } catch {}
  }

  async enqueueCommand(type: string, payload: Record<string, any>, customCommandId?: string): Promise<QueuedCommand> {
    const commandId = customCommandId || payload.command_id || generateCommandId(type.toLowerCase());
    payload.command_id = commandId;
    const cmd: QueuedCommand = {
      commandId,
      type,
      payload,
      state: 'PENDING',
      retryCount: 0,
      maxRetries: 5,
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    const list = await this.getCommands();
    list.push(cmd);
    await this.saveCommands(list);
    return cmd;
  }

  async getPendingCommands(): Promise<QueuedCommand[]> {
    const list = await this.getCommands();
    return list.filter((c) => c.state === 'PENDING' || c.state === 'SYNCING');
  }

  async updateCommandState(
    commandId: string,
    state: CommandState,
    opts?: { errorMessage?: string; response?: Record<string, any> }
  ): Promise<void> {
    const list = await this.getCommands();
    const cmd = list.find((c) => c.commandId === commandId);
    if (!cmd) return;

    cmd.state = state;
    cmd.updatedAt = Date.now();
    if (opts?.errorMessage) cmd.errorMessage = opts.errorMessage;
    if (opts?.response) cmd.response = opts.response;
    if (state === 'FAILED') cmd.retryCount += 1;

    await this.saveCommands(list);
  }

  async getPendingCount(): Promise<number> {
    const pending = await this.getPendingCommands();
    return pending.length;
  }
}

export const commandQueue = new CommandQueue();
