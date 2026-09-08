# Architecture

Pokachy connects Linux friends through a central service. Registration is open; poking requires a mutual friendship. There is no public directory, chat, or feed.

## One repository

The TypeScript server, Go CLI, and QML Omarchy panel share one repository and release version. The root manifest points to `plugins/omarchy/BarWidget.qml`; no separate plugin repository is required. Version tags produce Linux amd64 and arm64 archives.

## Server and identity

Hono runs in a Cloudflare Worker, with the onboarding/account UI served through Worker Static Assets on the same origin. D1 stores accounts, sessions, profiles, friendships, blocks, reports, and pokes. Drizzle adapts Better Auth's schema to D1; application queries use prepared D1 statements.

Better Auth supplies email codes, GitHub OAuth, browser sessions, and device authorization. The CLI requests a short-lived device code, opens a browser, and polls until the signed-in user reviews and approves the matching code. Local Git name/email and username are suggestions only; the user confirms and can edit them. They never prove identity. Browser sessions use secure cookies in production; the CLI stores a revocable bearer session with private file permissions.

Turnstile gates email-code requests. The backend requires success, the `login` action, and the exact production hostname. A Queue delivers codes using Cloudflare Email Service, retries temporary failures, and discards expired codes. Development uses a loopback-only email simulator. Production fails closed without its security configuration.

## Pokes and live delivery

A normalized friendship pair requires recipient acceptance. A conditional SQL insert checks friendship, blocking, suspension, existing outstanding pokes, and the ten-second cooldown. A D1 batch also resolves the reverse pending poke when replying. Unique sender/request keys make retries idempotent.

A Durable Object per user holds hibernating WebSockets. It checks session validity before publishing a `sync` signal. The signal contains no inbox data; clients retrieve current state from the authenticated HTTP API. Separate object names implement request budgets. Reconnects and periodic refresh recover missed signals.

## Desktop

The Go executable provides the CLI and notification daemon. The daemon reconnects with backoff, refreshes state periodically, and invokes `notify-send` with an argument array. Persistent notified IDs prevent duplicates after restarting. Quiet mode preserves the inbox and suppresses notifications; resuming does not replay suppressed notifications.

Credentials and cache live in `$XDG_CONFIG_HOME/pokachy`, normally `~/.config/pokachy`, with 0700 directories and 0600 files. `POKACHY_CONFIG_DIR` supports isolated instances. HTTP is allowed only for loopback development; redirects cannot forward bearer tokens. Omarchy reads `pokachy watch --json` and invokes CLI commands, never reading credentials itself.

## Operations

This release has one central service, no federation, and no mobile push. Blocking removes the friendship and pending pokes. Users can report abuse; configured administrator IDs can inspect reports and suspend accounts. Device sessions can be revoked from the account page.

D1 is the source of truth. WebSocket signals are disposable hints. Database recovery is independent of Worker rollback; migrations should remain additive. Monitor Workers and Queue failures, protect secrets, and test new releases with inboxes you control.
