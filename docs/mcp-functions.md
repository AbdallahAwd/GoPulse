# MCP gateway for serverless functions

GoPulse exposes selected serverless functions through its existing authenticated `/api/mcp` endpoint. No AWS service or separate MCP server process is needed. Assistants discover one tool per selected function, submit an event, and retrieve a durable operation containing the JSON return value, logs, execution status, and invocation ID.

## Connect and run

1. Open **Server → Functions**, create and deploy a function, and test it with a manual event.
2. Open its details and choose **Expose to MCP**. This immediate switch is separate from draft editing. New functions and existing installations default to hidden. The details panel shows the MCP endpoint and stable tool name.
3. Open **Agents → Connect agent**. Select the managed server, grant `server:write` and `docker:write`, and choose **Supervised** or **Full control**. Prefer supervised access for interactive use. Read-only agents cannot invoke functions. Only opaque credentials from this page are supported for function tools; legacy MCP JWTs cannot discover or invoke them.
4. Configure a Streamable HTTP client using the displayed endpoint and `Authorization: Bearer <agent credential>`. Use HTTPS outside localhost. Existing [Codex](examples/codex.mcp.toml) and [Claude Code](examples/claude-code.mcp.json) configuration examples apply. Keep credentials in environment variables and outside source control.
5. Discover tools, call the selected function with `event` and a stable `request_id`, then poll `get_operation_status`. Supervised calls remain `awaiting_approval` until their owner approves them in the dashboard's Activity view. Full-control calls start automatically within their granted scopes.

These credentials also authorize existing infrastructure tools covered by the same permissions. Selection controls the function catalog globally for all qualifying agents on that server; it is not a per-credential function allowlist. Do not grant these broad write scopes to an assistant that should only inspect infrastructure.

Example tool name: `function_9c35fc46017e4989a10fc205377b9ba5`. It uses the function UUID without hyphens, so display-name changes do not change the tool identity. User-supplied names appear only as descriptive data.

After the client's normal MCP initialization and protocol negotiation:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "function_9c35fc46017e4989a10fc205377b9ba5",
    "arguments": {
      "event": {"name": "World"},
      "request_id": "greeting-order-123"
    }
  }
}
```

The call returns an operation record under `structuredContent.data`. Preserve its `operation_id`. Poll at most every two seconds to stay within the MCP credential's 60-request/minute budget:

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_operation_status","arguments":{"operation_id":"RETURNED_OPERATION_ID"}}}
```

When `data.status` is `completed`, `data.result` contains:

```json
{
  "invocation_id": "INVOCATION_UUID",
  "function_id": "FUNCTION_UUID",
  "version_id": "DEPLOYED_VERSION_UUID",
  "status": "succeeded",
  "duration_ms": 84,
  "result": {"message": "Hello, World!"},
  "logs": "Invocation: ...\n...",
  "logs_truncated": false,
  "exit_code": 0,
  "cleanup_pending": false
}
```

The operation statuses `failed`, `interrupted`, `expired`, and `rejected` are terminal. A failed handler produces a failed operation with invocation/log metadata when available. An authorization failure returns no handler output. A successful operation may still report `cleanup_pending`; existing function cleanup protection prevents further execution until cleanup succeeds. MCP submission is asynchronous: an accepted call is not proof that the handler succeeded.

## Requirements and architecture

This extends the existing single-process Go/Chi service, SQLite stores, embedded dashboard and official MCP Go SDK. It adds no network service or external infrastructure dependency. A typical installation has one controller, up to 100 functions per managed server, and operator/assistant traffic rather than public high-volume invocation. There is no measured latency or availability SLO. Container startup and remote execution dominate latency.

```text
Assistant + scoped bearer credential
  → /api/mcp → current user/server permissions + origin/rate checks
  → dynamic catalog: selected + deployed + unpaused functions
  → tools/call → schema/event validation + durable audit
  → SQLite operation (idempotent request, optional human approval)
  → refreshed credentials + pinned deployment + exposure checks
  → FunctionService → SSH → isolated Docker handler
  → independent bounded JSON result + bounded logs → SQLite
  → get_operation_status → same owner/credential + current access checks
```

A per-function `mcp_enabled` field is persisted in the existing functions JSON record in SQLite; omitted historical fields default to false. The existing function run record gains an optional `result` JSON field. No table rewrite, backfill, token duplication, or new credential format is needed. Existing function/server indexes support lookups; existing operations enforce request-id deduplication and resource serialization. Function operations have a sixteen-minute worker deadline to cover the maximum fifteen-minute invocation plus setup/cleanup. Other operations retain their eight-minute deadline.

The stateless Streamable HTTP handler builds a fresh permission-filtered tool catalog for each request. The pinned SDK's normal initialization/version negotiation applies; its default protocol is `2025-06-18`, with supported legacy revisions handled by that SDK. This change does not implement the newer `2026-07-28` stateless-core wire protocol. Use a client that supports negotiation to the existing protocol. Clients should refresh discovery after exposure, pause, or deployment changes; no long-lived tool-list notification subscription is maintained.

## Permission and execution boundaries

- Every discovery and invocation requires an unexpired, unrevoked opaque agent credential narrowed by its owner's current rights, `server:write`, `docker:write`, and access to the managed server. The user's current persisted rights are checked by the function service as well.
- Discovery never returns source code or function bearer tokens. Function selection is only editable through the authenticated dashboard management API: `POST /api/servers/{serverID}/functions/{functionID}/mcp` with `{"enabled":true}` or `{"enabled":false}`. The route requires both write permissions and server scope. Agents cannot call it directly, and no MCP management tool exposes it.
- The event must be a JSON object up to 32 KiB. The outer arguments schema is closed and requires a nonempty `request_id` up to 128 characters. Arbitrary shell commands, runtime flags, and code uploads are unavailable through these function tools.
- The accepted operation captures the current immutable deployment. Pending approval fails if the deployment changes, exposure is removed, triggers are paused, or permissions expire. Draft-only edits do not change the captured deployed version. Future calls use the deployment active when they are accepted, even if a client cached an older description.
- The worker refreshes credential, owner, permission, server, exposure and version checks before starting, every second while running, and before releasing final output. A failure requests cancellation of the underlying invocation; existing SSH cancellation, host watchdogs and durable cleanup apply. Removal is not an instantaneous remote kill guarantee during network failure.
- Result polling requires the same operation owner and originating credential, current server/write access, and an enabled, deployed, unpaused function. Another credential owned by the same user cannot read this operation through MCP. Disabling exposure blocks subsequent MCP result reads as well as new calls.
- `request_id` is deduplicated per credential using the existing durable operations mechanism. Reusing it with a different event or deployment is rejected. On restart, interrupted operations are not replayed. Approval work does not survive restart. Inspect interrupted work before issuing a new ID.
- Existing global eight-worker/four-per-server function admission, per-function concurrency, network isolation, CPU/memory/PID/timeout limits, and cleanup blocking remain in force. MCP operation admission can further restrict concurrency; it does not create an unbounded queue.

## Results, logs and retention

Python and JavaScript wrappers emit a bounded JSON result record independently of console logging. Result collection continues even after the 64 KiB console log limit. The wrapper envelope must fit 32 KiB; invalid, missing, duplicate, unserializable or oversized results fail execution. JSON null, scalars and arrays are valid return values, although MCP input events are objects. Existing historical invocations do not gain a result retroactively. The dashboard renders return values as text.

Function history uses the existing latest-100/up-to-30-day retention. MCP results are also copied into existing durable operation records, whose lifecycle is separate; pruning function history does not erase those copies. Approval previews persist the event, with existing structured secret-key redaction; audit entries record identities/actions/status rather than raw bodies. Existing redaction masks known credential fields in structured MCP output. Arbitrary values embedded in log strings are not subject to content-level DLP. Treat events, logs, descriptions and results as untrusted data, and do not put secrets into them.

Back up the consistent control-plane SQLite database/WAL for functions and operations. Existing JSON exports and managed-database Remote Backups do not include these controller records. The architecture supports one active controller; active-active deployment would require shared admission, distributed locks and a durable job queue. A separate gateway/microservice or Redis cache would add authorization-invalidation and coordination complexity without improving the current operating envelope.

## Verification

Automated tests cover selection/default hiding, server/permission filtering, read-only/legacy restrictions, agent management-route denial, schema and size validation, result isolation, credential revocation, approval gating and rechecks, deployment replacement, retry deduplication, error results, result/log limits, native Python/JavaScript wrappers, and a real Streamable HTTP SDK client over a local HTTP server. Existing SSH tests exercise the production wrappers through a local SSH fixture and Docker command fixture. Frontend tests cover the exposure controls and safe rendering.

Run `make check` and `make test-race`. The real Docker isolation suite remains opt-in as documented in [Functions](functions.md); it was not run because the user previously chose to continue without live Docker tests. Automated fixtures do not prove kernel isolation or production SSH connectivity. No real server is modified by the gateway tests.

Protocol reference: [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and [Streamable HTTP specification, 2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports).
