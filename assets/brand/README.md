# Pokachy pixel parrot

The supplied green-and-paper parrot reference is expressed on a uniform pixel grid, cropped to its 12 × 16 occupied cells, without its background, glow, or shadow.

- `pokachy-parrot.svg`: transparent full-color master and desktop notification icon.
- `pokachy-symbol.svg`: monochrome symbol; `currentColor` follows its surrounding surface. A cutout separates the face from the feathers so the bird remains recognizable in one color.
- `plugins/omarchy/BrandIcon.qml`: native square-cell rendering of the same grid, used by the bar and poke actions. No raster image scaling or recoloring effect is required.

Prefer integer cell sizes: 16, 32, 48, or 64 pixels. The bar centers a 16-pixel mark in the shell's theme-scaled icon slot; actions use a 32-pixel canvas. Actual cell sizes are rounded down to whole pixels. Do not add glow, circles, decorative particles, or a background tile.

Keep personal avatars distinct: user initials (and later profile photos) identify people; the parrot identifies Pokachy and the poke action. Icon-only actions must retain contextual tooltips and accessible names. Incoming replies use the shell's urgent color; waiting actions are dimmed and disabled.

The installer places the full-color master at `<prefix>/share/icons/hicolor/scalable/apps/pokachy.svg`. The daemon uses that asset when available and falls back to the system mail icon otherwise. Website/onboarding branding is outside this panel slice.
