import { App, Notice, Vault, normalizePath } from "obsidian";
import { WsClient, ServerMessage, SyncConflictEntry, DownloadEntry } from "./ws-client";
import { FileWatcher } from "./file-watcher";
import { FileMetaStore, FileMeta } from "./file-meta-store";
import { ConflictQueue, ConflictEntry } from "./conflict-queue";
import { ConflictModal } from "./conflict-modal";
import { sha256 } from "./hash";

const BINARY_EXTENSIONS = new Set([
  "png", "jpg", "jpeg", "gif", "bmp", "svg", "webp",
  "pdf", "mp3", "mp4", "webm", "wav", "ogg",
  "zip", "tar", "gz",
]);

function isBinaryPath(path: string): boolean {
  const ext = path.split(".").pop()?.toLowerCase() || "";
  return BINARY_EXTENSIONS.has(ext);
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function base64ToArrayBuffer(base64: string): ArrayBuffer {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

export function makeConflictPath(path: string): string {
  const ts = new Date().toISOString().replace(/[:.]/g, "").slice(0, 15) + "Z";
  const lastDot = path.lastIndexOf(".");
  const lastSlash = path.lastIndexOf("/");
  if (lastDot === -1 || lastDot < lastSlash) {
    return `${path}.conflict-${ts}`;
  }
  return `${path.substring(0, lastDot)}.conflict-${ts}${path.substring(lastDot)}`;
}

export class SyncManager {
  private app: App;
  private vault: Vault;
  private wsClient: WsClient;
  private fileWatcher: FileWatcher;
  private fileMeta: FileMetaStore;
  private vaultName: string;
  private conflictQueue: ConflictQueue;
  private conflictModal: ConflictModal | null = null;
  private syncing = false;

  constructor(
    app: App,
    vault: Vault,
    serverUrl: string,
    token: string,
    vaultName: string,
    fileMeta: FileMetaStore,
  ) {
    this.app = app;
    this.vault = vault;
    this.vaultName = vaultName;
    this.fileMeta = fileMeta;
    this.wsClient = new WsClient(serverUrl, token);
    this.conflictQueue = new ConflictQueue();
    this.fileWatcher = new FileWatcher(
      vault,
      (change) => this.handleLocalChange(change),
    );
  }

  async start(): Promise<boolean> {
    this.wsClient.on("sync_result", (msg) => this.handleSyncResult(msg));
    this.wsClient.on("file_create_result", (msg) => this.handleFileCreateResult(msg));
    this.wsClient.on("file_update_result", (msg) => this.handleFileUpdateResult(msg));
    this.wsClient.on("file_delete_result", (msg) => this.handleFileDeleteResult(msg));
    this.wsClient.on("file_check_result", (msg) => this.handleFileCheckResult(msg));
    this.wsClient.on("conflict_resolve_result", (msg) => this.handleConflictResolveResult(msg));
    this.wsClient.on("reconnected", () => this.performSyncInit());

    this.fileWatcher.start();
    this.wsClient.startHealthCheck();

    try {
      await this.wsClient.connect();
      await this.performSyncInit();
      return true;
    } catch (err) {
      console.error("[obsidian-goat-sync] Initial connect failed, healthcheck will retry:", err);
      return false;
    }
  }

  async stop() {
    this.wsClient.disconnect();
    this.fileWatcher.destroy();
    await this.fileMeta.flush();
  }

  private async performSyncInit() {
    const localFiles = await this.fileWatcher.getAllFiles();
    const files = await Promise.all(
      localFiles.map(async ({ path }) => {
        const hash = await this.computeFileHash(path);
        if (hash === null) return null;
        const meta = this.fileMeta.get(path);
        return {
          path,
          prevServerVersion: meta?.prevServerVersion,
          prevServerHash: meta?.prevServerHash,
          currentClientHash: hash,
        };
      }),
    );
    const validFiles = files.filter((f): f is NonNullable<typeof f> => f !== null);
    this.wsClient.sendSyncInit(this.vaultName, validFiles);
  }

  private async handleLocalChange(change: { type: "create" | "modify" | "delete"; path: string }) {
    if (this.syncing) return;

    if (change.type === "delete") {
      const meta = this.fileMeta.get(change.path);
      if (meta) {
        this.wsClient.sendFileDelete(this.vaultName, change.path, meta.prevServerVersion);
      }
      return;
    }

    const content = await this.readFileContent(change.path);
    if (content === null) return;

    const hash = await this.computeHashFromContent(change.path, content);
    const encoding = isBinaryPath(change.path) ? "base64" : undefined;

    if (change.type === "create") {
      this.wsClient.sendFileCreate(this.vaultName, change.path, content, hash, encoding);
    } else {
      const meta = this.fileMeta.get(change.path);
      if (meta && meta.prevServerHash === hash) {
        return;
      }
      if (meta) {
        this.wsClient.sendFileUpdate(this.vaultName, change.path, content, meta.prevServerVersion, hash, encoding);
      } else {
        this.wsClient.sendFileCreate(this.vaultName, change.path, content, hash, encoding);
      }
    }
  }

  private async handleSyncResult(msg: ServerMessage) {
    this.syncing = true;
    try {
      if (msg.toDownload) {
        for (const entry of msg.toDownload) {
          await this.applyDownloadEntry(entry);
        }
      }

      if (msg.toDelete) {
        for (const path of msg.toDelete) {
          await this.deleteLocalFile(path);
          this.fileMeta.remove(path);
        }
      }

      if (msg.toUpdateMeta) {
        for (const entry of msg.toUpdateMeta) {
          this.fileMeta.set(entry.path, {
            prevServerVersion: entry.currentServerVersion,
            prevServerHash: entry.currentServerHash,
          });
        }
      }

      if (msg.toUpload) {
        for (const path of msg.toUpload) {
          await this.uploadFile(path);
        }
      }

      if (msg.toUpdate) {
        for (const path of msg.toUpdate) {
          await this.updateFile(path);
        }
      }

      if (msg.conflicts && msg.conflicts.length > 0) {
        for (const c of msg.conflicts) {
          await this.enqueueConflict(c);
        }
        this.openConflictModal();
      }
    } finally {
      this.syncing = false;
    }
  }

  private async handleFileCreateResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.ok && msg.currentServerVersion !== undefined && msg.currentServerHash) {
      this.fileMeta.set(msg.path, {
        prevServerVersion: msg.currentServerVersion,
        prevServerHash: msg.currentServerHash,
      });
    } else if (!msg.ok && msg.conflict) {
      const clientContent = await this.readFileContent(msg.path) || "";
      const clientHash = await this.computeHashFromContent(msg.path, clientContent);
      const entry: ConflictEntry = {
        path: msg.path,
        currentClientContent: clientContent,
        currentClientHash: clientHash,
        currentServerVersion: msg.conflict.currentServerVersion,
        currentServerHash: msg.conflict.currentServerHash,
        currentServerContent: msg.conflict.currentServerContent,
        encoding: msg.conflict.encoding,
        kind: "modify",
        conflictPath: makeConflictPath(msg.path),
      };
      this.conflictQueue.add(entry);
      this.openConflictModal();
    }
  }

  private async handleFileUpdateResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.ok) {
      if (msg.currentServerVersion !== undefined && msg.currentServerHash) {
        this.fileMeta.set(msg.path, {
          prevServerVersion: msg.currentServerVersion,
          prevServerHash: msg.currentServerHash,
        });
      }
    } else if (msg.conflict) {
      const clientContent = await this.readFileContent(msg.path) || "";
      const clientHash = await this.computeHashFromContent(msg.path, clientContent);
      const entry: ConflictEntry = {
        path: msg.path,
        prevServerVersion: msg.conflict.currentServerVersion,
        currentClientContent: clientContent,
        currentClientHash: clientHash,
        currentServerVersion: msg.conflict.currentServerVersion,
        currentServerHash: msg.conflict.currentServerHash,
        currentServerContent: msg.conflict.currentServerContent,
        encoding: msg.conflict.encoding,
        kind: "modify",
        conflictPath: makeConflictPath(msg.path),
      };
      this.conflictQueue.add(entry);
      this.openConflictModal();
    }
  }

  private async handleFileDeleteResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.ok) {
      this.fileMeta.remove(msg.path);
    } else if (msg.conflict) {
      const entry: ConflictEntry = {
        path: msg.path,
        prevServerVersion: msg.conflict.currentServerVersion,
        currentClientContent: "",
        currentClientHash: "",
        currentServerVersion: msg.conflict.currentServerVersion,
        currentServerHash: msg.conflict.currentServerHash,
        currentServerContent: msg.conflict.currentServerContent,
        encoding: msg.conflict.encoding,
        kind: "delete",
      };
      this.conflictQueue.add(entry);
      this.openConflictModal();
    }
  }

  private async handleFileCheckResult(msg: ServerMessage) {
    if (!msg.path) return;
    switch (msg.action) {
      case "up-to-date":
        break;
      case "update-meta":
        if (msg.currentServerVersion !== undefined && msg.currentServerHash) {
          this.fileMeta.set(msg.path, {
            prevServerVersion: msg.currentServerVersion,
            prevServerHash: msg.currentServerHash,
          });
        }
        break;
      case "download":
        if (msg.content && msg.currentServerVersion !== undefined && msg.currentServerHash) {
          await this.applyDownloadEntry({
            path: msg.path,
            content: msg.content,
            currentServerVersion: msg.currentServerVersion,
            currentServerHash: msg.currentServerHash,
            encoding: msg.encoding,
          });
        }
        break;
      case "conflict":
        if (msg.conflict) {
          const clientContent = await this.readFileContent(msg.path) || "";
          const clientHash = await this.computeHashFromContent(msg.path, clientContent);
          const entry: ConflictEntry = {
            path: msg.path,
            currentClientContent: clientContent,
            currentClientHash: clientHash,
            currentServerVersion: msg.conflict.currentServerVersion,
            currentServerHash: msg.conflict.currentServerHash,
            currentServerContent: msg.conflict.currentServerContent,
            encoding: msg.conflict.encoding,
            kind: "modify",
            conflictPath: makeConflictPath(msg.path),
          };
          this.conflictQueue.add(entry);
          this.openConflictModal();
        }
        break;
      case "deleted":
        await this.deleteLocalFile(msg.path);
        this.fileMeta.remove(msg.path);
        break;
    }
  }

  private async handleConflictResolveResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.ok) {
      const existing = this.conflictQueue.get(msg.path);
      if (existing?.kind === "delete") {
        this.fileMeta.remove(msg.path);
      } else if (msg.currentServerVersion !== undefined && msg.currentServerHash) {
        this.fileMeta.set(msg.path, {
          prevServerVersion: msg.currentServerVersion,
          prevServerHash: msg.currentServerHash,
        });
      }
      this.conflictQueue.remove(msg.path);
      if (this.conflictQueue.size() === 0) {
        this.conflictModal?.close();
        this.conflictModal = null;
      } else {
        this.conflictModal?.refreshIfOpen(msg.path);
      }
    } else if (msg.conflict) {
      const existing = this.conflictQueue.get(msg.path);
      if (existing) {
        existing.currentServerVersion = msg.conflict.currentServerVersion;
        existing.currentServerHash = msg.conflict.currentServerHash;
        existing.currentServerContent = msg.conflict.currentServerContent;
        existing.prevServerVersion = msg.conflict.currentServerVersion;
        existing.selection = undefined;
        new Notice("[obsidian-goat-sync] 서버에 더 최신 변경이 있습니다: " + msg.path);
        this.conflictModal?.refreshIfOpen(msg.path);
      }
    }
  }

  async applyConflictResolution(entry: ConflictEntry): Promise<void> {
    this.syncing = true;
    try {
      switch (entry.selection) {
        case "server":
          await this.applyServerContent(entry);
          this.conflictQueue.remove(entry.path);
          break;
        case "local":
          await this.applyLocalContent(entry);
          break;
        case "new":
          await this.applyNewSave(entry);
          this.conflictQueue.remove(entry.path);
          break;
      }
    } finally {
      this.syncing = false;
    }
  }

  private async applyServerContent(entry: ConflictEntry): Promise<void> {
    await this.writeFileContent(entry.path, entry.currentServerContent, entry.encoding);
    this.fileMeta.set(entry.path, {
      prevServerVersion: entry.currentServerVersion,
      prevServerHash: entry.currentServerHash,
    });
  }

  private async applyLocalContent(entry: ConflictEntry): Promise<void> {
    if (entry.kind === "delete") {
      this.wsClient.sendConflictResolveLocalDelete(
        this.vaultName,
        entry.path,
        entry.currentServerVersion,
      );
    } else {
      const encoding = isBinaryPath(entry.path) ? "base64" : undefined;
      this.wsClient.sendConflictResolveLocal(
        this.vaultName,
        entry.path,
        entry.currentClientContent,
        entry.currentClientHash,
        entry.currentServerVersion,
        encoding,
      );
    }
  }

  private async applyNewSave(entry: ConflictEntry): Promise<void> {
    const conflictPath = entry.conflictPath || makeConflictPath(entry.path);
    await this.writeFileContent(conflictPath, entry.currentClientContent, entry.encoding);
    const hash = await this.computeHashFromContent(conflictPath, entry.currentClientContent);
    const encoding = isBinaryPath(conflictPath) ? "base64" : undefined;
    this.wsClient.sendFileCreate(this.vaultName, conflictPath, entry.currentClientContent, hash, encoding);

    await this.writeFileContent(entry.path, entry.currentServerContent, entry.encoding);
    this.fileMeta.set(entry.path, {
      prevServerVersion: entry.currentServerVersion,
      prevServerHash: entry.currentServerHash,
    });
  }

  private async enqueueConflict(c: SyncConflictEntry): Promise<void> {
    const clientContent = await this.readFileContent(c.path) || "";
    const entry: ConflictEntry = {
      path: c.path,
      prevServerVersion: c.prevServerVersion,
      currentClientContent: clientContent,
      currentClientHash: c.currentClientHash,
      currentServerVersion: c.currentServerVersion,
      currentServerHash: c.currentServerHash,
      currentServerContent: c.currentServerContent,
      encoding: c.encoding,
      kind: "modify",
      conflictPath: makeConflictPath(c.path),
    };
    this.conflictQueue.add(entry);
  }

  private openConflictModal() {
    if (this.conflictModal && this.conflictModal.containerEl.isConnected) {
      return;
    }
    this.conflictModal = new ConflictModal(this.app, this.conflictQueue, this);
    this.conflictModal.open();
  }

  private async uploadFile(path: string) {
    const content = await this.readFileContent(path);
    if (content === null) return;
    const hash = await this.computeHashFromContent(path, content);
    const encoding = isBinaryPath(path) ? "base64" : undefined;
    this.wsClient.sendFileCreate(this.vaultName, path, content, hash, encoding);
  }

  private async updateFile(path: string) {
    const content = await this.readFileContent(path);
    if (content === null) return;
    const hash = await this.computeHashFromContent(path, content);
    const meta = this.fileMeta.get(path);
    if (!meta) {
      const encoding = isBinaryPath(path) ? "base64" : undefined;
      this.wsClient.sendFileCreate(this.vaultName, path, content, hash, encoding);
      return;
    }
    const encoding = isBinaryPath(path) ? "base64" : undefined;
    this.wsClient.sendFileUpdate(this.vaultName, path, content, meta.prevServerVersion, hash, encoding);
  }

  private async applyDownloadEntry(entry: DownloadEntry) {
    await this.writeFileContent(entry.path, entry.content, entry.encoding);
    this.fileMeta.set(entry.path, {
      prevServerVersion: entry.currentServerVersion,
      prevServerHash: entry.currentServerHash,
    });
  }

  private async writeFileContent(path: string, content: string, encoding?: string): Promise<void> {
    const normalized = normalizePath(path);
    const dir = normalized.includes("/") ? normalized.substring(0, normalized.lastIndexOf("/")) : "";
    if (dir && !(await this.vault.adapter.exists(dir))) {
      await this.vault.adapter.mkdir(dir);
    }
    if (encoding === "base64") {
      await this.vault.adapter.writeBinary(normalized, base64ToArrayBuffer(content));
    } else {
      await this.vault.adapter.write(normalized, content);
    }
  }

  private async deleteLocalFile(path: string): Promise<void> {
    const normalized = normalizePath(path);
    if (await this.vault.adapter.exists(normalized)) {
      await this.vault.adapter.remove(normalized);
    }
  }

  private async readFileContent(path: string): Promise<string | null> {
    const exists = await this.vault.adapter.exists(path);
    if (!exists) return null;

    if (isBinaryPath(path)) {
      const buffer = await this.vault.adapter.readBinary(path);
      return arrayBufferToBase64(buffer);
    }
    return await this.vault.adapter.read(path);
  }

  private async computeFileHash(path: string): Promise<string | null> {
    const exists = await this.vault.adapter.exists(path);
    if (!exists) return null;

    if (isBinaryPath(path)) {
      const buffer = await this.vault.adapter.readBinary(path);
      return await sha256(buffer);
    }
    const content = await this.vault.adapter.read(path);
    return await sha256(content);
  }

  private async computeHashFromContent(path: string, content: string): Promise<string> {
    if (isBinaryPath(path)) {
      const buf = base64ToArrayBuffer(content);
      return await sha256(buf);
    }
    return await sha256(content);
  }

  checkFileOnOpen(path: string): void {
    this.computeFileHash(path).then((hash) => {
      if (hash === null) return;
      const meta = this.fileMeta.get(path);
      this.wsClient.sendFileCheck(this.vaultName, {
        path,
        prevServerVersion: meta?.prevServerVersion,
        prevServerHash: meta?.prevServerHash,
        currentClientHash: hash,
      });
    });
  }
}
