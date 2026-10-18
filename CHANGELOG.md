# Changelog

User-visible changes are recorded here. Version tags follow Semantic Versioning;
alpha, beta and release-candidate versions are explicitly marked as prereleases.

## Unreleased

- Create release drafts only through explicit maintainer dispatch. Publishing a
  manually created draft can emit a tag push; it must not start a duplicate build.

## v0.1.0-alpha.1 — 2026-10-18

First evaluation prerelease. Linux is the deployment target; no production
readiness, fleet capacity or availability guarantee has been established.

### Added

- Draft GitHub Releases from version tags or manual dispatch, with verification
  gates, Linux amd64/arm64 downloads, SHA-256 checksums and bundled license notices.
- Binary `--version` output containing version, commit and build time.
- Installation, upgrade and rollback instructions, a roadmap and support guidance.

### Fixed

- Upgrade OpenTelemetry to 1.45.0 to address
  [GHSA-8wmf-6v46-5gfg](https://github.com/advisories/GHSA-8wmf-6v46-5gfg),
  a diagnostic logging disclosure in earlier SDK versions.
- Function-trigger documentation uses an environment variable instead of a
  literal token placeholder that triggered Gitleaks.
- Secret scans report redacted file/line details and run both source and history
  scopes even if the first scope fails. The exact historical documentation
  placeholder is narrowly allowlisted; real credentials remain blocking findings.
- Removed two exposed credential values from all ten retained remote branches;
  source and full-history scans now pass. Existing contributors must migrate to
  the rewritten history. The owner confirmed rotation and invalidation of the
  old exposed values on 2026-10-18.
- Removed the old local configuration and thirteen flagged infrastructure
  screenshots from retained history while preserving current branch contents.

### Included workflows

- Server workspaces, SSH-based management, Docker, database administration,
  domains/TLS, load balancers and scheduled jobs.
- GitHub-connected application deployments with durable history and logs.
- Resource-limited Python/JavaScript functions with HTTP and cron triggers.
- Persistent monitoring, alerts and encrypted remote database backups.
- Scoped MCP access, structured function results, audit and approval flows.

### Known limitations

- GitHub-managed PR refs, forks, caches and old clones may retain original
  objects and need separate cleanup; contributors must use the rewritten history.
- Remote integrations need broader disposable-host verification; no fleet
  capacity or availability guarantee has been established.
