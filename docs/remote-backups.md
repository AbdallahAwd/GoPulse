# Remote Backups

Remote Backups sends an encrypted copy of a managed database to a different SSH
server. Open a source server in the dashboard and select **Remote Backups**.
Create a paused plan, download its recovery key, and run the first backup manually
before enabling its schedule.

## Setup and supported data

- Add the source and destination through GoPulse's existing server inventory.
  Both need working SSH access with verified host keys. Configure
  `GOPULSE_SSH_KNOWN_HOSTS` or the dashboard account's `~/.ssh/known_hosts` using
  independently verified fingerprints. The destination needs SFTP and a writable
  absolute directory, for example `/srv/gopulse-backups`.
- The source needs Linux, Bash, GNU `timeout`, and its native database tools.
  PostgreSQL, MySQL, MongoDB and Redis use the tools inside their existing
  `gopulse-<database-id>` Docker containers. SQLite uses the source host's
  `sqlite3` command and its online backup API, including committed WAL data.
- PostgreSQL/MySQL/MongoDB back up the selected database. Redis backs up the
  instance's RDB data across its default 16 logical databases. SQLite backs up
  the managed `sqlite.db`. This does not back up application files, operating
  systems, database accounts, PostgreSQL roles/tablespaces, or the GoPulse
  control plane. PocketBase, Appwrite and Qdrant are not supported here.
- Restore tests require enough source disk space for the decrypted ZIP, extracted
  dump and restored data. Container tests use the exact locally cached image ID
  recorded at capture time. Keep that image available; GoPulse does not pull a
  replacement tag. Default test memory is 1024 MiB, configurable to 128–16384 MiB.
  SQLite testing requires disk space but has no container memory cap.
- PostgreSQL/MySQL/MongoDB database names are limited to 1–63 letters, digits,
  underscores and hyphens. Names cannot start with a hyphen. Existing databases
  outside this set need a separately reviewed native backup procedure.

Use a dedicated destination SSH account and directory. GoPulse creates new
directories with mode `0700` and files with `0600`; it preserves existing
directory permissions and refuses symlink directory components. Use separate
physical infrastructure for meaningful off-server protection. The app rejects
identical inventory IDs and known identical host strings/IP addresses, but cannot
prove that aliases resolve to different physical machines.

## Dashboard workflow

1. Choose a database, destination server, directory and plan name. Source database,
   destination and directory are immutable after creation.
2. Choose hourly, daily, weekly or five-field cron timing and an IANA timezone.
   The editor previews upcoming execution times.
3. Set retention days and a minimum number of recent copies. Automatic restore
   testing is enabled by default. Set timeout, maximum uncompressed dump size and
   container test memory under resource limits.
4. Save, then **Download recovery key** in the plan's details. Keep a separate
   secure copy outside both servers. Run a backup and inspect the restore result.
5. Enable the schedule. Details show the latest 100 archives and execution
   attempts, encrypted downloads, checksum, test results and manual retesting.
   Cancellation is available for an active execution.

Schedules run in the GoPulse process every five seconds, using persisted cron
state. Keep one control-plane process running against its SQLite database.
There are two global execution workers and at most one execution per plan.
Overlapping or capacity-limited schedule slots are recorded as skipped. Missed
slots during downtime are advanced to the next future occurrence on restart;
they are not replayed in a burst. Cron timezone rules determine DST behavior.
Each execution has a 120–7200 second deadline, followed by a separate cleanup
attempt of up to 30 seconds. Restore readiness waits are also bounded.

## Encryption and recovery

GoPulse generates a distinct age X25519 key for each plan. The secret identity is
stored as `$GOPULSE_DATA_DIR/remote-backup-keys/<plan-id>.agekey` with mode `0600`.
Only the public recipient is stored in plan metadata. A missing key fails the
execution; it is never silently replaced. Recovery-key downloads require both
write permissions and both server scopes and use `Cache-Control: no-store`.

Native dump bytes are streamed through a ZIP archive into standard authenticated
age encryption **before** they are uploaded. The destination receives only
`<directory>/<plan-id>/<backup-run-id>.zip.age`. Its encrypted manifest records
the engine, database name, image ID and source identifiers, without credentials.
The source may hold temporary plaintext dumps during capture and restore testing;
the dashboard relays streams without writing dump files to its local disk.

An upload uses an exclusive `.partial` file, syncs when the SFTP server supports
fsync, verifies the stored ciphertext size and SHA-256 by reading it back, then
renames it to its final name. Failed source commands, empty dumps, excessive size
and failed transfers cannot publish a successful backup record. A crash after
rename but before metadata commit can leave a recoverable encrypted orphan file.

For independent disaster recovery, install the official
[age CLI](https://github.com/FiloSottile/age) and decrypt an archive with the
exported key:

```sh
age --decrypt -i /safe/plan.agekey -o /private/backup.zip /safe/backup.zip.age
unzip -l /private/backup.zip
```

The ZIP contains `manifest.json` and `database.dump`. Extract into a private
directory and follow the database engine's native recovery procedure on a
separate recovery host. This screen performs isolated tests; it does not offer
an overwrite-production restore action. Independently inspect recovery results
before using recovered data for production.

For control-plane disaster recovery, preserve `gopulse.db` using a consistent
SQLite backup and preserve the `remote-backup-keys` directory separately. The
existing JSON configuration export does **not** contain these plans, run history,
artifacts or recovery keys. Restoring just the JSON export is insufficient.
If only a key file is missing, restore its original exported identity to the
path above with mode `0600`; its recipient must match the plan. If control-plane
metadata is lost, the archive's manifest and key still permit native recovery.

## Automatic restore testing and retention

Tests read the finalized archive back from the destination. Before executing any
restore, GoPulse verifies ciphertext length/checksum, complete age authentication,
ZIP checksums, size limits and manifest ownership. It extracts only the expected
dump into a private source workspace.

| Engine | Isolated restore and validation |
| --- | --- |
| PostgreSQL | Fresh container, native custom-format `pg_restore` with exit-on-error; read/count every user table |
| MySQL | Fresh container, import SQL with failure propagation; read/count every base table |
| MongoDB | Fresh container, archive restore with stop-on-error; validate each collection and count documents |
| Redis | Fresh container, load RDB, restart, read/serialize every key in logical databases 0–15 |
| SQLite | Separate snapshot file, `PRAGMA integrity_check`, read/count every user table |

Container tests use no external network, published ports or production host
mounts. They use a non-root image user, a read-only root filesystem, fresh
anonymous data volumes, dropped capabilities, no-new-privileges, one CPU, a PID
limit and the configured memory cap. An ownership label and run-specific UUID
name identify scratch containers. These tests confirm native recovery and data
reads; they do not prove application-level correctness, a recovery-time SLA or
point-in-time recovery. Tables using extensions or nontransactional engines may
need additional engine-specific backup policies.

Retention runs after a successful new backup (and its automatic test, when
enabled). An archive is eligible only when it is older than the retention window,
outside the newest `keep_last` copies, and is not the latest archive that passed a
restore test. A failed restore test retains its encrypted archive and skips
retention. Each pass prunes at most 100 eligible copies, only at exact owned paths.
It never deletes unrelated files. Metadata is retained after pruning.

Cleanup removes the known temporary files and owned scratch container/volumes.
An interrupted run is marked on restart and is not replayed. Cleanup failures
remain visible and retry every minute in a separate worker so offline hosts do
not block scheduling. Investigate persistent cleanup failures, disk capacity and
destination availability. Retention needs a successful backup to run; an offline
control plane or failing source can delay deletions.

Pause and remove plans before deleting either referenced server or the database.
Plan removal requires no active execution or pending cleanup and removes dashboard
metadata only. Remote encrypted files and the local recovery key remain available
for recovery; export the key before removing the plan. Remove orphaned files and
keys manually after their recovery value has been reviewed.

## Architecture, API and authorization

```text
Dashboard -> scoped REST handlers -> SQLite plans/runs/artifacts
                         |
                 two bounded Go workers
                         |
source SSH/native dump -> ZIP -> age encryption -> destination SFTP
                         ^                          |
                         +--- verified download ----+
                              isolated native restore
```

The feature stays in the existing Go monolith: it reuses server credentials,
RBAC, embedded UI, SQLite transactions and the cron parser. A durable claim and
unique active-run index prevent overlapping plans; job phases and artifact
results survive restart. This avoids adding a broker or separate database to a
single-instance management dashboard. A multi-replica control plane would need
distributed worker leases and fencing before deployment. Live metrics remain
cached separately; backup metadata is read from SQLite to avoid stale run state.
There is no CDN or shared cache for key or archive downloads.

The API base is `/api/servers/{serverID}/remote-backups`:

| Method | Suffix | Behavior |
| --- | --- | --- |
| GET / POST | `/` | List / create plan |
| GET | `/options`, `/preview?schedule=...&timezone=...` | Safe inventory choices / next schedule slots |
| PUT / DELETE | `/{planID}` | Update mutable settings / remove paused plan metadata |
| POST | `/{planID}/run`, `/{planID}/cancel` | Start backup / cancel execution |
| GET | `/{planID}/runs`, `/{planID}/artifacts` | Latest 100 records |
| POST | `/{planID}/artifacts/{artifactID}/test` | Retest a retained encrypted copy |
| GET | `/{planID}/artifacts/{artifactID}/download` | Stream encrypted archive with SHA-256 header |
| GET | `/{planID}/recovery-key` | Download plan secret identity |

Create/update accepts a strict JSON object up to 16 KiB:

```json
{
  "name": "Primary database to backup host",
  "database_id": "database-id",
  "destination_id": "backup-server-id",
  "directory": "/srv/gopulse-backups",
  "schedule": "0 2 * * *",
  "timezone": "UTC",
  "retention_days": 30,
  "keep_last": 3,
  "enabled": false,
  "auto_test": true,
  "timeout_seconds": 3600,
  "max_size_gb": 10,
  "test_memory_mb": 1024
}
```

Responses follow `{ "success": true, "data": ... }`; starting a run returns 202
with its persisted run ID. Invalid input returns 400, denied access 403, missing
resources 404 and overlapping/capacity-limited manual execution 409. Polling
history observes asynchronous progress rather than holding an HTTP request open.

Reads require access to the source and destination; plans outside destination
scope are hidden. Mutations and key/archive downloads require `database:write`
and `server:write` plus both scopes. A schedule executes under the user who most
recently saved the plan. The service rechecks that user's current stored
permissions before capture, transfer and testing; revocation causes failure.
Keep the dashboard behind HTTPS and restrict its administrators, since it holds
SSH credentials and recovery keys. Shell arguments are quoted; SQL identifiers
are quoted for read checks; command stderr and database credentials are excluded
from persisted failure messages.

The three SQLite tables use plan primary keys, source/destination/database foreign
keys with delete restriction, a due-schedule index, a partial unique active-run
index, and a plan/date archive index for retention. Secret keys stay outside SQL.
No full dump or encrypted archive is stored in the dashboard database. Transfer
memory is bounded by stream buffers; restore tests use source disk for ZIP random
access. Latency and throughput depend on database size, compression and both SSH
links. There is no claimed fleet size, availability SLA or benchmark. Plan limits
(100/source), worker limits, timeouts and size limits bound resource use; source
disk and SSH bandwidth are the principal bottlenecks.

## Verification

`make check` and `make test-race` include encryption/tamper/partial-dump tests,
schedule persistence, cancellation, overlap, restart cleanup, retention,
permissions, key protection and UI behavior. A real local SSH/SFTP integration
test captures a SQLite database, transfers ciphertext, restores and reads the
downloaded copy, checks cleanup, rejects symlinks/corruption and verifies that the
source database was unchanged. Command fixtures execute all four Docker restore
scripts and test isolation flags, native failures, cleanup and credential quoting.

Live PostgreSQL, MySQL, MongoDB and Redis Docker restores were not run in this
environment, at the user's request. The command fixtures do not replace testing
against the actual database images and datasets before production rollout.
