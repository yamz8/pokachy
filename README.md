<p align="center">
  <img src="server/public/pokachy-parrot-web.png" width="128" alt="Pokachy pixel parrot">
</p>

<h1 align="center">Pokachy</h1>

<p align="center">
  <strong>Poke. That's it.</strong><br>
  A tiny signal between Linux friends. No feed, no chat, no noise.
</p>

<p align="center">
  <strong><a href="https://pokachy.com">pokachy.com</a></strong> ·
  <strong><a href="https://pokachy.com/install">Install</a></strong> ·
  <strong><a href="docs/omarchy.md">Omarchy panel</a></strong>
</p>

## Get started

Pokachy supports Linux on amd64 and arm64. Install and connect this computer:

```sh
curl -fsSL https://pokachy.com/install.sh | sh
pokachy init
```

Follow the browser prompt to sign in and approve the device code. Pokachy will connect your account, set up notifications where supported, and offer to add itself to your Omarchy bar.

Prefer to inspect first? Download the script before running it:

```sh
curl -fsSLO https://pokachy.com/install.sh
less install.sh
bash install.sh
```

For manual downloads, checksum verification, PATH and systemd help, upgrades, removal, and troubleshooting, read the [installation guide](docs/install.md).

## Say hey

Account settings include device revocation. You can also access [account deletion](https://pokachy.com/delete-account.html), [privacy information](https://pokachy.com/privacy.html), and [support](https://pokachy.com/support.html). Deletion requires a recent sign-in, an email code, and explicit confirmation. Local uninstall does not delete your server account.

```sh
pokachy friends add @friend
# Your friend accepts on their computer:
pokachy friends accept @yourhandle

pokachy poke @friend
pokachy inbox
pokachy quiet on
pokachy quiet off
pokachy block @person
pokachy report @person "Reason for this report"
pokachy account
```

Only mutual friends can poke. There is one outstanding poke per direction and a ten-second cooldown. Replying clears the poke you received. Quiet mode preserves the inbox while suppressing notifications. `pokachy help` lists commands; `--json` supports automation.

## Omarchy

Pokachy uses Omarchy's native plugin manager:

```sh
omarchy plugin add https://github.com/yamz8/pokachy-omarchy.git --enable
```

Omarchy will show its own plugin confirmation and placement prompt. See [the panel guide](docs/omarchy.md).

## Develop

Requirements: Node.js 22 or later supported by Wrangler, npm, and Go 1.24 or later.

```sh
npm ci
npm run db:migrate
npm run dev
```

Open `http://127.0.0.1:8787`. Local mode offers **Fill the development code** and sends no real emails. In another terminal:

```sh
npm run build:cli
POKACHY_CONFIG_DIR=/tmp/pokachy-dev cli/bin/pokachy init --server http://127.0.0.1:8787 --no-desktop
POKACHY_CONFIG_DIR=/tmp/pokachy-dev cli/bin/pokachy daemon
```

Use a different `POKACHY_CONFIG_DIR` for each development account. Production credentials under `.secrets/` are not automatically loaded by the local server.

```sh
npm run verify
```

See [verification](docs/verification.md) for the isolated end-to-end checks, machine-readable report, and remaining live checks.

The Workers-runtime tests cover authentication, friendship consent, concurrent/idempotent pokes, blocking, session isolation/revocation, live events, Turnstile rejection, and queue retries. Go tests cover trusted origins, redirects, API errors, and private storage.

## Repository

| Path | Purpose |
| --- | --- |
| `server/` | Worker API, D1 migrations, browser onboarding |
| `cli/` | Go CLI and notification daemon |
| `plugins/omarchy/` | Native bar panel |
| `manifest.json` | Omarchy repository entry point |
| `packaging/`, `scripts/` | Installation, secrets setup, releases |
| `.github/workflows/` | Checks and version-tag releases |

See [architecture](docs/architecture.md), [deployment](docs/deployment.md), and [launch readiness](docs/launch-readiness.md). Licensed under [MIT](LICENSE).
