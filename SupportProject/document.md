# Support Project Introduction

This folder contains the original projects and the dashboard server used for FNode.

## Projects

### [V2bX](./V2bX)
V2bX is the original core project for FNode. It is a multi-core node server based on Xray and Sing-box, providing support for various protocols like Vmess, Vless, Trojan, Shadowsocks, and Hysteria 1/2. It is designed to work as a backend for the V2board/Xboard panel system.

### [V2bX-script](./V2bX-script)
This project contains the original scripts for managing FNode/V2bX. It includes:
- `install.sh`: One-click installation script.
- `V2bX.sh`: Main management script for the service.
- `V2bX.service`: Systemd service definition.
- `initconfig.sh`: Script for initializing configurations.

### [Xboard](./Xboard)
Xboard is the frontend dashboard server for managing nodes and users. It is a modern panel system built on Laravel 11 that communicates with FNode (V2bX) through an API to manage nodes and track usage.
