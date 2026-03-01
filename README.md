# Argus — Surveillance Index & Mapping Platform

A self-hosted platform for researching, cataloging, and geographically mapping real-world surveillance tools. Designed for privacy-conscious individuals, researchers, and communities who want transparent, community-driven knowledge about surveillance infrastructure.

**Transparency notice:** This project is openly "vibe coded" — designed and built with heavy AI assistance. The codebase should be treated as a starting point, not a finished audited product. See the [FRD](Argus_FRD_v1.7.docx) for full context.

## Features

- **Surveillance Tool Profiles** — Structured records with manufacturer, category, observability level, known vulnerabilities, countermeasures, and more
- **Image Gallery** — Upload and browse high-resolution photos of surveillance hardware, with automatic EXIF metadata stripping for contributor privacy
- **Interactive World Map** — Leaflet.js-based map with clustered pins showing where tools have been sighted, color-coded by category
- **Profile Browser** — Map-free default landing page with live search, multi-filter sidebar, and pagination
- **Federation-Ready** — JSON sync API designed in from day one, gated behind a single config toggle
- **Self-Hosted** — Runs as a single Go binary + SQLite file inside a Proxmox LXC container with no external dependencies

## Quick Start

### Prerequisites

- Go 1.22 or later
- curl (for downloading frontend dependencies)

### Build & Run

```bash
# Clone the repo
git clone https://github.com/dwightsabeast/argus.git
cd argus

# Download frontend dependencies (HTMX, Leaflet, MarkerCluster)
bash download-deps.sh

# Build
make build

# Run in development mode (creates local dev-data/ directory)
make run
```

Visit `http://localhost:8080` to access the profile browser.

### Proxmox LXC Deployment

Run the one-command installer on your Proxmox host:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/dwightsabeast/argus/main/install-proxmox.sh)"
```

The installer will prompt for hostname, IP address, gateway (for static IPs), resource allocation, and storage sizing, then create and configure the LXC automatically.

## Configuration

All settings are controlled via environment variables or a single `.env` file:

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `DB_PATH` | `/data/db/argus.db` | SQLite database file path |
| `DATA_PATH` | `/data/images` | Image storage root |
| `IMAGE_MAX_SIZE_MB` | `20` | Max upload size per image |
| `PAGE_SIZE` | `50` | Profiles per page in browser |
| `MAP_TILE_SOURCE` | `osm` | Tile provider: `osm` or `protomaps` |
| `PROTOMAPS_ENDPOINT` | *(empty)* | Local Protomaps tile server URL |
| `FEDERATION_ENABLED` | `false` | Enable/disable federation sync API |
| `BASE_URL` | `http://localhost:8080` | External URL for links |

## Architecture

```
┌─────────────────────────────────────┐
│         Proxmox LXC Container       │
│                                     │
│  ┌────────────┐   ┌──────────────┐  │
│  │  Reverse   │   │    Argus     │  │
│  │  Proxy     │──▶│  Go Binary   │  │
│  │  (Caddy/   │   │              │  │
│  │   Nginx)   │   │  ┌────────┐  │  │
│  └────────────┘   │  │ SQLite │  │  │
│                   │  └────────┘  │  │
│                   │  ┌────────┐  │  │
│                   │  │ Images │  │  │
│                   │  │ /data/ │  │  │
│                   │  └────────┘  │  │
│                   └──────────────┘  │
└─────────────────────────────────────┘
```

- **Backend**: Go — single static binary, no runtime dependencies
- **Database**: SQLite (WAL mode) — zero config, one file, PostgreSQL-swappable
- **Frontend**: HTMX + server-rendered HTML — no build pipeline, no npm, auditable source
- **Map**: Leaflet.js with OpenStreetMap tiles (swappable to self-hosted Protomaps)
- **Images**: Local filesystem with S3-interface abstraction for future swappability

## Storage Sizing

| Scale | Profiles | Images/Profile | Avg Size | Est. Storage | Recommended |
|-------|----------|---------------|----------|-------------|-------------|
| Personal | ≤500 | 1–2 | 5–10 MB | ~5 GB | 10 GB |
| Small community | ≤2,000 | 3–5 | 10–15 MB | ~60–150 GB | 50–100 GB |
| Large community | ≤10,000 | 5 | 20 MB | ~1 TB | 500 GB–1 TB |

## Project Structure

```
argus/
├── cmd/argus/main.go        # Entry point, routing, template loading
├── internal/
│   ├── config/              # Environment-based configuration
│   ├── database/            # SQLite schema, migrations, CRUD
│   ├── handlers/            # HTTP handlers (profiles, images, map, federation)
│   ├── middleware/           # Security headers, fingerprinting, logging
│   ├── models/              # Data models and enumerations
│   └── storage/             # Image storage with EXIF stripping
├── templates/               # Server-rendered HTML templates
│   ├── layouts/             # Base layout
│   ├── profiles/            # Profile list, detail, form
│   └── map/                 # Map view
├── static/                  # CSS, vendored JS (HTMX, Leaflet)
├── download-deps.sh         # Frontend dependency downloader
├── install-proxmox.sh       # One-command Proxmox LXC installer
├── Makefile                 # Build commands
└── README.md
```

## Security Notes

- All user input is sanitized server-side before storage or rendering
- EXIF metadata (GPS coordinates, device info) is stripped from all uploaded images
- No third-party analytics, tracking scripts, or CDN-hosted JavaScript
- Federation endpoints return 404 when disabled — no topology leakage
- Anonymous contributions use SHA-256(IP + User-Agent) fingerprints — no PII stored
- Image storage paths are outside the web root to prevent directory traversal

## API Endpoints

### Public

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` or `/profiles` | Profile browser |
| GET | `/profiles/{id}` | Profile detail |
| GET/POST | `/profiles/new` | Create profile |
| GET/POST | `/profiles/{id}/edit` | Edit profile |
| POST | `/profiles/{id}/delete` | Soft delete profile |
| POST | `/profiles/{id}/images` | Upload images |
| GET | `/images/file/{pid}/{name}` | Serve image |
| POST | `/images/{id}/delete` | Delete image |
| GET | `/map` | Map view |
| GET | `/api/pins` | Pins as GeoJSON |
| POST | `/api/pins` | Create pin |
| POST | `/api/pins/{id}/delete` | Delete pin |
| GET | `/health` | Health check |

### Federation (when `FEDERATION_ENABLED=true`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/profiles` | All profiles (paginated) |
| GET | `/api/v1/pins` | All pins (paginated) |
| GET | `/api/v1/since?timestamp=X` | Records modified since timestamp |
