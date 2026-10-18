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
