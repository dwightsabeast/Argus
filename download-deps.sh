#!/bin/bash
# Download required frontend dependencies.
# Run this once before building. All files are vendored into static/.
set -euo pipefail

STATIC_JS="static/js"
STATIC_CSS="static/css"

mkdir -p "$STATIC_JS" "$STATIC_CSS"

echo "Downloading HTMX v1.9.12..."
curl -sL "https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js" -o "$STATIC_JS/htmx.min.js"

echo "Downloading Leaflet v1.9.4..."
curl -sL "https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" -o "$STATIC_JS/leaflet.js"
curl -sL "https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" -o "$STATIC_CSS/leaflet.css"

echo "Downloading Leaflet.markercluster v1.5.3..."
curl -sL "https://unpkg.com/leaflet.markercluster@1.5.3/dist/leaflet.markercluster.js" -o "$STATIC_JS/leaflet.markercluster.js"

echo "All dependencies downloaded to static/."
echo "These files are vendored — no CDN requests at runtime."
