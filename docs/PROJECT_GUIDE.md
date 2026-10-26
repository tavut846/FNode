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
- **Server-Side Core (FNode)**:
  FNode does not generate or invoke external sing-box CLI processes or static configuration files. Instead, it drives sing-box **programmatically** via its Go library interface:
  - Inbounds are added dynamically using `box.Router().AddInbound(...)`.
  - Inbounds are deleted via `box.Router().DeleteInbound(tag)`.
  - Users are added/removed on the fly using `core.AddUsers(...)` / `core.DelUsers(...)`.
  - **User Debugging & Tag Formatting**: When users are registered in sing-box inbound options (VLESS, VMess, Shadowsocks, Trojan, TUIC, Hysteria, Hysteria2, AnyTLS), their `Name` field is formatted as `email: [inbound_tag]|uuid` (e.g., `email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-...`), matching the familiar V2bX debugging format. Sing-box logs every inbound connection and packet with this tag, allowing administrators to grep their own token/UUID directly (`grep <token> /var/log/...`) to trace connection events, rate limiting, and rule rejections.
  - Connection interception and byte counting are achieved via `HookServer` (`adapter.ConnectionTracker`), which extracts clean UUIDs (`format.ExtractUUID`) for accurate traffic tracking and tagUUIDs (`format.ExtractTagUUID`) for speed/device rate limit lookups.
- **Client-Side Core (Ecosystem)**:
  Beside serving as the backbone server core for FNode, sing-box is also a preeminent client core powering next-generation cross-platform proxy clients:
  - **Official sing-box Client**: Multiplatform GUI/CLI (iOS, Android, macOS, Windows, Linux).
  - **Karing**: Modern cross-platform proxy utility utilizing sing-box core.
  - **Hiddify**: Universal multi-platform client built on top of the sing-box core engine.
  - **NekoBox**: Feature-rich client on Android and desktop leveraging sing-box core.

### 2.3 Mihomo (Clash.Meta) Client Core (`SupportProject/mihomo-Meta`)
- [Mihomo](https://github.com/MetaCubeX/mihomo.git) (formerly Clash.Meta) is an open-source, rule-based proxy kernel written in Go:
  - **Dominant Client Core**: Mihomo is the primary client core used by the modern Clash ecosystem across Windows, macOS, Linux, and Android.
  - **Client Applications**: Powers applications such as **Clash Verge** / **Clash Verge Rev**, **Clash Mi**, **Clash Nyanpasu**, **Flclash**, and **Mihomo Party**. Most client applications embed Mihomo with customized GUI wrappers, rule converters, and client-specific UX enhancements.
  - **Panel & Node Interoperability**: Xboard provides tailored subscription outputs (`ClashMeta` format) that map nodes directly into Mihomo's extended protocol capabilities (VLESS Reality, Shadowsocks 2022, Hysteria 2, TUIC v5), which FNode terminates and proxies on the server side.

### 2.4 FNode-script (`FNode-script/`)
- Contains installation and administration scripts for Linux servers:
  - `install.sh`: Downloads prebuilt binaries, sets up systemd/openrc services, sets file permissions, and initializes `/etc/FNode/`. The systemd service is explicitly configured with `LimitNOFILE=1048576` and `LimitNPROC=512000` to prevent socket descriptor and process exhaustion during high-concurrency proxy traffic. Supports interactive version selection (Latest Release, Pre-release, or Specific Version) as well as direct CLI arguments (`bash install.sh latest`, `bash install.sh pre`, or `bash install.sh <version>`). Supports `FNODE_BRANCH` environment variable (defaults to `master`) for testing branch scripts. Non-interactive mode (works piped, `curl -fsSL …/install.sh | sudo bash -s -- …`, with no TTY and no prompts): `--mode machine --panel URL --token MACHINE_TOKEN --machine-id ID` or `--mode node --panel URL --token SERVER_TOKEN --node-id ID [--type TYPE]`, plus `--version x.y.z|latest|pre`, `--branch NAME`, `--cert-mode`, `--cert-domain`, An existing `/etc/FNode/config.json` no longer wins over the dashboard flags: it is backed up (`config.json.bak.<time>`) and its `Nodes` block is replaced with the flag entry (`jq`, installed by `install_base`, keeps `Log`/`Cores`; a FNode `Log.Level` of `error`/`fatal`/`panic` is raised to `info` so the panel connection shows in the journal). Without `jq`, or for a JSON5 config, the whole file is rewritten from defaults. `--keep-config` leaves an existing config untouched (the flags are then ignored), `--force` is still accepted from older dashboard commands and does nothing extra, `--print-config` is a dry run and `--print-config --existing PATH` prints PATH merged with the flags. The default FNode log level of a generated config is `info`. `install.sh uninstall --yes` removes FNode without prompts. Small-VPS behaviour (see 6.3.1): only missing packages are installed (`wget curl unzip tar jq`, `cron`, `ca-certificates`; no `socat`, no `apt-get update` when nothing is missing, `--no-install-recommends`), the install stops with a clear message when less than 120 MB of disk is free, only the files FNode needs are unzipped (`geoip.dat`/`geosite.dat` are skipped and no longer copied to `/etc/FNode/`). Every step is checked and the installer stops with what failed and "Nothing was changed" instead of printing success: after the package install it needs a downloader (`wget` or `curl`) and an extractor (`unzip`, else `bsdtar`, `python3` or `busybox unzip`, via `extract_zip`) and retries the package install once with the package manager's output visible; the zip is downloaded (`download_file`, wget then curl, HTTP errors fail, files under 1 MB are rejected) and unpacked into `/usr/local/FNode.new`, the binary must exist and `FNode version --short` must run (wrong architecture, noexec mount), and only then is `/usr/local/FNode` replaced (the old install is kept until the new one is in place, so a failed update leaves the working install). **Version lookup** (`resolve_version`, `resolve_explicit`): `latest` is the newest stable release and `pre` the newest release of any kind (a pre-release channel never falls behind a newer stable release), skipping drafts, `draft-*` tags and releases without `FNode-linux-<arch>.zip`. The GitHub API list is read with `jq` or, without it, an awk JSON walker that does not depend on key order or line breaks; when the API fails (the unauthenticated limit is 60 requests/hour per IP, reported as such; `GITHUB_TOKEN` raises it) the lookup falls back to the `releases/latest` redirect, the `releases.atom` feed and `git ls-remote`, checking each candidate's zip exists. An explicit version (`0.0.22-pre`, `v0.0.22`) is tried with and without the `v`. `FNODE_GH_API`, `FNODE_GH_WEB` and `FNODE_RAW` override the endpoints. **An update never changes the user's files**: `FNode update <version>` (a version and no panel flags) replaces only the binary tree `/usr/local/FNode` and the service unit and restarts; `/etc/FNode/config.json`, `sing_origin.json`, certificates and `generated/` are not written (only missing example JSONs are added), the config is checksummed before and restored if anything changed it, and `FNode.sh` refuses an empty or non-script `install.sh` download instead of reporting success. Only a dashboard install (`--mode ...`) rewrites the `Nodes` block, after a backup. The systemd unit has `ConditionPathIsExecutable=/usr/local/FNode/FNode` (openrc: `start_pre`), so a missing binary no longer restarts every 10 seconds forever. "Restart succeeded" is only printed when the service is really running; a dashboard install whose service does not start prints the last journal lines and exits non-zero. and the systemd unit gets `Environment=GOMEMLIMIT=<70% of RAM>` (RAM under 2 GB) and `MemoryMax=<85% of RAM>` (RAM under 1.5 GB); an openrc service exports the same `GOMEMLIMIT`. A swapfile command is printed when RAM is under 1 GB and there is no swap.
  - `FNode.sh`: Comprehensive management menu (`FNode` CLI command) supporting service control, log inspection, log cleanup, config generation, BBR installation, Caddy installation, and Cloudflare reverse proxy configuration. Includes an enhanced update function (Menu option `2` or `FNode update [latest|pre|<version>]`) providing interactive selection between latest stable releases, rolling pre-releases, and custom pinned versions (options `1`, `2`, `3`). Uses `FNODE_BRANCH` (defaults to `master`) for dynamic script self-upgrades and installer downloads. `FNode install <flags>` forwards flags to `install.sh`, `FNode uninstall --yes` skips the confirmation, `FNode config show [--node] [--summary]` runs the binary's effective-config viewer, and `FNode status` ends with `FNode config status` (panel, masked token, node tags).
  - `initconfig.sh`: Interactive CLI wizard for creating `/etc/FNode/config.json`. Defaults `DisableIPv6: true` across core and node configurations to eliminate outbound AAAA DNS lookup delays, routing stalls, and transient socket reset errors, while automatically binding inbounds to `[::]` on dual-stack hosts.

### 2.5 Caddy Integration & Reverse Proxy
- Caddy serves as a front-facing reverse proxy and automated SSL certificate manager.
- Supports Cloudflare DNS-01 ACME challenges (`tls { dns cloudflare {env.CLOUDFLARE_API_TOKEN} }`).
- Stores generated certificates in `/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/<domain>/`.
- FNode seamlessly ingests Caddy certificates using `CertMode: "file"`.

### 2.6 End-to-End Communication Flow Logic
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
    "Level": "info",       // debug, info (default), warn, error
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
      "TCPKeepAlive": 30,            // seconds idle before the first TCP keepalive probe on every TCP inbound (0 = 30, -1 = off)
      "TCPKeepAliveInterval": 15,    // seconds between probes (0 = 15)
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
- **Default origin** (`core/sing/default_origin.json`, embedded as `sing.DefaultOrigin`): outbounds `direct` (`prefer_ipv4`), `direct6` (`prefer_ipv6`) and `block`, `route.auto_detect_interface: true`, and the rules private IPs → block, `bittorrent` → block, TCP 25/465/587 → block, the abuse `domain_regex` list → block, then tcp/udp → direct. `install.sh`, `initconfig.sh` and `FNode.sh` write this exact file. FNode uses it when `OriginalPath` is empty, the file is missing (warning) or the file is invalid (error, until fixed), so a broken origin never stops the core and takes the node offline.
- **Custom Outbounds**: Custom protocol outbounds (`shadowsocks`, `vless`, `vmess`, `trojan`, `hysteria2`, `hysteria`, `tuic`, `anytls`, `ssh`, `socks`, `http`, `shadowtls`, `selector`, `urltest`, etc.) declared in `sing_origin.json` or `CustomOutbounds` are loaded into the core. Complete templates for all protocols are provided in `example/custom_outbound.json`, while `example/sing_origin.json` retains the default configuration generated by FNode install scripts. AnyTLS traffic obfuscation padding schemes can be generated using [`example/anytls_padding_generator.py`](../example/anytls_padding_generator.py).
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
- **Rule Precedence Chain** (first match wins; same order as xboard-node). Rules 2, 3 and 6 come from each node's panel config and get `inbound: [<node tag>]`, so on a machine running several nodes a node's routes only apply to its own users (rules with their own `inbound`, and `logical` rules, are kept as sent):
  1. Local `CustomRouteRules` from `config.json`
  2. Panel `custom_route_rules` (structured rules, when the panel sends them)
  3. Panel `custom_routes` (raw sing-box rules; `geosite`/`geoip`/`source_geoip` fields are dropped, `geoip: private` becomes `ip_is_private`)
  4. Anti-SSRF Protection (private IPv4 & IPv6 CIDRs blocked)
  5. IPv6 Blocking (when `DisableIPv6: true`)
  6. Panel `routes` from the node's route groups (`block`, `direct`, `proxy`/`route` → `action_value` outbound tag; `dns` routes are not router rules), sorted by route id (the dashboard order: CustomNode's move up/down swaps route ids), since the panel sends them in database row order. `custom_route_rules` and `custom_routes` keep their array order.
  7. Base `sing_origin.json` route rules (custom outbound detours, domain suffixes, rule-sets, regex, protocol blocks)
  8. Fallback final outbound (`direct`)

  A panel rule that targets an outbound tag that does not exist is dropped with a warning instead of failing at connection time with `router: outbound not found`, and the tag is reported to the panel (§4.9) so the CustomNode plugin can add that outbound to the node. Rules to `direct`/`block` are never dropped. Panel route items `geosite:*`/`geoip:*` are skipped with a warning (sing-box 1.12+ removed those databases; `geoip:private` becomes `ip_is_private`). Rules are decoded one by one and, when the router rejects one (`parse rule[i]: ...`), that rule is dropped and the rest applied, so a single bad panel rule no longer leaves the previous routing running.

This eliminates the need to bloat `config.json` with duplicated rules, allowing complex routing and remote rule sets to be maintained cleanly in `sing_origin.json`.

### 4.3 Automatic Hot-Reload & Related Files Monitoring
FNode includes an intelligent filesystem watcher ([`conf/watch.go`](file:///c:/Users/Keanghour/Documents/GitHub/FNode/FNode/conf/watch.go)) active by default (`-w` flag on `FNode server`):
- **Atomic Save Resilience**: Watches parent directories (`filepath.Dir(absPath)`) so inotify watches survive inode replacements caused by `vim`, `nano`, `sed -i`, and file move operations.
- **Related Assets Tracking**: In addition to `/etc/FNode/config.json`, the watcher automatically discovers and monitors `OriginalPath` (`/etc/FNode/sing_origin.json`), `CertFile`, and `KeyFile`. Any modification triggers an automated reload.
- **Safe Validation**: Validates the syntax of the new configuration and existence of declared files before applying; invalid JSON syntax logs an error and safely preserves the currently running configuration without crashing or interrupting active proxies.
- **Timer Debounce**: A 1-second debounce consolidates burst filesystem notifications into a single seamless reload cycle.

### 4.4 Dynamic VPS IP & Network Interface Auto-Detection
To ensure high availability on cloud VPS providers with dynamic or reassigned IP addresses (e.g., Lightsail, AWS, GCP, DHCP lease renewals):
- **Interface & Gateway Monitoring**: `AutoDetectInterface` defaults to `true` across `SingConfig` and sing-box route options. sing-box actively tracks kernel link and default gateway transitions via `InterfaceMonitor` (rtnetlink on Linux), ensuring outbound `direct` dials and DNS resolvers adapt immediately without entering connection timeout loops.
- **Wildcard Socket Binding**: Nodes bind inbounds to `0.0.0.0` (or `::` on dual-stack) and outbounds use `SendIP: "0.0.0.0"`, avoiding hardcoded IP addresses.

### 4.5 Hysteria 2 Transport & Multi-Port Architecture
To maximize throughput and resist ISP UDP QoS throttling without requiring subscription resets or panel variable churn:
- **UDP socket (`Hysteria2Config.ReuseAddr`, default off)**: the inbound's UDP socket is closed (`Inbound.Remove`) before a rebuilt inbound binds. `SO_REUSEADDR` used to be forced on; with it an old socket that lingers after a rebuild can keep receiving datagrams for the port (clients then time out) instead of the new bind failing loudly, and FNode is a single process, so it is opt-in now.
- **Idle behaviour is fixed by the library**: sing-quic's server and client both use `MaxIdleTimeout 30s` and `KeepAlivePeriod 10s` and sing-box does not expose them, so there is nothing to tune in FNode. What FNode controls is not dropping the QUIC connections by rebuilding the inbound (see 4.6.1).
- **Congestion control and "my provider flags UDP floods"** (`core/sing/hysteria2_cc.go`): sing-quic chooses per client. If the client's auth request declares a receive rate (Rx > 0) the server sends with **Brutal** (fixed rate, ignores loss) at `min(Rx, UpMbps)`, and with no limit at all when `UpMbps` is 0; otherwise (or with `IgnoreClientBandwidth` and `DownMbps` 0) it uses **BBR**. Xboard's node config carries only `up_mbps`/`down_mbps` (0 when the dashboard fields are empty) and never `ignore_client_bandwidth`, so an unlimited node plus a client declaring a rate far above its real link (say 1000 Mbps on a 20 Mbps or lossy path) made the VPS send UDP at that fixed rate: upload well above what arrives, heavy loss and resends, which looks like an attack to a provider. FNode now:
  - caps the Brutal rate at `Hysteria2Config.MaxBrutalMbps` (default **100**) when the panel's `up_mbps` is 0, and logs `Hysteria2 Brutal rate limited: ...` at info when the inbound is created (sing-quic gives no per-client hook, so the capped clients themselves are not logged; `BrutalDebug` logs ack rate and loss). `0` or `-1` = no cap; a positive value also lowers a larger panel `up_mbps`; a panel `up_mbps` > 0 is otherwise kept;
  - offers `Hysteria2Config.IgnoreClientBandwidth: true` (local config) to use BBR for every client. FNode then passes `DownMbps` 0, which BBR-for-all requires (with `DownMbps` > 0 a client that declares no rate is rejected as masquerade); the panel's own `ignore_client_bandwidth`, if a panel sends one, is passed through unchanged;
  - the real fix is also available without FNode: set realistic Up/Down in the dashboard node form, which lowers the Brutal cap per node.
  - Tradeoff: Brutal holds its rate through loss, which is why it can be faster on a lossy link, and why it floods when the declared rate is wrong; BBR backs off on loss, so it is gentler and usually slower on a link with random loss (mobile, long-haul).
- **Flexible Masquerade Fallback**: Supports `file://`, `http://`/`https://` reverse proxies, and string/HTML content fallback.
- **UDP Fragmentation & Timeouts**: Configurable via node config or local `Hysteria2Config` (`UDPFragment`, `UDPTimeout`, `BrutalDebug`).
- **Multi-Port Port Hopping**:
  - **Xboard Separation**: Xboard manages `server_port` (backend daemon port, e.g. `443`) and `port` (client port or hopping range, e.g. `10000-20000`).
  - **Linux Host Forwarding**: PREROUTING NAT redirection routes the client range to FNode's daemon port:
    ```bash
    iptables -t nat -A PREROUTING -p udp --dport 10000:20000 -j REDIRECT --to-ports 443
    ```
  - **Automation Script**: Managed via `FNode` maintenance CLI menu option `19` or `FNode porthop` (`FNode-script/FNode.sh`).

### 4.6 Panel Outbounds, Routes & Real-Time WebSocket Sync
Route changes made in the Xboard dashboard reach every FNode server without per-server edits:
- **Panel outbounds** (`core/sing/outbound.go`, `core/sing/apply.go`): the UniProxy config's `custom_outbounds` (`{tag, protocol, settings, proxy_tag}`, as written by the CustomNode plugin; a raw sing-box outbound is accepted too) are converted to sing-box outbounds (`{type: protocol, tag, ...settings}`, `proxy_tag` → `detour`). `custom_routes` (raw sing-box rules), `custom_route_rules` and the node's `routes` are kept on `NodeInfo`.
- **Merge order** (in memory only): `sing_origin.json` outbounds → local `CustomOutbounds` → panel `custom_outbounds` of every node (sorted by node tag). A later source wins on a duplicate tag and a warning is logged. Builtin `direct`/`block` are added only when `sing_origin.json` has no outbounds.
- **Applying**: every `AddNode` and routing change reconciles the outbound set through sing-box's `OutboundManager` using a per-tag hash: new or changed outbounds are created (dependencies first), outbounds that detour through a changed one are recreated too, and outbounds no longer wanted are removed. The router rules are then replaced. A config change that only touches `routes`/`custom_outbounds`/`custom_routes`/`custom_route_rules` (same inbound hash) is applied in place without recreating the inbound or re-adding users. One line is logged per apply: `applied N outbounds (origin a, local b, panel c, builtin d), M rules`.
- **Never written**: FNode never writes `sing_origin.json` or `config.json`. Both are watched (§4.3), so writing them would cause a reload loop and mix generated data into hand-written files.
- **WebSocket push** (`api/panel/ws.go`, `node/ws.go`): on start each node calls `POST /api/v2/server/handshake`. When `websocket.enabled` is true and `ws_url` starts with `ws://`/`wss://`, the node connects to `ws_url?token=…&node_id=…` (one connection per node, state kept per controller), answers `ping` with `pong`, and reconnects with exponential backoff (1s → 60s, with jitter). Events: `sync.config` (same shape as the HTTP config), `sync.users`, `sync.user.delta` (`add`/`remove` by id) and `sync.devices` (uid → IPs, used as the alive list). While the node's WS is connected the HTTP poll still runs every pull interval: `GET /config` and `GET /user`, both ETag-cached (a 304 when unchanged), so changes the panel did not push (hidden nodes, a missed Redis event, a missed user delta) still arrive. `GET /user` is also Xboard's heartbeat: it is the call that refreshes the node's `SERVER_<TYPE>_LAST_CHECK_AT` cache (300s TTL), so skipping it made WS-connected nodes show "Not Running" after 5 minutes and trigger a false offline alert after 30. Devices (alive list) come only from the WS; when it drops, `GET /alivelist` resumes. A reload that fails half way (`failReload`) sets `rebuild`, which forces a full config fetch and rebuild on the next check instead of a panic. A malformed `ws_url` is logged as an error and the node keeps polling. Traffic and alive reports stay on HTTP.
- **Generated files** (`core/sing/effective.go`): after every successful apply FNode atomically writes, per node, `<GeneratedDir>/<node>.effective.json` (the merged sing-box config: inbound without users, outbounds, route rules and rule sets, plus an `_fnode` block listing each outbound's source) and `<node>.panel.json` (the raw panel `routes`, `custom_outbounds`, `custom_routes`, `custom_route_rules`). `GeneratedDir` (in the sing core config) defaults to `generated/` next to `config.json` and is not watched. `<node>` is the node tag with unsafe characters replaced by `_`. Files of removed nodes are deleted. The files are mode 0600 and may contain outbound passwords and keys.
- **CLI**: `FNode config show [-c config.json] [--node <tag|file|part of tag>] [--summary] [--dir <dir>]` prints a per-node summary (outbound tag, type, source, contributing node) followed by the effective JSON.
- **CLI**: `FNode config status [-c config.json] [--dir <dir>]` (`cmd/config_status.go`) prints, per `Nodes` entry, `Panel:` (ApiHost and `machine N, from dashboard` or `node N`), `Token:` (first and last 5 characters; tokens of 10 characters or fewer show only the first) and `Node:` (the running tags read from the effective files; machine-assigned nodes end in `--Panel`, a node entry's tag is shown alone). Node entries claim their tags before a machine entry on the same panel.

### 4.6.0 Multiplex (smux) from the Panel

Xboard's node form (VLESS, VMess, Trojan) has a multiplex block that the subscription also hands to clients: `multiplex.enabled`, `.padding`, `.brutal.{enabled,up_mbps,down_mbps}` and the client-only `.protocol`, `.max_connections`, `.min_streams`, `.max_streams`. A client that turns multiplex on talks smux to the server, so the server inbound must have multiplex on too, or the connection fails or hangs. FNode used to read multiplex only from the local `config.json` (`SingOptions.Multiplex`), so a dashboard setting did nothing. Now (`api/panel/multiplex.go`, `core/sing/multiplex.go`):
- The panel's `multiplex` is parsed leniently (booleans and numbers as `0/1`, strings or numbers; `null`, `[]`, `{}` and non-objects mean "the panel did not decide") into `CommonNode.Multiplex`.
- **The panel wins when it sends an `enabled` value** (true or false); padding and Brutal then come from the panel exactly (padding defaults to off when the panel omits it). When the panel sends none, the local `SingOptions.Multiplex` is the fallback, as before. `protocol`, `max_connections`, `min_streams` and `max_streams` are parsed but unused (client options).
- It is applied to vless, vmess, trojan and shadowsocks inbounds, with or without Reality and with any flow (the server wraps the inbound the same way for XTLS Vision).
- `multiplex` is inbound config (not a routing key), so a change in the dashboard rebuilds the inbound.
- One info line per node build: `Multiplex is on` (`source` panel or local config, `padding`, `brutal`) or `Multiplex is off`.
- Brutal needs up and down of at least 1 Mbps (otherwise sing-box refuses to create the inbound and the node would not start): without them FNode runs multiplex without Brutal and warns. With Brutal on it warns that TCP Brutal needs the `brutal` kernel module on the server (`lsmod | grep brutal`).

### 4.6.2 Network Recovery (public IP changed, outbound lookups fail)

Symptom: after the provider changes or re-points the public IP, clients still connect but every outbound lookup fails at once with `lookup <host>: read udp 10.x.x.x:...->1.1.1.1:53: use of closed network connection` (later `file already closed`), after waiting up to ~30 s. sing-box keeps one UDP socket per DNS server; behind the new NAT mapping its replies never arrive, nothing resets the transport, and sing-box's interface monitor only reacts to a default-interface change, not to a changed address on the same interface. `Router.ResetNetwork()` closes those sockets and clears the DNS cache, and the next lookup dials a new socket. FNode now calls it (`core/sing/netrecovery.go`, default on, `Cores[].NetworkRecovery`: `"reset"` default, `"restart"`, `"off"`):
- **Address watcher**: every 10 s the interface name and local IPv4 address of the default route are read (a UDP dial that sends no packet; no third party is contacted). A change is confirmed once 3 s later, logged at WARN (`Network address changed ... old/new`) and followed by a reset. IPv6 is left out because privacy addresses rotate daily.
- **DNS failure watchdog** (the only signal when the public IP changed behind a NAT and the local address did not, as on most clouds): the sing-box log lines of failed lookups (`lookup ...: use of closed network connection`, `file already closed`, `i/o timeout`, `context deadline exceeded`, `network is unreachable`; "no such host" is not counted) are counted; 20 within 10 s reset the network once (WARN), with a 30 s cooldown. It needs the core `Log.Level` to be `error` or more verbose (the default); with `panic`/`fatal` no lines arrive.
- **If resets do not help** (failures again within 2 minutes of the second reset) FNode stops resetting and logs one ERROR (at most every 5 minutes) pointing at the DNS server in `sing_origin.json` (`dns.servers`), UDP 53 egress and the VPS network settings. With `"restart"` it also restarts the core through the config-reload path (`cmd/server.go` `reload`, serialised with file-change reloads), the first time after 5 minutes since the last restart, then 10, 20, 40 and at most 60 minutes while the network stays broken; 10 quiet minutes start over. It cannot loop: an hour of non-stop failures is at most two resets and a handful of restarts.
- `"off"` disables both. Manual workaround at any time: `systemctl restart FNode`.
- Limits: this is recovery built from the symptom's logs; FNode cannot tell that the public IP changed, only that lookups started to fail, and a reset cannot fix a DNS server that is really unreachable.

### 4.6.1 Inbound Stability & TCP Keepalive (idle clients that stop working)

Symptom: after a long idle time the next use of an AnyTLS or Hysteria2 proxy fails until the client reconnects.

What FNode does about it:
- **TCP keepalive on every TCP inbound** (`core/sing/keepalive.go`): the server probes an idle connection after `Options.TCPKeepAlive` seconds (default 30) and then every `Options.TCPKeepAliveInterval` (default 15); `TCPKeepAlive: -1` turns it off. sing-box's own defaults are 300 and 75 seconds, far slower than the 30-120 s after which a home router, CGNAT or firewall forgets an idle mapping. A mapping that is kept alive and a dead peer that is found sooner are all this can do. It does not apply to UDP (Hysteria2, TUIC).
- **No needless inbound rebuilds** (a rebuild drops every live connection and the client only notices after its own idle timeout): the panel's HTTP `/config` adds `base_config` (the push/pull intervals) while the full `sync.config` it pushes on every WebSocket connect does not, so the same config used to look changed and the inbound was recreated right after start-up (the double "tcp server started" in the journal) and again after a panel-side change. `base_config` is now excluded from `InboundHash` (`notInboundKeys` in `api/panel/node.go`), intervals are applied without a rebuild (`Controller.applyIntervals`), a pushed config without intervals keeps the known ones, and a config whose inbound and routing hashes are unchanged is a no-op. Routing-only changes never rebuild the inbound; user changes are applied in place for every protocol including AnyTLS (the sing-box fork's inbound implements `UpdateUsers`).
- **Every rebuild is logged at info** with the reason: `Node changed, recreating its inbound (live connections are dropped)` with `reason` (`inbound config changed`, `the previous reload failed`) and the old and new hash, `Inbound created` for a new inbound, and `Recreating inbound (live connections are dropped)` for the (unlikely) in-place user update failure. A pending certificate (`cert_config` file missing) keeps the running inbound and re-fetches the config; it rebuilds once, when the file appears.
- `recreateInbound` no longer takes the users lock it already holds.

What FNode cannot fix: the sing-anytls client keeps idle sessions in a pool and reuses one without checking it (the library has no active keepalive), so a session dropped by a middlebox only fails on first use. On the client set a short `idle_session_check_interval`/`idle_session_timeout` (about 30 s) and `min_idle_session: 0` so stale sessions are not kept. Hysteria2's QUIC idle timeout (30 s) and keepalive (10 s) are fixed in the library on both sides.

### 4.7 Machine Mode (Per-Server Token)
An Xboard *machine* groups nodes under one token (`MachineController`, `ServerV2` middleware). A `Nodes` entry whose `ApiConfig` has `MachineID > 0` runs in machine mode (`node/machine.go`):
- **Config**: `ApiHost`, `MachineID`, and the machine token in `MachineToken` (falls back to `ApiKey`). `NodeID`/`NodeType` are ignored; `Options` is the template for every node of the machine (`Name` gets `-<node id>` appended).
- **Discovery**: `POST /api/v2/server/machine/nodes` (`machine_id` + `token`) returns `[{id, type, name}]` and `base_config` intervals (pull < 30s and push < 10s fall back to 60s). One `Controller` per supported node uses `/api/v2/server/{config,user,push,alive,alivelist}` with `machine_id`, `token` and `node_id`; the node type is read from the config's `protocol` (`hysteria` + `version: 2` → `hysteria2`). Unsupported types are skipped with one warning. A node that fails to start is retried on the next discovery.
- **Node set changes**: the machine WebSocket (`ws_url?token=..&machine_id=..`) multiplexes all nodes; node events carry `node_id` and are routed to that controller, `sync.nodes` starts/stops controllers. The node list is also re-polled every pull interval.
- **Status**: every push interval `POST /api/v2/server/machine/status` with `cpu`, `mem{total,used}`, `swap`, `disk`, `net{in_speed,out_speed}` (sampled from `/proc` and `statfs` by `common/sysinfo`) plus `"client":"fnode","version":"<ver>"`.
- **Server token without `NodeType`**: a plain node entry (`ApiKey` + `NodeID`) without `NodeType` also uses the v2 endpoints and detects the type. With `NodeType` set, the v1 UniProxy behaviour is unchanged, except that when the v1 config request fails at start (e.g. `NodeType` does not match the panel node) FNode retries through the v2 API with the panel's type and logs a warning.
- **Local entries vs dashboard machine**: when a standalone entry and a machine entry in `config.json` name the same panel node (same `ApiHost` and node id), the standalone entry runs it and the machine skips it (one warning), so the two never fight over the same inbound tag and port. A node listed twice as standalone entries runs once.

### 4.7.1 Start-up Resilience
`node.Node.Start` never gives up on an entry: a machine or node that fails to start (panel unreachable at boot, `the panel has no users for this node yet`, a certificate error...) is logged and retried in the background (10s doubling to 2 min) until it starts or FNode stops; the other entries start normally. A reload that fails after the old inbound was removed no longer panics the process: the node is marked for rebuild and the next config check fetches the full config (`ResetConfigCache`) and recreates it. The machine's log shipper starts before the first panel request, so start-up errors reach the dashboard once the panel is reachable.

### 4.8 REALITY Mapping, Probe Logging & Dependency Tracking
- **Panel mapping** (`core/sing/node.go`, `core/sing/reality.go`): Xboard sends `tls: 2` and `reality_settings` as `tls_settings` (`server_name`, `server_port` as a string, `private_key`, `short_id` as one string or null, `public_key` for clients only) plus the top-level `flow`. FNode builds `tls.server_name`, a handshake destination `server_name:server_port` (or `dest[:port]`, default 443), the base64url `private_key` (standard base64 and `=` padding are normalised), and `short_id` as a list (comma-separated allowed; empty or null means the empty id, which sing-box accepts when the list is empty). Short ids that are odd-length, non-hex or longer than 8 bytes fail the node with a message naming `short_id`. sing-box supports one server name per inbound; `max_time_difference` is not sent by Xboard and stays unset.
- **Tests**: `core/sing/reality_test.go` starts a vless+reality server from a panel-style config, a local TLS 1.3 camouflage site, and a sing-box vless+reality client (needs `-tags with_utls`, the test skips otherwise): `xtls-rprx-vision` and plain, with a short id, a list, a short one and an empty one all connect; a client with a wrong public key is rejected and forwarded to the destination.
- **Rejected connections** (`core/sing/logcapture.go`, `core/sing/rejectlog.go`): sing-box writes its log to a pipe that FNode forwards to the configured `Log.Output` and `Log.Level`. `TLS handshake: REALITY: processed invalid connection` is the only thing sing-box says about a client that fails REALITY (wrong public key, short id, server name (SNI) or clock, TLS below 1.3, or a scanner); the utls library explains why only at **trace** level (`Log.Level: trace` prints `hs.c.ClientShortId`, `forwarded SNI`, ...), so FNode cannot name the exact reason below that. The raw line stays at debug; FNode counts it per inbound and logs **one WARN per inbound and reason per minute** (the first reject at once, then the count of the rest with the top source addresses): `Reality rejected N connection(s) on inbound/vless[<tag>] from <ip> xN: no valid Reality handshake ...`. The WARN carries the `tag` field, so it lands in that node's panel log at warn/info. Two more reasons get the same treatment: `flow mismatch: expected X, but got Y` (VLESS client flow differs from the node's; sing-box does not say which user, only the source address) and `REALITY: failed to dial dest: ...` (the handshake destination is unreachable from this server, so every client of the node fails). A busy public node that scanners keep probing therefore logs one WARN a minute.
- **Inbound start-up lines** (`core/sing/inboundlog.go`): every inbound logs one info line `Inbound created` with `node_id`, `tag`, `type`, `listen` (ip:port), `multiplex` and, for REALITY, `reality`, `server_name`, `dest` (host:port), `short_ids` (a count) and `flow`; keys, short ids and users are never printed. An inbound that cannot be built (bad private key, short id, missing dest) logs `Inbound cannot be built: ...` and one that cannot listen logs `Inbound failed to start: ...` (with a hint for a port already in use or a permission error), both at ERROR with `node_id`, `tag`, `type`, `port`. 10 minutes after an inbound is created without a single connection reaching the router (`HookServer.Accepted`), FNode warns once: `No client has connected to vless:<port> in 10m - check the firewall / cloud security list for this port, the DNS name of the node host, and the client's config` (plus `(this node has 0 users)` when it has none). Connections that fail the REALITY handshake do not count as connected.
- **Dependency**: REALITY comes from `github.com/xtls/reality` via the sing-box fork. `.github/dependabot.yml` opens a separate weekly PR for it; newer releases may require a newer Go toolchain than the `go` line in `go.mod`.

### 4.9 Log Shipping to the Panel
`common/logship` ships FNode and sing-box logs to the CustomNode plugin (`POST /api/v2/custom-node/logs`, `api/panel/logs.go`). A `Shipper` (one per machine, or one per standalone node) owns a 2000-entry ring buffer, filters by the panel's `log_mode` (default `info` until the panel answers), and flushes every `flush_interval` seconds (response field, default 10), at 200 entries, in batches of at most 500 with messages capped at 4096 bytes. Each push also carries `client: "fnode"`, `version`, `dropped` (entries lost to the full buffer since the last push) and `missing_outbounds` (`{"<node id>": ["tag", ...]}`, outbound tags the node's routes use but FNode does not have; a change is pushed at once). The response may carry `node_modes` (`{"<node id>": "debug"}`, the per-node modes chosen inside a machine), which replaces the per-node overrides. A failed push keeps the entries, logs one local warning (`logship` field, never shipped) and backs off (5s doubling to 2 min); HTTP 404 backs off 10 minutes; mode `off` clears the buffer and only polls with `entries: []` every 60s. **Standby** (CustomNode 1.13): the push response may carry `ship` (bool, absent = true) and `retain_seconds` (default 300), and `custom_node.log_mode` events may carry the same fields (absent = unchanged; per-node events carry no `ship`). With `ship: false` the shipper keeps collecting at the real `log_mode` level, trims the ring to entries newer than `retain_seconds` and at most 5000, sends no entries, but still sends an empty poll every `flush_interval` so the panel's connection status stays current and a change of `ship` is noticed over plain HTTP. When `ship` turns true (response or WS event) the flusher wakes and sends the backlog at once, oldest first, in batches of 500, keeping each entry's original `time`. `ship` never changes the mode: `off` stays the user's explicit off (and drops the ring); lowering the mode filters the ring as before. `custom_node.log_mode` WS events set the machine-wide mode (`node_id` null, which also clears per-node overrides) or one node's mode. The `Hub` routes entries from two sources: a logrus hook (`InstallLogrus` in `cmd/server.go`, which also wraps the formatter so the console keeps the configured level while the logger level is raised for the panel) and the sing-box log capture (`core/sing/logcapture.go`), attributing entries to nodes through the controller tag (`inbound/vless[<tag>]`). Shipper diagnostics carry the `logship` field and are never re-shipped.

---

## 5. Certificate Management & Caddy Integration

### 5.1 Supported Certificate Modes
| Mode | Description | Automated Renewal |
|------|-------------|-------------------|
| `file` | Reads existing `.crt` and `.key` files from filesystem (e.g. Caddy). If `CertFile`/`KeyFile` are omitted or not found, FNode automatically locates Caddy's certificates for `CertDomain`. | Managed externally (by Caddy) |
| `http` | Automatically requests Let's Encrypt cert via port 80 HTTP-01 challenge using Lego. | Automated by FNode Lego task |
| `dns` | Automatically requests Let's Encrypt cert via DNS-01 challenge using DNS provider API (e.g. Cloudflare). The default build links 22 providers (`alidns bunny cloudflare desec digitalocean dnspod duckdns dynu gandi gandiv5 godaddy hetzner hurricane ionos namecheap namesilo netcup ovh porkbun rfc2136 tencentcloud vultr`, `node/lego_dns_lite.go`); any other lego provider needs a build with the `with_all_dns` tag (`node/lego_dns_all.go`). A missing provider is reported with that hint; `file` mode with Caddy needs none of this. | Automated by FNode Lego task |
| `self` | Generates a local self-signed RSA-2048 certificate. | Valid for 30 years |
| `none` | Disables TLS configuration (e.g. for plain Shadowsocks or VLESS-Reality). | N/A |

### 5.1.1 Dashboard cert_config (per node)

Xboard sends a per-node `cert_config` in the node config (HTTP `/config` and WebSocket `sync.config`), xboard-node shape: `{cert_mode, domain, cert_file, key_file, email, dns_provider, dns_env, http_port, cert_content, key_content}`; older data uses `mode` for `cert_mode`. `api/panel/cert.go` parses it leniently (PHP `[]`, numbers as strings, a non-object is ignored) into `CommonNode.CertConfig`; `NodeInfo.PanelCert()` returns it when the mode is not `none`.

When present it replaces the local `Options.CertConfig` for that TLS node (`node/panel_cert.go`), so TLS nodes need no local `CertConfig`:
- Mapping: `cert_mode`→`CertMode`, `domain`→`CertDomain` (falls back to the node's `server_name`), `cert_file`/`key_file`, `email` (falls back to the local `Email`), `dns_provider`→`Provider`, `dns_env`→`DNSEnv`, `http_port`→`HTTPPort` (HTTP-01 port, default 80). `RejectUnknownSni` stays local.
- `content` mode writes `cert_content`/`key_content` to `/etc/FNode/panel_cert/node-<id>.crt|.key` (rewritten only when changed) and runs as `file`. `self`/`http`/`dns` without paths use the same `node-<id>` files. `file` keeps the Caddy auto-detect fallback.
- The local `CertConfig` is kept in `Controller.localCert`; the effective one is set on `Options.CertConfig` only after the cert is ready (the core reads TLS paths from there). Without a panel `cert_config` the local behaviour is unchanged.
- A missing cert/key is logged at error level (so it ships to the dashboard log card) as `node <id>: dashboard cert_config (<mode> mode): cert file not found: <path>`. The running inbound is kept; on a config update the client forgets the config ETag/hash (`Client.ResetConfigCache`) and the next check (which runs with or without the WebSocket) re-fetches it, so the node rebuilds once the file appears. A machine node whose first start fails is retried on every machine sync.
- `cert_config` is not a routing key, so a change changes `InboundHash` and rebuilds the inbound.

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

### 5.4 Device Limiting, Client IP Migration & Speed Limit Lifecycle

1. **Client IP Handover & Network Migration (`WiFi ↔ Cellular`)**:
   - In standard deployments, mobile clients frequently switch network interfaces (e.g., leaving home WiFi for 4G/5G, or dynamic IP reallocation).
   - FNode tracks each active IP with an individual `LastSeen` timestamp. When a client changes IP, FNode detects that the new IP originates from the same node and migrates the session by pruning the older inactive IP rather than locking out the client with a false device limit rejection.
   - Distinct active devices across other cluster nodes are properly evaluated against `deviceLimit` without false positives.

2. **Immediate Offline Device Synchronization**:
   - When users disconnect or their active IP times out (>60s inactivity), FNode explicitly reports `{ "UID": [] }` to Xboard's `/api/v1/server/UniProxy/alive` endpoint.
   - This triggers Xboard's `DeviceStateService::removeNodeDevices()` immediately, eliminating 5-minute Redis TTL ghost device deadlocks.

3. **Dynamic Speed Limit Expiration Safety**:
   - When dynamic speed limits expire, FNode resets `DynamicSpeedLimit = 0` and `ExpireTime = 0`, restoring normal plan speed (`SpeedLimit`) without deleting `UserLimitInfo`. This permanently prevents the "limited by ip or conn until restart" failure mode.

4. **Resilient Node Reloading (`nodeInfoMonitor`)**:
   - When node configuration changes in Xboard, TLS certificates are verified/renewed *before* deleting the running inbound. If cert validation fails, the active inbound continues proxying traffic safely.
   - Limiter configurations are updated seamlessly across reloads regardless of whether custom node `Name` overrides are used.
   - Periodic user traffic and online device reporting strictly adheres to `PushInterval`.

### 5.5 Automated Log Management & 7-Day Cleanup Cycle

To prevent VPS disk saturation on nodes operating continuously over months or years, FNode incorporates automated multi-tiered log rotation and vacuuming every 7 days:

1. **System Cron Task (`/etc/cron.d/FNode-cleanlog`)**:
   - On installation (`install.sh`), a scheduled cron entry is automatically registered at `/etc/cron.d/FNode-cleanlog` (or `/etc/periodic/weekly/fnode-cleanlog` on Alpine Linux):
     ```cron
     0 3 */7 * * root /usr/bin/FNode clearlog >/dev/null 2>&1
     ```
   - Automatically cleans systemd journal logs (`journalctl --vacuum-time=1s --unit=FNode.service` and `caddy.service`, size limited to 20MB).
   - Truncates custom log paths configured in `/etc/FNode/config.json` (`Log.Output`, `Cores[].Log.Output`) as well as standalone logs (`/var/log/FNode.log`, `/usr/local/FNode/box.log`, `/etc/FNode/box.log`, `/var/log/caddy/*.log`).
   - Prunes archived log rotation files older than 7 days (`find /var/log/ -name "*fnode*.log*" -mtime +7 -delete`).
   - Cleanly uninstalled when invoking `uninstall()` via `FNode.sh`.

2. **Internal Go Daemon Auto-Cleaner (`cmd/logcleaner.go`)**:
   - For environments running `FNode server` directly or in containerized systems where cron is inactive, `cmd/server.go` launches a background routine `startAutoLogCleaner(7*24*time.Hour, c.LogConfig.Output)`.
   - Fires every 7 days (`time.Hour * 24 * 7`) to execute `CleanLogs()`, truncating active file logs and vacuuming system journal logs without interrupting running inbounds or proxy services.

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

### 6.3.1 Binary Size & Small VPS (1 vCPU / under 1 GB RAM)

Measured on `linux/amd64`, tags `sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor`, `-s -w`:

| Build | Binary size |
|-------|-------------|
| all lego DNS providers (`with_all_dns`, the old default) | 87.5 MB |
| default (22 DNS providers) | 46.8 MB |
| DNS provider `cloudflare` only (experiment) | 41.7 MB |

Dropping `with_gvisor` saves about 4 MB, `with_wireguard` 0.6 MB, `with_acme` 1 MB and `with_grpc` 0.1 MB, so those tags stay. The 32 MB `crypto/internal/fips140/drbg.memory` symbol is zero-initialised memory (`.noptrbss`): no file bytes, and only touched pages count as RSS. UPX is not used: it needs the whole binary decompressed in RAM (no shared file-backed pages). Idle with no nodes the process had a working set of about 37 MB (Windows, measured with `Nodes: []`); with nodes the heap grows with users and traffic, which is what `GOMEMLIMIT` bounds.

The release zip no longer contains `geoip.dat`/`geosite.dat` (28 MB uncompressed, read by nothing since sing-box 1.12). To keep every lego DNS provider, build with `-tags "... with_all_dns"`.

### 6.4 Automated CI/CD & Version Tagging Strategy

FNode uses GitHub Actions (`.github/workflows/release.yml`) for automated multi-platform builds and releases:
- **Continuous Commit Releases**:
  - Each new commit build automatically fetches all existing tags (`git fetch --tags --force`) and increments the patch version (+1, e.g. `0.0.13` -> `0.0.14` -> `0.0.15`).
  - Automatically checks and verifies against existing git tags to prevent tag collisions, ensuring each commit creates a brand new GitHub Release instead of overwriting/replacing existing release assets.
- **`master` / `main` / `stable` Branches**:
  - Increments the stable patch version and publishes a full GitHub Release marked as **Latest** (`make_latest: true`, `prerelease: false`).
  - **Pre-Release Auto-Deletion**: Automatically detects, deletes, and cleans up obsolete pre-releases (e.g. `<version>-pre`) and remote git tags when an official version comes out, ensuring the GitHub releases list remains pristine.
- **`dev` / `dev_new` Branches**:
  - Automatically formats the tag in rolling pre-release form: `<upgraded_version>-pre` (e.g. `0.0.22-pre`).
  - Publishes and continuously updates the GitHub Release marked as **Pre-release** (`prerelease: true`, `make_latest: false`), updating the git tag and overwriting release binaries on each push to `dev` without cluttering the releases page with multiple versions.
- **Draft Releases Auto-Cleanup**:
  - The release workflow automatically sweeps and deletes all orphaned or stale draft releases before publishing new release artifacts.
  - Dedicated on-demand cleanup workflow [`.github/workflows/clean-drafts.yml`](../.github/workflows/clean-drafts.yml) provides 1-click bulk deletion of all draft releases via GitHub Actions `workflow_dispatch`.
  - Local CLI tool [`FNode-script/delete_drafts.py`](../FNode-script/delete_drafts.py) allows repository administrators to purge draft releases directly via GitHub REST API.
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
