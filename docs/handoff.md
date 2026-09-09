# Pokachy handoff — 2026-09-09

## Session boundary

This session covers CI/CD and verification only. Stop before desktop installation, Omarchy validation, or other launch features. The owner prefers bounded Luna reviews and Terra implementation, with the primary agent responsible for integration, security, and final verification.

## Implemented

- Push/PR CI checks Worker types/tests, Go tests/vet/build, native CLI integration, both release archives, isolated installation, and the production smoke checker's fixtures.
- Release tags run the full verification suite before publishing assets.
- `deploy.yml` provides manual, serialized production deployment from a tested immutable `main` commit, with migration-review attestation, required-secret preflight, recovery artifacts before migrations, and a live revision check afterward.
- GitHub's `production` environment is configured to allow only branch `main`.
- `/health` now reads D1 and reports deployment revision; database errors return a generic 503. This is live in production at revision `d89ed1af2d6b95e5cf2c6782bbe8f630b2f0c40a`.
- `npm run verify:live` checks production read-only and writes a fresh report.
- Deployment credential setup, migration review, code rollback, and separate D1 recovery are documented in `docs/deployment.md`.

## Verification in this session

- `npm run verify`: passed all seven stages; eight Worker tests, six smoke-checker tests, installer checks, and native CLI integration. Fresh local report: `artifacts/verification.json`, started `2026-09-09T03:32:29Z`.
- Production Wrangler dry run: passed.
- actionlint 1.7.7: passed on the final workflows, including the required-secret preflight.
- Live read-only smoke: all seven checks passed after deployment and `/health` returned the exact expected Git revision.
- Luna reviewed workflow credential scope, commit identity, migration/recovery ordering, and smoke failure behavior; no material findings. Pinning third-party action references to immutable SHAs remains optional hardening.
- Two earlier deployment runs stopped safely during verification before touching production. Their device-flow failures matched a five-second idle-connection race documented in Wrangler 4.129.1's development proxy. The disposable test fixture now keeps that proxy active with read-only health requests while waiting for device approval; it does not retry one-time token redemption or change production behavior.

## Production deployment completed

The owner created a dedicated Cloudflare token and stored it as the GitHub `production` environment secret `CLOUDFLARE_API_TOKEN`. Keep it private and rotate it through the hidden `gh secret set` prompt documented in `docs/deployment.md`; do not copy laptop OAuth credentials into CI or expose tokens in chat.

GitHub Actions run `34307601788` completed the first end-to-end production deployment successfully. It verified all seven local stages, passed the production dry run, confirmed required Worker secrets, saved pre-deployment Worker and D1 recovery metadata, found no pending migration, deployed the tested commit, and passed its revision-aware live smoke check. The retained run artifacts are `verification-d89ed1af2d6b95e5cf2c6782bbe8f630b2f0c40a`, `recovery-34307601788-1`, and `live-verification-34307601788-1`. A separate read-only smoke run also confirmed that production reports the exact deployed revision and all seven public checks pass.

CI/CD is operational. A recovery rehearsal has not been completed; follow `docs/deployment.md` before relying on rollback during an incident. Continue reviewing every pending migration before dispatching `deploy.yml`.

## Next session: release readiness

Reassess the repository before implementing these; this is a handoff, not a claim they are complete:

1. Validate the release installer, user service, real notifications, and native panel on Omarchy. Current tests use temporary installation paths and a mock notifier; ARM archives are built, not executed on ARM hardware.
2. Complete GitHub OAuth consent/linking and browser device approval against production with authorized accounts. Email sign-in was previously exercised with the owner's authorized inbox. Never assume local laptop setup identity proves email ownership.
3. Configure the owner's administrator user ID and verify reporting/suspension operations.
4. Review account deletion, privacy/retention, abuse controls, support contact, monitoring/alerts, and recovery rehearsal before public launch.
5. Run a small authorized pilot and fix failures before publishing broadly.

The app is not yet declared production-ready. Keep credentials in ignored private files, reports in ignored artifacts, and test fixtures local/disposable. Run `npm run verify` after authentication/API/CLI/daemon/integration changes, with real browser or desktop validation where applicable.
