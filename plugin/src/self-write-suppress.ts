type SuppressEntry =
  | { path: string; operation: "write"; expectedHash: string; until: number }
  | { path: string; operation: "delete"; until: number };

export class SelfWriteSuppress {
  private entries = new Map<string, SuppressEntry>();

  constructor(private now: () => number = () => Date.now()) {}

  addWrite(path: string, expectedHash: string, until: number): void {
    this.entries.set(path, {
      path,
      operation: "write",
      expectedHash,
      until,
    });
  }

  addDelete(path: string, until: number): void {
    this.entries.set(path, {
      path,
      operation: "delete",
      until,
    });
  }

  consumeWrite(path: string, actualHash: string): boolean {
    const entry = this.entries.get(path);
    if (!entry || entry.until < this.now() || entry.operation !== "write") return false;
    if (entry.expectedHash !== actualHash) return false;
    this.entries.delete(path);
    return true;
  }

  consumeDelete(path: string, exists: boolean): boolean {
    const entry = this.entries.get(path);
    if (!entry || entry.until < this.now() || entry.operation !== "delete") return false;
    if (exists) return false;
    this.entries.delete(path);
    return true;
  }
}
