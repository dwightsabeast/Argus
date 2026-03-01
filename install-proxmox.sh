#!/usr/bin/env bash
# ============================================================
# Argus — Proxmox LXC Installer
# One-command installer following Proxmox community script patterns.
# Run on the Proxmox host shell:
#   bash -c "$(curl -fsSL https://raw.githubusercontent.com/dwightsabeast/argus/main/install-proxmox.sh)"
# ============================================================
set -euo pipefail

# --- Colors & helpers ---------------------------------------------------------
RD='\033[0;31m'; GN='\033[0;32m'; YW='\033[1;33m'; CY='\033[0;36m'; NC='\033[0m'; BLD='\033[1m'
info()  { echo -e " ${GN}✓${NC} $1"; }
warn()  { echo -e " ${YW}⚠${NC} $1"; }
error() { echo -e " ${RD}✗${NC} $1"; exit 1; }
trap 'echo -e "\n${RD}✗ Failed at line $LINENO: $BASH_COMMAND${NC}"' ERR

# --- Pre-flight ---------------------------------------------------------------
command -v pct &>/dev/null || error "This script must be run on a Proxmox VE host (pct not found)."
command -v whiptail &>/dev/null || error "whiptail is required but not found."

APP="Argus"
APP_LC="argus"

# --- App defaults (like a community ct/ script header) ------------------------
var_os="debian"
var_version="12"
var_unprivileged="1"
var_cpu="2"
var_ram="1024"
var_disk="8"
var_data_disk="50"
var_image_max="20"
var_federation="false"
var_tile_source="osm"
var_net="dhcp"
var_brg="vmbr0"
var_hostname="argus"

# ==============================================================================
# HEADER
# ==============================================================================
clear
cat << 'EOF'
                                     
     █████╗ ██████╗  ██████╗ ██╗   ██╗███████╗
    ██╔══██╗██╔══██╗██╔════╝ ██║   ██║██╔════╝
    ███████║██████╔╝██║  ███╗██║   ██║███████╗
    ██╔══██║██╔══██╗██║   ██║██║   ██║╚════██║
    ██║  ██║██║  ██║╚██████╔╝╚██████╔╝███████║
    ╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝  ╚═════╝ ╚══════╝
     Surveillance Index & Mapping Platform
                                     
EOF

echo -e "${CY}This will create an LXC container and deploy ${APP}.${NC}\n"

# ==============================================================================
# SIMPLE vs ADVANCED
# ==============================================================================
if whiptail --backtitle "Argus LXC Installer" --title "CONFIGURATION" \
   --yesno "Use default settings?\n\n  OS:       Debian 12\n  CPU:      ${var_cpu} cores\n  RAM:      ${var_ram} MB\n  OS Disk:  ${var_disk} GB\n  Data:     ${var_data_disk} GB\n  Network:  DHCP\n\nSelect 'No' for advanced configuration." 18 58; then
    info "Using default configuration"
else
    # --- Advanced: resource allocation ----------------------------------------
    var_hostname=$(whiptail --backtitle "Argus LXC" --title "HOSTNAME" \
        --inputbox "Container hostname:" 8 58 "$var_hostname" 3>&1 1>&2 2>&3) || exit

    var_cpu=$(whiptail --backtitle "Argus LXC" --title "CPU CORES" \
        --inputbox "Number of CPU cores:" 8 58 "$var_cpu" 3>&1 1>&2 2>&3) || exit

    var_ram=$(whiptail --backtitle "Argus LXC" --title "RAM" \
        --inputbox "RAM in MB:" 8 58 "$var_ram" 3>&1 1>&2 2>&3) || exit

    var_disk=$(whiptail --backtitle "Argus LXC" --title "OS DISK" \
        --inputbox "OS disk size in GB:" 8 58 "$var_disk" 3>&1 1>&2 2>&3) || exit

    var_data_disk=$(whiptail --backtitle "Argus LXC" --title "DATA VOLUME" \
        --inputbox "Data volume in GB (images + DB, min 10):" 8 58 "$var_data_disk" 3>&1 1>&2 2>&3) || exit

    if [[ "$var_data_disk" -lt 10 ]]; then
        if ! whiptail --backtitle "Argus LXC" --title "WARNING" \
           --yesno "Data volume is under 10 GB.\nThis may fill quickly with image uploads.\n\nContinue anyway?" 10 58; then
            exit 1
        fi
    fi

    # --- Advanced: networking -------------------------------------------------
    var_net=$(whiptail --backtitle "Argus LXC" --title "NETWORK" \
        --menu "IP address configuration:" 12 58 2 \
        "dhcp"   "Automatic (DHCP)" \
        "static" "Manual static IP" \
        3>&1 1>&2 2>&3) || exit

    if [[ "$var_net" == "static" ]]; then
        STATIC_IP=$(whiptail --backtitle "Argus LXC" --title "STATIC IP" \
            --inputbox "IP address with CIDR (e.g. 192.168.1.26/24):" 8 58 "" 3>&1 1>&2 2>&3) || exit

        # Auto-suggest gateway from the IP (.1)
        DEFAULT_GW=$(echo "$STATIC_IP" | sed 's|/.*||; s/\.[0-9]*$/.1/')
        GATEWAY=$(whiptail --backtitle "Argus LXC" --title "GATEWAY" \
            --inputbox "Gateway IP:" 8 58 "$DEFAULT_GW" 3>&1 1>&2 2>&3) || exit
    fi

    # --- Advanced: bridge selection -------------------------------------------
    BRIDGES=$(ip -o link show type bridge 2>/dev/null | awk -F': ' '{print $2}' | tr '\n' ' ')
    if [[ -n "$BRIDGES" && $(echo "$BRIDGES" | wc -w) -gt 1 ]]; then
        BRIDGE_MENU=()
        for b in $BRIDGES; do BRIDGE_MENU+=("$b" ""); done
        var_brg=$(whiptail --backtitle "Argus LXC" --title "BRIDGE" \
            --menu "Network bridge:" 14 58 6 "${BRIDGE_MENU[@]}" 3>&1 1>&2 2>&3) || exit
    fi

    # --- Advanced: Argus-specific settings ------------------------------------
    var_image_max=$(whiptail --backtitle "Argus LXC" --title "IMAGE LIMIT" \
        --inputbox "Max image upload size in MB:" 8 58 "$var_image_max" 3>&1 1>&2 2>&3) || exit

    if whiptail --backtitle "Argus LXC" --title "FEDERATION" \
       --yesno "Enable federation sync endpoints?" 8 58 --defaultno; then
        var_federation="true"
    fi

    var_tile_source=$(whiptail --backtitle "Argus LXC" --title "MAP TILES" \
        --menu "Map tile source:" 12 58 2 \
        "osm"       "OpenStreetMap (default, requires internet)" \
        "protomaps" "Self-hosted Protomaps (fully offline)" \
        3>&1 1>&2 2>&3) || exit

    PROTOMAPS_EP=""
    if [[ "$var_tile_source" == "protomaps" ]]; then
        PROTOMAPS_EP=$(whiptail --backtitle "Argus LXC" --title "PROTOMAPS" \
            --inputbox "Protomaps tile server URL:" 8 58 "http://localhost:8081" 3>&1 1>&2 2>&3) || exit
    fi
fi

# ==============================================================================
# AUTO-DETECT INFRASTRUCTURE
# ==============================================================================
echo ""

# Container ID
CTID=$(pvesh get /cluster/nextid 2>/dev/null)
# Validate the ID isn't somehow taken (LVM ghost, etc.)
while [[ -f "/etc/pve/lxc/${CTID}.conf" ]] || [[ -f "/etc/pve/qemu-server/${CTID}.conf" ]]; do
    CTID=$((CTID + 1))
done
info "Container ID: ${CTID}"

# Template storage (needs 'vztmpl' content type — typically 'local')
TMPL_STORAGE=$(pvesm status --content vztmpl 2>/dev/null | awk 'NR>1 {print $1; exit}')
[[ -z "$TMPL_STORAGE" ]] && error "No storage with 'vztmpl' content type found. Cannot store container templates."
info "Template storage: ${TMPL_STORAGE}"

# Container storage (needs 'rootdir' content type — typically 'local-lvm')
CT_STORAGE=$(pvesm status --content rootdir 2>/dev/null | awk 'NR>1 {print $1; exit}')
[[ -z "$CT_STORAGE" ]] && error "No storage with 'rootdir' content type found. Cannot create container disks."
info "Container storage: ${CT_STORAGE}"

# ==============================================================================
# DOWNLOAD TEMPLATE
# ==============================================================================
TEMPLATE_NAME="debian-12-standard"
TEMPLATE_FILE=$(pveam list "$TMPL_STORAGE" 2>/dev/null | grep "$TEMPLATE_NAME" | awk '{print $1}' | sort -V | tail -1)

if [[ -z "$TEMPLATE_FILE" ]]; then
    info "Downloading Debian 12 template..."
    pveam update &>/dev/null || true
    AVAILABLE=$(pveam available --section system 2>/dev/null | grep "$TEMPLATE_NAME" | awk '{print $2}' | sort -V | tail -1)
    [[ -z "$AVAILABLE" ]] && error "Cannot find Debian 12 template. Check internet connectivity."
    pveam download "$TMPL_STORAGE" "$AVAILABLE" &>/dev/null || error "Template download failed."
    TEMPLATE_FILE=$(pveam list "$TMPL_STORAGE" 2>/dev/null | grep "$TEMPLATE_NAME" | awk '{print $1}' | sort -V | tail -1)
    [[ -z "$TEMPLATE_FILE" ]] && error "Template downloaded but not found in storage."
fi
info "Template: ${TEMPLATE_FILE}"

# ==============================================================================
# BUILD NETWORK CONFIG
# ==============================================================================
NET_CONFIG="name=eth0,bridge=${var_brg}"
if [[ "$var_net" == "static" ]]; then
    NET_CONFIG="${NET_CONFIG},ip=${STATIC_IP},gw=${GATEWAY}"
else
    NET_CONFIG="${NET_CONFIG},ip=dhcp"
fi

# ==============================================================================
# CREATE CONTAINER
# ==============================================================================
echo ""
info "Creating LXC container..."
pct create "$CTID" "$TEMPLATE_FILE" \
    --hostname "$var_hostname" \
    --cores "$var_cpu" \
    --memory "$var_ram" \
    --rootfs "${CT_STORAGE}:${var_disk}" \
    --net0 "$NET_CONFIG" \
    --unprivileged "$var_unprivileged" \
    --features nesting=1 \
    --onboot 1 \
    --tags "community-script;argus" \
    --start 0 &>/dev/null

info "Container ${CTID} created."

# --- Attach data volume -------------------------------------------------------
pct set "$CTID" -mp0 "${CT_STORAGE}:${var_data_disk},mp=/data" &>/dev/null
info "Data volume: ${var_data_disk} GB mounted at /data"

# --- Start container ----------------------------------------------------------
pct start "$CTID" &>/dev/null
info "Container started. Waiting for network..."
sleep 5

# --- Detect IP after boot (the community-script way) -------------------------
IP_ADDR=""
for i in {1..10}; do
    IP_ADDR=$(pct exec "$CTID" -- hostname -I 2>/dev/null | awk '{print $1}') || true
    [[ -n "$IP_ADDR" ]] && break
    sleep 2
done
[[ -z "$IP_ADDR" ]] && IP_ADDR="(unable to detect — check container networking)"
info "IP address: ${IP_ADDR}"

# ==============================================================================
# INSTALL ARGUS INSIDE CONTAINER
# ==============================================================================
echo ""
info "Installing ${APP} inside container..."

pct exec "$CTID" -- bash -c "
    set -euo pipefail

    # Minimal deps
    apt-get update -qq &>/dev/null
    apt-get install -y -qq curl ca-certificates &>/dev/null

    # Directory structure
    mkdir -p /data/images /data/db /opt/argus/static /opt/argus/templates

    # Download binary (placeholder — uncomment for release builds)
    # ARCH=\$(dpkg --print-architecture)
    # case \"\$ARCH\" in
    #     amd64) BINARY=\"argus-linux-amd64\" ;;
    #     arm64) BINARY=\"argus-linux-arm64\" ;;
    #     *)     echo \"Unsupported architecture: \$ARCH\"; exit 1 ;;
    # esac
    # curl -fsSL \"https://github.com/dwightsabeast/argus/releases/latest/download/\${BINARY}\" -o /opt/argus/argus
    # chmod +x /opt/argus/argus

    # Placeholder binary until first release
    cat > /opt/argus/argus <<'PLACEHOLDER'
#!/bin/bash
echo \"Argus placeholder — replace with real binary from GitHub Releases.\"
echo \"  Build:  cd argus && make build\"
echo \"  Copy:   pct push ${CTID} ./build/argus /opt/argus/argus\"
echo \"  Start:  systemctl restart argus\"
PLACEHOLDER
    chmod +x /opt/argus/argus

    # Environment config
    cat > /opt/argus/argus.env <<ENVFILE
LISTEN_ADDR=:8080
DB_PATH=/data/db/argus.db
DATA_PATH=/data/images
IMAGE_MAX_SIZE_MB=${var_image_max}
PAGE_SIZE=50
FEDERATION_ENABLED=${var_federation}
MAP_TILE_SOURCE=${var_tile_source}
PROTOMAPS_ENDPOINT=${PROTOMAPS_EP:-}
ENVFILE

    # systemd service
    cat > /etc/systemd/system/argus.service <<'SERVICE'
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
SERVICE

    systemctl daemon-reload
    systemctl enable argus.service &>/dev/null
" &>/dev/null

info "${APP} installed."

# ==============================================================================
# SUMMARY
# ==============================================================================
echo ""
echo -e "${CY}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "  ${BLD}${APP} LXC created successfully.${NC}"
echo -e "${CY}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "  ${BLD}ID:${NC}       ${CTID}"
echo -e "  ${BLD}Hostname:${NC} ${var_hostname}"
echo -e "  ${BLD}IP:${NC}       ${IP_ADDR}"
echo -e "  ${BLD}CPU:${NC}      ${var_cpu} cores"
echo -e "  ${BLD}RAM:${NC}      ${var_ram} MB"
echo -e "  ${BLD}OS Disk:${NC}  ${var_disk} GB"
echo -e "  ${BLD}Data:${NC}     ${var_data_disk} GB → /data"
echo -e ""
echo -e "  ${BLD}Access:${NC}   ${GN}http://${IP_ADDR}:8080${NC}"
echo -e ""
echo -e "  ${BLD}Next steps:${NC}"
echo -e "    1. Build or download the Argus binary"
echo -e "    2. pct push ${CTID} argus-linux-amd64 /opt/argus/argus"
echo -e "    3. pct exec ${CTID} -- systemctl start argus"
echo -e "    4. Set up a reverse proxy with TLS"
echo -e "${CY}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Write summary inside the LXC
pct exec "$CTID" -- bash -c "cat > /root/README.txt <<EOF
Argus Surveillance Index — Deployment Summary
==============================================
Container: ${CTID} (${var_hostname})
IP: ${IP_ADDR}
Data: /data (${var_data_disk} GB)
Config: /opt/argus/argus.env
Service: systemctl {start|stop|status} argus
Logs: journalctl -u argus -f

To expand data volume later:
  pct resize ${CTID} mp0 +50G   (on Proxmox host)
  pct exec ${CTID} -- resize2fs /dev/sdb1
EOF" &>/dev/null || true
