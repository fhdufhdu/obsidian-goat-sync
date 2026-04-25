import { describe, expect, it } from "vitest";
import { BlockedPaths } from "../blocked-paths";

describe("BlockedPaths", () => {
  it("registers, checks, and clears", () => {
    const blocked = new BlockedPaths();

    blocked.block({
      path: "a.md",
      reason: "conflict",
      serverVersion: 5,
      serverHash: "H5",
      isDeleted: false,
    });

    expect(blocked.has("a.md")).toBe(true);

    blocked.clear("a.md");
    expect(blocked.has("a.md")).toBe(false);
  });
});
