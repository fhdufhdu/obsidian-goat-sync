import { AsyncMutex } from "./async-mutex";

export type FlushResult = "ok" | "transientFailure";

export interface SyncOrchestratorHandlers {
  flushDeleteQueue(): Promise<FlushResult>;
  flushDirtyQueue(): Promise<FlushResult>;
  runSyncInit(): Promise<void>;
  notifyTransientFailure(): void;
}

export class SyncOrchestrator {
  private mutex = new AsyncMutex();
  private running = false;

  constructor(private handlers: SyncOrchestratorHandlers) {}

  async runStartupSync(): Promise<void> {
    await this.mutex.runExclusive(async () => {
      const deleteResult = await this.handlers.flushDeleteQueue();
      if (deleteResult === "transientFailure") {
        this.handlers.notifyTransientFailure();
        return;
      }

      const dirtyResult = await this.handlers.flushDirtyQueue();
      if (dirtyResult === "transientFailure") {
        this.handlers.notifyTransientFailure();
        return;
      }

      await this.handlers.runSyncInit();
    });
  }

  async runIntervalWorker(): Promise<void> {
    if (this.running) return;
    this.running = true;
    try {
      await this.runStartupSync();
    } finally {
      this.running = false;
    }
  }
}
