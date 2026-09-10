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

## Releases

Keep root/server package versions, CLI version, and manifest version aligned. Pushes and pull requests run CI. A `v0.1.0`-style tag validates and publishes Linux amd64/arm64 archives plus `SHA256SUMS`. A tag does not deploy the Worker or migrate production data.

Local packaging: `scripts/package-release.sh v0.1.0`. Archives contain the CLI, installer, user service, and plugin. Omarchy can also install the Git repository directly.

## Checks and recovery

Verify `/health`, email delivery, GitHub callback, device approval, and a poke between two accepted friends. Revocation must reject the old device session; blocking must prevent further requests/pokes. The development code endpoint must remain unavailable in production.

Inspect Workers logs and Queue metrics. Delivery retries stop when the code expires; exhausted retries go to the dead-letter queue. Never log codes, tokens, secrets, or email bodies. D1 recovery is independent of code rollback, so inspect migrations before reverting a release.

Stop desktop notifications with `systemctl --user disable --now pokachy.service`; revoke this computer with `pokachy logout`. Account settings can revoke other signed-in devices.
