import esbuild from "esbuild";
import { copyFileSync, mkdirSync } from "fs";
import { dirname, resolve } from "path";
import process from "process";
import { fileURLToPath } from "url";

const prod = process.argv[2] === "production";
const pluginDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(pluginDir, "..");
const outdir = resolve(pluginDir, "dist", "obsidian-sync");

mkdirSync(outdir, { recursive: true });

esbuild.build({
  entryPoints: [resolve(pluginDir, "src/main.ts")],
  bundle: true,
  external: ["obsidian"],
  format: "cjs",
  target: "es2018",
  logLevel: "info",
  sourcemap: prod ? false : "inline",
  treeShaking: true,
  outfile: resolve(outdir, "main.js"),
  minify: prod,
}).then(() => {
  copyFileSync(resolve(pluginDir, "manifest.json"), resolve(outdir, "manifest.json"));
  copyFileSync(resolve(pluginDir, "styles.css"), resolve(outdir, "styles.css"));

  copyFileSync(resolve(outdir, "main.js"), resolve(repoRoot, "main.js"));
  copyFileSync(resolve(outdir, "manifest.json"), resolve(repoRoot, "manifest.json"));
  copyFileSync(resolve(outdir, "styles.css"), resolve(repoRoot, "styles.css"));
}).catch(() => process.exit(1));
