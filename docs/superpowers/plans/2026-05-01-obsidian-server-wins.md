# Obsidian Server-Wins Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make non-plugin `.obsidian/**` conflicts resolve automatically by applying the server state, keep `.obsidian/plugins/**` excluded, and enlarge the conflict modal.

**Architecture:** Add a shared plugin path policy module, reuse it from file watching and sync conflict handling, and keep server-side sync classification unchanged. Server-wins conflict handling should reuse existing download/delete helpers so metadata, self-write suppression, binary writes, and queue cleanup stay consistent.

**Tech Stack:** TypeScript, Obsidian plugin APIs, Vitest, esbuild, CSS.

**Workspace:** Implement in the current workspace. Do not create or switch to a separate git worktree for this change.

---

## File Structure

- Create: `plugin/src/path-policy.ts`
  - Owns path classification for excluded sync paths and server-wins paths.
- Create: `plugin/src/__tests__/path-policy.test.ts`
  - Tests the path policy directly.
- Modify: `plugin/src/file-watcher.ts`
  - Replace the local `.obsidian/plugins` exclusion helper with `isSyncExcludedPath`.
- Create: `plugin/src/__tests__/file-watcher.test.ts`
  - Verifies recursive listing skips `.obsidian/plugins/**`.
- Modify: `plugin/src/sync.ts`
  - Import the shared path policy.
  - Skip excluded metadata-only entries in sync init.
  - Auto-apply server-wins conflicts before they enter `ConflictQueue`.
  - Clear dirty/delete/blocked state after server-wins application.
- Modify: `plugin/src/__tests__/sync-auto-merge.test.ts`
  - Add coverage for `.obsidian/**` modify and delete conflicts.
- Modify: `plugin/src/conflict-modal.ts`
  - Move modal sizing and card layout from cramped inline styles to class-driven layout.
- Modify: `plugin/styles.css`
  - Add wide responsive modal styles.

---

### Task 1: Add Shared Path Policy

**Files:**
- Create: `plugin/src/path-policy.ts`
- Create: `plugin/src/__tests__/path-policy.test.ts`

- [ ] **Step 1: Write the failing path policy test**

Create `plugin/src/__tests__/path-policy.test.ts`:

```ts
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
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run:

```bash
rtk npm --prefix plugin test -- path-policy
```

Expected: FAIL because `../path-policy` does not exist.

- [ ] **Step 3: Implement the path policy module**

Create `plugin/src/path-policy.ts`:

```ts
const OBSIDIAN_DIR = ".obsidian";
const OBSIDIAN_PLUGINS_DIR = ".obsidian/plugins";

export function isSyncExcludedPath(path: string): boolean {
  return path === OBSIDIAN_PLUGINS_DIR || path.startsWith(`${OBSIDIAN_PLUGINS_DIR}/`);
}

export function isServerWinsPath(path: string): boolean {
  if (isSyncExcludedPath(path)) return false;
  return path === OBSIDIAN_DIR || path.startsWith(`${OBSIDIAN_DIR}/`);
}
```

- [ ] **Step 4: Run the focused test to verify it passes**

Run:

```bash
rtk npm --prefix plugin test -- path-policy
```

Expected: PASS for all four path policy tests.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add plugin/src/path-policy.ts plugin/src/__tests__/path-policy.test.ts
rtk git commit -m "feat(plugin): add sync path policy"
```

---

### Task 2: Use Path Policy in FileWatcher

**Files:**
- Modify: `plugin/src/file-watcher.ts`
- Create: `plugin/src/__tests__/file-watcher.test.ts`

- [ ] **Step 1: Write the failing FileWatcher recursive listing test**

Create `plugin/src/__tests__/file-watcher.test.ts`:

```ts
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
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
rtk npm --prefix plugin test -- file-watcher
```

Expected: PASS before or after implementation if the current local helper already excludes plugins. Keep the test because it locks the required behavior while the helper moves.

- [ ] **Step 3: Replace the local exclusion helper**

Modify the top of `plugin/src/file-watcher.ts` from:

```ts
import { Vault, TFile, TAbstractFile } from "obsidian";

function isExcluded(path: string): boolean {
  return path === ".obsidian/plugins" || path.startsWith(".obsidian/plugins/");
}
```

to:

```ts
import { Vault, TFile, TAbstractFile } from "obsidian";
import { isSyncExcludedPath } from "./path-policy";
```

Then replace every `isExcluded(` call in `plugin/src/file-watcher.ts` with `isSyncExcludedPath(`.

- [ ] **Step 4: Run FileWatcher and path policy tests**

Run:

```bash
rtk npm --prefix plugin test -- file-watcher path-policy
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add plugin/src/file-watcher.ts plugin/src/__tests__/file-watcher.test.ts
rtk git commit -m "test(plugin): cover excluded obsidian plugin paths"
```

---

### Task 3: Auto-Apply Server-Wins Conflicts

**Files:**
- Modify: `plugin/src/sync.ts`
- Modify: `plugin/src/__tests__/sync-auto-merge.test.ts`

- [ ] **Step 1: Write failing sync tests for `.obsidian/**` conflicts**

Append these tests inside the existing `describe("auto merge flow", () => { ... })` block in `plugin/src/__tests__/sync-auto-merge.test.ts`:

```ts
  test("syncResult applies obsidian settings conflict from server without opening modal", async () => {
    const serverHash = await sha256("server settings");
    const harness = await createSyncManagerHarness({
      files: { ".obsidian/app.json": "local settings" },
      meta: { ".obsidian/app.json": { prevServerVersion: 1, prevServerHash: "base" } },
      dirty: [{ path: ".obsidian/app.json", baseVersion: 1, lastSeenHash: await sha256("local settings") }],
    });
    harness.manager["openConflictModal"] = vi.fn();

    await harness.manager["handleSyncResult"]({
      type: "syncResult",
      conflicts: [{
        path: ".obsidian/app.json",
        baseVersion: 1,
        localHash: await sha256("local settings"),
        serverVersion: 2,
        serverHash,
        serverContent: "server settings",
        isDeleted: false,
      }],
    });

    expect(await harness.adapter.read(".obsidian/app.json")).toBe("server settings");
    expect(harness.fileMeta.get(".obsidian/app.json")).toEqual({
      prevServerVersion: 2,
      prevServerHash: serverHash,
    });
    expect(harness.dirtyQueue.get(".obsidian/app.json")).toBeUndefined();
    expect(harness.deleteQueue.get(".obsidian/app.json")).toBeUndefined();
    expect(harness.manager["blockedPaths"].has(".obsidian/app.json")).toBe(false);
    expect(harness.manager["conflictQueue"].get(".obsidian/app.json")).toBeUndefined();
    expect(harness.manager["openConflictModal"]).not.toHaveBeenCalled();
  });

  test("filePutResult applies obsidian settings conflict from server without opening modal", async () => {
    const serverHash = await sha256("server hotkeys");
    const harness = await createSyncManagerHarness({
      files: { ".obsidian/hotkeys.json": "local hotkeys" },
      meta: { ".obsidian/hotkeys.json": { prevServerVersion: 3, prevServerHash: "old" } },
      dirty: [{ path: ".obsidian/hotkeys.json", baseVersion: 3, lastSeenHash: await sha256("local hotkeys") }],
    });
    harness.manager["openConflictModal"] = vi.fn();

    await harness.manager["handleFilePutResult"]({
      type: "filePutResult",
      path: ".obsidian/hotkeys.json",
      action: "conflict",
      conflict: {
        serverVersion: 4,
        serverHash,
        serverContent: "server hotkeys",
        isDeleted: false,
      },
    });

    expect(await harness.adapter.read(".obsidian/hotkeys.json")).toBe("server hotkeys");
    expect(harness.fileMeta.get(".obsidian/hotkeys.json")).toEqual({
      prevServerVersion: 4,
      prevServerHash: serverHash,
    });
    expect(harness.dirtyQueue.get(".obsidian/hotkeys.json")).toBeUndefined();
    expect(harness.manager["conflictQueue"].get(".obsidian/hotkeys.json")).toBeUndefined();
    expect(harness.manager["openConflictModal"]).not.toHaveBeenCalled();
  });

  test("fileDeleteResult applies obsidian settings delete conflict from server", async () => {
    const harness = await createSyncManagerHarness({
      files: { ".obsidian/snippets/foo.css": "local css" },
      meta: { ".obsidian/snippets/foo.css": { prevServerVersion: 2, prevServerHash: "old" } },
      deleted: [{ path: ".obsidian/snippets/foo.css", baseVersion: 2, serverHash: "old" }],
    });
    harness.manager["openConflictModal"] = vi.fn();

    await harness.manager["handleFileDeleteResult"]({
      type: "fileDeleteResult",
      path: ".obsidian/snippets/foo.css",
      action: "deleteConflict",
      conflict: {
        serverVersion: 5,
        serverHash: "deleted-hash",
        serverContent: "",
        isDeleted: true,
      },
    });

    expect(await harness.adapter.exists(".obsidian/snippets/foo.css")).toBe(false);
    expect(harness.fileMeta.get(".obsidian/snippets/foo.css")).toEqual({
      prevServerVersion: 5,
      prevServerHash: "deleted-hash",
    });
    expect(harness.deleteQueue.get(".obsidian/snippets/foo.css")).toBeUndefined();
    expect(harness.manager["conflictQueue"].get(".obsidian/snippets/foo.css")).toBeUndefined();
    expect(harness.manager["openConflictModal"]).not.toHaveBeenCalled();
  });
```

- [ ] **Step 2: Run the focused sync tests to verify they fail**

Run:

```bash
rtk npm --prefix plugin test -- sync-auto-merge
```

Expected: FAIL because `.obsidian/**` conflicts are still queued and/or open the modal.

- [ ] **Step 3: Import the shared path policy**

Modify the imports at the top of `plugin/src/sync.ts`:

```ts
import { isServerWinsPath, isSyncExcludedPath } from "./path-policy";
```

- [ ] **Step 4: Skip excluded metadata-only entries during sync init**

In `performSyncInit`, change the metadata loop guard from:

```ts
if (localPaths.has(path) || this.blockedPaths.has(path)) continue;
```

to:

```ts
if (isSyncExcludedPath(path) || localPaths.has(path) || this.blockedPaths.has(path)) continue;
```

- [ ] **Step 5: Add server-wins helper methods**

Add these private methods in `plugin/src/sync.ts` near `enqueueConflict`:

```ts
  private async applyServerWinsConflict(c: SyncConflictEntry): Promise<void> {
    if (c.isDeleted) {
      await this.applyServerDelete(c.path, c.serverVersion, c.serverHash);
    } else {
      await this.applyDownloadEntry({
        path: c.path,
        content: c.serverContent,
        serverVersion: c.serverVersion,
        serverHash: c.serverHash,
        encoding: c.encoding,
      });
    }
    await this.clearTransientState(c.path);
  }

  private async applyServerWinsLatestConflict(msg: ServerMessage): Promise<boolean> {
    if (!msg.path || !msg.conflict || !isServerWinsPath(msg.path)) return false;

    if (msg.conflict.isDeleted || msg.action === "deleteConflict") {
      await this.applyServerDelete(
        msg.path,
        msg.conflict.serverVersion,
        msg.conflict.serverHash,
      );
    } else {
      await this.applyDownloadEntry({
        path: msg.path,
        content: msg.conflict.serverContent,
        serverVersion: msg.conflict.serverVersion,
        serverHash: msg.conflict.serverHash,
        encoding: msg.conflict.encoding,
      });
    }
    await this.clearTransientState(msg.path);
    return true;
  }

  private async clearTransientState(path: string): Promise<void> {
    await this.dirtyQueue.remove(path);
    await this.deleteQueue.remove(path);
    this.blockedPaths.clear(path);
    this.conflictQueue.remove(path);
  }
```

- [ ] **Step 6: Use server-wins helper in `handleSyncResult`**

In the `msg.conflicts` loop, change:

```ts
for (const c of msg.conflicts) {
  await this.enqueueConflict(c);
}
this.openConflictModal();
```

to:

```ts
let openedConflict = false;
for (const c of msg.conflicts) {
  if (isServerWinsPath(c.path)) {
    await this.applyServerWinsConflict(c);
  } else {
    await this.enqueueConflict(c);
    openedConflict = true;
  }
}
if (openedConflict) {
  this.openConflictModal();
}
```

- [ ] **Step 7: Use server-wins helper in single-file conflict handlers**

Before existing queue/block logic in each conflict branch, add these guards:

In `handleFilePutResult`, inside:

```ts
} else if ((msg.action === "conflict" || msg.action === "deleteConflict") && msg.conflict) {
```

add:

```ts
      if (await this.applyServerWinsLatestConflict(msg)) return;
```

In `handleFileDeleteResult`, inside:

```ts
} else if (msg.action === "deleteConflict" && msg.conflict) {
```

add:

```ts
      if (await this.applyServerWinsLatestConflict(msg)) return;
```

In `handleFileCheckResult`, inside:

```ts
case "conflict":
case "deleteConflict":
  if (msg.conflict) {
```

add:

```ts
          if (await this.applyServerWinsLatestConflict(msg)) return;
```

In `handleMergePutResult`, inside:

```ts
if ((msg.action === "conflict" || msg.action === "deleteConflict") && msg.conflict) {
```

add:

```ts
        if (await this.applyServerWinsLatestConflict(msg)) return;
```

- [ ] **Step 8: Run focused sync tests**

Run:

```bash
rtk npm --prefix plugin test -- sync-auto-merge
```

Expected: PASS.

- [ ] **Step 9: Commit**

Run:

```bash
rtk git add plugin/src/sync.ts plugin/src/__tests__/sync-auto-merge.test.ts
rtk git commit -m "feat(plugin): resolve obsidian settings conflicts from server"
```

---

### Task 4: Enlarge Conflict Modal

**Files:**
- Modify: `plugin/src/conflict-modal.ts`
- Modify: `plugin/styles.css`

- [ ] **Step 1: Move modal layout to classes**

In `plugin/src/conflict-modal.ts`, update `onOpen` so the top-level layout uses classes:

```ts
    contentEl.empty();
    contentEl.addClass("obsidian-goat-sync-conflict-modal");

    const header = contentEl.createEl("h2", { text: "Sync Conflicts" });
    header.addClass("obsidian-goat-sync-conflict-title");

    const body = contentEl.createDiv();
    body.addClass("obsidian-goat-sync-conflict-body");

    this.sidebarEl = body.createDiv();
    this.sidebarEl.addClass("obsidian-goat-sync-conflict-sidebar");

    this.cardsEl = body.createDiv();
    this.cardsEl.addClass("obsidian-goat-sync-conflict-cards");

    const footer = contentEl.createDiv();
    footer.addClass("obsidian-goat-sync-conflict-footer");
```

Keep the existing `Apply All` button creation and click handler, but replace its inline style with:

```ts
    applyBtn.addClass("mod-cta");
```

- [ ] **Step 2: Add card layout classes**

In `renderCards`, replace:

```ts
cardsRow.style.cssText = "display:flex;gap:12px;";
```

with:

```ts
cardsRow.addClass("obsidian-goat-sync-conflict-card-row");
```

In `createCard`, after `const card = container.createDiv();`, replace the long `card.style.cssText = ...` assignment with:

```ts
    card.addClass("obsidian-goat-sync-conflict-card");
```

For the preview element, replace:

```ts
preview.style.cssText = "max-height:220px;overflow:auto;font-size:12px;flex:1;";
```

with:

```ts
    preview.addClass("obsidian-goat-sync-conflict-preview");
```

For the save-as-new card in `renderCards`, replace the long inline style assignment with:

```ts
      newCard.addClass("obsidian-goat-sync-conflict-card");
      newCard.addClass("obsidian-goat-sync-conflict-save-new");
```

- [ ] **Step 3: Add responsive modal CSS**

Replace the content of `plugin/styles.css` with:

```css
/* plugin/styles.css */

.modal:has(.obsidian-goat-sync-conflict-modal) {
  width: min(1200px, 96vw);
  max-width: 96vw;
}

.obsidian-goat-sync-conflict-modal {
  display: flex;
  flex-direction: column;
  height: 86vh;
  min-height: 520px;
}

.obsidian-goat-sync-conflict-title {
  margin: 0 0 12px;
}

.obsidian-goat-sync-conflict-body {
  display: flex;
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
  overflow: hidden;
}

.obsidian-goat-sync-conflict-sidebar {
  width: 240px;
  min-width: 190px;
  border-right: 1px solid var(--background-modifier-border);
  overflow-y: auto;
  padding-right: 12px;
}

.obsidian-goat-sync-conflict-cards {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
}

.obsidian-goat-sync-conflict-card-row {
  display: flex;
  gap: 12px;
  min-height: 0;
}

.obsidian-goat-sync-conflict-card {
  flex: 1 1 0;
  min-width: 0;
  border: 1px solid var(--background-modifier-border);
  border-radius: 8px;
  padding: 12px;
  cursor: pointer;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.obsidian-goat-sync-conflict-preview {
  flex: 1 1 auto;
  min-height: 460px;
  overflow: auto;
  font-size: 12px;
}

.obsidian-goat-sync-conflict-footer {
  margin-top: 12px;
  text-align: right;
}

@media (max-width: 900px) {
  .obsidian-goat-sync-conflict-modal {
    height: 86vh;
    min-height: 420px;
  }

  .obsidian-goat-sync-conflict-body {
    flex-direction: column;
  }

  .obsidian-goat-sync-conflict-sidebar {
    width: auto;
    min-width: 0;
    max-height: 160px;
    border-right: 0;
    border-bottom: 1px solid var(--background-modifier-border);
    padding-right: 0;
    padding-bottom: 8px;
  }

  .obsidian-goat-sync-conflict-card-row {
    flex-direction: column;
  }

  .obsidian-goat-sync-conflict-preview {
    min-height: 260px;
  }
}
```

- [ ] **Step 4: Run plugin build**

Run:

```bash
rtk npm --prefix plugin run build
```

Expected: PASS. If TypeScript reports unused variables or syntax errors, fix the exact reported lines and rerun the same command.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add plugin/src/conflict-modal.ts plugin/styles.css
rtk git commit -m "style(plugin): enlarge conflict modal"
```

---

### Task 5: Full Verification

**Files:**
- Verify all modified plugin files.

- [ ] **Step 1: Run all plugin tests**

Run:

```bash
rtk npm --prefix plugin test
```

Expected: PASS.

- [ ] **Step 2: Run plugin production build**

Run:

```bash
rtk npm --prefix plugin run build
```

Expected: PASS.

- [ ] **Step 3: Inspect git status**

Run:

```bash
rtk git status --short
```

Expected: clean working tree after all task commits.

- [ ] **Step 4: Optional graph review context**

Run code-review graph change detection before final review:

```bash
detect_changes changed_files=[
  "plugin/src/path-policy.ts",
  "plugin/src/file-watcher.ts",
  "plugin/src/sync.ts",
  "plugin/src/conflict-modal.ts",
  "plugin/styles.css",
  "plugin/src/__tests__/path-policy.test.ts",
  "plugin/src/__tests__/file-watcher.test.ts",
  "plugin/src/__tests__/sync-auto-merge.test.ts"
]
```

Expected: review risks focus on `SyncManager` conflict paths and modal layout only; no server-side files are changed.

---

## Self-Review

- Spec coverage: path policy, server-wins `.obsidian/**` conflict handling, plugin exclusion, modal enlargement, and plugin verification are covered by Tasks 1-5.
- Placeholder scan: no task contains open implementation placeholders.
- Type consistency: `isSyncExcludedPath`, `isServerWinsPath`, `applyServerWinsConflict`, `applyServerWinsLatestConflict`, and `clearTransientState` are named consistently across tasks.
