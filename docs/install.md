# Install Pokachy

Pokachy supports Linux on `amd64` (most PCs) and `arm64` (ARM machines). It installs into your user directory and never needs `sudo`.

## Quick start

```sh
curl -fsSL https://pokachy.com/install.sh | sh
pokachy init
```

The installer downloads the current release for your architecture, verifies its SHA-256 checksum, and installs the command locally. `pokachy init` opens a browser to sign in, asks you to compare a device code, and completes this computer's connection.

If you use Omarchy, setup can offer its native plugin installer. You can add the bar plugin later with:

```sh
omarchy plugin add https://github.com/yamz8/pokachy-omarchy.git --enable
```

Omarchy shows its own plugin confirmation before enabling the panel. See the [Omarchy panel guide](omarchy.md) for how the widget works.

## Review the installer first

Download and inspect the bootstrap script before executing it:

```sh
curl -fsSLO https://pokachy.com/install.sh
less install.sh
bash install.sh
```

The script uses the public release archives and their `SHA256SUMS` file. It creates a temporary directory, verifies the archive, runs the packaged installer, and removes the temporary files afterward.

## Fully manual install

1. Download the matching `pokachy_linux_amd64.tar.gz` or `pokachy_linux_arm64.tar.gz` archive and `SHA256SUMS` from the same [GitHub release](https://github.com/yamz8/pokachy/releases/latest).
2. Verify the archive before extracting it:

   ```sh
   archive=pokachy_linux_amd64.tar.gz
   awk -v name="$archive" '$2 == name { print }' SHA256SUMS | sha256sum -c -
   ```

3. Extract the verified archive and run its packaged installer:

   ```sh
   # The first directory listed is the versioned release directory.
   tar -tzf pokachy_linux_amd64.tar.gz | sed -n '1p'
   tar -xzf pokachy_linux_amd64.tar.gz
   cd pokachy_<version>_linux_amd64
   bash scripts/install.sh
   pokachy init
   ```

   Replace `<version>` with the release directory printed by the first command. Substitute the arm64 archive and directory name when installing on ARM.

The packaged installer installs the command at `~/.local/bin/pokachy`, a user service at `~/.config/systemd/user/pokachy.service`, and an application icon. It does not download software, use `sudo`, start services, or alter your credentials.

## PATH and notifications

Open a new terminal after installation. If `pokachy` is not found, ensure `~/.local/bin` is on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Add the equivalent line to the startup file for your shell if needed, then open a new terminal. You can always run the installed command directly at `~/.local/bin/pokachy`.

`pokachy init` normally enables background notifications on systems with systemd user services. To repair or perform that step manually:

```sh
systemctl --user daemon-reload
systemctl --user enable --now pokachy.service
```

Pokachy sends desktop notifications through `notify-send`. Install a libnotify-compatible notification utility and use a desktop with a notification service if `notify-send` is unavailable.

## Without systemd

The command-line client works without systemd. To keep background notifications running for the current terminal session, run:

```sh
pokachy daemon
```

Use your system's process manager if you want to supervise it persistently. Without the daemon, you can still use `pokachy inbox` and the other CLI commands.

## Upgrade

Use Pokachy’s managed updater:

```sh
pokachy update
```

It asks before downloading the release installer and `SHA256SUMS`, verifies the installer before execution, and then verifies the selected release archive as well. The update preserves your session and cached state, reloads and restarts the systemd user service when available, and updates the installed Omarchy panel through Omarchy’s plugin manager. Use `pokachy update --yes` only in an already-reviewed non-interactive workflow.

For a custom destination, set an absolute `POKACHY_INSTALL_PREFIX` for install, update, and uninstall so the lifecycle commands operate on the same prefix.

To check your installation, connection, notification dependencies, systemd service, and Omarchy panel state:

```sh
pokachy doctor
```

## Uninstall

Remove Pokachy from this computer with its managed uninstaller:

```sh
pokachy uninstall
```

It asks for confirmation, stops and disables the user service, removes this computer’s credentials and cache, and removes the Omarchy panel through `omarchy plugin remove` when it can confirm the panel is installed. Use `pokachy uninstall --yes` only in an already-reviewed non-interactive workflow.

Local uninstall does not delete your Pokachy account or affect other devices. Manage signed-in devices from [your account](https://pokachy.com/account), or permanently delete the account from the [account deletion page](https://pokachy.com/delete-account.html).

## Troubleshooting

| Problem | What to do |
| --- | --- |
| `pokachy: command not found` | Open a new terminal and add `~/.local/bin` to `PATH`, or run `~/.local/bin/pokachy`. |
| Installer rejects the archive | Delete the download and retry. Never extract an archive whose checksum does not verify. |
| Browser does not open during `init` | Copy the URL printed by the command into a browser on the same computer, then compare the device code before approving. |
| No notifications arrive | Run `pokachy doctor`, ensure `notify-send` is installed, and make sure your desktop notification service is running. |
| No systemd user manager | Run `pokachy daemon` from a terminal or use a local supervisor. |
| Omarchy panel is missing | Run `omarchy plugin add https://github.com/yamz8/pokachy-omarchy.git --enable` and choose its bar placement when Omarchy asks. |

For help, open a [GitHub issue](https://github.com/yamz8/pokachy/issues) without private data, or contact [Pokachy support](https://pokachy.com/support.html).
