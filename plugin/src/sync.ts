import { App, Notice, Vault, normalizePath } from "obsidian";
import { WsClient, ServerMessage, SyncConflictEntry, DownloadEntry, FilePayload } from "./ws-client";
import { FileWatcher } from "./file-watcher";
import { FileMetaStore } from "./file-meta-store";
import { ConflictQueue, ConflictEntry } from "./conflict-queue";
import { DeleteQueue } from "./delete-queue";
import { DirtyQueue } from "./dirty-queue";
import { BlockedPaths } from "./blocked-paths";
import { SelfWriteSuppress } from "./self-write-suppress";
import { ConflictModal } from "./conflict-modal";
import { SyncOrchestrator, FlushResult } from "./sync-orchestrator";
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
  private deleteQueue: DeleteQueue;
  private dirtyQueue: DirtyQueue;
  private blockedPaths: BlockedPaths;
  private selfWriteSuppress: SelfWriteSuppress;
  private orchestrator: SyncOrchestrator;

  constructor(
    app: App,
    vault: Vault,
    serverUrl: string,
    token: string,
    vaultName: string,
    fileMeta: FileMetaStore,
    deleteQueuePath: string,
  ) {
    this.app = app;
    this.vault = vault;
    this.vaultName = vaultName;
    this.fileMeta = fileMeta;
    this.wsClient = new WsClient(serverUrl, token);
    this.conflictQueue = new ConflictQueue();
    this.deleteQueue = new DeleteQueue(this.vault.adapter, deleteQueuePath);
    this.dirtyQueue = new DirtyQueue();
    this.blockedPaths = new BlockedPaths();
    this.selfWriteSuppress = new SelfWriteSuppress();
    this.orchestrator = new SyncOrchestrator({
      flushDeleteQueue: () => this.flushDeleteQueue(),
      flushDirtyQueue: () => this.flushDirtyQueue(),
      runSyncInit: () => this.performSyncInit(),
      notifyTransientFailure: () => new Notice("[obsidian-goat-sync] 서버 연결이 불안정해서 동기화가 중지됩니다"),
    });
    this.fileWatcher = new FileWatcher(
      vault,
      (change) => this.handleLocalChange(change),
    );
  }

  async start(): Promise<boolean> {
    this.wsClient.on("syncResult", (msg) => this.handleSyncResult(msg));
    this.wsClient.on("sync_result", (msg) => this.handleSyncResult(msg));

    this.wsClient.on("fileCheckResult", (msg) => this.handleFileCheckResult(msg));
    this.wsClient.on("file_check_result", (msg) => this.handleFileCheckResult(msg));

    this.wsClient.on("filePutResult", (msg) => this.handleFilePutResult(msg));
    this.wsClient.on("fileDeleteResult", (msg) => this.handleFileDeleteResult(msg));
    this.wsClient.on("conflictResolveResult", (msg) => this.handleConflictResolveResult(msg));
    this.wsClient.on("conflict_resolve_result", (msg) => this.handleConflictResolveResult(msg));
    this.wsClient.on("reconnected", () => {
      this.orchestrator.runStartupSync().catch((err) => {
        console.error("[obsidian-goat-sync] Reconnected sync failed:", err);
      });
    });

    this.fileWatcher.start();
    this.wsClient.startHealthCheck();

    try {
      await this.deleteQueue.load();
      await this.wsClient.connect();
      await this.orchestrator.runStartupSync();
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
    const localPaths = new Set(localFiles.map((f) => f.path));
    const files: FilePayload[] = [];

    for (const { path } of localFiles) {
      if (this.blockedPaths.has(path)) continue;
      const localHash = await this.computeFileHash(path);
      if (localHash === null) continue;
      const meta = this.fileMeta.get(path);
      files.push({
        path,
        exists: true,
        baseVersion: meta?.prevServerVersion,
        baseHash: meta?.prevServerHash,
        localHash,
      });
    }

    for (const [path, meta] of this.fileMeta.entries()) {
      if (localPaths.has(path) || this.blockedPaths.has(path)) continue;
      files.push({
        path,
        exists: false,
        baseVersion: meta.prevServerVersion,
        baseHash: meta.prevServerHash,
      });
    }

    const validFiles = files;
    if (!this.wsClient.sendSyncInit(this.vaultName, validFiles)) {
      console.warn("[obsidian-goat-sync] Skipped syncInit because websocket is not open");
    }
  }

  private async handleLocalChange(change: { type: "create" | "modify" | "delete"; path: string }) {
    if (change.type === "delete") {
      this.blockedPaths.clear(change.path);
      const exists = await this.vault.adapter.exists(change.path);
      if (this.selfWriteSuppress.consumeDelete(change.path, exists)) return;

      const meta = this.fileMeta.get(change.path);
      if (meta) {
        await this.deleteQueue.enqueue({ path: change.path, baseVersion: meta.prevServerVersion, serverHash: meta.prevServerHash });
        await this.dirtyQueue.remove(change.path);
      }
      this.orchestrator.runIntervalWorker().catch((err) => {
        console.error("[obsidian-goat-sync] Failed to run interval sync:", err);
      });
      return;
    }

    this.blockedPaths.clear(change.path);
    const hash = await this.computeFileHash(change.path);
    if (hash === null) return;
    if (this.selfWriteSuppress.consumeWrite(change.path, hash)) return;

    const meta = this.fileMeta.get(change.path);
    await this.dirtyQueue.enqueue({ path: change.path, baseVersion: meta?.prevServerVersion, lastSeenHash: hash });
    this.orchestrator.runIntervalWorker().catch((err) => {
      console.error("[obsidian-goat-sync] Failed to run interval sync:", err);
    });
  }

  private async handleSyncResult(msg: ServerMessage) {
    if (msg.toDownload) {
      for (const entry of msg.toDownload) {
        await this.applyDownloadEntry(entry);
      }
    }

    if (msg.toDeleteLocal) {
      for (const entry of msg.toDeleteLocal) {
        if (!entry.path) continue;
        await this.applyServerDelete(entry.path, entry.serverVersion, entry.serverHash || "");
      }
    }

    if (msg.toUpdateMeta) {
      for (const entry of msg.toUpdateMeta) {
        if (!entry.path) continue;
        this.fileMeta.set(entry.path, {
          prevServerVersion: entry.serverVersion,
          prevServerHash: entry.serverHash || "",
        });
      }
    }

    if (msg.toRemoveMeta) {
      for (const entry of msg.toRemoveMeta) {
        if (entry.path) this.fileMeta.remove(entry.path);
      }
    }

    if (msg.toPut) {
      for (const path of msg.toPut) {
        await this.updateFile(path);
      }
    }

    if (msg.conflicts && msg.conflicts.length > 0) {
      for (const c of msg.conflicts) {
        await this.enqueueConflict(c);
      }
      this.openConflictModal();
    }
  }

  private async handleFilePutResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.action === "okUpdateMeta" && msg.meta) {
      this.fileMeta.set(msg.path, {
        prevServerVersion: msg.meta.serverVersion,
        prevServerHash: msg.meta.serverHash || "",
      });
      const entry = this.dirtyQueue.get(msg.path);
      if (entry?.sentHash) {
        await this.dirtyQueue.completeSuccess(msg.path, entry.sentHash, {
          serverVersion: msg.meta.serverVersion,
          serverHash: msg.meta.serverHash || "",
        });
      }
    } else if (msg.action === "toDeleteLocal" && msg.meta) {
      await this.applyServerDelete(msg.path, msg.meta.serverVersion, msg.meta.serverHash || "");
      await this.dirtyQueue.remove(msg.path);
    } else if ((msg.action === "conflict" || msg.action === "deleteConflict") && msg.conflict) {
      await this.dirtyQueue.remove(msg.path);
      this.blockedPaths.block({
        path: msg.path,
        reason: msg.action === "deleteConflict" ? "deleteConflict" : "conflict",
        serverVersion: msg.conflict.serverVersion,
        serverHash: msg.conflict.serverHash,
        isDeleted: msg.action === "deleteConflict",
      });
      await this.enqueueLatestConflict(msg);
    }
  }

  private async handleFileDeleteResult(msg: ServerMessage) {
    if (!msg.path) return;
    if (msg.action === "okRemoveMeta") {
      this.fileMeta.remove(msg.path);
      await this.deleteQueue.remove(msg.path);
    } else if (msg.action === "okUpdateMeta" && msg.meta) {
      this.fileMeta.set(msg.path, {
        prevServerVersion: msg.meta.serverVersion,
        prevServerHash: msg.meta.serverHash || "",
      });
      await this.deleteQueue.remove(msg.path);
    } else if (msg.action === "deleteConflict" && msg.conflict) {
      await this.deleteQueue.remove(msg.path);
      this.blockedPaths.block({
        path: msg.path,
        reason: "deleteConflict",
        serverVersion: msg.conflict.serverVersion,
        serverHash: msg.conflict.serverHash,
        isDeleted: true,
      });
      await this.enqueueLatestConflict(msg);
    }
  }

  private async handleFileCheckResult(msg: ServerMessage) {
    if (!msg.path || !msg.action) return;
    switch (msg.action) {
      case "upToDate":
        if (msg.meta?.serverVersion !== undefined) {
          this.fileMeta.set(msg.path, {
            prevServerVersion: msg.meta.serverVersion,
            prevServerHash: msg.meta.serverHash || "",
          });
        }
        break;
      case "updateMeta":
        if (msg.meta?.serverVersion !== undefined) {
          this.fileMeta.set(msg.path, {
            prevServerVersion: msg.meta.serverVersion,
            prevServerHash: msg.meta.serverHash || "",
          });
        }
        break;
      case "toDownload":
        if (msg.content && msg.meta) {
          await this.applyDownloadEntry({
            path: msg.path,
            content: msg.content,
            serverVersion: msg.meta.serverVersion,
            serverHash: msg.meta.serverHash || "",
            encoding: msg.encoding,
          });
        }
        break;
      case "put":
        const dirtyEntry = this.dirtyQueue.get(msg.path);
        if (dirtyEntry) {
          await this.putDirtyFile(dirtyEntry);
        } else {
          await this.updateFile(msg.path);
        }
        break;
      case "conflict":
      case "deleteConflict":
        if (msg.conflict) {
          await this.enqueueLatestConflict(msg);
        }
        break;
      case "toDeleteLocal":
        if (msg.meta) {
          await this.applyServerDelete(msg.path, msg.meta.serverVersion, msg.meta.serverHash || "");
        }
        break;
      case "toRemoveMeta":
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
      } else if (msg.meta) {
        this.fileMeta.set(msg.path, {
          prevServerVersion: msg.meta.serverVersion,
          prevServerHash: msg.meta.serverHash || "",
        });
      }
      this.conflictQueue.remove(msg.path);
      if (this.conflictQueue.size() === 0) {
        this.conflictModal?.close();
        this.conflictModal = null;
      } else {
        this.conflictModal?.refreshIfOpen(msg.path);
      }
      this.blockedPaths.clear(msg.path);
    } else if (msg.conflict) {
      const existing = this.conflictQueue.get(msg.path);
      if (existing) {
        existing.currentServerVersion = msg.conflict.serverVersion;
        existing.currentServerHash = msg.conflict.serverHash;
        existing.currentServerContent = msg.conflict.serverContent;
        existing.prevServerVersion = msg.conflict.serverVersion;
        existing.selection = undefined;
        new Notice("[obsidian-goat-sync] 서버에 더 최신 변경이 있습니다: " + msg.path);
        this.conflictModal?.refreshIfOpen(msg.path);
      }
    }
  }

  async applyConflictResolution(entry: ConflictEntry): Promise<void> {
    switch (entry.selection) {
      case "server":
        await this.applyServerContent(entry);
        this.conflictQueue.remove(entry.path);
        this.blockedPaths.clear(entry.path);
        break;
      case "local":
        await this.applyLocalContent(entry);
        break;
      case "new":
        await this.applyNewSave(entry);
        this.conflictQueue.remove(entry.path);
        this.blockedPaths.clear(entry.path);
        break;
    }
  }

  private async applyServerContent(entry: ConflictEntry): Promise<void> {
    this.selfWriteSuppress.addWrite(entry.path, entry.currentServerHash, Date.now() + 5000);
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
    this.selfWriteSuppress.addWrite(conflictPath, hash, Date.now() + 5000);
    const sent = this.wsClient.sendFilePut(this.vaultName, conflictPath, entry.currentClientContent, {
      path: conflictPath,
      exists: true,
      localHash: hash,
    }, encoding);
    if (!sent) {
      await this.dirtyQueue.enqueue({ path: conflictPath, lastSeenHash: hash });
    }

    this.selfWriteSuppress.addWrite(entry.path, entry.currentServerHash, Date.now() + 5000);
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
      prevServerVersion: c.baseVersion,
      currentClientContent: clientContent,
      currentClientHash: c.localHash,
      currentServerVersion: c.serverVersion,
      currentServerHash: c.serverHash,
      currentServerContent: c.serverContent,
      encoding: c.encoding,
      kind: c.isDeleted ? "delete" : "modify",
      conflictPath: makeConflictPath(c.path),
    };
    this.conflictQueue.add(entry);
  }

  private async enqueueLatestConflict(msg: ServerMessage): Promise<void> {
    if (!msg.path || !msg.conflict) return;

    const clientContent = await this.readFileContent(msg.path) || "";
    const clientHash = await this.computeHashFromContent(msg.path, clientContent);
    const entry: ConflictEntry = {
      path: msg.path,
      prevServerVersion: msg.conflict.serverVersion,
      currentClientContent: clientContent,
      currentClientHash: clientHash,
      currentServerVersion: msg.conflict.serverVersion,
      currentServerHash: msg.conflict.serverHash,
      currentServerContent: msg.conflict.serverContent,
      encoding: msg.conflict.encoding,
      kind: msg.action === "deleteConflict" ? "delete" : "modify",
      conflictPath: makeConflictPath(msg.path),
    };
    this.conflictQueue.add(entry);
    this.openConflictModal();
  }

  private openConflictModal() {
    if (this.conflictModal && this.conflictModal.containerEl.isConnected) {
      return;
    }
    this.conflictModal = new ConflictModal(this.app, this.conflictQueue, this);
    this.conflictModal.open();
  }

  private async updateFile(path: string) {
    const content = await this.readFileContent(path);
    if (content === null) return;
    const hash = await this.computeHashFromContent(path, content);
    const meta = this.fileMeta.get(path);
    if (!meta) {
      const encoding = isBinaryPath(path) ? "base64" : undefined;
      const sent = this.wsClient.sendFilePut(this.vaultName, path, content, {
        path,
        exists: true,
        localHash: hash,
      }, encoding);
      if (!sent) {
        await this.dirtyQueue.enqueue({ path, lastSeenHash: hash });
      }
      return;
    }
    const encoding = isBinaryPath(path) ? "base64" : undefined;
    const sent = this.wsClient.sendFilePut(this.vaultName, path, content, {
      path,
      exists: true,
      baseVersion: meta.prevServerVersion,
      baseHash: meta.prevServerHash,
      localHash: hash,
    }, encoding);
    if (!sent) {
      await this.dirtyQueue.enqueue({ path, baseVersion: meta.prevServerVersion, lastSeenHash: hash });
    }
  }

  private async applyDownloadEntry(entry: DownloadEntry) {
    const localHash = entry.encoding === "base64"
      ? await sha256(base64ToArrayBuffer(entry.content))
      : await sha256(entry.content);
    this.selfWriteSuppress.addWrite(entry.path, localHash, Date.now() + 5000);
    await this.writeFileContent(entry.path, entry.content, entry.encoding);
    this.fileMeta.set(entry.path, {
      prevServerVersion: entry.serverVersion,
      prevServerHash: entry.serverHash,
    });
  }

  private async applyServerDelete(path: string, serverVersion: number, serverHash: string): Promise<void> {
    this.selfWriteSuppress.addDelete(path, Date.now() + 5000);
    await this.deleteQueue.remove(path);
    await this.deleteLocalFile(path);
    this.fileMeta.set(path, {
      prevServerVersion: serverVersion,
      prevServerHash: serverHash,
    });
  }

  private async flushDeleteQueue(): Promise<FlushResult> {
    const entries = this.deleteQueue.list();
    for (const entry of entries) {
      if (entry.status !== "pending" && entry.status !== "retryableFailed") continue;

      try {
        const exists = await this.vault.adapter.exists(entry.path);
        if (exists) {
          await this.deleteQueue.remove(entry.path);
          const hash = await this.computeFileHash(entry.path);
          await this.dirtyQueue.enqueue({
            path: entry.path,
            baseVersion: entry.baseVersion,
            lastSeenHash: hash || this.fileMeta.get(entry.path)?.prevServerHash || "",
          });
          continue;
        }

        const payloadBaseHash = this.fileMeta.get(entry.path)?.prevServerHash || entry.serverHash;
        const sent = this.wsClient.sendFileDelete(this.vaultName, {
          path: entry.path,
          exists: false,
          baseVersion: entry.baseVersion,
          baseHash: payloadBaseHash,
        });
        if (!sent) {
          return "transientFailure";
        }
      } catch (err) {
        console.error("[obsidian-goat-sync] Failed to flush delete queue entry:", err);
        await this.deleteQueue.remove(entry.path);
        return "transientFailure";
      }
    }
    return "ok";
  }

  private async flushDirtyQueue(): Promise<FlushResult> {
    while (true) {
      const next = await this.dirtyQueue.claimNext();
      if (!next) return "ok";

      try {
        await this.putDirtyFile(next);
      } catch (err) {
        console.error("[obsidian-goat-sync] Failed to flush dirty queue entry:", err);
        await this.dirtyQueue.completeRetryableFailure(next.path);
        return "transientFailure";
      }
    }
  }

  private async putDirtyFile(snapshot: { path: string; baseVersion?: number; lastSeenHash: string }): Promise<void> {
    const content = await this.readFileContent(snapshot.path);
    if (content === null) {
      await this.dirtyQueue.remove(snapshot.path);
      if (snapshot.baseVersion !== undefined) {
        await this.deleteQueue.enqueue({
          path: snapshot.path,
          baseVersion: snapshot.baseVersion,
          serverHash: this.fileMeta.get(snapshot.path)?.prevServerHash || "",
        });
      }
      return;
    }

    const hash = await this.computeHashFromContent(snapshot.path, content);
    const encoding = isBinaryPath(snapshot.path) ? "base64" : undefined;
    await this.dirtyQueue.markSentHash(snapshot.path, snapshot.lastSeenHash, hash);

    const payload: FilePayload = { path: snapshot.path, exists: true, localHash: hash };
    if (snapshot.baseVersion !== undefined) {
      payload.baseVersion = snapshot.baseVersion;
      payload.baseHash = this.fileMeta.get(snapshot.path)?.prevServerHash;
    }
    if (!this.wsClient.sendFilePut(this.vaultName, snapshot.path, content, payload, encoding)) {
      throw new Error("websocket is not open");
    }
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
      const sent = this.wsClient.sendFileCheck(this.vaultName, {
        path,
        exists: true,
        baseVersion: meta?.prevServerVersion,
        baseHash: meta?.prevServerHash,
        localHash: hash,
      });
      if (!sent) {
        console.warn("[obsidian-goat-sync] Skipped fileCheck because websocket is not open:", path);
      }
    });
  }
}
