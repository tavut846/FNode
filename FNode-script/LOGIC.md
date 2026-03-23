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
-   **Dependency Management**: Installs required tools like `curl`, `wget`, `unzip`, and `socat` (for TLS).
-   **Binary Deployment**:
    -   Fetches the latest version of the FNode backend from GitHub.
    -   Installs the binary to `/usr/local/FNode/`.
    -   Copies required assets like `geoip.dat` and `geosite.dat` to `/etc/FNode/`.
-   **Service Setup**: Generates and registers a systemd unit (or OpenRC init script for Alpine) to enable auto-start on boot.
-   **Management Link**: Downloads the management script and creates a symlink at `/usr/bin/fnode` for easy access.

### 2. Management Logic (`FNode.sh`)
-   **Service Control**: Wraps `systemctl` commands to provide a user-friendly menu for starting, stopping, and restarting the service.
-   **Configuration Generator**: Contains a built-in wizard to generate `config.json` specifically for the **sing-box** core. It prunes non-essential options to ensure a streamlined setup.
-   **Update Mechanism**: Allows one-click updates to the latest backend version without losing configuration.

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
This will open the management menu where you can generate certificates, update the core, or check service logs.

## Directory Structure (Standard Installation)
-   **Binary directory**: `/usr/local/FNode/`
-   **Configuration directory**: `/etc/FNode/`
-   **Management script**: `/usr/bin/FNode`
