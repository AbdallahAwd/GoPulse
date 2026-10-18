# Changelog

User-visible changes are recorded here. Version tags follow Semantic Versioning;
alpha, beta and release-candidate versions are explicitly marked as prereleases.
No versioned release has been published yet.

## Unreleased

### Added

- Draft GitHub Releases from version tags or manual dispatch, with verification
  gates, Linux amd64/arm64 downloads, SHA-256 checksums and bundled license notices.
- Binary `--version` output containing version, commit and build time.
- Installation, upgrade and rollback instructions, a roadmap and support guidance.

### Fixed

- Function-trigger documentation uses an environment variable instead of a
  literal token placeholder that triggered Gitleaks.
- Secret scans report redacted file/line details and run both source and history
  scopes even if the first scope fails. The exact historical documentation
  placeholder is narrowly allowlisted; real credentials remain blocking findings.

### Initial feature set awaiting a reviewed release

- Server workspaces, SSH-based management, Docker, database administration,
  domains/TLS, load balancers and scheduled jobs.
- GitHub-connected application deployments with durable history and logs.
- Resource-limited Python/JavaScript functions with HTTP and cron triggers.
- Persistent monitoring, alerts and encrypted remote database backups.
- Scoped MCP access, structured function results, audit and approval flows.

### Known release blockers

- Historical credentials require owner revocation/rotation and coordinated
  history cleanup. A clean current tree is insufficient.
- Remote integrations need broader disposable-host verification; no fleet
  capacity or availability guarantee has been established.
