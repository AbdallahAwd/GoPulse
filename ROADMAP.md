# Roadmap

GoPulse focuses on making a small team's own servers easier to operate. This is
a direction for discussion, not a delivery schedule. Propose concrete workflows
through [feature requests](https://github.com/AbdallahAwd/GoPulse/issues/new?template=feature_request.yml).

## Next: establish a reviewed first prerelease

- Resolve historical credential exposure and pass all release gates.
- Exercise installation, upgrades, backups and recovery on disposable Linux hosts.
- Expand integration coverage for Docker, SSH, databases, TLS and deployments.
- Publish versioned Linux archives with clear limits and migration notes.

## Reliability and deployment

- Bundle/review browser dependencies, compile Tailwind and tighten CSP/integrity.
- Define application-level secret encryption and an operator-managed key lifecycle.
- Measure fleet size, collector load, scheduler capacity and database growth.
- Add actionable control-plane diagnostics and structured operational telemetry.

## Contributor experience

- Improve onboarding examples with synthetic infrastructure and sanitized fixtures.
- Turn reproducible bugs and focused documentation gaps into approachable issues.
- Keep the single-process deployment model clear as integrations grow.

Multi-controller orchestration, hostile multi-tenant function hosting and
hyperscaler feature parity are outside the current scope. Changes to those
boundaries need a separate architecture and security review.
