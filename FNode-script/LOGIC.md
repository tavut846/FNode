# FNode-script Logic and Installation Guide

This document explains the internal logic of the `FNode-script` and provides the necessary commands to install the service on a VPS.

## Core Components

The FNode deployment system consists of three main parts:
1.  **`install.sh`**: The initialization script that handles environment preparation and binary installation.
2.  **`FNode.sh`**: The management tool installed to `/usr/bin/FNode`, used for day-to-day operations.
3.  **`FNode.service`**: The systemd service unit that ensures FNode runs as a background daemon.

## Script Logic Breakdown

### 1. Installation Logic (`install.sh`)
-   **Environment Detection**: Automatically identifies the Linux distribution (CentOS, Debian, Ubuntu, Arch, Alpine) and CPU architecture (amd64, arm64).
-   **Dependency Management**: Installs only the missing tools (`curl`, `wget`, `unzip`, `tar`, `jq`, `cron`, `ca-certificates`), skipping the package manager when nothing is missing.
-   **Binary Deployment**:
    -   Fetches the latest version of the FNode backend from GitHub.
    -   Installs the binary to `/usr/local/FNode/`.
    -   Unzips only what FNode needs (no `geoip.dat`/`geosite.dat`).
-   **Service Setup**: Generates and registers a systemd unit (or OpenRC init script for Alpine) to enable auto-start on boot.
-   **Management Link**: Downloads the management script and creates a symlink at `/usr/bin/fnode` for easy access.

### 2. Management Logic (`FNode.sh` & `initconfig.sh`)
-   **Service Control**: Wraps `systemctl` commands to provide a user-friendly menu for starting, stopping, and restarting the service.
-   **Configuration Generator**: Contains a built-in wizard to generate `config.json` specifically for the **sing-box** core. It prunes non-essential options to ensure a streamlined setup. Supports TLS modes: `http`, `dns`, `self`, and `file`.
-   **Caddy & Reverse Proxy Integration**:
    -   Option to install Caddy with the Cloudflare DNS module (`caddy-dns/cloudflare`).
    -   Automated multi-domain configuration of `/etc/caddy/Caddyfile` with reverse proxy camouflage and Cloudflare DNS-01 ACME certificate issuance. `FNode caddy-port` changes the Caddy HTTPS port afterwards (see "Reality self-steal with Caddy").
    -   Auto-detection of individual Caddy certificates under `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain>/` for seamless binding across multiple nodes in FNode with `CertMode: "file"`.
-   **Log Management & Cleanup**:
    -   View real-time logs via `journalctl -u FNode.service -e --no-pager -f`.
    -   Log cleanup utility (`FNode clearlog` or menu option 9) to rotate and vacuum systemd journal logs (`--vacuum-time=1s` and `--vacuum-size=20M`), truncate file logs (`/var/log/FNode.log`, `box.log`, Caddy logs), and delete archived log files older than 7 days.
    -   **Automatic 7-day Log Cleanup**: An automated cron job is provisioned (`/etc/cron.d/FNode-cleanlog` or `/etc/periodic/weekly/fnode-cleanlog`) running every 7 days (`0 3 */7 * *`). Additionally, the Go daemon (`cmd/server.go`) runs a background ticker every 7 days to automatically clean and truncate logs.
-   **Update Mechanism**: Allows one-click updates to the latest backend version without losing configuration.

### Reality self-steal with Caddy (FNode on 443, Caddy on 8443)
A VLESS+Reality node can use its own Caddy site as the handshake destination: scanners and probes then get a real site with a valid certificate. FNode listens on 443, so Caddy must move to another port.
1.  DNS: an `A` record for the domain pointing to the VPS, **grey cloud** (DNS only) in Cloudflare, never proxied.
2.  Run the Caddy wizard first (`FNode caddy`, menu option 18). It is unchanged: Caddy on 443, no global options block.
3.  Then run `FNode caddy-port` (menu option 20; `FNode caddy-port 8443` or `CADDY_HTTPS_PORT=8443 FNode caddy-port` for scripts). It needs the Caddyfile and the caddy binary (otherwise it tells you to run the wizard), shows the current port, asks the new one (default 8443, number 1-65535), and warns when another program than caddy already listens on it. It backs up the Caddyfile to `/etc/caddy/Caddyfile.bak.<timestamp>`, writes `https_port <port>` into the Caddyfile's single global block (inserting or replacing only that line, or creating the block at the top; site blocks stay untouched), runs `caddy validate` with `/etc/caddy/caddy.env` loaded (on failure the backup is restored), restarts Caddy (systemd or openrc), waits and shows `ss -ltnp` for the port. A global block it cannot edit safely (for example `{ debug }` on one line) is left alone and the script prints the line to add. Port `443` restores the default: the option is removed, and the global block too when nothing else is in it. Certificates keep using the Cloudflare DNS-01 challenge, so neither 80 nor 443 is needed for them.
4.  Panel (Xboard, VLESS node): security Reality, **Destination Site** = the domain, **port** = the Caddy port (8443), Connection/Server port **443**, flow `xtls-rprx-vision`, multiplex off. The command prints this line at the end. If Caddy was on 443 before, make sure nothing but FNode holds 443 afterwards (`ss -ltnp | grep ':443 '`).
5.  Test: `curl -v https://<domain>:8443/` must return the decoy with a valid certificate; a plain `curl -v https://<domain>/` goes to FNode's Reality inbound on 443.
6.  Hairpin caveat: a VPS that cannot reach its own public IP cannot use the domain as `dest`; `127.0.0.1` would be needed, but FNode uses the dest host as the Reality server name, so the panel form cannot express it. Use a dest on another host in that case.

## Installation on a VPS

To install FNode on a clean VPS, use the following command. 

> [!IMPORTANT]
> Ensure you have replaced the placeholder URL with your actual GitHub repository path if you have moved the scripts.

### One-Click Installation Command
```bash
wget -N https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh && bash install.sh
```

### Post-Installation Usage
Once installed, you can manage the service by simply typing:
```bash
fnode
```
This will open the management menu where you can generate configurations, install Caddy, configure reverse proxy with Cloudflare TLS, update the core, or check service logs.

## Directory Structure (Standard Installation)
-   **Binary directory**: `/usr/local/FNode/`
-   **Configuration directory**: `/etc/FNode/`
-   **Management script**: `/usr/bin/FNode`
-   **Caddy configuration**: `/etc/caddy/Caddyfile`, `/etc/caddy/caddy.env`
-   **Caddy certificates**: `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/`
