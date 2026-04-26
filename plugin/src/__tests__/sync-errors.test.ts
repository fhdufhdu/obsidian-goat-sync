import { describe, expect, it } from "vitest";
const notices: string[] = [];
(globalThis as unknown as { __obsidianGoatSyncNotices: string[] }).__obsidianGoatSyncNotices = notices;

async function makeManager() {
  const { SyncManager } = await import("../sync");
  return Object.create(SyncManager.prototype) as {
    handleServerError: (msg: { type: string; path?: string; error?: string }) => boolean;
    handleFilePutResult: (msg: { type: string; path?: string; error?: string }) => Promise<void>;
  };
}

describe("sync error notices", () => {
  it("shows generic server errors", async () => {
    notices.length = 0;
    const manager = await makeManager();
    manager.handleServerError({ type: "error", error: "boom" });

    expect(notices).toEqual(["[obsidian-goat-sync] error failed: boom"]);
  });

  it("shows result errors with path before action handling", async () => {
    notices.length = 0;
    const manager = await makeManager();

    await manager.handleFilePutResult({ type: "filePutResult", path: "notes/a.md", error: "FOREIGN KEY constraint failed" });

    expect(notices).toEqual([
      "[obsidian-goat-sync] filePutResult failed for notes/a.md: FOREIGN KEY constraint failed",
    ]);
  });

});
