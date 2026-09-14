# Deployment

Production is `server/wrangler.jsonc` → `production`, Worker `pokachy`, origin `https://pokachy.com`. Cloudflare manages DNS; GoDaddy can remain the registrar. Workers Paid supports login emails to open-registration users.

## Credentials

Run `python3 scripts/configure-secrets.py` to enter the GitHub OAuth Client ID and Client Secret. It generates `BETTER_AUTH_SECRET` and writes `.secrets/production.json` with private permissions. Git ignores this directory. Never put secrets in commits, command arguments, or issue reports.

GitHub OAuth callback: `https://pokachy.com/api/auth/callback/github`. The CLI uses Pokachy's device flow; GitHub's separate device flow is unnecessary. Store the managed Turnstile widget secret as `TURNSTILE_SECRET`. The public sitekey belongs in production vars. Production `TURNSTILE_HOSTNAMES` must contain only `pokachy.com`.

## Resources

Production requires D1 database `pokachy`, Durable Object `UserHub`, queues `pokachy-mail` and `pokachy-mail-failed`, and Email Sending enabled for `pokachy.com`. Bind email as `EMAIL` and the producer queue as `MAIL_QUEUE`.

For another account, create these resources with the project's Wrangler, replace account/database IDs in the configuration, and onboard the sending domain. Verify DNS before testing with a real inbox you control.

## GitHub deployment credential (one-time setup)

The GitHub `production` environment permits only branch `main`. Store a dedicated Cloudflare API token there as `CLOUDFLARE_API_TOKEN`. The account ID is already public configuration; application secrets stay in the Worker and are not copied to GitHub.

Use Cloudflare's **Edit Cloudflare Workers** token template, scope it to the Pokachy account and `pokachy.com`, and include account **D1 Edit** and **Queues Edit** for migrations and queue configuration. Review the template's permissions and remove access to unrelated resources. See [Cloudflare's GitHub Actions setup](https://developers.cloudflare.com/workers/ci-cd/external-cicd/github-actions/) and [permission reference](https://developers.cloudflare.com/fundamentals/api/reference/permissions/). A token created for another integration may not have these permissions; the first deployment must verify them.

Enter the token through the hidden terminal prompt:

```sh
gh secret set CLOUDFLARE_API_TOKEN --repo yamz8/pokachy --env production
```

Do not paste it into chat, commit it, or use a laptop's Wrangler OAuth token as a permanent CI credential.

## Deploy

Push the reviewed change to `main`. Inspect pending migration SQL in `server/migrations/` against production's migration list:

```sh
cd server
npx --no-install wrangler d1 migrations list pokachy --env production --remote
```

From the repository root, dispatch **Deploy production** in GitHub Actions, selecting `main` and confirming migration review, or run:

```sh
gh workflow run deploy.yml --repo yamz8/pokachy --ref main -f migrations_reviewed=true
```

Only confirm after reviewing pending migrations, including when there are none. Migrations must be additive and remain compatible with the currently deployed code. Destructive changes require a separate staged migration and recovery plan. Do not run manual deployments concurrently with this workflow.

The workflow pins the checkout to the dispatch commit, runs `npm run verify` and a production dry run without deployment credentials, then uses the production environment token for remote operations. It uploads the prior deployment metadata, a D1 Time Travel bookmark, and pending migration list **before** applying migrations. It deploys the tested commit with `DEPLOY_REVISION`, preserves existing Worker secrets, and verifies the live revision and public endpoints. Deployments are serialized and never canceled halfway through migrations. Pushes and release tags do not deploy production.

Download the verification and recovery artifacts from the run. A failed post-deploy check means production may have changed; inspect before retrying. Dry-run success validates packaging, not live permissions or integrations.

## Code rollback and database recovery

1. Stop dispatching deployments and inspect the failed run plus its `recovery-*` artifact.
2. Identify the previously active version ID in `deployments-before.json`; confirm it was a single-version deployment and remains compatible with the current database schema and bindings.
3. From `server/`, roll back to that explicitly selected version:

```sh
npx --no-install wrangler rollback PREVIOUS_VERSION_ID --env production --message "Recovery from failed deployment"
```

4. From the repository root, run `npm run verify:live -- --expected-revision PREVIOUS_GIT_SHA`. For a deployment predating revision metadata, omit that option and separately verify its Cloudflare version ID.

Code rollback does not reverse D1 changes. Cloudflare also restricts rollback across Durable Object lifecycle changes and incompatible resource changes; inspect [rollback limitations](https://developers.cloudflare.com/workers/versions-and-deployments/rollbacks/) before executing it.

`d1-before.json` records a recovery bookmark, **not an exported backup**. On Workers Paid, D1 Time Travel retains recovery history for 30 days. A database restore can discard newer user writes, so it is never automated here: stop writes, assess data loss, and obtain explicit owner authorization before a restore. Follow [D1 Time Travel](https://developers.cloudflare.com/d1/reference/time-travel/) using the recorded bookmark. Recovery artifacts are retained for 30 days; their existence does not prove a restore has been rehearsed.

## Administrator

After the owner's email is verified, configure the Worker secret `ADMIN_USER_IDS` with their Better Auth user ID (comma-separated for multiple administrators). Use Wrangler's hidden `secret put` prompt or the Cloudflare dashboard; never commit the IDs or pass them in command arguments. Authorization uses IDs rather than claimed handles or emails. Endpoints: `GET /api/admin/reports` and `POST /api/admin/suspend/:handle`. Suspension revokes all that user's sessions.

### Report review and appeals

Use authenticated requests from a configured administrator, with credentials read from private storage rather than pasted into commands or logs. Review reports daily during the pilot. Do not suspend a user solely because a report exists; inspect the behavior and use the least disruptive response justified by it.

| Request | Behavior |
| --- | --- |
| `GET /api/admin/reports?page=true&status=open` | Up to 50 open reports and `next_cursor`; send the cursor as URL-encoded `before` to continue. `status=resolved` and `status=all` also work. |
| `POST /api/admin/reports/:id/resolve` with `{}` | Close a reviewed report. Returns 404 if missing or already closed. |
| `POST /api/admin/reports/:id/reopen` with `{}` | Reopen a closed report for further review. |
| `POST /api/admin/suspend/:handle` with `{}` | Suspend another account and revoke every session. |
| `POST /api/admin/unsuspend/:handle` with `{}` | Lift a suspension. The person must sign in again; revoked sessions stay invalid. |

The original unpaginated report endpoint remains an array of the latest 100 open reports for compatibility. Use pagination for complete review. Keep review rationale and appeal correspondence in restricted operator records, never a public issue. Resolve the relevant reports after acting; suspension alone does not close them. Acknowledge private appeals through the verified support inbox once configured. Configured administrators cannot delete their account until an operator transfers responsibility and removes their ID from configuration.

### Account deletion and retention

The updated account deletion page requires a session created within five minutes, a six-digit email verification code, and the exact confirmation `DELETE MY ACCOUNT`. `POST /api/account/deletion-code` sends to the authenticated account's verified email (maximum three per five minutes); `POST /api/account/delete` verifies the code (maximum five attempts per five minutes) and deletes the active data in one D1 transaction. Suspended accounts can still delete after authenticating. OAuth/device access alone does not replace the independent email-code check.

Deletion removes sessions, linked accounts, profile, friendships, blocks, shared poke history, reports involving the account, associated device codes, and current known email OTP records. Friends are asked to refresh and live sockets are revalidated. Existing queued emails, provider logs, recovery history, and offline desktop caches are not remotely erased. A newly registered account can reuse a deleted handle; deletion is not a permanent identity ban. Reports have no independent archive after an involved account is deleted.

An hourly production Cron Trigger runs bounded cleanup of expired sessions, verification records, device codes, and local development mail; inactive authentication rate-limit rows become eligible after 24 hours. Each category removes at most 1,000 rows per run. Investigate cleanup failures or backlogs using `expired_auth_cleanup` events. Poke history and resolved reports have no automatic age-based deletion; the public privacy page states that policy.

After restoring a database, previously deleted accounts and revoked sessions may reappear. Before reopening writes, identify and reapply deletions and moderation changes made after the recovery point, revoke restored sessions, and verify the result. Never treat a successful restore as permission to reactivate old credentials. If the necessary deletion/incident record is unavailable, keep the restored service isolated and escalate the recovery decision.

## Releases

Keep root/server package versions, CLI version, and manifest version aligned. Pushes and pull requests run CI. A `v0.1.0`-style tag validates and publishes Linux amd64/arm64 archives plus `SHA256SUMS`. A tag does not deploy the Worker or migrate production data.

Local packaging: `scripts/package-release.sh v0.1.0`. Archives contain the CLI, installer, user service, and plugin. Omarchy can also install the Git repository directly.

## Checks and recovery

Verify `/health`, email delivery, GitHub callback, device approval, and a poke between two accepted friends. Revocation must reject the old device session; blocking must prevent further requests/pokes. The development code endpoint must remain unavailable in production.

Inspect Workers logs and Queue metrics. Delivery retries stop when the code expires; exhausted retries go to the dead-letter queue. Never log codes, tokens, secrets, or email bodies. D1 recovery is independent of code rollback, so inspect migrations before reverting a release.

Stop desktop notifications with `systemctl --user disable --now pokachy.service`; revoke this computer with `pokachy logout`. Account settings can revoke other signed-in devices.

## Read-only production monitoring

`.github/workflows/monitor.yml` invokes `scripts/verify-live.py` every 30 minutes, and supports a manual dispatch. It uses only public unauthenticated HTTPS requests and `contents: read`; it has no production environment, Cloudflare credential, deployment permission, or secret. The monitor creates no account, email, poke, or remote mutation. It uploads the fresh `artifacts/live-verification.json` report for 30 days even when a check fails.

Assign an operations owner to receive GitHub Actions workflow-failure notifications and test that delivery. Those notifications and the workflow artifact are diagnostic evidence, not external availability alerting. Still configure and test independent alerts in Cloudflare or the chosen monitoring service for Worker errors/availability, D1 errors and capacity, queue retries and depth, `pokachy-mail-failed` dead-letter queue growth, Email Sending failures, domain/DNS expiry, and Cloudflare/GitHub billing or quota events. This repository does not show that any dashboard, billing, or DLQ alert has been configured.

On a failed smoke run, download its report, check the preceding successful report, inspect Worker logs and DNS, and assess whether the condition is transient. Do not auto-deploy, roll back, restore D1, or retry migrations based solely on a monitor result. If a recent deployment is involved, use the recovery artifact and the code rollback procedure above. A recovery rehearsal is still required; [launch readiness](launch-readiness.md) gives a safe isolated procedure.
