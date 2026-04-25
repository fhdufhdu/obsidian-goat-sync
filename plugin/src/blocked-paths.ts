export type BlockReason = "conflict" | "deleteConflict";

export interface BlockedPath {
  path: string;
  reason: BlockReason;
  serverVersion: number;
  serverHash: string;
  isDeleted: boolean;
  createdAt: number;
}

export class BlockedPaths {
  private entries = new Map<string, BlockedPath>();

  block(input: Omit<BlockedPath, "createdAt">): void {
    this.entries.set(input.path, {
      ...input,
      createdAt: Date.now(),
    });
  }

  clear(path: string): void {
    this.entries.delete(path);
  }

  has(path: string): boolean {
    return this.entries.has(path);
  }

  list(): BlockedPath[] {
    return Array.from(this.entries.values()).map((entry) => ({ ...entry }));
  }
}
