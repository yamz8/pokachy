# Launch readiness

Pokachy is not declared ready for a broad public launch by this document. It records what is published, the checks that can be repeated safely, and the operational work that still needs an owner.

## Published release

`v0.1.8` was published on 2026-09-15 from commit `abc664a604392c3ba29272690a1eed4684f40eac` (the tagged `main` commit). Production was verified at that same revision. Its published Linux assets are:

| Architecture | Asset | SHA-256 |
| --- | --- | --- |
| amd64 | `pokachy_linux_amd64.tar.gz` | `061d3ff367c133f824d8eca17ec1870e52e31cce165a0f7ee98a106d4ce7f0cd` |
| arm64 | `pokachy_linux_arm64.tar.gz` | `af4d8e5bac50d8801d090a9fc0cb4d7080ee41f37fa3330976c6822473a63873` |

Download the archive and `SHA256SUMS` from the [v0.1.8 GitHub release](https://github.com/yamz8/pokachy/releases/tag/v0.1.8), then verify the downloaded file before extracting it:

```sh
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf pokachy_linux_amd64.tar.gz
cd pokachy_0.1.8_linux_amd64
bash scripts/install.sh
pokachy version
```

Use the `arm64` filename on ARM Linux. The recorded amd64 artifact was isolated-install verified; arm64 was cross-build and content verified, not executed on ARM hardware. Re-check the release's `SHA256SUMS` before every install; copied checksums in this document are release records, not a substitute for verifying the downloaded manifest.

## Readiness check — 2026-09-16 (Israel)

- The public bootstrap at `https://pokachy.com/install.sh` downloaded, checksum-verified, and installed the published `0.1.8` amd64 CLI into disposable directories. No desktop service or account was changed. Evidence: `artifacts/public-install-verification.json` (ignored local report).
- CI, release, and production deployment succeeded for `abc664a`; the read-only smoke check verified that exact deployed revision and all seven public checks. The local Omarchy fix subsequently passed all seven stages of `npm run verify` (report started `2026-09-15T23:54:24Z`, exit 0, 16 Worker tests).
- The live browser rendered sign-in, installation, privacy, support, and the signed-out deletion page. The install page's Omarchy copy control worked; installation and support layouts had no horizontal overflow at 390px. This does not establish email delivery, GitHub consent, or authenticated deletion.
- A separate live email sign-in succeeded using an owner-authorized inbox and a received code. Handle creation, matching device-code approval, and CLI connection all completed. The embedded browser's Turnstile frame initially failed, then passed automatically after one reload. GitHub consent was not re-tested.
- A local, unpublished Omarchy fix adds `~/.local/bin` as a fallback for CLI processes while preserving custom PATH precedence. A disposable Quickshell process fixture verified fallback, precedence, and literal argument handling. The full QML parsed and passed Omarchy manifest validation; the fixture did not render the complete installed panel.
- Two explicitly authorized production accounts accepted a friendship and exchanged pokes with the published `0.1.8` CLI. A temporary companion daemon displayed a real Omarchy toast, visually inspected in a private capture. The return poke reached the existing account while its quiet setting stayed enabled. The installed desktop daemon was active/running with zero automatic restarts. This was two accounts on one physical desktop; historical two-desktop checks are recorded in `docs/handoff.md`.
- A fresh disposable local SQL recovery exercise passed on 2026-09-15 UTC, including restored-session revocation, re-deletion, and zero foreign-key violations. Evidence: `artifacts/recovery-rehearsal.json`. Hosted Worker rollback and D1 Time Travel remain untested here.

The fresh two-account smoke test passed on one physical Omarchy desktop. Publish the reviewed Omarchy fix before inviting users who need its PATH fallback. A fresh install on a second physical desktop remains a useful pilot check. Keep test inboxes and credentials out of this public record.

## Invite-only pilot

Keep the initial cohort small and individually invited. There are no pilot participants yet; recruiting an explicitly invited initial cohort remains a launch gate. Before each invitation, confirm the person has a supported Linux desktop, `~/.local/bin` on `PATH`, `notify-send`, and a compatible notification service. Give them the exact release link and checksum procedure above, not an unpinned repository snapshot.

For each pilot participant, record consent, installation architecture, release tag, checksum result, onboarding outcome, service status, and the support route agreed for the pilot. Private support is available at `support@pokachy.com`: Cloudflare routing is enabled, the forwarding destination is verified, and the owner confirmed successful inbox delivery after sending a test email. Ask participants to test email sign-in or GitHub sign-in, device-code matching, adding and accepting one friend, one received poke, quiet mode, blocking, logout, and service restart. Do not ask participants to share device codes, session tokens, email codes, screenshots containing them, or account data beyond what is needed for their support request.

Before widening the pilot, review support volume, delivery failures, reports, account/session revocation behavior, and the monitoring checks below. A real moderation exercise requires explicitly authorized disposable accounts: create no report or suspension against an owner or ordinary pilot account. A positive suspension revokes the target's sessions, so document consent and the expected recovery path first.

## Read-only monitoring and response

On 2026-09-14, the enabled Cloudflare policy **Pokachy infrastructure incidents** was configured for minor, major, and critical incidents affecting Workers, Workers Assets, D1, Durable Objects, Queues, Email Routing, Email Sending, Authoritative DNS, and DNS Updates. Its recipient is the owner's designated private operations inbox. The policy was read back successfully, Cloudflare accepted its test notification, and the owner confirmed the test email arrived. This covers Cloudflare-reported infrastructure incidents; it does not detect Pokachy's own application errors, queue backlog, or delivery failures.

The default branch includes `.github/workflows/monitor.yml`, scheduled every 30 minutes and also available for manual dispatch. Its [scheduled run on 2026-09-15 at 22:12 UTC](https://github.com/yamz8/pokachy/actions/runs/35029733866) succeeded, as did the preceding two observed scheduled runs. It invokes `scripts/verify-live.py`. It makes only unauthenticated public HTTPS requests to `https://pokachy.com`; it creates no account, email, poke, deployment, secret, or Cloudflare API request. Each run writes and retains `artifacts/live-verification.json` as a GitHub Actions artifact for 30 days. GitHub schedules may be delayed; this is a basic smoke monitor, not a real-time availability guarantee.

The workflow fails when health, public configuration, production development-mail isolation, unauthenticated access control, homepage, or public assets fail. It uses no production secrets and has only `contents: read` permission. A scheduled failure should produce the normal GitHub Actions workflow-failure notification for watchers/subscribers whose GitHub notification settings allow it. Configure and test those notifications for the operations owner; GitHub workflow notifications and a retained report are not a substitute for independently configured external paging or alerting.

On a monitor failure, download the report, compare it with the prior successful run, check Cloudflare Workers status/logs and DNS, and decide whether the condition is transient. Do not automatically deploy, roll back, retry migrations, or expose credentials from a smoke failure. Follow the recovery procedure in [deployment](deployment.md) if a deployment is implicated.

## Recovery rehearsal

Run `python3 scripts/rehearse-recovery.py` for the disposable local SQL exercise. It uses the real initial schema, backs up synthetic account/session records, demonstrates how restore resurrects deleted data, then reapplies deletion and session revocation. Its fresh report is `artifacts/recovery-rehearsal.json`. This does not exercise Cloudflare Worker version rollback or D1 Time Travel; the hosted rehearsal below remains separate.

The repository has deployment recovery metadata and a documented rollback path. The disposable local SQL rehearsal passed on 2026-09-15 UTC; a hosted rollback/Time Travel rehearsal has not been recorded. Rehearse only in an isolated disposable environment or against a deliberately selected non-production Worker and database. Never restore the production D1 database, use real inboxes, or suspend real accounts for a rehearsal.

1. Create disposable test accounts and an isolated Worker/D1/queue configuration with private test inboxes or the local development mail path.
2. Deploy a known-compatible revision, record its version ID and a D1 recovery point, then make an additive test change that has a documented rollback path.
3. Perform the documented code rollback to the exact prior version, run `npm run verify:live -- --base-url https://ISOLATED_ORIGIN --expected-revision PRIOR_GIT_SHA` against the isolated origin, and verify the old device session and public endpoints behave as expected.
4. Practice the decision process for D1 recovery without executing a production restore: identify writes that would be lost, the owner authorized to approve it, and the user communication needed.
5. Save the date, operator, version IDs, observations, and gaps in an internal run record. Remove the disposable accounts and resources after evidence is retained.

## Outstanding launch gates

- The owner receives verified Cloudflare infrastructure incident alerts. Add application-specific alerts for Worker availability/errors, D1 errors and capacity, queue depth/retries, dead-letter queue growth, email-delivery failures, domain expiry, and Cloudflare/GitHub billing or quota events. Those separate rules are not yet configured. Inspection of the available Workers Observability dashboard and API did not expose a rule-creation control; a notification policy alone would not establish an application-error trigger.
- Test that the GitHub Actions failure notification reaches the responsible operator. Scheduled smoke runs have been observed succeeding; failure-notification delivery has not been verified.
- Complete the isolated recovery rehearsal above and keep the result with the deployment record.
- Complete positive report, moderation, suspension, and session-revocation validation only with disposable accounts explicitly authorized for that purpose.
- Complete live authenticated account-deletion and moderation validation with authorized disposable accounts. Privacy/support/deletion pages are deployed and were reviewed in the browser; support forwarding and inbox delivery were previously verified. Operational alert configuration and delivery remain separate outstanding checks.
- Recruit the first explicitly invited pilot cohort and confirm that its agreed support route works before distributing the published release.
- Re-test live OAuth consent, email delivery, inbox delivery, and desktop notifications for the intended pilot environment. The read-only smoke monitor cannot prove them.
