import { Plugin, Notice, TFile } from "obsidian";
import { SyncSettingTab, SyncSettings, DEFAULT_SETTINGS } from "./settings";
import { SyncManager } from "./sync";
import { FileMetaStore, FileMeta } from "./file-meta-store";

export default class ObsidianSyncPlugin extends Plugin {
  settings!: SyncSettings;
  syncManager: SyncManager | null = null;
  private fileMetaStore: FileMetaStore | null = null;

  async onload() {
    await this.loadSettings();
    this.addSettingTab(new SyncSettingTab(this.app, this));

    this.addCommand({
      id: "connect-sync",
      name: "Connect to sync server",
      callback: () => this.connectSync(),
    });

    this.addCommand({
      id: "disconnect-sync",
      name: "Disconnect from sync server",
      callback: () => this.disconnectSync(),
    });

    this.registerEvent(
      this.app.workspace.on("file-open", (file) => {
        if (file instanceof TFile && this.syncManager) {
          this.syncManager.checkFileOnOpen(file.path);
        }
      }),
    );

    if (this.settings.serverUrl && this.settings.token && this.settings.vaultName) {
      this.connectSync().catch((err) =>
        console.error("[obsidian-sync] Auto-connect failed:", err),
      );
    }
  }

  async onunload() {
    await this.disconnectSync();
  }

  async connectSync() {
    if (this.syncManager) {
      await this.disconnectSync();
    }

    const { serverUrl, token, vaultName } = this.settings;
    if (!serverUrl || !token || !vaultName) {
      new Notice("Obsidian Sync: Please configure server URL, token, and vault name");
      return;
    }

    const initialMeta: Record<string, FileMeta> = this.settings.fileMeta || {};
    this.fileMetaStore = new FileMetaStore(initialMeta, async (data) => {
      this.settings.fileMeta = data;
      await this.saveData(this.settings);
    });

    this.syncManager = new SyncManager(
      this.app,
      this.app.vault,
      serverUrl,
      token,
      vaultName,
      this.fileMetaStore,
    );

    const connected = await this.syncManager.start();
    if (connected) {
      new Notice("Obsidian Sync: Connected");
    } else {
      new Notice("Obsidian Sync: Initial connection failed, will retry every 30s");
    }
  }

  async disconnectSync() {
    if (this.syncManager) {
      await this.syncManager.stop();
      this.syncManager = null;
      new Notice("Obsidian Sync: Disconnected");
    }
    this.fileMetaStore = null;
  }

  async reconnectSync() {
    if (this.syncManager) {
      await this.disconnectSync();
    }
    await this.connectSync();
  }

  async loadSettings() {
    const data = await this.loadData();
    this.settings = Object.assign({}, DEFAULT_SETTINGS, data);
    if (!this.settings.fileMeta) {
      this.settings.fileMeta = {};
    }
  }

  async saveSettings() {
    await this.saveData(this.settings);
  }
}
