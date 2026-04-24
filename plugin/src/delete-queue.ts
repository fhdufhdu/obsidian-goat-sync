import { AsyncMutex } from "./async-mutex";

export interface QueueFileAdapter {
  read(path: string): Promise<string>;
  write(path: string, data: string): Promise<void>;
  exists(path: string): Promise<boolean>;
  remove(path: string): Promise<void>;
  rename(from: string, to: string): Promise<void>;
}

export interface DeleteEntry {
  path: string;
  baseVersion: number;
  serverHash: string;
  queuedAt: number;
  status: "pending" | "retryableFailed";
}

export class DeleteQueue {
  private entries = new Map<string, DeleteEntry>();
  private mutex = new AsyncMutex();
  private tempPath: string;

  constructor(private adapter: QueueFileAdapter, private queuePath: string) {
    this.tempPath = queuePath.replace(/\.json$/, ".tmp");
  }

  async load(): Promise<void> {
    await this.mutex.runExclusive(async () => {
      if (await this.adapter.exists(this.tempPath)) {
        await this.adapter.remove(this.tempPath);
      }

      if (!(await this.adapter.exists(this.queuePath))) return;
      const raw = await this.adapter.read(this.queuePath);
      const parsed = JSON.parse(raw) as DeleteEntry[];
      this.entries = new Map(parsed.map((entry) => [entry.path, entry]));
    });
  }

  async enqueue(input: { path: string; baseVersion: number; serverHash: string }): Promise<void> {
    await this.mutex.runExclusive(async () => {
      const existing = this.entries.get(input.path);
      if (existing) {
        existing.queuedAt = Date.now();
      } else {
        this.entries.set(input.path, {
          ...input,
          queuedAt: Date.now(),
          status: "pending",
        });
      }

      await this.saveLocked();
    });
  }

  async remove(path: string): Promise<void> {
    await this.mutex.runExclusive(async () => {
      this.entries.delete(path);
      await this.saveLocked();
    });
  }

  list(): DeleteEntry[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }

  private async saveLocked(): Promise<void> {
    const data = JSON.stringify(this.list(), null, 2);
    await this.adapter.write(this.tempPath, data);
    await this.adapter.rename(this.tempPath, this.queuePath);
  }
}
