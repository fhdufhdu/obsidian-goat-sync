export interface SyncInitFileMsg {
  path: string;
  prevServerVersion?: number;
  prevServerHash?: string;
  currentClientHash: string;
}

export interface DownloadEntry {
  path: string;
  content: string;
  currentServerVersion: number;
  currentServerHash: string;
  encoding?: string;
}

export interface UpdateMetaEntry {
  path: string;
  currentServerVersion: number;
  currentServerHash: string;
}

export interface ConflictInfo {
  currentServerVersion: number;
  currentServerHash: string;
  currentServerContent: string;
  encoding?: string;
}

export interface SyncConflictEntry {
  path: string;
  prevServerVersion?: number;
  currentClientHash: string;
  currentServerVersion: number;
  currentServerHash: string;
  currentServerContent: string;
  encoding?: string;
}

export interface ServerMessage {
  type: string;
  vault?: string;
  path?: string;
  ok?: boolean;
  noop?: boolean;
  currentServerVersion?: number;
  currentServerHash?: string;
  action?: string;
  content?: string;
  encoding?: string;
  conflict?: ConflictInfo;
  toUpload?: string[];
  toUpdate?: string[];
  toDownload?: DownloadEntry[];
  toDelete?: string[];
  toUpdateMeta?: UpdateMetaEntry[];
  conflicts?: SyncConflictEntry[];
  error?: string;
}

type MessageCallback = (msg: ServerMessage) => void;

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

  send(msg: Record<string, unknown>) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  sendSyncInit(vault: string, files: SyncInitFileMsg[]) {
    this.send({ type: "sync_init", vault, files });
  }

  sendFileCheck(vault: string, file: SyncInitFileMsg) {
    this.send({
      type: "file_check",
      vault,
      path: file.path,
      prevServerVersion: file.prevServerVersion,
      prevServerHash: file.prevServerHash,
      currentClientHash: file.currentClientHash,
    });
  }

  sendFileCreate(vault: string, path: string, content: string, currentClientHash: string, encoding?: string) {
    const msg: Record<string, unknown> = { type: "file_create", vault, path, content, currentClientHash };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendFileUpdate(
    vault: string,
    path: string,
    content: string,
    prevServerVersion: number,
    currentClientHash: string,
    encoding?: string,
  ) {
    const msg: Record<string, unknown> = {
      type: "file_update",
      vault,
      path,
      content,
      prevServerVersion,
      currentClientHash,
    };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendFileDelete(vault: string, path: string, prevServerVersion: number) {
    this.send({ type: "file_delete", vault, path, prevServerVersion });
  }

  sendConflictResolveLocal(
    vault: string,
    path: string,
    content: string,
    currentClientHash: string,
    prevServerVersion: number,
    encoding?: string,
  ) {
    const msg: Record<string, unknown> = {
      type: "conflict_resolve",
      vault,
      path,
      resolution: "local",
      content,
      currentClientHash,
      prevServerVersion,
    };
    if (encoding) msg.encoding = encoding;
    this.send(msg);
  }

  sendConflictResolveLocalDelete(vault: string, path: string, prevServerVersion: number) {
    this.send({
      type: "conflict_resolve",
      vault,
      path,
      resolution: "local",
      action: "delete",
      prevServerVersion,
    });
  }

  sendVaultCreate(vault: string) {
    this.send({ type: "vault_create", vault });
  }
}
