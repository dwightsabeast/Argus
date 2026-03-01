#!/usr/bin/env bash
# ============================================================
# Argus — Proxmox LXC Installer
# One-command installer following Proxmox community script patterns.
# Run this on the Proxmox host shell.
# ============================================================
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

header() { echo -e "\n${CYAN}━━━ $1 ━━━${NC}\n"; }
info() { echo -e "${GREEN}✓${NC} $1"; }
warn() { echo -e "${YELLOW}⚠${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; exit 1; }

# Check we're on Proxmox
command -v pct &>/dev/null || error "This script must be run on a Proxmox host (pct not found)."

header "Argus — Surveillance Index & Mapping Platform"
echo "This installer will create a new LXC container and deploy Argus."
echo ""

# --- Prompts with defaults (FRD Section 3.4) ---
read -rp "Hostname [surveillance-index]: " HOSTNAME
HOSTNAME=${HOSTNAME:-surveillance-index}

read -rp "IP address (or 'dhcp') [dhcp]: " IP_ADDR
IP_ADDR=${IP_ADDR:-dhcp}

read -rp "CPU cores [2]: " CPU
CPU=${CPU:-2}

read -rp "RAM in MB [1024]: " RAM
RAM=${RAM:-1024}

read -rp "OS disk size in GB [8]: " OS_DISK
OS_DISK=${OS_DISK:-8}

read -rp "Data volume size in GB (min 10, for images + DB) [50]: " DATA_DISK
DATA_DISK=${DATA_DISK:-50}

if [ "$DATA_DISK" -lt 10 ]; then
    warn "Data volume is less than 10 GB. This may fill quickly with image uploads."
    read -rp "Continue anyway? [y/N]: " CONFIRM
    [ "${CONFIRM,,}" = "y" ] || exit 1
fi

read -rp "Maximum image upload size in MB [20]: " IMAGE_MAX
IMAGE_MAX=${IMAGE_MAX:-20}

read -rp "Enable federation? (true/false) [false]: " FED_ENABLED
FED_ENABLED=${FED_ENABLED:-false}

read -rp "Map tile source (osm/protomaps) [osm]: " TILE_SOURCE
TILE_SOURCE=${TILE_SOURCE:-osm}

PROTOMAPS_EP=""
if [ "$TILE_SOURCE" = "protomaps" ]; then
    read -rp "Protomaps endpoint URL: " PROTOMAPS_EP
fi

# --- Determine next available CT ID ---
CTID=$(pvesh get /cluster/nextid)
info "Using container ID: $CTID"

# --- Select storage ---
STORAGE=$(pvesm status --content rootdir | awk 'NR>1 {print $1; exit}')
[ -z "$STORAGE" ] && error "No storage found for container rootdir."
info "Using storage: $STORAGE"

# --- Download template if needed ---
TEMPLATE="debian-12-standard"
TEMPLATE_FILE=$(pveam list "$STORAGE" 2>/dev/null | grep "$TEMPLATE" | awk '{print $1}' | head -1)
if [ -z "$TEMPLATE_FILE" ]; then
    info "Downloading Debian 12 template..."
    pveam download "$STORAGE" "${TEMPLATE}_12.7-1_amd64.tar.zst" || \
    pveam download "$STORAGE" $(pveam available | grep "$TEMPLATE" | awk '{print $2}' | head -1)
    TEMPLATE_FILE=$(pveam list "$STORAGE" | grep "$TEMPLATE" | awk '{print $1}' | head -1)
fi

# --- Network config ---
NET_CONFIG="name=eth0,bridge=vmbr0"
if [ "$IP_ADDR" != "dhcp" ]; then
    NET_CONFIG="${NET_CONFIG},ip=${IP_ADDR}/24"
else
    NET_CONFIG="${NET_CONFIG},ip=dhcp"
fi

# --- Create LXC ---
header "Creating LXC Container"
pct create "$CTID" "$TEMPLATE_FILE" \
    --hostname "$HOSTNAME" \
    --cores "$CPU" \
    --memory "$RAM" \
    --rootfs "${STORAGE}:${OS_DISK}" \
    --net0 "$NET_CONFIG" \
    --unprivileged 1 \
    --features nesting=1 \
    --onboot 1 \
    --start 0

info "Container $CTID created."

# --- Add data volume ---
header "Configuring Data Volume"
pct set "$CTID" -mp0 "${STORAGE}:${DATA_DISK},mp=/data"
info "Data volume ${DATA_DISK}GB mounted at /data"

# --- Start container ---
pct start "$CTID"
sleep 3
info "Container started."

# --- Install Argus inside the container ---
header "Installing Argus"

pct exec "$CTID" -- bash -c "
    set -euo pipefail
    
    # Update and install minimal deps
    apt-get update -qq
    apt-get install -y -qq curl ca-certificates

    # Create directories
    mkdir -p /data/images /data/db /opt/argus/static /opt/argus/templates

    # Download latest Argus binary (placeholder — replace with actual release URL)
    # curl -sL https://github.com/argus-platform/argus/releases/latest/download/argus-linux-amd64 -o /opt/argus/argus
    # chmod +x /opt/argus/argus

    echo '#!/bin/bash' > /opt/argus/argus
    echo 'echo \"Replace this with the actual Argus binary from a release build.\"' >> /opt/argus/argus
    chmod +x /opt/argus/argus

    # Write config
    cat > /opt/argus/argus.env <<EOF
DATA_PATH=/data/images
DB_PATH=/data/db/argus.db
LISTEN_ADDR=:8080
IMAGE_MAX_SIZE_MB=${IMAGE_MAX}
PAGE_SIZE=50
FEDERATION_ENABLED=${FED_ENABLED}
MAP_TILE_SOURCE=${TILE_SOURCE}
PROTOMAPS_ENDPOINT=${PROTOMAPS_EP}
EOF

    # Create systemd service
    cat > /etc/systemd/system/argus.service <<EOF
[Unit]
Description=Argus Surveillance Index
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/argus
EnvironmentFile=/opt/argus/argus.env
ExecStart=/opt/argus/argus
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable argus.service
"

info "Argus installed at /opt/argus"

# --- Print summary ---
header "Installation Complete"

IP_DISPLAY="$IP_ADDR"
if [ "$IP_ADDR" = "dhcp" ]; then
    IP_DISPLAY=$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}' || echo "(check DHCP)")
fi

cat <<SUMMARY
    Container ID:    $CTID
    Hostname:        $HOSTNAME
    IP Address:      $IP_DISPLAY
    CPU Cores:       $CPU
    RAM:             ${RAM} MB
    OS Disk:         ${OS_DISK} GB
    Data Volume:     ${DATA_DISK} GB (mounted at /data)
    Max Image Size:  ${IMAGE_MAX} MB
    Federation:      $FED_ENABLED
    Tile Source:     $TILE_SOURCE
    
    Access Argus at: http://${IP_DISPLAY}:8080
    
    Next steps:
    1. Copy the Argus binary and static assets to /opt/argus/
    2. Start the service: pct exec $CTID -- systemctl start argus
    3. Set up a reverse proxy with TLS for production use.
SUMMARY

# Write summary inside LXC
pct exec "$CTID" -- bash -c "cat > /root/README.txt <<EOF
Argus Surveillance Index — Deployment Summary
==============================================
Container: $CTID ($HOSTNAME)
IP: $IP_DISPLAY
Data: /data (${DATA_DISK}GB)
Config: /opt/argus/argus.env
Service: systemctl {start|stop|status} argus
Logs: journalctl -u argus -f

To expand data volume later:
  1. pct resize $CTID mp0 +50G   (on Proxmox host)
  2. resize2fs /dev/...           (inside LXC)
EOF"

info "Summary written to /root/README.txt inside the LXC."
echo ""
