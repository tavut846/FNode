# Universal API Architecture Design Guide

This guide outlines a tech-stack agnostic, robust, and scalable API architecture. Whether you are using **Laravel (PHP)**, **Express.js (Node)**, **FastAPI (Python)**, or **Go**, these principles ensure your system remains maintainable, secure, and easy to integrate.

---

## 1. Modular Layering (The "S.O.L.I.D." Structure)
Decouple your code into distinct layers to isolate side effects and simplify testing.

| Layer | Responsibility | Tech Example (Express) | Tech Example (Laravel) |
| :--- | :--- | :--- | :--- |
| **Route** | URL mapping & Middleware attachment | `routes/v1/user.routes.js` | `app/Http/Routes/V1/UserRoute.php` |
| **Controller** | Request parsing & Response formatting | `controllers/user.controller.js` | `app/Http/Controllers/UserController.php` |
| **Service** | Core Business Logic & Calculations | `services/user.service.js` | `app/Services/UserService.php` |
| **Repository** | Data access (SQL/ORM/NoSQL) | `models/user.model.js` | `app/Models/User.php` |

- **Feature Update Reason:** By isolating business logic in the **Service Layer**, you can update a feature (e.g., changing how discounts are calculated) in one file without touching your controllers or database queries.
- **Security Reason:** The **Route Layer** acts as the first line of defense, ensuring only authorized users reach the logic layer.

---

## 2. Versioning & Evolution
Always prefix your API paths (e.g., `/api/v1/...`).

- **Strategy:** When a breaking change is needed (e.g., changing a field from `name` to `full_name`), create `/api/v2`. Keep `v1` active for a grace period.
- **Feature Update Reason:** Allows you to roll out "Beta" versions of features to specific users without breaking the experience for everyone else.

---

## 3. The "Fortress" Layer (Security & Integrity)

### A. Middleware-First Defense
- **Authentication:** Use industry standards like **Supabase Auth**, JWT, or OAuth2.
- **Authorization (RBAC):** Don't just check "if logged in." Check "if admin" or "if resource owner" at the middleware level.
- **Validation (The Gatekeeper):** Use schema validators (e.g., **Zod/Joi** for Express, **FormRequests** for Laravel) to reject bad data before it hits your database.

### B. Security Reason
- **Data Integrity:** Validation prevents SQL injection and malformed data from corrupting your SQLite/MySQL tables.
- **Audit Trails:** Use global middleware to log high-impact actions (e.g., `DELETE`, `POST /admin/settings`).

---

## 4. The "Friendly" Layer (Developer & User Experience)

### A. Consistent JSON Envelope
Frontend developers should never guess what your response looks like.

```json
{
  "status": "success | fail | error",
  "message": "Human-readable feedback",
  "data": { "items": [], "meta": {} }, 
  "error_code": "INSUFFICIENT_FUNDS"
}
```

### B. Standardized Pagination
Always return metadata with lists: `total_count`, `limit`, `offset`, and `has_next`.

### C. User Friendly Reason
- **Frontend Performance:** Standardized envelopes allow React/Vite developers to create generic "hooks" (e.g., `useApiFetch`) that handle loading, errors, and data automatically.
- **Error Handling:** Providing specific `error_code` strings allows the frontend to show localized, friendly messages (e.g., "Your card was declined") instead of "Error 500."

---

## 5. Implementation Reference (Express + Supabase/SQL)

```text
src/
├── api/
│   ├── v1/                  <-- API Versioning
│   │   ├── routes/          <-- Express Routers
│   │   ├── controllers/     <-- req/res handling
│   │   └── middlewares/     <-- Supabase Auth check, Rate limiting
│   └── v2/
├── services/                <-- Business Logic (Shared with v1/v2)
├── lib/                     <-- Supabase client, DB connections
├── validations/             <-- Zod/Joi Schemas
└── types/                   <-- TypeScript Definitions
```

---

## Conclusion
A great API structure is defined by its **Contract**. Whether you use Laravel or Express, if your layers are decoupled and your responses are consistent, your project will be easier to scale, harder to break, and a joy for frontend developers to use.
