# Serverless Functions

GoPulse Functions runs short Python and JavaScript handlers in disposable Docker containers on a registered Linux server. Create a draft in **Server → Functions**, deploy it, then invoke the deployed version with a test event, a cron schedule, or a dedicated authenticated HTTP trigger. No AWS account is required.

## Setup and first invocation

The managed host needs Linux, Bash, GNU `timeout`, Docker Engine, a verified SSH host key, and an SSH account allowed to use Docker. Docker must report memory, swap, CPU quota and PID limits, with its default seccomp profile enabled. Deployments and invocations fail closed when those protections are unavailable; the host capability check is repeated before every invocation. Docker Hub access is required during deployment to pull the allowlisted runtime. Invocations use the cached immutable image ID with `--pull=never`.

1. Open **Functions → Create function** on a managed server.
2. Choose **Python 3.13** or **JavaScript / Node.js 24**. Start with the supplied example. Save the draft.
3. Select **Deploy draft**, then open its deployment logs. The previous deployed version remains active if validation fails. Deployment validates syntax, not application behavior; test an event before relying on it.
4. Open the function, enter a JSON event, and select **Run function**. Returns are JSON records in the invocation logs alongside stdout and stderr.
5. Optional: edit the draft to enable a cron or HTTP trigger, then deploy again. For HTTP, create a bearer token in the function details and copy it immediately. It is shown once; rotation invalidates the previous token.

Python uses `def handler(event, context)` or `async def handler(event, context)`. JavaScript uses CommonJS `module.exports.handler = async (event, context) => { ... }`. Only the runtime's standard library is available. Context contains `invocation_id`, `function_id`, `version_id`, and `trigger`. Each invocation starts fresh. There is no package installation, environment-secret manager, outbound network access, persistent disk, or provisioned warm container in this release.

```python
def handler(event, context):
    print('Invocation:', context['invocation_id'])
    return {'message': 'Hello, ' + event.get('name', 'World') + '!'}
```

```javascript
module.exports.handler = async (event, context) => {
  console.log('Invocation:', context.invocation_id);
  return { message: `Hello, ${event.name || 'World'}!` };
};
```

A trigger request returns **202 Accepted** with an invocation ID, not the function's return value. View results and logs through the authenticated dashboard. Use the exact endpoint shown there:

```sh
curl --request POST 'https://YOUR-GOPULSE/api/function-triggers/SERVER_ID/FUNCTION_ID' \
  --header 'Authorization: Bearer YOUR_FUNCTION_TOKEN' \
  --header 'Content-Type: application/json' \
  --data '{"name":"World"}'
```

Protect HTTP tokens and use HTTPS. Tokens cannot read code, retrieve logs, deploy or cancel functions. GoPulse stores only their SHA-256 hash. Trigger calls also recheck the current owner's account permissions and server scope. Pausing triggers disables both HTTP and scheduled execution immediately; manual runs remain available. Resuming schedules starts from the next future slot.

## Requirements and operating envelope

This is a self-hosted feature for trusted operators, not a hostile multi-tenant code hosting service. Containers share the managed host's kernel. Place functions on dedicated worker servers when stronger separation from production workloads is needed.

Functional scope: draft editing; syntax validation; immutable deployed snapshots; manual/HTTP/five-field cron triggers; deployment and invocation history; live bounded logs; cancellation; pause/resume; concurrency and resource limits; durable cleanup after interruption.

Assumptions: one GoPulse control-plane process with a local persistent SQLite volume; approximately 1–20 concurrent operators; administrative workloads rather than public high-volume compute. There is no DAU-based autoscaling or invocation SLA. Latency includes SSH setup and container cold start; there is no guaranteed warm-start latency. Availability depends on the controller, SSH, Docker and the selected host. Schedules run only while GoPulse is running; downtime and capacity misses are skipped, never replayed.

| Limit | Enforced value |
| --- | --- |
| Source | 1–32768 UTF-8 bytes per draft |
| Event | JSON, maximum 32768 bytes; not separately persisted |
| Function timeout | 1–900 seconds (default 30) |
| Memory | 128–2048 MiB (default 256); swap disabled |
| CPU | 100–2000 millicores (default 500) |
| Concurrent invocations | 1–4 per function (default 1) |
| Execution workers | 8 across the controller, maximum 4 on one server; deployments consume a worker |
| Temporary filesystem | 64 MiB tmpfs, charged to container memory |
| Processes / file descriptors | 64 PIDs, 128 file descriptors |
| Logs | 64 KiB combined stdout/stderr; additional output discarded while streams continue draining |
| History | Latest 100 attempts per function and up to 30 days; pending cleanup records preserved |
| Versions | Latest 20 plus deployed/referenced versions; pruning runs each minute |
| Functions | Maximum 100 per server |
| Deployment deadline | 250 seconds, including a 180-second pull and 30-second syntax check |

These are ceilings, not reserved host capacity. Size worker servers for their aggregate limits: four 2 GiB functions can consume 8 GiB plus Docker/OS overhead. At full log retention, 100 functions can store about 625 MiB of logs, plus source versions and metadata. SQLite WAL and filesystem backups require additional space. Normal dashboard polling is every three seconds only while the Functions view is active.

## Architecture and data flow

```text
Browser dashboard ── session + RBAC ──┐
External caller ── per-function token ├─ Chi handlers → FunctionService
Cron tick ── current owner RBAC ──────┘                    │
                              SQLite drafts / versions / runs / token hashes
                                                         │
                                      8 workers, max 4 per server
                                                         │ verified SSH
                                      managed Linux host + GNU timeout
                                                         │
                        allowlisted Docker image pinned by sha256 image ID
                        non-root, read-only rootfs, network=none, no host mounts
                        memory/swap/CPU/PID/fd limits, tmpfs, bounded output
```

The existing Go monolith owns authorization, scheduling and orchestration. No additional control-plane service, Redis, queue, AWS dependency, or frontend build system is needed. SQLite transactions protect claims and deployments; a service mutex serializes scheduling/configuration changes in the supported single-controller model. Independent workers execute remote operations outside that mutex. A separate cleanup loop prevents an offline host from delaying schedules.

Deployment lifecycle:

1. Authorize both `server:write` and `docker:write`, plus server access. Save source and limits as a new draft revision.
2. Claim a durable deployment run, reserve worker capacity, and snapshot the revision.
3. Read Docker capabilities, pull one allowlisted runtime (`python:3.13-alpine` or `node:24-alpine`), and resolve its immutable image ID.
4. Send the source through SSH stdin into an isolated syntax-check container. User source is never interpolated into host shell commands or written to host files.
5. Atomically insert a version and activate it, leaving any concurrently edited draft intact. Store the source SHA-256, runtime image ID, limits and trigger configuration in that version.
6. Record deployment output/status and remove its container. Failed validation leaves the earlier deployed version available.

Invocation lifecycle:

1. Authenticate a dashboard operator or validate a function-specific HTTP bearer token. HTTP and cron invocations check that triggers are enabled and not paused; all executions recheck a current user's permissions.
2. Snapshot the deployed version. Reject excess manual/HTTP capacity with 429 or overlap/cleanup conflicts with 409; no unbounded queue is created. Scheduled conflicts are recorded as skipped and the next slot advances.
3. Persist the running receipt, run the pinned image over a dedicated SSH connection, and send code/event/context via stdin.
4. A wrapper calls the handler and writes its JSON return to stdout. Both stdout and stderr stream into a concurrency-safe bounded log writer; snapshots persist once per second.
5. Record exit status, elapsed time, errors and final logs. Cancellation closes the SSH connection and triggers a fresh cleanup attempt. A host-side watchdog and shell exit/signal traps remove the invocation container on normal timeout/exit. Remote transport loss is an uncertain outcome until cleanup is confirmed.
6. If cleanup fails, persist `needs_cleanup`, block additional executions for that function, and retry each minute. Startup marks previously running attempts interrupted and retries cleanup; no invocation is automatically replayed.

Retries by HTTP callers can produce additional invocations; this release does not provide idempotency keys or exactly-once execution. Avoid automatically retrying calls with uncertain outcomes for non-idempotent work. Exit 124 is a watchdog timeout; exit 137 indicates forced termination, which can include timeout or memory exhaustion. The UI therefore labels these attempts “Timed out / killed”.

## API and persistence

All dashboard endpoints are under `/api/servers/{serverID}/functions` and require session authentication and server scope. Writes additionally require **both** `server:write` and `docker:write`, rechecked against the persisted user account. Server-scoped viewers can inspect source, versions and logs; do not put credentials in source, test events or console output. Machine agents using only agent-scoped credentials cannot deploy functions through this user-owned scheduler; use a dashboard operator session for management and a function token for invocations.

| Method / suffix | Behavior |
| --- | --- |
| GET / | List drafts and active versions |
| POST / | Create draft from `FunctionSpec` |
| PUT /{id} | Replace draft; increment revision |
| DELETE /{id} | Delete function, versions, token hash and history; reject active/pending-cleanup attempts |
| POST /{id}/deploy | Deploy current draft; return 202 run receipt |
| POST /{id}/run | Invoke deployed version with raw JSON event; return 202 receipt |
| POST /{id}/pause | `{ "paused": true }` or false; immediate automatic-trigger switch |
| POST /{id}/http-token | Rotate token; return plaintext once and trigger path |
| GET /{id}/versions | Most recent 20 immutable versions |
| GET /{id}/runs | Most recent 100 attempts, including logs |
| POST /{id}/runs/{runID}/cancel | Cancel active attempt |

`POST /api/function-triggers/{serverID}/{functionID}` uses only a function bearer token, independently of dashboard cookies. The session-auth exemption matches only that exact UUID-shaped POST path. Origin protection and existing API rate limits still apply. Invalid credentials return 401; disabled/paused HTTP returns 400. Responses do not expose code, logs or function return values.

`FunctionSpec` fields are `name`, `runtime` (`python` or `javascript`), `code`, `timeout_seconds`, `memory_mb`, `cpu_millis`, `concurrency`, `schedule`, `timezone`, `schedule_enabled`, and `http_enabled`. Unknown fields, non-object configuration bodies, multiple JSON values and oversized bodies are rejected.

SQLite schema is migrated at startup:

- `functions`: ID, server foreign key, hash-only HTTP token, JSON draft and active-version snapshot. Server deletion is restricted until functions are removed.
- `function_versions`: immutable ID, function foreign key and JSON snapshot; indexed by function.
- `function_runs`: invocation ID, function/server IDs, kind, status, cleanup flag and JSON output metadata. Function/status index supports claims and history; a partial unique index prevents concurrent deployments of the same function.

There is no cache: code and configuration are authoritative in SQLite, and request-local snapshots prevent stale draft execution. All successful deployment activation is transactional. Keep `gopulse.db` and its consistent WAL state in control-plane backups; existing JSON configuration exports and Remote Backups of managed databases **do not include function state**. No remote source workspace needs restoring. The runtime image must still be available by its recorded image ID; redeploy a saved draft if the host/image cache is lost. Runtime image cache maintenance is left to operators so other workloads' images are not removed unexpectedly.

## Reliability, security and trade-offs

Resource flags follow Docker's [resource constraint documentation](https://docs.docker.com/engine/containers/resource_constraints/) and [container run reference](https://docs.docker.com/reference/cli/docker/container/run/). Each invocation uses a dedicated SSH connection with known-host verification. Containers run as UID/GID 10001, drop all capabilities, keep default seccomp, set no-new-privileges, have no published ports, Docker socket or host volumes, and use no Docker log file driver. The runtime pulls can access the network on the host; handler containers cannot. Shell commands accept only fixed runtimes, validated UUIDs, numeric limits and validated image hashes.

Cleanup checks the exact UUID ownership label before deleting a container and refuses to interpret Docker/SSH failure as proof of absence. Pending cleanup survives restart, log pruning and failed deletion. Very long controller/host outages can delay cleanup: inspect the run and host before manually resolving it. Protect SSH/Docker access as privileged infrastructure access. A malicious kernel exploit is outside the isolation guarantees of ordinary containers.

This design scales to zero containers between runs and bounds controller work without introducing a broker. It intentionally uses cold starts and fail-fast admission rather than maintaining idle capacity or accepting an unbounded backlog. A monolith and SQLite keep installation simple but do not support active-active controllers or automatic cross-host failover. There is no request load balancer between workers; the function's selected server is authoritative. Function invocations are stateful POSTs with no CDN caching; existing static dashboard assets can be cached separately.

Future work can introduce a durable broker with idempotency keys, per-tenant quotas, dedicated worker agents, stronger VM-based isolation, package bundles, secret injection, controlled egress, result objects separate from console output, and traced invocation metrics. Those changes require explicit trust, delivery and capacity policies; they are not implied by the current Lambda-like workflow.

## Verification

`make check` verifies formatting, vetting, Go tests, frontend tests and an optimized embedded-assets build. `make test-race` checks concurrency. Function tests cover real local SSH transport, both real language interpreters with Docker command fixtures, syntax/native exceptions, stdin separation, container flags, bounded output, immutable deploys, edits during deploy, RBAC and scoped reads, hashed token rotation, origin checks, trigger pause/revocation, schedules, admission limits, live log persistence, cancellation, retention, restart and cleanup ownership/failure handling.

The ordinary suite does **not** run a Docker daemon or prove kernel cgroup/network enforcement. An opt-in integration test exercises the production backend against real Docker on a disposable Linux host with GNU timeout:

```sh
GOPULSE_TEST_FUNCTION_DOCKER=1 go test ./internal/services -run '^TestFunctionsLiveDockerIsolation$' -count=1 -timeout=6m
```

That test pulls a runtime image and verifies non-root/read-only/no-network execution, watchdog timeout and memory failure. Never point it at a production workload. Live Docker testing was skipped during this change per the user's earlier preference.
