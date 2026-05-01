import { describe, expect, test, vi } from "vitest";
import { FileWatcher } from "../file-watcher";

function createVault() {
  const files = new Set([
    "notes/a.md",
    ".obsidian/app.json",
    ".obsidian/plugins/calendar/main.js",
  ]);

  const foldersByDir: Record<string, { files: string[]; folders: string[] }> = {
    "": { files: ["notes/a.md"], folders: [".obsidian"] },
    ".obsidian": {
      files: [".obsidian/app.json"],
      folders: [".obsidian/plugins"],
    },
    ".obsidian/plugins": {
      files: [".obsidian/plugins/calendar/main.js"],
      folders: [],
    },
  };

  return {
    on: vi.fn(),
    off: vi.fn(),
    adapter: {
      async list(dir: string) {
        return foldersByDir[dir] || { files: [], folders: [] };
      },
      async stat(path: string) {
        return files.has(path) ? { type: "file" } : null;
      },
    },
  } as any;
}

describe("FileWatcher", () => {
  test("getAllFiles includes obsidian settings but excludes installed plugins", async () => {
    const watcher = new FileWatcher(createVault(), vi.fn());

    await expect(watcher.getAllFiles()).resolves.toEqual([
      { path: "notes/a.md" },
      { path: ".obsidian/app.json" },
    ]);
  });
});
