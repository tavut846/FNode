# Node Availability & API Reference

This document outlines the supported node types and API endpoints for both Xboard (Frontend/Management) and FNode (Backend/Service).

## 1. Supported Node Types

| Node Type | Xboard Support | FNode Support | Xboard-Node Support | Notes |
| :--- | :---: | :---: | :---: | :--- |
| **VMess / V2Ray** | ✅ | ✅ | ✅ | Standard protocol. |
| **VLess** | ✅ | ✅ | ✅ | Modern Xray/Sing-box protocol. |
| **Trojan** | ✅ | ✅ | ✅ | Widely used for censorship circumvention. |
| **Shadowsocks** | ✅ | ✅ | ✅ | Supports various ciphers (AEAD, 2022). |
| **Hysteria / Hysteria2** | ✅ | ✅ | ✅ | UDP-based high-performance protocol. |
| **Tuic** | ✅ | ✅ | ✅ | QUIC-based protocol. |
| **AnyTLS** | ✅ | ✅ | ✅ | Specialized TLS-based protocol. |
| **Socks** | ✅ | ❌ | ✅ | Supported by Xboard-Node natively. |
| **HTTP** | ✅ | ❌ | ✅ | Supported by Xboard-Node natively. |
| **NaiveProxy** | ✅ | ❌ | ✅ | Native support in Xboard-Node. |
| **Mieru** | ✅ | ❌ | ✅ | Supported by Xboard-Node. |

---

## 2. API Endpoints Reference

### 2.1 General Node API (UniProxy / V1)

Used by FNode for most operations.

- `GET /api/v1/server/UniProxy/config`: Retrieve node configuration.
- `GET /api/v1/server/UniProxy/user`: Sync user list (supports msgpack/ETag).
- `POST /api/v1/server/UniProxy/push`: Report traffic usage.
- `POST /api/v1/server/UniProxy/alive`: Report online user devices (IPs).
- `GET /api/v1/server/UniProxy/alivelist`: Get global online list for limits.
- `POST /api/v1/server/UniProxy/status`: Report node load (CPU/Mem/Disk) - *Not implemented in FNode*.

### 2.2 Xboard V2 (Modern API)

Newer, more efficient endpoints.

- `POST /api/v2/server/handshake`: Real-time websocket handshake.
- `POST /api/v2/server/report`: Unified reporter (traffic + status + metrics).
- `GET /api/v2/server/config` / `user`: Aliases for V1 UniProxy.

### 2.3 Specialized / Legacy APIs

For specific backends like `tidalab-ss` or `tidalab-trojan`.

- **Shadowsocks (Tidalab)**:
- `GET /api/v1/server/ShadowsocksTidalab/user`
- `POST /api/v1/server/ShadowsocksTidalab/submit`
- **Trojan (Tidalab)**:
- `GET /api/v1/server/TrojanTidalab/config`
- `GET /api/v1/server/TrojanTidalab/user`
- `POST /api/v1/server/TrojanTidalab/submit`

---

## 3. Missing Pieces & Implementation Gaps

Despite the available APIs in Xboard, FNode (the current backend) has several missing implementations:

1. **System Status Reporting**:
    - **Gap**: FNode does not collect or report CPU load, memory usage, or disk space to Xboard.
    - **Impact**: Dashboard cannot show "Node Load" or "System Status" for FNode instances.
    - **API missing**: `POST /api/v1/server/UniProxy/status` (V1) or `POST /api/v2/server/report` (V2).

2. **Real-time Synchronization (V2 Handshake)**:
    - **Gap**: FNode relies on periodic polling (Pull/Push Interval) rather than persistent websocket connections.
    - **Impact**: Configuration changes or user additions take up to one polling interval to apply.
    - **API missing**: `POST /api/v2/server/handshake`.

3. **XHTTP Transport Support**:
    - **Status**: ✅ **Implemented in FNode** (via fork switch to `cedar2025/sing-box`).
    - **Note**: While `xhttp` is now supported, this fork lacks the `Xver` field for Reality configurations.

4. **Metrics & Detail Monitoring**:
    - **Gap**: Advanced metrics like active connection counts per user are not fully reported.
    - **API missing**: V2 `metrics` field in reporting.

5. **Additional Protocol Support**:
    - **Gap**: Protocols like `naive`, `socks`, `http`, and `mieru` are defined in Xboard but not handled in FNode's API client.
