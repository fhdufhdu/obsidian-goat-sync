import { describe, expect, it } from "vitest";
import { DeleteQueue, QueueFileAdapter } from "../delete-queue";

class MemoryAdapter implements QueueFileAdapter {
  files = new Map<string, string>();

  async read(path: string): Promise<string> {
    const value = this.files.get(path);
    if (value === undefined) throw new Error("missing");
    return value;
  }

  async write(path: string, data: string): Promise<void> {
    this.files.set(path, data);
  }

  async exists(path: string): Promise<boolean> {
    return this.files.has(path);
  }

  async remove(path: string): Promise<void> {
    this.files.delete(path);
  }

  async rename(from: string, to: string): Promise<void> {
    const value = this.files.get(from);
    if (value === undefined) throw new Error("missing temp");
    this.files.set(to, value);
    this.files.delete(from);
  }
}

describe("DeleteQueue", () => {
  it("dedupes by path and preserves original baseVersion", async () => {
    const adapter = new MemoryAdapter();
    const q = new DeleteQueue(adapter, "delete-queue.json");

    await q.load();
    await q.enqueue({ path: "a.md", baseVersion: 10, serverHash: "H10" });
    await q.enqueue({ path: "a.md", baseVersion: 11, serverHash: "H11" });

    expect(q.list()).toMatchObject([
      {
        path: "a.md",
        baseVersion: 10,
        serverHash: "H10",
        status: "pending",
        queuedAt: expect.any(Number),
      },
    ]);
  });

  it("persists with temp rename", async () => {
    const adapter = new MemoryAdapter();
    const q = new DeleteQueue(adapter, "delete-queue.json");

    await q.load();
    await q.enqueue({ path: "a.md", baseVersion: 10, serverHash: "H10" });

    expect(adapter.files.has("delete-queue.json.tmp")).toBe(false);
    expect(JSON.parse(adapter.files.get("delete-queue.json") || "[]")).toHaveLength(1);
  });
});
