import { describe, it, expect, vi, beforeEach } from "vitest";
import { FileMetaStore, FileMeta } from "../file-meta-store";

describe("FileMetaStore", () => {
  let saved: Record<string, FileMeta> | null;
  let onSave: ReturnType<typeof vi.fn>;
  let store: FileMetaStore;

  beforeEach(() => {
    saved = null;
    onSave = vi.fn(async (data: Record<string, FileMeta>) => {
      saved = { ...data };
    });
    store = new FileMetaStore({}, onSave);
  });

  it("returns undefined for unknown path", () => {
    expect(store.get("notes/a.md")).toBeUndefined();
  });

  it("stores and retrieves meta", () => {
    store.set("notes/a.md", { prevServerVersion: 5, prevServerHash: "abc" });
    const meta = store.get("notes/a.md");
    expect(meta?.prevServerVersion).toBe(5);
    expect(meta?.prevServerHash).toBe("abc");
  });

  it("removes meta", () => {
    store.set("notes/a.md", { prevServerVersion: 1, prevServerHash: "x" });
    store.remove("notes/a.md");
    expect(store.get("notes/a.md")).toBeUndefined();
  });

  it("entries returns all entries", () => {
    store.set("a.md", { prevServerVersion: 1, prevServerHash: "h1" });
    store.set("b.md", { prevServerVersion: 2, prevServerHash: "h2" });
    const entries = store.entries();
    expect(entries).toHaveLength(2);
  });

  it("initializes from provided data", () => {
    const initial = { "notes/x.md": { prevServerVersion: 3, prevServerHash: "xyz" } };
    const s = new FileMetaStore(initial, onSave);
    expect(s.get("notes/x.md")?.prevServerVersion).toBe(3);
  });

  it("flush saves immediately", async () => {
    store.set("notes/a.md", { prevServerVersion: 1, prevServerHash: "h" });
    await store.flush();
    expect(onSave).toHaveBeenCalled();
    expect(saved?.["notes/a.md"]?.prevServerVersion).toBe(1);
  });

  it("schedules debounced save on set", async () => {
    vi.useFakeTimers();
    store.set("notes/a.md", { prevServerVersion: 1, prevServerHash: "h" });
    expect(onSave).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(500);
    expect(onSave).toHaveBeenCalledOnce();
    vi.useRealTimers();
  });

  it("debounce resets on multiple sets", async () => {
    vi.useFakeTimers();
    store.set("a.md", { prevServerVersion: 1, prevServerHash: "h1" });
    await vi.advanceTimersByTimeAsync(200);
    store.set("b.md", { prevServerVersion: 2, prevServerHash: "h2" });
    await vi.advanceTimersByTimeAsync(300);
    expect(onSave).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(200);
    expect(onSave).toHaveBeenCalledOnce();
    vi.useRealTimers();
  });
});
