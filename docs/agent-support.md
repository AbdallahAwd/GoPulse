# GoPulse agent support

GoPulse provides a stateless, authenticated MCP endpoint at `/api/mcp` for Claude Code, Codex, and other Streamable HTTP MCP clients. Open **Agents** in the global navigation to create a credential, select servers and permissions, and copy client configuration.

## Research and approach

Both [Codex](https://developers.openai.com/codex/mcp/) and [Claude Code](https://code.claude.com/docs/en/mcp) support HTTP MCP servers with bearer authentication. Codex can read the bearer from an environment variable; Claude Code supports environment references in `.mcp.json` headers. This makes one MCP integration useful to both clients without a client-specific backend or model API subscription inside GoPulse.

Use the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) for protocol negotiation, JSON-RPC, notifications, cancellation, tool discovery, and Streamable HTTP. Version `v1.3.1` is pinned to preserve this project's Go 1.24 toolchain. Later inspected SDK releases require Go 1.25. The current implementation interoperates through the SDK's supported stable protocol revisions, including `2025-11-25`; it does not claim support for the newer draft protocol. The [MCP transport specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports) permits stateless JSON responses; no persistent MCP sessions or SSE subscription service is needed for the initial integration.

### Existing gaps addressed

The previous endpoint had 14 hand-coded tools focused on Qdrant, Nginx, metrics, and a few services. It issued one-hour JWTs inheriting the owner's access, had no per-credential revocation or server selection, and exposed connection setup through a database/Activity dialog. It did not cover applications, Docker, files, firewall, cron, or general server administration.

The new implementation retains the legacy tool names and one-hour token flow for compatibility, while adding the wider dashboard tool catalog, revocable scoped credentials, an Agents page, per-agent audit identity, and full-control/supervised policies. Legacy JWT credentials remain limited to the original tool family and require supervised writes; use a new credential for the expanded catalog.

## Requirements and implemented scope

- Connect native MCP clients using an HTTPS endpoint or a trusted local loopback address.
- Inspect authorized servers, applications, databases, Docker resources, operating-system users/processes, firewall, cron jobs, domains, and load balancers.
- Perform the corresponding dashboard actions using typed, allowlisted tools.
- Provide read-only, supervised, and full-control access, constrained by the owner's current rights and explicit server selection.
- Track writes asynchronously, preserve operation history across restarts, and prevent duplicate writes when the client supplies a stable `request_id`.
- Store only bearer hashes, expose newly created secrets once, support expiry/revocation, and deny ordinary REST access to agent credentials.
- Preserve existing dashboard behavior and validate without changing managed resources.

This is an administrative control plane, not an application data plane. There are no invented DAU or throughput assumptions. Concrete initial limits are 25 active credentials per user, 500 cron jobs per server, 60 MCP requests/minute per credential, 1 MiB MCP request bodies, 128 KiB captured action output, 30-second synchronous tool deadlines, eight concurrent operation workers, eight-minute operation deadlines, and 15-minute approval expiry. Existing API rate limits also apply to dispatched requests. High-volume automation should use explicit pagination and sensible polling intervals.

## Architecture

```text
Claude Code / Codex / MCP client
  └─ Authorization: Bearer <credential>
      └─ /api/mcp
          ├─ GoPulse authentication: token hash, expiry, owner ID, current rights
          ├─ MCP SDK: discovery, negotiation, JSON-RPC and tool calls
          ├─ closed tool schema + server scope + permission + mode checks
          ├─ durable audit receipt
          ├─ read: authenticated in-process API dispatch / existing service
          └─ write: persisted operation
              ├─ supervised: review → approve or reject in dashboard
              └─ full control: execute within granted scope
                  └─ recheck credential/rights → existing API/ownership checks
                      └─ services → SSH / Docker / engine adapters

SQLite: credential hashes, audit identity, operations, request deduplication
Browser: agent connection setup, one-time secret display, Activity approvals
```

The API bridge makes no loopback network request and does not fabricate a browser cookie or impersonate an administrator. It constructs a request for a fixed allowlisted route and re-enters normal authentication, permission, and resource-ownership middleware. Its dispatch marker exists only in Go context; an HTTP header cannot grant that capability. Each dispatch receives a new Chi routing context rather than reusing the pooled context of the original MCP request.

## Access modes and permissions

| Mode | Reads | Changes | Intended use |
| --- | --- | --- | --- |
| Read-only | Inventory and permitted diagnostic/sensitive reads | Mutation tools are hidden and blocked | Investigation and status checks |
| Supervised | Same scope checks | Every write waits for browser approval | Controlled administration |
| Full control | Same scope checks | Permitted writes start automatically | Explicitly delegated automation |

Full control does **not** grant missing permissions or another server's access. Agents cannot approve their own requests, mint credentials, or use arbitrary dashboard REST routes. Client-side approval policy remains independent: the generated configuration does not disable it.

Scopes reuse the dashboard's write permissions: `server:write`, `projects:write`, `database:write`, `docker:write`, `domains:write`, `cron:write`, `files:write`, `terminal:write`, `services:write`, `processes:write`, `system:write`, `firewall:write`, and `user:manage`.

Additional scopes are:

- `files:read`: read file contents and directories, potentially containing credentials.
- `logs:read`: application, Docker, firewall, and cron execution output.
- `database:read`: read rows/documents through the explorer.
- `secrets:read`: application environment values; database credential retrieval also requires `database:write` and the owner's corresponding authority.
- `dashboard:admin`: global administration, including registering servers and controlling the GoPulse host. Global account/admin tools additionally require all-server access. Only administrators can select all current and future servers.

Read scopes narrow access to server-readable routes; they are not blanket write grants. Token permissions are intersected with the owner's current permissions on every request. Credentials bind to the user's stable ID: deleting and recreating the same username cannot revive an old credential. Selecting all servers deliberately includes future registered servers; a selected-server credential does not.

Shell commands and cron command definitions inherit the configured OS/SSH account's privileges. GoPulse scopes select the dispatch target; they are not an operating-system sandbox or network firewall for commands running as root. Files, logs, query results, and repository content are untrusted input and never additional authorization. Prefer supervised access when the agent is processing such content alongside consequential operations.

## Tool coverage

The initial catalog contains 153 tools. Tool discovery is filtered for each credential. Inputs are closed JSON schemas, paths are assembled from validated identifiers, query values are encoded, and there is no generic arbitrary HTTP-request tool.

| Area | Supported operations |
| --- | --- |
| Servers | Inventory, metrics/history, connection configuration, register/update/unregister, remote restart/shutdown |
| Applications | Create/update, setup/build/start/stop/redeploy/delete, releases, logs, health, deployments, environment, webhook and boot automation |
| Docker | Containers/status/stats/logs/lifecycle/removal; image list/pull/run/build/removal; Compose build/deploy; volumes/networks list/removal |
| Databases | Provision/lifecycle/removal, engine health and telemetry, queries, users/grants, backups/restore/schedules, schema/table/row CRUD |
| Vector databases | Qdrant install, collections, diagnostics, snapshots and restore through existing workflows |
| Networking | Domains/TLS, Nginx status, load balancer deployment/configuration/backends/logs |
| Files and commands | Directory/content reads, text writes, mkdir/rename/chmod/delete/archive, bounded non-interactive command dispatch |
| Cron | Create/edit/pause/resume/delete/run/cancel, schedule preview, history/output |
| Firewall | State, installation, enable/disable/reload/reset, policies, rule CRUD/bulk, backups/restore, schedules and logs |
| Server tools | Processes/termination, supported service status/restart, software installation, operating-system users |
| Global administration | Dashboard user creation/revocation and GoPulse host power actions, behind explicit global scopes |

Capabilities are bounded by the existing services. For example, file management on localhost and engine-specific explorer operations retain their existing backend limitations. Tools do not make an unsupported engine operation work by bypassing the service layer.

Large binary project uploads, database import/download files, and full configuration transfer remain existing dashboard workflows. MCP is not used to inject multi-gigabyte artifacts into model context. Non-interactive shell dispatch can run an explicitly authorized server-side workflow; interactive terminal/WebSocket sessions are not MCP tools.

## Connect a client

1. Open **Agents → Connect agent** in GoPulse.
2. Set a name, expiry, access mode, servers, and permission scopes. Prefer short-lived, narrowly scoped credentials.
3. Create the credential and save it in a local secret store or the environment used to launch the client. The dashboard shows it only once; closing the dialog clears the value.
4. Copy the appropriate configuration. Do not put an actual bearer secret in committed configuration or a URL.

Claude Code `.mcp.json`:

```json
{
  "mcpServers": {
    "gopulse": {
      "type": "http",
      "url": "https://gopulse.example.com/api/mcp",
      "headers": {
        "Authorization": "Bearer ${GOPULSE_AGENT_TOKEN}"
      }
    }
  }
}
```

Codex `~/.codex/config.toml`:

```toml
[mcp_servers.gopulse]
url = "https://gopulse.example.com/api/mcp"
bearer_token_env_var = "GOPULSE_AGENT_TOKEN"
startup_timeout_sec = 20
tool_timeout_sec = 60
```

Use `/mcp` to inspect the connection in either client; Codex also offers `codex mcp list`. Alternatively, Codex can register the HTTP server with `codex mcp add gopulse --url https://gopulse.example.com/api/mcp --bearer-token-env-var GOPULSE_AGENT_TOKEN`. These examples follow the [Codex MCP configuration documentation](https://developers.openai.com/codex/mcp/) and [Claude Code's environment-reference support](https://code.claude.com/docs/en/mcp#environment-variable-expansion-in-mcpjson).

For a local instance use `http://127.0.0.1:<your-port>/api/mcp`; for remote access use HTTPS. Configure the environment before starting the desktop/CLI process. A GUI launched independently may not inherit the shell environment. The UI preserves the current deployment's port and normalizes a local `0.0.0.0` display address to `127.0.0.1`.

This version uses custom bearer credentials. `codex mcp login` and Claude's browser OAuth login apply to servers implementing OAuth; GoPulse does not yet provide OAuth discovery/authorization endpoints. Use the supplied bearer configuration instead.

## Request lifecycle and reliability

1. Authenticate the bearer hash and verify the credential has not expired or been revoked; resolve the stable owner ID and current access.
2. The SDK negotiates the protocol and exposes permitted tools.
3. Validate the selected tool's schema, requested server, linked backends, permission scopes, and access mode. Persist a request/audit event before execution.
4. Read tools call the authenticated API/service and return structured results. Structured credential fields are redacted unless a designated secret-reading tool was explicitly permitted. File/log content requires its own scopes; it is not automatically scrubbed of all sensitive text.
5. Write tools return an operation record (`data.operation_id`, `data.status`). In supervised mode, approve or reject the operation from **Activity → My operations and approvals**. Agents cannot approve themselves.
6. Before work starts, recheck the original credential, parent rights, server scope, and linked backend scope. Dispatch with normal resource-ownership middleware. Results and final errors persist in the operation record.
7. Poll `get_operation_status` at a sensible interval. Approved operations may take several minutes. Database/cron job invocations may additionally return their own run IDs; inspect the domain history when the action starts background work.

Use a stable `request_id` for every write. Replaying the same ID and payload returns the same persisted operation, including after the first execution completes. Reusing the ID for a different payload fails. Deduplication is namespaced by credential and persisted in SQLite; it is not a promise that remote commands themselves are transactional or exactly-once across transport loss.

Commands are never retried automatically. On failed/interrupted operations, inspect remote state before submitting a new ID. Revocation denies new calls and prevents queued work from dispatching; already running work can finish. Pending approval callbacks exist in process memory and become Interrupted after restart rather than replaying unknown side effects.

Captured API/stream output is bounded and may be marked truncated. Large binary transfer should use the existing upload/download workflows. Operation concurrency is bounded; capacity conflicts mean the action was not started. Application/database state transitions, validation, backup semantics, and supported software remain those of the existing service implementations.

## API and persistence

Browser-only management endpoints:

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET | `/api/agents` | Current user's credential metadata; never raw secrets/hashes |
| POST | `/api/agents` | Create credential; one-time secret response, `Cache-Control: no-store` |
| DELETE | `/api/agents/{agentID}` | Revoke an owned credential |
| GET | `/api/agents/configuration` | Permitted servers/scopes, endpoint, access modes, tool category counts |
| GET | `/api/agents/audit` | Latest 100 audit events for the current user |
| POST | `/api/operations/{operationID}/approve` | Browser approval; special human-approval header required |
| POST | `/api/operations/{operationID}/reject` | Browser rejection; same session/header policy |

Creation accepts `name`, `mode`, `permissions`, `server_ids`, `all_servers`, and `expiry_hours` (1–720). Unknown scopes, inaccessible servers, invalid modes, oversized bodies, and attempts to grant global administration without global server access fail before credential issuance.

`agent_credentials` stores a unique SHA-256 hash of a cryptographically random 256-bit secret, stable owner ID, scopes, mode, expiry, revocation, and last use. It lives alongside existing tables in the normal GoPulse SQLite data directory. Issued secrets and credential records are intentionally not part of portable configuration exports: create fresh credentials after a migration. Audit rows retain credential identity without request secrets. Existing audit/operation tables migrate in place; histories are not dropped.

## Trade-offs and next phases

- A layered monolith and in-process API bridge preserve validation, service behavior, and ownership checks. This is preferable to maintaining duplicate SSH/Docker implementations per agent client. A fixed registry must be updated deliberately when new dashboard APIs are added.
- Native bearer configuration is immediately useful to Claude Code/Codex. A public multi-tenant connector should add OAuth 2.1 with PKCE, resource/audience binding, consent, and discovery, preferably through an existing identity provider. That is a separate phase, not an advertised capability of this release.
- Stateless HTTP is easy to deploy behind the existing reverse proxy. Durable operation tracking carries long-running progress across client reconnects; approvals themselves are intentionally not resumed automatically after a GoPulse restart.
- Full control satisfies autonomous operation but grants consequential power. Supervised mode supplies a human boundary for every write. Neither annotations nor prompts alone enforce security; the server's checks do.
- A later artifact API can support resumable authenticated upload/download references, and operation cancellation can become a first-class control rather than relying on the existing domain cancellation/lifecycle tools.
- At a larger deployment scale, move operation workers into a durable queue and isolate execution per server. SQLite and the single control plane remain consistent with the present application; no speculative microservices were introduced.

## Validation

Tests exercise a real MCP SDK client against a temporary HTTP server, temporary SQLite databases, and fake resource handlers. They cover discovery, schema validation, bearer identity/expiry/revocation, parent access changes, server scope, read-only enforcement, explicit full control, supervised approve/reject, REST/self-approval bypass denial, output limits, owner namespace isolation, durable request deduplication, and generated Claude/Codex configuration without inline secrets. Managed-server browser verification performs only reads and opens/cancels setup dialogs; no real agent credential or infrastructure mutation is created while testing.

Completed checks: `go test ./...`, focused MCP/agent/operation race tests, `go vet` on the changed backend packages and server, all 144 frontend tests, JavaScript syntax checks, and a Linux build with CGO disabled. Desktop and 390-pixel mobile browser checks verified the Agents page, capability catalog, audit empty state, mode controls, and scrolling connection dialog. Claude Code/Codex setup follows their official configuration formats; protocol interoperability was tested with the official SDK client, rather than connecting a live client with real server privileges.
