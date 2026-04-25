import { describe, expect, it } from "vitest";
import { buildFilePutMessage, buildSyncInitMessage } from "../ws-client";

describe("ws protocol builders", () => {
  it("builds syncInit with baseVersion and localHash", () => {
    expect(
      buildSyncInitMessage("personal", [
        { path: "a.md", exists: true, baseVersion: 3, baseHash: "H3", localHash: "H4" },
      ]),
    ).toEqual({
      type: "syncInit",
      vault: "personal",
      files: [{ path: "a.md", exists: true, baseVersion: 3, baseHash: "H3", localHash: "H4" }],
    });
  });

  it("builds filePut", () => {
    expect(
      buildFilePutMessage("personal", "a.md", "body", { path: "a.md", exists: true, baseVersion: 3, localHash: "H4" }),
    ).toMatchObject({
      type: "filePut",
      vault: "personal",
      path: "a.md",
      content: "body",
      file: { path: "a.md", exists: true, baseVersion: 3, localHash: "H4" },
    });
  });
});
