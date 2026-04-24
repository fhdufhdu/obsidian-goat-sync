# Obsidian Goat Sync

Self-hosted Obsidian sync plugin and Go server.

## Install with BRAT

1. Install and enable the BRAT community plugin in Obsidian.
2. Run `BRAT: Add a beta plugin for testing`.
3. Enter `https://github.com/fhdufhdu/obsidian-goat-sync`.
4. Enable `Obsidian Sync` from Obsidian community plugins.

BRAT looks for `manifest.json` in the repository root and installs release
assets named `main.js`, `manifest.json`, and `styles.css`. The plugin build
mirrors those files to the repository root for beta installation.

## Development

```bash
cd plugin
npm ci
npm test
npm run build
```

```bash
cd server
go test ./...
```
