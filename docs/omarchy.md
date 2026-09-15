# Omarchy panel

The Pokachy monorepo keeps the panel source in `plugins/omarchy/`. Tested releases publish those files to a minimal standalone plugin repository. The panel reads the CLI's local cache through `pokachy watch --json`; it never reads credentials or tokens.

Install and connect the `pokachy` CLI first. `pokachy init` offers this native Omarchy installation automatically, or you can run it yourself:

```sh
omarchy plugin add https://github.com/yamz8/pokachy-omarchy.git --enable
```

Omarchy validates the plugin, asks for confirmation, and prompts for its bar placement. Manage it later with `omarchy plugin update com.pokachy.poke` or `omarchy plugin remove com.pokachy.poke`. The widget hot-reloads after plugin updates or shell layout changes.

The panel launches `pokachy init` in Omarchy's floating terminal when the computer is not connected. It shows cached data as offline when the companion has not synced recently. Middle-click the bar icon refreshes the local status.
