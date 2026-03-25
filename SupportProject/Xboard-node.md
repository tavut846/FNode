# Xboard Node API Analysis

This document provides a detailed analysis of the Xboard project's node API structure and how FNode connects to it.

## 1. Xboard Node API Structure

Xboard (a Laravel-based dashboard) provides two versions of its node API. FNode currently primarily utilizes the V1 (UniProxy) style.

### 1.1 API Versions and Routing
Routes are defined in `app/Http/Routes/V1/ServerRoute.php` and `app/Http/Routes/V2/ServerRoute.php`. They are loaded by `App\Providers\RouteServiceProvider`.

#### V1 (Legacy / UniProxy Style)
- **Base Prefix**: `/api/v1/server/UniProxy/`
- **Middleware**: `server` (validates node token and ID)

| Endpoint | Method | Controller Action | Description |
| :--- | :--- | :--- | :--- |
| `config` | GET | `UniProxyController@config` | Fetches node configuration (network, TLS, rules). |
| `user` | GET | `UniProxyController@user` | Fetches available users. Supports ETag and msgpack. |
| `push` | POST | `UniProxyController@push` | Reports user traffic (upload/download). |
| `alive` | POST | `UniProxyController@alive` | Reports online user device IPs. |
| `alivelist` | GET | `UniProxyController@alivelist` | Fetches current online devices for enforcement. |
| `status` | POST | `UniProxyController@status` | Reports node load (CPU, Memory, Disk). |

#### V2 (Modern Style)
- **Base Prefix**: `/api/v2/server/`
- **Middleware**: `server`

| Endpoint | Method | Controller Action | Description |
| :--- | :--- | :--- | :--- |
| `handshake` | POST | `ServerController@handshake` | Exchanges websocket info for real-time updates. |
| `report` | POST | `ServerController@report` | Unified report (traffic + alive + status + metrics). |
| `config` | GET | `UniProxyController@config` | Alias for V1 config. |
| `user` | GET | `UniProxyController@user` | Alias for V1 user. |

---

## 2. FNode API Connection Analysis

FNode (Go-based node) implements its communication logic in the `api/panel` package.

### 2.1 Implementation Details
- **API Client**: `api/panel/panel.go` defines the `Client` struct using the `resty` HTTP client. It automatically appends `node_type`, `node_id`, and `token` as query parameters.
- **Node Configuration**: `api/panel/node.go` implements `GetNodeInfo()`, which calls `/api/v1/server/UniProxy/config`.
- **User Management**: `api/panel/user.go` implements:
    - `GetUserList()`: Calls `/api/v1/server/UniProxy/user`.
    - `GetUserAlive()`: Calls `/api/v1/server/UniProxy/alivelist`.
    - `ReportUserTraffic()`: Calls `/api/v1/server/UniProxy/push`.
    - `ReportNodeOnlineUsers()`: Calls `/api/v1/server/UniProxy/alive`.

### 2.2 Task Management
Background tasks are managed in `node/task.go`:
- **`nodeInfoMonitor`**: Periodically pulls node metadata and user lists.
- **`reportUserTrafficTask`**: Periodically reports traffic usage and online devices.
- Intervals are controlled by `PullInterval` and `PushInterval` received from the Xboard config.

### 2.3 Key Findings & Gaps
1. **API Version**: FNode uses the **V1 UniProxy** endpoints and does not yet support the **V2 report** unified endpoint or the **handshake** mechanism.
2. **Missing Status Reporting**: FNode lacks an implementation for reporting system resource usage (CPU, Memory, Disk). While Xboard provides `/status` (V1) and `report` (V2) for this, FNode does not call them.
3. **Data Format**: FNode supports both JSON and **msgpack** for user list synchronization to optimize bandwidth.

---
*Analysis completed on 2026-03-25.*
