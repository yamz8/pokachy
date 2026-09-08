# Deployment

Production is `server/wrangler.jsonc` → `production`, Worker `pokachy`, origin `https://pokachy.com`. Cloudflare manages DNS; GoDaddy can remain the registrar. Workers Paid supports login emails to open-registration users.

## Credentials

Run `python3 scripts/configure-secrets.py` to enter the GitHub OAuth Client ID and Client Secret. It generates `BETTER_AUTH_SECRET` and writes `.secrets/production.json` with private permissions. Git ignores this directory. Never put secrets in commits, command arguments, or issue reports.

GitHub OAuth callback: `https://pokachy.com/api/auth/callback/github`. The CLI uses Pokachy's device flow; GitHub's separate device flow is unnecessary. Store the managed Turnstile widget secret as `TURNSTILE_SECRET`. The public sitekey belongs in production vars. Production `TURNSTILE_HOSTNAMES` must contain only `pokachy.com`.

## Resources

Production requires D1 database `pokachy`, Durable Object `UserHub`, queues `pokachy-mail` and `pokachy-mail-failed`, and Email Sending enabled for `pokachy.com`. Bind email as `EMAIL` and the producer queue as `MAIL_QUEUE`.

For another account, create these resources with the project's Wrangler, replace account/database IDs in the configuration, and onboard the sending domain. Verify DNS before testing with a real inbox you control.

## Deploy

From the repository root:

```sh
npm ci
npm run check
npm test
npm run test:cli
npm run deploy:check --workspace server
```

From `server/`:

```sh
npx wrangler d1 migrations apply pokachy --env production --remote
npx wrangler deploy --env production --secrets-file ../.secrets/production.json
```

Secrets are uploaded alongside code. Later deployments preserve existing secrets unless explicitly changed. A dry run validates packaging, not DNS, email delivery, or OAuth callbacks.

## Administrator

After the owner's email is verified, configure `ADMIN_USER_IDS` with their Better Auth user ID (comma-separated for multiple administrators). Authorization uses IDs rather than claimed handles or emails. Endpoints: `GET /api/admin/reports` and `POST /api/admin/suspend/:handle`. Suspension revokes all that user's sessions.

## Releases

Keep root/server package versions, CLI version, and manifest version aligned. Pushes and pull requests run CI. A `v0.1.0`-style tag validates and publishes Linux amd64/arm64 archives plus `SHA256SUMS`. A tag does not deploy the Worker or migrate production data.

Local packaging: `scripts/package-release.sh v0.1.0`. Archives contain the CLI, installer, user service, and plugin. Omarchy can also install the Git repository directly.

## Checks and recovery

Verify `/health`, email delivery, GitHub callback, device approval, and a poke between two accepted friends. Revocation must reject the old device session; blocking must prevent further requests/pokes. The development code endpoint must remain unavailable in production.

Inspect Workers logs and Queue metrics. Delivery retries stop when the code expires; exhausted retries go to the dead-letter queue. Never log codes, tokens, secrets, or email bodies. D1 recovery is independent of code rollback, so inspect migrations before reverting a release.

Stop desktop notifications with `systemctl --user disable --now pokachy.service`; revoke this computer with `pokachy logout`. Account settings can revoke other signed-in devices.
