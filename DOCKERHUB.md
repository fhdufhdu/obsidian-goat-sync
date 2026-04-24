# Obsidian Goat Sync Server

Self-hosted server for the Obsidian Goat Sync plugin.

## Quick Start

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

Open `http://localhost:8080` and sign in with the admin credentials above.

## Docker Compose

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

Run it with:

```bash
docker compose up -d
```

## Plugin

Install the Obsidian plugin with BRAT from:

```text
https://github.com/fhdufhdu/obsidian-goat-sync
```
