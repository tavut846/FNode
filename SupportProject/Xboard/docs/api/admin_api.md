# Admin API Documentation

This document describes the APIs available to administrators.

## Configuration and Downloads

### System Settings (Download Configuration)
*   **Endpoint**: `GET /api/v2/admin/config/fetch`
*   **Description**: Retrieves the full current system configuration, including client download URLs and versions.

### Save Configuration
*   **Endpoint**: `POST /api/v2/admin/config/save`
*   **Description**: Updates system settings.
*   **Parameters**:
    *   `windows_version`: Latest Windows client version.
    *   `windows_download_url`: Latest Windows client download URL.
    *   `macos_version`: Latest macOS client version.
    *   `macos_download_url`: Latest macOS client download URL.
    *   `android_version`: Latest Android client version.
    *   `android_download_url`: Latest Android client download URL.

## User Management and Exports

### Fetch Users
*   **Endpoint**: `ANY /api/v2/admin/user/fetch`
*   **Description**: Lists users with advanced filtering and sorting.
*   **Parameters**:
    *   `filter`: JSON array of filter conditions.
    *   `sort`: JSON array of sorting rules.

### Export Users (Download API)
*   **Endpoint**: `POST /api/v2/admin/user/dumpCSV`
*   **Description**: Generates and downloads a CSV file containing the user list.
*   **Parameters**:
    *   `scope`: `all`, `selected`, or `filtered`.
    *   `user_ids`: Required if scope is `selected`.

### Batch Generate Users (Download API)
*   **Endpoint**: `POST /api/v2/admin/user/generate`
*   **Description**: Batch creates users.
*   **Parameters**:
    *   `generate_count`: Number of users to create.
    *   `download_csv`: If true, returns a CSV file of generated users.

## Statistics and Usage

### Dashboard Stats (Usage API)
*   **Endpoint**: `GET /api/v2/admin/stat/getOverride`
*   **Description**: Provides real-time dashboard data, including online users, nodes, and today/monthly traffic.
*   **Response**:
    ```json
    {
        "online_users": 100,
        "online_devices": 150,
        "today_traffic": { "upload": 1024, "download": 2048, "total": 3072 }
    }
    ```

### Detailed Analytics (Usage API)
*   **Endpoint**: `GET /api/v2/admin/stat/getStats`
*   **Description**: Provides comprehensive revenue, user growth, and traffic analytics.

### Traffic Ranking (Usage API)
*   **Endpoint**: `GET /api/v2/admin/stat/getTrafficRank`
*   **Description**: Returns top nodes or users by traffic consumption over a period.
*   **Parameters**:
    *   `type`: `node` or `user`.
    *   `start_time`: Unix timestamp.
    *   `end_time`: Unix timestamp.

### User Traffic Record (Usage API)
*   **Endpoint**: `GET /api/v2/admin/stat/getStatUser`
*   **Description**: Retrieves traffic logs for a specific user.
*   **Parameters**:
    *   `user_id`: Target user ID.

## Server Management

### List All Nodes
*   **Endpoint**: `GET /api/v2/admin/server/manage/getNodes`
*   **Description**: Retrieves all server nodes for all groups.

### Save/Update Node
*   **Endpoint**: `POST /api/v2/admin/server/manage/save`
*   **Description**: Creates or updates a node configuration.

## System Monitoring

### Queue Status
*   **Endpoint**: `GET /api/v2/admin/system/getQueueStats`
*   **Description**: Returns current Laravel job queue statistics.

### System Audit Log
*   **Endpoint**: `ANY /api/v2/admin/system/getAuditLog`
*   **Description**: Lists administrative activity logs.
