# GoPulse — LLM Agent Architecture & Design Guide

Welcome! This document provides a developer-level architectural map of GoPulse to help you understand the design patterns, storage mechanisms, and codebase constraints.

---

## 🏛️ Directory Structure & Layering

GoPulse is built as a monolithic VPS management dashboard with embedded frontend assets:

```
GoPulse/
├── cmd/server/          # Server bootstrapper & route registry (main.go)
├── internal/
│   ├── config/          # Viper-based configuration loader (config.yaml/env)
│   ├── middleware/      # HTTP middleware (auth, recovery, logging)
│   ├── handlers/        # HTTP controllers (translates requests to services)
│   ├── services/        # Business logic layer (SSH executors, backup schedulers)
│   ├── monitor/         # Background collector & stats history ring buffer
│   ├── store/           # Data persistence layer (SQLite + legacy JSON)
│   └── models/          # Shared domain entities & data schemas
├── web/
│   ├── templates/       # Go html templates (embedded at build time)
│   └── static/          # Static assets (Tailwind CSS, vanilla JS)
```

### Flow Direction
Requests enter through `main.go` → Routed via `chi` router → Validated by `middleware/auth.go` → Dispatched to `handlers` → Executed via `services` → Saved/fetched via `store`.

---

## 🗄️ Database & Storage Layer (`internal/store/`)

GoPulse uses **SQLite** as its database engine.
- **SQLite Database Path**: Located at `~/.gopulse/gopulse.db`.
- **Legacy JSON Support**: The stores originally persisted data in individual JSON files (e.g. `servers.json`, `users.json`). To maintain compatibility, `sqlite.go` implements a one-time legacy import helper (`importJSONOnce`) which reads legacy JSON files and populates the SQLite tables during initialization.
- **Schema Migrations**: Database tables are defined and migrated programmatically on startup in `sqlite.go`. Avoid raw SQLite configuration modifications; always update the migration array in `sqlite.go` if schema modifications are required.
- **Timestamp Preservation**: Store addition methods (e.g. `AddProject`, `AddDatabase`, `AddDomain`) conditionally set `CreatedAt = time.Now()` only when the field `IsZero()`. This preserves timestamps during imports or restorations.

---

## 🔐 Authentication & User Management

- **User Bootstrapping**: If the database contains zero users, the first user created is automatically assigned the `admin` role with a wildcard permission (`*`).
- **Passwords**: User passwords are saved as bcrypt hashes.
- **User Import**: The `AddUserRaw(models.User)` store method bypasses the default role allocation and password-hashing steps. It writes the record exactly as provided. This allows account migrations between instances without resetting passwords.

---

## 💾 Export/Import Backup & Migration Feature

The dashboard includes full configuration backup capabilities to facilitate migration between server hosts.

### Data Model
- **`GoPulseExport`**: An envelope containing a version, timestamp, and arrays of all 5 entities (`Servers`, `Projects`, `Databases`, `Domains`, `Users`).
- **`ImportReport`**: Logs the count of records that were `created`, `skipped`, or `updated` per entity, along with any individual validation/insertion errors.

### Conflict Resolution Modes
- **`skip` (Default)**: If an entity matching the imported ID already exists, it is left completely untouched.
- **`overwrite`**: Updates existing fields with the imported data:
  - **Domains**: Since `DomainStore` does not expose an update query, the engine performs a secure `RemoveDomain` followed by `AddDomain` sequence.
  - **Users**: Overwrites fields including the password hash without re-crypting it.

### 🔑 SSH Private Key Portability
To ensure migrated backups do not break because of local file dependencies:
1. **On Export**: The `ExportAll()` store method reads the local private key file content for key-authenticated servers and embeds it into the `ssh_key_content` JSON field.
2. **On Import**: The `ImportAll()` method extracts the key content, writes it to the local system keys directory (`~/.gopulse/keys`), assigns it a collision-proof filename (`imp_<uuid>_<original_name>`) with restricted `0600` permissions, and rewrites the server's database `KeyPath` to point to the local path.

---

## ⚠️ Important Implementation Guidelines

1. **Keep Imports Port-Agnostic**: Key paths must always be derived dynamically using the target environment's home directory (`os.Getenv("HOME")`). Do not hardcode filesystem roots.
2. **Clipboard Access**: When implementing copy buttons, provide a fallback text selector (`document.execCommand('copy')`) in JavaScript to support plain-HTTP contexts.
3. **Vanilla CSS & Tailwind**: The project uses Tailwind CSS loaded via CDN globally. Minimize introducing complex custom CSS build pipes; rely on vanilla Tailwind classes and inline styles where appropriate.
