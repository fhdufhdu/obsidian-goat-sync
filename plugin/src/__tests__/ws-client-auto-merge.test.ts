import { expect, test } from "vitest";
import { buildMergePutMessage } from "../ws-client";

test("buildMergePutMessage includes expectedServerVersion", () => {
  expect(buildMergePutMessage("personal", "notes/a.md", "local", {
    path: "notes/a.md",
    exists: true,
    baseVersion: 1,
    baseHash: "base",
    localHash: "local",
  }, 2)).toEqual({
    type: "mergePut",
    vault: "personal",
    path: "notes/a.md",
    content: "local",
    file: {
      path: "notes/a.md",
      exists: true,
      baseVersion: 1,
      baseHash: "base",
      localHash: "local",
    },
    expectedServerVersion: 2,
  });
});
