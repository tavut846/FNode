# FNode

A V2board node server based on sing-box core, copied from V2bX.  
一个基于 sing-box 内核的V2board节点服务端，从 V2bX 复制而来，支持V2ay,Trojan,Shadowsocks,Hysteria2,Tuic等协议。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

## 特点

* 永久开源且免费。
* 支持多种协议 (Vmess/Vless, Trojan, Shadowsocks, Hysteria2, Tuic, AnyTLS).
* 支持Vless和reality等新特性。
* 支持单实例对接多节点，无需重复启动。
* 支持限制在线IP。
* 支持限制Tcp连接数。
* 支持节点端口级别、用户级别限速。
* 配置简单明了。
* 修改配置自动重启实例。
* 基于 sing-box 内核，性能卓越且功能齐全。

## 功能介绍

| 功能        | sing-box |
|-----------|-------|
| 自动申请tls证书 | √     |
| 自动续签tls证书 | √     |
| 在线人数统计    | √     |
| 审计规则      | √     |
| 自定义DNS    | √     |
| 在线IP数限制   | √     |
| 连接数限制     | √     |
| 按照用户限速    | √     |
| 动态限速(未测试) | √     |

## 面板路由与实时同步 (Panel routes & real-time sync)

FNode applies the routing you manage in the Xboard dashboard, so a route change needs no per-server edit.

- **Panel outbounds and routes**: the node config's `custom_outbounds` (`{tag, protocol, settings, proxy_tag}`, e.g. global proxies synced by the CustomNode plugin), `custom_routes`, `custom_route_rules` and the node's `routes` are turned into sing-box outbounds and rules. A route with action `proxy` sends traffic to the outbound named in its `action_value`. `proxy_tag` becomes the sing-box `detour`.
- **Merge order**: `sing_origin.json` (`OriginalPath`) → local `CustomOutbounds` in `config.json` → panel `custom_outbounds`. On a duplicate tag the later source wins and a warning is logged. The merge happens in memory: FNode never writes `sing_origin.json` or `config.json` (both are watched for hot reload).
- **Live apply**: outbounds are added, updated and removed in the running sing-box (no restart). When only routing changed, the inbound and its connections are left alone. Each apply logs one line: `applied N outbounds (origin a, local b, panel c, builtin d), M rules`.
- **WebSocket push**: on start each node calls `POST /api/v2/server/handshake`. If the panel enables WebSocket and `ws_url` starts with `ws://` or `wss://`, the node connects to it and applies `sync.config`, `sync.users`, `sync.user.delta` and `sync.devices` as they arrive. While connected, that node's HTTP polling is paused; if the connection drops, polling resumes and FNode reconnects with backoff. A malformed `ws_url` is logged and the node keeps polling.

### 对接模式 (Panel connection modes)

A `Nodes` entry connects in one of three ways:

| Mode | `ApiConfig` fields | Panel API |
|------|--------------------|-----------|
| Node (existing) | `ApiHost`, `ApiKey` (server token), `NodeID`, `NodeType` | v1 `/api/v1/server/UniProxy/*` |
| Node, type auto-detected | `ApiHost`, `ApiKey` (server token), `NodeID`, no `NodeType` | v2 `/api/v2/server/*`, type read from the config's `protocol` |
| Machine (per-server token) | `ApiHost`, `MachineID`, `MachineToken` (or the token in `ApiKey`) | v2 `/api/v2/server/*` and `/api/v2/server/machine/*` |

```json
{
  "ApiConfig": { "ApiHost": "https://panel.example.com", "MachineID": 3, "MachineToken": "MACHINE_TOKEN" },
  "Options": { "Core": "sing", "ListenIP": "0.0.0.0", "SendIP": "0.0.0.0", "CertConfig": { "CertMode": "none" } }
}
```

Every panel request (HTTP and the WebSocket dial) carries `User-Agent: FNode/<version>`.

In machine mode FNode calls `POST /api/v2/server/machine/nodes` on start and runs one node for every node the panel assigns to the machine (types FNode cannot serve, such as `mieru` or `naive`, are skipped with a warning). `Options` is the template for all of them; a `Name` gets `-<node id>` appended. Nodes added to or removed from the machine are started or stopped when the panel pushes `sync.nodes` over the machine WebSocket (`ws_url?token=..&machine_id=..`) and on every pull interval. Every push interval FNode posts `POST /api/v2/server/machine/status` with `cpu`, `mem`, `swap`, `disk`, `net` and `"client": "fnode", "version": "<version>"`.

### REALITY (vless / trojan) notes

- FNode maps the panel's `reality_settings` (`server_name`, `server_port`, `private_key`, `short_id`) to a sing-box REALITY inbound: the camouflage destination is `server_name:server_port` (443 when `server_port` is empty), `short_id` may be a single id, a comma-separated list, or empty (clients with an empty `short_id`), and the private key may be raw base64url or padded standard base64. An invalid short id (odd length, not hex, longer than 8 bytes) or key is reported with the field name when the node starts. A loopback test (`core/sing/reality_test.go`) connects a real sing-box vless+reality client with `xtls-rprx-vision`, with and without a short id.
- **"REALITY: processed invalid connection" is not a client error by itself.** sing-box logs it whenever a ClientHello fails REALITY authentication and is forwarded to the camouflage site. Internet scanners cause it constantly; a real client with a wrong public key, short id, SNI or a clock off by minutes causes the same line, and the two cannot be told apart from the server. FNode logs these at debug level only and writes `REALITY: rejected N probes in the last 5m (inbound ..., top sources: ...)` at info. If a specific client cannot connect, check its `public_key`, `short_id`, `serverName` and system time first.
- The REALITY implementation is `github.com/xtls/reality`, pulled in by the sing-box fork. `dependabot.yml` tracks it so new upstream versions arrive as pull requests.

### Log shipping to the panel (CustomNode plugin)

FNode can send its logs and sing-box's logs to the panel so they can be read in the dashboard (Xboard CustomNode plugin, `POST /api/v2/custom-node/logs`). Nothing to configure on the node: it uses the same credentials as the node API (machine mode: `machine_id` + machine token; node mode: server token + `node_id`) and `User-Agent: FNode/<version>`.

- **Log mode**: the panel decides which levels are shipped (`off`, `error`, `warn` (default), `info`, `debug`); every response carries the current `log_mode` and `flush_interval`. The panel can also push `custom_node.log_mode` over the WebSocket (`node_id: null` for the whole machine, or one node) and it applies immediately.
- **Delivery**: entries are buffered in a ring of 2000 (the oldest are dropped when the panel is down), flushed every `flush_interval` seconds or at 200 entries, at most 500 per request, messages cut at 4096 bytes. A failed push never affects a node and is retried with backoff. In mode `off` nothing is shipped and an empty request is sent every 60 seconds to learn the current mode (an idle node polls once a minute as well). On HTTP 404 (plugin missing) FNode retries every 10 minutes, silently.
- **Sources**: FNode's own log and sing-box's (inbound, outbound, router). Entries carry the node: sing-box lines by their `inbound/<type>[<node tag>]` tag, FNode lines by their `tag` field; global messages go to every connected panel. While the panel wants more detail than your `Log.Level`, the logger level is raised and the console still prints only the level you configured.

### Generated files

After every apply FNode writes, per node, into `GeneratedDir` (default: `generated/` next to `config.json`; set `"GeneratedDir"` in the sing core config to change it):

| File | Content |
|------|---------|
| `<node>.effective.json` | The final merged sing-box config: the node's inbound (users dropped), all outbounds, route rules and rule sets, plus an `_fnode` block with each outbound's source (`origin`, `local`, `panel`, `builtin`). For reading only. |
| `<node>.panel.json` | The raw panel data: `routes`, `custom_outbounds`, `custom_routes`, `custom_route_rules`. |

`<node>` is the node tag with unsafe characters replaced by `_` (e.g. `https_panel.example.com_-vless_1`). Files are written atomically, are not watched, and are removed when the node goes away. They may contain outbound passwords and keys (mode 0600).

### See what was applied

```bash
FNode config show
```

```bash
FNode config show --node vless:1 --summary
```

`FNode config show [-c /etc/FNode/config.json] [--node <tag, file name or part of the tag>] [--summary] [--dir <dir>]` prints, per node, the outbound tags with their type, source and contributing node, followed by the effective JSON (omitted with `--summary`).

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh && bash install.sh
```

### 脚本化安装 (Non-interactive install)

Machine mode (one Xboard machine token runs every node assigned to the machine):

```bash
curl -fsSL https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh | sudo bash -s -- --mode machine --panel 'https://panel' --token 'MACHINE_TOKEN' --machine-id 3
```

Node mode (server token + node id; the node type is read from the panel unless `--type` is given):

```bash
curl -fsSL https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh | sudo bash -s -- --mode node --panel 'https://panel' --token 'SERVER_TOKEN' --node-id 12
```

| Flag | Meaning |
|------|---------|
| `--mode machine\|node` | Connection mode (required for a scripted install) |
| `--panel URL` | Panel address, `http://` or `https://` |
| `--token TOKEN` | Machine token (machine mode) or server token (node mode) |
| `--machine-id ID` / `--node-id ID` | Machine id or node id |
| `--type TYPE` | Node mode only, optional: `vless`, `vmess`, `trojan`, `shadowsocks`, `hysteria`, `hysteria2`, `tuic`, `anytls`. With it FNode uses the v1 UniProxy API |
| `--version x.y.z\|latest\|pre` | Release to install (default `latest`) |
| `--branch NAME` | Branch for the `FNode` management script (default `master`) |
| `--cert-mode none\|http\|dns\|self\|file`, `--cert-domain DOMAIN` | Certificate settings written to `CertConfig` (default `none`) |
| `--force` | Replace an existing `/etc/FNode/config.json` (backed up to `config.json.bak.<time>`; with `jq` only the `Nodes` block is replaced) |
| `--print-config` | Print the `config.json` the flags produce and exit (dry run, no root needed) |

With any of these flags the script asks nothing, writes `/etc/FNode/config.json` (and a default `sing_origin.json` if there is none), enables and starts the service. Without flags it stays interactive, and `bash install.sh 0.0.21` still installs that version. An existing `config.json` is never overwritten without `--force`.

Uninstall without prompts:

```bash
curl -fsSL https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh | sudo bash -s -- uninstall --yes
```

```bash
FNode uninstall --yes
```

## 构建

# 编译 sing 内核

GOEXPERIMENT=jsonv2 go build -v -o build_assets/FNode -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" -trimpath -ldflags "-X 'github.com/tavut846/FNode/cmd.version=$version' -s -w -buildid="

## 免责声明

* 本项目复制自 [V2bX](https://github.com/wyx2685/V2bX)，只是学术研究和个人使用。
* 本人不对使用本项目产生的任何 bug、错误或后果承担任何责任。
* 由于本人能力有限，无法保证所有功能的稳定性和可用性。

## Thanks

* [Project X](https://github.com/XTLS/)
* [V2Fly](https://github.com/v2fly)
* [VNet-V2ray](https://github.com/ProxyPanel/VNet-V2ray)
* [Air-Universe](https://github.com/crossfw/Air-Universe)
* [XrayR](https://github.com/XrayR/XrayR)
* [sing-box](https://github.com/SagerNet/sing-box)
