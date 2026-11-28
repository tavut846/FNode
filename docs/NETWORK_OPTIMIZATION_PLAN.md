# Network optimization plan: idle drops and packet loss (4-20%)

Status: **research and plan only, nothing implemented**. Written 2026-11-07. Pick items first; each one is small and off by default unless noted.

How to read the confidence column: **S** = backed by a source listed in section 8, **C** = read in our code or the vendored sing-box/sing-quic in this repo, **G** = guess or general experience, needs a test on a lossy link before we rely on it.

## 0. Short answer

- **The server cannot reduce the physical loss rate.** Loss of 4% is a property of the path. Sending fewer or more packets does not change the percentage. What FNode can do: keep NAT/conntrack state alive, notice dead peers faster, avoid congestion control that reads loss as congestion, and make recovery cheaper. Visible stalls drop, loss does not.
- **Idea (a) "server requests a connection to the client": not feasible as stated.** Clients sit behind NAT/CGNAT and no protocol we run lets the server dial the client. The feasible form is **keepalive on the existing connection**, which FNode already does for TCP (30 s idle, 15 s interval, `core/sing/keepalive.go`) and which QUIC does on its own.
- **Idea (b) "detect a sudden loss and reconnect back to the client": not feasible as stated** for the same reason. Feasible: detect the dead connection quickly on the server (keepalive, `TCP_USER_TIMEOUT`, QUIC idle timeout) and free it; the **client** reconnects. Reconnect speed is mostly decided by client settings (section 6).
- **Idea (c) reduce the effect of loss: feasible in part**, mainly by (1) BBR instead of CUBIC on the server, (2) tail-loss/RACK settings that already default on, (3) MTU probing against black holes, (4) UDP/QUIC transports (hysteria2, tuic) on bad links, with BBR and not Brutal at fixed rate, (5) diagnostics so we stop guessing.

## 1. How an idle connection dies under loss

| Mechanism | What happens | Value / default | Applies to |
|---|---|---|---|
| NAT/CGNAT mapping expiry (client side) | Mapping for an idle flow is dropped; the next packet from either side goes nowhere or gets a RST | Standards recommend long timeouts (TCP established >= 2 h 4 min, UDP >= 2 min, 5 min recommended: **from memory, not verified**, search returned only vendor defaults of 7440 s and 300 s). Real home/mobile/CGNAT boxes are often far shorter (30-120 s, see comment in `keepalive.go`) | All TCP and UDP nodes |
| Linux conntrack on the VPS or in front of it | `nf_conntrack_tcp_timeout_established` 432000 s (5 days); UDP 30 s, 120 s once a flow is seen both ways | Kernel doc **S** | UDP nodes behind a stateful firewall; hy2/tuic idle > 30 s with no keepalive |
| Stateful firewall / cloud security group | Own idle timers, often 60-350 s | G | Provider dependent |
| Retransmission backoff | Under loss, RTO doubles per retry. `tcp_retries2` = 15 means about 924 s before the kernel gives up on an unacknowledged established connection (RFC 1122 asks for >= 100 s, i.e. 8) | Kernel doc **S** | Every TCP connection with outstanding data |
| `tcp_syn_retries` | New connection attempts retry SYN (kernel default 6, about 127 s total; **not re-checked here**). A user on a lossy path sees long connect stalls | G | New connections, clients that reconnect |
| TCP keepalive | Off per socket unless the app asks. Kernel default 2 h idle. sing-box default for its own dials is 5 min idle / 75 s interval (docs **S**); FNode already sets inbound 30 s / 15 s (**C**) | | TCP inbounds |
| `TCP_USER_TIMEOUT` | Bounds how long unacknowledged data may stay before the socket is aborted; with keepalive it replaces the keepalive "count x interval" logic. Not in sing-box listen options (**C**: no such field in the sing-box `option/inbound.go`) | Analysis article **S** | Would need a sing-box patch or `setsockopt` hook |
| `tcp_slow_start_after_idle` | Default 1: cwnd shrinks after an idle period, so the first burst after idle is slow. Setting 0 helps bursty long-lived connections (disputed generality) | Docs/thread **S** | TCP nodes with bursty use |
| Loss detection | RACK-TLP (RFC 8985) turns many tail-loss RTOs into fast recovery; Linux enables it (`tcp_recovery`, `tcp_early_retrans` TLP). Checking values on the VPS is part of the diagnostic | RFC + kernel doc **S**, defaults on a given VPS unverified | TCP |
| Black-hole PMTU | A path that drops large packets without ICMP looks like "works for small, stalls when busy". `tcp_mtu_probing` 1 enables probing after a black hole is detected, 2 always on at `tcp_base_mss` | Kernel doc **S** | TCP; also hy2/tuic through QUIC's own PMTUD |
| Buffers | Too small limits throughput on long RTT; large `tcp_notsent_lowat` is irrelevant for loss | G | |

What this means for the user's symptom ("idle client gets disconnected"): the likely causes, in order, are (1) a NAT or firewall mapping that expired during silence and the next packets, at 4-20% loss, repeatedly hit the dead mapping or run into RTO backoff, (2) a UDP/QUIC idle timeout (30 s in hysteria2, see section 3) when keepalive packets get lost in a row, (3) retransmission backoff growing into tens of seconds so the app-level timeout fires first. These need measurement (section 5, item 1) before we change anything.

## 2. Can a server connect back to the client?

Plainly: **no, not in general.**

- vless, vmess, trojan, shadowsocks, anytls, hysteria2, tuic are all client-initiated. The server only has the client's NAT-translated address and an open mapping that exists while traffic flows. A new inbound packet from the server to that address works only for UDP and only while the mapping is alive (the same keepalive question as above). With CGNAT or a symmetric NAT even that fails.
- What is possible on the **existing** connection:
  - **TCP keepalive on accepted sockets.** Already done for every FNode inbound: 30 s idle, 15 s interval by default, configurable with `Options.TCPKeepAlive` / `TCPKeepAliveInterval` (`conf/node.go`, `core/sing/keepalive.go`, applied in `core/sing/node.go:80`). Keeps NAT state fresh and kills dead peers after about 30 + probes x 15 s (the probe count is the kernel default, 9 unless sing-box changes it: **not verified**).
  - **QUIC keepalive and idle timeout.** hysteria2: server and client defaults in the vendored sing-quic are `MaxIdleTimeout` 30 s and `KeepAlivePeriod` 10 s (**C**: `sing-quic .../hysteria/protocol.go`), hard-coded, not exposed by sing-box's hysteria2 options. tuic: sing-box inbound option `heartbeat`, docs default 10 s (**S**); FNode does not set it today (**C**: `core/sing/node.go:440`). QUIC endpoints must raise the idle timeout to at least 3 x PTO (RFC 9000 s10.1, quoted in a bug report **S**), so at high loss PTO grows and the 30 s idle value is on the edge.
  - **Mux ping.** sing-box multiplex (smux/yamux/h2mux, `h2mux` default) has no documented keepalive field in the docs page found (**S**, silence in docs). Behaviour of the vendored sing-mux library is **not verified**.
  - **Dead-connection detection**: keepalive failure, `TCP_USER_TIMEOUT` (needs patch), QUIC idle timeout.
  - **Graceful close notices so clients reconnect quickly**: TCP FIN/RST on close does this by itself; for hy2/tuic an application close frame is sent by the library on `Close()`. FNode's own restart path (`reload` in `cmd/server.go`) already closes the core; no extra protocol message exists to ask a client to reconnect. G.
- What FNode can do beyond that: log why each connection ended (section 5, item 6) so reconnect patterns become visible.

## 3. UDP and QUIC under loss

- **Hysteria2**: Brutal sends at a fixed declared rate and ignores loss, which keeps goodput up on lossy paths but cannot back off and is detectable (a 2025 study found fixed-rate senders transmit orders of magnitude more than loss-based CCAs at 5% loss **S**). With `ignoreClientBandwidth` the server uses BBR (**S** official docs; **C** FNode: `Hysteria2Config.IgnoreClientBandwidth`, `MaxBrutalMbps`, `core/sing/hysteria2_cc.go`). Choice at 4-20% loss: Brutal at an honest rate usually gives the smoothest result, BBR is the safe choice behind providers that flag UDP floods. FNode already offers both; this is a documentation and defaults question, not new code. Confidence: G for "smoothest".
- **QUIC loss recovery**: PTO replaces RTO/TLP (RFC 9002 **S**); a PTO does not shrink cwnd; only persistent congestion does (needs a long run of loss, RFC 9002 s7.6). So QUIC degrades more gracefully than TCP at 5-10% loss, which is why moving a node to hy2/tuic is the largest "real" mitigation we have. G for magnitude.
- **tuic**: `congestion_control` cubic (default), new_reno or bbr (**S**); under loss prefer `bbr`. FNode passes the panel value (`core/sing/node.go:442`). `heartbeat` default 10 s (**S**).
- **UDP NAT timeouts**: sing-box `udp_timeout` 5 min default on inbounds (**S**, applies to UDP NAT of proxied traffic, not the QUIC connection). FNode exposes `Hysteria2Config.UDPTimeout`. The QUIC connection itself is kept by the 10 s keepalive inside the 30 s idle window.
- **FEC**: none in sing-box/sing-quic. Hysteria 1 had an optional FEC option; hysteria2 does not. See section 4.

## 4. What actually reduces visible loss

| Option | Verdict |
|---|---|
| BBR on the server for TCP nodes | **Worth doing.** Studies: BBRv1 keeps goodput near capacity up to roughly 20% random loss in a netem LAN test, CUBIC collapses (0.1% loss -> ~10x less, >1% -> stalls on the B4 test) **S**. BBRv2/v3 are more loss-reactive; BBRv2 degraded badly at 10% in two papers, BBRv3 around 1% was fine **S**. Mainline Linux ships BBRv1 (`tcp_bbr`); assume v1 unless the kernel is a Google BBRv3 build. Caveat: BBRv1 produces more loss itself in shallow buffers **S**. Needs `fq` or kernel pacing. `FNode.sh` option 12 runs a third-party script (`ylx2016/Linux-NetSpeed`); the plan replaces that with a local, auditable preset (item 2). |
| Application-level FEC | **Out of scope.** Needs a protocol change on both ends, sing-box has none, and for TCP-carried traffic FEC must be below TCP. Revisit only if a QUIC-datagram FEC appears upstream. |
| Multipath / redundant send | **Out of scope** for the server: needs client support. `tcp_multi_path` exists as a listen field in sing-box (**C**) but needs MPTCP on both ends and a kernel with `net.mptcp.enabled`; untested here. Mention only. |
| Smaller MSS | Fewer bytes per lost segment but more packets; for random (not size-dependent) loss it does not help much; it does help a PMTU black hole. Prefer `tcp_mtu_probing=1` (item 3). G. |
| Pacing (`fq`) | Helps BBR and avoids burst loss on shallow buffers. Low risk. S/G. |
| Fast reconnect, retries | Client-side (section 6). |
| UDP transports on lossy links | **Best structural fix**: put affected users on hysteria2/tuic. Panel/ops decision, not code. |

## 5. Prioritized plan for FNode

Defaults stay unchanged unless the item says otherwise. File/area is where the change goes.

| # | Item | Where | Benefit | Effort | Risk | Confidence |
|---|---|---|---|---|---|---|
| 1 | **Diagnostic command** `FNode netcheck [client-ip]`: sampled loss/RTT to the gateway and a client IP, `ss -tin state established` retrans and `rto`/`unacked`, `nstat -az TcpRetransSegs TcpExtTCPTimeouts TcpExtTCPLossProbes`, `sysctl` dump of the relevant knobs, current congestion control, MTU, a conntrack timeout listing. Read-only. | `FNode.sh` on branch `FNode-script` (menu entry, subcommand), docs | Replaces guessing; tells whether the cause is NAT expiry, RTO backoff, MTU or plain loss | S-M | None | G for usefulness, tools are standard |
| 2 | **Optional sysctl preset** `FNode tune lossy` (opt-in menu entry, writes `/etc/sysctl.d/99-fnode-lossy.conf`, `FNode tune off` removes it, prints before/after): `net.core.default_qdisc=fq`, `net.ipv4.tcp_congestion_control=bbr`, `net.ipv4.tcp_mtu_probing=1`, `net.ipv4.tcp_slow_start_after_idle=0`. Not default; `tcp_retries2` is **not** in the preset (see below). | `FNode.sh`, replaces the call out to a third-party script in `install_bbr` | Medium on TCP nodes at 5-20% loss | S | Low-medium (see "when not to use") | S for BBR/mtu/idle semantics, G for net effect |
| 3 | **Expose `tcp_user_timeout`** as a node option (e.g. `Options.TCPUserTimeout` seconds, 0 = off), set through a `net.ListenConfig.Control` hook. sing-box has no field, so this is a patch to the vendored core or a listen `Control` wrapper in `core/sing/`. Value suggestion 30-60 s so a half-dead path is cut in tens of seconds, not 15 minutes. | `conf/node.go`, `core/sing/keepalive.go`, possibly `third_party/sing-box` | Faster dead-connection detection, faster client reconnect | M | Medium: too small kills healthy connections on a lossy path | S on semantics, G on value |
| 4 | **Expose tuic `heartbeat`** and a **QUIC idle/keepalive knob for hysteria2** (needs a vendored change in sing-quic options or the hy2 inbound; today hard-coded 30 s / 10 s) with defaults equal to today's values. Allow e.g. keepalive 5 s, idle 60 s for 10-20% loss. | `conf/sing.go` (`Hysteria2Config`, tuic), `core/sing/node.go`, `third_party/sing-box/option`, sing-quic patch | Fewer "idle 30 s" drops when several keepalives in a row are lost | M | Low | S (idle >= 3 x PTO rule) |
| 5 | **Document and test the existing TCP keepalive**: show effective values in `FNode config show`, add an `ss -o` check to item 1 so users can see the timer counting. Verify it also applies to Reality and anytls inbounds (keepalive is a listener option, so it should; add a unit test that asserts it on every inbound type built in `node.go`). | `core/sing/keepalive_test.go`, `docs/PROJECT_GUIDE.md` | Confirms idea (a) already works in its feasible form | S | None | C |
| 6 | **Metrics and logs**: per node count of connections closed by reason (client closed, idle timeout, keepalive failure, error), reconnects per user (same UUID new connection within N s), RTT/retrans sample from `ss` (item 1) every `PushInterval` into the debug log. Push only to the log at first; panel upload is a separate decision. | `core/sing/hook.go` (careful, see CLAUDE.md warning), `node/` | Shows whether changes helped | M | Medium: the hook is on the hot path; do it as counters only | G |
| 7 | **Hysteria2 guidance for lossy links** in the guide: when Brutal, when `IgnoreClientBandwidth`, `MaxBrutalMbps`. No code. | `docs/PROJECT_GUIDE.md` | Avoids misconfiguration | S | None | S |
| 8 | **Client guidance** page (section 6) in the docs, linkable from the panel. | docs | High (many drops are client-side) | S | None | G |
| 9 | Auto "network recovery" already exists for DNS (`core/sing/netrecovery.go`). **Not needed** for this problem; do not extend it to connection reconnects. | | | | | |

**sysctl notes (exact values, and when not to use them)**

```
# /etc/sysctl.d/99-fnode-lossy.conf  (proposed, opt-in)
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_slow_start_after_idle = 0
```

- Do **not** apply on OpenVZ/LXC guests (sysctls are read-only or shared with the host), or if `tcp_bbr` is missing (`modprobe tcp_bbr` fails; check `sysctl net.ipv4.tcp_available_congestion_control`).
- Do **not** run it on a server whose users are on a clean path and mostly download from the VPS: BBRv1 can raise retransmissions on shallow buffers and be unfair to CUBIC flows (**S**).
- **Do not lower `tcp_retries2`** as part of a preset: it shortens the time a healthy-but-lossy connection survives a burst of loss (RFC 1122 minimum is 100 s, value 8). Prefer `TCP_USER_TIMEOUT` per socket (item 3).
- **Do not set Brutal as the system default congestion control** (the Brutal project warns this slows every connection to about 1 Mbps **S**).
- `tcp_mtu_probing=2` is not recommended unless the black hole is confirmed.

## 6. Client-side recommendations (the server cannot do these)

- Use a **fallback/URLTest group** with a short interval (30-60 s) and several nodes or protocols; at 4-20% loss, TCP nodes health-check badly and flap. Prefer `interval` >= 60 s with tolerance, not seconds.
- Client TCP keepalive: sing-box outbound default is 5 min idle / 75 s (**S**, docs); set `tcp_keep_alive` 30 s and `tcp_keep_alive_interval` 15 s on the **client** outbound too, so NAT mappings stay alive from the other side.
- AnyTLS: `idle_session_check_interval` / `idle_session_timeout` / `min_idle_session` exist on the client (semantics not verified here, only an example with 30 s found); keep at least one idle session pre-opened so reconnect is instant.
- hysteria2/tuic: lower `keepalive`/`heartbeat` to 5-10 s, keep `ignore_client_bandwidth` off only if the declared rate is honest.
- Multiplex: with TCP Brutal and mux the rate is per connection and needs both ends configured (**S**); without a reason, leave mux off on lossy links, because a single TCP stall blocks every stream in the mux (head-of-line blocking, G).
- Reconnect backoff: use short, jittered retry (1 s then up to 5-10 s) rather than long exponential backoff on this class of link.

## 7. How to test on a lossy link (before and after each item)

Run on a scratch VPS or a veth/netns pair, never on production traffic. Egress-only netem affects one direction; apply to both ends for symmetrical loss.

```bash
# 5%, 10%, 20% random loss (egress), 40 ms delay
tc qdisc add dev eth0 root netem delay 40ms loss 5%
tc qdisc change dev eth0 root netem delay 40ms loss 10%
tc qdisc change dev eth0 root netem delay 40ms loss 20%
# bursty loss (more like a bad radio link): 10% loss, 25% correlation
tc qdisc change dev eth0 root netem loss 10% 25%
# remove when done
tc qdisc del dev eth0 root
```

Measure: `iperf3 -c` (TCP and `-u`) through the node, an idle test (open a session, send nothing for 60/120/300 s, then send), `ss -tin` retrans/rto, `nstat -az TcpRetransSegs TcpExtTCPTimeouts`, and the client's own reconnect count. Idle test is the one that matches the reported symptom. Compare CUBIC vs BBR, hy2 with Brutal vs BBR, tuic cubic vs bbr, each at 5/10/20%. Our own outbound `tc` on the same machine will also shape local DNS and the panel API; scope it with `tc filter` to the test port if that matters.

Logs/metrics to look at: `journalctl -u FNode -e` (closes, reload), `FNode config show`, and once item 6 exists the per-reason counters.

## 8. Sources

- sing-box Dial/Listen docs (TCP keepalive defaults 5 m / 75 s; `tcp_fast_open`; `udp_timeout` 5 m): https://sing-box.sagernet.org/configuration/shared/dial/ , https://sing-box.sagernet.org/configuration/shared/listen
- sing-box TUIC inbound (`congestion_control`, `heartbeat` 10 s, `zero_rtt_handshake`): https://sing-box.sagernet.org/configuration/inbound/tuic
- sing-box Multiplex and TCP Brutal docs: https://sing-box.sagernet.org/configuration/shared/multiplex/ , https://sing-box.sagernet.org/configuration/shared/tcp-brutal/
- Hysteria 2 full server/client config (`maxIdleTimeout` 30 s, `keepAlivePeriod` 10 s, `ignoreClientBandwidth`, BBR fallback): https://v2.hysteria.network/docs/advanced/Full-Server-Config/ , https://v2.hysteria.network/docs/advanced/Full-Client-Config/
- Linux kernel `ip-sysctl` (tcp_retries2 924.6 s, tcp_keepalive_time, tcp_slow_start_after_idle, tcp_mtu_probing, tcp_early_retrans/TLP): https://docs.kernel.org/networking/ip-sysctl.html
- Linux `nf_conntrack-sysctl` (TCP established 432000 s, UDP 30/120 s): https://sources.debian.org/src/linux/6.18.9-1/Documentation/networking/nf_conntrack-sysctl.rst
- RFC 8985 (RACK-TLP): https://rfc-editor.org/rfc/rfc8985 ; RFC 9002 (QUIC loss detection, PTO, persistent congestion): https://rfc-editor.org/rfc/rfc9002.html
- TCP_USER_TIMEOUT + keepalive behaviour: https://idea.popcount.org/2019-09-20-when-tcp-sockets-refuse-to-die/
- BBR vs CUBIC under loss: https://www3.cs.stonybrook.edu/~anshul/imc19_bbr.pdf (IMC 2019), https://queue.acm.org/detail.cfm?id=3022184 (BBR, B4 loss test), https://par.nsf.gov/servlets/purl/10252943 (BBRv2 alpha), https://arxiv.org/pdf/2107.03057 (BBRv2+), https://par.nsf.gov/servlets/purl/10545111 (BBRv3 evaluation)
- TCP Brutal behaviour and warnings: https://github.com/apernet/tcp-brutal
- netem examples: https://oneuptime.com/blog/post/2026-03-20-simulate-packet-loss-tc-netem/view ; ThousandEyes 1% loss impact: https://www.thousandeyes.com/blog/path-quality-surprising-impact-one-percent-packet-loss
- Code read in this repo: `core/sing/keepalive.go`, `core/sing/node.go`, `core/sing/hysteria2_cc.go`, `core/sing/netrecovery.go`, `conf/node.go`, `conf/sing.go`, the sing-box `option/inbound.go`, sing-quic `hysteria/protocol.go`, `hysteria2/service.go`.

## 9. Not verified

- Behaviour of the vendored sing-mux/h2mux keepalive, and AnyTLS idle-session semantics (docs search returned nothing usable).
- RFC 4787/5382/8085 timeout figures were not read from the RFC texts (vendor docs and memory only). `tcp_syn_retries` default and the keepalive probe count were not re-checked against the kernel doc.
- No source gave BBR vs CUBIC goodput at exactly 5/10/20% loss; the 20% cliff for BBRv1 is from one netem study (IMC 2019) read through a summary, and BBRv2 collapse at 10% comes from emulation papers. The VPS kernel's actual `tcp_recovery`, `tcp_early_retrans`, available congestion controls and BBR version were not checked.
- Nothing was run: no netem test, no `ss`/`nstat` on a real server, no build. All "benefit" ratings are expectations until item 1 and the test matrix in section 7 are run.
