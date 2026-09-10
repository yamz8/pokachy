# Pokachy handoff — 2026-09-09

## Session boundary

This handoff began with CI/CD verification and now also records the completed Omarchy desktop installation and release validation. The owner prefers bounded Luna reviews and Terra implementation, with the primary agent responsible for integration, security, and final verification.

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

## Desktop validation completed — 2026-09-09

- Built and installed the initial `0.1.0` desktop artifacts, then prepared the aligned `0.1.1` release candidate after the desktop gate passed. No release was published during desktop validation.
- Production CLI onboarding was completed as `@yamz8` on this machine and `@edwin` on the second laptop. Both installed systemd user services are enabled, active, and remained at zero restarts during the final checks.
- Fixed CLI onboarding hints being double-escaped in the activation URL, which prevented the browser from prefilling email, handle, and name. Added regression coverage for reserved characters and rebuilt/reinstalled the corrected artifact on both machines.
- Installed the public repository through `omarchy plugin add ... --enable`. The `com.pokachy.poke` widget is enabled in the right bar and rendered live owner, online, friend-request, friend, and waiting-inbox states without Pokachy QML errors.
- With Omarchy Do Not Disturb temporarily disabled, `@edwin` sent a production poke from the second laptop. This machine rendered the real desktop toast, cached the matching inbox item, and persisted its ID in `notified.json`. Restarting the daemon did not replay the notification. The test poke was dismissed and DND was restored to its original `on` state.
- A fresh `0.1.1` candidate `npm run verify` passed all seven stages. Report: `artifacts/verification.json`, started `2026-09-09T08:44:30Z`.

The x86_64 release installer, onboarding, user service, real notification path, and native panel have now passed on actual Omarchy desktops. The arm64 archive is still build-verified only.

## Release v0.1.1 published

- Commit `202f3b4a6ee648512e822ee930a59c083face6ad` passed all four GitHub CI jobs in run `34331377685` before tagging.
- The guarded release workflow run `34331518475` reran the full verification suite, rebuilt the archives, and published `v0.1.1` as the latest GitHub release on `2026-09-09T08:54:01Z`. It did not deploy the Worker.
- Downloaded all three public assets and verified both archives against the published `SHA256SUMS`. The amd64 and arm64 archives are present; real arm64 hardware execution remains outstanding.
- Installed the exact public amd64 binary on both Omarchy laptops. Both copies have SHA-256 `0d436b17ce6a35bb3fde3a9361d9a50f6dcaff182d6be967e4151c1ae892d30a`, retained their authenticated sessions, and run under active enabled user services.
- Repeated the production two-machine poke with the public binaries and visually confirmed the real Omarchy notification. The test inbox was cleared, DND restored to `on`, and this machine's installed panel updated to manifest `0.1.1` at the release commit.

## v0.1.2 connection-race follow-up

- Documentation-only CI run `34332071989` exposed the known five-second Wrangler idle-close race again: device approval and health checks succeeded, but the CLI's first token POST reused a connection as the development proxy closed it. No credentials were saved and the Worker remained healthy.
- The CLI now closes idle HTTP connections before each device-token attempt. This starts the one-time POST on a fresh connection without retrying redemption or changing authentication semantics. The end-to-end fixture comment now describes the actual protection.
- Prepared aligned version `0.1.2`. A fresh `npm run verify` passed all seven stages with report start `2026-09-09T09:05:18Z`, followed by three additional consecutive successful `npm run verify:e2e` runs.

## Release v0.1.2 published

- Commit `3979fae11db702f85ba90b4c96b4c87b933435b3` passed all four GitHub CI jobs in run `34332876307` before tagging.
- Before publication, the exact candidate passed fresh production onboarding in an isolated config, authenticated as `@yamz8`, and the temporary device was then revoked. Both Omarchy laptops ran the candidate under enabled active user services with zero restarts.
- The candidate passed a real production poke from `@edwin` to `@yamz8`: the Omarchy toast was visually confirmed, the native panel rendered the pending inbox item and actions, and a service restart preserved the notification ledger without replaying the toast. The installed panel is manifest `0.1.2` at the release commit with no Pokachy QML errors.
- Tag CI run `34335012648` and guarded release workflow run `34335012584` both passed at the same commit. The release workflow reran all seven verification stages, rebuilt the archives, and published `v0.1.2` on `2026-09-09T09:31:35Z`. It did not deploy the Worker.
- Downloaded all three public assets and verified both archives against the published `SHA256SUMS`. The public archive digests are `553702b1596b15666b5fd0e31ce7199415edb23fe37b60131d7ad4348635d4ae` for amd64 and `5ef6145b8b39e08c78dbd8eec549e54b94d414f512d4f5248e1306c189f28ff0` for arm64.
- Installed the exact public amd64 binary on both laptops. Both copies have SHA-256 `44044f9056aedd1fe60cf4fcff5b76ef6b1eb898200117a04fe86b211b0ea7d0`, retained their authenticated sessions, and passed another visually confirmed production poke. The test inbox was cleared and Omarchy DND was explicitly restored to its original `on` state.

## v0.1.3 native-panel follow-up — 2026-09-10

- The panel now gives immediate `Sending…` feedback, disables a poke while it is unanswered, shows `Waiting` until the recipient answers or dismisses it, and displays CLI success output. This matches the server's one-outstanding-poke rule instead of leaving an apparently inert `Poke` button.
- Standard controls are keyboard reachable through Qt's native Tab chain, Return/Enter activates them, and Escape closes the panel. Live keyboard checks covered Quiet/Resume, Add, Poke, Back, Dismiss, Remove, Cancel, Accept, Decline, Block, and Unblock; invalid friend input rendered the CLI error without losing focus.
- CLI processes are launched through `/usr/bin/env` with argument arrays. A missing executable now produces a friendly installed-CLI error rather than looking logged out, and the live watcher automatically clears the error and reconnects when the executable returns.
- Logged-out, missing-CLI, and recovery states rendered in the actual Omarchy shell. Both authorized accounts were exercised through the full friendship state matrix and restored as mutual friends with empty inboxes and block lists.
- A real production poke from `@edwin` produced the Omarchy desktop toast and live native inbox. The exact `0.1.3` amd64 candidate was installed on both laptops (binary SHA-256 `c657ebb03f7f0cd3d598fe134e87b6dcdb11e86b9df22d8c7dc2ce4ea72c9270`), and its final toast/panel/dismiss round trip passed. Both user services are enabled and active with zero restarts; account quiet mode is off and Omarchy DND is restored to `on`.
- Fresh `npm run verify` passed all seven stages from report start `2026-09-10T09:08:50.671641+00:00`. The first sandboxed attempt stopped at `listen EPERM` because local binding was denied; the exact suite passed after granting the isolated Worker permission. No Pokachy QML errors appeared in the final shell logs. The arm64 archive remains build/install verified only.
- Commit `a8f62c49d28bbc0f38d9afd889db4f79e0fe8db1` passed all four GitHub CI jobs in run `34460674111` before tagging. Tag CI run `34460867159` and the guarded release run `34460867119` also passed at the same commit; the release workflow reran all seven verification stages before publishing `v0.1.3` at `2026-09-10T09:30:42Z`.
- Downloaded the three public release assets and verified both archives against the published `SHA256SUMS`. Public archive digests are `dd1763890c236a81685750fd64ef3a750e904756f8d600278daf3feedf94d5b4` for amd64 and `8bcff3dc48a5aac340ef2a15d1dd5a10bfaedfaf43efde4c38fed5951c45367c` for arm64.
- Installed the exact public amd64 artifact on both Omarchy laptops. Both binaries have SHA-256 `0f3cb98c0866272640a7c161e48115a08dcd4bbb130d98a37a28f31ee3956646`, retained their sessions, and run in enabled active services with zero restarts. A final public-binary production poke rendered the real toast and native inbox, keyboard Dismiss cleared it, both accounts ended clean and online, Pokachy quiet mode is off, and Omarchy DND is explicitly `on`.

## Next session: release readiness

Reassess the repository before implementing these; this is a handoff, not a claim they are complete:

1. Complete GitHub OAuth consent/linking against production with authorized accounts. Email sign-in and browser device approval are verified. Never assume local laptop setup identity proves email ownership.
2. Configure the owner's administrator user ID and verify reporting/suspension operations.
3. Review account deletion, privacy/retention, abuse controls, support contact, monitoring/alerts, and recovery rehearsal before public launch.
4. Run a small authorized pilot and fix failures before publishing broadly.
5. Execute the arm64 archive on real ARM hardware when available.

The app is not yet declared production-ready. Keep credentials in ignored private files, reports in ignored artifacts, and test fixtures local/disposable. Run `npm run verify` after authentication/API/CLI/daemon/integration changes, with real browser or desktop validation where applicable.
