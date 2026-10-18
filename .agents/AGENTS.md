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
│   ├── monitor/         # Background collector & persistent stats history adapter
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
- **SQLite Database Path**: Located at `$GOPULSE_DATA_DIR/gopulse.db`, defaulting to `~/.gopulse/gopulse.db`.
- **Legacy JSON Support**: The stores originally persisted data in individual JSON files (e.g. `servers.json`, `users.json`). To maintain compatibility, `sqlite.go` implements a one-time legacy import helper (`importJSONOnce`) which reads legacy JSON files and populates the SQLite tables during initialization.
- **Schema Migrations**: Database tables are defined and migrated programmatically on startup in `sqlite.go`. Avoid raw SQLite configuration modifications; always update the migration array in `sqlite.go` if schema modifications are required.
- **Timestamp Preservation**: Store addition methods (e.g. `AddProject`, `AddDatabase`, `AddDomain`) conditionally set `CreatedAt = time.Now()` only when the field `IsZero()`. This preserves timestamps during imports or restorations.

---

## 🔐 Authentication & User Management

- **User Bootstrapping**: Public registration uses the atomic `RegisterFirstUser` insert. Exactly one initial account receives the `admin` role and wildcard permission (`*`); further public registrations are rejected. Administrator-created users use `AddUser`.
- **Passwords**: HTTP authentication handlers hash user passwords with bcrypt before calling the store; `AddUser` does not hash passwords itself.
- **User Import**: The `AddUserRaw(models.User)` store method bypasses the default role allocation. It expects an already hashed password, just like the other store methods. It writes the record exactly as provided. This allows account migrations between instances without resetting passwords.

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

## Contributor verification

Run `make check`, `make test-race` and `make test-tooling`. `make security` scans reachable Go vulnerabilities; `make secrets` scans publishable source and all locally available Git history. Both remain blocking in CI/releases. See `CONTRIBUTING.md` for setup, `docs/releases.md` for versioning and packaging, and `SECURITY.md` for deployment boundaries and historical credential exposure. Keep local configuration, runtime state, private keys and real infrastructure screenshots out of Git. The canonical module path is `github.com/AbdallahAwd/GoPulse`, matching the repository clone URL.

## Monitoring and alert delivery

Production history reads SQLite `metric_minutes` through `MonitoringStore`; the legacy in-memory `StatsHistory` constructor remains available for embedders. `MonitoringService.Observe` atomically commits minute summaries, rule evaluation state, incident events, and webhook outbox entries. Network delivery runs separately with leases and retries. Keep raw measurements distinct from SQL chart aggregates; never substitute zero for missing readings. The alert routes require server scope and `server:write` for writes. Protect and redact webhook URLs and signing keys. See `docs/monitoring.md` for evaluation semantics, retention, and migration limits.

## Remote Backups

`RemoteBackupService` owns persisted scheduling, two execution workers, per-plan overlap protection and restart cleanup. `SSHBackupBackend` streams native dumps through standard age encryption to SFTP; tests download the recorded archive, authenticate it fully, and restore into isolated scratch storage. Never restore tests into production containers or mount production data. Keep keys under `DataDirectory()/remote-backup-keys` and out of exports/logs. Both `database:write` and `server:write`, plus both server scopes, are required for writes and key/archive downloads. Resource deletion must check backup-plan dependencies before any remote destructive action; database lifecycle changes use the shared lock. See `docs/remote-backups.md` for recovery, retention boundaries and verification limits.

## Serverless Functions

`FunctionService` owns immutable deployment snapshots, eight execution workers (four per managed server), user/RBAC rechecks, cron triggers, bounded live logs, and durable cleanup. `SSHFunctionBackend` passes source/event only through stdin to allowlisted, pinned Docker images. Keep functions non-root, network-disabled, read-only, mount-free, and resource-limited. Function bearer tokens are hash-only and authenticate only the exact POST trigger endpoint; never broaden the auth exemption. Pending cleanup must block executions and deletion and survive history pruning. The supported control plane is one GoPulse process. See `docs/functions.md`; real Docker tests are explicit opt-in.

## MCP function gateway

Reuse `/api/mcp` and the official MCP SDK; never add a public function management auth exemption. Only explicitly selected, deployed, unpaused functions may appear as dynamic tools. Require an opaque scoped agent credential, both server:write/docker:write, current owner authorization and server scope. Use the durable operations approval/idempotency/audit flow, pin the accepted version, and recheck exposure, deployment, scopes and credential before execution and result delivery. Revocation cancels running MCP invocations on the next one-second authorization check. Store bounded JSON returns independently of the console log limit; output is untrusted. See `docs/mcp-functions.md`.
