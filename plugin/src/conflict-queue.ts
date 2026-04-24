export interface ConflictEntry {
  path: string;
  prevServerVersion?: number;
  currentClientContent: string;
  currentClientHash: string;
  currentServerVersion: number;
  currentServerHash: string;
  currentServerContent: string;
  encoding?: string;
  kind: "modify" | "delete";
  selection?: "server" | "local" | "new";
  conflictPath?: string;
}

export class ConflictQueue {
  private entries: Map<string, ConflictEntry> = new Map();

  add(entry: ConflictEntry): void {
    this.entries.set(entry.path, entry);
  }

  list(): ConflictEntry[] {
    return Array.from(this.entries.values());
  }

  get(path: string): ConflictEntry | undefined {
    return this.entries.get(path);
  }

  selectAt(path: string, choice: "server" | "local" | "new"): void {
    const entry = this.entries.get(path);
    if (entry) {
      entry.selection = choice;
    }
  }

  remove(path: string): void {
    this.entries.delete(path);
  }

  isAllResolved(): boolean {
    if (this.entries.size === 0) return false;
    for (const entry of this.entries.values()) {
      if (!entry.selection) return false;
    }
    return true;
  }

  size(): number {
    return this.entries.size;
  }

  clear(): void {
    this.entries.clear();
  }
}
