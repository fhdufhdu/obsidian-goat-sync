import { AsyncMutex } from "./async-mutex";

export type DirtyStatus = "pending" | "inFlight" | "retryableFailed";

export interface DirtyEntry {
  path: string;
  baseVersion?: number;
  queuedAt: number;
  lastSeenHash: string;
  status: DirtyStatus;
  sentHash?: string;
}

export interface DirtySnapshot {
  path: string;
  baseVersion?: number;
  lastSeenHash: string;
}

export interface ServerMeta {
  serverVersion: number;
  serverHash: string;
}

export class DirtyQueue {
  private entries = new Map<string, DirtyEntry>();
  private mutex = new AsyncMutex();

  async enqueue(input: { path: string; baseVersion?: number; lastSeenHash: string }): Promise<void> {
    await this.mutex.runExclusive(() => {
      const existing = this.entries.get(input.path);
      if (existing) {
        existing.lastSeenHash = input.lastSeenHash;
        existing.queuedAt = Date.now();
        return;
      }
      this.entries.set(input.path, {
        path: input.path,
        baseVersion: input.baseVersion,
        queuedAt: Date.now(),
        lastSeenHash: input.lastSeenHash,
        status: "pending",
      });
    });
  }

  async claimNext(): Promise<DirtySnapshot | null> {
    return await this.mutex.runExclusive(() => {
      for (const entry of this.entries.values()) {
        if (entry.status === "pending" || entry.status === "retryableFailed") {
          entry.status = "inFlight";
          return {
            path: entry.path,
            baseVersion: entry.baseVersion,
            lastSeenHash: entry.lastSeenHash,
          };
        }
      }
      return null;
    });
  }

  async claimNextExcluding(excludedPaths: Set<string>): Promise<DirtySnapshot | null> {
    return await this.mutex.runExclusive(() => {
      for (const entry of this.entries.values()) {
        if (excludedPaths.has(entry.path)) continue;
        if (entry.status === "pending" || entry.status === "retryableFailed") {
          entry.status = "inFlight";
          return {
            path: entry.path,
            baseVersion: entry.baseVersion,
            lastSeenHash: entry.lastSeenHash,
          };
        }
      }
      return null;
    });
  }

  async markSentHash(path: string, claimHash: string, sentHash: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      entry.sentHash = sentHash;
      if (entry.lastSeenHash === claimHash) {
        entry.lastSeenHash = sentHash;
      }
    });
  }

  async completeSuccess(path: string, sentHash: string, meta: ServerMeta): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      if (entry.lastSeenHash === sentHash) {
        this.entries.delete(path);
        return;
      }
      entry.baseVersion = meta.serverVersion;
      entry.status = "pending";
      entry.sentHash = undefined;
    });
  }

  async completeMergeSuccess(path: string, sentHash: string, meta: ServerMeta): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      if (entry.lastSeenHash === sentHash) {
        this.entries.delete(path);
        return;
      }
      entry.baseVersion = meta.serverVersion;
      entry.status = "pending";
      entry.sentHash = undefined;
    });
  }

  async completeRetryableFailure(path: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      entry.status = "retryableFailed";
      entry.sentHash = undefined;
    });
  }

  async release(path: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      const entry = this.entries.get(path);
      if (!entry) return;
      entry.status = "pending";
      entry.sentHash = undefined;
    });
  }

  async remove(path: string): Promise<void> {
    await this.mutex.runExclusive(() => {
      this.entries.delete(path);
    });
  }

  get(path: string): DirtyEntry | undefined {
    const entry = this.entries.get(path);
    return entry ? { ...entry } : undefined;
  }

  list(): DirtyEntry[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }
}
