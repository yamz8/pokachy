# Verification

After `npm ci`, run from the repository root:

```sh
npm run verify
```

Requirements: Linux, Node.js 22+, Go 1.24+, and Python 3.10+. The first run may download Go dependencies. The suite needs permission to bind local loopback sockets; it does not need Cloudflare login or production secrets.

The command exits nonzero on a failure and writes `artifacts/verification.json`, including each completed check, duration, and exit code. A failed check stops the suite. The report is ignored by Git and contains no account tokens or email codes.

Checks run in order:

1. Generate Worker binding types and run TypeScript checking.
2. Run the Worker integration suite: email verification, device approval, friendship consent, concurrent and idempotent pokes, blocking, session revocation, Turnstile rejection, and mail queue retries.
3. Run Go unit tests and static analysis.
4. Start a separate local Worker and exercise the compiled native CLI and daemon against it.

The native end-to-end check uses a temporary database, a free loopback port, fake `example.test` accounts, private temporary CLI configuration, and a mock desktop notifier. It checks account/device onboarding, friendship approval, poke delivery into the daemon cache, notifications, quiet mode, restart deduplication, and revoked credentials. Temporary resources are removed on completion or failure. It does not use or reset your normal development database, send real email, install a service, or modify your desktop.

Run only the native integration check with:

```sh
npm run verify:e2e
```

GitHub Actions runs this check on pushes, pull requests, and before publishing releases, alongside the Worker and Go tests. Failures prevent a green CI result.

## What remains a live check

A passing local suite does not prove delivery by an external email provider, GitHub OAuth configuration, production DNS, browser rendering, or compatibility with a real Omarchy shell/notification server. Verify those when changing their integration:

- Complete email and GitHub sign-in at the deployed site.
- Compare and approve the device code displayed by `pokachy init`.
- Confirm a real desktop notification and the Omarchy panel after an accepted friend pokes you.
- Check `/health` and that `/api/dev/mail` returns 404 in production.

Use only accounts authorized for live testing. Never add a production OTP bypass or store live credentials in test fixtures.
