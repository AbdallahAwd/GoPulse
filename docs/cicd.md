# GoPulse GitHub CI/CD

GoPulse accepts a GitHub-compatible, HMAC-signed deployment notification at:

```text
https://YOUR-GOPULSE-HOST/api/webhooks/github/PROJECT_ID
```

The receiver verifies the signature against the project secret, accepts only
the repository's default branch, stores a durable deployment record, and runs
the deploy asynchronously. A GitHub retry with the same `X-GitHub-Delivery`
value is idempotent, so it cannot deploy the same delivery twice. Deployment
status is available from:

```text
GET /api/servers/SERVER_ID/projects/PROJECT_ID/deployments
GET /api/servers/SERVER_ID/projects/PROJECT_ID/deployments/DEPLOYMENT_ID
```

## Connect an existing uploaded project

An uploaded ZIP project can be converted to a Git-backed project from
**Project → Operations → Automation → Connect GitHub repository**. GoPulse
verifies the selection using the connected GitHub account, creates a local Git
snapshot of the currently uploaded files for rollback, configures the selected
repository as `origin`, and fetches its default branch. It does not replace the
running files during connection; the first successful CI/CD deployment switches
to the requested GitHub revision.

After the connection succeeds, enable CI/CD and copy the generated webhook URL
and secret into the repository's Actions secrets. Private repositories require
the GitHub account connection to remain authorized so GoPulse can fetch future
revisions.

## Recommended: GitHub Actions (CI first, deploy second)

This is the Vercel-like path: tests/build must pass before GoPulse is notified.
In the repository's GitHub settings, add these **Actions secrets**:

- `GOPULSE_WEBHOOK_URL`: the URL shown by the GoPulse project webhook panel.
- `GOPULSE_WEBHOOK_SECRET`: the matching secret shown by that panel.

The GoPulse repository ships a manual deployment workflow gated by real tests and the `production` environment. Configure environment reviewers before using it. For a different application repository, adapt the following example and replace the CI step with its real checks; do not deploy from a placeholder test. Pin Actions to reviewed commit SHAs.

Create `.github/workflows/gopulse-deploy.yml`:

```yaml
name: CI and GoPulse deploy

on:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - uses: actions/setup-node@v7
        with:
          node-version: '22'
      - run: make check # Adapt to the consuming repository's actual checks.

  deploy:
    needs: ci
    runs-on: ubuntu-latest
    steps:
      - name: Notify GoPulse
        shell: bash
        env:
          GOPULSE_WEBHOOK_URL: ${{ secrets.GOPULSE_WEBHOOK_URL }}
          GOPULSE_WEBHOOK_SECRET: ${{ secrets.GOPULSE_WEBHOOK_SECRET }}
          REPOSITORY: ${{ github.repository }}
          REF: ${{ github.ref }}
          BRANCH: ${{ github.ref_name }}
          COMMIT: ${{ github.sha }}
          RUN_ID: ${{ github.run_id }}
          RUN_ATTEMPT: ${{ github.run_attempt }}
          COMMIT_MESSAGE: ${{ github.event.head_commit.message }}
        run: |
          set -euo pipefail
          test -n "$GOPULSE_WEBHOOK_URL"
          test -n "$GOPULSE_WEBHOOK_SECRET"

          payload="$(jq -cn \
            --arg ref "$REF" \
            --arg after "$COMMIT" \
            --arg full_name "$REPOSITORY" \
            --arg default_branch "$BRANCH" \
            --arg message "$COMMIT_MESSAGE" \
            '{ref:$ref, after:$after, deleted:false,
              repository:{full_name:$full_name, default_branch:$default_branch},
              pusher:{name:"github-actions"},
              head_commit:{id:$after, message:$message}}')"

          signature="sha256=$(printf '%s' "$payload" | \
            openssl dgst -sha256 -hmac "$GOPULSE_WEBHOOK_SECRET" -hex | \
            sed 's/^.* //')"
          delivery="actions-${RUN_ID}-${RUN_ATTEMPT}-${COMMIT}"

          curl --fail-with-body --silent --show-error \
            --retry 5 --retry-all-errors --retry-delay 2 \
            -X POST "$GOPULSE_WEBHOOK_URL" \
            -H 'Content-Type: application/json' \
            -H 'X-GitHub-Event: push' \
            -H "X-GitHub-Delivery: $delivery" \
            -H "X-Hub-Signature-256: $signature" \
            --data-binary "$payload"
```

The workflow intentionally sends the same signed shape as a native GitHub
push event. GoPulse identifies the delivery as `github_actions` for the
deployment history, while native GitHub webhook deliveries are recorded as
`github_webhook`.

## Alternative: native GitHub webhook

Use this when GitHub Actions is not needed. In **Repository → Settings →
Webhooks → Add webhook**:

1. Set the Payload URL to the GoPulse URL.
2. Choose `application/json`.
3. Enter the GoPulse webhook secret.
4. Select **Just the push event** and save.

GitHub's ping delivery should return `200`. A valid default-branch push returns
`202` with a deployment ID. Do not enable both a native push webhook and the
Actions workflow, or two independent deliveries will be created for one
commit.

## Operational notes

- The GoPulse endpoint must be reachable from GitHub over HTTPS; put it behind
  a reverse proxy or tunnel if the panel is private.
- Never put the webhook secret in a repository file or URL query parameter.
- A deployment can be `queued`, `running`, `succeeded`, or `failed`; queued
  work resumes after a GoPulse restart, while failed builds leave the previous
  runtime running when rollback is possible.
- Re-enable the webhook after rotating its secret, then update the GitHub
  secret to the new value.
