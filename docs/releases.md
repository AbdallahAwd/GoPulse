# Installation, releases and upgrades

## Release status and supported platforms

No versioned release has been published yet. Use the source-build instructions in
the [README](../README.md) until a reviewed prerelease appears on
[GitHub Releases](https://github.com/AbdallahAwd/GoPulse/releases).

Release archives target Linux `amd64` (x86-64) and `arm64` (AArch64), with CGO
disabled. macOS is supported for source development; Windows is not a deployment
target. The binary embeds GoPulse's templates and local frontend assets. Browser
libraries still require CDN access. Docker, systemd, Nginx and database engines
are optional integrations with their own host requirements.

## Install a published archive

1. Select an explicit version on GitHub Releases and read its notes. Early
   versions use tags such as `v0.1.0-alpha.1`; a prerelease is for evaluation.
2. Check `uname -m`: choose `amd64` for `x86_64`, `arm64` for `aarch64`/`arm64`.
3. Download that architecture's archive and matching `.sha256` file. In the
   directory containing both files, verify before extraction:

   ```sh
   # Replace the version and architecture with the files you downloaded.
   sha256sum --check gopulse_v0.1.0-alpha.1_linux_amd64.tar.gz.sha256
   tar -xzf gopulse_v0.1.0-alpha.1_linux_amd64.tar.gz
   cd gopulse_v0.1.0-alpha.1_linux_amd64
   ./gopulse --version
   ```

   Checksums detect changed or incomplete downloads; they are not independent
   publisher signatures. Get both files from the intended repository release.
4. Set a persistent random JWT secret. Generate it once and keep it in a private
   environment file or secret manager across restarts:

   ```sh
   export GOPULSE_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
   export GOPULSE_SERVER_HOST=127.0.0.1
   export GOPULSE_DATA_DIR="$HOME/.gopulse"
   ./gopulse
   ```

5. Open `http://localhost:8080` locally, or use an SSH tunnel. Create the first
   administrator before making the listener reachable to other users. There is
   no default account. Configure independently verified SSH host keys through
   `GOPULSE_SSH_KNOWN_HOSTS`. Use HTTPS and the secure-cookie setting for remote
   access. Read [SECURITY.md](../SECURITY.md) before connecting real infrastructure.

Use a dedicated service account and private persistent state directory. Keep
configuration and keys outside a directory you replace on upgrades. Do not run
multiple GoPulse processes against one state directory.

## Upgrade and rollback

1. Read release notes and migrations. Record the running `--version` output and
   retain the previous binary/configuration.
2. Stop GoPulse and ensure no second instance is writing state. Back up the
   entire `GOPULSE_DATA_DIR` (default `~/.gopulse`), configuration, persistent JWT
   secret and any SSH key files stored elsewhere. Preserve permissions. A stopped
   backup must include any remaining SQLite WAL/SHM files together with the DB.
   Encrypt backups: infrastructure state and configuration exports contain secrets.
3. Verify the new download, extract into a separate directory, then replace only
   the binary. Start it using the existing environment and state path.
4. Verify login, server inventory and a read-only monitoring operation. Confirm
   schedules and integrations before enabling write operations.
5. If rollback is necessary, stop the new process. Restore the prior binary,
   configuration and pre-upgrade state backup together. Never assume an older
   binary can read a database migrated by a newer version. Restoring the backup
   loses changes made after it; inspect remote work separately before retrying.

Rehearse upgrades and restores on isolated infrastructure. No release support
SLA or maintained stable support branch is currently promised.

## Build release archives locally

With the Go toolchain declared in `go.mod`, Git, Bash and Make:

```sh
make package VERSION=v0.1.0-alpha.1 ARCH=amd64
make package VERSION=v0.1.0-alpha.1 ARCH=arm64
```

The commands produce `dist/gopulse_<version>_linux_<arch>.tar.gz` and a matching
`.sha256`. Each archive contains a versioned binary, configuration examples,
tracked documentation, GoPulse's MIT license, the Go license and dependency
license notices. Ignored local configuration, private screenshots and runtime
state are excluded. A local package is a build artifact, not a reviewed release.
Uncommitted changes are marked with `-dirty` in the embedded commit metadata.
CI also smoke-tests the amd64 archive against isolated state before uploading it.

## Maintainer release procedure

1. Resolve the historical exposures recorded in the [readiness report](open-source-readiness.md).
   Revoke/rotate exposed credentials with the providers, invalidate affected
   sessions, and coordinate history cleanup across affected branches/tags. A
   scanner cannot confirm revocation. Check old artifacts, forks and caches as
   appropriate; rewriting this checkout alone does not clean every copy.
2. From a full-history clone, run `make check`, `make test-race`,
   `make test-tooling`, `make security` and `make secrets`. All must pass. Never
   baseline a real credential or disable the history gate to publish a release.
3. Verify Linux/container startup in CI and applicable integrations on disposable
   hosts. Review dependency licenses and update this changelog with migration
   notes and known limitations. Start with a prerelease while the project matures.
4. Merge the reviewed commit. Either push an annotated version tag or dispatch
   the **Release** workflow on that commit with an explicit version. Accepted
   forms are `vMAJOR.MINOR.PATCH` and `vMAJOR.MINOR.PATCH-{alpha,beta,rc}.N`.
   Existing tags must point to the selected commit.
5. The workflow verifies the code and full history, builds both Linux archives,
   verifies checksums and native version output, then creates a **draft** release.
   It marks prerelease versions accordingly and generates change notes. Only
   the draft-creation job receives repository write permission. Reruns fail on
   an existing release rather than overwriting its assets.
6. Review the draft's notes, exact commit, downloaded archives, license notices
   and installation instructions. Remove the maintainer reminder in the notes.
   Publish the draft only when those checks are complete. Versions/tags must not
   be reused for different source; fix mistakes in a new version.

The current history scan intentionally remains blocking until real historical
credential findings are remediated. The documentation-placeholder fix does not
establish that previously exposed credentials are safe.
