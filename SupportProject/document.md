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
Xboard is the modern frontend dashboard server for managing nodes and users. Built on Laravel 11, it provides a comprehensive panel system for overseeing various backends (FNode, V2bX, Xboard-Node) through a unified API.

### [Xboard-Node](./Xboard-Node)
Xboard-Node is a Go-based backend specifically built by the Xboard team as the native node server. It leverages a specialized sing-box fork (`cedar2025/sing-box`) to provide first-class support for modern protocols like `xhttp` (SplitHTTP) and `naive` transport, as well as real-time synchronization via the V2 handshake API.

### [sing-box](./sing-box)
sing-box is the universal, high-performance proxy platform. In the FNode ecosystem:
- **Server-Side Core**: Sing-box is exclusively integrated as FNode's in-memory server engine, managing dynamic inbounds, multiplexing, routing, and traffic accounting.
- **Client-Side Core**: Beyond being the server core for FNode, sing-box is also an industry-standard client core engine used by prominent client applications such as the official **sing-box** client, **Karing**, **Hiddify**, and **NekoBox**.

### [mihomo-Meta](./mihomo-Meta)
[Mihomo](https://github.com/MetaCubeX/mihomo.git) (formerly Clash.Meta) is a rule-based proxy kernel based on Clash:
- **Client Core Engine**: Mihomo is the primary core engine used by client proxy applications across desktop and mobile platforms.
- **Client App Adoption**: Widely utilized by Clash-family client applications such as **Clash Verge** (and **Clash Verge Rev**), **Clash Mi**, **Clash Nyanpasu**, **Flclash**, and **Mihomo Party**, typically bundled with self-modified GUI wrappers, enhanced script engines, and custom rule providers tailored for each respective client app.
- **Panel Compatibility**: Xboard provides native subscription templates (`ClashMeta` format) tailored to Mihomo's extended features (such as VLESS Reality, TUIC v5, Hysteria 2, and Shadowsocks 2022).

