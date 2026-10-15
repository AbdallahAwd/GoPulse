# Infrastructure audit — before implementation (2026-10-08)

Scope: bootstrap/routes, auth/RBAC, SQLite stores/migrations/import/export, SSH manager, monitoring/WebSocket collector, Docker/process/files/system services, database provisioning/lifecycle/explorer/backups/scheduler, domains, Nginx load balancers/configuration/health/logs, deployment queue/webhooks, embedded templates/JavaScript, tests and CI. Existing CI workflow has user edits and is excluded from this change.

This is the initial finding record; classifications below describe the code before correction. Final implementation, remaining limitations and verified evidence are in [infrastructure-management.md](infrastructure-management.md).

Architecture: embedded HTML/Tailwind/vanilla JS → Chi/JWT → Go service layer → SQLite or SSH → Docker/systemd/Nginx. Preserve this monolith and existing engines (MySQL, PostgreSQL, MongoDB, Redis, SQLite, PocketBase, Appwrite). There is no MCP or vector database integration.

| Component | Audit classification | Evidence / defect |
|---|---|---|
| SQLite and legacy imports | Implemented, baseline tests pending | WAL, single connection, parameterized queries; LB creation does not persist supplied backends atomically. |
| Authentication | Partial/security defects | JWT privileges are stale until expiry; insecure fallback secret; server mutations lack permission checks; cookie CSRF/origin policy absent. |
| Resource authorization | Broken | Database and LB handlers use arbitrary resource IDs independently of authorized route server. Linked LB target servers are not authorized. |
| SSH | Partial/security defects | Host key verification disabled; commands have no execution deadlines; transport retries can duplicate already executed writes. |
| Database provisioning | Partial/security defects | Credentials returned in responses; PostgreSQL/Mongo/Redis parameters inserted into shell; unchecked deletes remove records on failure. Existing engines have real SSH commands, not mocks, but no live evidence. |
| Database explorer/backups | Implemented with residual risk | Several legacy engine commands interpolate inputs; no complete remote integration coverage. Must not expose arbitrary query/command execution through MCP. |
| Nginx load balancers | Broken deployment recovery | Creates without backends; asynchronous errors; unvalidated domain/algorithm/host; failed nginx -t deletes previous configuration; removal drops record before cleanup; health probes originate from dashboard instead of entry VPS. |
| Load balancer UI | Partial | One large form plus separate backend forms; no deployment progress or retry; configuration cannot be edited safely. |
| Monitoring | Partial/security defects | Global socket broadcasts every server; any Origin accepted; history query not server scoped. |
| Projects/deployments/firewall/domains | Existing real implementations | Reviewed interfaces, commands, routes and tests; not replaced. Remote effects remain unverified. |
| Qdrant / MCP | Missing | No service, API, UI or protocol transport. |

Verification policy: user authorized isolated tests only. Never connect to configured production servers or start the normal application against ~/.gopulse. Test with temporary stores, injectable remote executors, and isolated fixtures. Baseline go test is downloading dependencies; record its outcome separately. Local tests cannot prove VPS package permissions, host firewall, ACME/DNS, Docker image startup, or multi-VPS routing.

Execution order: (1) audit, (2) shared security/reliability corrections, (3) Qdrant and tests, (4) Nginx safe deployment/wizard and tests, (5) MCP/auth/approval/audit and tests, (6) final build/vet/race/frontend verification and setup report.

## Phase 2 corrections

Current user permissions resolved from SQLite on every authenticated request; HS256 and expiry required. Added common write permission/origin guards, wired and repaired rate limiting, scoped database/LB IDs, filtered WebSocket broadcasts, protected stats history, redacted database/server response credentials, strict known_hosts SSH verification, bounded command execution without write retries, quoted provisioning parameters, and made remote deletion failures retain metadata. Removed the insecure JWT fallback and repaired environment key mapping. Regression tests and compilation follow as dependencies become available.

## Phase 3 implementation / verification checkpoint

Added Qdrant Docker deployment with pinned configurable image version, private IPv4 binding, persistent storage/snapshots, generated or supplied API key stored only in a 0600 VPS configuration, authenticated collection health verification, lifecycle, collection CRUD/details, resource usage/logs with redaction, safe removal retaining data by default, asynchronous installation/progress, and dedicated dashboard workflows. Existing database service/store owns the records. Isolated tests cover private install commands/verification, invalid input, and failed deletion retaining metadata. First test attempt was blocked by sandbox writes to the default Go cache; pinned dependencies are downloading with authorized tool escalation, and a temporary build cache is being used. Docker daemon is unavailable. Live Docker/VPS behavior remains unverified; final test gate is mandatory.

## Phase 4 implementation / verification checkpoint

Preserved Nginx. Added strict domain/host/port/algorithm validation, authorized linked server/project targets, atomic LB/backend persistence, no creation without enabled primary backends, serialized per-entry-server deployment, entry-VPS health probes, safe staged file/symlink application with rollback trap for syntax/reload/routing failures, metadata preservation on failed edits/deletes, retry/configuration editing/backend enable-disable, dedicated logs, operation progress, and a six-decision wizard followed by deployment verification. TLS configuration consumes existing Domains certificates; absent certificates produce an actionable failure. No firewall modifications occur. Isolated shell tests exercise rollback. Real local Nginx routing and backend failure tests require localhost listeners; sandbox listener restriction was encountered and a bounded escalation requested. Qdrant unit/fixture tests passed before this phase.

## Phase 5 implementation / verification checkpoint

Added a stateless MCP JSON transport over existing typed services, MCP-only short-lived bearer tokens, explicit read/low/high-risk tool allowlists, current RBAC and server/resource checks, closed input schemas, durable tool audit, rate limits and bounded execution. High-risk calls persist a preview and await an owner browser approval; agent tokens cannot approve or access REST. Revalidation, approval expiration, duplicate admission and replay protection are covered by HTTP/service tests. No destructive, firewall or arbitrary shell tools are exposed. OAuth discovery and SSE subscriptions are outside this implementation.

## Phase 6 final evidence and follow-up corrections

Full Go race tests and vet passed; native and Linux amd64 builds passed; five dependency-free frontend tests and 22 JavaScript syntax checks passed. Real isolated Nginx routed to two HTTP backends and survived one backend stopping. Shell rollback tests preserved previous configuration after syntax/reload/routing failures. Browser checks covered the complete wizard review and private Qdrant setup with disposable state; deployments were not submitted.

Final review also corrected duplicate-domain admission, verification against the wrong Nginx virtual host, stale health updates re-enabling disabled backends, signal trap recursion, partial Qdrant cleanup ownership checks, login-page metric socket errors, live metric permission revocation, operation panic handling and premature asynchronous backend success messages. Committed OAuth values were removed from configuration files and rotation documented. No production state was used. Real Qdrant Docker/API effects, Linux VPS operations, ACME/DNS/TLS negotiation and cloud firewall isolation remain unverified.
