import { describe, it, expect, beforeEach } from "vitest";
import { ConflictQueue, ConflictEntry } from "../conflict-queue";

function makeEntry(path: string, kind: "modify" | "delete" = "modify"): ConflictEntry {
  return {
    path,
    currentClientContent: "local",
    currentClientHash: "localhash",
    currentServerVersion: 5,
    currentServerHash: "serverhash",
    currentServerContent: "server",
    kind,
  };
}

describe("ConflictQueue", () => {
  let queue: ConflictQueue;

  beforeEach(() => {
    queue = new ConflictQueue();
  });

  it("starts empty", () => {
    expect(queue.list()).toHaveLength(0);
    expect(queue.size()).toBe(0);
  });

  it("adds entries", () => {
    queue.add(makeEntry("a.md"));
    queue.add(makeEntry("b.md"));
    expect(queue.list()).toHaveLength(2);
  });

  it("get retrieves by path", () => {
    queue.add(makeEntry("a.md"));
    const e = queue.get("a.md");
    expect(e?.path).toBe("a.md");
  });

  it("get returns undefined for unknown path", () => {
    expect(queue.get("unknown.md")).toBeUndefined();
  });

  it("selectAt sets selection", () => {
    queue.add(makeEntry("a.md"));
    queue.selectAt("a.md", "server");
    expect(queue.get("a.md")?.selection).toBe("server");
  });

  it("selectAt allows changing selection", () => {
    queue.add(makeEntry("a.md"));
    queue.selectAt("a.md", "server");
    queue.selectAt("a.md", "local");
    expect(queue.get("a.md")?.selection).toBe("local");
  });

  it("remove deletes entry", () => {
    queue.add(makeEntry("a.md"));
    queue.remove("a.md");
    expect(queue.get("a.md")).toBeUndefined();
    expect(queue.list()).toHaveLength(0);
  });

  it("isAllResolved returns false when empty", () => {
    expect(queue.isAllResolved()).toBe(false);
  });

  it("isAllResolved returns false when some unresolved", () => {
    queue.add(makeEntry("a.md"));
    queue.add(makeEntry("b.md"));
    queue.selectAt("a.md", "server");
    expect(queue.isAllResolved()).toBe(false);
  });

  it("isAllResolved returns true when all resolved", () => {
    queue.add(makeEntry("a.md"));
    queue.add(makeEntry("b.md"));
    queue.selectAt("a.md", "server");
    queue.selectAt("b.md", "local");
    expect(queue.isAllResolved()).toBe(true);
  });

  it("clear empties the queue", () => {
    queue.add(makeEntry("a.md"));
    queue.add(makeEntry("b.md"));
    queue.clear();
    expect(queue.list()).toHaveLength(0);
    expect(queue.isAllResolved()).toBe(false);
  });

  it("add overwrites existing entry for same path", () => {
    queue.add(makeEntry("a.md"));
    queue.selectAt("a.md", "server");
    const updated = makeEntry("a.md");
    updated.currentServerVersion = 10;
    queue.add(updated);
    expect(queue.get("a.md")?.currentServerVersion).toBe(10);
    expect(queue.get("a.md")?.selection).toBeUndefined();
  });
});
