import { describe, expect, it } from "vitest";
import { SyncOrchestrator } from "../sync-orchestrator";

describe("SyncOrchestrator", () => {
  it("runs delete, dirty, then syncInit under one mutex", async () => {
    const calls: string[] = [];
    const orchestrator = new SyncOrchestrator({
      flushDeleteQueue: async () => {
        calls.push("delete");
        return "ok";
      },
      flushDirtyQueue: async () => {
        calls.push("dirty");
        return "ok";
      },
      runSyncInit: async () => {
        calls.push("syncInit");
      },
      notifyTransientFailure: () => calls.push("notice"),
    });

    await orchestrator.runStartupSync();
    expect(calls).toEqual(["delete", "dirty", "syncInit"]);
  });

  it("skips syncInit after transient failure", async () => {
    const calls: string[] = [];
    const orchestrator = new SyncOrchestrator({
      flushDeleteQueue: async () => {
        calls.push("delete");
        return "transientFailure";
      },
      flushDirtyQueue: async () => {
        calls.push("dirty");
        return "ok";
      },
      runSyncInit: async () => {
        calls.push("syncInit");
      },
      notifyTransientFailure: () => calls.push("notice"),
    });

    await orchestrator.runStartupSync();
    expect(calls).toEqual(["delete", "notice"]);
  });

  it("runs again when work is queued during an active interval worker", async () => {
    const calls: string[] = [];
    let releaseFirstSync!: () => void;
    const firstSyncBlocked = new Promise<void>((release) => {
      releaseFirstSync = release;
    });
    let resolveFirstSyncStarted!: () => void;
    const firstSyncStarted = new Promise<void>((resolve) => {
      resolveFirstSyncStarted = resolve;
    });
    const orchestrator = new SyncOrchestrator({
      flushDeleteQueue: async () => {
        calls.push("delete");
        return "ok";
      },
      flushDirtyQueue: async () => {
        calls.push("dirty");
        return "ok";
      },
      runSyncInit: async () => {
        calls.push("syncInit");
        resolveFirstSyncStarted();
        if (calls.length === 3) {
          await firstSyncBlocked;
        }
      },
      notifyTransientFailure: () => calls.push("notice"),
    });

    const firstRun = orchestrator.runIntervalWorker();
    await firstSyncStarted;
    await orchestrator.runIntervalWorker();
    releaseFirstSync();
    await firstRun;

    expect(calls).toEqual(["delete", "dirty", "syncInit", "delete", "dirty", "syncInit"]);
  });
});
