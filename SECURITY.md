# Security policy

GoPulse is an administrative control plane with access to SSH credentials,
Docker, terminal commands, databases and deployment secrets. Treat its host and
all administrator accounts as privileged infrastructure.

## Reporting vulnerabilities

Use [GitHub private vulnerability reporting](https://github.com/AbdallahAwd/GoPulse/security/advisories/new)
when enabled. If the private form is unavailable, open an issue requesting a
private reporting channel without exploit details or sensitive data. Do not
post credentials, exports, host addresses or unredacted logs in public issues.
Include affected commits, impact and a minimal reproduction using dummy data.
There is no promised response SLA or maintained stable release support branch
yet. Early prereleases are for evaluation; use reviewed revisions and monitor
security advisories. Read the [release guide](docs/releases.md) before upgrades.

## Deployment boundary

- The default listener is `127.0.0.1:8080`. Complete first-account setup over
  localhost or an SSH tunnel before exposing any listener. The first account
  becomes administrator; there is no default password.
- Use HTTPS through the managed dashboard domain or a trusted reverse proxy.
  For another TLS proxy, set `GOPULSE_AUTH_SECURE_COOKIES=true` and configure an
  exact HTTPS `GOPULSE_GITHUB_CALLBACK_URL` for OAuth; arbitrary
  `X-Forwarded-Proto` headers are deliberately not trusted. Restrict access
  through a private network, VPN or firewall as appropriate for your hosts.
- Generate and preserve a random JWT secret of at least 32 characters. Rotating
  it invalidates sessions. Browser logout clears the cookie; a copied JWT can
  remain valid until expiry unless its user is deleted or the secret is rotated.
- SSH requires verified known_hosts entries via `GOPULSE_SSH_KNOWN_HOSTS` or the
  default SSH known_hosts file. Verify fingerprints independently; `ssh-keyscan`
  alone does not authenticate a host.
- Run with the least host and SSH privileges your workflows require. Docker
  socket access grants extensive host control. The example container runs as
  an unprivileged user and does not mount the Docker socket or host filesystem.
- SQLite and configuration backups contain infrastructure secrets without
  application-level encryption. The data directory and SSH key files need
  restricted permissions, encrypted storage and encrypted external backups.
  Backup exports can include private SSH keys and password hashes.
- Permissions for terminal, file execution, SQL, builds and cron allow powerful
  operations. Grant these only to trusted operators; RBAC is not a sandbox.
- Remote installation scripts, package registries and frontend CDNs are supply
  chain dependencies. Review them for your environment. Current CDN assets are
  not bundled or protected by a complete integrity/CSP policy.
- Do not run multiple GoPulse instances against one state directory. Schedulers,
  deployment workers and in-memory SSH/metrics state assume one active process.

## Historical credential exposure and release gate

Previously committed JWT and GitHub OAuth secrets were removed from the retained
branch history on 2026-10-18. All ten remote branches were rewritten, and `main`
includes the scanner and release-tooling fixes. Source and full-history scans pass
without suppressing real credentials. See the [readiness report](docs/open-source-readiness.md).

**Credential rotation has not been verified.** Treat the previously exposed values
as compromised. The owner must revoke/rotate the OAuth client secret and every
reused JWT signing secret, update affected deployments and invalidate affected
sessions before publishing a reviewed release. History cleanup cannot invalidate
copied credentials. GitHub-managed pull-request refs, cached views, forks and old
clones may still retain original objects; coordinate their cleanup separately.
Existing contributors should clone the cleaned repository again or carefully
migrate their work without merging the old history back into it.

`make secrets` checks publishable source and all locally available branches and
tags. CI uses a full-history checkout and keeps both scopes blocking. The only
allowlists cover an exact deterministic test fixture and an exact historical
documentation placeholder; real historical findings are not suppressed. The
release workflow cannot create a draft until this combined check passes.
