# Omarchy panel

Pokachy ships an Omarchy bar-widget manifest at the repository root. The panel is in `plugins/omarchy/` and reads the CLI's local cache through `pokachy watch --json`; it never reads credentials or tokens.

Build and install the `pokachy` CLI first, including its user companion service. Then install this repository as an Omarchy plugin using the public repository URL:

```sh
omarchy plugin add https://github.com/yamz8/pokachy.git --enable
```

Add **Pokachy** from the bar widgets settings, or add `com.pokachy.poke` to the preferred `bar.layout` section in `~/.config/omarchy/shell.json`. The widget hot-reloads after plugin files or shell layout change.

The panel launches `pokachy init` in Omarchy's floating terminal when the computer is not connected. It shows cached data as offline when the companion has not synced recently. Middle-click the bar icon refreshes the local status.
