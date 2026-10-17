# Metrics history, alerts, and webhooks

Open a server and select **Alerts** to create CPU, memory, disk-capacity, or metrics-availability rules. Administrators and users with `server:write` on that server can manage rules, silence notifications, inspect the signing key, and queue a test notification. Assigned viewers can inspect state and event history; webhook destinations and signing keys are redacted.

## Persistent history

The existing Overview charts now read SQLite-backed summaries. Select **1h**, **6h**, **24h**, **7d**, or **30d**. History survives a GoPulse restart. No additional database, agent, or service is required.

- Each successful collection contributes CPU usage, memory usage, the highest monitored filesystem usage, and cumulative network byte counters to a one-minute bucket. CPU, memory, and disk percentages are averaged within the bucket.
- Local collection excludes read-only mounts, device/proc/sys/cgroup pseudo-filesystems, squashfs, ISO images, and UDF images; remote Linux `df` excludes squashfs, ISO, and UDF. Individual writable disks are not selectable in this release.
- Collection failures are recorded separately. A minute without successful readings is marked unavailable; it is not presented as zero usage. Missing buckets also appear as gaps in charts.
- Larger query ranges aggregate buckets in SQL to return at most 1000 points. Percentages are weighted by successful sample count. Network throughput is estimated from the last successful cumulative counters per bucket; counter resets cannot produce negative speeds.
- Metrics and incident/delivery events are retained for 30 days. Expired rows are removed at startup and hourly. Rules remain until deleted. SQLite can reuse deleted space; pruning does not shrink the database file automatically.
- Storage is in `$GOPULSE_DATA_DIR/gopulse.db`, or `~/.gopulse/gopulse.db` by default. Allow roughly 43,200 metric rows per continuously monitored server for a full 30 days, plus rules and events. Capacity across large fleets has not been benchmarked.

## Rule evaluation

Rules evaluate raw collection results, independently of minute averages. Usage must stay **strictly above** the threshold for the configured duration. Availability rules breach when GoPulse cannot obtain valid host metrics; they do not independently probe application HTTP endpoints or prove the host itself is offline. A zero-second duration fires on the first matching observation.

| State | Meaning |
| --- | --- |
| Healthy | The latest valid observation does not breach the rule |
| Pending | The rule is breached but its duration has not elapsed |
| Firing | The duration has elapsed and an incident is open |
| Unknown | No valid usage reading is available |
| Disabled | The rule is paused |

Missing data resets a pending usage duration. A collection gap longer than the larger of 30 seconds or three polling intervals also resets it. The watchdog marks stale usage rules unknown and evaluates missing collection availability every five seconds. No watchdog reading is fabricated in metric history. Existing incidents remain open during missing data and recover only after a valid non-breaching observation.

Evaluator state survives restarts. A short restart can preserve a pending duration; an interval beyond the gap limit restarts that duration. Saving a rule resets its evaluator state without inventing a recovery event. Deleting a rule also deletes its event history and queued notifications. Removing a server cascades to its metric history, rules, and events.

Choose **Silence 1h** during maintenance, then **End silence** to resume earlier. Evaluation and incident history continue while silenced. If an incident begins during silence and is still firing afterward, the next evaluation queues a firing notification. Silenced events are recorded but not delivered. Explicit test notifications bypass silence. The API also supports silences up to seven days.

GoPulse must be running to collect, evaluate, and deliver. Run one active GoPulse process per database. In-process monitoring cannot notify you about a complete outage of GoPulse itself; use a separate uptime monitor for the dashboard.

## Webhook delivery

Enter a public **HTTPS** URL when creating or editing a rule. Leave it empty for dashboard-only incidents. The receiver must accept GoPulse JSON; provider-specific Slack/Discord payload formats require an adapter. No AWS account is required.

After saving, edit the rule to view its generated signing secret. The event body contains:

```json
{
  "id": "event-uuid",
  "rule_id": "rule-uuid",
  "server_id": "server-uuid",
  "rule_name": "Disk capacity",
  "metric": "disk",
  "state": "firing",
  "value": 93.5,
  "threshold": 85,
  "occurred_at": "2026-10-17T12:00:00Z",
  "delivery_status": "pending",
  "attempts": 0
}
```

The immutable payload describes the original event, so `delivery_status` and `attempts` in it do not change on retries. Inspect incident history for current delivery status and attempt count. Event states are `firing`, `recovered`, and explicitly requested `test`. Test events have a null value.

Requests include:

- `Content-Type: application/json`
- `X-GoPulse-Event-ID`: stable event ID for deduplication
- `X-GoPulse-Timestamp`: Unix timestamp of this delivery attempt
- `X-GoPulse-Signature`: `sha256=` followed by the hexadecimal HMAC-SHA256 of `timestamp + "." + exact_request_body`, using the rule signing secret **as its UTF-8 string**, not hex-decoded bytes

Verify the signature using a constant-time comparison, enforce a reasonable timestamp tolerance (for example five minutes), and deduplicate event IDs. A receiver should durably accept the event before returning a 2xx response.

State changes, incident records, and queued webhook events commit together in one SQLite transaction. Delivery happens outside that transaction. Delivery is **at least once**: a crash after the receiver accepts an event may cause a duplicate. A 30-second lease recovers interrupted attempts. Pending deliveries resume after restart.

Connection failures, HTTP 408, HTTP 429, and 5xx responses retry with exponential delays starting at five seconds, for up to eight attempts. Integer `Retry-After` values on HTTP 429 are honored up to one hour. Other non-2xx responses, including redirects, fail immediately. Requests time out after ten seconds. Events for the same rule retain queue order; a recovery waits until the preceding firing event is delivered or finally fails. A rule edit does not rewrite already queued payloads, destinations, or keys. Test requests have a 30-second cooldown per rule.

Webhook URLs cannot contain URL-user credentials or fragments. Delivery disables proxies and redirects, verifies TLS certificates, and validates every DNS result at connection time, blocking private, loopback, link-local, metadata, and reserved destinations. No response body or credential-bearing URL is stored in delivery errors. Destinations and signing keys are sensitive configuration stored in the protected SQLite file; they are not separately encrypted at rest. Protect the data directory and its backups.

## API

All routes require authentication and assigned-server access. Writes additionally require `server:write`.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/servers/{serverID}/stats/history?range=30d` | Saved metric summaries |
| GET | `/api/servers/{serverID}/alerts` | Rules and current state |
| POST | `/api/servers/{serverID}/alerts` | Create a rule |
| PUT | `/api/servers/{serverID}/alerts/{ruleID}` | Replace rule configuration |
| DELETE | `/api/servers/{serverID}/alerts/{ruleID}` | Delete rule and events |
| GET | `/api/servers/{serverID}/alerts/events` | Latest 100 incident/delivery events |
| PUT | `/api/servers/{serverID}/alerts/{ruleID}/silence` | Set `{"seconds":3600}` or clear with `{"seconds":0}` |
| POST | `/api/servers/{serverID}/alerts/{ruleID}/test` | Queue an explicit test notification |

Creation and update accept `name`, `metric` (`cpu`, `memory`, `disk`, `availability`), `threshold` (0–100), `duration_seconds` (0–86400), `enabled`, and `webhook_url`. The availability threshold is ignored. A server can have up to 100 rules. Rule IDs are server-scoped; a rule cannot be edited through another server's URL. Invalid ranges, unknown JSON fields, concatenated bodies, and oversized requests are rejected.

Monitoring tables remain in the SQLite database. The existing JSON configuration export/import does **not** include monitoring history, alert rules, signing keys, or delivery queues; preserve the database through your normal consistent SQLite backup process when migrating this feature.

## Verification

Regression coverage exercises aggregation, retention, bounded queries, actual database close/reopen, alert duration, missing data, stale collection, restart state, silencing, concurrent evaluation, atomic rollback, notification signatures, real local HTTPS delivery, retry/backoff, delivery order, interrupted delivery leases, destination restrictions, server deletion, RBAC and credential redaction, frontend actions, and chart gaps. Run `make check` and `make test-race`.
