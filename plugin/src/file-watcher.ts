import { Vault, TFile, TAbstractFile } from "obsidian";
import { isSyncExcludedPath } from "./path-policy";

export interface FileChange {
  type: "create" | "modify" | "delete";
  path: string;
}

export class FileWatcher {
  private vault: Vault;
  private onChange: (change: FileChange) => void;
  private eventHandlers: Array<{ name: string; handler: (file: TAbstractFile) => void }> = [];
  private pendingChangeTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly debounceMs = 75;

  constructor(
    vault: Vault,
    onChange: (change: FileChange) => void,
  ) {
    this.vault = vault;
    this.onChange = onChange;
  }

  start() {
    const onCreate = (file: TAbstractFile) => {
      if (file instanceof TFile && !isSyncExcludedPath(file.path)) {
        this.scheduleChange({ type: "create", path: file.path });
      }
    };

    const onModify = (file: TAbstractFile) => {
      if (file instanceof TFile && !isSyncExcludedPath(file.path)) {
        this.scheduleChange({ type: "modify", path: file.path });
      }
    };

    const onDelete = (file: TAbstractFile) => {
      if (file instanceof TFile && !isSyncExcludedPath(file.path)) {
        this.scheduleChange({ type: "delete", path: file.path });
      }
    };

    this.vault.on("create", onCreate);
    this.vault.on("modify", onModify);
    this.vault.on("delete", onDelete);

    this.eventHandlers = [
      { name: "create", handler: onCreate },
      { name: "modify", handler: onModify },
      { name: "delete", handler: onDelete },
    ];
  }

  destroy() {
    for (const { name, handler } of this.eventHandlers) {
      this.vault.off(name, handler);
    }
    for (const timer of this.pendingChangeTimers.values()) {
      clearTimeout(timer);
    }
    this.eventHandlers = [];
    this.pendingChangeTimers = new Map();
  }

  private scheduleChange(change: FileChange) {
    const existing = this.pendingChangeTimers.get(change.path);
    if (existing) clearTimeout(existing);

    const timer = setTimeout(() => {
      this.pendingChangeTimers.delete(change.path);
      this.onChange(change);
    }, this.debounceMs);
    this.pendingChangeTimers.set(change.path, timer);
  }

  async getAllFiles(): Promise<{ path: string }[]> {
    const files: { path: string }[] = [];
    await this.listRecursive("", files);
    return files;
  }

  private async listRecursive(dir: string, result: { path: string }[]) {
    const listing = await this.vault.adapter.list(dir);
    for (const filePath of listing.files) {
      if (isSyncExcludedPath(filePath)) continue;
      const stat = await this.vault.adapter.stat(filePath);
      if (stat && stat.type === "file") {
        result.push({ path: filePath });
      }
    }
    for (const folder of listing.folders) {
      if (isSyncExcludedPath(folder)) continue;
      await this.listRecursive(folder, result);
    }
  }
}
