# Universal Auth & Email Architecture Guide

This guide details a robust, secure, and scalable implementation of Authentication, User Management, and Email systems. It is based on a layered architecture that isolates business logic from transport and data layers.

---

## 1. Authentication Architecture

### A. Token-Based Auth (Sanctum/JWT)
Instead of traditional sessions, use token-based authentication for better compatibility with modern frontends and mobile apps.

- **Mechanism:** Generate a secure token upon login/signup (e.g., Laravel Sanctum, Supabase Auth).
- **Session Management:** Allow users to view and revoke active sessions (tokens) to improve account security.
- **Statelessness:** The API should ideally be stateless, relying on the `Authorization: Bearer <token>` header.

### B. Multi-Layer Security
- **Rate Limiting:** 
    - **IP-Based:** Limit registrations per IP to prevent bot attacks.
    - **Account-Based:** Limit failed login attempts per email to prevent brute-force attacks.
- **Verification:**
    - **Email Verification:** Mandate a verification code before allowing registration.
    - **Captcha:** Use services like Cloudflare Turnstile or hCaptcha on public auth routes.

---

## 2. Code Structure & Logic

### A. The "Service-First" Pattern
All authentication and user management logic is isolated in dedicated **Services** to keep Controllers lean and maintainable.

| Service | Responsibility |
| :--- | :--- |
| `RegisterService` | Validates registration data (Captcha, Whitelist, IP limits), handles invite codes, and creates user records. |
| `LoginService` | Handles credential verification, brute-force protection (rate limiting), account status checks (banning), and password resets. |
| `AuthService` | Manages Sanctum tokens, session retrieval, and token revocation. |
| `UserService` | Core user creation and utility logic (e.g., resetting traffic, managing reset days). |

### B. API Endpoints

#### 1. Authentication API (`/api/v1/passport/auth`)

| Endpoint | Method | Description | Key Payload Fields |
| :--- | :--- | :--- | :--- |
| `/register` | `POST` | User Registration | `email`, `password`, `invite_code`, `email_code` |
| `/login` | `POST` | User Login | `email`, `password` |
| `/forget` | `POST` | Password Reset | `email`, `email_code`, `password` |
| `/token2Login`| `GET` | Token-based Auth | `token`, `verify` |

#### 2. User Management API (`/api/v1/user`)

| Endpoint | Method | Description | Security Logic |
| :--- | :--- | :--- | :--- |
| `/info` | `GET` | Get Profile | Scoped to authenticated user ID |
| `/getSubscribe`| `GET` | Get Subscription | Scoped to authenticated user ID |
| `/update` | `POST` | Update Preferences | Limited to non-sensitive fields |
| `/resetSecurity`| `GET` | Reset Token/UUID | Regenerates identifiers and subscription URL |

---

## 3. User Lifecycle Management

### A. User Banning (Security Enforcement)
Banning is a two-step process to ensure immediate lockout:
1. **Status Update:** The `banned` field in the `users` table is set to `1`.
2. **Session Termination:** All active Sanctum tokens for the user are deleted via `AuthService->removeAllSessions()`.
3. **Login Prevention:** The `LoginService` explicitly checks for the `banned` status before granting a new token.

### B. User Deletion (Data Integrity)
User deletion is handled as an atomic operation (Transaction) to prevent orphaned data:
- **Related Data Cleanup:** All associated records (orders, tickets, invite codes, usage statistics) are deleted before the user record itself.
- **Atomic Deletion:** If any part of the cleanup fails, the entire operation is rolled back to maintain database consistency.

### C. Access Control (Privacy & Security)
To prevent users from accessing or modifying other users' information:
- **Middleware Protection:** The `user` middleware ensures only authenticated requests reach user endpoints.
- **ID-Based Scoping:** Controllers **never** trust user IDs passed in request bodies or query strings for profile actions. Instead, they fetch the user context directly from the authentication guard:
  ```php
  // Correct: Scoped to the authenticated user
  $user = User::where('id', $request->user()->id)->first();
  ```

---

## 4. Scalable Email System

### A. Dynamic Configuration
Instead of hardcoding SMTP settings in `.env`, settings are loaded from the database (via `admin_setting()`). This enables live updates without server restarts.

### B. Asynchronous Sending (Queues)
Emails are processed in the background to ensure API responsiveness:
1. **Job Dispatch:** Dispatch a `SendEmailJob`.
2. **Worker Processing:** Laravel Horizon or a standard queue worker handles the SMTP handshake and delivery.
3. **Variable Injection:** Uses a placeholder system (e.g., `{{ user.email }}`) for dynamic content.

---

## 5. Implementation Checklist

- [ ] **Auth:** Are tokens generated with an expiration date?
- [ ] **Security:** Is there a rate limit on the `/login` and `/register` endpoints?
- [ ] **Privacy:** Are all user endpoints scoped to `$request->user()->id`?
- [ ] **Banning:** Does banning a user also invalidate their active sessions?
- [ ] **Email:** Are emails sent via a background queue?
- [ ] **Audit:** Are password changes and sensitive updates logged?

---

## Conclusion
A robust Auth & Email system is the foundation of user trust. By decoupling logic into services, enforcing strict data scoping, and ensuring all side effects (like banning or deletion) are handled comprehensively, you create a system that is both secure and high-performing.

