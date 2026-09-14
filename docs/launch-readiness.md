# Launch readiness

Pokachy is not declared ready for a broad public launch by this document. It records what is published, the checks that can be repeated safely, and the operational work that still needs an owner.

## Published release

`v0.1.5` was published on 2026-09-13 from commit `2a94f7d5413bc99a31b6b96e88d24c374cb27dbc` (the tagged `main` commit). Its published Linux assets are:

| Architecture | Asset | SHA-256 |
| --- | --- | --- |
| amd64 | `pokachy_0.1.5_linux_amd64.tar.gz` | `487c00b863c1a34affc635121aa0c52a946614b3855486ea7c5429f557a9f04a` |
| arm64 | `pokachy_0.1.5_linux_arm64.tar.gz` | `12a4ade21b25b9875d19232cdccafee5fd48713b982a0f73944be43b4eaca3e4` |

Download the archive and `SHA256SUMS` from the [v0.1.5 GitHub release](https://github.com/yamz8/pokachy/releases/tag/v0.1.5), then verify the downloaded file before extracting it:

```sh
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf pokachy_0.1.5_linux_amd64.tar.gz
cd pokachy_0.1.5_linux_amd64
bash scripts/install.sh
pokachy version
```

Use the `arm64` filename on ARM Linux. The recorded amd64 artifact was isolated-install verified; arm64 was cross-build and content verified, not executed on ARM hardware. Re-check the release's `SHA256SUMS` before every install; copied checksums in this document are release records, not a substitute for verifying the downloaded manifest.

## Invite-only pilot

Keep the initial cohort small and individually invited. There are no pilot participants yet; recruiting an explicitly invited initial cohort remains a launch gate. Before each invitation, confirm the person has a supported Linux desktop, `~/.local/bin` on `PATH`, `notify-send`, and a compatible notification service. Give them the exact release link and checksum procedure above, not an unpinned repository snapshot.

For each pilot participant, record consent, installation architecture, release tag, checksum result, onboarding outcome, service status, and the support route agreed for the pilot. Private support is available at `support@pokachy.com`: Cloudflare routing is enabled, the forwarding destination is verified, and the owner confirmed successful inbox delivery after sending a test email. Ask participants to test email sign-in or GitHub sign-in, device-code matching, adding and accepting one friend, one received poke, quiet mode, blocking, logout, and service restart. Do not ask participants to share device codes, session tokens, email codes, screenshots containing them, or account data beyond what is needed for their support request.

Before widening the pilot, review support volume, delivery failures, reports, account/session revocation behavior, and the monitoring checks below. A real moderation exercise requires explicitly authorized disposable accounts: create no report or suspension against an owner or ordinary pilot account. A positive suspension revokes the target's sessions, so document consent and the expected recovery path first.

## Read-only monitoring and response

Once merged into the default branch, `.github/workflows/monitor.yml` runs the existing `scripts/verify-live.py` every 30 minutes and can also be dispatched manually. It makes only unauthenticated public HTTPS requests to `https://pokachy.com`; it creates no account, email, poke, deployment, secret, or Cloudflare API request. Each run writes and retains `artifacts/live-verification.json` as a GitHub Actions artifact for 30 days. GitHub schedules may be delayed; this is a basic smoke monitor, not a real-time availability guarantee.

The workflow fails when health, public configuration, production development-mail isolation, unauthenticated access control, homepage, or public assets fail. It uses no production secrets and has only `contents: read` permission. A scheduled failure should produce the normal GitHub Actions workflow-failure notification for watchers/subscribers whose GitHub notification settings allow it. Configure and test those notifications for the operations owner; GitHub workflow notifications and a retained report are not a substitute for independently configured external paging or alerting.

On a monitor failure, download the report, compare it with the prior successful run, check Cloudflare Workers status/logs and DNS, and decide whether the condition is transient. Do not automatically deploy, roll back, retry migrations, or expose credentials from a smoke failure. Follow the recovery procedure in [deployment](deployment.md) if a deployment is implicated.

## Recovery rehearsal

Run `python3 scripts/rehearse-recovery.py` for the disposable local SQL exercise. It uses the real initial schema, backs up synthetic account/session records, demonstrates how restore resurrects deleted data, then reapplies deletion and session revocation. Its fresh report is `artifacts/recovery-rehearsal.json`. This does not exercise Cloudflare Worker version rollback or D1 Time Travel; the hosted rehearsal below remains separate.

The repository has deployment recovery metadata and a documented rollback path, but no recovery rehearsal is recorded here. Rehearse only in an isolated disposable environment or against a deliberately selected non-production Worker and database. Never restore the production D1 database, use real inboxes, or suspend real accounts for a rehearsal.

1. Create disposable test accounts and an isolated Worker/D1/queue configuration with private test inboxes or the local development mail path.
2. Deploy a known-compatible revision, record its version ID and a D1 recovery point, then make an additive test change that has a documented rollback path.
3. Perform the documented code rollback to the exact prior version, run `npm run verify:live -- --base-url https://ISOLATED_ORIGIN --expected-revision PRIOR_GIT_SHA` against the isolated origin, and verify the old device session and public endpoints behave as expected.
4. Practice the decision process for D1 recovery without executing a production restore: identify writes that would be lost, the owner authorized to approve it, and the user communication needed.
5. Save the date, operator, version IDs, observations, and gaps in an internal run record. Remove the disposable accounts and resources after evidence is retained.

## Outstanding launch gates

- Configure a named operations owner and external alerts for Worker availability/errors, D1 errors and capacity, queue depth/retries, dead-letter queue growth, email-delivery failures, domain/DNS expiry, and Cloudflare/GitHub billing or quota events. No dashboard alert, billing alert, or dead-letter-queue alert configuration is evidenced by this repository.
- Test that the GitHub Actions failure notification reaches the responsible operator. The scheduled workflow is added here but has not been observed running in this change.
- Complete the isolated recovery rehearsal above and keep the result with the deployment record.
- Complete positive report, moderation, suspension, and session-revocation validation only with disposable accounts explicitly authorized for that purpose.
- Deploy and review the implemented privacy/support/deletion pages and moderation procedures in `docs/deployment.md`. Support forwarding and inbox delivery are verified; operational alert configuration and delivery remain separate outstanding checks.
- Recruit the first explicitly invited pilot cohort and confirm that its agreed support route works before distributing the published release.
- Re-test live OAuth consent, email delivery, inbox delivery, and desktop notifications for the intended pilot environment. The read-only smoke monitor cannot prove them.
