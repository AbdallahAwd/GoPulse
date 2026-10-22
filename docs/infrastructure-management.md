# Infrastructure management: implementation and verification

Date: 2026-10-08. Scope: existing GoPulse architecture, shared security fixes, Qdrant, Nginx load balancers and MCP. The user authorized **isolated local tests only**. No existing VPS inventory was used and no production installation, firewall change or deletion was performed.

## 1. Audit and delivered changes

The pre-implementation audit is in [infrastructure-audit.md](infrastructure-audit.md). It covers routes, handlers, services, SQLite, imports/exports, SSH, monitoring, jobs, authentication, permissions, frontend and existing integrations. Existing MySQL, PostgreSQL, MongoDB, Redis, SQLite, PocketBase and Appwrite implementations remain in place. Qdrant is the only added engine.

| Area | Defect corrected | Implementation |
|---|---|---|
| Authentication | Insecure JWT fallback and privileges cached in tokens | Required configured secret; HS256 and expiry; current user permissions read for every request. |
| Authorization | Writes lacked permissions; arbitrary database/LB IDs bypassed route scope | Shared permission guard, resource ownership checks, linked backend authorization and approval-time permission checks. |
| Browser security | Cross-origin requests and metric streams accepted | Origin guards, scoped snapshots/broadcasts, live stream authorization and token-expiry checks. |
| SSH | Host verification disabled; commands could hang or retry writes | Strict known_hosts, handshake deadline, bounded session creation/execution, no transport retries of executed writes. |
| Credentials | Server/database response credentials and embedded OAuth secret | Response redaction; removed committed OAuth values; private SQLite file mode; Qdrant secret stays on VPS. |
| Database provisioning | Unsafe shell parameters and silent destructive failures | Quoted engine provisioning arguments, validated inputs, checked cleanup and retained metadata on failure. |
| Nginx persistence | Creation omitted backends; edits could lose working config | Atomic LB/backend transactions, serialized deployment, backups and rollback on syntax/reload/routing failure. |
| Nginx routing | Health probes ran from dashboard; success could target a different vhost | Entry-VPS probes and verification of the candidate's response identity header. |
| UX | Large setup form; early success notifications; no progress/retry | Sequential wizard, durable operation progress, concrete failures, safe editing, backend enable/disable and retries. |
| AI tools | Missing integration and approval policy | Authenticated MCP transport, closed tool schemas, allowlisted services, durable audit and human approvals. |

The audit found real existing implementations rather than an application made entirely of mocks. Their complete VPS behavior was not verified in this environment. Legacy explorer, backup, domain and other remote command paths still need a separate Linux/VPS integration and security review; this change does not certify them as production safe.

## 2. Requirements and design decisions

Functional requirements delivered: API-key authenticated Qdrant provisioning and management; collection and snapshot workflows; safe Nginx configuration and guided setup; existing engine preservation; operation tracking; authenticated structured AI tools using the same services as the dashboard.

Planning assumptions, **not measured capacity**: one dashboard instance, up to 50 managed VPS nodes, up to 10 simultaneous operators, predominantly metrics reads with occasional infrastructure writes. No DAU/MAU, availability SLA or fleet benchmark was supplied. At five-second polling, 50 nodes create roughly 10 collection cycles per second; collection work depends on each provider's remote calls. Scaling to thousands of nodes requires testing and a revised scheduler, not an unsupported capacity claim.

| Non-functional requirement | Current mechanism and limit |
|---|---|
| Latency | Metrics served from memory with timestamps. HTTP handlers bounded; MCP reads use 20-second contexts, remote HTTP uses 10 seconds, asynchronous writes have an eight-minute ceiling. These are limits, not measured latency SLOs. |
| Availability | Dashboard and SQLite remain a single control-plane failure domain. Nginx and Docker containers continue on their VPS independently when the dashboard is unavailable. |
| Reliability | Persistent progress/results, exclusive active-resource index, per-server Nginx locks, per-database Qdrant mutation locks, checked deployment results and safe retry entry points. |
| Security | Current RBAC, server scopes, strict SSH trust, same-origin policy, bounded input, no shell MCP tool, secret redaction and browser-only approval. HTTPS must be provided for remote access. |
| Scalability | Goroutines and the existing per-SSH-client session semaphore preserve current concurrency. SQLite and in-process locks intentionally support a single application instance. |

```text
Dashboard HTML / vanilla JS ---- REST handlers ---+
                                                  |
LLM / external MCP client ---- MCP JSON-RPC -------+--> shared Go services
                                                       |       |
                                       operations/audit|       |SSH with known_hosts
                                                       v       v
                                                     SQLite   VPS
                                                              Docker/Qdrant
                                                              Nginx/systemd

VPS metric providers --> collector --> scoped cache/history --> REST / WebSocket
```

Go/Chi remains the backend because the project already has typed services and one-binary deployment. Embedded HTML/Tailwind/vanilla JS keeps the existing UI and avoids a framework migration. REST remains appropriate for explicit resource and lifecycle operations; GraphQL would add a second policy surface without a demonstrated query-composition need. MCP translates validated tool calls directly into existing services.

SQLite WAL remains the durable control-plane store: relational resource ownership and atomic configuration writes matter more here than distributed write throughput. No Redis, external message broker or microservices were added. Metrics are cached in the existing in-memory collector; asynchronous work is executed by bounded goroutines while its intent and results persist in SQLite. Process restart marks active work `interrupted` rather than replaying potentially destructive commands. Pending approval closures intentionally do not survive restart.

### Persistence and indexes

Existing `servers`, `users`, `projects`, `databases`, `domains`, `load_balancers` and `load_balancer_backends` tables are preserved. Server IDs scope resources; LB IDs scope backend rows. Existing server/resource indexes remain. New startup migrations add:

| Table | Important columns | Purpose |
|---|---|---|
| `resource_settings` | `id` primary key, JSON `data` | Nonsecret Qdrant version, bind, port and storage path; removed transactionally with its database. |
| `operations` | `id` primary key, `owner`, `resource`, `status`, JSON `data` | Preview, affected targets, progress, errors and results. |
| `tool_audit` | numeric ID, actor, tool, server, outcome, operation ID, timestamp | Durable tool and approval history without credentials or raw arguments. |

The partial unique index on `operations(resource)` applies while status is `running` or `awaiting_approval`. This makes duplicate admission atomic. SQLite uses a five-second busy timeout and a single connection per shared store. Operation history currently lists the latest 200 records globally before filtering by owner. Audit/history retention and pagination are future work, not implemented claims.

## 3. Setup and migration requirements

1. Back up the current dashboard data before upgrading. Migrations are additive. A stopped application permits a consistent copy of its database, keys and configuration; copying a live `.db` alone can omit WAL data.
2. Supply `GOPULSE_AUTH_JWT_SECRET` with at least 32 random characters. The insecure fallback is rejected. Keep it stable across restarts; changing it invalidates existing sessions. The first account created on an empty store becomes admin; there is no active `admin/changeme` bootstrap login.
3. In **Connect server**, enter the SSH port and choose **Review SSH key**. Verify the displayed SHA-256 fingerprint through a trusted channel, then choose **Trust this key**. Use the **Verify SSH key** shield button on an existing server card to approve its key or review a replacement. Discovery never sends login credentials or saves trust. Approved keys persist per server in SQLite and configuration exports; a mismatch fails closed without falling back to known_hosts. For servers without a saved pin, supply a trusted OpenSSH known_hosts file through `GOPULSE_SSH_KNOWN_HOSTS`, or use the dashboard process owner's `~/.ssh/known_hosts`, including `[host]:port` notation for nonstandard ports. There is no automatic trust-on-first-use toggle.
4. `GOPULSE_DATA_DIR` overrides the normal `~/.gopulse` store for isolated deployments and tests. New directories are created with mode 0700 and SQLite with mode 0600.
5. The OAuth secret previously committed in `config.yaml` and `config.example.yaml` must be rotated in GitHub. Configure your replacement with `GOPULSE_GITHUB_CLIENT_ID` and `GOPULSE_GITHUB_CLIENT_SECRET`; both configuration files now contain empty values. Removing a value from the current checkout does not erase Git history.
6. Put the dashboard behind HTTPS for remote users and MCP clients. Preserve the external Host header. Direct TLS sets Secure cookies; plain HTTP behind a TLS-terminating proxy does not automatically set that flag in the existing deployment model. Do not expose the plain HTTP origin publicly.
7. Existing viewers need explicit write permissions. Database actions use `database:write`; load balancers use the existing `domains:write`; service restart uses the added `services:write`. The user editor includes services, processes, system and firewall permissions. File command execution requires `terminal:write`; global reboot/shutdown of the dashboard host requires wildcard permission. Admin remains unrestricted; custom LB hosts require administrator/wildcard permission.

Existing non-Qdrant infrastructure credentials remain in SQLite and administrator configuration exports. Those exports can include SSH private keys for migration. They are sensitive backups; file permissions do not provide encryption at rest. Adding a key-managed credential vault would require an explicit migration design and was not represented as completed here.

## 4. Qdrant management

**Manage** opens a dedicated vector database workbench in the server workspace. Overview shows the authenticated collection count and connection status; Collections supports filtering, vector configuration details, creation and confirmed deletion; Connection provides HTTP/gRPC endpoints and explicit key reveal; Snapshots provides collection-specific creation and typed-confirmation restore; Settings contains network access and deployment details; Logs loads remote diagnostics on demand. Resource and tab links survive refresh and browser history. API keys are cleared when switching tabs or leaving the resource. Inventory and other database engines retain their existing views.

Use **Databases → Vector Databases → Qdrant → select VPS → Install and verify**. Advanced settings are collapsed by default. The VPS needs Docker daemon access, Python 3, disk space, image-registry access and noninteractive sudo. This workflow checks prerequisites but does not install Docker or change firewall rules.

Defaults: `qdrant/qdrant:v1.19.0`, host bind `0.0.0.0`, HTTP port `6333`, gRPC port `6334`, dedicated storage `/var/lib/gopulse/databases/<database-id>`. The image version is pinned and configurable as an explicit `v1.x.y` tag; no automatic upgrade uses `latest`. The pinned [upstream Dockerfile](https://raw.githubusercontent.com/qdrant/qdrant/v1.19.0/Dockerfile) supplies a root-default image compatible with these protected bind mounts. Alternate custom image users/rootless mappings were not tested.

Installation creates private `storage` and `snapshots` directories, writes a 0600 credential configuration, runs a labeled container with persistent mounts and restart policy, then polls the authenticated collections endpoint before reporting running. Existing storage directories are refused to prevent adopting or purging unrelated data. A generated API key stays on the target VPS; it is absent from SQL, inventory responses and operation previews. Authorized database managers can explicitly retrieve it from **Manage → Connection → Show API key** or `GET .../databases/{id}/credentials`; that response uses `Cache-Control: no-store`. You may also supply your own 32–256 character key at setup. Key rotation is not included.

New installations publish HTTP and gRPC on all IPv4 interfaces by default, including installations requested through MCP (which still require owner approval). Choose `127.0.0.1` explicitly for local access or another IPv4 address to bind a specific interface. API-key authentication remains required. GoPulse health checks use `127.0.0.1` when the published bind is `0.0.0.0`.

For an existing local-only database, open **Manage → Settings → Network access**, set the bind address to `0.0.0.0`, choose the HTTP/gRPC ports, and apply. The tracked operation stops and retains the previous owned container, starts the replacement with the same storage/snapshots/credential mounts, verifies authenticated health, saves metadata, and removes the stopped previous container. Startup failures restore the previous container. A backup name ending in `-network-backup` after an interrupted operation requires reconciliation before retrying; it is never silently overwritten. Existing instances are not exposed merely by upgrading GoPulse. Legacy HTTP-only settings remain HTTP-only until explicitly changed; `grpc_port: 0` disables gRPC in the network update.

Use the displayed server endpoint (for example `http://YOUR_SERVER_IP:6333`) in an app and send the key in the `api-key` header. The gRPC endpoint normally uses port `6334`. Allow the selected TCP ports in host and cloud firewalls if needed. GoPulse does not alter firewall policy or configure Qdrant TLS; use a trusted network or TLS termination to protect traffic and credentials.

The Manage view provides authenticated status, version, Docker CPU/memory stats, redacted logs, start/stop/restart, collection list/details/create/delete, snapshots and removal. Collection creation validates name, dimensions and distance, handles an already matching collection and verifies it after creation. Status can report starting/unavailable after restart until connectivity returns; success is not assumed from a Docker command's exit alone.

Snapshot restore accepts only a validated snapshot filename already in the managed collection snapshot directory, requires typed collection confirmation, and runs as a tracked operation. It cannot fetch an arbitrary remote URL. Snapshots stay on the VPS: scheduled Qdrant backup, offsite copies, snapshot download/upload, cluster recovery and restore-success behavior against a real image were not verified or added.

Removing the service requires confirmation and retains persistent files by default. Optional API purge requires the same confirmation. Cleanup first obtains a successful Docker listing and verifies the ownership label before removing an existing container; missing containers from partial installs are recoverable. Failed daemon access, ownership mismatch or cleanup retains dashboard metadata. Retained storage includes the protected configuration/key; service removal is not credential erasure.

### API additions

All server routes require authentication, server access and resource ownership; writes also require the appropriate permission. Responses use the existing `{success,data,message/error}` envelope.

| Route (under `/api`) | Behavior |
|---|---|
| `POST /servers/{server}/databases/qdrant/install` | `{name,api_key?,config:{version?,bind_address?,host_port?,grpc_port?,storage_path?}}`; returns 202 with operation. |
| `GET /servers/{server}/databases/{id}/qdrant/config` | Nonsecret deployment settings. |
| `PUT /servers/{server}/databases/{id}/qdrant/network` | `{bind_address,host_port,grpc_port}`; returns a tracked operation. Requires `database:write` and server ownership. |
| `GET /servers/{server}/databases/{id}/credentials` | Explicit Qdrant API-key read for `database:write`; no-store response. |
| `GET /servers/{server}/databases/{id}/qdrant/diagnostics` | Health, version, resource stats and redacted logs. |
| `GET/POST .../qdrant/collections` | List or create using `{name,size,distance}`. |
| `GET/DELETE .../qdrant/collections/{name}` | Details or deletion; deletion requires `?confirm={name}`. |
| `GET/POST .../qdrant/collections/{name}/snapshots` | List or create a snapshot. |
| `POST .../qdrant/collections/{name}/snapshots/restore?confirm={name}` | `{filename}`; returns tracked operation. |
| Existing `POST .../databases/{id}/start`, `/stop`, `/restart` | Qdrant lifecycle dispatch through DatabaseService. |
| Existing `DELETE .../databases/{id}?confirm={id}` | Remove; keep persistent data by default. |

## 5. Load balancer workflow and request lifecycle

Nginx remains the load balancing engine. Wizard decisions are entry VPS → backend VPS nodes → traffic → strategy → health → review. The seventh stage shows deployment progress. Round Robin, Least Connections and IP Hash explain their behavior in plain language. Advanced health settings stay collapsed; selection cannot advance without a backend.

1. Dashboard submits the complete configuration, including backends. RBAC checks the entry server and every linked backend/project server; unknown project/server references fail.
2. The service validates names, domain/IP, ports, health path/interval, IDs, weights, algorithms and at least one enabled primary backend. Duplicate domains on an entry VPS are rejected.
3. The API persists an exclusive operation and returns 202 promptly. The UI polls the operation rather than declaring early success. A second operation for the entry VPS is rejected while one is active.
4. The worker checks SSH, curl, sudo and Nginx prerequisites. If Nginx is missing, automatic installation supports Debian/Ubuntu through apt. Other package managers are not automatically installed. It checks listener collisions, inspects UFW when available and probes backends **from the entry VPS**.
5. Candidate file/symlink contents are staged with backups. `nginx -t` must succeed before reload. Shell traps restore the old file and symlink if staging, syntax, reload or local routing verification fails.
6. Routing must return a successful health response and the candidate's `X-GoPulse-Load-Balancer` identity header. This prevents an unrelated pre-existing virtual host from producing a false successful verification.
7. Only verified edits replace stored configuration/backends atomically. Failed edits preserve the prior active configuration and record the error. Failed creates remain visible for Retry. SQL persistence failure after application triggers a remote rollback attempt for edits.

The backend list supports add/remove and enabled/disabled state; stale health updates cannot re-enable a disabled backend. Deployment is serialized per entry server. Nginx uses passive failure detection (`max_fails`, `fail_timeout`, retryable upstream failures); dashboard health probes report status and do not rewrite Nginx to implement an active health-check module. Traffic counters are not claimed: per-LB access/error logs are available.

HTTPS requires a DNS name, port 443 and an existing Let's Encrypt certificate/key on the entry VPS. Configuration generation includes TLS and HTTP-to-HTTPS redirect. Certificate issuance/renewal stays with the existing Domains/Certbot integration and was not exercised here. A competing Domains virtual host for the same hostname must be reconciled before deployment; the new routing check rejects false success and restores the old candidate. There is no automatic virtual-host ownership transfer.

New load balancer routes: `PUT /api/servers/{server}/load-balancers/{id}` for supported edits and `POST .../{id}/deploy` for retry/redeployment. Create/update/backend changes return a tracked operation. Deletion requires `?confirm={id}` and checks remote cleanup before removing metadata. Existing listener/backends and target scope cannot be inferred solely from an HTTP 202.

The shell rollback handles command failures and trappable signals; abrupt VPS loss, SIGKILL or broken SSH can leave an uncertain remote state. There is no distributed transaction between SQLite and a VPS filesystem. Inspect state before retrying interrupted work. External DNS, host/cloud firewall reachability, actual certificate negotiation and Internet traffic are separate deployment checks.

## 6. MCP connection and security policy

Endpoint: `POST https://<dashboard-host>/api/mcp`. Implementation is stateless Streamable HTTP with JSON responses, protocol `2025-06-18`; authenticated GET returns 405 because SSE subscriptions are not offered. This follows the transport's [JSON response and optional SSE rules](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports). It does not implement OAuth discovery, automatic token refresh, old SSE transport, sampling, resources or prompts. Use a client that permits a manually configured Authorization header; no particular external LLM client was certified in this run.

Open **Connect AI client → Create MCP token** as the intended dashboard user. Tokens expire after one hour and inherit current user/server permissions. Set the client's Authorization header to `Bearer <token>`. The token's MCP-only audience prevents using it for ordinary REST writes or approving its own operation. Keep it out of URLs, logs and repository configuration. Generate a new token when it expires; deleting the account revokes access.

Example request to initialize (the placeholder is your private token):

```http
POST /api/mcp HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"your-client","version":"1.0"}}}
```

Send `notifications/initialized` and include `MCP-Protocol-Version: 2025-06-18` on subsequent requests. Discover schemas with `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`. Example call:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_server_metrics","arguments":{"server_id":"your-server-id"}}}
```

| Risk | Tools | Policy |
|---|---|---|
| Read | `list_servers`, `get_server_metrics`, `get_service_status`, `list_databases`, `get_database_status`, `list_qdrant_collections`, `list_load_balancers`, `get_load_balancer_status`, `get_operation_status` | Authentication, current server scope, resource ownership and audit. Database status checks are Qdrant-only; list status can be stale. |
| Low write | `create_qdrant_collection` | `database:write`, validated schema, exclusive tracked operation, post-create verification. |
| High write | `install_qdrant`, `restart_service`, `create_load_balancer`, `deploy_configuration` | Matching write permission plus owner approval from the dashboard before execution. |
| Destructive | None exposed | Database/LB deletion, snapshot restore, firewall changes and arbitrary shell are unavailable through MCP. |

Service tools allow only `nginx`, `postgresql`, `mongod`, `mysql`, `redis-server`; no invented service names or shell command arguments. MCP Qdrant installation uses the external-access defaults and previews the bind and ports before owner approval. LB tools use existing authorized servers rather than arbitrary network hosts.

High-risk calls return an operation in `awaiting_approval` with a concrete preview and affected target IDs. The human opens **Operations / Approvals** under the same account, reviews the requested action, then approves with a browser cookie session and dedicated approval header. Bearer approval is rejected. Permissions and all target scopes are checked again; approval expires after 15 minutes and cannot be replayed. This is a human approval boundary, not independent two-person approval.

Tool results include `structuredContent` as well as a JSON text content block. Long operations return `data.operation_id` and status; poll `get_operation_status`. Operation statuses are `awaiting_approval`, `running`, `completed`, `failed`, `expired`, `interrupted`. A completed operation means the implemented checks succeeded; it is not proof of external Internet reachability. Notification cancellation does not cancel accepted durable writes; deadlines/shutdown bound execution.

Closed schemas reject unknown properties, wrong types, missing fields, invalid ranges and unsupported values. Requests are limited to 64 KiB and one JSON-RPC message. MCP limits each user to 60 requests/minute, in addition to the shared 100 requests/minute IP limit. Origins are checked, and a durable audit row must be written before any tool execution. Audit records do not include API keys, passwords or raw tool bodies.

## 7. Verification evidence

| Check | Result and environment |
|---|---|
| `go test -race ./...` | Passed across all packages; isolated stores and localhost listeners. No race detector failures. |
| `go vet ./...` | Passed. |
| Native `go build ./cmd/server` | Passed with embedded assets. |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/server` | Passed; build validation, not a Linux runtime test. |
| `node --test web/tests/infrastructure.test.cjs` | Five tests passed: invalid wizard input, duplicate submission prevention, full backend payload/operation tracking, retry feedback, display escaping and absent login socket elements. |
| `node --check` | All 22 frontend JavaScript files passed syntax checks. No TypeScript stack exists. |
| Nginx routing/failover | **Real local Nginx** with two local HTTP backends: generated syntax valid, both backends received traffic, requests reached the survivor after one stopped, identity header checked. |
| Deployment recovery | Real isolated shell execution with a local sudo shim: syntax, reload and routing failures restore the previous file/symlink. SSH/package/systemd effects simulated. |
| Qdrant | Injectable executor fixtures cover installation/private port/credential handling, authenticated verification, collection CRUD/idempotency, bounded snapshot paths, partial failures and metadata preservation. Real isolated shell tests also cover cleanup of missing/owned containers and rejection of replaced/offline containers with a Docker shim. Docker/container/API effects simulated. |
| MCP/API/security | HTTP tests cover initialization/discovery, token restrictions, permissions, resource ownership, revocation, schema rejection, approval/no execution before approval, replay protection, audit, origin and rate limits. Remote effects simulated. |
| SSH | Trusted/mismatched/missing host key tests and cancellation. No live VPS session. |
| Browser | Real isolated dashboard with disposable SQLite data: all six wizard decisions, empty-backend rejection, two selected backends and review; private Qdrant defaults and advanced settings; operations and MCP connection instructions. Install/Create/token issuance were not submitted. |

macOS dependency compilation/linking emitted existing go-m1cpu/LC_DYSYMTAB warnings; commands exited successfully. Early cache/listener sandbox restrictions were resolved with a temporary Go cache and authorized isolated listener execution. Two new fixture failures were corrected (missing fixture user ID and server store); final gates passed.

Screenshots: [load-balancer-wizard.png](load-balancer-wizard.png), [qdrant-setup.png](qdrant-setup.png). These show disposable QA records, not production deployments. Real Qdrant image startup, Linux sudo/systemd package behavior, live snapshot recovery, multi-VPS routing, ACME/DNS and cloud firewall isolation remain unverified because only local testing was authorized and Docker was unavailable.

## 8. Reliability, performance and next improvements

There is no active-active control-plane failover. A standby restore must include dashboard state, trusted host keys and private keys; Qdrant storage/snapshots require separate VPS backups. Configuration export includes nonsecret Qdrant deployment settings but not its VPS API key, data or snapshots. Operation/audit tables are not part of the JSON migration export; full SQLite backup preserves them. Restart recovery records interruption and requires operator reconciliation, preventing unsafe automatic replay.

The bottlenecks are sequential LB probes, remote command latency, a single SQLite write connection and slow metric WebSocket clients. Write deadlines bound stalled clients; the existing cache avoids SSH work per metrics view. No new capacity or availability benchmark was run. A public static CDN remains part of the existing frontend dependencies; embedded JS does not remove external Tailwind/font availability dependencies.

Before fleet scale increases, measure collector cycle duration, SSH queue wait, SQLite busy time, operation duration/failure rate and WebSocket delivery delay. Useful next changes are bounded health-probe concurrency, audit/history retention, metrics/export tracing, a tested credential vault, live Linux/Qdrant/HTTPS integration CI and packaged frontend dependencies. For multiple control-plane instances, first introduce distributed operation ownership/leases and an appropriate shared database/queue; simply load balancing this process would break lock/recovery assumptions.

The existing user-edited `.github/workflows/gopulse-deploy.yml` was preserved. No commits, push or deployment were performed.
