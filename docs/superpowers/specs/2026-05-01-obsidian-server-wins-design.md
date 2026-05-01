# Obsidian Settings Server-Wins Sync Design

Date: 2026-05-01

## Summary

Obsidian settings files under `.obsidian/**` should keep syncing, but conflicts in that area should never require manual resolution. When a conflict involves `.obsidian/**`, the server state wins and the local vault is overwritten or deleted to match the server. The actual Obsidian plugin install directory, `.obsidian/plugins/**`, remains fully excluded from synchronization.

This work will proceed in the current workspace without creating or using a separate git worktree.

## Goals

- Treat `.obsidian/**` conflicts as server-wins conflicts.
- Keep `.obsidian/plugins/**` out of all sync inputs and outputs.
- Prevent `.obsidian/**` conflicts from appearing in the conflict modal.
- Make the conflict modal large enough to inspect long file contents comfortably.
- Keep the server-side sync classifier unchanged.

## Non-Goals

- Add a user-facing setting for this behavior.
- Change server conflict classification rules.
- Sync installed Obsidian community plugins from `.obsidian/plugins/**`.
- Build a full visual diff viewer.

## Path Policy

The plugin will use one shared path policy helper so all sync surfaces classify paths the same way:

- `isSyncExcludedPath(path)` returns true for `.obsidian/plugins` and `.obsidian/plugins/**`.
- `isServerWinsPath(path)` returns true for `.obsidian/**` except excluded plugin paths.
- Normal notes and attachments remain regular sync paths.

`FileWatcher` will use `isSyncExcludedPath` for create, modify, delete, and recursive initial listing. `SyncManager` will also use the same helper when building sync init payloads and when deciding how to handle conflicts.

## Sync Behavior

For ordinary files, conflict handling stays unchanged: conflicts enter `ConflictQueue` and the modal lets the user choose server, local, or save-as-new.

For `.obsidian/**` paths that are not excluded:

- If the server reports a modify conflict with server content, the plugin applies the server content to the local file, updates file metadata to the server version and hash, clears any blocked state, and removes stale dirty/delete queue entries for that path.
- If the server reports a delete conflict or instructs local deletion, the plugin deletes the local file and updates/removes metadata through the existing server-delete flow.
- These entries do not get added to `ConflictQueue`, and the conflict modal is not opened for them.

Local `.obsidian/**` edits still upload normally when there is no conflict. The server-wins policy applies to conflicts and server delete/download instructions, not to every local edit.

## Conflict Modal Layout

The modal will move away from small inline dimensions and use class-based sizing:

- Modal width targets `min(1200px, 96vw)`.
- Modal height targets around `86vh`.
- The content area uses a stable flex layout with a sidebar and a preview area.
- Preview cards use available height instead of a fixed `220px` cap.
- Narrow viewports can stack or compress the cards so text does not become unreadable.

The current comparison model remains the same: server, local, and save-as-new choices. This change only improves available reading space and maintainability of the modal styling.

## Error Handling

Server-wins application will reuse existing write/delete helpers so self-write suppression, metadata updates, binary handling, and notices remain consistent with the rest of the sync manager. If applying server content fails, the error should be logged and surfaced with a notice rather than silently dropping the conflict.

Excluded `.obsidian/plugins/**` paths should be ignored rather than deleted or overwritten by sync policy.

## Testing

Add focused plugin tests for the path policy:

- `.obsidian/app.json` is server-wins.
- `.obsidian/snippets/foo.css` is server-wins.
- `.obsidian/plugins/main.js` and nested plugin files are excluded.
- Regular vault files are neither excluded nor server-wins.

Add sync behavior coverage where the existing test harness supports it:

- A `.obsidian/**` conflict is resolved by applying server content without adding a conflict queue entry.
- A `.obsidian/**` delete conflict follows the server-delete path without opening the modal.
- `.obsidian/plugins/**` does not appear in initial sync payloads or file watcher changes.

Verification should run `npm test` and `npm run build` inside `plugin`. Server tests are not required for this design because server code is intentionally unchanged.

## Open Decisions

No open product decisions remain. The implementation should use the current workspace directly, without a separate git worktree.
