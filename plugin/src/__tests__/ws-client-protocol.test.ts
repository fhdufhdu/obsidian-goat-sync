import { afterEach, describe, expect, it, vi } from "vitest";
import { buildFilePutMessage, buildSyncInitMessage, WsClient } from "../ws-client";

afterEach(() => {
  vi.restoreAllMocks();
});

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

  it("logs outgoing raw messages", () => {
    const debug = vi.spyOn(console, "debug").mockImplementation(() => undefined);
    const send = vi.fn();
    const client = new WsClient("ws://localhost", "token");
    (client as unknown as { ws: { readyState: number; send: (data: string) => void } }).ws = {
      readyState: WebSocket.OPEN,
      send,
    };

    expect(
      client.send(
        buildFilePutMessage("personal", "a.md", "secret body", {
          path: "a.md",
          exists: true,
          baseVersion: 3,
          localHash: "H4",
        }),
      ),
    ).toBe(true);

    expect(debug).toHaveBeenCalledWith(
      "[obsidian-goat-sync] ws outgoing raw",
      JSON.stringify({
        type: "filePut",
        vault: "personal",
        path: "a.md",
        content: "secret body",
        file: { path: "a.md", exists: true, baseVersion: 3, localHash: "H4" },
      }),
    );
    expect(JSON.stringify(debug.mock.calls)).toContain("secret body");
  });
});
