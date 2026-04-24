import { App, Modal, Notice } from "obsidian";
import { ConflictQueue, ConflictEntry } from "./conflict-queue";
import type { SyncManager } from "./sync";

const IMAGE_EXTENSIONS = new Set(["png", "jpg", "jpeg", "gif", "bmp", "svg", "webp"]);

function extOf(path: string): string {
  return path.split(".").pop()?.toLowerCase() || "";
}

function mimeForImage(ext: string): string {
  if (ext === "jpg") return "image/jpeg";
  if (ext === "svg") return "image/svg+xml";
  return `image/${ext}`;
}

function base64ByteSize(b64: string): number {
  const len = b64.length;
  if (len === 0) return 0;
  const padding = (b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0);
  return Math.floor((len * 3) / 4) - padding;
}

function shortHash(hash: string): string {
  if (!hash) return "";
  if (hash.length <= 12) return hash;
  return hash.slice(0, 8) + "…" + hash.slice(-4);
}

export class ConflictModal extends Modal {
  private queue: ConflictQueue;
  private syncManager: SyncManager;
  private currentPath: string | null = null;
  private sidebarEl!: HTMLElement;
  private cardsEl!: HTMLElement;

  constructor(app: App, queue: ConflictQueue, syncManager: SyncManager) {
    super(app);
    this.queue = queue;
    this.syncManager = syncManager;
  }

  onOpen() {
    const { contentEl } = this;
    contentEl.empty();
    contentEl.addClass("obsidian-sync-conflict-modal");
    contentEl.style.cssText = "display:flex;flex-direction:column;height:80vh;min-width:700px;";

    const header = contentEl.createEl("h2", { text: "Sync Conflicts" });
    header.style.cssText = "margin-bottom:12px;";

    const body = contentEl.createDiv();
    body.style.cssText = "display:flex;flex:1;gap:16px;overflow:hidden;";

    this.sidebarEl = body.createDiv();
    this.sidebarEl.style.cssText =
      "width:220px;min-width:180px;border-right:1px solid var(--background-modifier-border);overflow-y:auto;padding-right:12px;";

    this.cardsEl = body.createDiv();
    this.cardsEl.style.cssText = "flex:1;overflow-y:auto;";

    const footer = contentEl.createDiv();
    footer.style.cssText = "margin-top:12px;text-align:right;";
    const applyBtn = footer.createEl("button", { text: "Apply All" });
    applyBtn.style.cssText = "padding:8px 20px;background:var(--interactive-accent);color:var(--text-on-accent);border:none;border-radius:6px;cursor:pointer;";
    applyBtn.disabled = true;
    applyBtn.onclick = () => this.applyAll();

    this.renderSidebar(applyBtn);

    const entries = this.queue.list();
    if (entries.length > 0) {
      this.selectEntry(entries[0].path);
    }
  }

  private renderSidebar(applyBtn: HTMLButtonElement) {
    this.sidebarEl.empty();
    const entries = this.queue.list();

    for (const entry of entries) {
      const row = this.sidebarEl.createDiv();
      row.style.cssText =
        "padding:8px;border-radius:4px;cursor:pointer;margin-bottom:4px;word-break:break-all;font-size:13px;";

      const fileName = entry.path.split("/").pop() || entry.path;
      const kindTag = entry.kind === "delete" ? " (삭제)" : "";
      const selLabel = entry.selection ? ` [${entry.selection}]` : " [unresolved]";

      row.textContent = fileName + kindTag + selLabel;
      row.style.background =
        entry.path === this.currentPath
          ? "var(--interactive-accent)"
          : "var(--background-secondary)";

      row.onclick = () => {
        this.currentPath = entry.path;
        this.renderSidebar(applyBtn);
        this.renderCards(entry);
      };
    }

    applyBtn.disabled = !this.queue.isAllResolved();
  }

  private selectEntry(path: string) {
    const entry = this.queue.get(path);
    if (!entry) return;
    this.currentPath = path;
    this.renderCards(entry);
  }

  private renderCards(entry: ConflictEntry) {
    this.cardsEl.empty();

    const title = this.cardsEl.createEl("h3", { text: entry.path });
    title.style.cssText = "margin-bottom:12px;font-size:14px;word-break:break-all;";

    const cardsRow = this.cardsEl.createDiv();
    cardsRow.style.cssText = "display:flex;gap:12px;";

    const isDelete = entry.kind === "delete";
    const serverLabel = isDelete ? "SERVER (복구)" : "SERVER";
    const localLabel = isDelete ? "LOCAL (강제 삭제)" : "LOCAL";

    const serverPreview = this.createCard(
      cardsRow,
      serverLabel,
      entry.currentServerContent,
      entry.currentServerVersion,
      entry.encoding,
      entry.path,
      entry.currentServerHash,
      isDelete ? "서버 내용으로 로컬 파일 복구" : undefined,
      () => {
        this.queue.selectAt(entry.path, "server");
        this.afterSelect(entry.path);
      },
    );

    const localPreview = this.createCard(
      cardsRow,
      localLabel,
      isDelete ? "" : entry.currentClientContent,
      undefined,
      entry.encoding,
      entry.path,
      entry.currentClientHash,
      isDelete ? "서버에서도 삭제" : undefined,
      () => {
        this.queue.selectAt(entry.path, "local");
        this.afterSelect(entry.path);
      },
    );

    this.syncScrolls(serverPreview, localPreview);

    if (!isDelete) {
      const conflictPath = entry.conflictPath || entry.path;
      const newCard = cardsRow.createDiv();
      newCard.style.cssText =
        "flex:1;border:1px solid var(--background-modifier-border);border-radius:8px;padding:12px;cursor:pointer;";
      newCard.createEl("h4", { text: "SAVE AS NEW" }).style.cssText =
        "margin-bottom:8px;font-size:12px;color:var(--text-muted);";
      const pathEl = newCard.createEl("p", { text: conflictPath });
      pathEl.style.cssText = "font-size:11px;word-break:break-all;color:var(--text-accent);";
      newCard.createEl("p", { text: "로컬 내용 → 새 파일, 서버 내용 → 원본 경로" }).style.cssText =
        "font-size:11px;color:var(--text-muted);margin-top:8px;";
      newCard.onclick = () => {
        this.queue.selectAt(entry.path, "new");
        this.afterSelect(entry.path);
      };
    }
  }

  private createCard(
    container: HTMLElement,
    label: string,
    content: string,
    version: number | undefined,
    encoding: string | undefined,
    path: string,
    hash: string | undefined,
    subNote: string | undefined,
    onClick: () => void,
  ): HTMLElement {
    const card = container.createDiv();
    card.style.cssText =
      "flex:1;border:1px solid var(--background-modifier-border);border-radius:8px;padding:12px;cursor:pointer;overflow:hidden;display:flex;flex-direction:column;";

    const header = card.createDiv();
    header.style.cssText = "display:flex;justify-content:space-between;margin-bottom:8px;";
    header.createEl("h4", { text: label }).style.cssText =
      "font-size:12px;color:var(--text-muted);";
    if (version !== undefined) {
      header.createEl("span", { text: `v${version}` }).style.cssText =
        "font-size:11px;color:var(--text-faint);";
    }

    if (subNote) {
      const note = card.createEl("p", { text: subNote });
      note.style.cssText = "font-size:11px;color:var(--text-muted);margin:0 0 8px 0;";
    }

    const preview = card.createDiv();
    preview.style.cssText = "max-height:220px;overflow:auto;font-size:12px;flex:1;";

    this.renderPreviewInto(preview, content, encoding, path, hash);

    card.onclick = onClick;
    return preview;
  }

  private renderPreviewInto(
    target: HTMLElement,
    content: string,
    encoding: string | undefined,
    path: string,
    hash: string | undefined,
  ) {
    if (content === "") {
      const empty = target.createEl("p", { text: "(empty)" });
      empty.style.cssText = "color:var(--text-muted);";
      return;
    }

    if (encoding === "base64") {
      const ext = extOf(path);
      if (IMAGE_EXTENSIONS.has(ext)) {
        const img = target.createEl("img");
        img.src = `data:${mimeForImage(ext)};base64,${content}`;
        img.style.cssText = "max-width:100%;max-height:200px;object-fit:contain;display:block;";
        return;
      }
      const meta = target.createDiv();
      meta.style.cssText = "color:var(--text-muted);font-size:11px;line-height:1.6;";
      meta.createEl("div", { text: `type: binary (${ext || "unknown"})` });
      meta.createEl("div", { text: `size: ${base64ByteSize(content)} B` });
      if (hash) meta.createEl("div", { text: `hash: ${shortHash(hash)}` });
      return;
    }

    const pre = target.createEl("pre");
    pre.style.cssText =
      "white-space:pre-wrap;word-break:break-all;font-family:monospace;font-size:11px;margin:0;";
    pre.textContent = content;
  }

  private syncScrolls(a: HTMLElement, b: HTMLElement) {
    let locked = false;
    const pair = (src: HTMLElement, dst: HTMLElement) => {
      src.addEventListener("scroll", () => {
        if (locked) return;
        locked = true;
        dst.scrollTop = src.scrollTop;
        dst.scrollLeft = src.scrollLeft;
        requestAnimationFrame(() => {
          locked = false;
        });
      });
    };
    pair(a, b);
    pair(b, a);
  }

  private afterSelect(path: string) {
    const entries = this.queue.list();
    const nextUnresolved = entries.find((e) => e.path !== path && !e.selection);
    const applyBtn = this.contentEl.querySelector("button") as HTMLButtonElement | null;

    if (nextUnresolved) {
      this.currentPath = nextUnresolved.path;
      this.renderCards(nextUnresolved);
    } else if (this.queue.get(path)) {
      this.renderCards(this.queue.get(path)!);
    }

    if (applyBtn) {
      this.renderSidebar(applyBtn);
    }
  }

  refreshIfOpen(path: string) {
    if (!this.containerEl.isConnected) return;
    const applyBtn = this.contentEl.querySelector("button") as HTMLButtonElement | null;
    const entries = this.queue.list();

    if (this.currentPath === path) {
      const current = this.queue.get(path);
      if (current) {
        this.renderCards(current);
      } else if (entries.length > 0) {
        this.currentPath = entries[0].path;
        this.renderCards(entries[0]);
      } else {
        this.cardsEl.empty();
      }
    }

    if (applyBtn) {
      this.renderSidebar(applyBtn);
    }
  }

  private async applyAll() {
    const entries = this.queue.list().filter((e) => e.selection);
    for (const entry of entries) {
      try {
        await this.syncManager.applyConflictResolution(entry);
      } catch (err) {
        new Notice(`[obsidian-sync] Failed to apply conflict for ${entry.path}: ${err}`);
      }
    }
    if (this.queue.size() === 0) {
      this.close();
    } else {
      const remaining = this.queue.list();
      if (remaining.length > 0) {
        this.currentPath = remaining[0].path;
        this.renderCards(remaining[0]);
      }
      const applyBtn = this.contentEl.querySelector("button") as HTMLButtonElement | null;
      if (applyBtn) {
        this.renderSidebar(applyBtn);
      }
    }
  }

  onClose() {
    this.contentEl.empty();
  }
}
