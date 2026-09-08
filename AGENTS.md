# Working on Pokachy

Pokachy is one monorepo: Cloudflare Worker/browser onboarding in `server/`, Go CLI/daemon in `cli/`, and the native Omarchy panel in `plugins/omarchy/`.

## Verify changes

- Run `npm run verify` after changes affecting authentication, the API, CLI, daemon, or their integration. It checks types, Worker tests, Go tests/static analysis, and the real CLI against an isolated local Worker.
- Use `npm run verify:e2e` to reproduce a native integration failure. See `docs/verification.md` for prerequisites and coverage.
- Inspect `artifacts/verification.json` and the command exit status. A report from an earlier run is not evidence for new changes. Do not claim success if the command failed or did not finish.
- UI changes also need browser verification; QML changes need Omarchy validation. The local suite does not verify GitHub consent, inbox delivery, DNS, browser appearance, or a real desktop notification server.
- Keep integration fixtures local and disposable. Do not weaken production authentication, reset the normal developer database, send test messages to unapproved inboxes, or read live secrets into reports.
- Keep credentials under ignored private files. Never commit `.secrets/`, generated binaries, runtime databases, or test artifacts.

## Delegation preference

The owner prefers cost-conscious delegation. Use cheaper models for bounded implementation and review tasks when available (Luna for small reviews, Terra for implementation), with short task-specific context. The primary agent handles architecture, integration, security decisions, and final verification. Delegate only independent useful work; avoid duplicate test runs and unnecessary agents. Lower per-token pricing does not guarantee fewer total tokens or less plan usage.
