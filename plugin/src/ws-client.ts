export interface FilePayload {
  path: string;
  exists: boolean;
  baseVersion?: number;
  baseHash?: string;
  localHash?: string;
}

export interface ServerMetaPayload {
  path?: string;
  serverVersion: number;
  serverHash?: string;
  isDeleted: boolean;
}

export interface DownloadEntry {
  path: string;
  content: string;
  serverVersion: number;
  serverHash: string;
  encoding?: string;
}

export type ServerAction =
  | "toPut" | "toUpdateMeta" | "toDownload" | "toDeleteLocal" | "toRemoveMeta"
  | "none" | "conflict" | "deleteConflict"
  | "put" | "updateMeta" | "upToDate"
  | "okUpdateMeta" | "okRemoveMeta";

export interface UpdateMetaEntry {
  path: string;
  serverVersion: number;
  serverHash: string;
}

export interface ConflictInfo {
  serverVersion: number;
  serverHash: string;
  serverContent: string;
  isDeleted: boolean;
  encoding?: string;
}

export interface SyncConflictEntry {
  path: string;
  baseVersion?: number;
  baseHash?: string;
  localHash: string;
  serverVersion: number;
  serverHash: string;
  serverContent: string;
  isDeleted: boolean;
  encoding?: string;
}

export interface ServerMessage {
  type: string;
  vault?: string;
  path?: string;
  ok?: boolean;
  noop?: boolean;
  serverVersion?: number;
  serverHash?: string;
  serverContent?: string;
  action?: ServerAction;
  content?: string;
  encoding?: string;
  conflict?: ConflictInfo;
  toPut?: string[];
  toDownload?: DownloadEntry[];
  toUpdateMeta?: ServerMetaPayload[];
  toDeleteLocal?: ServerMetaPayload[];
  toRemoveMeta?: ServerMetaPayload[];
  conflicts?: SyncConflictEntry[];
  meta?: ServerMetaPayload;
  error?: string;
}

export type MessageCallback = (msg: ServerMessage) => void;

export function buildSyncInitMessage(vault: string, files: FilePayload[]) {
  return { type: "syncInit", vault, files };
}

export function buildFilePutMessage(vault: string, path: string, content: string, file: FilePayload, encoding?: string) {
  const msg: Record<string, unknown> = { type: "filePut", vault, path, content, file };
  if (encoding) msg.encoding = encoding;
  return msg;
}

export class WsClient {
  private ws: WebSocket | null = null;
  private serverUrl: string;
  private token: string;
  private callbacks: Map<string, MessageCallback[]> = new Map();
  private healthCheckTimer: ReturnType<typeof setInterval> | null = null;
  private connecting = false;

  constructor(serverUrl: string, token: string) {
    this.serverUrl = serverUrl;
    this.token = token;
  }

  connect(): Promise<void> {
    this.connecting = true;
    return new Promise((resolve, reject) => {
      if (this.ws) {
        this.ws.onclose = null;
        this.ws.onerror = null;
        this.ws.close();
        this.ws = null;
      }

      const url = `${this.serverUrl}/ws?token=${this.token}`;
      this.ws = new WebSocket(url);

      let settled = false;

      this.ws.onopen = () => {
        settled = true;
        this.connecting = false;
        resolve();
      };

      this.ws.onmessage = (event) => {
        console.debug("[obsidian-goat-sync] ws incoming raw", event.data);
        const msg: ServerMessage = JSON.parse(event.data);
        const handlers = this.callbacks.get(msg.type) || [];
        handlers.forEach((cb) => cb(msg));
      };

      this.ws.onclose = () => {
        this.ws = null;
        if (!settled) {
          settled = true;
          this.connecting = false;
          reject(new Error("WebSocket closed before connecting"));
        }
      };

      this.ws.onerror = (err) => {
        settled = true;
        this.connecting = false;
        reject(err);
      };
    });
  }

  startHealthCheck() {
    this.stopHealthCheck();
    this.healthCheckTimer = setInterval(() => {
      if (!this.connecting && (!this.ws || this.ws.readyState !== WebSocket.OPEN)) {
        this.connect()
          .then(() => {
            const handlers = this.callbacks.get("reconnected") || [];
            handlers.forEach((cb) => cb({ type: "reconnected" }));
          })
          .catch((err) => {
            console.error("[obsidian-goat-sync] Reconnect failed:", err);
          });
      }
    }, 30000);
  }

  private stopHealthCheck() {
    if (this.healthCheckTimer) {
      clearInterval(this.healthCheckTimer);
      this.healthCheckTimer = null;
    }
  }

  disconnect() {
    this.stopHealthCheck();
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
  }

  on(type: string, callback: MessageCallback) {
    if (!this.callbacks.has(type)) {
      this.callbacks.set(type, []);
    }
    this.callbacks.get(type)!.push(callback);
  }

  send(msg: Record<string, unknown>): boolean {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      const data = JSON.stringify(msg);
      console.debug("[obsidian-goat-sync] ws outgoing raw", data);
      this.ws.send(data);
      return true;
    }
    return false;
  }

  sendSyncInit(vault: string, files: FilePayload[]): boolean {
    return this.send(buildSyncInitMessage(vault, files));
  }

  sendFileCheck(vault: string, file: FilePayload): boolean {
    return this.send({ type: "fileCheck", vault, path: file.path, file });
  }

  sendFilePut(vault: string, path: string, content: string, file: FilePayload, encoding?: string): boolean {
    return this.send(buildFilePutMessage(vault, path, content, file, encoding));
  }

  sendFileDelete(vault: string, file: FilePayload): boolean {
    return this.send({ type: "fileDelete", vault, path: file.path, file });
  }

  sendConflictResolveLocal(
    vault: string,
    path: string,
    content: string,
    localHash: string,
    baseVersion: number,
    encoding?: string,
  ): boolean {
    const msg: Record<string, unknown> = {
      type: "conflictResolve",
      vault,
      path,
      resolution: "local",
      file: {
        path,
        exists: true,
        baseVersion,
        localHash,
      },
      content,
    };
    if (encoding) msg.encoding = encoding;
    return this.send(msg);
  }

  sendConflictResolveLocalDelete(vault: string, path: string, baseVersion: number): boolean {
    return this.send({
      type: "conflictResolve",
      vault,
      path,
      resolution: "local",
      action: "delete",
      file: {
        path,
        exists: true,
        baseVersion,
      },
    });
  }

  sendVaultCreate(vault: string): boolean {
    return this.send({ type: "vaultCreate", vault });
  }
}
