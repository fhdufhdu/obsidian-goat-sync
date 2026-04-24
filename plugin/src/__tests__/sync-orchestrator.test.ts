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
});
