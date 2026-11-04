# Panel appearance

Open **Settings > Appearance**. Select a theme using the dropdown or preview
cards, then choose **Save theme**. **Cancel preview** restores the last saved
selection. **Default** preserves the original LiteCP design.

Available presets: Default, shadcn/ui, Material UI (MUI), Mantine, HeroUI,
Chakra UI, PrimeReact, Fluent UI, daisyUI and Base UI.

These are native LiteCP visual presets inspired by the named design systems,
not installations of those component libraries. LiteCP keeps its existing
HTML/JavaScript controls, APIs and hosting workflows. All presets use dark
surfaces; the file editor stays dark. No external fonts or theme assets are
downloaded.

The selection is stored under `litecp.uiTheme` in this browser's local storage.
It survives refreshes and panel updates and synchronizes between open tabs
on the same origin. Other browsers and devices choose independently. Changing
the panel address or clearing browser storage returns to Default. If storage
is unavailable, previews still work and the page reports that saving failed.

The existing Makefile remains the build entry point. Theme checks can be run
without building or packaging:

```text
node --test scripts/appearance.test.cjs scripts/theme-toggle.test.cjs
```
