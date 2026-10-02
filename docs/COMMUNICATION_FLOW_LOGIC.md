# System Communication & Information Flow Logic

This document details the complete end-to-end information flow between the three core entities in the proxy ecosystem:
1. **FNode** (Backend Proxy Node Server)
2. **Manager Dashboard** (Xboard Centralized Web Panel & Database)
3. **Client** (End-User Device & Proxy Application, e.g., Clash, Sing-box, Shadowrocket, v2rayN)

---

## High-Level Architecture Overview

```mermaid
graph TD
    Dashboard["Manager Dashboard (Xboard)"]
    FNode["FNode (sing-box Core)"]
    Client["Client (User Device / App)"]

    %% 1 & 3: FNode <-> Dashboard
    FNode -- "1. Node Heartbeat, Traffic (Push), Active IPs (Alive)" --> Dashboard
    Dashboard -- "3. Node Config, User Credentials, Quotas, Routing Rules" --> FNode

    %% 2 & 5: FNode <-> Client
    Client -- "5. Encrypted Inbound Proxy Traffic, Target Host/Port, Auth Handshake" --> FNode
    FNode -- "2. Decrypted Remote Web Responses, TLS Handshakes, Rate Limits" --> Client

    %% 4 & 6: Client <-> Dashboard
    Client -- "6. User Login, Plan Purchases, Sub Token Requests" --> Dashboard
    Dashboard -- "4. Subscription Configs (Clash/Sing-box YAML/JSON), Plan Expiry, Traffic Quotas" --> Client
```

---

## 1. FNode to Manager Dashboard (Xboard)

FNode acts as an HTTP client communicating with Xboard via the **UniProxy REST API** (`/api/v1/server/UniProxy/*`).

| Item / Action | Endpoint & Method | Transmitted Information & Format | Purpose in Dashboard |
| :--- | :--- | :--- | :--- |
| **Node Heartbeat & Polling** | `GET /api/v1/server/UniProxy/user`<br>`GET /api/v1/server/UniProxy/config` | Query Parameters: `token=<key>&node_id=<id>&node_type=<type>`<br>Header: `If-None-Match: "<ETag>"` | Tells Xboard that FNode is alive. Triggers `ServerService::touchNode()`, updating `last_check_at` so the node displays as **Online (Green)** in admin UI. |
| **User Traffic Consumption** | `POST /api/v1/server/UniProxy/push` | JSON Body:<br>`{ "<UID>": [ <upload_bytes>, <download_bytes> ] }`<br>Example:<br>`{ "101": [1048576, 52428800] }` | Deducts consumed bandwidth from the user's plan quota (`u` and `d`), increments node traffic volume counters, and logs traffic history. |
| **Online Client IP Addresses** | `POST /api/v1/server/UniProxy/alive` | JSON Body:<br>`{ "<UID>": [ "<client_ip_1>", "<client_ip_2>" ] }`<br>Example:<br>`{ "101": ["114.24.50.12", "223.104.134.79"] }` | Only reports clients whose traffic exceeds `DeviceOnlineMinTraffic` (200 KB). Stored in Redis (`USER_ALIVE_DEVICES_<uid>`) to enforce cross-node concurrent `device_limit`. |
| **System Resource Metrics** *(Optional)* | `POST /api/v1/server/UniProxy/status` | JSON Body:<br>`{ "cpu": 12.5, "mem": { "total": 2147483648, "used": 536870912 }, ... }` | Reports CPU load, RAM usage, swap, and disk stats to show node hardware status gauges on Xboard admin panel. |

---

## 2. FNode to Client

FNode handles low-level proxy transport connections directly with the user's client device:

| Action / Stream | Protocol / Channel | Information Transmitted to Client |
| :--- | :--- | :--- |
| **Cryptographic Handshake Response** | TLS / Reality / QUIC / Shadowsocks | - **TLS Certificate** (Let's Encrypt / Caddy cert / Self-signed) or Reality Server Hello.<br>- Protocol-specific response (Shadowsocks AEAD sub-session confirmations, VMess header responses, Hysteria 2 / TUIC connection acceptances). |
| **Proxied Internet Traffic** | Inbound connection stream | Streams back decrypted, unwrapped response payloads from requested internet services (HTML websites, video streams, API responses, game UDP packets). |
| **Bandwidth Rate Limiting** | TCP Window / Token Bucket | FNode's built-in `limiter` throttles transmission speeds to conform with the user's assigned `speed_limit` (Mbps) or dynamic throttles. |
| **Instant Rejection / Block Responses** | TCP RST / ICMP / Fin | - Instant connection termination if user is disabled or exceeded device limit.<br>- Immediate reject on outbound IPv6 destinations with `DisableIPv6: true` (enabled by default across core and node configurations), preventing client 10-second dial timeouts, eliminating `exchange6` DNS socket errors, and triggering instant IPv4 fallback. |

---

## 3. Manager Dashboard (Xboard) to FNode

Xboard responds to FNode's UniProxy polling requests with centralized node parameters and user states:

| Response Endpoint | Information Sent from Dashboard to FNode | How FNode Uses It |
| :--- | :--- | :--- |
| `GET /config` | - **Listen & Port**: `server_port`, `listen_ip`.<br>- **Transport Settings**: WebSocket path, gRPC service name, HTTPUpgrade path, HTTP request headers.<br>- **Security Settings**: Standard TLS parameters or Reality configurations (`server_name`, `dest`, `server_port`, `short_id`, `private_key`).<br>- **Bandwidth & Obfs**: `up_mbps`, `down_mbps`, `obfs`, `obfs-password`, `salamander`.<br>- **Base Intervals**: `pull_interval` (user poll frequency), `push_interval` (traffic report frequency).<br>- **Auditing & Route Rules**: Blocking rules for BT/torrents, spam, government sites, or anti-SSRF CIDRs. | FNode dynamically invokes `core.AddNode(tag, node, options)` to create or update the sing-box inbound (binding SNI to `tls.server_name` and camouflage destination to `handshake.server`) and router rules in memory without restarting the process. |
| `GET /user` | - Array of active users:<br>`[{ "id": 101, "uuid": "...", "speed_limit": 100, "device_limit": 3 }]`<br>- Encoded via MsgPack (fast binary) or JSON streaming. | FNode parses the list, dynamically updates active users via `core.AddUsers()` / `core.DelUsers()`, and configures the local speed and device limiters. |
| `GET /alivelist` | Map of active IP counts per user across all servers in the cluster:<br>`{ "alive": { "101": 2, "102": 1 } }` | FNode checks if a user has already hit their global `device_limit` on other nodes before allowing a new IP to establish a connection. |

---

## 4. Manager Dashboard (Xboard) to Client

Xboard provides the client with configuration and subscription profiles:

| Channel / Interface | Information Sent to Client |
| :--- | :--- |
| **Subscription Delivery** (`/api/v1/client/subscribe?token=...`) | Formats node lists into client-compatible configuration formats (Clash YAML, Sing-box JSON, Surge, Shadowrocket, V2Ray links).<br>Contains: node domain/IP, port, UUID, password, transport path, TLS SNI, Reality public keys. |
| **Quota & Account Headers** | HTTP headers sent during subscription fetch:<br>`Subscription-Userinfo: upload=1048576; download=52428800; total=107374182400; expire=1780000000`<br>Informs client apps of used bandwidth, total plan allowance, and account expiration timestamp. |
| **Web Portal UI** | Web interfaces for registering accounts, buying plans, submitting support tickets, and viewing service announcements. |

---

## 5. Client to FNode

The client device initiates proxy connections through FNode:

| Stage | Data Transmitted to FNode |
| :--- | :--- |
| **1. Protocol Handshake** | Client connects to FNode listening port and sends cryptographic handshake proving identity:<br>- **Shadowsocks**: Key-derived AEAD sub-keys.<br>- **VMess / VLESS**: User UUID and authentication hash.<br>- **Trojan**: SHA-224 password hash.<br>- **Hysteria 2 / TUIC**: User UUID/password with optional Salamander obfuscation. |
| **2. Target Destination Request** | Encapsulated destination address (Domain name or IPv4/IPv6 IP + target port, e.g. `google.com:443`). |
| **3. Upstream Data Stream** | Raw application data sent by user applications (HTTP requests, TLS Client Hello, DNS queries, uploaded files). |

---

## 6. Client to Manager Dashboard (Xboard)

Clients interact directly with Xboard for management and subscription lifecycle:

| Action | Information Sent to Dashboard |
| :--- | :--- |
| **Subscription Fetch** | HTTP `GET /api/v1/client/subscribe?token=<user_sub_token>` with client `User-Agent` (e.g. `ClashMeta`, `sing-box`, `Shadowrocket`) so Xboard can return the correct syntax. |
| **Authentication & Login** | User email, hashed password, 2FA TOTP code, or Telegram OAuth login tokens. |
| **Plan Purchases & Orders** | Plan selection, billing cycle, discount coupons, and payment gateway interactions (Alipay, WeChat, Stripe, USDT/Cryptomus). |
| **Support Inquiries** | Customer service support tickets, bug reports, and account cancellation requests. |

---

## Full End-to-End Sequence Diagram

```mermaid
sequenceDiagram
    autonumber
    actor User as User Device / App (Client)
    participant X as Manager Dashboard (Xboard)
    participant F as FNode Backend
    participant Web as Target Internet Host

    Note over User,X: Phase 1: Subscription & Setup
    User->>X: GET /api/v1/client/subscribe?token=XYZ (User-Agent: clash)
    X-->>User: Returns Node IPs, Ports, UUIDs, Quota Headers

    Note over F,X: Phase 2: Node Initialization & Sync
    F->>X: GET /api/v1/server/UniProxy/config?token=KEY&node_id=1
    X-->>F: Returns Inbound Ports, Protocols, TLS, Reality Keys, Rules
    F->>X: GET /api/v1/server/UniProxy/user?token=KEY&node_id=1
    X-->>F: Returns Active User UUIDs, Speed & Device Limits
    Note over X: Node marked Online (last_check_at updated)

    Note over User,F: Phase 3: Proxying Client Traffic
    User->>F: Handshake with UUID/Password + Target (e.g. google.com:443)
    F->>F: Authenticate UUID, check SpeedLimit and DeviceLimit
    F->>Web: Connect to google.com:443 (Outbound Direct)
    Web-->>F: Return Web Data / Stream Packets
    F-->>User: Encrypt and Stream Response to Client

    Note over F,X: Phase 4: Periodic Accounting & Reporting
    F->>X: POST /api/v1/server/UniProxy/push: { "101": [upload_bytes, download_bytes] }
    X-->>F: 200 OK (Traffic Deducted from Plan)
    F->>X: POST /api/v1/server/UniProxy/alive: { "101": ["114.24.50.12"] }
    X-->>F: 200 OK (Cluster Active IP Records Updated)
```
