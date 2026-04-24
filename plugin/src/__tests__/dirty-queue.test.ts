import { describe, expect, it } from "vitest";
import { AsyncMutex } from "../async-mutex";
import { DirtyQueue } from "../dirty-queue";

describe("DirtyQueue", () => {
  it("coalesces same-path pending changes", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.enqueue({ path: "a.md", baseVersion: 11, lastSeenHash: "H2" });
    expect(q.list()).toEqual([
      {
        path: "a.md",
        baseVersion: 10,
        lastSeenHash: "H2",
        status: "pending",
        queuedAt: expect.any(Number),
      },
    ]);
  });

  it("keeps inFlight status and updates only latest hash", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    const claim = await q.claimNext();
    expect(claim?.lastSeenHash).toBe("H1");
    await q.markSentHash("a.md", "H1", "H1");
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H2" });
    expect(q.get("a.md")).toMatchObject({
      status: "inFlight",
      sentHash: "H1",
      baseVersion: 10,
      lastSeenHash: "H2",
    });
  });

  it("removes entry when sent hash is still latest", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.claimNext();
    await q.markSentHash("a.md", "H1", "H1");
    await q.completeSuccess("a.md", "H1", { serverVersion: 11, serverHash: "H1" });
    expect(q.get("a.md")).toBeUndefined();
  });

  it("rebases entry when in-flight update changed latest hash", async () => {
    const q = new DirtyQueue();
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H1" });
    await q.claimNext();
    await q.markSentHash("a.md", "H1", "H1");
    await q.enqueue({ path: "a.md", baseVersion: 10, lastSeenHash: "H2" });
    await q.completeSuccess("a.md", "H1", { serverVersion: 11, serverHash: "H1" });
    expect(q.get("a.md")).toMatchObject({
      baseVersion: 11,
      lastSeenHash: "H2",
      status: "pending",
    });
  });
});

describe("AsyncMutex", () => {
  it("continues running queued work after a rejected callback", async () => {
    const mutex = new AsyncMutex();
    await expect(mutex.runExclusive(async () => {
      throw new Error("boom");
    })).rejects.toThrow("boom");

    await expect(mutex.runExclusive(() => "ok")).resolves.toBe("ok");
  });
});
