# Obsidian Goat Sync

Self-hosted Obsidian sync plugin and Go server.

## Install with BRAT

1. Install and enable the BRAT community plugin in Obsidian.
2. Run `BRAT: Add a beta plugin for testing`.
3. Enter `https://github.com/fhdufhdu/obsidian-goat-sync`.
4. Enable `Obsidian Goat Sync` from Obsidian community plugins.

BRAT installs the release assets named `main.js`, `manifest.json`, and
`styles.css`. The GitHub Actions release workflow builds those files from
`plugin/` and attaches them to the GitHub release.

## Run the server

```bash
docker run -d \
  --name obsidian-goat-sync \
  -p 8080:8080 \
  -e OBSIDIAN_GOAT_SYNC_ADMIN_USER=admin \
  -e OBSIDIAN_GOAT_SYNC_ADMIN_PASS=change-this-password \
  -e OBSIDIAN_GOAT_SYNC_PORT=8080 \
  -v obsidian-goat-sync-data:/app/data \
  --restart unless-stopped \
  fhdufhdu/obsidian-goat-sync:latest
```

Or with Docker Compose:

```yaml
services:
  obsidian-goat-sync:
    image: fhdufhdu/obsidian-goat-sync:latest
    ports:
      - "8080:8080"
    environment:
      OBSIDIAN_GOAT_SYNC_ADMIN_USER: admin
      OBSIDIAN_GOAT_SYNC_ADMIN_PASS: change-this-password
      OBSIDIAN_GOAT_SYNC_PORT: "8080"
    volumes:
      - obsidian-goat-sync-data:/app/data
    restart: unless-stopped

volumes:
  obsidian-goat-sync-data:
```

The GitHub Actions release workflow uploads BRAT assets and publishes the
server image to Docker Hub. Configure repository secrets
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` before running it.

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
