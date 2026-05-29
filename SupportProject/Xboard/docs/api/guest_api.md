# Guest API Documentation

This document describes the APIs available to guest users (unauthenticated).

## Authentication (Passport)

### Register User
*   **Endpoint**: `POST /api/v1/passport/auth/register`
*   **Description**: Registers a new account.
*   **Parameters**:
    *   `email` (required): User's email address.
    *   `password` (required): User's password.
    *   `email_code`: Email verification code (if enabled).
    *   `invite_code`: Referral invite code (optional).

### Login
*   **Endpoint**: `POST /api/v1/passport/auth/login`
*   **Description**: Authenticates a user and returns a token.
*   **Parameters**:
    *   `email` (required): User's email.
    *   `password` (required): User's password.

### Forgot Password
*   **Endpoint**: `POST /api/v1/passport/auth/forget`
*   **Description**: Initiates the password recovery process.

### Quick Login URL
*   **Endpoint**: `POST /api/v1/passport/auth/getQuickLoginUrl`
*   **Description**: Generates a quick login URL for a user.

### Email Verification
*   **Endpoint**: `POST /api/v1/passport/comm/sendEmailVerify`
*   **Description**: Sends a verification code to the provided email.
*   **Parameters**:
    *   `email` (required): Target email address.

## Public Information

### Fetch Plans
*   **Endpoint**: `GET /api/v1/guest/plan/fetch`
*   **Description**: Retrieves the list of available subscription plans.

### Public Configuration
*   **Endpoint**: `GET /api/v1/guest/comm/config`
*   **Description**: Retrieves common site configuration (app name, base URL, etc.).

## Client Downloads

### Get App Versions and Download Links
*   **Endpoint**: `GET /api/v1/client/app/getVersion`
*   **Description**: Returns the latest versions and download URLs for Windows, macOS, and Android clients.
*   **Response**:
    ```json
    {
        "windows_version": "1.0.0",
        "windows_download_url": "https://example.com/win.exe",
        "macos_version": "1.0.0",
        "macos_download_url": "https://example.com/mac.dmg",
        "android_version": "1.0.0",
        "android_download_url": "https://example.com/android.apk"
    }
    ```

## External Integrations

### Telegram Webhook
*   **Endpoint**: `POST /api/v1/guest/telegram/webhook`
*   **Description**: Receives incoming Telegram bot messages.

### Payment Notification
*   **Endpoint**: `GET|POST /api/v1/guest/payment/notify/{method}/{uuid}`
*   **Description**: Receives asynchronous payment status updates from gateways.
