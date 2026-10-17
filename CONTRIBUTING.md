# Contributing to GoPulse

Open an issue before substantial architectural or behavior changes. For bugs,
include the commit, operating system, Go version, reproduction steps, expected
behavior, and sanitized logs. Report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md).

## Local development

1. Install the Go version declared in `go.mod` or newer, Node.js 22+ for frontend
   tests, and Git. Linux is the deployment target; macOS supports development.
2. Clone `https://github.com/AbdallahAwd/GoPulse.git` and run `make deps`.
3. Copy `.env.example` to `.env` and supply a random JWT secret generated with
   `openssl rand -hex 32`. Never commit `.env`, `config.yaml`, keys, or state.
4. Optionally copy `config.example.yaml` to `config.yaml`. Use a temporary
   `GOPULSE_DATA_DIR` when experimenting; the default is `~/.gopulse`.
5. Run `make run`, then create the initial administrator on localhost.
   Hot reload requires [Air](https://github.com/air-verse/air) on PATH.

The UI embeds templates, CSS and JavaScript in the binary; rebuild after changes.
Browser dependencies currently load from CDNs, so the UI needs internet access.
Docker, systemd, Nginx, database engines and SSH hosts are optional integrations,
not prerequisites for unit tests. Use disposable VMs for destructive integration
checks. Never point tests at a production host.

## Before submitting a pull request

Run `make check` and `make test-race`. Run `make security` with a current patched
Go toolchain and `make secrets` before sharing commits. Tools installed by these
last two commands are placed in ignored `bin/tools`.

Keep handlers, services and stores separate. Preserve RBAC, server ownership,
strict SSH host verification and MCP approval boundaries. Add regression tests
for behavioral fixes. Use `gofmt`; avoid unrelated formatting changes. Document
configuration and API changes and explain user impact, validation and limitations
in the pull request. CI tests use mocks and temporary stores; passing them does
not establish that every remote integration works.

## Licensing and review

Contributions are accepted under the repository's MIT license. Only contribute
code and assets you have permission to license; identify third-party sources and
retain their license notices. Maintainers review and merge changes; opening a PR
does not guarantee acceptance. Treat contributors respectfully, focus criticism
on the work, and do not publish private credentials or personal information.
