# User API Documentation

This document describes the APIs available to authenticated users.

## Authentication and Profile

### User Info
*   **Endpoint**: `GET /api/v1/user/info`
*   **Description**: Returns the current user's profile and balance.

### Change Password
*   **Endpoint**: `POST /api/v1/user/changePassword`
*   **Description**: Updates the user's login password.
*   **Parameters**:
    *   `old_password` (required): Current password.
    *   `new_password` (required): New password.

### Reset Security
*   **Endpoint**: `GET /api/v1/user/resetSecurity`
*   **Description**: Resets the user's UUID and subscription token.
*   **Response**:
    ```json
    {
        "subscribe_url": "https://example.com/api/v1/client/subscribe?token=newtoken"
    }
    ```

## Subscriptions and Client Downloads

### Get Subscription Details (Download API)
*   **Endpoint**: `GET /api/v1/user/getSubscribe`
*   **Description**: Returns active subscription details, current traffic usage, and the subscription download URL.
*   **Response**:
    ```json
    {
        "plan_id": 1,
        "token": "xxxxxxxxxx",
        "expired_at": 1672531200,
        "u": 1048576, // Uploaded bytes
        "d": 5242880, // Downloaded bytes
        "transfer_enable": 10737418240, // Total allowance in bytes
        "subscribe_url": "https://example.com/api/v1/client/subscribe?token=xxxxxxxxxx"
    }
    ```

### Subscription URL (Download API)
*   **Endpoint**: `GET /api/v1/client/subscribe`
*   **Description**: Downloads the client configuration (V2Ray/Clash/Shadowsocks format).
*   **Parameters**:
    *   `token` (required): User's subscription token.

## Usage and Statistics

### Traffic Log (Usage API)
*   **Endpoint**: `GET /api/v1/user/stat/getTrafficLog`
*   **Description**: Returns daily traffic consumption for the current month.
*   **Response**:
    ```json
    [
        {
            "u": 1024,
            "d": 2048,
            "record_at": 1672531200,
            "server_rate": "1.0"
        }
    ]
    ```

### Current Status
*   **Endpoint**: `GET /api/v1/user/getStat`
*   **Description**: Returns counts of pending orders, open tickets, and total invites.

## Servers and Knowledge

### Fetch Available Servers
*   **Endpoint**: `GET /api/v1/user/server/fetch`
*   **Description**: Lists all nodes currently available to the user based on their plan.

### Fetch Knowledge Base
*   **Endpoint**: `GET /api/v1/user/knowledge/fetch`
*   **Description**: Retrieves tutorials and help documentation.

## Orders and Billing

### Fetch Plans
*   **Endpoint**: `GET /api/v1/user/plan/fetch`
*   **Description**: Lists available plans for the user.

### Create Order
*   **Endpoint**: `POST /api/v1/user/order/save`
*   **Description**: Creates a new order for a plan.

### List Orders
*   **Endpoint**: `GET /api/v1/user/order/fetch`
*   **Description**: Lists the user's order history.

### Checkout Order
*   **Endpoint**: `POST /api/v1/user/order/checkout`
*   **Description**: Initiates payment for an order and returns the payment URL.
