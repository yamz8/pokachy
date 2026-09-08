# Pokachy

A little nudge for your Linux friends. Poke from your terminal or Omarchy bar and receive desktop notifications.

**[pokachy.com](https://pokachy.com)** · **[Download](https://github.com/yamz8/pokachy/releases/latest)** · **[Omarchy panel](docs/omarchy.md)**

## Install

Download and extract the Linux release archive for your architecture: `amd64` for most PCs, `arm64` for ARM machines. Each release includes `SHA256SUMS`. From the extracted directory:

```sh
bash scripts/install.sh
pokachy init
systemctl --user daemon-reload
systemctl --user enable --now pokachy.service
```

The installer copies the CLI into `~/.local/bin` and installs a user service. Ensure that directory is on PATH. Notifications require `notify-send` (libnotify) and a compatible desktop notification service. systemd runs the background companion; the CLI also works independently on other Linux systems.

Sign in with email or GitHub, choose a handle, compare the browser device code with your terminal, and approve it. Omarchy/Git setup values can prefill editable suggestions with your confirmation. Email verification is still required.

## Say hey

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

After installing and connecting the CLI:

```sh
omarchy plugin add https://github.com/yamz8/pokachy.git --enable
```

Add Pokachy through Omarchy's bar widget settings. See [the panel guide](docs/omarchy.md). The CLI, server, and plugin are maintained in this one repository.

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
POKACHY_CONFIG_DIR=/tmp/pokachy-dev cli/bin/pokachy init --server http://127.0.0.1:8787
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

See [architecture](docs/architecture.md) and [deployment](docs/deployment.md). Licensed under [MIT](LICENSE).
