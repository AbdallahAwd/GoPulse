GoPulse brings application deployments, functions, databases, Docker, monitoring,
backups, and MCP into a self-hosted VPS dashboard built in Go.

## Install

Download the archive for your Linux architecture and its `.sha256` file from the
assets below. Verify it with `sha256sum --check <archive>.sha256`, extract it,
and run `./gopulse --version` to inspect the build.

Follow the included README for first-account setup, a persistent random JWT
secret, verified SSH host keys, and HTTPS. There are no default credentials.
The [installation and upgrade guide](https://github.com/AbdallahAwd/GoPulse/blob/main/docs/releases.md)
covers setup, backups, and rollback.

## Scope

This is early software for trusted operators. Linux is the deployment target;
browser libraries currently require CDN access. Run one control-plane process
per state directory. Read SECURITY.md and the changelog before upgrading.

Maintainers: review these notes, the generated changes, verification results,
and credential remediation before publishing this draft.
