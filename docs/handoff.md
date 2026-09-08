# Pokachy handoff — 2026-09-09

## Session boundary

This session covers CI/CD and verification only. Stop before desktop installation, Omarchy validation, or other launch features. The owner prefers bounded Luna reviews and Terra implementation, with the primary agent responsible for integration, security, and final verification.

## Implemented

- Push/PR CI checks Worker types/tests, Go tests/vet/build, native CLI integration, both release archives, isolated installation, and the production smoke checker's fixtures.
- Release tags run the full verification suite before publishing assets.
- `deploy.yml` provides manual, serialized production deployment from a tested immutable `main` commit, with migration-review attestation, required-secret preflight, recovery artifacts before migrations, and a live revision check afterward.
- GitHub's `production` environment is configured to allow only branch `main`.
- `/health` now reads D1 and reports deployment revision; database errors return a generic 503. This change becomes live only after the next deployment.
- `npm run verify:live` checks production read-only and writes a fresh report.
- Deployment credential setup, migration review, code rollback, and separate D1 recovery are documented in `docs/deployment.md`.

## Verification in this session

- `npm run verify`: passed all seven stages; eight Worker tests, six smoke-checker tests, installer checks, and native CLI integration. Fresh local report: `artifacts/verification.json`, started `2026-09-08T22:06:44Z` (September 9 in Israel).
- Production Wrangler dry run: passed.
- actionlint 1.7.7: passed on the final workflows, including the required-secret preflight.
- Live read-only smoke: all seven checks passed on the existing deployment. This is not proof of deployment through GitHub or of the new revision metadata.
- Luna reviewed workflow credential scope, commit identity, migration/recovery ordering, and smoke failure behavior; no material findings. Pinning third-party action references to immutable SHAs remains optional hardening.

## Immediate remaining CI/CD step

GitHub has no Cloudflare deployment API token yet. The connected Cloudflare API cannot create one (authorization scope failure). Have the owner create a dedicated scoped token and enter it using the hidden `gh secret set` prompt in `docs/deployment.md`. Do not copy laptop OAuth credentials into CI or expose tokens in chat.

Once set, dispatch `deploy.yml` on main after reviewing pending migrations. Observe the entire run, inspect verification/recovery artifacts, and confirm `/health` matches the dispatched SHA. Resolve any token-permission failures before claiming CD works end to end. The first real GitHub deployment and a recovery rehearsal have not been completed.

## Next session: release readiness

Reassess the repository before implementing these; this is a handoff, not a claim they are complete:

1. Validate the release installer, user service, real notifications, and native panel on Omarchy. Current tests use temporary installation paths and a mock notifier; ARM archives are built, not executed on ARM hardware.
2. Complete GitHub OAuth consent/linking and browser device approval against production with authorized accounts. Email sign-in was previously exercised with the owner's authorized inbox. Never assume local laptop setup identity proves email ownership.
3. Configure the owner's administrator user ID and verify reporting/suspension operations.
4. Review account deletion, privacy/retention, abuse controls, support contact, monitoring/alerts, and recovery rehearsal before public launch.
5. Run a small authorized pilot and fix failures before publishing broadly.

The app is not yet declared production-ready. Keep credentials in ignored private files, reports in ignored artifacts, and test fixtures local/disposable. Run `npm run verify` after authentication/API/CLI/daemon/integration changes, with real browser or desktop validation where applicable.
