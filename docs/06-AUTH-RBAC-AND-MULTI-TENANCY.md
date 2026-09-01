# 06. Authentication, RBAC & Multi-Tenancy

> **Enterprise Multi-Tenant Security & Access Control**
> Enforces strict tenant data isolation and Casbin role-based permission policies.

---

## 1. Multi-Tenant Isolation Architecture

The platform guarantees zero cross-tenant data leaks:
- Every authenticated request derives the active `TenantID` from context:
  ```go
  tenantID := shared.TenantIDFromContext(ctx)
  ```
- **SQL Guard**: All database queries parameterize `WHERE tenant_id = ?`.
- **Pre-Commit Enforcement**: The repository includes automated tenant lint scripts (`scripts/tenant-lint.sh`) that fail the build if hardcoded `TenantID: "1"` literals exist in code.

---

## 2. Role-Based Access Control (Casbin RBAC Matrix)

| Capability / Resource Area | Super Admin (`admin`) | Org Admin (`org_admin`) | Dispatcher (`dispatcher`) | Accountant (`accountant`) | Viewer (`viewer`) | Driver (`driver`) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| **User & Team Management** | ✅ All Tenants | ✅ Tenant Only | ❌ | ❌ | ❌ | ❌ |
| **Fleet & Vehicles** | ✅ | ✅ | ✅ | ❌ | 👁️ Read | 👁️ Assigned |
| **Bookings & Trips** | ✅ | ✅ | ✅ | ❌ | 👁️ Read | 👁️ Assigned |
| **Live Map & Telemetry** | ✅ | ✅ | ✅ | ❌ | 👁️ Read | ❌ |
| **Invoices & Settlements** | ✅ | ✅ | ❌ | ✅ | 👁️ Read | ❌ |
| **Kharcha Approvals** | ✅ | ✅ | ❌ | ✅ | 👁️ Read | ❌ |
| **Founder Dashboard** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **AI Assistant Chat** | ✅ | ✅ | ✅ | ✅ | 👁️ Read | ❌ |

---

## 3. Session & API Token Authentication

- **Web Portal Users**: Secure HTTP-only session cookies with CSRF validation.
- **Mobile Driver App & External APIs**: Cryptographic Bearer tokens (`Authorization: Bearer <token>`) validated via `middleware.RequireAPIAuth`.
- **Password Policy**: Bcrypt hashed passwords requiring minimum length, uppercase, numbers, and special symbols.
