import { describe, expect, test, vi } from "vitest";
import { SyncManager } from "../sync";
import { FileMetaStore } from "../file-meta-store";
import { DirtyQueue } from "../dirty-queue";
import { sha256 } from "../hash";

function createAdapter(initial: Record<string, string>) {
  const files = new Map(Object.entries(initial));
  return {
    async exists(path: string) { return files.has(path); },
    async read(path: string) { return files.get(path) || ""; },
    async write(path: string, data: string) { files.set(path, data); },
    async readBinary(path: string) { return new TextEncoder().encode(files.get(path) || "").buffer; },
    async writeBinary(path: string, data: ArrayBuffer) { files.set(path, new TextDecoder().decode(data)); },
    async mkdir(_path: string) {},
    async remove(path: string) { files.delete(path); },
    async rename(from: string, to: string) { files.set(to, files.get(from) || ""); files.delete(from); },
  };
}

async function createSyncManagerHarness(input: {
  files: Record<string, string>;
  meta: Record<string, { prevServerVersion: number; prevServerHash: string }>;
  dirty?: Array<{ path: string; baseVersion?: number; lastSeenHash: string }>;
}) {
  const adapter = createAdapter(input.files);
  const vault = { adapter } as any;
  const fileMeta = new FileMetaStore(input.meta, async () => {});
  const manager = new SyncManager({} as any, vault, "ws://localhost", "token", "personal", fileMeta, ".goat-delete-queue.json");
  const wsClient = {
    on: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    startHealthCheck: vi.fn(),
    sendMergePut: vi.fn(() => true),
    sendFilePut: vi.fn(() => true),
    sendFileCheck: vi.fn(() => true),
    sendFileDelete: vi.fn(() => true),
    sendSyncInit: vi.fn(() => true),
  };
  const dirtyQueue = new DirtyQueue();
  for (const entry of input.dirty || []) {
    await dirtyQueue.enqueue(entry);
  }
  (manager as any).wsClient = wsClient;
  (manager as any).dirtyQueue = dirtyQueue;
  return { manager: manager as any, wsClient, adapter, fileMeta, dirtyQueue };
}

describe("auto merge flow", () => {
  test("syncResult toAutoMerge sends mergePut with current local content", async () => {
    const harness = await createSyncManagerHarness({
      files: { "notes/a.md": "local" },
      meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
    });
    await harness.manager["handleSyncResult"]({
      type: "syncResult",
      toAutoMerge: [{
        path: "notes/a.md",
        baseVersion: 1,
        baseHash: "base",
        localHash: "stale-local",
        serverVersion: 2,
        serverHash: "server",
      }],
    });
    expect(harness.wsClient.sendMergePut).toHaveBeenCalledWith(
      "personal",
      "notes/a.md",
      "local",
      expect.objectContaining({ path: "notes/a.md", exists: true, baseVersion: 1, baseHash: "base" }),
      2,
      undefined,
    );
  });

  test("mergePutResult toDownload applies merged content and clears dirty queue", async () => {
    const harness = await createSyncManagerHarness({
      files: { "notes/a.md": "local" },
      meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
      dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: "local" }],
    });
    await harness.manager["handleMergePutResult"]({
      type: "mergePutResult",
      path: "notes/a.md",
      action: "toDownload",
      content: "merged",
      meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
    });
    expect(await harness.adapter.read("notes/a.md")).toBe("merged");
    expect(harness.fileMeta.get("notes/a.md")).toEqual({ prevServerVersion: 3, prevServerHash: await sha256("merged") });
    expect(harness.dirtyQueue.get("notes/a.md")).toBeUndefined();
  });

  test("mergePutResult preserves dirty entry when user edited during merge", async () => {
    const localHash = await sha256("local");
    const newerHash = await sha256("newer local");
    const harness = await createSyncManagerHarness({
      files: { "notes/a.md": "newer local" },
      meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
      dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: localHash }],
    });
    await harness.dirtyQueue.markSentHash("notes/a.md", localHash, localHash);
    await harness.dirtyQueue.enqueue({ path: "notes/a.md", baseVersion: 1, lastSeenHash: newerHash });
    await harness.manager["handleMergePutResult"]({
      type: "mergePutResult",
      path: "notes/a.md",
      action: "toDownload",
      content: "merged",
      meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
    });
    expect(harness.dirtyQueue.get("notes/a.md")?.lastSeenHash).toBe(newerHash);
  });

  test("mergePutResult preserves on-disk content when user edited during merge", async () => {
    const localHash = await sha256("local");
    const newerHash = await sha256("newer local");
    const harness = await createSyncManagerHarness({
      files: { "notes/a.md": "newer local" },
      meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
      dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: localHash }],
    });
    await harness.dirtyQueue.markSentHash("notes/a.md", localHash, localHash);
    await harness.dirtyQueue.enqueue({ path: "notes/a.md", baseVersion: 1, lastSeenHash: newerHash });
    await harness.manager["handleMergePutResult"]({
      type: "mergePutResult",
      path: "notes/a.md",
      action: "toDownload",
      content: "merged",
      meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
    });
    expect(await harness.adapter.read("notes/a.md")).toBe("newer local");
    expect(harness.fileMeta.get("notes/a.md")).toEqual({ prevServerVersion: 3, prevServerHash: await sha256("merged") });
    expect(harness.dirtyQueue.get("notes/a.md")).toMatchObject({ baseVersion: 3, lastSeenHash: newerHash });
  });

  test("filePutResult toDownload preserves on-disk content when user edited during put", async () => {
    const localHash = await sha256("local");
    const newerHash = await sha256("newer local");
    const harness = await createSyncManagerHarness({
      files: { "notes/a.md": "newer local" },
      meta: { "notes/a.md": { prevServerVersion: 1, prevServerHash: "base" } },
      dirty: [{ path: "notes/a.md", baseVersion: 1, lastSeenHash: localHash }],
    });
    await harness.dirtyQueue.markSentHash("notes/a.md", localHash, localHash);
    await harness.dirtyQueue.enqueue({ path: "notes/a.md", baseVersion: 1, lastSeenHash: newerHash });
    await harness.manager["handleFilePutResult"]({
      type: "filePutResult",
      path: "notes/a.md",
      action: "toDownload",
      content: "merged",
      meta: { path: "notes/a.md", serverVersion: 3, serverHash: await sha256("merged"), isDeleted: false },
    });
    expect(await harness.adapter.read("notes/a.md")).toBe("newer local");
    expect(harness.fileMeta.get("notes/a.md")).toEqual({ prevServerVersion: 3, prevServerHash: await sha256("merged") });
    expect(harness.dirtyQueue.get("notes/a.md")).toMatchObject({ baseVersion: 3, lastSeenHash: newerHash });
  });
});
