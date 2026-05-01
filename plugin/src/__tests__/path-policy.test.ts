import { describe, expect, test } from "vitest";
import { isServerWinsPath, isSyncExcludedPath } from "../path-policy";

describe("path policy", () => {
  test("treats obsidian settings as server-wins paths", () => {
    expect(isServerWinsPath(".obsidian/app.json")).toBe(true);
    expect(isServerWinsPath(".obsidian/snippets/foo.css")).toBe(true);
    expect(isServerWinsPath(".obsidian/workspace.json")).toBe(true);
  });

  test("excludes installed obsidian plugins from sync", () => {
    expect(isSyncExcludedPath(".obsidian/plugins")).toBe(true);
    expect(isSyncExcludedPath(".obsidian/plugins/calendar/main.js")).toBe(true);
    expect(isSyncExcludedPath(".obsidian/plugins/calendar/styles.css")).toBe(true);
  });

  test("does not treat excluded plugin files as server-wins", () => {
    expect(isServerWinsPath(".obsidian/plugins")).toBe(false);
    expect(isServerWinsPath(".obsidian/plugins/calendar/main.js")).toBe(false);
  });

  test("leaves regular vault files as normal sync paths", () => {
    expect(isSyncExcludedPath("notes/a.md")).toBe(false);
    expect(isServerWinsPath("notes/a.md")).toBe(false);
    expect(isSyncExcludedPath(".obsidian-plugin-note.md")).toBe(false);
    expect(isServerWinsPath(".obsidian-plugin-note.md")).toBe(false);
  });
});
