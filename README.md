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
  <strong><a href="https://github.com/yamz8/pokachy/releases/latest">Download</a></strong> ·
  <strong><a href="docs/omarchy.md">Omarchy panel</a></strong>
</p>

## Install

Download the Linux release archive for your architecture: `amd64` for most PCs, `arm64` for ARM machines, plus its `SHA256SUMS` file from the same [GitHub release](https://github.com/yamz8/pokachy/releases/latest). Verify the downloaded archive before extracting it:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

From the extracted directory:

```sh
bash scripts/install.sh
pokachy init
systemctl --user daemon-reload
systemctl --user enable --now pokachy.service
```

The installer copies the CLI into `~/.local/bin` and installs a user service. Ensure that directory is on PATH. Notifications require `notify-send` (libnotify) and a compatible desktop notification service. systemd runs the background companion; the CLI also works independently on other Linux systems.

Sign in with email or GitHub, choose a handle, compare the browser device code with your terminal, and approve it. Omarchy/Git setup values can prefill editable suggestions with your confirmation. Email verification is still required.

To upgrade, download and verify the newer release, extract it, run its `bash scripts/install.sh`, then reload and restart the user service:

```sh
systemctl --user daemon-reload
systemctl --user restart pokachy.service
pokachy version
```

The installer replaces the local binary and service unit but preserves your local session. To uninstall, sign out and stop the service, then remove the installed files. Logout clears this computer's credentials and offline cached state; removing `~/.config/pokachy` removes any remaining local client files. Use account settings to revoke other devices.

```sh
pokachy logout
systemctl --user disable --now pokachy.service
rm -f ~/.local/bin/pokachy ~/.config/systemd/user/pokachy.service ~/.local/share/icons/hicolor/scalable/apps/pokachy.svg
rm -rf ~/.config/pokachy
systemctl --user daemon-reload
```

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

See [architecture](docs/architecture.md), [deployment](docs/deployment.md), and [launch readiness](docs/launch-readiness.md). Licensed under [MIT](LICENSE).
