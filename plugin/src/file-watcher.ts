import { Vault, TFile, TAbstractFile } from "obsidian";

function isExcluded(path: string): boolean {
  return path === ".obsidian/plugins" || path.startsWith(".obsidian/plugins/");
}

export interface FileChange {
  type: "create" | "modify" | "delete";
  path: string;
}

export class FileWatcher {
  private vault: Vault;
  private onChange: (change: FileChange) => void;
  private eventHandlers: Array<{ name: string; handler: (file: TAbstractFile) => void }> = [];

  constructor(
    vault: Vault,
    onChange: (change: FileChange) => void,
  ) {
    this.vault = vault;
    this.onChange = onChange;
  }

  start() {
    const onCreate = (file: TAbstractFile) => {
      if (file instanceof TFile && !isExcluded(file.path)) {
        this.onChange({ type: "create", path: file.path });
      }
    };

    const onModify = (file: TAbstractFile) => {
      if (file instanceof TFile && !isExcluded(file.path)) {
        this.onChange({ type: "modify", path: file.path });
      }
    };

    const onDelete = (file: TAbstractFile) => {
      if (file instanceof TFile && !isExcluded(file.path)) {
        this.onChange({ type: "delete", path: file.path });
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
    this.eventHandlers = [];
  }

  async getAllFiles(): Promise<{ path: string }[]> {
    const files: { path: string }[] = [];
    await this.listRecursive("", files);
    return files;
  }

  private async listRecursive(dir: string, result: { path: string }[]) {
    const listing = await this.vault.adapter.list(dir);
    for (const filePath of listing.files) {
      if (isExcluded(filePath)) continue;
      const stat = await this.vault.adapter.stat(filePath);
      if (stat && stat.type === "file") {
        result.push({ path: filePath });
      }
    }
    for (const folder of listing.folders) {
      if (isExcluded(folder)) continue;
      await this.listRecursive(folder, result);
    }
  }
}
