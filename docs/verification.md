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
4. Test the read-only deployment smoke checker against an isolated HTTP fixture.
5. Build both release archives and verify the installer in isolated directories.
6. Start a separate local Worker and exercise the compiled native CLI and daemon against it.

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

## Release installer check

```sh
npm run verify:install
```

This builds fresh Linux release archives in a temporary directory, checks their checksums and required contents, and runs the archived installer into isolated directories. It verifies the installed native binary and the generated service path, including a custom prefix containing spaces and `%`. Attempts to start services or send desktop notifications fail the check.

This command requires Go, Python, and the standard Linux packaging tools. It does not install Pokachy onto your desktop. Running the installed daemon, validating the real Omarchy panel, and testing an ARM binary on ARM hardware remain separate checks.

## Production smoke check

```sh
npm run verify:live
npm run verify:live -- --expected-revision FULL_GIT_SHA
```

This makes read-only requests to `https://pokachy.com`: health, public authentication configuration, disabled development mail endpoint, unauthenticated state, homepage, and assets. It writes a fresh timestamped `artifacts/live-verification.json` and exits nonzero on failure. It refuses redirects and retries transient failures within a shared deadline. It creates no accounts, emails, or pokes. With an expected revision it also rejects an older deployment. A passing result does not prove browser appearance, OAuth consent, inbox delivery, or desktop integration.

CI and release gates include the installer and smoke-checker fixture tests. The manually dispatched production deployment runs the full suite, then saves recovery metadata, migrates, deploys, and runs the live check. See [deployment.md](deployment.md).
