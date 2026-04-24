import esbuild from "esbuild";
import { copyFileSync, mkdirSync } from "fs";
import { dirname, resolve } from "path";
import process from "process";
import { fileURLToPath } from "url";

const prod = process.argv[2] === "production";
const pluginDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(pluginDir, "..");
const bratOutdir = resolve(repoRoot, "build", "brat");

mkdirSync(bratOutdir, { recursive: true });

esbuild.build({
  entryPoints: [resolve(pluginDir, "src/main.ts")],
  bundle: true,
  external: ["obsidian"],
  format: "cjs",
  target: "es2018",
  logLevel: "info",
  sourcemap: prod ? false : "inline",
  treeShaking: true,
  outfile: resolve(bratOutdir, "main.js"),
  minify: prod,
}).then(() => {
  copyFileSync(resolve(pluginDir, "manifest.json"), resolve(bratOutdir, "manifest.json"));
  copyFileSync(resolve(pluginDir, "styles.css"), resolve(bratOutdir, "styles.css"));
}).catch(() => process.exit(1));
