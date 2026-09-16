# Pokachy for Omarchy

The native Omarchy bar panel for [Pokachy](https://pokachy.com). Install and
connect the Pokachy CLI first, then add the panel through Omarchy:

```sh
omarchy plugin add https://github.com/yamz8/pokachy-omarchy.git --enable
```

The panel reads the CLI's local cache through `pokachy watch --json`. It does
not read Pokachy credentials or session tokens. It keeps the desktop's
existing `PATH` order and also checks `~/.local/bin`, where the packaged
installer places `pokachy`.

Open the panel and select the profile-settings button beside notification
quiet mode to change your public handle or display name. The profile picture
itself is clickable there: choose a PNG, JPEG, WebP, or AVIF file up to 2 MB,
or remove the upload to return to the linked GitHub picture.

The settings view also reports whether GitHub is linked. Connecting it opens
an explicit linking confirmation in the system browser; GitHub credentials and
OAuth tokens never pass through the panel.

The Account section shows the signed-in email in full. Changing it opens the
system browser for verification of both the current inbox and the new inbox;
the old address remains active until both six-digit code steps succeed.
Browser settings links use a five-minute, single-use handoff tied to the
panel's signed-in CLI account. A browser signed in as another Pokachy user is
stopped before any settings can change.
