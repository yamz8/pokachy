# Early-release announcement

Suggested X post (attach the demo):

> I built Pokachy 🦜
> A tiny way to say hey to your Linux friends.
> No feed. No chat. Just a poke that lands on their desktop.
>
> Native Omarchy panel + Linux CLI. Open source.
>
> Early release—try it with a friend: https://pokachy.com

## Assets

- Demo: `artifacts/launch/pokachy-demo.mp4` (local, ignored; 22 seconds, 1200×630, H.264). It shows a real incoming notification, the native panel, and the return-poke state. Commands were issued offscreen over the two authorized desktop sessions. No audio.
- Social preview: `server/public/pokachy-social.png` (1200×630). Editable source: `assets/brand/pokachy-social-card.svg`.
- Download: https://github.com/yamz8/pokachy/releases/tag/v0.2.0
- Support: support@pokachy.com

Review the demo before posting: it contains the native panel's visible friend names/avatars. The recording was trimmed to the empty-workspace test; unrelated application windows are excluded from the exported clip.

## Before publishing

Deploy the homepage metadata/image through the normal tested production workflow, then check that the public image URL and metadata are available. A local image inspection does not prove X's crawler has refreshed its preview. Browser verification remains pending because the browser runtime could not load its required module during preparation.

Keep the announcement framed as an early release. See `launch-readiness.md` for current evidence and outstanding checks; existing-session desktop delivery does not establish fresh live signup, OAuth consent, account deletion, or alert-email delivery. ARM64 remains cross-build verified only.

No X post was sent during preparation.
