# FNode Supported Components & Configuration Reference

This document details all supported components, protocols, features, and configuration parameters in **FNode**.

---

## 1. Core Engines (`Cores[].Type`)

FNode exclusively integrates and embeds the modern **sing-box** core engine for superior memory efficiency, high throughput, and unified protocol routing.

| Core Identifier | Engine | Notes |
| :--- | :--- | :--- |
| `"sing"` | **sing-box** (v1.11+ / v1.14+) | Complete in-memory lifecycle control; supports dynamic inbounds, outbounds, and router rules without process restarts. |

---

## 2. Supported Node Types (`Nodes[].NodeType`)

FNode synchronizes configuration and users from the panel for the following node types:

| Node Type | Protocol | Transports & Features |
| :--- | :--- | :--- |
| `"shadowsocks"` | Shadowsocks | AEAD Ciphers, Shadowsocks 2022 (`2022-blake3-aes-128-gcm`, `2022-blake3-aes-256-gcm`, `2022-blake3-chacha20-poly1305`), TCP Fast Open, Multiplex. |
| `"vmess"` | VMess | Transports: `tcp` (HTTP headers), `ws` (WebSocket with early data), `grpc`, `httpupgrade`, `xhttp`. TLS / self-signed TLS. |
| `"vless"` | VLESS | Transports: `tcp`, `ws`, `grpc`, `httpupgrade`, `xhttp`. Security: Standard TLS and **VLESS-Reality** (dest fallback, short IDs, server names). |
| `"trojan"` | Trojan | Transports: `tcp`, `ws`, `grpc`, `httpupgrade`, `xhttp`. Multiplex, Fallback & ALPN-based fallback routing (`FallBackConfigs`). |
| `"tuic"` | TUIC v5 | QUIC / HTTP/3 transport, Congestion Control (`bbr`, `cubic`), Zero-RTT handshakes (`ZeroRTTHandshake`). |
| `"anytls"` | AnyTLS | Direct TLS tunneling with customizable padding scheme (`PaddingScheme`) and dynamic per-user authentication. |
| `"hysteria"` | Hysteria 1 | QUIC / UDP-based protocol, bandwidth rate control (`UpMbps`, `DownMbps`), port hopping, obfuscation (`Obfs`). |
| `"hysteria2"` | Hysteria 2 | QUIC / HTTP/3 transport, Salamander obfuscation (`salamander`), masquerade (HTTP/HTTPS reverse proxy or static file directory), client bandwidth overrides. |

---

## 3. Panel Protocols & APIs

FNode connects with **Xboard** and **V2board** using the UniProxy API standard:

- **Config Synchronization**: `GET /api/v1/server/UniProxy/config` (supports ETag and SHA-256 conditional requests to minimize payload transfer).
- **User Synchronization**: `GET /api/v1/server/UniProxy/user` (pulls active UUIDs, passwords, speed limits, and device thresholds).
- **Traffic Reporting**: `POST /api/v1/server/UniProxy/push` (batch reports uploaded/downloaded bytes per user).
- **Online Device Reporting**: `POST /api/v1/server/UniProxy/alive` (reports active IP addresses and connection counts).

---

## 4. TLS & Certificate Modes (`CertConfig.CertMode`)

FNode provides flexible TLS certificate acquisition and binding options:

| Mode | Identifier | Description |
| :--- | :--- | :--- |
| **File Mode** | `"file"` | Loads certificate from custom files (`CertFile` and `KeyFile`). Automatically integrates with **Caddy** ACME storage (`/root/.local/share/caddy/certificates/...`) if `CertDomain` is provided. |
| **DNS-01 ACME** | `"dns"` | Automatically requests and renews Let's Encrypt certificates via Lego DNS challenges. Supported providers include `cloudflare`, `alidns`, `dnspod`, and any Lego provider via `DNSEnv`. |
| **HTTP-01 ACME** | `"http"` | Requests Let's Encrypt certificate using port 80 HTTP-01 challenge. |
| **Self-Signed** | `"self"` | Automatically creates self-signed certificates on startup for testing and development. |
| **Disabled** | `"none"` | Disables TLS handling in FNode (used when TLS terminates upstream at Caddy/Nginx reverse proxy or when using Reality). |

---

## 5. Routing, Outbounds & Network Optimization

Configured under `Cores[].SingConfig` and `Nodes[].Options`:

- **IPv6 Management**:
  - `DisableIPv6`: Instantly drops IPv6 traffic to prevent connection stalls on IPv4-only servers.
  - `DomainStrategy`: `"prefer_ipv4"`, `"ipv4_only"`, `"prefer_ipv6"`, `"ipv6_only"`.
- **Dialing Timeout**:
  - `ConnectTimeout`: TCP/UDP connection dial timeout in seconds (default: `5s`).
- **Custom Route Rules (`CustomRouteRules`)**:
  - **Match conditions**: `domains`, `domain_suffixes`, `ip_cidrs`, `ports`, `networks`, `source_cidrs`, `source_ports`.
  - **Actions**:
    - `"direct"`: Bypass proxy and connect directly.
    - `"block"`: Drop connection.
    - `"route"`: Route through a specific `Target` outbound tag.
- **Custom Outbounds (`CustomOutbounds`)**: Pass raw sing-box outbound definitions (e.g. Warp, Tor, secondary proxies).
- **Base Template Merging (`OriginalPath`)**: Merge custom JSON configurations (e.g., [`sing_origin.json`](./sing_origin.json)) directly into the active sing-box instance.

---

## 6. Traffic, Rate & Connection Limiting (`LimitConfig`)

FNode includes built-in rate-limiting and concurrent device enforcement:

- **Speed Limits**:
  - `SpeedLimit`: Global or per-user rate limit (Bps / Mbps).
- **Device & Connection Limits**:
  - `DeviceLimit`: Maximum concurrent IP addresses allowed per user.
  - `ConnLimit`: Maximum concurrent TCP/UDP streams per user.
- **Dynamic Speed Limiting (`DynamicSpeedLimitConfig`)**:
  - Throttles client bandwidth after exceeding a specific traffic quota within an observation window (`Periodic`, `Traffic`, `SpeedLimit`, `ExpireTime`).
- **Cluster IP Synchronization (`IpRecorderConfig`)**:
  - Synchronizes active IP states across multiple node instances using **Redis** or a centralized **HTTP Webhook Recorder**.

---

## 7. Logging & Diagnostics

### FNode Process Logs (`Log`)
- **Config**: `"Log": { "Level": "info", "Output": "/var/log/FNode.log" }`
- **Supported Levels**: `"debug"`, `"info"`, `"warn"`, `"error"`.

### Sing-box Core Logs (`Cores[].Log`)
- **Config**: `"Log": { "Level": "info", "Timestamp": true }`
- **Supported Levels**: `"trace"`, `"debug"`, `"info"`, `"warn"`, `"error"`, `"fatal"`, `"panic"`.

### Log Maintenance & Cleanup
- **CLI Commands**: `FNode clearlog` (aliases: `cleanlog`, `clean-log`).
- **Script Management Menu**: Option `9. 清理 FNode 日志` in `FNode.sh`.
- **Cleanup Actions**:
  - Rotates and vacuums systemd journal logs (`journalctl --vacuum-time=1s` and limits journal size to `20M`).
  - Vacuums Caddy logs if Caddy service is installed.
  - Automatically truncates custom log file outputs defined in `/etc/FNode/config.json` (`Log.Output`) and standalone logs (`/var/log/FNode.log`, `/usr/local/FNode/box.log`, `/var/log/caddy/*.log`).

---

## 8. Geo IP & Geosite Assets

Located in the [`example/`](./) directory or installed under `/etc/FNode/`:
- **GeoIP Databases**: `geoip.dat`, `geoip.db`
- **GeoSite Rule Lists**: `geosite.dat`, `geosite.db`
