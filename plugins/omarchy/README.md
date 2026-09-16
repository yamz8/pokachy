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
