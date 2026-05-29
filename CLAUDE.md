# FNode — AI Agent Context

## What is FNode?

FNode is a **Go-based backend node server** (`github.com/tavut846/FNode`) that sits between a proxy panel (Xboard) and a proxy engine (sing-box). It polls Xboard for node configuration and user lists, then dynamically configures sing-box inbounds so that users can connect through the proxy node.

It is a fork of [V2bX](https://github.com/wyx2685/V2bX) (kept under `SupportProject/V2bX/` as a reference). FNode is **sing-box only** — the xray/hy2 core paths from V2bX are not active.

---

## System Architecture

```
Xboard (Panel / server manager)
    │  REST API  (/api/v1/server/UniProxy/*)
    ▼
FNode (this project)
    ├── api/panel/      ← Xboard API client
    ├── conf/           ← Config loader (JSON5, Include support)
    ├── core/sing/      ← sing-box wrapper (the only active core)
    ├── node/           ← Node controller — orchestrates everything
    ├── limiter/        ← Rate-limit / device-limit / IP tracking
    └── cmd/            ← CLI (cobra); entry: cmd/server.go
    │
    ▼
sing-box (proxy engine)
    └── handles actual inbound/outbound network traffic
```

**Data flow on startup:**
1. `cmd/server.go` loads config, starts `core/sing` (sing-box instance).
2. `node/node.go` creates one `Controller` per configured node.
3. Each `Controller` calls Xboard API → gets `NodeInfo` + `UserList`.
4. `NodeInfo` is passed to sing-box via `core.AddNode(tag, node, options)`.
5. Users are added to the inbound via `core.AddUsers(...)`.
6. Periodic tasks (`node/task.go`) keep polling for changes and reporting traffic.

---

## Relationship with Xboard

Xboard is the **server management panel** (a Laravel PHP app). FNode is its backend node agent.

### API endpoints FNode calls (all under `/api/v1/server/UniProxy/`)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/config` | Fetch node configuration (ETag-cached) |
| GET | `/user` | Fetch user list (ETag-cached, msgpack or JSON) |
| GET | `/alivelist` | Fetch alive-IP count per user |
| POST | `/push` | Report per-user upload/download traffic |
| POST | `/alive` | Report online users and their IPs |

### Auth
Every request carries `node_type`, `node_id`, and `token` as query parameters (set in `api/panel/panel.go:New()`).

### Config keys (`conf/node.go → ApiConfig`)
```json
{
  "ApiHost": "https://your-xboard-domain",
  "ApiKey":  "node-secret-token",
  "NodeID":  1,
  "NodeType": "vmess",   // or vless, trojan, shadowsocks, hysteria2, tuic, anytls
  "Timeout":  30
}
```

---

## Relationship with sing-box

sing-box is the **proxy engine**. FNode manages it programmatically — it never writes a static sing-box config file; instead it calls the sing-box Go API to add/remove inbounds and users at runtime.

### Dependency
```
github.com/sagernet/sing-box v1.13.0
  → replaced by github.com/cedar2025/sing-box (fork with extra features)
```

### Core interface (`core/interface.go`)
All cores implement `vCore.Core`:
```go
Start() error
Close() error
AddNode(tag string, info *panel.NodeInfo, config *conf.Options) error
DelNode(tag string) error
AddUsers(params *AddUsersParams) (int, error)
DelUsers(users []panel.UserInfo, tag string, info *panel.NodeInfo) error
GetUserTrafficSlice(tag string, reset bool) ([]panel.UserTraffic, error)
```

The only registered core is `sing` (`core/sing/sing.go`):
```go
func init() {
    vCore.RegisterCore("sing", New)
}
```

### How nodes are added to sing-box
`core/sing/node.go` translates `panel.NodeInfo` → `sing-box option.Inbound` and calls `box.Router().AddInbound(...)`.

---

## Project Directory Layout

```
FNode/                        ← repo root (working directory)
├── FNode/                    ← actual Go project (go.mod here)
│   ├── main.go
│   ├── go.mod                module: github.com/tavut846/FNode
│   ├── api/panel/            Xboard API client
│   │   ├── panel.go          Client struct, New()
│   │   ├── node.go           GetNodeInfo() → NodeInfo
│   │   └── user.go           GetUserList(), ReportUserTraffic(), etc.
│   ├── conf/
│   │   ├── conf.go           Top-level Conf{Log, Cores, Nodes}
│   │   ├── node.go           NodeConfig, ApiConfig, Options
│   │   └── sing.go           SingConfig, SingOptions
│   ├── core/
│   │   ├── interface.go      Core interface + RegisterCore
│   │   ├── selector.go       Core registry / factory
│   │   └── sing/             sing-box implementation
│   │       ├── sing.go       Sing struct, New(), Start(), Close()
│   │       ├── node.go       AddNode(), DelNode()
│   │       └── user.go       AddUsers(), DelUsers(), GetUserTrafficSlice()
│   ├── node/
│   │   ├── node.go           Node{} — starts all controllers
│   │   ├── controller.go     Controller — lifecycle per node
│   │   ├── task.go           Periodic tasks (nodeInfoMonitor, reportUserTrafficTask)
│   │   └── user.go           Traffic reporting, compareUserList
│   ├── limiter/
│   │   ├── limiter.go        Limiter — speed, device, IP limits
│   │   ├── rule.go           Domain / protocol audit rules
│   │   └── dynamic.go        Dynamic speed-limit logic
│   ├── cmd/
│   │   ├── cmd.go            Root cobra command
│   │   └── server.go         `server` subcommand — main runtime loop
│   ├── example/              Sample config files
│   └── SupportProject/V2bX/ Upstream reference (not compiled into FNode)
├── sing-box/                 sing-box source (reference)
├── Xboard-master/            Xboard source (reference)
└── graphify-out/             Generated knowledge-graph output
```

> The Go module lives at `FNode/FNode/`, not the repo root.

---

## Configuration File (`/etc/FNode/config.json`)

```json5
{
  "Log": { "Level": "info", "Output": "" },
  "Cores": [
    {
      "Type": "sing",
      "Log": { "Level": "error", "Timestamp": true },
      "OriginalPath": ""        // optional: base sing-box config to merge
    }
  ],
  "Nodes": [
    {
      "ApiConfig": {
        "ApiHost":  "https://panel.example.com",
        "ApiKey":   "secret",
        "NodeID":   1,
        "NodeType": "vmess"
      },
      "Options": {
        "Core":         "sing",
        "ListenIP":     "0.0.0.0",
        "SendIP":       "0.0.0.0",
        "EnableSniff":  true,
        "LimitConfig":  { "SpeedLimit": 0, "DeviceLimit": 0 },
        "CertConfig":   { "CertMode": "http", "Email": "admin@example.com" }
      }
    }
  ]
}
```

Config supports **JSON5** (comments, trailing commas) and `"Include": "path/to/file"` inside any node entry.

---

## Supported Protocols

`vmess`, `vless` (+ Reality, XTLS-Vision, XUDP), `trojan`, `shadowsocks`, `hysteria`, `hysteria2`, `tuic`, `anytls`

---

## Key Behaviors to Know

- **Single instance, multi-node**: One FNode process connects to multiple Xboard nodes simultaneously.
- **ETag caching**: Node config and user list only re-parse when the server's ETag changes.
- **Hot reload**: `cmd/server.go` uses `conf.Watch()` (fsnotify) — editing the config restarts cores+nodes automatically (`-w` flag, default on).
- **TLS / ACME**: Managed by `node/cert.go` using the `lego` library. Cert modes: `http`, `dns`, `tls`, `self`, `file`, `none`.
- **Traffic reporting**: Collected inside sing-box via a `HookServer` (`core/sing/hook.go`) that intercepts connections; aggregated and POSTed to Xboard on the `PushInterval` from node config.
- **Device limit**: Enforced in `limiter/limiter.go:CheckLimit()` using per-UUID IP maps; the alive count comes from Xboard's `/alivelist` endpoint.
- **Dynamic speed limit**: Configurable; if a user exceeds a traffic threshold within a period, their speed is capped temporarily.

---

## Build

```bash
# sing-box core (only active core)
GOEXPERIMENT=jsonv2 go build -v -o build_assets/FNode \
  -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" \
  -trimpath \
  -ldflags "-X 'github.com/tavut846/FNode/cmd.version=$version' -s -w -buildid="
```

Go version required: **1.25+** (`GOEXPERIMENT=jsonv2` is needed for the `encoding/json/v2` import).

---

## What NOT to Change Without Understanding the Impact

- `api/panel/panel.go` — query-param auth is baked in; changing param names breaks Xboard compatibility.
- `core/sing/hook.go` — traffic counting hook; mistakes here cause silent zero-traffic reports.
- `limiter/limiter.go:CheckLimit()` — device-limit logic is stateful and subtle (two-pass with `OldUserOnline`).
- `go.mod` replace directive for `sing-box` — uses a custom fork (`cedar2025/sing-box`); do not swap to upstream without testing.
