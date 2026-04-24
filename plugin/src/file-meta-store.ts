export interface FileMeta {
  prevServerVersion: number;
  prevServerHash: string;
}

export class FileMetaStore {
  private data: Record<string, FileMeta> = {};
  private saveTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly onSave: (data: Record<string, FileMeta>) => Promise<void>;

  constructor(
    initial: Record<string, FileMeta>,
    onSave: (data: Record<string, FileMeta>) => Promise<void>,
  ) {
    this.data = { ...initial };
    this.onSave = onSave;
  }

  get(path: string): FileMeta | undefined {
    return this.data[path];
  }

  set(path: string, meta: FileMeta): void {
    this.data[path] = meta;
    this.scheduleSave();
  }

  remove(path: string): void {
    delete this.data[path];
    this.scheduleSave();
  }

  entries(): [string, FileMeta][] {
    return Object.entries(this.data);
  }

  getAll(): Record<string, FileMeta> {
    return { ...this.data };
  }

  private scheduleSave(): void {
    if (this.saveTimer) clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => {
      this.saveTimer = null;
      this.onSave(this.data).catch((err) =>
        console.error("[obsidian-goat-sync] Failed to save file meta:", err),
      );
    }, 500);
  }

  async flush(): Promise<void> {
    if (this.saveTimer) {
      clearTimeout(this.saveTimer);
      this.saveTimer = null;
      await this.onSave(this.data);
    }
  }
}
