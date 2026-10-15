# Cron jobs

Open a server and choose **Cron jobs** under Server tools. Jobs belong to that server and use the same access restrictions as its other resources.

## Schedule and manage a job

1. Choose **Create job** and give it a name.
2. Select a frequency: every few minutes, hourly, daily, weekly, or monthly. Pick the time and day using the visual controls. The timezone defaults to your browser's IANA timezone and can be changed.
3. Review the plain-language summary and the next five execution times. Advanced opens the equivalent five-field cron expression; custom expressions support ranges, lists, steps, and named weekdays/months.
4. Enter the shell command, optional absolute working directory, and timeout (1–3600 seconds). Use absolute executable paths. Keep secrets in files on the server rather than in commands visible to dashboard users.
5. Choose whether to enable the schedule and save.

History and Edit are visible on each job. Manage contains Pause/Resume, Run once, Cancel run (while running), and Delete. Running jobs cannot be deleted. Editing changes future invocations; an active run keeps the original command snapshot. Pause only disables future scheduled invocations. Run once works even on paused jobs and asks for confirmation.

Execution history shows status, trigger, actor, timestamps, duration, exit code, and combined stdout/stderr. Inspect opens output and supports copying on HTTPS and plain HTTP. The latest 100 runs per job are retained; output is capped at 64 KiB per run and is available when the command finishes. History refreshes every five seconds while this section is visible.

## Execution behavior

- GoPulse is the scheduler. It must stay running and be able to reach the selected server. This feature does not read, replace, or install operating-system crontabs. Run one GoPulse scheduler process per SQLite data directory.
- Schedules and run history are stored in `gopulse.db` in the existing GoPulse data directory. The regular configuration export includes job definitions. Imports validate jobs and restore them **paused**; execution history is not copied in configuration exports. Enable restored jobs explicitly after checking the destination server.
- Remote commands run through an isolated SSH connection as the server's configured SSH user using `/bin/sh`. Trusted SSH host keys are required. GNU `timeout` must be available on remote Linux servers; execution fails without it rather than running without a deadline. The remote watchdog sends TERM at the deadline and KILL after five additional seconds. Cancellation or a lost SSH connection can leave an uncertain remote outcome: verify side effects before rerunning. Commands that intentionally daemonize are outside watchdog guarantees.
- Local Unix jobs run as the GoPulse operating-system user. Empty working directories use that account's home directory. Local timeouts/cancellation kill the command's process group. Local Windows execution is unsupported; remote Linux jobs remain supported.
- A job never overlaps itself. The scheduler has eight concurrent workers. Scheduled invocations are recorded as skipped when the previous execution is active or capacity is exhausted; manual invocations return a conflict and may be retried deliberately.
- Missed schedules during downtime are advanced to the next future slot. Previously running jobs become Interrupted after an unclean restart. Failed, timed-out, or uncertain runs are never retried automatically.
- Calendar schedules use the chosen IANA timezone, including daylight-saving transitions. A nonexistent clock time is skipped; repeated clock times can occur twice. Monthly schedules on days 29–31 skip months without that day. Standard cron day-of-month/day-of-week OR semantics apply to advanced expressions.

## Permissions and API

Viewing jobs/history requires access to the server. All mutations require the dedicated **`cron:write`** permission (or administrator/wildcard access). This permission allows shell-command execution and should be granted to trusted operators. Existing users do not automatically gain it. It is available in Team & access when creating users.

All endpoints are scoped under `/api/servers/{serverID}/cron`:

| Method | Endpoint suffix | Action |
| --- | --- | --- |
| GET | `/` | List jobs and latest run metadata |
| GET | `/preview?schedule=…&timezone=…` | Validate and preview five upcoming runs |
| POST | `/` | Create a job |
| PUT | `/{jobID}` | Update a definition |
| PUT | `/{jobID}/enabled` | Enable or pause (`{"enabled":true/false}`) |
| DELETE | `/{jobID}` | Delete an inactive job and its history |
| POST | `/{jobID}/run` | Run once; returns 202 and the run record |
| POST | `/{jobID}/cancel` | Request cancellation; returns 202 |
| GET | `/{jobID}/runs?limit=25&offset=0` | Paginated history (max limit 100) |
| GET | `/{jobID}/runs/{runID}` | Inspect a run and its output |

Create/update accepts `name`, `schedule`, `timezone`, `command`, `working_dir`, `timeout_seconds`, and `enabled`. Server, job, creator, timestamps, and run status cannot be supplied through the editor API. Unknown fields and oversized JSON are rejected. There is a limit of 500 jobs per server.

The parser uses [robfig/cron's standard five-field syntax](https://pkg.go.dev/github.com/robfig/cron/v3). Timezone data is embedded so scheduling also works in minimal deployment images.

## Validation

Unit/integration tests use temporary SQLite databases, fake executors, a local mock SSH protocol server, and harmless local shell commands. They cover schedule conversion, timezone previews, validation, authorization, scope isolation, concurrent claims, worker limits, immutable snapshots, pause behavior, cancellation, timeout, output limits, retention, recovery, and paused backup import. Browser checks on managed servers are read-only and never submit or execute a job.
