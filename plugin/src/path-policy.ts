const OBSIDIAN_DIR = ".obsidian";
const OBSIDIAN_PLUGINS_DIR = ".obsidian/plugins";

export function isSyncExcludedPath(path: string): boolean {
  return path === OBSIDIAN_PLUGINS_DIR || path.startsWith(`${OBSIDIAN_PLUGINS_DIR}/`);
}

export function isServerWinsPath(path: string): boolean {
  if (isSyncExcludedPath(path)) return false;
  return path === OBSIDIAN_DIR || path.startsWith(`${OBSIDIAN_DIR}/`);
}
