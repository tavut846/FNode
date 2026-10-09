# FNode Comprehensive Project Guide & Architecture

## 1. Executive Summary

**FNode** (`github.com/tavut846/FNode`) is a high-performance Go-based backend proxy node agent. It acts as the bridge between a centralized management panel (**Xboard**) and the **sing-box** proxy engine. 

FNode was derived from [V2bX](https://github.com/wyx2685/V2bX) and [V2bX-script](https://github.com/wyx2685/V2bX-script). Unlike V2bX, FNode is **sing-box only** — obsolete Xray and standalone Hysteria cores have been eliminated in favor of a lean, unified, and highly optimized sing-box implementation.

```
                    ┌─────────────────────────────────────────┐
                    │            Xboard Dashboard             │
                    │        (Centralized Panel / API)        │
                    └────────────────────┬────────────────────┘
                                         │  REST API (UniProxy)
                                         ▼
                    ┌─────────────────────────────────────────┐
                    │               FNode Node                │
                    │  ├── api/panel/   (API Client)          │
                    │  ├── conf/        (JSON5 Config & Watch)│
                    │  ├── node/        (Node & Cert Control) │
                    │  ├── limiter/     (Speed & Device Limit)│
                    │  └── core/sing/   (Singbox Core Adapter)│
                    └────────────────────┬────────────────────┘
                                         │  Go API / In-Memory
                                         ▼
                    ┌─────────────────────────────────────────┐
                    │             sing-box Core               │
                    │  ├── Inbound Traffic (VLESS/VMess/etc.) │
                    │  ├── HookServer (Connection Tracker)    │
                    │  └── Outbound Network Routing           │
                    └─────────────────────────────────────────┘
```

---

## 2. Component Roles & Ecosystem

### 2.1 Xboard (`SupportProject/Xboard`)
- The main web panel written in PHP/Laravel.
- Provides RESTful UniProxy endpoints:
  - `GET  /api/v1/server/UniProxy/config`: Node server configuration, port, security settings, transport settings (supports ETag caching).
  - `GET  /api/v1/server/UniProxy/user`: User credentials (UUIDs, passwords), speed limits, device limits (supports ETag caching, MsgPack & JSON streaming).
  - `GET  /api/v1/server/UniProxy/alivelist`: Cluster-wide online IP count per user for device limit enforcement.
  - `POST /api/v1/server/UniProxy/push`: Periodic user traffic upload/download consumption report.
  - `POST /api/v1/server/UniProxy/alive`: Report currently online client IPs associated with user IDs.
- **Authentication**: Every request includes `node_type`, `node_id`, and `token` query parameters.

### 2.2 sing-box (`SupportProject/sing-box`)
- The modern, universal proxy core engine.
- FNode does not generate or invoke external sing-box CLI processes or static configuration files. Instead, it drives sing-box **programmatically** via its Go library interface:
  - Inbounds are added dynamically using `box.Router().AddInbound(...)`.
  - Inbounds are deleted via `box.Router().DeleteInbound(tag)`.
  - Users are added/removed on the fly using `core.AddUsers(...)` / `core.DelUsers(...)`.
  - Connection interception and byte counting are achieved via `HookServer` (`adapter.ConnectionTracker`).

### 2.3 FNode-script (`FNode-script/`)
- Contains installation and administration scripts for Linux servers:
  - `install.sh`: Downloads prebuilt binaries, sets up systemd/openrc services, sets file permissions, and initializes `/etc/FNode/`. The systemd service is explicitly configured with `LimitNOFILE=1048576` and `LimitNPROC=512000` to prevent socket descriptor and process exhaustion during high-concurrency proxy traffic.
  - `FNode.sh`: Comprehensive management menu (`FNode` CLI command) supporting service control, log inspection, log cleanup, config generation, BBR installation, Caddy installation, and Cloudflare reverse proxy configuration.
  - `initconfig.sh`: Interactive CLI wizard for creating `/etc/FNode/config.json`. Defaults `DisableIPv6: true` across core and node configurations to eliminate outbound AAAA DNS lookup delays, routing stalls, and transient socket reset errors, while automatically binding inbounds to `[::]` on dual-stack hosts.

### 2.4 Caddy Integration & Reverse Proxy
- Caddy serves as a front-facing reverse proxy and automated SSL certificate manager.
- Supports Cloudflare DNS-01 ACME challenges (`tls { dns cloudflare {env.CLOUDFLARE_API_TOKEN} }`).
- Stores generated certificates in `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain>/`.
- FNode seamlessly ingests Caddy certificates using `CertMode: "file"`.

### 2.5 End-to-End Communication Flow Logic
For an exhaustive analysis with diagrams, payloads, and sequence charts, see [docs/COMMUNICATION_FLOW_LOGIC.md](docs/COMMUNICATION_FLOW_LOGIC.md). The system operates on 6 primary information flows:
1. **FNode → Manager Dashboard**: Heartbeats (`touchNode`), user traffic upload/download bytes (`POST /push`), active device IPs (`POST /alive`).
2. **FNode → Client**: TLS/Reality handshakes, decrypted internet response streaming, token-bucket bandwidth rate limiting, and instant IPv6 blocking (`DisableIPv6`).
3. **Manager Dashboard → FNode**: Dynamic inbound configuration (`GET /config`), user lists with UUIDs, speed and device limits (`GET /user`), cluster-wide online counts (`GET /alivelist`).
4. **Manager Dashboard → Client**: Subscription delivery (`/api/v1/client/subscribe`), node connection profiles (Clash/Sing-box/Shadowrocket), account quotas & expiry headers (`Subscription-Userinfo`).
5. **Client → FNode**: Cryptographic identity handshakes (UUID/password), encapsulated target requests (`host:port`), and upstream client data.
6. **Client → Manager Dashboard**: Web portal account login, subscription updates, plan purchases, payments, and support ticket submissions.

---

## 3. Directory Layout

```
FNode/
├── api/panel/            # Xboard REST API client implementation
│   ├── panel.go          # Client initialization, query parameters, timeouts
│   ├── node.go           # NodeInfo parser, ETag & response hash validation
│   ├── user.go           # User list streaming (MsgPack/JSON), traffic reporting
│   └── utils.go          # Response validation and HTTP error checking
├── build_assets/         # Compiled binaries and release assets
├── cmd/                  # CLI commands powered by Cobra (server, version, etc.)
│   ├── cmd.go            # Root command definition
│   └── server.go         # 'server' command running the node controllers
├── common/               # Shared utility packages
│   ├── counter/          # Atomic traffic counters
│   ├── crypt/            # Cryptographic helpers
│   ├── file/             # File existence and I/O helpers
│   ├── format/           # Tag and string formatting
│   ├── json5/            # JSON5 decoding support
│   ├── rate/             # Token bucket rate limiting
│   ├── systime/          # System timing helpers
│   └── task/             # Periodic task scheduler
├── conf/                 # Configuration structures and file loader
│   ├── cert.go           # CertConfig structure
│   ├── conf.go           # Top-level Conf parser (Log, Cores, Nodes)
│   ├── node.go           # NodeConfig, ApiConfig, Options
│   ├── sing.go           # SingConfig, SingOptions
│   └── watch.go          # Hot-reload file watcher using fsnotify
├── core/                 # Proxy core abstraction layer
│   ├── interface.go      # vCore.Core interface definition
│   ├── selector.go       # Core registry and factory
│   └── sing/             # sing-box implementation
│       ├── sing.go       # Sing struct, lifecycle (Start/Close)
│       ├── node.go       # Node translation (NodeInfo -> option.Inbound)
│       ├── user.go       # User injection and removal
│       └── hook.go       # HookServer (ConnectionTracker, limiter enforcement)
├── docs/                 # Documentation and architecture references
├── example/              # Example configuration files (config.json)
├── FNode-script/         # Deployment and maintenance shell scripts
├── limiter/              # Speed limiter, device limiter, and rule audit
│   ├── limiter.go        # Main rate & device limiter (CheckLimit)
│   ├── rule.go           # Protocol & domain rule blocking
│   └── dynamic.go        # Dynamic speed limit calculator
├── main.go               # Program entrypoint (calls cmd.Execute())
├── go.mod / go.sum       # Module dependencies (requires GOEXPERIMENT=jsonv2)
└── SupportProject/       # Upstream references and core sources
    ├── sing-box/         # sing-box source tree
    ├── V2bX/             # Original V2bX upstream reference
    ├── V2bX-script/      # Original V2bX-script reference
    ├── Xboard/           # Xboard panel source
    └── Xboard-Node/      # Native Xboard node implementation
```

---

## 4. Configuration Reference (`/etc/FNode/config.json`)

FNode configuration is JSON5 compatible (permits comments and trailing commas).

```json5
{
  "Log": {
    "Level": "info",       // debug, info, warn, error, none
    "Output": ""          // Path to log file, or empty for stdout
  },
  "Cores": [
    {
      "Type": "sing",     // Only "sing" is supported
      "Log": {
        "Level": "info",
        "Timestamp": true
      },
      "DisableIPv6": true, // Defaults to true: rejects outbound IPv6 immediately to prevent hangs and AAAA lookup delays
      "DomainStrategy": "prefer_ipv4",
      "ConnectTimeout": 5, // Outbound dial timeout in seconds
      "NTP": {
        "Enable": false,
        "Server": "time.apple.com",
        "ServerPort": 0
      },
      "OriginalPath": "/etc/FNode/sing_origin.json" // Optional sing-box base config
    }
  ],
  "Nodes": [
    {
      "Core": "sing",
      "ApiHost": "https://xboard.example.com",
      "ApiKey": "secret_token_from_xboard",
      "NodeID": 1,
      "NodeType": "vless", // vmess, vless, trojan, shadowsocks, hysteria, hysteria2, tuic, anytls
      "Timeout": 30,
      "ListenIP": "0.0.0.0",
      "SendIP": "0.0.0.0",
      "DeviceOnlineMinTraffic": 200, // KB minimum traffic before counting device online
      "MinReportTraffic": 0,
      "TCPFastOpen": false,
      "SniffEnabled": true,
      "CertConfig": {
        "CertMode": "file", // file, http, dns, self, none
        "RejectUnknownSni": false,
        "CertDomain": "node1.example.com",
        "CertFile": "/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/node1.example.com/node1.example.com.crt",
        "KeyFile": "/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/node1.example.com/node1.example.com.key",
        "Provider": "cloudflare",
        "Email": "admin@example.com",
        "DNSEnv": {
          "CF_DNS_API_TOKEN": "your_token"
        }
      }
    }
  ]
}
```

### 4.2 Base Configuration (`sing_origin.json`) & Route Rules Persistence

When `OriginalPath` points to a base sing-box config (e.g. `/etc/FNode/sing_origin.json`), FNode seamlessly combines panel-managed controls with native sing-box capabilities:
- **Custom Outbounds**: Custom protocol outbounds (`shadowsocks`, `vless`, `vmess`, `trojan`, `hysteria2`, `hysteria`, `tuic`, `anytls`, `ssh`, `socks`, `http`, `shadowtls`, `selector`, `urltest`, etc.) declared in `sing_origin.json` or `CustomOutbounds` are loaded into the core. Complete templates for all protocols are provided in `example/custom_outbound.json`, while `example/sing_origin.json` retains the default configuration generated by FNode install scripts.
- **Persistent Routing & Rule Sets**: FNode preserves all base `route.rules` and `route.rule_set` (including remote binary `.srs` rule sets) across dynamic node sync events (`AddNode`/`DelNode`).
- **Node Protection & Anti-Abuse Rules**: The default `sing_origin.json` generated by `initconfig.sh` and `FNode.sh` incorporates multi-layered protection designed for shared nodes:
  - **BitTorrent Protocol Filter**: Matches wire protocol `bittorrent` directly to drop P2P/uTP traffic at packet level and prevent DMCA copyright strikes.
  - **SMTP Anti-Spam Filter**: Blocks outbound TCP traffic on ports 25, 465, and 587 to prevent mail spam abuse and VPS provider account suspension.
  - **SSRF Mitigation**: Rejects private IP ranges (`ip_is_private: true`).
  - **Modernized Domain Regex**: Blocks categorized threat domains without catastrophic backtracking or syntax leaks (using robust character class `[.]`):
    - Torrent trackers, portals, and download clients (`thepiratebay`, `1337x`, `nyaa`, `opentrackr`, `qBittorrent`, `xunlei`)
    - Cryptojacking & mining pools (`ethermine`, `supportxmr`, `nanopool`, `nicehash`, `stratum`)
    - Disposable mail services & spamboxes (`guerrillamail`, `temp-mail`, `10minutemail`, `mailinator`)
    - Carding, credit card validation & DDoS stressers (`bincheck`, `mrchecker`, `ipstresser`, `booter`)
    - Public speedtest scrapers and probe bots (`miaoko`, `flows.pages.dev`)
    - Antivirus honeypots & telemetry reporting (`360`, `guanjia.qq.com`, `rising`, `kingsoft`, `umeng`)
    - Cyber police & reporting portals (`12377`, `12321`, `12315`, `cyberpolice`, `110.qq`)
- **Rule Precedence Chain**:
  1. Anti-SSRF Protection (private IPv4 & IPv6 CIDRs blocked)
  2. IPv6 Blocking (when `DisableIPv6: true`)
  3. Panel-defined route rules (audits, blocked protocols/domains from Xboard)
  4. Base `sing_origin.json` route rules (custom outbound detours, domain suffixes, rule-sets, regex, protocol blocks)
  5. Fallback final outbound (`direct`)

This eliminates the need to bloat `config.json` with duplicated rules, allowing complex routing and remote rule sets to be maintained cleanly in `sing_origin.json`.

---

## 5. Certificate Management & Caddy Integration

### 5.1 Supported Certificate Modes
| Mode | Description | Automated Renewal |
|------|-------------|-------------------|
| `file` | Reads existing `.crt` and `.key` files from filesystem (e.g. Caddy). If `CertFile`/`KeyFile` are omitted or not found, FNode automatically locates Caddy's certificates for `CertDomain`. | Managed externally (by Caddy) |
| `http` | Automatically requests Let's Encrypt cert via port 80 HTTP-01 challenge using Lego. | Automated by FNode Lego task |
| `dns` | Automatically requests Let's Encrypt cert via DNS-01 challenge using DNS provider API (e.g. Cloudflare). | Automated by FNode Lego task |
| `self` | Generates a local self-signed RSA-2048 certificate. | Valid for 30 years |
| `none` | Disables TLS configuration (e.g. for plain Shadowsocks or VLESS-Reality). | N/A |

### 5.2 Caddy Reverse Proxy & Multi-Domain Setup
Caddy can be configured using `FNode caddy` or option `18` in `FNode.sh`. The wizard supports configuring multiple domains (comma- or space-separated, e.g. `domain1.com, domain2.com`) and generates independent site blocks directing incoming traffic through a camouflage reverse proxy target while obtaining SSL certificates using Cloudflare DNS-01 validation:

```caddy
domain1.com {
    encode gzip

    tls {
        dns cloudflare {env.CLOUDFLARE_API_TOKEN}
        protocols tls1.2 tls1.3
    }

    reverse_proxy https://simulate-news.316293.xyz {
        header_up Host simulate-news.316293.xyz
        header_up X-Real-IP {http.request.remote}
        header_up X-Forwarded-Proto https
        header_up Cache-Control "no-cache, no-store, must-revalidate"
        header_up Pragma "no-cache"
        header_up Expires "0"

        header_down Cache-Control "no-cache, no-store, must-revalidate"
        header_down Pragma "no-cache"
        header_down Expires "0"
    }
}

domain2.com {
    encode gzip

    tls {
        dns cloudflare {env.CLOUDFLARE_API_TOKEN}
        protocols tls1.2 tls1.3
    }

    reverse_proxy https://simulate-news.316293.xyz {
        header_up Host simulate-news.316293.xyz
        header_up X-Real-IP {http.request.remote}
        header_up X-Forwarded-Proto https
        header_up Cache-Control "no-cache, no-store, must-revalidate"
        header_up Pragma "no-cache"
        header_up Expires "0"

        header_down Cache-Control "no-cache, no-store, must-revalidate"
        header_down Pragma "no-cache"
        header_down Expires "0"
    }
}
```

When Caddy runs, individual certificates for each domain are stored in:
- `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain1>/<domain1>.crt`
- `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain1>/<domain1>.key`
- `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain2>/<domain2>.crt`
- `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain2>/<domain2>.key`

Both `initconfig.sh` and FNode's `node/cert.go` (`findCaddyCertificate`) automatically inspect and resolve these paths when `CertMode: "file"` is selected for any configured node.

### 5.3 REALITY Configuration & Camouflage Handshake (VLESS & Trojan)
REALITY eliminates traditional server-side TLS certificates by impersonating existing TLS 1.3 servers (e.g. Apple, Microsoft, Cloudflare). FNode implements full native REALITY support for VLESS and Trojan nodes using sing-box:

1. **SNI & ServerName Alignment (`tls.server_name`)**:
   - In sing-box (`common/tls/reality_server.go`), inbound SNI validation is governed by `tlsConfig.ServerNames = map[string]bool{options.ServerName: true}`.
   - FNode automatically extracts `server_name` from the panel's `tls_settings` (with fallback to `dest` host or common `server_name`) and binds it to `tls.ServerName`. This ensures incoming TLS ClientHello SNI matches the expected camouflage domain rather than falling through to probe fallbacks.

2. **Handshake Target & Destination Splitting (`dest:port`)**:
   - Camouflage servers can be supplied via `dest` (e.g. `gateway.icloud.com:443`) or `server_name` with optional `server_port`.
   - FNode robustly splits `host:port` pairs, preventing invalid `:port:port` concatenation in sing-box dialers.
   - If destination port is omitted or zero, it automatically defaults to `443`.

3. **Panel JSON Compatibility**:
   - Xboard sends `tls_settings.server_port` as a numeric integer (e.g. `443`) and `xver` as an integer. FNode's `TlsSettings.UnmarshalJSON` accepts both integer and string variants seamlessly.
   - `short_id` flexibly accepts single strings, comma-separated strings, or JSON arrays of hexadecimal strings.

4. **Engine Build Requirement**:
   - REALITY is powered by sing-box's uTLS stack. All compilation and tests must include the `-tags "with_utls"` build tag (`-tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor"`).

---

## 6. Build & Test Instructions

### 6.1 Prerequisites
- **Go Version**: 1.25 or higher
- **GOEXPERIMENT**: Must be set to `jsonv2` for `encoding/json/v2` and `encoding/json/jsontext` packages.

### 6.2 Running Tests
```bash
# In PowerShell:
$env:GOEXPERIMENT="jsonv2"; go test ./...

# In Bash:
GOEXPERIMENT=jsonv2 go test ./...
```

### 6.3 Building the Release Binary
```bash
# In PowerShell:
$env:GOEXPERIMENT="jsonv2"
go build -v -o build_assets/FNode.exe `
  -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" `
  -trimpath `
  -ldflags "-X 'github.com/tavut846/FNode/cmd.version=v1.0.0' -s -w -buildid=" .

# In Linux / Bash:
GOEXPERIMENT=jsonv2 go build -v -o build_assets/FNode \
  -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" \
  -trimpath \
  -ldflags "-X 'github.com/tavut846/FNode/cmd.version=v1.0.0' -s -w -buildid=" .
```

### 6.4 Automated CI/CD & Version Tagging Strategy

FNode uses GitHub Actions (`.github/workflows/release.yml`) for automated multi-platform builds and releases:
- **Continuous Commit Releases**:
  - Each new commit build automatically fetches all existing tags (`git fetch --tags --force`) and increments the patch version (+1, e.g. `0.0.13` -> `0.0.14` -> `0.0.15`).
  - Automatically checks and verifies against existing git tags to prevent tag collisions, ensuring each commit creates a brand new GitHub Release instead of overwriting/replacing existing release assets.
- **`master` / `main` / `stable` Branches**:
  - Increments the stable patch version and publishes a full GitHub Release marked as **Latest** (`make_latest: true`, `prerelease: false`).
- **`dev` / `dev_new` Branches**:
  - Automatically formats the tag in pre-release form: `<upgraded_version>-pre-<commit_number>` (e.g. `0.0.14-pre-1`, `0.0.14-pre-2`).
  - Publishes a GitHub Release marked as **Pre-release** (`prerelease: true`, `make_latest: false`).
- **Verification Gate**:
  - Every release workflow automatically runs the complete test suite (`GOEXPERIMENT=jsonv2 go test -v -tags "with_utls" ./...`) before any release artifact is built or published.

### 6.5 Dependabot & Dependency Management
FNode uses Dependabot (`.github/dependabot.yml`) paired with automated GitHub Actions (`.github/workflows/dependabot.yml`):
- **Grouped Updates**: Go module updates and GitHub Actions are grouped into weekly bundles (`groups.dependencies` and `groups.actions`) to prevent multiple conflicting PRs and excessive CI runs.
- **Automated Non-Major Merging**: Safe minor and patch updates can be automatically tested and merged, keeping core libraries secure with minimal manual intervention.

---

## 7. Guidelines for AI Assistants & Contributors

1. **Always preserve sing-box core exclusivity**: Never reintroduce Xray, Hy1-standalone, or legacy V2bX dependencies.
2. **Preserve Query Param Auth**: In `api/panel/panel.go`, query parameter keys (`node_type`, `node_id`, `token`) are strictly expected by Xboard.
3. **Be vigilant about network nil guards**: All HTTP responses in `api/panel/` must check `if err != nil` and `if r == nil` before dereferencing `r.StatusCode()` or `r.Body()`.
4. **Maintain Hot-Reload Integrity**: Config changes are watched by `conf/watch.go`. Test watchers must never block with an unescaped `select {}`.
5. **Keep tests decoupled from live ACME endpoints**: Never make live ACME challenge calls in unit tests without verifying explicit environment credentials.
6. **Mandatory Testing Before Reporting Done**: After making ANY code or configuration change, AI assistants (Antigravity, Gemini, Claude, Cursor, Copilot, etc.) must write and run tests verifying that the change actually works before reporting done (`$env:GOEXPERIMENT="jsonv2"; go test -v -tags "with_utls" ./...`).
