# Omarchy panel

The Pokachy monorepo keeps the panel source in `plugins/omarchy/`. Tested releases publish those files to a minimal standalone plugin repository. The panel reads the CLI's local cache through `pokachy watch --json`; it never reads credentials or tokens.

Install and connect the `pokachy` CLI first. `pokachy init` offers this native Omarchy installation automatically, or you can install or repair the panel later with:

```sh
pokachy omarchy install
```

Pokachy delegates new installations to `omarchy plugin add`, so Omarchy still validates the plugin, asks for confirmation, and prompts for its bar placement. Manage it later with `omarchy plugin update com.pokachy.poke` or `omarchy plugin remove com.pokachy.poke`. The widget hot-reloads after plugin updates or shell layout changes.

The panel launches `pokachy init` in Omarchy's floating terminal when the computer is not connected. It shows cached data as offline when the companion has not synced recently. Middle-click the bar icon refreshes the local status. Its CLI processes preserve Omarchy's existing `PATH` order and also check `~/.local/bin`, the packaged install location, so the panel can find a normal per-user install even when the desktop session did not inherit that directory.
