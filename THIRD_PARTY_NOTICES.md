# Third-party notices

The MIT license in `LICENSE` applies to GoPulse's original work. Dependencies,
fonts, icons and external services retain their respective terms. The Go module
path is `github.com/AbdallahAwd/GoPulse`; it is the project's own code, not a
third-party dependency.

## Go dependencies

`go.mod` and `go.sum` define the build dependency graph and integrity hashes.
The combined Linux amd64 and macOS arm64 dependency license inventory is in [docs/licenses/go-dependencies.csv](docs/licenses/go-dependencies.csv).
Run `make licenses` to collect full license notices into ignored
`third_party_licenses/`. The Linux artifact workflow includes that directory in
each archive. Distributors should regenerate notices for the exact source and
build target they ship, retaining applicable notices from dependencies and the
Go toolchain. The container base OS and installed packages have their own
copyright notices under `/usr/share/doc`.

The macOS build includes `github.com/shoenig/go-m1cpu` v0.1.6 under MPL-2.0.
Its unmodified source is available at [the exact upstream tag](https://github.com/shoenig/go-m1cpu/tree/v0.1.6).
`go-licenses save` collects source for restricted license categories; include
these materials when distributing macOS builds. Linux builds do not import that
platform-specific module.

Remote Backups uses `filippo.io/age` v1.3.2 and `filippo.io/hpke` v0.4.0,
both under BSD-3-Clause. Their notices are included by `make licenses` and the
distribution workflow.

License detection is evidence for review, not a substitute for reviewing upstream
terms. Any unknown or restricted license must be resolved before distribution.

## Frontend libraries loaded from CDNs

| Component | Version referenced | License | Upstream notice |
| --- | --- | --- | --- |
| Tailwind CSS | Play CDN, currently unpinned | MIT | [Tailwind Labs](https://github.com/tailwindlabs/tailwindcss/blob/v3.4.17/LICENSE) |
| Chart.js | 4.5.1 | MIT | [Chart.js](https://github.com/chartjs/Chart.js/blob/v4.5.1/LICENSE.md) |
| xterm.js and fit addon | 5.3.0 / 0.8.0 | MIT | [xterm.js](https://github.com/xtermjs/xterm.js/blob/5.3.0/LICENSE) |
| Ace | 1.32.7 | BSD-3-Clause | [Ace](https://github.com/ajaxorg/ace/blob/v1.32.7/LICENSE) |
| Font Awesome Free | 6.4.0 | Icons: CC-BY-4.0; fonts: OFL-1.1; code: MIT | [Font Awesome](https://github.com/FortAwesome/Font-Awesome/blob/6.4.0/LICENSE.txt) |

Copies of these notices are kept in [docs/licenses/frontend](docs/licenses/frontend).
The Tailwind notice is from 3.4.17; the unpinned CDN can change independently.
The current application embeds its own UI files, but loads these libraries and
fonts from remote CDNs at runtime. It requires internet access and trusts those
providers. Bundling reviewed versions with retained notices, generating Tailwind
CSS at build time, and implementing a stricter script integrity/CSP policy are
recommended production improvements.
