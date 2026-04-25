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
  private rerunRequested = false;

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
    if (this.running) {
      this.rerunRequested = true;
      return;
    }

    this.running = true;
    try {
      do {
        this.rerunRequested = false;
        await this.runStartupSync();
      } while (this.rerunRequested);
    } finally {
      this.running = false;
    }
  }
}
