import { defineConfig } from "vitest/config";

const obsidianStub = `
export class App {}
export class Vault {}
export class TFile { constructor() { this.path = ""; } }
export class TAbstractFile { constructor() { this.path = ""; } }
export class Plugin {
  constructor() {
    this.app = { workspace: { on: () => ({}) } };
    this.manifest = {};
  }
  addSettingTab() {}
  addCommand() {}
  registerEvent() {}
  async loadData() { return {}; }
  async saveData() {}
}
export class PluginSettingTab {
  constructor(app, plugin) {
    this.app = app;
    this.plugin = plugin;
    this.containerEl = makeElement();
  }
  display() {}
}
export class Setting {
  setName() { return this; }
  setDesc() { return this; }
  addText(cb) {
    cb({ setPlaceholder: () => this, setValue: () => this, onChange: () => this });
    return this;
  }
  addButton(cb) {
    cb({ setButtonText: () => this, setCta: () => this, onClick: () => this });
    return this;
  }
}
export class Modal {
  constructor() {
    this.contentEl = makeElement();
    this.containerEl = { isConnected: false };
  }
  close() {}
  onOpen() {}
}
export class Notice {
  constructor(message) {
    globalThis.__obsidianGoatSyncNotices?.push(message);
  }
}
export function normalizePath(path) { return path; }
function makeElement() {
  return {
    style: {},
    empty() {},
    addClass() {},
    createDiv() { return makeElement(); },
    createEl() { return makeElement(); },
  };
}
`;

export default defineConfig({
  plugins: [
    {
      name: "obsidian-test-stub",
      enforce: "pre",
      resolveId(id) {
        return id === "obsidian" ? "\0obsidian-test-stub" : null;
      },
      load(id) {
        return id === "\0obsidian-test-stub" ? obsidianStub : null;
      },
    },
  ],
  test: {
    environment: "jsdom",
    globals: true,
  },
});
