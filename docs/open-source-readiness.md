# Open-source readiness review

Review date: 2026-10-17. Starting revision: `7d9b0e9`.

## History cleanup applied: 2026-10-18

The owner authorized the tested cleanup. All ten GitHub branches were updated
atomically with exact per-branch leases. `main` now includes the release tooling
and scanner fixes from PR #9. Two distinct exposed values (the JWT signing secret
and a GitHub OAuth client secret appearing in two files) were removed from file
contents and commit/tag messages throughout the mirrored history. Current branch
tip contents were preserved; `main` intentionally advanced to the reviewed PR.

The full-history scanner passes in the rewritten mirror and `make secrets`
passes both source and history scopes in a fresh verification clone. Every
reachable object in the preview was also checked for the two exact old values.
Scanner regression checks pass. No real-credential allowlist or baseline was added.

The owner subsequently confirmed on 2026-10-18 that both exposed secrets were
rotated and the old values invalidated. That is an owner confirmation, not an
independent provider-side audit. Old clones, forks, cached objects and GitHub-managed PR refs may retain
the original history; normal branch pushes cannot purge those copies. Contributors
must use the cleaned history and avoid merging old commits back into the project.

The earlier reviews below record their original state and validation results.
Their statements that branch history still contains the three scanner findings
were superseded by this cleanup and the owner's rotation confirmation. The
operational limits remain applicable to the evaluation prerelease.

## Earlier follow-up: 2026-10-18 (before history cleanup)

The repository is now public. The findings below remain a remediation record;
source changes cannot verify provider-side credential revocation. The latest
hosted main-branch CI run at `5234200` passed Linux/macOS tests, both Linux binary
builds and container startup; its security job failed in `make secrets`.

Reproduction identified a documentation false positive: the literal
`YOUR_FUNCTION_TOKEN` in `docs/functions.md`. The example now uses an environment
variable, and only that exact historical placeholder/path/detector is allowlisted.
Three real historical credential entries remain blocking. The scanner now runs
both scopes even after a failure and prints redacted file/line details. Regression
checks exercise allowed fixtures, ignored local files, new files, redaction and
credentials removed from source but retained in history.

Release tooling now packages versioned Linux amd64/arm64 binaries, checksums,
configuration examples, tracked documentation and license notices. The GitHub
workflow verifies the code and full history before creating a release draft.
There is no real-credential baseline, history rewrite or verified rotation in
this follow-up. See [the release guide](releases.md) for the remaining steps.

The original review below describes its own checks and publication-time state.

## Decision

The repository has been prepared for external contributors and repeatable
verification. **Publication is blocked by historical credential and infrastructure
exposure until the owner resolves the items below.** A clean `dist/gopulse-source.tar.gz` snapshot is prepared without Git history,
ignored configuration, state or flagged screenshots. The existing checkout still
retains its history. No public repository, release, deployment, credential rotation
or destructive history rewrite was performed by this review.

## Publication blockers requiring the owner

1. **Revoke or rotate historical credentials.** Commit `21a1623bb916` contains
   nonempty JWT and GitHub OAuth secrets in `config.yaml` (lines 13 and 20 in
   that revision); an OAuth secret also appears in `config.example.yaml:20` in
   commit `888642b1ac1f`. Values are deliberately omitted from this report. Rotation
   status cannot be established from source code. Rotate the OAuth client secret,
   change any reused signing secrets, and invalidate affected sessions/tokens.
2. **Remove sensitive history before publication.** Ignoring files and removing
   them from the index does not remove earlier commits. Choose a coordinated
   history rewrite or publish a new repository from the reviewed source snapshot
   without its `.git` directory. Historical screenshots contain infrastructure
   addresses. Re-scan every retained branch and tag; review old PRs, forks,
   artifacts and GitHub caches separately if the repository was ever public.
3. **Run the new hosted checks.** Docker is installed locally but its daemon is
   stopped, so image build/startup checks and Linux runtime checks require CI or
   an available Docker/Linux environment. Remote systemd, Nginx/TLS, UFW, SSH,
   database engines, GitHub OAuth and actual deployment flows need disposable
   infrastructure integration checks before a production release.
4. **Configure repository controls.** Enable private vulnerability reporting and
   secret scanning where available; protect the default branch with required
   checks. Configure the `production` environment and reviewers before using the
   manual deployment workflow. Confirm ownership/licensing rights for all
   contributed code and assets before public distribution.

## Completed preparation

- Aligned the Go module and internal imports with the canonical GitHub repository so external installation resolves the correct project.
- Added the MIT license already advertised by the README; contributor guide,
  security policy, bug/feature issue forms, PR template and third-party notices.
- Removed local `config.yaml` from Git tracking while retaining the working copy.
  Expanded ignore rules for secrets, keys, databases, runtime data and outputs.
- OCR-reviewed 50 documentation screenshots for addresses/email-like strings.
  Removed 13 flagged screenshots from tracking and retained local copies. The
  remaining screenshots are not certified free of all sensitive data; OCR can
  miss text and cannot assess asset ownership.
- Added real CI for Go and frontend tests, race detection, formatting, vet,
  vulnerability and secret scans, Linux amd64/arm64 binaries, checksums, license
  notices, and container build/startup checks. Actions are pinned to verified
  release commit SHAs; Dependabot proposes weekly Go/Actions/image updates.
- Changed automatic push deployment to manual dispatch with a `production`
  environment. Replaced placeholder CI commands with real validation.
- Added a multi-stage, unprivileged Docker image, persistent state in the example
  run command, mandatory external JWT configuration and a restricted build context.
- Corrected misleading setup/security documentation and pinned Chart.js 4.5.1.
  Documented internet/CDN dependencies, privileged host operations, unencrypted
  infrastructure state, single-process operation and platform limitations.

## Security and correctness repairs

| Finding | Resolution | Regression evidence |
| --- | --- | --- |
| First-user check raced with insertion | Atomic conditional SQLite insert creates exactly one initial administrator | 24 concurrent registrations through two store handles |
| Read-only project responses exposed webhook secrets and environment values | Restrict secret-bearing reads to project writers and redact reader snapshots | Project list/detail/automation/environment tests |
| GitHub clone/connect accepted out-of-scope targets | Require `projects:write` and check target server/project scope before remote/API work | Permission and cross-server denial tests |
| Forwarding headers selected OAuth redirect origin | Use configured callback URL or direct request origin; HTTPS callbacks set Secure OAuth cookies | Spoofed-forwarding and TLS callback tests |
| Origin guard rejected cross-site OAuth navigation | Permit safe top-level document navigation while retaining cross-site API, iframe and WebSocket rejection | Browser Fetch Metadata regression cases |
| OAuth codes/state appeared in request logs | Log escaped URL path without query values | Captured-log regression |
| Session configuration was ignored | Apply configured token duration and explicit secure-cookie option | Login token and cookie expiry test |
| Public default listener exposed bootstrap | Default to localhost; document initial setup and HTTPS | Default/configuration tests and isolated HTTP smoke check |
| Sensitive responses could be cached/framed | No-store outside static assets, nosniff, no-referrer, frame and object restrictions | Response-header tests |
| File names/paths entered HTML and inline JS without escaping | Encode text/attributes and JSON arguments; avoid path-derived CSS selectors | List, grid and breadcrumb injection tests with exact path preservation |
| Uploaded/imported SSH keys ignored custom state directory | Centralize `GOPULSE_DATA_DIR` resolution for state and key storage | Import test verifies configured key directory |
| Legacy Docker monolith carried module-wide daemon advisories | Use the maintained client/API SDK modules, preserving configured API 1.41 | HTTP daemon fixture covers inventory, actions, pull stream and multiplexed logs |
| Shell fixture timeout failed under race instrumentation | Raise only the mocked shell timeout from 1s to 10s | Race suite |

Authentication bodies are also bounded to 16 KiB. Invalid ports, nonpositive
session lifetimes and polling intervals fail configuration loading. Configured
HTTPS proxies should set `GOPULSE_AUTH_SECURE_COOKIES=true` and an explicit
`GOPULSE_GITHUB_CALLBACK_URL`; forwarded headers are deliberately not trusted.
Existing explicit listener/configuration values continue to override defaults.

## Verification

| Check | Outcome |
| --- | --- |
| `make check` | Passed: formatting, `go vet`, all Go tests, 154 frontend tests, optimized native build |
| `go test -race ./...` | Passed |
| Isolated native HTTP startup | Passed: fresh registration, login, registration lock, unauthorized API rejection, authenticated inventory and response headers |
| Linux cross-builds (`CGO_ENABLED=0`) | Passed for amd64 and arm64; binaries inspected as static Linux ELF files |
| `govulncheck ./...` on Linux amd64 | Zero reachable vulnerabilities; zero affected imported packages; one advisory in an unused module package |
| Gitleaks 8.30.0 publishable source snapshot | No findings; ignored local state/config/screenshots excluded |
| Gitleaks all 12 historical commits | Fails as intended: three real historical credential entries remain; one exact deterministic MCP test key is narrowly allowlisted |
| Module verification and tidiness | Passed `go mod verify` and `go mod tidy` |
| Go license inventory and notice collection | Passed: 65 combined Linux/macOS library entries; no unknown licenses. MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, and macOS-only MPL-2.0 |
| Actions workflow validation | Passed `actionlint` 1.7.12 |
| Container build/runtime and remote infrastructure | Not run locally; Docker daemon is stopped and no disposable remote hosts were provisioned |

The toolchain minimum is now Go 1.26.9. Updated chi, MCP SDK, SSH/crypto,
OpenTelemetry and related dependencies; replaced the legacy Docker monolith
with `github.com/moby/moby/client` v0.6.2 and `github.com/moby/moby/api` v1.56.1.
The Docker SDK regression uses a mock daemon at API 1.41 and tests inventory,
start/stop/restart/remove, image pulling, and multiplexed logs. It does not replace
an actual daemon integration test.

The unused module advisory, [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932),
concerns `golang.org/x/crypto/openpgp`, which GoPulse does not import. It is
recorded separately from reachable findings; no
vulnerability suppressions were configured. Historical secret findings are not
baselined or suppressed. The combined `make secrets` command intentionally fails
until the retained Git history is cleaned. Scanner databases change over time;
these results apply to this review, not future revisions.

Logs and redacted scanner output remain outside the repository; no credential
values belong in public artifacts.

## Architecture and operational limits

GoPulse remains a modular Go monolith: embedded HTML/JavaScript → chi middleware
(authentication, origin, permissions, rate limits) → handlers → services → SQLite
or SSH/host integrations. This fits a single self-hosted administrative instance;
there is no demonstrated fleet-size, throughput or availability guarantee.
SQLite uses one process-local connection pool and WAL; goroutine schedulers,
SSH pools and metric history assume one active process. Do not scale replicas
against the same state directory. Back up private state and keys together and
rehearse restoration on an isolated instance; SQLite WAL files must be handled
consistently, or stop the process before copying state.

The local Docker stats method currently returns placeholder zero values; this
review does not certify live resource metrics for every provider. Container
deployments target remote management; local Docker CLI operations require an
additional Docker CLI installation and explicit socket permissions.

Future work: reviewed/bundled frontend libraries with compiled Tailwind and a
strict script CSP; application-level secret encryption with a documented key
lifecycle; broader end-to-end Linux integration coverage; measured fleet and
scheduler capacity; metrics/tracing and bounded retention policies. These are
explicit limitations, not claims satisfied by adding CI. This review is a source
and tooling assessment, not an exhaustive penetration test or compliance audit.
