import { App, PluginSettingTab, Setting } from "obsidian";
import type ObsidianSyncPlugin from "./main";
import { FileMeta } from "./file-meta-store";

export interface SyncSettings {
  serverUrl: string;
  token: string;
  vaultName: string;
  fileMeta: Record<string, FileMeta>;
}

export const DEFAULT_SETTINGS: SyncSettings = {
  serverUrl: "",
  token: "",
  vaultName: "",
  fileMeta: {},
};

export class SyncSettingTab extends PluginSettingTab {
  plugin: ObsidianSyncPlugin;

  constructor(app: App, plugin: ObsidianSyncPlugin) {
    super(app, plugin);
    this.plugin = plugin;
  }

  display(): void {
    const { containerEl } = this;
    containerEl.empty();
    containerEl.createEl("h2", { text: "Obsidian Sync Settings" });

    new Setting(containerEl)
      .setName("Server URL")
      .setDesc("WebSocket server URL (e.g. ws://192.168.1.100:8080)")
      .addText((text) =>
        text
          .setPlaceholder("ws://your-server:8080")
          .setValue(this.plugin.settings.serverUrl)
          .onChange((value) => {
            this.plugin.settings.serverUrl = value;
          }),
      );

    new Setting(containerEl)
      .setName("API Token")
      .setDesc("Server access token")
      .addText((text) =>
        text
          .setPlaceholder("your-token")
          .setValue(this.plugin.settings.token)
          .onChange((value) => {
            this.plugin.settings.token = value;
          }),
      );

    new Setting(containerEl)
      .setName("Vault Name")
      .setDesc("Server-side vault name")
      .addText((text) =>
        text
          .setPlaceholder("personal")
          .setValue(this.plugin.settings.vaultName)
          .onChange((value) => {
            this.plugin.settings.vaultName = value;
          }),
      );

    const errorEl = containerEl.createDiv({ cls: "obsidian-sync-error" });
    errorEl.style.color = "var(--text-error)";

    new Setting(containerEl).addButton((btn) =>
      btn
        .setButtonText("Save")
        .setCta()
        .onClick(async () => {
          const { serverUrl, token, vaultName } = this.plugin.settings;
          if (!serverUrl || !token || !vaultName) {
            errorEl.textContent = "All fields are required";
            return;
          }
          errorEl.textContent = "";
          await this.plugin.saveSettings();
          await this.plugin.reconnectSync();
        }),
    );
  }
}
